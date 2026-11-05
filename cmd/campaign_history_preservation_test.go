package main

import (
	"testing"

	"github.com/knadh/listmonk/models"
)

func campaignHistoryAccess() models.WorkspaceAccess {
	return models.WorkspaceAccess{
		UserID:    3,
		Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember},
	}
}

func TestCampaignHistorySurvivesCustomerDeletion(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,
		owner_user_id,organization_id,visibility,planned_total,to_send,sent,send_errors)
		VALUES(gen_random_uuid(),'History campaign','Subject','sender@example.test','Body','email','finished',
		3,101,'organization',3,3,1,1) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	statuses := []string{"sent", "pending", "deferred"}
	ids := make([]int, 0, len(statuses))
	for i, status := range statuses {
		var id int
		if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,owner_user_id,organization_id)
			VALUES(gen_random_uuid(),$1,$2,3,101) RETURNING id`,
			"history"+string(rune('a'+i))+"@example.test", "History "+status); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot,sent_at)
			VALUES($1,$2,$3,$4,CASE WHEN $5::BOOL THEN '2026-11-04T12:00:00Z'::TIMESTAMPTZ END)`,
			campaignID, id, status, "history"+status+"@example.test", status == "sent"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_send_errors(campaign_id,campaign_owner_user_id,campaign_organization_id,
		recipient_type,recipient_id,recipient_organization_id,customer_code,name,email,stage,category,smtp_code,error)
		VALUES($1,3,101,'private',$2,101,'C-1','History failed','historyfailed@example.test','send','smtp_rejected',550,'550 rejected')`,
		campaignID, ids[2]); err != nil {
		t.Fatal(err)
	}

	// Deleting the pending and failed customers must not remove the snapshot.
	for _, id := range []int{ids[1], ids[2]} {
		if _, err := a.db.Exec(`DELETE FROM customers WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}

	var counts struct {
		Total       int  `db:"total_count"`
		Sent        int  `db:"sent_count"`
		Unsent      int  `db:"unsent_count"`
		HasSnapshot bool `db:"has_snapshot"`
	}
	if err := a.db.Get(&counts, `SELECT total_count,sent_count,unsent_count,has_snapshot FROM campaign_send_counts WHERE campaign_id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	if counts.Total != 3 || counts.Sent != 1 || counts.Unsent != 2 || !counts.HasSnapshot {
		t.Fatalf("send counts=(%d,%d,%d,%t), want (3,1,2,true)", counts.Total, counts.Sent, counts.Unsent, counts.HasSnapshot)
	}

	access := campaignHistoryAccess()
	overview, err := a.core.GetCampaignDeliveryOverview(access, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Planned != 3 || overview.Sent != 1 || overview.Unsent != 2 || overview.Failed != 1 || overview.Waiting != 1 {
		t.Fatalf("overview=%+v, want planned=3 sent=1 unsent=2 failed=1 waiting=1", overview)
	}

	camp, err := a.core.GetWorkspaceCampaign(access, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if camp.DeliveryTotal != 3 || camp.DeliverySent != 1 || camp.DeliveryPending != 2 || camp.DeliveryUnavailable != 0 {
		t.Fatalf("delivery counts=(%d,%d,%d,%d), want (3,1,2,0)",
			camp.DeliveryTotal, camp.DeliverySent, camp.DeliveryPending, camp.DeliveryUnavailable)
	}

	summary, err := a.core.GetCampaignReportSummary(campaignID, "2026-11-04T00:00:00Z", "2026-11-04T23:59:59Z", true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Sent != 1 {
		t.Fatalf("report sent=%d, want 1", summary.Sent)
	}
}

func TestCampaignOverviewUsesFailureHistoryWithoutSnapshot(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,
		owner_user_id,organization_id,visibility,planned_total,to_send,sent,send_errors)
		VALUES(gen_random_uuid(),'Deleted snapshot','Subject','sender@example.test','Body','email','finished',
		3,101,'organization',3,0,1,2) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.db.Exec(`INSERT INTO campaign_send_errors(campaign_id,campaign_owner_user_id,campaign_organization_id,
			recipient_type,recipient_id,recipient_organization_id,customer_code,name,email,stage,category,smtp_code,error)
			VALUES($1,3,101,'private',$2,101,$3,'Historical','historical@example.test','send','smtp_rejected',550,'550 rejected')`,
			campaignID, 9000+i, "C-"+string(rune('1'+i))); err != nil {
			t.Fatal(err)
		}
	}

	overview, err := a.core.GetCampaignDeliveryOverview(campaignHistoryAccess(), campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Planned != 3 || overview.Sent != 1 || overview.Unsent != 2 || overview.Failed != 2 || overview.Waiting != 0 {
		t.Fatalf("overview=%+v, want planned=3 sent=1 unsent=2 failed=2 waiting=0", overview)
	}
}

func TestCampaignReportSummaryFallsBackToDailyUsage(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,
		owner_user_id,organization_id,visibility,planned_total,to_send,sent)
		VALUES(gen_random_uuid(),'Daily usage','Subject','sender@example.test','Body','email','finished',
		3,101,'organization',5,0,5) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_daily_usage(campaign_id,usage_date,sent_count)
		VALUES($1,'2026-11-04',5)`, campaignID); err != nil {
		t.Fatal(err)
	}

	summary, err := a.core.GetCampaignReportSummary(campaignID, "2026-11-04T00:00:00Z", "2026-11-04T23:59:59Z", true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Sent != 5 {
		t.Fatalf("report sent=%d, want 5", summary.Sent)
	}
	combined, err := a.core.GetCampaignsReportSummary([]int{campaignID}, "2026-11-04T00:00:00Z", "2026-11-04T23:59:59Z", true)
	if err != nil {
		t.Fatal(err)
	}
	if combined.Sent != 5 {
		t.Fatalf("combined sent=%d, want 5", combined.Sent)
	}
}
