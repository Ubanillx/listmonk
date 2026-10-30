package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/models"
)

// Hold the first delivery until the scheduler checks a full reservation. The
// injected failure takes the real worker's ordinary delivery-error path, which
// returns that recipient to pending without recording a successful send.
type dailyLimitRetryStore struct {
	*store
	checked chan struct{}
	once    sync.Once
	attempt atomic.Int32
}

func (s *dailyLimitRetryStore) NextCustomers(id, limit int) ([]models.CampaignCustomer, error) {
	rows, err := s.store.NextCustomers(id, limit)
	if len(rows) == 0 {
		s.once.Do(func() { close(s.checked) })
	}
	return rows, err
}

func (s *dailyLimitRetryStore) ValidateCampaignReplyRoute(int, int64, string) error {
	if s.attempt.Add(1) == 1 {
		select {
		case <-s.checked:
			return errors.New("temporary delivery failure")
		case <-time.After(3 * time.Second):
			return errors.New("scheduler did not recheck the reserved daily allowance")
		}
	}
	return nil
}

func TestCampaignDailyLimitCountsSuccessfulDeliveriesAfterRetry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		maxErrors  int
		recipients int
		wantStatus string
	}{
		{"retry", 5, 3, models.CampaignStatusDeferred},
		{"pause and resume", 1, 3, models.CampaignStatusDeferred},
		{"finish at exact daily limit", 5, 2, models.CampaignStatusFinished},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testCampaignDailyLimitRetry(t, tc.maxErrors, tc.recipients, tc.wantStatus)
		})
	}
}

func testCampaignDailyLimitRetry(t *testing.T, maxErrors, recipients int, wantStatus string) {
	t.Helper()
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	resumeMediaNotifsOnce.Do(func() { notifs.Initialize(notifs.Opt{}, nil, nil, a.log) })
	var campaignID, listID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,
		messenger,status,owner_user_id,daily_send_limit,smtp_rate_limit) VALUES(gen_random_uuid(),'Daily retry','Subject',
		'sender@example.test','Body','plain','email','running',1,2,1000000) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&listID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id)
		VALUES(gen_random_uuid(),'Daily retry audience','private',1) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id) VALUES($1,$2)`, campaignID, listID); err != nil {
		t.Fatal(err)
	}
	for i := range recipients {
		var id int
		if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,owner_user_id)
			VALUES(gen_random_uuid(),$1,'Recipient',1) RETURNING id`, fmt.Sprintf("daily%d@example.test", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status)
			VALUES($1,$2,'confirmed')`, id, listID); err != nil {
			t.Fatal(err)
		}
	}
	server, _ := resumeMediaSMTP(t, nil)
	if _, err := a.db.Exec(`INSERT INTO user_smtp_servers(uuid,user_id,host,port,auth_protocol,tls_type)
		VALUES(gen_random_uuid(),1,$1,$2,'none','none')`, server.Host, server.Port); err != nil {
		t.Fatal(err)
	}
	s := &dailyLimitRetryStore{store: newManagerStore(a.queries, a.core, nil, a.db), checked: make(chan struct{})}
	settled := make(chan string, 2)
	m := manager.New(manager.Config{
		RootURL: "https://example.test", BatchSize: 2, Concurrency: 1, MessageRate: 100,
		ScanCampaigns: true, ScanInterval: 10 * time.Millisecond, DisableTracking: true, MaxSendErrors: maxErrors,
		PersonalSMTP: func(int) (*email.Emailer, error) { return email.New(email.MessengerName, server) },
		AuditCampaign: func(action string, _ *models.Campaign, _ map[string]any) {
			if action == "campaign.deferred" || action == "campaign.paused" || action == "campaign.finished" {
				settled <- action
			}
		},
	}, s, a.i18n, a.log)
	runDone := make(chan struct{})
	go func() { m.Run(); close(runDone) }()
	t.Cleanup(func() { m.Close(); <-runDone })
	if maxErrors == 1 {
		select {
		case action := <-settled:
			if action != "campaign.paused" {
				t.Fatalf("failed attempt consumed the daily allowance: %s", action)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("campaign did not pause at the error threshold")
		}
		var state campaignSendState
		if err := a.queries.GetCampaignSendState.Get(&state, campaignID, currentLocalDate()); err != nil {
			t.Fatal(err)
		}
		if state.DailySentCount != 0 || state.QueuedCount != 0 {
			t.Fatalf("failed run retained daily usage: successful=%d queued=%d", state.DailySentCount, state.QueuedCount)
		}
		access := models.WorkspaceAccess{UserID: 1, Workspace: models.Workspace{Personal: true}}
		if err := m.WithCampaignStatusChange(func() error {
			_, err := a.core.UpdateCampaignStatusInWorkspace(access, campaignID, models.CampaignStatusRunning)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case action := <-settled:
		if action != "campaign."+wantStatus {
			t.Fatalf("campaign settled to %s, want %s", action, wantStatus)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("campaign did not reach its daily successful-send limit")
	}
	var state campaignSendState
	if err := a.queries.GetCampaignSendState.Get(&state, campaignID, currentLocalDate()); err != nil {
		t.Fatal(err)
	}
	if state.DailySentCount != 2 || state.QueuedCount != 0 || state.UnsentCount != recipients-2 || state.Status != wantStatus {
		t.Fatalf("daily cap consumed by a failed attempt: status=%s successful=%d queued=%d unsent=%d; want %s, 2, 0, %d",
			state.Status, state.DailySentCount, state.QueuedCount, state.UnsentCount, wantStatus, recipients-2)
	}
	var failures int
	if err := a.db.Get(&failures, `SELECT send_errors FROM campaigns WHERE id=$1`, campaignID); err != nil || failures != 1 {
		t.Fatalf("failure history = %d, %v; want one failed attempt", failures, err)
	}
}

func TestCampaignSuccessfulSendAccountingIsAtomic(t *testing.T) {
	for _, kind := range []string{"private", "pool"} {
		t.Run(kind, func(t *testing.T) {
			a, _, campaignID, ids := newSendErrorReportFixture(t)
			q := prepareQueries(readTestQueries(t), a.db, koanf.New("."))
			s := newManagerStore(q, a.core, nil, a.db)
			id := int64(ids[0])
			statusQuery := `SELECT status FROM campaign_recipients WHERE campaign_id=$1 AND customer_id=$2`
			markSent := func() error { return s.MarkCampaignMessageSent(campaignID, int(id)) }
			if kind == "pool" {
				poolID := seedOrgPoolAllocationPool(t, a.db, "Daily usage pool")
				if err := a.db.Get(&id, `INSERT INTO pool_contacts(email) VALUES('daily@example.test') RETURNING id`); err != nil {
					t.Fatal(err)
				}
				if _, err := a.db.Exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,email_snapshot,status)
					VALUES($1,$2,$3,'daily@example.test','queued')`, campaignID, id, poolID); err != nil {
					t.Fatal(err)
				}
				statusQuery = `SELECT status FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`
				markSent = func() error { return s.MarkPoolCampaignMessageSent(campaignID, id) }
			} else if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status)
				VALUES($1,$2,'queued')`, campaignID, id); err != nil {
				t.Fatal(err)
			}
			// Fail the daily counter write after the recipient transition. No observer
			// may see that reservation released unless its success is counted as well.
			if _, err := a.db.Exec(`ALTER TABLE campaign_daily_usage ADD CONSTRAINT reject_test_usage CHECK(sent_count=0)`); err != nil {
				t.Fatal(err)
			}
			if err := markSent(); err == nil {
				t.Fatal("successful-send accounting unexpectedly bypassed the failing counter")
			}
			var status string
			if err := a.db.Get(&status, statusQuery, campaignID, id); err != nil {
				t.Fatal(err)
			}
			if status != models.CampaignRecipientStatusQueued {
				t.Fatalf("reservation released before the daily success was recorded: status=%s", status)
			}
			if _, err := a.db.Exec(`ALTER TABLE campaign_daily_usage DROP CONSTRAINT reject_test_usage`); err != nil {
				t.Fatal(err)
			}
			// Repeated completion reports, including concurrent ones, count the
			// recipient once and preserve the already-recorded delivery.
			results := make(chan error, 2)
			for range 2 {
				go func() { results <- markSent() }()
			}
			for range 2 {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			var daily, sent int
			if err := a.db.Get(&daily, `SELECT sent_count FROM campaign_daily_usage WHERE campaign_id=$1 AND usage_date=$2::date`, campaignID, currentLocalDate()); err != nil {
				t.Fatal(err)
			}
			if err := a.db.Get(&sent, `SELECT sent FROM campaigns WHERE id=$1`, campaignID); err != nil {
				t.Fatal(err)
			}
			if daily != 1 || sent != 1 {
				t.Fatalf("duplicate success consumed daily quota: daily=%d sent=%d", daily, sent)
			}
		})
	}
}
