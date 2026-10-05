package manager

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

type replyCaptureMessenger struct{ messages chan models.Message }

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
