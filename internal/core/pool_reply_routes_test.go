package core

import (
	"database/sql"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestPoolReplyToValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"", true}, {"reply@example.com", true}, {"Reply@example.com", true},
		{"bad", false}, {"Name <reply@example.com>", false}, {"one@example.com,two@example.com", false},
		{"reply@example.com\r\nBcc: injected@example.com", false},
	} {
		if err := validatePoolReplyTo(tc.value); (err == nil) != tc.valid {
			t.Errorf("validatePoolReplyTo(%q) = %v", tc.value, err)
		}
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
	row.ReplyTo = "second@example.com"
	result, err := env.core.ImportPoolContacts(pool, 1, []models.PoolContactImportRow{row}, false)
	if err != nil || result.Existing != 1 || result.Created != 0 {
		t.Fatalf("reimport: %+v, %v", result, err)
	}
	var reply string
	if err := env.db.Get(&reply, `SELECT reply_to FROM pool_contacts WHERE customer_code='REPLY-1'`); err != nil || reply != row.ReplyTo {
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
			for _, id := range []int64{withReply, withoutReply} {
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
			refresh()
			assertRoute(withReply, "registered@example.com", "contact", &contactMailbox)
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
