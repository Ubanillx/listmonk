package main

import (
	"fmt"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/models"
)

func TestCampaignRecipientRetryLimitSurvivesResumeAndRestart(t *testing.T) {
	for _, limit := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { testCampaignRecipientRetryLimit(t, limit) })
	}
}

func TestCampaignPoolRecipientRetryLimit(t *testing.T) {
	for _, scope := range []string{models.CampaignPoolScopeOrganization, models.CampaignPoolScopeAllOrganizations} {
		t.Run(scope, func(t *testing.T) {
			a, _, campaignID, _ := newSendErrorReportFixture(t)
			q := prepareQueries(readTestQueries(t), a.db, koanf.New("."))
			s := newManagerStore(q, a.core, nil, a.db)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := a.db.Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			id := func(query string, args ...any) int64 {
				t.Helper()
				var out int64
				if err := a.db.Get(&out, query, args...); err != nil {
					t.Fatal(err)
				}
				return out
			}
			exec(`DELETE FROM campaign_send_errors WHERE campaign_id=$1`, campaignID)
			exec(`INSERT INTO settings(key,value) VALUES('smtp_delivery','{"max_recipient_retries":1}') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`)
			exec(`UPDATE campaigns SET pool_scope=$2,send_errors=0 WHERE id=$1`, campaignID, scope)
			exec(`UPDATE users SET status='enabled' WHERE id=3`)
			poolID := seedOrgPoolAllocationPool(t, a.db, "Limited retries pool")
			listID := id(`INSERT INTO customer_lists(uuid,name,type,organization_id,owner_user_id) VALUES(gen_random_uuid(),'Limited allocation','org_pool_allocation',101,3) RETURNING id`)
			allocationID := id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,101) RETURNING id`, listID, poolID)
			contactID := id(`INSERT INTO pool_contacts(email,name,reply_to) VALUES('bounded@example.test','Bounded pool','reply@example.test') RETURNING id`)
			exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, poolID, contactID)
			exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, allocationID, contactID)
			exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,101)`, poolID)
			exec(`INSERT INTO campaign_customer_lists(campaign_id,pool_id) VALUES($1,$2)`, campaignID, poolID)
			exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,from_email,auth_protocol,tls_type)
				VALUES(gen_random_uuid(),3,'127.0.0.1',1,'sender@example.test','none','none')`)
			exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,email_snapshot,name_snapshot,reply_to_snapshot)
				VALUES($1,$2,$3,$4,101,'bounded@example.test','Bounded pool','reply@example.test')`, campaignID, contactID, poolID, allocationID)
			campaign, err := s.GetCampaign(campaignID)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				rows, err := s.NextCustomers(campaignID, 1)
				if err != nil || len(rows) != 1 || rows[0].PoolContactID != contactID {
					t.Fatalf("pool attempt: rows=%+v err=%v", rows, err)
				}
				failure := models.CampaignSendFailure{CampaignID: campaignID, CampaignOwnerUserID: campaign.OwnerUserID,
					CampaignOrganizationID: campaign.OrganizationID, RecipientType: "pool", RecipientID: contactID, RecipientOrganizationID: 101, Stage: "send", Category: "network", Error: "connection refused"}
				if err := s.RecordCampaignSendFailure(failure); err != nil {
					t.Fatal(err)
				}
				if err := s.MarkPoolCampaignRecipientStatus(campaignID, contactID, models.CampaignRecipientStatusPending); err != nil {
					t.Fatal(err)
				}
				s = newManagerStore(q, a.core, nil, a.db)
			}
			// Audience refresh, ordinary resumption and the organization allocator
			// must all preserve the terminal state.
			if err := a.core.EnsurePoolCampaignRecipients(campaignID); err != nil {
				t.Fatal(err)
			}
			rows, err := s.NextCustomers(campaignID, 1)
			if err != nil || len(rows) != 0 {
				t.Fatalf("pool recipient retried past its limit: %+v %v", rows, err)
			}
			access := models.WorkspaceAccess{UserID: 3, Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}}
			report, err := a.core.GetCampaignFailedRecipients(access, campaignID, models.CampaignSendErrorFilters{IncludePool: true}, 0, 20)
			if err != nil || report.Total != 1 || report.Results[0].Retryable || report.Results[0].Status != "failed" {
				t.Fatalf("exhausted pool recipient report: %+v %v", report, err)
			}
			overview, err := a.core.GetCampaignDeliveryOverview(access, campaignID)
			if err != nil || overview.Failed != 1 || overview.Unsent != 0 || len(overview.Organizations) != 1 || overview.Organizations[0].Failed != 1 {
				t.Fatalf("pool failure overview: %+v %v", overview, err)
			}
		})
	}
}

func testCampaignRecipientRetryLimit(t *testing.T, limit int) {
	t.Helper()
	a, _, id, ids := newSendErrorReportFixture(t)
	q := prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	s := newManagerStore(q, a.core, nil, a.db)
	if _, err := a.db.Exec(`WITH cleared AS (DELETE FROM campaign_send_errors WHERE campaign_id=$1)
		UPDATE campaigns SET send_errors=0 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO settings(key,value) VALUES('smtp_delivery',jsonb_build_object('max_recipient_retries',$1::INT))
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, limit); err != nil {
		t.Fatal(err)
	}
	var listID int
	if err := a.db.Get(&listID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
		VALUES(gen_random_uuid(),'Bounded retry audience','private',3,101) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id) VALUES($1,$2)`, id, listID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($1,$3,'confirmed'),($2,$3,'confirmed')`, ids[0], ids[1], listID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,email_snapshot)
		VALUES($1,$2,'pending','retry@example.test'),($1,$3,'cancelled','healthy@example.test')`, id, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),3,'127.0.0.1',1,'none','none')`); err != nil {
		t.Fatal(err)
	}
	campaign, err := s.GetCampaign(id)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range limit + 1 {
		rows, err := s.NextCustomers(id, 1)
		if err != nil || len(rows) != 1 || rows[0].ID != ids[0] {
			t.Fatalf("attempt %d: rows=%+v err=%v", attempt+1, rows, err)
		}
		failure := models.CampaignSendFailure{CampaignID: id, RecipientType: "private", RecipientID: int64(ids[0]),
			CampaignOwnerUserID: campaign.OwnerUserID, CampaignOrganizationID: campaign.OrganizationID,
			Stage: "send", Category: "network", Error: "connection refused"}
		if err := s.RecordCampaignSendFailure(failure); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkCampaignRecipientStatus(id, ids[0], models.CampaignRecipientStatusPending); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateCampaignStatus(id, models.CampaignStatusPaused); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateCampaignStatus(id, models.CampaignStatusRunning); err != nil {
			t.Fatal(err)
		}
		s = newManagerStore(q, a.core, nil, a.db)
	}
	// Re-enable another audience recipient only after testing the isolated
	// lifetime ceiling. Retry-order fairness has a separate multi-recipient test.
	if _, err := a.db.Exec(`UPDATE campaign_recipients SET status='pending' WHERE campaign_id=$1 AND customer_id=$2`, id, ids[1]); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{UserID: 3, Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}}
	filters := models.CampaignSendErrorFilters{IncludePrivate: true, SensitivePrivate: true, PrivateDiagnostics: true}
	report, err := a.core.GetCampaignFailedRecipients(access, id, filters, 0, 20)
	if err != nil || report.Total != 1 || report.Results[0].Retryable || report.Results[0].Status != "failed" {
		t.Fatalf("exhausted recipient disappeared or remained retryable: %+v %v", report, err)
	}
	overview, err := a.core.GetCampaignDeliveryOverview(access, id)
	if err != nil || overview.Failed != 1 || overview.Waiting != 1 || overview.Unsent != 1 {
		t.Fatalf("exhausted recipient skewed pending/failed counts: %+v %v", overview, err)
	}
	if err := s.UpdateCampaignStatus(id, models.CampaignStatusPaused); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.RetryCampaignFailedRecipients(access, id, filters, []models.CampaignRetryRecipient{{Type: "private", ID: int64(ids[0])}}); err == nil {
		t.Fatal("targeted retry bypassed the lifetime limit")
	}
	if err := s.UpdateCampaignStatus(id, models.CampaignStatusRunning); err != nil {
		t.Fatal(err)
	}
	if limit > 0 {
		// Hidden historical ownership must not let an exhausted row cancel out
		// another recipient's visible, still-retryable failure in the overview.
		if _, err := a.db.Exec(`UPDATE campaign_send_errors SET campaign_owner_user_id=NULL WHERE campaign_id=$1 AND recipient_id=$2`, id, ids[0]); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordCampaignSendFailure(models.CampaignSendFailure{CampaignID: id, RecipientType: "private", RecipientID: int64(ids[1]),
			CampaignOwnerUserID: campaign.OwnerUserID, CampaignOrganizationID: campaign.OrganizationID, Stage: "send", Category: "network", Error: "temporary failure"}); err != nil {
			t.Fatal(err)
		}
		overview, err = a.core.GetCampaignDeliveryOverview(access, id)
		if err != nil || overview.Failed != 1 || overview.Waiting != 0 || overview.Unsent != 1 {
			t.Fatalf("hidden history skewed waiting counts: %+v %v", overview, err)
		}
	}
	rows, err := s.NextCustomers(id, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != ids[1] {
		t.Fatalf("recipient retried after its lifetime limit or blocked the healthy recipient: rows=%+v err=%v", rows, err)
	}
	if err := s.MarkCampaignMessageSent(id, ids[1]); err != nil {
		t.Fatal(err)
	}
	rows, err = s.NextCustomers(id, 1)
	if err != nil || len(rows) != 0 {
		t.Fatalf("exhausted recipient was revived: %+v %v", rows, err)
	}
	var daily int
	if err := a.db.Get(&daily, `SELECT sent_count FROM campaign_daily_usage WHERE campaign_id=$1 AND usage_date=$2::DATE`, id, currentLocalDate()); err != nil || daily != 1 {
		t.Fatalf("retries consumed daily success quota: daily=%d err=%v", daily, err)
	}
}
