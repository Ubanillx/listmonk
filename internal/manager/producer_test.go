package manager

import (
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

// oneBatchStore hands each campaign its recipients exactly once so a producer
// stops after the first batch instead of looping forever.
type oneBatchStore struct {
	sendErrorStore
	mu      sync.Mutex
	batches map[int][]models.CampaignCustomer
}

func (s *oneBatchStore) NextCustomers(id, _ int) ([]models.CampaignCustomer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.batches[id]
	delete(s.batches, id)
	return rows, nil
}

func newProducerTestPipe(t *testing.T, m *Manager, id int, uuid string, rate int) *pipe {
	t.Helper()
	c := &models.Campaign{
		Base:          models.Base{ID: id},
		UUID:          uuid,
		Type:          models.CampaignTypeRegular,
		Messenger:     "email",
		SMTPRateLimit: rate,
		Body:          "hello",
	}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatal(err)
	}
	return &pipe{
		m:        m,
		camp:     c,
		wg:       &sync.WaitGroup{},
		done:     make(chan struct{}),
		progress: make(chan struct{}, 1),
		rate:     ratecounter.NewRateCounter(time.Minute),
	}
}

type recordingMessenger struct {
	testMessenger
	mu       sync.Mutex
	reserved []bool
}

func (m *recordingMessenger) Push(msg models.Message) error {
	m.mu.Lock()
	m.reserved = append(m.reserved, msg.CampaignRateReserved)
	m.mu.Unlock()
	return nil
}

// TestWorkerPassesCampaignRateReservationToMessenger pins the producer → worker
// hand-off of CampaignRateReserved. Losing it makes Emailer.Push pace the
// message a second time and halves the campaign's effective rate.
func TestWorkerPassesCampaignRateReservationToMessenger(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.store = &sendErrorStore{}
	msgr := &recordingMessenger{testMessenger: testMessenger{name: "custom"}}
	p := &pipe{
		m:         m,
		camp:      &models.Campaign{Base: models.Base{ID: 41}, Messenger: "custom"},
		messenger: msgr,
		wg:        &sync.WaitGroup{},
		done:      make(chan struct{}),
		rate:      ratecounter.NewRateCounter(time.Minute),
	}
	p.wg.Add(1)
	go m.worker()
	m.campMsgQ <- CampaignMessage{
		Campaign:             p.camp,
		Customer:             models.Customer{Base: models.Base{ID: 7}},
		pipe:                 p,
		CampaignRateReserved: true,
	}
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}

	msgr.mu.Lock()
	defer msgr.mu.Unlock()
	if len(msgr.reserved) != 1 || !msgr.reserved[0] {
		t.Fatalf("campaign reservation flag lost: %v", msgr.reserved)
	}
}

// TestCampaignMessageCompleteReleasesAdmission pins that every message dropped
// after admission (pause, shutdown, queue race) returns its outstanding slot.
// A leak with maxOutstanding=1 would stop the campaign permanently.
func TestCampaignMessageCompleteReleasesAdmission(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	camp := &models.Campaign{UUID: "release-check", Messenger: "email", SMTPRateLimit: 1000000}
	if err := m.deliveryLimiter.WaitCampaign(camp, 1, nil); err != nil {
		t.Fatal(err)
	}
	p := &pipe{m: m, camp: camp, wg: &sync.WaitGroup{}, done: make(chan struct{})}
	p.wg.Add(1)
	CampaignMessage{Campaign: camp, pipe: p, CampaignRateReserved: true}.complete()

	admitted := make(chan error, 1)
	go func() { admitted <- m.deliveryLimiter.WaitCampaign(camp, 1, nil) }()
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("complete() leaked the campaign's outstanding slot")
	}
	m.deliveryLimiter.ReleaseCampaign(camp)
}

// TestSlowCampaignPacingDoesNotBlockAnotherCampaign pins that each campaign
// has its own producer: a campaign waiting a full minute for its next send
// slot must not delay another campaign's batch fetch, pacing or admission.
func TestSlowCampaignPacingDoesNotBlockAnotherCampaign(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.store = &oneBatchStore{batches: map[int][]models.CampaignCustomer{
		1: {
			{Customer: models.Customer{Email: "slow-1@example.test"}},
			{Customer: models.Customer{Email: "slow-2@example.test"}},
		},
		2: {{Customer: models.Customer{Email: "fast@example.test"}}},
	}}

	// One message per minute: the second message waits ~60s for its slot.
	slow := newProducerTestPipe(t, m, 1, "slow-campaign", 1)
	slow.wg.Add(1)
	go slow.produce()
	first := <-m.campMsgQ
	if first.Campaign.UUID != "slow-campaign" || !first.CampaignRateReserved {
		t.Fatalf("slow campaign did not admit its first paced message: %+v", first)
	}

	fast := newProducerTestPipe(t, m, 2, "fast-campaign", 6000)
	fast.wg.Add(1)
	go fast.produce()
	select {
	case msg := <-m.campMsgQ:
		if msg.Campaign.UUID != "fast-campaign" || !msg.CampaignRateReserved {
			t.Fatalf("unexpected admitted message: %+v", msg)
		}
		msg.complete()
	case <-time.After(2 * time.Second):
		t.Fatal("fast campaign waited behind the slow campaign's pacing")
	}

	// The slow producer is now waiting for its second message. Pausing the
	// campaign must cancel that wait and release the producer.
	slow.Stop(stopReasonPause, false)
	first.complete()
	slowDone := make(chan struct{})
	go func() { slow.wg.Wait(); close(slowDone) }()
	select {
	case <-slowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("paused campaign retained its producer")
	}
	fastDone := make(chan struct{})
	go func() { fast.wg.Wait(); close(fastDone) }()
	select {
	case <-fastDone:
	case <-time.After(2 * time.Second):
		t.Fatal("fast campaign did not finish")
	}
}

// TestProducerBoundsOutstandingMessages pins that a campaign keeps at most
// maxOutstanding admitted messages, so a paused drain cannot later release a
// pre-admitted batch as a burst. One worker and a very high rate make the cap
// (min(burst, concurrency) = 1) the only limiting factor.
func TestProducerBoundsOutstandingMessages(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.store = &oneBatchStore{batches: map[int][]models.CampaignCustomer{
		1: {
			{Customer: models.Customer{Email: "one@example.test"}},
			{Customer: models.Customer{Email: "two@example.test"}},
			{Customer: models.Customer{Email: "three@example.test"}},
		},
	}}

	p := newProducerTestPipe(t, m, 1, "capped-campaign", 1000000)
	if got := p.maxOutstanding(); got != 1 {
		t.Fatalf("maxOutstanding = %d, want 1", got)
	}
	p.wg.Add(1)
	go p.produce()

	first := <-m.campMsgQ
	select {
	case msg := <-m.campMsgQ:
		t.Fatalf("second message admitted while the first was outstanding: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	first.complete()
	select {
	case second := <-m.campMsgQ:
		second.complete()
	case <-time.After(time.Second):
		t.Fatal("completed delivery did not admit the next message")
	}
	select {
	case third := <-m.campMsgQ:
		third.complete()
	case <-time.After(time.Second):
		t.Fatal("third message was not admitted")
	}

	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not finish")
	}
}
