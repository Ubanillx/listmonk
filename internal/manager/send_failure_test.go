package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

func TestSendFailureClassificationAndSecretRedaction(t *testing.T) {
	for _, tc := range []struct {
		err             error
		stage, category string
		code            int
	}{
		{fmt.Errorf("delivery: %w", &textproto.Error{Code: 535, Msg: "authentication failed"}), "send", "smtp_auth", 535},
		{errors.New("delivery: 550 5.1.1 recipient unknown"), "send", "smtp_rejected", 550},
		{&textproto.Error{Code: 451, Msg: "try again later"}, "send", "smtp_temporary", 451},
		{fmt.Errorf("dial: %w", context.DeadlineExceeded), "send", "timeout", 0},
		{&net.OpError{Op: "dial", Err: errors.New("connection refused")}, "send", "network", 0},
		{ErrPersonalSMTPUnavailable, "send", "smtp_unavailable", 0},
		{ErrReplyMailboxUnavailable, "send", "reply_unavailable", 0},
		{errors.New("template execute failed"), "render", "render", 0},
		{errors.New("custom delivery failed"), "send", "other", 0},
	} {
		category, code := classifySendFailure(tc.stage, tc.err)
		if category != tc.category || code != tc.code {
			t.Fatalf("%v classified as %s/%d", tc.err, category, code)
		}
	}
	got := sanitizeSendFailure(errors.New("550 bad recipient test@example.test password=private-one api_key='private-two' Authorization: Bearer private-three smtp://user:private-four@host\x00"))
	for _, secret := range []string{"private-one", "private-two", "private-three", "private-four", "\x00"} {
		if strings.Contains(got, secret) {
			t.Fatalf("diagnostic exposed %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "550 bad recipient test@example.test") {
		t.Fatalf("SMTP diagnostic lost: %s", got)
	}
}

type detailedSendErrorStore struct {
	sendErrorStore
	failures []models.CampaignSendFailure
}

func (s *detailedSendErrorStore) RecordCampaignSendFailure(f models.CampaignSendFailure) error {
	s.failures = append(s.failures, f)
	s.errors++
	return nil
}

func TestWorkerPersistsRecipientFailureSnapshotOncePerAttempt(t *testing.T) {
	for _, poolID := range []int64{0, 41} {
		t.Run(fmt.Sprint(poolID), func(t *testing.T) {
			s := &detailedSendErrorStore{}
			m := newTestManager()
			defer m.Close()
			m.store = s
			p := &pipe{m: m, camp: &models.Campaign{Base: models.Base{ID: 31}}, messenger: &failedMessenger{err: errors.New("550 5.1.1 recipient unknown")},
				wg: &sync.WaitGroup{}, done: make(chan struct{}), rate: ratecounter.NewRateCounter(time.Minute)}
			p.wg.Add(1)
			go m.worker()
			m.campMsgQ <- CampaignMessage{Campaign: p.camp, Customer: models.Customer{Base: models.Base{ID: 7}, Name: "Customer", Email: "sent@example.test", CustomerCode: "C-7"},
				PoolContactID: poolID, PoolOrganizationID: 202, pipe: p}
			done := make(chan struct{})
			go func() { p.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not drain")
			}
			if len(s.failures) != 1 || s.errors != 1 {
				t.Fatalf("detail/count diverged: %+v", s)
			}
			f := s.failures[0]
			if f.Email != "sent@example.test" || f.Name != "Customer" || f.CustomerCode != "C-7" || f.SMTPCode != 550 || f.Category != "smtp_rejected" || f.Stage != "send" {
				t.Fatalf("incorrect snapshot: %+v", f)
			}
			if poolID > 0 && (f.RecipientID != poolID || f.RecipientType != "pool" || f.RecipientOrganizationID != 202) {
				t.Fatalf("pool recipient mapped incorrectly: %+v", f)
			}
			if poolID == 0 && (f.RecipientID != 7 || f.RecipientType != "private") {
				t.Fatalf("private recipient mapped incorrectly: %+v", f)
			}
		})
	}
}
