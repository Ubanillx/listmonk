package mailbox

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

// TestScanLeavesUndeliveredBounceInMailbox covers the data-loss path: the
// scanner used a non-blocking send and deleted every downloaded message, so a
// queue that was full (a database falling behind) discarded the bounce and then
// removed the only copy of it from the server. Message 3 is the one that yields
// a bounce; it must survive a scan whose handoff never completes.
func TestScanLeavesUndeliveredBounceInMailbox(t *testing.T) {
	defer func(prev time.Duration) { handoffTimeout = prev }(handoffTimeout)
	handoffTimeout = 100 * time.Millisecond

	opt, done := fakePOP(t, "plain", "")

	// Unbuffered with no reader: the processor never takes the bounce.
	ch := make(chan models.Bounce)
	p := NewPOP(opt, log.New(io.Discard, "", 0))

	scanDone := make(chan error, 1)
	go func() { scanDone <- p.Scan(10, ch) }()

	select {
	case err := <-scanDone:
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not finish: the handoff was expected to time out")
	}

	var commands []string
	select {
	case commands = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fake POP server did not finish")
	}

	for _, cmd := range commands {
		if cmd == "DELE 3" {
			t.Fatalf("deleted the message whose bounce was never handed to the processor: %v", commands)
		}
	}

	// Messages that produced no bounce at all are still removed, so a single
	// unreadable message cannot keep the mailbox from draining.
	if !containsCommand(commands, "DELE 1") || !containsCommand(commands, "DELE 2") {
		t.Fatalf("expected messages without a bounce to be deleted: %v", commands)
	}
}

// TestScanDeletesDeliveredBounce is the positive case: once the processor takes
// the bounce, its message is removed.
func TestScanDeletesDeliveredBounce(t *testing.T) {
	opt, done := fakePOP(t, "plain", "")

	out := make(chan models.Bounce, 8)
	p := NewPOP(opt, log.New(io.Discard, "", 0))
	if err := p.Scan(10, out); err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	commands := <-done
	if !containsCommand(commands, "DELE 3") {
		t.Fatalf("expected the delivered message to be deleted: %v", commands)
	}

	select {
	case b := <-out:
		if b.Type == "" {
			t.Fatalf("unexpected bounce: %+v", b)
		}
	default:
		t.Fatal("expected the bounce to reach the processor")
	}
}

func containsCommand(commands []string, want string) bool {
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == want {
			return true
		}
	}

	return false
}
