package core

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestPoolReplyToNormalization(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
		valid bool
	}{
		{"", "", true},
		{"reply@example.com", "reply@example.com", true},
		{"Reply@example.com", "Reply@example.com", true},
		{" one@example.com ", "one@example.com", true},
		{"one@example.com,two@example.com", "one@example.com, two@example.com", true},
		{"one@example.com; two@example.com", "one@example.com, two@example.com", true},
		{"ONE@example.com ，two@example.com；", "ONE@example.com, two@example.com", true},
		{"one@example.com, , two@example.com;", "one@example.com, two@example.com", true},
		{"one@example.com, ONE@example.com", "one@example.com", true},
		{"bad", "", false},
		{"Name <reply@example.com>", "", false},
		{"one@example.com, bad", "", false},
		{"one@example.com\nBcc: injected@example.com", "", false},
		{"reply@example.com\r\nBcc: injected@example.com", "", false},
		{",;，；", "", false},
		{"用户@example.com", "", false},
		{"reply@\u00a0example.com", "", false},
	} {
		got, err := normalizePoolReplyTo(tc.value)
		if (err == nil) != tc.valid || got != tc.want {
			t.Errorf("normalizePoolReplyTo(%q) = %q, %v; want %q (valid=%v)", tc.value, got, err, tc.want, tc.valid)
		}
	}
	// The SMTP header line limit covers the field name and the whole list.
	longest := strings.Repeat("a", 242) + "@example.com"
	if got, err := normalizePoolReplyTo(longest); err != nil || got != longest {
		t.Errorf("254-byte address rejected: %q, %v", got, err)
	}
	tooLongAddress := strings.Repeat("a", 243) + "@example.com"
	if _, err := normalizePoolReplyTo(tooLongAddress); err == nil {
		t.Error("255-byte address accepted")
	}
	var overLimit strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&overLimit, "b%d@example.com, ", i)
	}
	if _, err := normalizePoolReplyTo(overLimit.String()); err == nil {
		t.Error("address list over the header limit accepted")
	}
}

func TestPoolReplyImportUpdatesExistingAndRejectsUnsafeAddresses(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
		ALTER TABLE pool_contacts ADD COLUMN allocation_department TEXT NOT NULL DEFAULT '';
		CREATE TABLE pool_merge_conflicts (pool_id INTEGER,contact_id BIGINT,customer_code TEXT,
			existing_snapshot JSONB,incoming_snapshot JSONB,created_by_user_id INTEGER);`)
	org := env.seedOrganization("Reply department")
	pool := env.seedPool("Reply import")
	env.seedAllocation(pool, org)
	row := models.PoolContactImportRow{Row: 2, CustomerCode: "REPLY-1", Name: "Customer", Email: "customer@example.com",
		AllocationDepartment: "Reply department", ReplyTo: "first@example.com"}
	if _, err := env.core.ImportPoolContacts(pool, 1, []models.PoolContactImportRow{row}, false); err != nil {
		t.Fatal(err)
	}
	row.ReplyTo = "second@example.com; third@example.com;"
	result, err := env.core.ImportPoolContacts(pool, 1, []models.PoolContactImportRow{row}, false)
	if err != nil || result.Existing != 1 || result.Created != 0 {
		t.Fatalf("reimport: %+v, %v", result, err)
	}
	var reply string
	if err := env.db.Get(&reply, `SELECT reply_to FROM pool_contacts WHERE customer_code='REPLY-1'`); err != nil || reply != "second@example.com, third@example.com" {
		t.Fatalf("updated reply = %q, %v", reply, err)
	}
	row.ReplyTo = "bad\r\nBcc: bad@example.com"
	result, err = env.core.ImportPoolContacts(pool, 1, []models.PoolContactImportRow{row}, false)
	if err != nil || result.Invalid != 1 || result.Issues[0].Reason != "invalid_reply_to" {
		t.Fatalf("unsafe import: %+v, %v", result, err)
	}
	row.ReplyTo = ""
	if _, err := env.core.ImportPoolContacts(pool, 1, []models.PoolContactImportRow{row}, false); err != nil {
		t.Fatal(err)
	}
	if got := env.countRows(`SELECT COUNT(*) FROM pool_contacts WHERE customer_code='REPLY-1' AND reply_to=''`); got != 1 {
		t.Fatalf("cleared address count = %d", got)
	}
	// Reply addresses are internal routing data; recipient emails stay masked.
	safe := (models.PoolContact{Email: "customer@example.com", ReplyTo: "reply@example.com"}).Safe()
	if safe.Email == "customer@example.com" || safe.ReplyTo != "reply@example.com" {
		t.Fatalf("safe DTO = %+v", safe)
	}
}

func TestPoolReplyPrioritySnapshotsAndFallback(t *testing.T) {
	for _, allOrganizations := range []bool{false, true} {
		t.Run(map[bool]string{false: "organization", true: "all_organizations"}[allOrganizations], func(t *testing.T) {
			env := newPoolRecipientsTestEnv(t)
			org := env.seedOrganization("Reply org")
			pool := env.seedPool("Reply pool")
			allocation := env.seedAllocation(pool, org)
			mailbox := env.seedMailbox(org, "org@example.com")
			env.setOrganizationMailbox(org, &mailbox)
			contactMailbox := env.seedMailbox(org, "registered@example.com")
			foreignOrg := env.seedOrganization("Other org")
			env.seedMailbox(foreignOrg, "external@example.com")
			withReply := env.seedContact("A", "A", "a@example.com", "active")
			withoutReply := env.seedContact("B", "B", "b@example.com", "active")
			multiReply := env.seedContact("C", "C", "c@example.com", "active")
			for _, id := range []int64{withReply, withoutReply, multiReply} {
				env.joinPool(pool, id)
				env.allocate(allocation, id, "active")
			}
			env.exec(`UPDATE pool_contacts SET reply_to='external@example.com' WHERE id=$1`, withReply)
			camp := env.seedCampaign()
			env.seedAudience(camp, pool, org, nil, &mailbox)
			if allOrganizations {
				env.exec(`UPDATE campaigns SET pool_scope='all_organizations' WHERE id=$1`, camp)
			}
			refresh := func() {
				t.Helper()
				if err := env.core.EnsurePoolCampaignRecipients(camp); err != nil {
					t.Fatal(err)
				}
			}
			assertRoute := func(id int64, want, source string, mailboxID *int64) {
				t.Helper()
				var row struct {
					Email   string        `db:"reply_to_snapshot"`
					Source  string        `db:"reply_to_source"`
					Mailbox sql.NullInt64 `db:"reply_mailbox_id"`
				}
				if err := env.db.Get(&row, `SELECT reply_to_snapshot,reply_to_source,reply_mailbox_id FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, camp, id); err != nil {
					t.Fatal(err)
				}
				if row.Email != want || row.Source != source || row.Mailbox.Valid != (mailboxID != nil) || mailboxID != nil && row.Mailbox.Int64 != *mailboxID {
					t.Fatalf("contact %d route = %+v; want %s/%s/%v", id, row, want, source, mailboxID)
				}
			}
			refresh()
			assertRoute(withReply, "external@example.com", "contact", nil)
			assertRoute(withoutReply, "org@example.com", "organization", &mailbox)
			env.exec(`UPDATE campaigns SET pool_reply_priority='organization_first' WHERE id=$1`, camp)
			refresh()
			assertRoute(withReply, "org@example.com", "organization", &mailbox)
			env.exec(`UPDATE campaigns SET pool_reply_priority='contact_first' WHERE id=$1`, camp)
			env.exec(`UPDATE pool_contacts SET reply_to='registered@example.com' WHERE id=$1`, withReply)
			env.exec(`UPDATE pool_contacts SET reply_to='external@example.com, registered@example.com' WHERE id=$1`, multiReply)
			refresh()
			assertRoute(withReply, "registered@example.com", "contact", &contactMailbox)
			// The whole address list is the route; the registered member still
			// attributes the reply mailbox.
			assertRoute(multiReply, "external@example.com, registered@example.com", "contact", &contactMailbox)
			// A queued route is immutable; pending routes observe configuration changes.
			env.exec(`UPDATE campaign_pool_recipients SET status='queued' WHERE campaign_id=$1 AND pool_contact_id=$2`, camp, withReply)
			env.exec(`UPDATE pool_contacts SET reply_to='changed@example.com' WHERE id=$1`, withReply)
			env.exec(`UPDATE reply_mailboxes SET status='disabled' WHERE id=$1`, mailbox)
			refresh()
			assertRoute(withReply, "registered@example.com", "contact", &contactMailbox)
			env.exec(`UPDATE pool_contacts SET reply_to='fallback@example.com' WHERE id=$1`, withoutReply)
			env.exec(`UPDATE campaigns SET pool_reply_priority='organization_first' WHERE id=$1`, camp)
			refresh()
			assertRoute(withoutReply, "fallback@example.com", "contact", nil)
			// Only the organization-scoped validation does not also require SMTP.
			if !allOrganizations {
				if err := env.core.ValidatePoolCampaignAudience(camp); err != nil {
					t.Fatalf("customer-only routes rejected: %v", err)
				}
				env.exec(`UPDATE pool_contacts SET reply_to='' WHERE id=$1`, withoutReply)
				if err := env.core.ValidatePoolCampaignAudience(camp); err == nil {
					t.Fatal("accepted a customer with no reply route")
				}
			}
		})
	}
}

func TestPoolContactNameOnlyEditKeepsLegacyReplyTo(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
		ALTER TABLE pool_contacts ADD COLUMN allocation_department TEXT NOT NULL DEFAULT '';`)
	env.seedOrganization("Legacy department")
	pool := env.seedPool("Legacy pool")
	contact := env.seedContact("LEGACY", "Legacy", "legacy@example.com", "active")
	env.joinPool(pool, contact)
	env.exec(`UPDATE pool_contacts SET reply_to='用户@example.com', allocation_department='Legacy department' WHERE id=$1`, contact)

	updated, err := env.core.UpdatePoolContact(pool, contact, models.PoolContact{Name: "Renamed"}, true)
	if err != nil || updated.Name != "Renamed" || updated.ReplyTo != "用户@example.com" {
		t.Fatalf("name-only update = %+v, %v", updated, err)
	}
	if _, err := env.core.UpdatePoolContact(pool, contact, models.PoolContact{
		Name: "Renamed", Email: "legacy@example.com", ReplyTo: "用户@example.com",
	}, false); err == nil {
		t.Fatal("full edit accepted a non-ASCII reply address")
	}
}

func TestReplyAIDeliveryMatchesAnyListedReplyAddress(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
		ALTER TABLE campaigns ADD COLUMN uuid UUID NOT NULL DEFAULT gen_random_uuid();
		ALTER TABLE campaigns ADD COLUMN reply_mailbox_id INTEGER;
		ALTER TABLE reply_mailboxes ADD COLUMN user_id INTEGER;
		CREATE TABLE customers (
			id SERIAL PRIMARY KEY,
			uuid UUID NOT NULL DEFAULT gen_random_uuid(),
			email TEXT NOT NULL DEFAULT '',
			owner_user_id INTEGER,
			organization_id BIGINT,
			transfer_pending_at TIMESTAMPTZ
		);
		CREATE TABLE campaign_recipients (
			campaign_id INTEGER NOT NULL,
			customer_id INTEGER NOT NULL,
			status campaign_recipient_status NOT NULL DEFAULT 'pending',
			email_snapshot TEXT,
			reply_to_snapshot TEXT
		);`)
	org := env.seedOrganization("Attribution org")
	pool := env.seedPool("Attribution pool")
	allocation := env.seedAllocation(pool, org)
	first := env.seedMailbox(org, "first@example.com")
	second := env.seedMailbox(org, "second@example.com")
	unlisted := env.seedMailbox(org, "unlisted@example.com")
	foreignOrg := env.seedOrganization("Attribution other org")
	foreign := env.seedMailbox(foreignOrg, "foreign@example.com")
	env.exec(`UPDATE reply_mailboxes SET user_id=1`)
	contact := env.seedContact("MULTI", "Multi", "customer@example.com", "active")
	env.joinPool(pool, contact)
	env.allocate(allocation, contact, "active")
	campaign := env.seedCampaign()
	env.exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,
		organization_id,reply_mailbox_id,status,email_snapshot,reply_to_snapshot,reply_to_source)
		VALUES($1,$2,$3,$4,$5,$6,'sent','customer@example.com','first@example.com；second@example.com；foreign@example.com','contact')`,
		campaign, contact, pool, allocation, org, first)
	// Full-width separators in a stored snapshot still split; the reply lands in
	// the second listed mailbox while the snapshot is attributed to the first.
	got, ok, err := findReplyAIDelivery(env.db, int(second), "customer@example.com", 0, "")
	if err != nil || !ok {
		t.Fatalf("attribution = %+v, %v, %v", got, ok, err)
	}
	if got.CampaignID != campaign || got.PoolContactID != contact || got.PoolID != pool || got.OrganizationID != int(org) {
		t.Fatalf("attribution = %+v; want campaign %d contact %d pool %d org %d", got, campaign, contact, pool, org)
	}
	// A mailbox that is not in the list, or belongs to another organization,
	// must never be attributed.
	for _, mailbox := range []int64{unlisted, foreign} {
		if _, matched, err := findReplyAIDelivery(env.db, int(mailbox), "customer@example.com", 0, ""); err != nil || matched {
			t.Fatalf("mailbox %d matched outside the listed addresses: %v, %v", mailbox, matched, err)
		}
	}
}

func TestPoolAddressOnlyReplyMailbox(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)
	org := env.seedOrganization("Address-only org")
	pool := env.seedPool("Address-only pool")
	allocation := env.seedAllocation(pool, org)
	mailbox := env.seedMailbox(org, "reply@example.com")
	env.exec(`UPDATE reply_mailboxes SET ai_enabled=FALSE,verified_at=NULL WHERE id=$1`, mailbox)
	env.setOrganizationMailbox(org, &mailbox)
	contact := env.seedContact("ADDR", "Customer", "customer@example.com", "active")
	env.joinPool(pool, contact)
	env.allocate(allocation, contact, "active")
	campaign := env.seedCampaign()
	env.seedAudience(campaign, pool, org, nil, &mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err != nil {
		t.Fatalf("address-only reply route rejected: %v", err)
	}
	if err := env.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatal(err)
	}
	var reply string
	if err := env.db.Get(&reply, `SELECT reply_to_snapshot FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, campaign, contact); err != nil || reply != "reply@example.com" {
		t.Fatalf("address-only reply snapshot = %q, %v", reply, err)
	}
	env.exec(`UPDATE reply_mailboxes SET ai_enabled=TRUE WHERE id=$1`, mailbox)
	if err := env.core.ValidatePoolCampaignAudience(campaign); err == nil {
		t.Fatal("AI mailbox without a verified receiving connection was accepted")
	}
}
