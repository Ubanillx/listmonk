package main

import (
	"testing"

	"github.com/knadh/listmonk/internal/migrations"
	"github.com/knadh/listmonk/models"
)

func TestCampaignDeliveryCountsUseRecipientStates(t *testing.T) {
	a, _, campaignID, ids := newSendErrorReportFixture(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Stale persisted counters and many failure attempts cannot define people.
	exec(`UPDATE campaigns SET sent=90,to_send=999,send_errors=2821 WHERE id=$1`, campaignID)
	states := []string{"sent", "pending", "queued", "deferred", "failed", "cancelled"}
	for i, state := range states {
		exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,$3)`, campaignID, ids[i], state)
	}
	var poolID int
	if err := a.db.Get(&poolID, `INSERT INTO customer_lists(uuid,name,type,organization_id) VALUES(gen_random_uuid(),'Delivery snapshot','pool',101) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"sent", "failed", "cancelled", "deferred"} {
		var contactID int
		if err := a.db.Get(&contactID, `INSERT INTO pool_contacts(email,name) VALUES($1,'Delivery count') RETURNING id`, "counts"+state+"@example.test"); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,organization_id,email_snapshot,status) VALUES($1,$2,$3,$4,$5,$6)`, campaignID, contactID, poolID, 101, "counts"+state+"@example.test", state)
	}
	access := models.WorkspaceAccess{UserID: 3, Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}}
	check := func(wantTotal, wantSent, wantPending, wantUnavailable int) {
		t.Helper()
		camp, err := a.core.GetWorkspaceCampaign(access, campaignID)
		if err != nil {
			t.Fatal(err)
		}
		assert := func(total, sent, pending, unavailable int) {
			t.Helper()
			if total != wantTotal || sent != wantSent || pending != wantPending || unavailable != wantUnavailable || total != sent+pending+unavailable {
				t.Fatalf("delivery counts=(%d,%d,%d,%d), want (%d,%d,%d,%d)", total, sent, pending, unavailable, wantTotal, wantSent, wantPending, wantUnavailable)
			}
		}
		assert(camp.DeliveryTotal, camp.DeliverySent, camp.DeliveryPending, camp.DeliveryUnavailable)
		camps, _, err := a.core.QueryWorkspaceCampaigns(access, "", nil, nil, "created_at", "desc", 0, 20)
		if err != nil || len(camps) != 1 {
			t.Fatalf("list: %+v %v", camps, err)
		}
		assert(camps[0].DeliveryTotal, camps[0].DeliverySent, camps[0].DeliveryPending, camps[0].DeliveryUnavailable)
		stats, err := a.core.GetRunningCampaignStats()
		if err != nil || len(stats) != 1 {
			t.Fatalf("live: %+v %v", stats, err)
		}
		assert(stats[0].DeliveryTotal, stats[0].DeliverySent, stats[0].DeliveryPending, stats[0].DeliveryUnavailable)
		if camp.SendErrors != 2821 || stats[0].SendErrors != 2821 {
			t.Fatal("history was rewritten")
		}
	}
	check(10, 2, 4, 4)
	// Upgrade/new install parity and repeated migration; no state rewrites.
	exec(`DROP VIEW campaign_delivery_counts`)
	for range 2 {
		if err := migrations.V6_69_0(a.db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		check(10, 2, 4, 4)
	}
	// Success after a transient error moves one person from pending to sent.
	exec(`UPDATE campaign_recipients SET status='sent' WHERE campaign_id=$1 AND customer_id=$2`, campaignID, ids[1])
	check(10, 3, 3, 4)
	// Preserve owner/workspace validity and single-organization pool scope.
	exec(`UPDATE customers SET owner_user_id=4 WHERE id=$1`, ids[2])
	exec(`UPDATE campaign_pool_recipients SET organization_id=102 WHERE campaign_id=$1 AND status='deferred'`, campaignID)
	check(8, 3, 1, 4)
	exec(`UPDATE campaigns SET pool_scope='all_organizations' WHERE id=$1`, campaignID)
	check(9, 3, 2, 4)
	// No snapshots: legacy and draft counts use saved values, no fabricated failures.
	exec(`DELETE FROM campaign_recipients WHERE campaign_id=$1`, campaignID)
	exec(`DELETE FROM campaign_pool_recipients WHERE campaign_id=$1`, campaignID)
	check(999, 90, 909, 0)
	exec(`UPDATE campaigns SET sent=0,to_send=0 WHERE id=$1`, campaignID)
	check(0, 0, 0, 0)
}
