package manager

import (
	"errors"
	"io"
	"log"
	"sync/atomic"
	"testing"

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
