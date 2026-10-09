package main

import (
	"errors"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
)

func TestCampaignDeferralPreservesManualStop(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	s := newManagerStore(a.queries, nil, nil, a.db)
	for _, status := range []string{"paused", "cancelled", "finished", "running"} {
		t.Run(status, func(t *testing.T) {
			var campaignID, customerID int
			if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status)
				VALUES(gen_random_uuid(),'Quota race','Subject','sender@example.test','Body','email',$1::campaign_status) RETURNING id`, status); err != nil {
				t.Fatal(err)
			}
			if err := a.db.Get(&customerID, `INSERT INTO customers(uuid,email,name)
				VALUES(gen_random_uuid(),gen_random_uuid()::text||'@example.test','Recipient') RETURNING id`); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id) VALUES($1,$2)`, campaignID, customerID); err != nil {
				t.Fatal(err)
			}
			err := s.DeferCampaign(campaignID, time.Now().Add(time.Hour))
			if status == models.CampaignStatusRunning {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, manager.ErrCampaignNotRunning) {
				t.Fatalf("stale quota transition returned %v", err)
			}
			var state struct {
				Status    string `db:"status"`
				Recipient string `db:"recipient"`
				Scheduled bool   `db:"scheduled"`
			}
			if err := a.db.Get(&state, `SELECT c.status,cr.status AS recipient,c.next_resume_at IS NOT NULL AS scheduled
				FROM campaigns c JOIN campaign_recipients cr ON cr.campaign_id=c.id WHERE c.id=$1`, campaignID); err != nil {
				t.Fatal(err)
			}
			wantStatus, wantRecipient := status, "pending"
			if status == models.CampaignStatusRunning {
				wantStatus, wantRecipient = "deferred", "deferred"
			}
			if state.Status != wantStatus || state.Recipient != wantRecipient || state.Scheduled != (status == models.CampaignStatusRunning) {
				t.Fatalf("quota worker overwrote manual state: %+v", state)
			}
		})
	}
}

func TestBackgroundCampaignStatusPreservesManualStop(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	s := newManagerStore(a.queries, nil, nil, a.db)
	for _, target := range []string{"paused", "finished"} {
		for _, current := range []string{"paused", "cancelled", "deferred", "finished", "running"} {
			t.Run(current+"/"+target, func(t *testing.T) {
				var id int
				if err := a.db.Get(&id, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status)
					VALUES(gen_random_uuid(),'Cleanup race','Subject','sender@example.test','Body','email',$1::campaign_status) RETURNING id`, current); err != nil {
					t.Fatal(err)
				}
				changed, err := s.UpdateRunningCampaignStatus(id, target)
				if err != nil || changed != (current == "running") {
					t.Fatalf("stale background transition: changed=%v error=%v", changed, err)
				}
				want := current
				if changed {
					want = target
				}
				var status string
				if err := a.db.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, id); err != nil || status != want {
					t.Fatalf("manual campaign state changed: status=%s error=%v", status, err)
				}
			})
		}
	}
}

func TestCampaignDeferralRollsBackAllRecipients(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	s := newManagerStore(a.queries, nil, nil, a.db)
	// Simulate a failure after regular recipients are changed but before public
	// pool recipients are changed. The campaign and both audiences must roll back.
	if _, err := a.db.Exec(`
		INSERT INTO campaigns(id,uuid,name,subject,from_email,body,messenger,status)
			VALUES(1,gen_random_uuid(),'Atomic quota','Subject','sender@example.test','Body','email','running');
		INSERT INTO customers(id,uuid,email,name) VALUES(1,gen_random_uuid(),'regular@example.test','Regular');
		INSERT INTO campaign_recipients(campaign_id,customer_id) VALUES(1,1);
		INSERT INTO customer_lists(id,uuid,name,type) VALUES(1,gen_random_uuid(),'Pool','pool');
		INSERT INTO pool_contacts(id,customer_code,name,email) VALUES(1,'CODE','Pool','pool@example.test');
		INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,email_snapshot) VALUES(1,1,1,'pool@example.test');
		CREATE FUNCTION fail_deferral() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'recipient fixture failure'; END; $$;
		CREATE TRIGGER fail_deferral BEFORE UPDATE ON campaign_pool_recipients FOR EACH ROW EXECUTE FUNCTION fail_deferral();
	`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeferCampaign(1, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("recipient failure was ignored")
	}
	var intact bool
	if err := a.db.Get(&intact, `SELECT c.status='running' AND c.next_resume_at IS NULL AND cr.status='pending' AND cpr.status='pending'
		FROM campaigns c JOIN campaign_recipients cr ON cr.campaign_id=c.id JOIN campaign_pool_recipients cpr ON cpr.campaign_id=c.id WHERE c.id=1`); err != nil || !intact {
		t.Fatalf("partial quota transition survived: intact=%v error=%v", intact, err)
	}
}
