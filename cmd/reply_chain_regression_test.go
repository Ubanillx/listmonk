package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/replyai"
	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

// Regression tests use disposable databases and local fake services only.
func auditReplyApp(t *testing.T) (*App, int) {
	t.Helper()
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	if _, err := a.db.Exec(`UPDATE users SET status='enabled'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE settings SET value=jsonb_set(value,'{enabled}','true') WHERE key='reply_ai'`); err != nil {
		t.Fatal(err)
	}
	var mailbox int
	if err := a.db.Get(&mailbox, `INSERT INTO reply_mailboxes
		(user_id,organization_id,email,username,password,status,ai_enabled,verified_at)
		VALUES(2,101,'reply@example.com','test-user','test-secret','active',TRUE,NOW()) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	return a, mailbox
}

func auditCustomer(t *testing.T, a *App, owner int, address string) int {
	t.Helper()
	var id int
	if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,organization_id,owner_user_id)
		VALUES(gen_random_uuid(),$1,'Review customer',101,$2) RETURNING id`, address, owner); err != nil {
		t.Fatal(err)
	}
	return id
}

func auditCampaign(t *testing.T, a *App, owner, mailbox int) int {
	t.Helper()
	var id int
	if err := a.db.Get(&id, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,organization_id,owner_user_id,reply_mailbox_id)
		VALUES(gen_random_uuid(),'Review campaign','Review','sender@example.com','Review body','email',101,$1,$2) RETURNING id`, owner, mailbox); err != nil {
		t.Fatal(err)
	}
	return id
}

func auditPoolSnapshot(t *testing.T, a *App, mailbox int, address, state string) (int64, int64) {
	t.Helper()
	pool := seedOrgPoolAllocationPool(t, a.db, "Review pool")
	var list, allocation, contact int64
	if err := a.db.Get(&list, `INSERT INTO customer_lists(uuid,name,type,organization_id,owner_user_id)
		VALUES(gen_random_uuid(),'Review allocation','org_pool_allocation',101,2) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&allocation, `INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id)
		VALUES($1,$2,101,2) RETURNING id`, list, pool); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&contact, `INSERT INTO pool_contacts(email,name,customer_code)
		VALUES($1,'Review pool customer',$2) RETURNING id`, address, fmt.Sprintf("AUDIT-%d", pool)); err != nil {
		t.Fatal(err)
	}
	campaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,reply_mailbox_id,email_snapshot,status,reply_to_snapshot)
		VALUES($1,$2,$3,$4,101,$5,$6,$7,(SELECT email FROM reply_mailboxes WHERE id=$5))`, campaign, contact, pool, allocation, mailbox, address, state); err != nil {
		t.Fatal(err)
	}
	return contact, allocation
}

func auditEvent(t *testing.T, a *App, mailbox int, address string) models.ReplyAIEvent {
	t.Helper()
	var id int
	if err := a.queries.InsertReplyAIEvent.Get(&id, mailbox, "review-message", address,
		"Re: review", "Please unsubscribe me", "test-hash", nil, ""); err != nil {
		t.Fatal(err)
	}
	var event models.ReplyAIEvent
	if err := a.queries.ClaimReplyAIEvent.Get(&event); err != nil {
		t.Fatal(err)
	}
	return event
}

func auditClassifier(t *testing.T, a *App, during func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if during != nil {
			during()
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"unsubscribe\",\"confidence\":0.99,\"reason_code\":\"explicit_unsubscribe\"}"}}]}`)
	}))
	t.Cleanup(server.Close)
	client, err := replyai.New(replyai.Options{Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "test-model", Timeout: "5s", MinConfidence: 0.98})
	if err != nil {
		t.Fatal(err)
	}
	a.replyAI = client
}

func TestAuditSharedMailboxMemberReply(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 3, "member-customer@example.com")
	campaign := auditCampaign(t, a, 3, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,'sent')`, campaign, customer); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 101}, UserID: 3}
	if err := a.validateCampaignReplyMailbox(access, &models.Campaign{ReplyMailboxID: null.IntFrom(mailbox)}); err != nil {
		t.Fatal(err)
	}
	auditClassifier(t, a, nil)
	event := auditEvent(t, a, mailbox, "member-customer@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var state, reason string
	if err := a.db.QueryRow(`SELECT status,reason_code FROM reply_ai_events WHERE id=$1`, event.ID).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "processed" {
		t.Fatalf("shared mailbox accepted for member campaign, but its sent customer's reply became %s/%s", state, reason)
	}
}

func TestAuditPoolReplyPrivateCollision(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 2, "both@example.com")
	auditPoolSnapshot(t, a, mailbox, "both@example.com", "sent")
	auditClassifier(t, a, nil)
	event := auditEvent(t, a, mailbox, "both@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var status string
	var exclusions int
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customer); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&exclusions, `SELECT COUNT(*) FROM org_pool_allocation_exclusions`); err != nil {
		t.Fatal(err)
	}
	if status != "enabled" || exclusions != 1 {
		t.Fatalf("reply to sent pool campaign changed private customer to %s; pool exclusions=%d", status, exclusions)
	}
}

func TestAuditPoolPendingSnapshotMatch(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	auditPoolSnapshot(t, a, mailbox, "unsent@example.com", "pending")
	recipient, ok, err := a.core.FindReplyAIDelivery(mailbox, "unsent@example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("a pending, never-sent snapshot was accepted as reply source, campaign=%d", recipient.CampaignID)
	}
}

func TestAuditPoolAmbiguousSenderMatch(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	first, _ := auditPoolSnapshot(t, a, mailbox, "duplicate@example.com", "sent")
	second, _ := auditPoolSnapshot(t, a, mailbox, "duplicate@example.com", "sent")
	contact, ok, err := a.core.FindReplyAIDelivery(mailbox, "duplicate@example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("ambiguous sender matched contact=%d among %d,%d instead of ignoring", contact.PoolContactID, first, second)
	}
}

func TestAuditMailboxOptOutDuringClassification(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 2, "opt-out@example.com")
	campaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,'sent')`, campaign, customer); err != nil {
		t.Fatal(err)
	}
	auditClassifier(t, a, func() {
		if _, err := a.db.Exec(`UPDATE reply_mailboxes SET ai_enabled=FALSE WHERE id=$1`, mailbox); err != nil {
			panic(err)
		}
	})
	event := auditEvent(t, a, mailbox, "opt-out@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customer); err != nil {
		t.Fatal(err)
	}
	if status != "enabled" {
		t.Fatalf("AI switched off before classification returned, but customer became %s", status)
	}
}

func TestAuditArchivedOrganizationDuringPoolClassification(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	auditPoolSnapshot(t, a, mailbox, "archived@example.com", "sent")
	auditClassifier(t, a, func() {
		if _, err := a.db.Exec(`UPDATE organizations SET status='archived' WHERE id=101`); err != nil {
			panic(err)
		}
	})
	event := auditEvent(t, a, mailbox, "archived@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var exclusions int
	if err := a.db.Get(&exclusions, `SELECT COUNT(*) FROM org_pool_allocation_exclusions`); err != nil {
		t.Fatal(err)
	}
	if exclusions != 0 {
		t.Fatalf("organization archived before classification returned, but committed %d pool exclusion", exclusions)
	}
}

func TestAuditDisableEnablePreservesAI(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	c, _ := replyMailboxConfigContext(2, 101, mailbox, `{}`)
	if err := a.DisableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	c, recorder := replyMailboxConfigContext(2, 101, mailbox, `{}`)
	if err := a.EnableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data models.ReplyMailbox `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.AIEnabled {
		t.Fatalf("disable then enable silently turned AI off (status=%s, verified=%v)", response.Data.Status, response.Data.VerifiedAt != nil)
	}
}

func TestAuditPlatformCreatedOrganizationMailboxScan(t *testing.T) {
	a, _ := auditReplyApp(t)
	var mailbox int
	if err := a.db.Get(&mailbox, `INSERT INTO reply_mailboxes(user_id,organization_id,email,status,ai_enabled,verified_at,password)
		VALUES(1,101,'platform-created@example.com','active',TRUE,NOW(),'test-secret') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	var sources []replyAIMailboxSource
	if err := a.queries.GetReplyAIMailboxes.Select(&sources); err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.ID == mailbox {
			return
		}
	}
	t.Fatal("verified AI mailbox owned by platform admin in an organization without membership is omitted from scan")
}

func TestAuditPoolReplyArrivesAtDifferentMailbox(t *testing.T) {
	a, originalMailbox := auditReplyApp(t)
	auditPoolSnapshot(t, a, originalMailbox, "wrong-mailbox@example.com", "sent")
	var actualMailbox int
	if err := a.db.Get(&actualMailbox, `INSERT INTO reply_mailboxes(user_id,organization_id,email,status,ai_enabled,verified_at,password)
		VALUES(2,101,'other-reply@example.com','active',TRUE,NOW(),'test-secret') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	auditClassifier(t, a, nil)
	event := auditEvent(t, a, actualMailbox, "wrong-mailbox@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var exclusions int
	if err := a.db.Get(&exclusions, `SELECT COUNT(*) FROM org_pool_allocation_exclusions`); err != nil {
		t.Fatal(err)
	}
	if exclusions != 0 {
		t.Fatalf("sent route mailbox=%d, inbound mailbox=%d, but wrote %d exclusion", originalMailbox, actualMailbox, exclusions)
	}
}

func TestReplyAIDeliveryReferencesResolveCollision(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 2, "collision@example.com")
	privateCampaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,reply_to_snapshot) VALUES($1,$2,'sent','collision@example.com','reply@example.com')`, privateCampaign, customer); err != nil {
		t.Fatal(err)
	}
	contact, _ := auditPoolSnapshot(t, a, mailbox, "collision@example.com", "sent")
	var campaign int
	var uuid string
	if err := a.db.Get(&campaign, `SELECT campaign_id FROM campaign_pool_recipients WHERE pool_contact_id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&uuid, `SELECT uuid FROM campaigns WHERE id=$1`, campaign); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := a.core.FindReplyAIDelivery(mailbox, "collision@example.com", 0); err != nil || ok {
		t.Fatalf("ambiguous sender resolved: %v %v", ok, err)
	}
	reference := models.CampaignReplyMessageID(uuid, 0, contact)
	delivery, ok, err := a.core.FindReplyAIDelivery(mailbox, "collision@example.com", 0, reference)
	if err != nil || !ok || delivery.PoolContactID != contact || delivery.CustomerID != 0 {
		t.Fatalf("reply reference failed: %+v %v %v", delivery, ok, err)
	}
	event := auditEvent(t, a, mailbox, "collision@example.com")
	if _, err := a.db.Exec(`UPDATE reply_ai_events SET reply_references=$2 WHERE id=$1`, event.ID, reference); err != nil {
		t.Fatal(err)
	}
	event.ReplyReferences = reference
	auditClassifier(t, a, nil)
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customer); err != nil {
		t.Fatal(err)
	}
	if status != "enabled" {
		t.Fatal("referenced pool reply mutated private customer")
	}
	// A known reference to another target must not fall back to sender alone.
	if _, ok, err := a.core.FindReplyAIDelivery(mailbox, "collision@example.com", 0, models.CampaignReplyMessageID(uuid, 0, contact+999)); err != nil || ok {
		t.Fatalf("foreign recipient reference resolved: %v %v", ok, err)
	}
}

func TestReplyMailboxPrivateSendGuardAndHistory(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 2, "private@example.com")
	campaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,reply_to_snapshot) VALUES($1,$2,'sent','private@example.com','reply@example.com')`, campaign, customer); err != nil {
		t.Fatal(err)
	}
	store := &store{db: a.db}
	if err := store.ValidateCampaignReplyRoute(campaign, 0, "reply@example.com"); err != nil {
		t.Fatal(err)
	}
	c, _ := replyMailboxConfigContext(2, 101, mailbox, `{}`)
	if err := a.DisableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateCampaignReplyRoute(campaign, 0, "reply@example.com"); err == nil {
		t.Fatal("disabled mailbox accepted at send")
	}
	if err := a.validateCampaignReplyMailbox(models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 101}, UserID: 2}, &models.Campaign{ReplyMailboxID: null.IntFrom(mailbox)}); err == nil {
		t.Fatal("disabled mailbox accepted at launch")
	}
	var route string
	if err := a.db.Get(&route, `SELECT reply_to_snapshot FROM campaign_recipients WHERE campaign_id=$1 AND customer_id=$2`, campaign, customer); err != nil {
		t.Fatal(err)
	}
	if route != "reply@example.com" {
		t.Fatal("send history was erased on disable")
	}
}

func TestReplyAIGlobalOptOutDuringClassification(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	customer := auditCustomer(t, a, 2, "global-opt-out@example.com")
	campaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,'sent')`, campaign, customer); err != nil {
		t.Fatal(err)
	}
	auditClassifier(t, a, func() {
		if _, err := a.db.Exec(`UPDATE settings SET value=jsonb_set(value,'{enabled}','false') WHERE key='reply_ai'`); err != nil {
			panic(err)
		}
	})
	event := auditEvent(t, a, mailbox, "global-opt-out@example.com")
	if err := a.processReplyAIEvent(event); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customer); err != nil {
		t.Fatal(err)
	}
	if status != "enabled" {
		t.Fatal("global AI opt-out ignored during classification")
	}
}

func TestReplyMailboxSchedulerRejectsDisabledRoute(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	campaign := auditCampaign(t, a, 2, mailbox)
	if _, err := a.db.Exec(`UPDATE campaigns SET status='scheduled',send_at=NOW()-INTERVAL '1 minute' WHERE id=$1;`, campaign); err != nil {
		t.Fatal(err)
	}
	c, _ := replyMailboxConfigContext(2, 101, mailbox, `{}`)
	if err := a.DisableReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	s := &store{db: a.db, queries: a.queries, core: a.core}
	ready, err := s.NextCampaigns([]int64{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 {
		t.Fatal("disabled mailbox campaign became runnable")
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, campaign); err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("disabled scheduled campaign status=%s want draft", status)
	}
}

func TestReplyAIImportedAddressConfiguredAfterDelivery(t *testing.T) {
	a, mailbox := auditReplyApp(t)
	contact, _ := auditPoolSnapshot(t, a, mailbox, "imported-customer@example.com", "sent")
	// The imported route was external at send time; registering a verified
	// receiving connection later must resolve by the immutable address/org,
	// without requiring a mailbox ID that did not exist on the sent snapshot.
	if _, err := a.db.Exec(`UPDATE campaign_pool_recipients SET reply_mailbox_id=NULL WHERE pool_contact_id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	delivery, ok, err := a.core.FindReplyAIDelivery(mailbox, "imported-customer@example.com", 0)
	if err != nil || !ok || delivery.PoolContactID != contact {
		t.Fatalf("imported address lost delivery source: %+v %v %v", delivery, ok, err)
	}
	auditClassifier(t, a, nil)
	if err := a.processReplyAIEvent(auditEvent(t, a, mailbox, "imported-customer@example.com")); err != nil {
		t.Fatal(err)
	}
	var excluded int
	if err := a.db.Get(&excluded, `SELECT COUNT(*) FROM org_pool_allocation_exclusions WHERE contact_id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	if excluded != 1 {
		t.Fatal("registered imported route did not process its actual reply")
	}
}
