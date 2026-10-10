package manager

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

type sendErrorStore struct {
	deferCaptureStore
	errors, sent, resets int
	customerID           int
	poolID               int64
	recipients           []models.CampaignCustomer
}

func (s *sendErrorStore) RecordCampaignSendError(int) error                   { s.errors++; return nil }
func (s *sendErrorStore) MarkCampaignMessageSent(int, int) error              { s.sent++; return nil }
func (s *sendErrorStore) MarkPoolCampaignMessageSent(int, int64) error        { s.sent++; return nil }
func (s *sendErrorStore) ResetPoolCampaignQueuedRecipients(int, string) error { return nil }
func (s *sendErrorStore) MarkCampaignRecipientStatus(_ int, id int, status string) error {
	if status != models.CampaignRecipientStatusPending {
		return fmt.Errorf("unexpected status %s", status)
	}
	s.customerID = id
	s.resets++
	return nil
}
func (s *sendErrorStore) MarkPoolCampaignRecipientStatus(_ int, id int64, status string) error {
	if status != models.CampaignRecipientStatusPending {
		return fmt.Errorf("unexpected status %s", status)
	}
	s.poolID = id
	s.resets++
	return nil
}
func (s *sendErrorStore) NextCustomers(int, int) ([]models.CampaignCustomer, error) {
	return s.recipients, nil
}

type failedMessenger struct {
	testMessenger
	err error
}

func (m *failedMessenger) Push(models.Message) error { return m.err }

func TestCampaignWorkerRecordsFailuresWithoutCountingSchedulingControls(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		err                              error
		poolID                           int64
		wantErrors, wantSent, wantResets int
	}{
		{"customer failure", errors.New("SMTP rejected recipient"), 0, 1, 0, 1},
		{"pool failure", errors.New("SMTP rejected recipient"), 41, 1, 0, 1},
		{"successful delivery", nil, 0, 0, 1, 0},
		{"quota deferral", fmt.Errorf("quota: %w", email.ErrSMTPQuotaExceeded), 0, 0, 0, 0},
		{"intentional stop", fmt.Errorf("cancel: %w", email.ErrSendCancelled), 0, 0, 0, 0},
		{"manager closed", ErrManagerClosed, 0, 0, 0, 0},
		{"SMTP unavailable", ErrPersonalSMTPUnavailable, 0, 1, 0, 0},
		{"reply route unavailable", ErrReplyMailboxUnavailable, 0, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &sendErrorStore{}
			m := newTestManager()
			defer m.Close()
			m.store = s
			var logs bytes.Buffer
			m.log.SetOutput(&logs)
			p := &pipe{m: m, camp: &models.Campaign{Base: models.Base{ID: 31}, DailyResumeTime: "17:00"},
				messenger: &failedMessenger{err: tc.err}, wg: &sync.WaitGroup{}, done: make(chan struct{}),
				rate: ratecounter.NewRateCounter(time.Minute)}
			p.wg.Add(1)
			go m.worker()
			m.campMsgQ <- CampaignMessage{Campaign: p.camp, Customer: models.Customer{Base: models.Base{ID: 7}}, PoolContactID: tc.poolID, pipe: p}
			done := make(chan struct{})
			go func() { p.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not drain")
			}
			if s.errors != tc.wantErrors || s.sent != tc.wantSent || s.resets != tc.wantResets {
				t.Fatalf("errors=%d sent=%d resets=%d", s.errors, s.sent, s.resets)
			}
			if tc.wantErrors == 0 && strings.Contains(logs.String(), "error sending message") {
				t.Fatalf("scheduling control logged as a delivery error: %s", logs.String())
			}
			if tc.wantResets > 0 && (s.poolID != tc.poolID || (tc.poolID == 0 && s.customerID != 7)) {
				t.Fatalf("wrong recipient reset: customer=%d pool=%d", s.customerID, s.poolID)
			}
		})
	}
}

func TestCampaignRenderingFailureIsRecordedAndRecipientRemainsRetryable(t *testing.T) {
	for _, poolID := range []int64{0, 41} {
		t.Run(fmt.Sprint(poolID), func(t *testing.T) {
			s := &sendErrorStore{recipients: []models.CampaignCustomer{{Customer: models.Customer{Base: models.Base{ID: 7}}, PoolContactID: poolID}}}
			m := newTestManager()
			defer m.Close()
			m.store = s
			m.cfg.MaxSendErrors = 1
			c := &models.Campaign{Base: models.Base{ID: 31}, Tpl: template.Must(template.New("body").Parse(`{{.NonexistentField}}`))}
			p := &pipe{m: m, camp: c, done: make(chan struct{})}
			if _, err := p.NextCustomers(); err != nil {
				t.Fatal(err)
			}
			if s.errors != 1 || s.resets != 1 || !p.withErrors.Load() || !p.stopped.Load() {
				t.Fatalf("render failure lost: errors=%d resets=%d paused=%v", s.errors, s.resets, p.stopped.Load())
			}
		})
	}
}

func TestCampaignErrorThresholdRemainsPerRun(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	p := &pipe{m: m, camp: &models.Campaign{}, done: make(chan struct{})}
	p.OnError()
	if p.errors.Load() != 1 || p.stopped.Load() {
		t.Fatal("disabled threshold lost count or stopped campaign")
	}
	m.cfg.MaxSendErrors = 3
	p.OnError()
	if p.stopped.Load() {
		t.Fatal("paused before threshold")
	}
	p.OnError()
	if !p.withErrors.Load() {
		t.Fatal("did not pause at threshold")
	}
}
