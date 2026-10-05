package email

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

func delayTestSMTP(t *testing.T) (Server, <-chan time.Time) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	sent := make(chan time.Time, 10)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				fmt.Fprint(conn, "220 localhost SMTP\r\n")
				r := bufio.NewReader(conn)
				data := false
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimSpace(line)
					if data {
						if line == "." {
							data = false
							sent <- time.Now()
							fmt.Fprint(conn, "250 accepted\r\n")
						}
						continue
					}
					switch {
					case line == "DATA":
						data = true
						fmt.Fprint(conn, "354 send data\r\n")
					case line == "QUIT":
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "250 localhost\r\n")
					}
				}
			}()
		}
	}()
	return Server{UUID: t.Name(), AuthProtocol: "none", TLSType: "none", Opt: smtppool.Opt{
		Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
		MaxConns: 2, IdleTimeout: time.Second, PoolWaitTimeout: time.Second,
	}}, sent
}

func TestSMTPDelaySerializesAcrossEmailers(t *testing.T) {
	server, sent := delayTestSMTP(t)
	server.SendDelayMin, server.SendDelayMax = 30*time.Millisecond, 30*time.Millisecond
	first, err := New("account", server)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New("organization", server)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	started := time.Now()
	results := make(chan error, 2)
	for _, sender := range []*Emailer{first, second} {
		go func(e *Emailer) {
			results <- e.Push(models.Message{From: "sender@example.test", To: []string{"recipient@example.test"}, Body: []byte("hello")})
		}(sender)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("SMTP sends did not finish")
		}
	}
	one, two := <-sent, <-sent
	if one.Sub(started) < server.SendDelayMin || two.Sub(one) < server.SendDelayMin {
		t.Fatalf("concurrent senders bypassed delay: first %s, gap %s", one.Sub(started), two.Sub(one))
	}
}

type delayQuota struct {
	reserved            chan struct{}
	committed, released int
}

func (q *delayQuota) HasServerQuota(string, int) (bool, error) { return true, nil }
func (q *delayQuota) ReserveServer(string, int) (bool, error)  { close(q.reserved); return true, nil }
func (q *delayQuota) CommitServer(string) error                { q.committed++; return nil }
func (q *delayQuota) ReleaseServer(string)                     { q.released++ }

func TestSMTPDelayCancellationDoesNotSendOrConsumeQuota(t *testing.T) {
	for _, kind := range []string{"close", "shutdown", "pause"} {
		t.Run(kind, func(t *testing.T) {
			server, sent := delayTestSMTP(t)
			server.SendDelayMin, server.SendDelayMax, server.DailyLimit = time.Hour, time.Hour, 10
			e, err := New("", server)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			quota := &delayQuota{reserved: make(chan struct{})}
			e.SetQuotaTracker(quota)
			cancel := make(chan struct{})
			msg := models.Message{UseSMTPQuota: true}
			if kind == "shutdown" {
				msg.SendCancel = cancel
			}
			if kind == "pause" {
				msg.CampaignCancel = cancel
			}
			result := make(chan error, 1)
			go func() { result <- e.Push(msg) }()
			<-quota.reserved
			want := ErrSendCancelled
			if kind == "close" {
				e.Close()
				want = ErrSMTPUnavailable
			} else {
				close(cancel)
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("cancel = %v, want %v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("delay was not cancelled promptly")
			}
			if quota.committed != 0 || quota.released != 1 {
				t.Fatalf("quota not released: %+v", quota)
			}
			select {
			case <-sent:
				t.Fatal("cancelled message sent")
			default:
			}
		})
	}
}

func TestRandomSendDelayRangeAndGateLifecycle(t *testing.T) {
	for range 100 {
		d := randomSendDelay(time.Second, 3*time.Second)
		if d < time.Second || d > 3*time.Second {
			t.Fatalf("out of range: %s", d)
		}
	}
	if randomSendDelay(0, 0) != 0 {
		t.Fatal("disabled delay must be zero")
	}
	a, b := acquireSendGate(t.Name()), acquireSendGate(t.Name())
	if a != b {
		t.Fatal("same sender must share gate")
	}
	releaseSendGate(t.Name())
	releaseSendGate(t.Name())
	c := acquireSendGate(t.Name())
	defer releaseSendGate(t.Name())
	if c == a {
		t.Fatal("unused gate was retained")
	}
}

func TestQueuedDelayCanBeCancelledWithoutReleasingActiveSend(t *testing.T) {
	g := acquireSendGate(t.Name())
	defer releaseSendGate(t.Name())
	if err := g.wait(0, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cancel := make(chan struct{})
	close(cancel)
	if err := g.wait(time.Hour, time.Hour, nil, cancel, nil); !errors.Is(err, ErrSendCancelled) {
		t.Fatal(err)
	}
	if len(g.token) != 1 {
		t.Fatal("cancelled waiter released active delivery")
	}
	g.release()
	// Another sender is independent of this one.
	other := acquireSendGate(t.Name() + "-other")
	defer releaseSendGate(t.Name() + "-other")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := other.wait(0, 0, nil, nil, nil); err == nil {
			other.release()
		}
	}()
	wg.Wait()
}
