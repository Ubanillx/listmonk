package bounce

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

// TestNewRejectsMissingWebhookCredentials locks the fail-fast gate: an enabled
// webhook provider without credentials would otherwise accept unauthenticated
// (or trivially forgeable) bounce events on a public endpoint.
func TestNewRejectsMissingWebhookCredentials(t *testing.T) {
	newManager := func(mutate func(*Opt)) error {
		opt := Opt{WebhooksEnabled: true}
		mutate(&opt)
		_, err := New(opt, &Queries{}, log.New(io.Discard, "", 0))
		return err
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*Opt)
		wantErr bool
	}{
		{"postmark enabled without credentials", func(o *Opt) { o.Postmark.Enabled = true }, true},
		{"postmark with username only", func(o *Opt) {
			o.Postmark.Enabled = true
			o.Postmark.Username = "user"
		}, true},
		{"postmark with both credentials", func(o *Opt) {
			o.Postmark.Enabled = true
			o.Postmark.Username = "user"
			o.Postmark.Password = "pass"
		}, false},
		{"forwardemail enabled without key", func(o *Opt) { o.ForwardEmail.Enabled = true }, true},
		{"forwardemail with key", func(o *Opt) {
			o.ForwardEmail.Enabled = true
			o.ForwardEmail.Key = "secret"
		}, false},
		{"sendgrid enabled without key", func(o *Opt) { o.SendgridEnabled = true }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The sendgrid case is validated by NewSendgrid itself; the key
			// here is intentionally invalid so the test documents that a bad
			// key fails startup instead of leaving a nil client behind.
			tc := tc
			err := newManager(tc.mutate)
			if tc.wantErr && err == nil {
				t.Fatal("expected initialization to fail")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected initialization to succeed, got %v", err)
			}
		})
	}
}

// TestRecordReportsFullQueue covers the webhook producer path. It must not block
// indefinitely when the database writer falls behind: the request has to finish
// so the sender can retry, and the failure has to be reported rather than the
// event being silently dropped.
func TestRecordReportsFullQueue(t *testing.T) {
	defer func(prev time.Duration) { queueSendTimeout = prev }(queueSendTimeout)
	queueSendTimeout = 50 * time.Millisecond

	m := &Manager{queue: make(chan models.Bounce, 1)}

	if err := m.Record(models.Bounce{Email: "first@example.com"}); err != nil {
		t.Fatalf("expected the first event to be buffered: %v", err)
	}

	start := time.Now()
	err := m.Record(models.Bounce{Email: "second@example.com"})
	if err == nil {
		t.Fatal("expected a full queue to be reported")
	}
	if !strings.Contains(err.Error(), "queue is full") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Record blocked for %v instead of giving up", elapsed)
	}
}

// TestRecordDeliversWhenSpaceFreesUp covers the burst case the bounded wait
// exists for: a consumer that catches up lets the event through instead of
// failing the webhook.
func TestRecordDeliversWhenSpaceFreesUp(t *testing.T) {
	m := &Manager{queue: make(chan models.Bounce, 1)}
	if err := m.Record(models.Bounce{Email: "first@example.com"}); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		<-m.queue
	}()

	if err := m.Record(models.Bounce{Email: "second@example.com"}); err != nil {
		t.Fatalf("expected the event to be delivered once the queue drained: %v", err)
	}
}
