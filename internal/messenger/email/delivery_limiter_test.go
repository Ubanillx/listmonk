package email

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

func TestCampaignPacingDefaultsAndIndependentCampaigns(t *testing.T) {
	for _, tc := range []struct {
		source   string
		limit    int
		interval time.Duration
	}{
		{"personal", 0, 3 * time.Second},
		{"organization", 0, 600 * time.Millisecond},
		{"organization", 30, 2 * time.Second},
	} {
		l := NewDeliveryLimiter(1000, 0, 0, nil)
		now := time.Now()
		campaign := &models.Campaign{Base: models.Base{ID: 1}, SMTPSource: tc.source, SMTPRateLimit: tc.limit}
		if d := l.take(now, campaign); d != 0 {
			t.Fatal(d)
		}
		if d := l.take(now, campaign); d != tc.interval {
			t.Fatalf("source=%s limit=%d wait=%s, want %s", tc.source, tc.limit, d, tc.interval)
		}
		other := &models.Campaign{Base: models.Base{ID: 2}, SMTPRateLimit: 1}
		if d := l.take(now, other); d != 0 {
			t.Fatalf("another campaign inherited first campaign's limit: %s", d)
		}
		if d := l.take(now.Add(tc.interval), campaign); d != 0 {
			t.Fatalf("campaign did not resume when its interval elapsed: %s", d)
		}
	}
}

func TestPlatformCapIncludesAllSMTPAndRollingWindow(t *testing.T) {
	l := NewDeliveryLimiter(2, 3, time.Minute, nil)
	now := time.Now()
	for range 2 {
		if d := l.take(now, nil); d != 0 {
			t.Fatal(d)
		}
	}
	if d := l.take(now, &models.Campaign{UUID: "another", SMTPRateLimit: 100}); d != time.Second {
		t.Fatalf("campaign bypassed global SMTP cap: %s", d)
	}
	if d := l.take(now.Add(time.Second), nil); d != 0 {
		t.Fatal(d)
	}
	if d := l.take(now.Add(2*time.Second), nil); d != 58*time.Second {
		t.Fatalf("system mail bypassed platform rolling window: %s", d)
	}
	if d := l.take(now.Add(time.Minute), nil); d != 0 {
		t.Fatal(d)
	}
}

func TestSMTPPoolsSharePlatformCapUnderConcurrency(t *testing.T) {
	server, sent := delayTestSMTP(t)
	l := NewDeliveryLimiter(2, 0, 0, nil)
	var wg sync.WaitGroup
	for i := range 4 {
		// Independent sender pools, sharing only the platform limiter.
		server.UUID = t.Name() + string(rune('a'+i))
		e, err := New("email", server)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		e.SetDeliveryLimiter(l)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.Push(models.Message{From: "a@example.com", To: []string{"b@example.com"}, Body: []byte("test")}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	starts := make([]time.Time, 4)
	for i := range starts {
		starts[i] = <-sent
	}
	if starts[2].Sub(starts[0]) < 950*time.Millisecond || starts[3].Sub(starts[1]) < 950*time.Millisecond {
		t.Fatalf("concurrent senders exceeded the platform cap: %v", starts)
	}
}

func TestRateWaitCancelledWithoutUsingCapacity(t *testing.T) {
	for _, cause := range []string{"campaign", "shutdown", "pool"} {
		t.Run(cause, func(t *testing.T) {
			cancel := make(chan struct{})
			l := NewDeliveryLimiter(1, 0, 0, nil)
			msg := models.Message{}
			var closed <-chan struct{}
			switch cause {
			case "campaign":
				msg.CampaignCancel = cancel
			case "shutdown":
				l.shutdown = cancel
			case "pool":
				closed = cancel
			}
			if err := l.Wait(msg, closed); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- l.Wait(msg, closed) }()
			close(cancel)
			select {
			case err := <-result:
				if !errors.Is(err, ErrSendCancelled) && !errors.Is(err, ErrSMTPUnavailable) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("rate wait was not interrupted")
			}
			if len(l.secondStarts) != 1 {
				t.Fatal("cancelled wait consumed capacity")
			}
		})
	}
}

func TestSharedSMTPConnectionCapAcrossPools(t *testing.T) {
	server, _ := delayTestSMTP(t)
	first, err := New("email", server)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New("email", server)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	g := first.servers[0].connections
	if g != second.servers[0].connections || cap(g.token) != server.MaxConns {
		t.Fatal("same SMTP UUID has independent connection allowances")
	}
	for range server.MaxConns {
		if err := g.wait(0, 0, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		defer g.release()
	}
	cancel := make(chan struct{})
	close(cancel)
	if err := second.servers[0].connections.wait(0, 0, nil, cancel, nil); !errors.Is(err, ErrSendCancelled) {
		t.Fatal("second pool bypassed connection cap", err)
	}
}

func TestPlatformConcurrencyQueuesSystemAndCampaignMail(t *testing.T) {
	l := NewDeliveryLimiter(100, 0, 0, nil)
	l.SetConcurrency(1)
	if err := l.Wait(models.Message{}, nil); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- l.Wait(models.Message{Campaign: &models.Campaign{UUID: "campaign", SMTPRateLimit: 100}}, nil)
	}()
	select {
	case err := <-result:
		t.Fatalf("campaign bypassed system mail's platform concurrency slot: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	l.Release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued campaign did not resume after system mail completed")
	}
	l.Release()
	if l.active != 0 || len(l.secondStarts) != 2 {
		t.Fatal("concurrency wait consumed rate capacity or leaked a slot")
	}
}
