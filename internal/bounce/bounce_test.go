package bounce

import (
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

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
