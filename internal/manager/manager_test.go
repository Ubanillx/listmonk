package manager

import (
	"errors"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/schedule"
	"github.com/knadh/listmonk/models"
)

type testMessenger struct {
	name   string
	pushed atomic.Int32
}

func (m *testMessenger) Name() string { return m.name }

func (m *testMessenger) Push(models.Message) error {
	m.pushed.Add(1)
	return nil
}

func (m *testMessenger) Flush() error { return nil }

func (m *testMessenger) Close() error { return nil }

func newTestManager() *Manager {
	return New(Config{Concurrency: 1}, nil, nil, log.New(io.Discard, "", 0))
}

func TestManagerCloseIsIdempotentAndRejectsQueuedMessages(t *testing.T) {
	m := newTestManager()
	m.Close()
	m.Close()

	if err := m.PushMessage(models.Message{Subject: "after close"}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("PushMessage after Close() = %v, want ErrManagerClosed", err)
	}
	if err := m.PushCampaignMessage(CampaignMessage{Campaign: &models.Campaign{Name: "after close"}}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("PushCampaignMessage after Close() = %v, want ErrManagerClosed", err)
	}
}

func TestManagerCanSendMessageNeverFallsBackToPlatformSMTP(t *testing.T) {
	m := newTestManager()
	platform := &testMessenger{name: "email"}
	if err := m.AddMessenger(platform); err != nil {
		t.Fatal(err)
	}

	err := m.CanSendMessage(models.Message{
		Messenger:   "email",
		OwnerUserID: 42,
	})
	if !errors.Is(err, ErrPersonalSMTPUnavailable) {
		t.Fatalf("CanSendMessage() = %v, want ErrPersonalSMTPUnavailable", err)
	}
	if got := platform.pushed.Load(); got != 0 {
		t.Fatalf("platform messenger was unexpectedly used (%d pushes)", got)
	}
}

func TestManagerDistinguishesCustomMessengerWithEmailPrefix(t *testing.T) {
	m := newTestManager()
	custom := &testMessenger{name: "emailwebhook"}
	if err := m.AddMessenger(custom); err != nil {
		t.Fatal(err)
	}

	if err := m.CanSendMessage(models.Message{
		Messenger:   custom.name,
		OwnerUserID: 42,
	}); err != nil {
		t.Fatalf("custom messenger was treated as personal SMTP: %v", err)
	}
}

type deferCaptureStore struct {
	Store
	nextResumeAt     time.Time
	deferCalls       int
	resetStatus      string
	resetQueuedCalls int
}

func (s *deferCaptureStore) DeferCampaign(_ int, nextResumeAt time.Time) error {
	s.nextResumeAt = nextResumeAt
	s.deferCalls++
	return nil
}

func (s *deferCaptureStore) ResetCampaignQueuedRecipients(_ int, toStatus string) error {
	s.resetStatus = toStatus
	s.resetQueuedCalls++
	return nil
}

func TestPipeDeferPersistsNextDailyResumeAt(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	defer func() { time.Local = oldLocal }()

	resumeTime := time.Now().Add(2 * time.Hour).Format(schedule.DailyResumeLayout)
	store := &deferCaptureStore{}
	m := newTestManager()
	m.store = store
	p := &pipe{
		camp: &models.Campaign{
			Base:            models.Base{ID: 7},
			DailyResumeTime: resumeTime,
		},
		m: m,
	}

	p.Defer()

	if store.deferCalls != 1 {
		t.Fatalf("DeferCampaign calls = %d, want 1", store.deferCalls)
	}
	if !store.nextResumeAt.After(time.Now()) {
		t.Fatalf("next resume time = %s, want a future time", store.nextResumeAt)
	}
	if got := store.nextResumeAt.Format(schedule.DailyResumeLayout); got != resumeTime {
		t.Fatalf("next resume clock = %s, want %s", got, resumeTime)
	}
	if p.stopped.Load() {
		t.Fatal("campaign-limit deferral stopped queued messages")
	}
	if !p.deferred.Load() {
		t.Fatal("pipe was not marked deferred")
	}
}

func TestPipeCleanupKeepsCampaignDeferredAfterDrainingLimitBatch(t *testing.T) {
	store := &deferCaptureStore{}
	m := newTestManager()
	m.store = store
	p := &pipe{
		camp: &models.Campaign{
			Base: models.Base{ID: 8},
			Name: "daily-limit",
		},
		m: m,
	}
	p.deferred.Store(true)

	p.cleanup()

	if store.resetQueuedCalls != 1 || store.resetStatus != models.CampaignRecipientStatusDeferred {
		t.Fatalf("queued recipient reset = (%d, %q), want (1, %q)",
			store.resetQueuedCalls, store.resetStatus, models.CampaignRecipientStatusDeferred)
	}
}

func TestPipeDeferImmediatelyStopsQueuedMessages(t *testing.T) {
	store := &deferCaptureStore{}
	m := newTestManager()
	m.store = store
	p := &pipe{
		camp: &models.Campaign{
			Base:            models.Base{ID: 9},
			DailyResumeTime: time.Now().Add(time.Hour).Format(schedule.DailyResumeLayout),
		},
		m: m,
	}

	p.DeferImmediately()

	if !p.stopped.Load() || p.stopReason.Load() != stopReasonDeferred {
		t.Fatal("SMTP quota deferral did not stop queued messages")
	}
}
