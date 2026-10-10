package main

import (
	"sync"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestCampaignSendErrorCountsSurviveConcurrentAttemptsAndSuccessfulRetry(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	s := newManagerStore(a.queries, nil, nil, a.db)
	var campaignID, customerID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status)
		VALUES(gen_random_uuid(),'Errors','Subject','sender@example.test','Body','email','running') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&customerID, `INSERT INTO customers(uuid,email,name) VALUES(gen_random_uuid(),'retry@example.test','Retry') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES($1,$2,'queued')`, campaignID, customerID); err != nil {
		t.Fatal(err)
	}
	const attempts = 24
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.RecordCampaignSendError(campaignID) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var stats []models.CampaignStats
	if err := a.queries.GetCampaignStatus.Select(&stats, models.CampaignStatusRunning); err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].SendErrors != attempts {
		t.Fatalf("live error stats=%+v", stats)
	}
	if err := s.UpdateCampaignStatus(campaignID, models.CampaignStatusPaused); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCampaignStatus(campaignID, models.CampaignStatusRunning); err != nil {
		t.Fatal(err)
	}
	// Re-create the store to exercise persistence independently of process memory.
	s = newManagerStore(a.queries, nil, nil, a.db)
	if err := s.MarkCampaignMessageSent(campaignID, customerID); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCampaign(campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if c.SendErrors != attempts || c.Sent != 1 {
		t.Fatalf("successful retry erased errors: %+v", c)
	}
}
