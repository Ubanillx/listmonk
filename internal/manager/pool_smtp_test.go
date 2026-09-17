package manager

import (
	"errors"
	"io"
	"log"
	"testing"

	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

// TestResolveMessengerUsesAssignedPoolSMTP pins the platform-level public-pool
// routing rule: a message carrying an assigned SMTP UUID is resolved by that
// UUID and never by the campaign owner's personal pool, while an ordinary
// campaign message keeps the owner path.
func TestResolveMessengerUsesAssignedPoolSMTP(t *testing.T) {
	var resolved []string
	m := New(Config{
		Concurrency: 1,
		PoolSMTP: func(uuid string) (*email.Emailer, error) {
			resolved = append(resolved, uuid)
			return nil, errors.New("test resolver has no SMTP")
		},
	}, nil, nil, log.New(io.Discard, "", 0))

	campaign := &models.Campaign{Name: "platform pool campaign"}
	_, err := m.resolveMessenger(models.Message{
		Messenger:          email.MessengerName,
		OwnerUserID:        7,
		Campaign:           campaign,
		PoolSenderSMTPUUID: "11111111-1111-1111-1111-111111111111",
	})
	if !errors.Is(err, ErrPersonalSMTPUnavailable) {
		t.Fatalf("resolveMessenger(pool) = %v, want ErrPersonalSMTPUnavailable", err)
	}
	if len(resolved) != 1 || resolved[0] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("pool resolver received %v, want the assigned UUID", resolved)
	}

	// An ordinary campaign message must not consult the pool resolver at all.
	resolved = nil
	_, err = m.resolveMessenger(models.Message{
		Messenger:   email.MessengerName,
		OwnerUserID: 7,
		Campaign:    campaign,
	})
	if !errors.Is(err, ErrPersonalSMTPUnavailable) {
		t.Fatalf("resolveMessenger(owner) = %v, want ErrPersonalSMTPUnavailable", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("owner message used the pool resolver: %v", resolved)
	}
}

// TestPoolSMTPSenderCacheIsSharedAndInvalidated pins that one SMTP account is
// resolved and cached once, and that invalidation drops the cached pool so the
// next resolution revalidates the account.
func TestPoolSMTPSenderCacheIsSharedAndInvalidated(t *testing.T) {
	const uuid = "22222222-2222-2222-2222-222222222222"
	calls := 0
	m := New(Config{
		Concurrency: 1,
		PoolSMTP: func(got string) (*email.Emailer, error) {
			calls++
			if got != uuid {
				t.Errorf("PoolSMTP(%q), want %q", got, uuid)
			}
			return email.New(email.MessengerName, email.Server{
				Name: "pooled", UUID: uuid, TLSType: "none",
				Opt: smtppool.Opt{Host: "smtp.example.invalid", Port: 465, MaxConns: 1},
			})
		},
	}, nil, nil, log.New(io.Discard, "", 0))

	for i := 0; i < 3; i++ {
		if _, err := m.resolvePoolSMTPMessenger(uuid); err != nil {
			t.Fatalf("resolvePoolSMTPMessenger: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1 (cached)", calls)
	}

	m.InvalidateAllPoolSMTP()
	if _, err := m.resolvePoolSMTPMessenger(uuid); err != nil {
		t.Fatalf("resolvePoolSMTPMessenger after invalidation: %v", err)
	}
	if calls != 2 {
		t.Fatalf("resolver calls after invalidation = %d, want 2", calls)
	}
}
