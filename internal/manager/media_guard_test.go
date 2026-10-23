package manager

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
	null "gopkg.in/volatiletech/null.v6"
)

func TestEmailMediaRejectsUnresolvedSources(t *testing.T) {
	for _, body := range []string{
		`<img src="/api/media/file/95/signature.jpg">`,
		`<img alt="a > b" src='/uploads/missing.png'>`,
		`<img src="https://external.test/ok.png" srcset="/uploads/missing.png 2x">`,
		`<td background="/api/media/file/95/signature.jpg">`,
		`<div style="background-image:url('/uploads/missing.png')">`,
		`<!--[if mso]><v:fill src="/uploads/missing.png"><![endif]-->`,
		`<img src="cid:media-95@listmonk">`,
		`<img src="blob:https://mail.test/example">`,
		`<img src="data:image/png;base64,aGVsbG8=">`,
		`<img src="signature.jpg">`,
	} {
		t.Run(body, func(t *testing.T) {
			_, _, _, err := LinkMediaAttachments([]byte(body), nil, nil, "https://mail.test", false)
			if err == nil {
				t.Fatal("unresolved image was allowed to reach email delivery")
			}
		})
	}
}

type mediaPreflightStore struct {
	Store
	failed bool
}

func (s *mediaPreflightStore) MarkCampaignStartFailure(int, string) error {
	s.failed = true
	return nil
}

func TestSchedulerRejectsMissingTemplateImageBeforeCreatingPipe(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.cfg.RootURL = "https://mail.test"
	s := &mediaPreflightStore{}
	m.store = s
	camp := &models.Campaign{Messenger: "email", ContentType: models.CampaignContentTypeHTML,
		PoolScope:     models.CampaignPoolScopeAllOrganizations,
		ResourceScope: models.ResourceScope{OwnerUserID: null.Int{Int: 17, Valid: true}},
		Body:          "Hello", TemplateBody: `<html>{{ template "content" . }}<img src="/api/media/file/95/signature.jpg"></html>`,
		// A preloaded unrelated attachment must not bypass the body guard.
		Attachments: linkedTestMedia()}
	camp.ID = 110
	if _, err := m.newPipe(camp); !errors.Is(err, ErrUnresolvedMedia) {
		t.Fatalf("scheduler allowed missing template image: %v", err)
	}
	if !s.failed || m.HasRunningCampaigns() {
		t.Fatalf("scheduler created an active pipe or did not persist failure: failed=%t", s.failed)
	}
}

func TestLinkMediaIncludesBackgroundSrcsetAndOutlookSources(t *testing.T) {
	body := `<td background="/uploads/photo.png" style="background:url('/uploads/photo.png')"><img srcset="/uploads/photo.png 1x, /api/media/file/7/photo.png 2x"><!--[if mso]><v:fill src="/uploads/photo.png"><![endif]-->`
	out, _, _, err := LinkMediaAttachments([]byte(body), nil, linkedTestMedia(), "https://mail.test", false)
	if err != nil || strings.Contains(string(out), "/uploads/") || strings.Contains(string(out), "/api/media/file/") {
		t.Fatalf("background, srcset or Outlook image was left protected: %s %v", out, err)
	}
}

type mediaGuardStore struct {
	Store
	failure models.CampaignSendFailure
	pending bool
}

func (s *mediaGuardStore) RecordCampaignSendFailure(f models.CampaignSendFailure) error {
	s.failure = f
	return nil
}
func (s *mediaGuardStore) MarkCampaignRecipientStatus(_, _ int, status string) error {
	s.pending = status == models.CampaignRecipientStatusPending
	return nil
}
func (*mediaGuardStore) MarkCampaignMessageSent(_, _ int) error { return nil }

func TestWorkerBlocksUnassociatedImageAndPreservesRecipient(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.cfg.RootURL = "https://mail.test"
	s := &mediaGuardStore{}
	m.store = s
	capture := &linkedCaptureMessenger{replyCaptureMessenger{messages: make(chan models.Message, 1)}}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()
	camp := &models.Campaign{Messenger: "email", ContentType: models.CampaignContentTypeHTML}
	camp.ID = 110
	p := &pipe{camp: camp, messenger: capture, wg: &sync.WaitGroup{}, rate: ratecounter.NewRateCounter(time.Second), done: make(chan struct{}), m: m}
	p.wg.Add(1)
	if err := m.PushCampaignMessage(CampaignMessage{Campaign: camp, pipe: p, body: []byte(`<img src="/api/media/file/95/signature.jpg">`)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish validating the message")
	}
	select {
	case <-capture.messages:
		t.Fatal("actual email worker delivered an unresolved internal image")
	default:
	}
	if !s.pending || !p.stopped.Load() || s.failure.Category != "media" || !strings.Contains(s.failure.Error, "95") {
		t.Fatalf("failure must pause, preserve pending recipient and report media ID: pending=%t stopped=%t failure=%+v", s.pending, p.stopped.Load(), s.failure)
	}
	if _, _, _, err := LinkMediaAttachments([]byte(`<img src="/uploads/missing.png">`), nil, nil, "https://mail.test", false); !errors.Is(err, ErrUnresolvedMedia) {
		t.Fatalf("expected a typed media error: %v", err)
	}
}
