package manager

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

type replyCaptureMessenger struct{ messages chan models.Message }

type replyGuardStore struct {
	Store
	checked chan struct{}
}

func (s *replyGuardStore) ValidateCampaignReplyRoute(int, int64, string) error {
	close(s.checked)
	return ErrReplyMailboxUnavailable
}

func (m *replyCaptureMessenger) Name() string                  { return "reply-test" }
func (m *replyCaptureMessenger) Push(msg models.Message) error { m.messages <- msg; return nil }
func (m *replyCaptureMessenger) Flush() error                  { return nil }
func (m *replyCaptureMessenger) Close() error                  { return nil }

func TestWorkerSendsPoolReplySnapshotAfterCustomHeaders(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	capture := &replyCaptureMessenger{messages: make(chan models.Message, 3)}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()
	for _, tc := range []struct {
		poolID         int64
		snapshot, want string
	}{
		{1, "imported@example.com", "imported@example.com"},
		{1, "", ""},
		{0, "", "campaign@example.com"},
	} {
		camp := &models.Campaign{Messenger: "reply-test", ContentType: models.CampaignContentTypePlain,
			ReplyMailboxEmail: "campaign@example.com", Headers: models.Headers{{"Reply-To": "custom@example.com"}}}
		if err := m.PushCampaignMessage(CampaignMessage{Campaign: camp, PoolContactID: tc.poolID,
			PoolReplyMailboxEmail: tc.snapshot, to: "customer@example.com", body: []byte("Test")}); err != nil {
			t.Fatal(err)
		}
		select {
		case msg := <-capture.messages:
			if got := msg.Headers.Get("Reply-To"); got != tc.want {
				t.Fatalf("Reply-To=%q; want %q", got, tc.want)
			}
		case <-time.After(time.Second):
			t.Fatal("worker did not send captured message")
		}
	}
}

func TestWorkerMixedAudienceReplyRoutesAndReferences(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	capture := &replyCaptureMessenger{messages: make(chan models.Message, 2)}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()
	camp := &models.Campaign{Messenger: "reply-test", UUID: "11111111-2222-3333-4444-555555555555", ReplyMailboxEmail: "changed@example.com", ContentType: models.CampaignContentTypePlain}
	for _, tc := range []struct {
		customer int
		pool     int64
		want     string
	}{{8, 0, "private@example.com"}, {0, 9, "imported@example.com"}} {
		customer := models.Customer{}
		customer.ID = tc.customer
		if err := m.PushCampaignMessage(CampaignMessage{Campaign: camp, Customer: customer, PrivateReplyTo: null.StringFrom("private@example.com"), PoolContactID: tc.pool, PoolReplyMailboxEmail: "imported@example.com", to: "customer@example.com", body: []byte("body")}); err != nil {
			t.Fatal(err)
		}
		select {
		case sent := <-capture.messages:
			if sent.Headers.Get("Reply-To") != tc.want {
				t.Fatalf("route=%q want=%q", sent.Headers.Get("Reply-To"), tc.want)
			}
			refs := models.ParseReplyReferences(sent.Headers.Get("Message-ID"))
			if len(refs) != 1 || refs[0].CustomerID != tc.customer || refs[0].PoolContactID != tc.pool {
				t.Fatalf("bad delivery reference: %+v", refs)
			}
		case <-time.After(time.Second):
			t.Fatal("worker did not send")
		}
	}
}

func TestWorkerRejectsMailboxDisabledBeforeSend(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	guard := &replyGuardStore{Store: m.store, checked: make(chan struct{})}
	m.store = guard
	capture := &replyCaptureMessenger{messages: make(chan models.Message, 1)}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()
	camp := &models.Campaign{Messenger: "reply-test", ContentType: models.CampaignContentTypePlain, ReplyMailboxEmail: "disabled@example.com"}
	if err := m.PushCampaignMessage(CampaignMessage{Campaign: camp, to: "customer@example.com", body: []byte("body")}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-guard.checked:
	case <-time.After(time.Second):
		t.Fatal("send route guard not checked")
	}
	select {
	case <-capture.messages:
		t.Fatal("message sent after mailbox became unavailable")
	case <-time.After(30 * time.Millisecond):
	}
}
