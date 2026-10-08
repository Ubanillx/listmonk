package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeReplyIMAP serves a local read-only IMAP conversation and records commands.
func fakeReplyIMAP(t *testing.T, messages []string) (int, <-chan []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	done := make(chan []string, 16)
	handle := func(c net.Conn) {
		var commands []string
		defer func() { done <- commands }()
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprint(c, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
		reader := bufio.NewReader(c)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 2 {
				return
			}
			tag := fields[0]
			cmd := strings.Join(fields[1:], " ")
			commands = append(commands, strings.ReplaceAll(cmd, `"`, ""))
			switch {
			case strings.HasPrefix(cmd, "LOGIN "):
			case cmd == "CAPABILITY":
				fmt.Fprint(c, "* CAPABILITY IMAP4rev1\r\n")
			case strings.HasPrefix(cmd, "EXAMINE "):
				fmt.Fprintf(c, "* %d EXISTS\r\n* OK [UIDVALIDITY 42] valid\r\n* OK [UIDNEXT %d] next\r\n", len(messages), len(messages)+1)
			case strings.HasPrefix(cmd, "UID SEARCH "):
				fmt.Fprint(c, "* SEARCH")
				for i := range messages {
					fmt.Fprintf(c, " %d", i+1)
				}
				fmt.Fprint(c, "\r\n")
			case strings.HasPrefix(cmd, "UID FETCH "):
				uid, err := strconv.Atoi(fields[3])
				if err != nil || uid < 1 || uid > len(messages) {
					fmt.Fprintf(c, "%s NO missing\r\n", tag)
					continue
				}
				raw := messages[uid-1]
				if strings.Contains(cmd, "BODY.PEEK") {
					fmt.Fprintf(c, "* %d FETCH (UID %d BODY[]<0> {%d}\r\n%s)\r\n", uid, uid, len(raw), raw)
				} else {
					fmt.Fprintf(c, "* %d FETCH (UID %d RFC822.SIZE %d)\r\n", uid, uid, len(raw))
				}
			case cmd == "LOGOUT":
				fmt.Fprintf(c, "* BYE bye\r\n%s OK logout\r\n", tag)
				return
			default:
				fmt.Fprintf(c, "%s BAD unexpected\r\n", tag)
				continue
			}
			fmt.Fprintf(c, "%s OK done\r\n", tag)
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, done
}

// silentReplyAIPOP accepts a connection and never replies, mimicking a server
// that completes the TCP handshake and then hangs. The returned function closes
// the accepted connection and the listener.
func silentReplyAIPOP(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu     sync.Mutex
		conn   net.Conn
		closed bool
	)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if closed {
			c.Close()
			return
		}
		conn = c
	}()

	var once sync.Once
	release := func() {
		once.Do(func() {
			mu.Lock()
			closed = true
			c := conn
			mu.Unlock()
			ln.Close()
			if c != nil {
				c.Close()
				return
			}
			// The scan dials from another goroutine; wait briefly for the
			// accept so the connection is really torn down.
			for i := 0; i < 200; i++ {
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				c = conn
				mu.Unlock()
				if c != nil {
					c.Close()
					return
				}
			}
		})
	}
	t.Cleanup(release)
	return ln.Addr().(*net.TCPAddr).Port, release
}

// dribblingReplyAIPOP greets the client and then stalls: it trickles bytes
// without ever finishing a response line, so every client read succeeds and the
// idle read deadline never fires. Only the scan budget can end this exchange.
// The returned channel is closed when the client connection goes away.
func dribblingReplyAIPOP(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(c, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
		for {
			if _, err := fmt.Fprint(c, "STAT "); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, done
}

func replyAITestSource(id, port int) replyAIMailboxSource {
	return replyAIMailboxSource{ID: id, Host: "127.0.0.1", Port: port, Username: "user", Password: "pass"}
}

// allowLoopbackMailboxHosts relaxes the mailbox host policy for these tests.
// They run fake POP servers on the loopback interface, which the production
// policy refuses on purpose (see cmd/mailbox_host.go).
func allowLoopbackMailboxHosts(t *testing.T) {
	t.Helper()
	previous := mailboxHostPolicyCheck
	mailboxHostPolicyCheck = func(net.IP) error { return nil }
	t.Cleanup(func() { mailboxHostPolicyCheck = previous })
}

type replyAIScanResult struct {
	id     int
	err    error
	bodies []string
}

// TestReplyAIPoolScansOtherMailboxesWhileOneHangs is the liveness regression for
// the defect: with a serial poller a single hung POP server stalled every other
// mailbox. The hung mailbox must occupy one worker while the remaining
// mailboxes are still scanned in the same round.
func TestReplyAIPoolScansOtherMailboxesWhileOneHangs(t *testing.T) {
	allowLoopbackMailboxHosts(t)
	hungPort, releaseHung := silentReplyAIPOP(t)
	firstPort, firstCommands := fakeReplyIMAP(t, []string{"Subject: first\r\n\r\nfirst body\r\n"})
	secondPort, secondCommands := fakeReplyIMAP(t, []string{"Subject: second\r\n\r\nsecond body\r\n"})

	// The hung mailbox is scanned first and gets a long budget, so it is still
	// blocked while the other two are scanned.
	sources := []replyAIMailboxSource{
		replyAITestSource(1, hungPort),
		replyAITestSource(2, firstPort),
		replyAITestSource(3, secondPort),
	}
	budgets := map[int]replyAIScanBudget{
		1: {dial: time.Second, read: 5 * time.Second, scan: 5 * time.Second},
		2: {dial: time.Second, read: 5 * time.Second, scan: 5 * time.Second},
		3: {dial: time.Second, read: 5 * time.Second, scan: 5 * time.Second},
	}

	results := make(chan replyAIScanResult, len(sources))
	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		runReplyAIMailboxPool(sources, 2, func(source replyAIMailboxSource) {
			var bodies []string
			err := fetchReplyAIMailbox(source, budgets[source.ID], func(_ int, raw []byte) {
				bodies = append(bodies, string(raw))
			})
			results <- replyAIScanResult{id: source.ID, err: err, bodies: bodies}
		})
	}()

	// Both healthy mailboxes must be scanned while mailbox 1 is still hanging.
	healthy := map[int]replyAIScanResult{}
	for len(healthy) < 2 {
		select {
		case r := <-results:
			if r.id == 1 {
				t.Fatalf("hung mailbox returned before its budget expired: %+v", r)
			}
			healthy[r.id] = r
		case <-time.After(3 * time.Second):
			t.Fatalf("mailbox 1 hanging prevented the other mailboxes from being scanned: %+v", healthy)
		}
	}
	select {
	case r := <-results:
		t.Fatalf("hung mailbox produced a result while it should still be blocked: %+v", r)
	case <-roundDone:
		t.Fatal("scan round finished even though one mailbox was still hung")
	default:
	}
	for id, r := range healthy {
		if r.err != nil || len(r.bodies) != 1 {
			t.Fatalf("mailbox %d scan = %+v, want one message and no error", id, r)
		}
	}
	for _, tc := range []struct {
		id   int
		cmds <-chan []string
		want string
	}{
		{2, firstCommands, "LOGIN user pass"},
		{3, secondCommands, "LOGIN user pass"},
	} {
		if got := strings.Join(<-tc.cmds, ","); !strings.Contains(got, tc.want) || !strings.Contains(got, "EXAMINE INBOX") || !strings.Contains(got, "BODY.PEEK[]") {
			t.Fatalf("mailbox %d IMAP conversation = %q, want %q", tc.id, got, tc.want)
		}
	}

	// Releasing the hung server must end the round with a failure for that
	// mailbox only: the worker is not leaked.
	releaseHung()
	select {
	case r := <-results:
		if r.id != 1 || r.err == nil {
			t.Fatalf("hung mailbox result = %+v, want a failure for mailbox 1", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hung mailbox did not fail after its connection was closed")
	}
	select {
	case <-roundDone:
	case <-time.After(3 * time.Second):
		t.Fatal("scan round did not finish after the hung connection was closed")
	}
}

// TestReplyAIMailboxScanTimesOutOnSilentServer proves the read deadline: a
// server that accepts the connection and never replies must fail the scan
// within the configured timeout instead of blocking forever. The timeout is a
// package variable, so shrinking it here also proves the production path reads
// it.
func TestReplyAIMailboxScanTimesOutOnSilentServer(t *testing.T) {
	allowLoopbackMailboxHosts(t)
	port, _ := silentReplyAIPOP(t)

	previous := replyAIMailboxReadTimeout
	replyAIMailboxReadTimeout = 250 * time.Millisecond
	t.Cleanup(func() { replyAIMailboxReadTimeout = previous })

	start := time.Now()
	err := fetchReplyAIMailbox(replyAITestSource(9, port), replyAIDefaultScanBudget(), func(id int, _ []byte) {
		t.Errorf("silent server produced message %d", id)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("scan of a server that never replies returned no error")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("scan took %s, want it bounded by the %s read timeout", elapsed, replyAIMailboxReadTimeout)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("scan failed after %s, too early for the %s read deadline to have fired: %v", elapsed, replyAIMailboxReadTimeout, err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) && !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("scan failed for an unexpected reason: %v", err)
	}
	t.Logf("silent server failed the scan after %s: %v", elapsed, err)
}

// TestReplyAIMailboxScanAbandonsDribblingServer covers the case the idle read
// deadline cannot catch: a server that keeps sending bytes without ever
// finishing a response. The scan budget must force the connection closed so the
// worker (and its goroutine) is released.
func TestReplyAIMailboxScanAbandonsDribblingServer(t *testing.T) {
	allowLoopbackMailboxHosts(t)
	port, serverDone := dribblingReplyAIPOP(t)
	budget := replyAIScanBudget{dial: time.Second, read: 2 * time.Second, scan: 300 * time.Millisecond}

	start := time.Now()
	err := fetchReplyAIMailbox(replyAITestSource(4, port), budget, func(int, []byte) {})
	elapsed := time.Since(start)

	if !errors.Is(err, errReplyAIScanTimeout) {
		t.Fatalf("scan error = %v, want %v", err, errReplyAIScanTimeout)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("scan took %s, want it bounded by the %s budget", elapsed, budget.scan)
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("connection was not torn down after the scan budget expired")
	}
}

// TestReplyAIPoolBoundsConcurrency guards requirement 2: mailboxes are scanned
// by a fixed pool, never by one goroutine per mailbox.
func TestReplyAIPoolBoundsConcurrency(t *testing.T) {
	allowLoopbackMailboxHosts(t)
	const (
		workers = 3
		total   = 12
	)
	sources := make([]replyAIMailboxSource, total)
	for i := range sources {
		sources[i] = replyAIMailboxSource{ID: i + 1}
	}

	var running, peak, scanned int32
	runReplyAIMailboxPool(sources, workers, func(replyAIMailboxSource) {
		atomic.AddInt32(&scanned, 1)
		cur := atomic.AddInt32(&running, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&running, -1)
	})

	if got := atomic.LoadInt32(&scanned); got != total {
		t.Fatalf("scanned %d mailboxes, want %d", got, total)
	}
	if got := atomic.LoadInt32(&peak); got > workers {
		t.Fatalf("ran %d scans concurrently, want at most %d", got, workers)
	} else if got < 2 {
		t.Fatalf("pool only ever ran %d scan at a time, workers are not used", got)
	}
}

// TestReplyAIPoolHandlesEmptySourceList keeps an empty mailbox list from
// starting or blocking on workers.
func TestReplyAIPoolHandlesEmptySourceList(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runReplyAIMailboxPool(nil, replyAIMaxConcurrent, func(replyAIMailboxSource) {
			t.Error("scan called without sources")
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("empty source list blocked the pool")
	}
}

// TestReplyAIMailboxFailureMessage covers requirement 4: every failure names the
// mailbox and the server it points at, so a hung server is distinguishable from
// a failing one.
func TestReplyAIMailboxFailureMessage(t *testing.T) {
	source := replyAIMailboxSource{ID: 42, Host: "pop.example.com", Port: 995}

	hung := replyAIMailboxFailureMessage(source, fmt.Errorf("%w after 2m0s", errReplyAIScanTimeout), replyAIMailboxTimeout)
	if !strings.Contains(hung, "mailbox 42") || !strings.Contains(hung, "pop.example.com:995") ||
		!strings.Contains(hung, "timed out") || !strings.Contains(hung, "unresponsive") {
		t.Fatalf("timeout message does not name the mailbox and its server: %q", hung)
	}

	failed := replyAIMailboxFailureMessage(source, errors.New("connection refused"), replyAIMailboxTimeout)
	if !strings.Contains(failed, "mailbox 42") || !strings.Contains(failed, "pop.example.com:995") ||
		!strings.Contains(failed, "connection refused") {
		t.Fatalf("failure message does not name the mailbox and its server: %q", failed)
	}
	if strings.Contains(failed, "timed out") {
		t.Fatalf("a plain failure must not be reported as a hang: %q", failed)
	}
}
