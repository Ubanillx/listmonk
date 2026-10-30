package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/knadh/listmonk/models"
)

func TestCampaignRetryOrderSurvivesPauseAndResume(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if _, err := f.a.db.Exec(`WITH reset AS (UPDATE campaigns SET sent=0 WHERE id=$1)
		UPDATE campaign_recipients SET status='pending',sent_at=NULL WHERE campaign_id=$1`, f.id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),3,'127.0.0.1',1,'none','none')`); err != nil {
		t.Fatal(err)
	}
	s := newManagerStore(f.a.queries, f.a.core, nil, f.a.db)
	campaign, err := s.GetCampaign(f.id)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.NextCustomers(f.id, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != f.customers[0].ID {
		t.Fatalf("initial recipient: %+v %v", rows, err)
	}
	if err := s.RecordCampaignSendFailure(models.CampaignSendFailure{CampaignID: f.id, RecipientType: "private",
		RecipientID: int64(f.customers[0].ID), CampaignOwnerUserID: campaign.OwnerUserID, CampaignOrganizationID: campaign.OrganizationID,
		Stage: "send", Category: "network", Error: "temporary failure"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCampaignRecipientStatus(f.id, f.customers[0].ID, models.CampaignRecipientStatusPending); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{models.CampaignStatusPaused, models.CampaignStatusRunning} {
		if err := s.UpdateCampaignStatus(f.id, status); err != nil {
			t.Fatal(err)
		}
	}
	s = newManagerStore(f.a.queries, f.a.core, nil, f.a.db)
	rows, err = s.NextCustomers(f.id, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != f.customers[1].ID {
		t.Fatalf("failed recipient blocked the untouched recipient after resume: %+v %v", rows, err)
	}
	if rows, err := s.NextCustomers(f.id, 1); !errors.Is(err, manager.ErrCampaignBatchPending) || len(rows) != 0 {
		t.Fatalf("retry overtook the in-flight first pass: %+v %v", rows, err)
	}
	if err := s.MarkCampaignMessageSent(f.id, f.customers[1].ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.NextCustomers(f.id, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != f.customers[0].ID {
		t.Fatalf("retry was not available after the first pass: %+v %v", rows, err)
	}
}

func TestCampaignRetryOrderAcrossRecipientSources(t *testing.T) {
	for _, scope := range []string{models.CampaignPoolScopeOrganization, models.CampaignPoolScopeAllOrganizations} {
		t.Run(scope, func(t *testing.T) {
			f := newUnsubscribeFixture(t)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.a.db.Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			id := func(query string, args ...any) int64 {
				t.Helper()
				var result int64
				if err := f.a.db.Get(&result, query, args...); err != nil {
					t.Fatal(err)
				}
				return result
			}
			exec(`UPDATE campaigns SET pool_scope=$2,sent=0 WHERE id=$1`, f.id, scope)
			privateStatus := "pending"
			if scope == models.CampaignPoolScopeAllOrganizations {
				privateStatus = "cancelled"
			}
			exec(`UPDATE campaign_recipients SET status=$2::campaign_recipient_status,sent_at=NULL WHERE campaign_id=$1`, f.id, privateStatus)
			exec(`UPDATE users SET status='enabled' WHERE id IN (3,4)`)
			exec(`INSERT INTO organization_members(organization_id,user_id,role) VALUES(102,4,'member')`)
			exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,from_email,auth_protocol,tls_type)
				VALUES(gen_random_uuid(),3,'127.0.0.1',1,'sender3@example.test','none','none'),
				(gen_random_uuid(),4,'127.0.0.1',1,'sender4@example.test','none','none')`)
			pool := seedOrgPoolAllocationPool(t, f.a.db, "Retry ordering pool")
			exec(`INSERT INTO campaign_customer_lists(campaign_id,pool_id) VALUES($1,$2)`, f.id, pool)
			allocations := map[int]int64{}
			for i := range 2 {
				org, owner := 101, 3
				if scope == models.CampaignPoolScopeAllOrganizations && i == 1 {
					org, owner = 102, 4
				}
				allocation, ok := allocations[org]
				if !ok {
					list := id(`INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
						VALUES(gen_random_uuid(),$1,'org_pool_allocation',$2,$3) RETURNING id`, fmt.Sprintf("Retry allocation %d", i), owner, org)
					allocation = id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,$3) RETURNING id`, list, pool, org)
					allocations[org] = allocation
				}
				contact := id(`INSERT INTO pool_contacts(email,name,reply_to) VALUES($1,'Retry contact','reply@example.test') RETURNING id`, fmt.Sprintf("pool-retry-%d@example.test", i))
				exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, contact)
				exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, allocation, contact)
				exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, pool, org)
				exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,email_snapshot,reply_to_snapshot)
					VALUES($1,$2,$3,$4,$5,$6,'reply@example.test')`, f.id, contact, pool, allocation, org, fmt.Sprintf("pool-retry-%d@example.test", i))
			}
			s := newManagerStore(f.a.queries, f.a.core, nil, f.a.db)
			campaign, err := s.GetCampaign(f.id)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.NextCustomers(f.id, 1)
			if err != nil || len(rows) != 1 {
				t.Fatalf("initial claim: %+v %v", rows, err)
			}
			failed := rows[0]
			fail := func(row models.CampaignCustomer) {
				t.Helper()
				kind, recipient := "private", int64(row.ID)
				if row.PoolContactID > 0 {
					kind, recipient = "pool", row.PoolContactID
				}
				if err := s.RecordCampaignSendFailure(models.CampaignSendFailure{CampaignID: f.id, RecipientType: kind, RecipientID: recipient,
					CampaignOwnerUserID: campaign.OwnerUserID, CampaignOrganizationID: campaign.OrganizationID,
					Stage: "send", Category: "network", Error: "temporary failure"}); err != nil {
					t.Fatal(err)
				}
				if kind == "pool" {
					err = s.MarkPoolCampaignRecipientStatus(f.id, recipient, models.CampaignRecipientStatusPending)
				} else {
					err = s.MarkCampaignRecipientStatus(f.id, int(recipient), models.CampaignRecipientStatusPending)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			fail(failed)
			remaining := 3
			if scope == models.CampaignPoolScopeAllOrganizations {
				remaining = 1
			}
			for range remaining {
				rows, err = s.NextCustomers(f.id, 1)
				if err != nil || len(rows) != 1 || (rows[0].ID == failed.ID && rows[0].PoolContactID == failed.PoolContactID) {
					t.Fatalf("retry overtook another source/organization: %+v %v", rows, err)
				}
				if rows[0].PoolContactID > 0 {
					err = s.MarkPoolCampaignMessageSent(f.id, rows[0].PoolContactID)
				} else {
					err = s.MarkCampaignMessageSent(f.id, rows[0].ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			rows, err = s.NextCustomers(f.id, 1)
			if err != nil || len(rows) != 1 || rows[0].ID != failed.ID || rows[0].PoolContactID != failed.PoolContactID {
				t.Fatalf("retry lost after first pass: %+v %v", rows, err)
			}
		})
	}
}

func TestCampaignRetryQueueUpgradeMatchesFreshInstall(t *testing.T) {
	f := newUnsubscribeFixture(t)
	var fresh, upgraded string
	if err := f.a.db.Get(&fresh, `SELECT pg_get_viewdef('campaign_retry_queue'::regclass)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`DROP VIEW campaign_retry_queue`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrations.V6_67_0(f.a.db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.a.db.Get(&upgraded, `SELECT pg_get_viewdef('campaign_retry_queue'::regclass)`); err != nil || fresh != upgraded {
		t.Fatalf("upgrade and fresh install have different retry queues: %v", err)
	}
}

func TestCampaignRetryRoundsDoNotStarveOtherFailures(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if _, err := f.a.db.Exec(`WITH reset AS (UPDATE campaigns SET sent=0 WHERE id=$1)
		UPDATE campaign_recipients SET status='pending',sent_at=NULL WHERE campaign_id=$1`, f.id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),3,'127.0.0.1',1,'none','none')`); err != nil {
		t.Fatal(err)
	}
	s := newManagerStore(f.a.queries, f.a.core, nil, f.a.db)
	campaign, err := s.GetCampaign(f.id)
	if err != nil {
		t.Fatal(err)
	}
	// Both recipients fail once; after the first fails again, the second must
	// receive its first retry before the first can have a second retry.
	for _, index := range []int{0, 1, 0, 1} {
		rows, err := s.NextCustomers(f.id, 1)
		if err != nil || len(rows) != 1 || rows[0].ID != f.customers[index].ID {
			t.Fatalf("retry round starved recipient %d: %+v %v", index, rows, err)
		}
		if err := s.RecordCampaignSendFailure(models.CampaignSendFailure{CampaignID: f.id, RecipientType: "private",
			RecipientID: int64(rows[0].ID), CampaignOwnerUserID: campaign.OwnerUserID, CampaignOrganizationID: campaign.OrganizationID,
			Stage: "send", Category: "network", Error: "temporary failure"}); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkCampaignRecipientStatus(f.id, rows[0].ID, models.CampaignRecipientStatusPending); err != nil {
			t.Fatal(err)
		}
	}
	// A no-longer-subscribed, untouched recipient must not hold the queue back.
	if _, err := f.a.db.Exec(`WITH cleared AS (DELETE FROM campaign_send_errors WHERE campaign_id=$1 AND recipient_id=$2)
		UPDATE customer_list_memberships SET status='unsubscribed' WHERE customer_id=$2`, f.id, f.customers[1].ID); err != nil {
		t.Fatal(err)
	}
	rows, err := s.NextCustomers(f.id, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != f.customers[0].ID {
		t.Fatalf("unsubscribed recipient blocked an eligible retry: %+v %v", rows, err)
	}
}

func TestCampaignRetryWaitingAppliesOnlyToRegularSMTP(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if _, err := f.a.db.Exec(`WITH reset AS (UPDATE campaigns SET sent=0 WHERE id=$1)
		UPDATE campaign_recipients SET status='queued',sent_at=NULL WHERE campaign_id=$1`, f.id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),3,'127.0.0.1',1,'none','none')`); err != nil {
		t.Fatal(err)
	}
	s := newManagerStore(f.a.queries, f.a.core, nil, f.a.db)
	for _, tc := range []struct {
		kind, messenger string
		wait            bool
	}{{"regular", "email", true}, {"optin", "email", false}, {"regular", "postback", false}} {
		if _, err := f.a.db.Exec(`UPDATE campaigns SET type=$2,messenger=$3 WHERE id=$1`, f.id, tc.kind, tc.messenger); err != nil {
			t.Fatal(err)
		}
		rows, err := s.NextCustomers(f.id, 1)
		if len(rows) != 0 || (tc.wait && !errors.Is(err, manager.ErrCampaignBatchPending)) || (!tc.wait && err != nil) {
			t.Fatalf("unexpected retry wait for %s/%s: %+v %v", tc.kind, tc.messenger, rows, err)
		}
	}
}
