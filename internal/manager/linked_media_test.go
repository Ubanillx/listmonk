package manager

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

func linkedTestMedia() []models.Attachment {
	return []models.Attachment{
		{Name: "photo.png", MediaID: 7, SourceURL: "/api/media/file/7/photo.png", DeliveryURL: "/email-media/image-token/photo.png",
			Header: MakeAttachmentHeader("photo.png", "base64", "image/png"), Content: []byte("image binary")},
		{Name: "catalog.pdf", MediaID: 8, SourceURL: "/api/media/file/8/catalog.pdf", DeliveryURL: "/email-media/pdf-token/catalog.pdf",
			Header: MakeAttachmentHeader("catalog.pdf", "base64", "application/pdf"), Content: []byte("pdf binary")},
	}
}

func TestLinkedMediaDisplaysRemoteImagesAndFileLinks(t *testing.T) {
	media := linkedTestMedia()
	for _, source := range []string{
		`"/api/media/file/7/photo.png?organization_id=1"`,
		`'https://listmonk.topmax.cn/api/media/file/7/photo.png'`,
		`/uploads/photo.png`, `"cid:media-7@listmonk"`,
	} {
		body, alt, files, err := LinkMediaAttachments([]byte(`<html><body><img width="300" src=`+source+`><img src="https://external.example/uploads/photo.png"></body></html>`),
			[]byte("Hello"), media, "https://listmonk.topmax.cn", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 0 || strings.Contains(string(body), "cid:") || strings.Contains(string(body), "image binary") {
			t.Fatalf("binary attachment survived: %s %+v", body, files)
		}
		for _, want := range []string{`width="300" src="https://listmonk.topmax.cn/email-media/image-token/photo.png"`,
			`src="https://external.example/uploads/photo.png"`,
			`<a href="https://listmonk.topmax.cn/email-media/pdf-token/catalog.pdf">catalog.pdf</a></p></body>`} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("missing %q in %s", want, body)
			}
		}
		if !strings.Contains(string(alt), "catalog.pdf: https://") || string(media[0].Content) != "image binary" {
			t.Fatal("alternative text omitted links or cached media was mutated")
		}
	}
}

func TestLinkedMediaPlainAndExplicitImage(t *testing.T) {
	for _, plain := range []bool{false, true} {
		body, _, files, err := LinkMediaAttachments([]byte("Hello"), nil, linkedTestMedia(), "https://example.com", plain)
		if err != nil || len(files) != 0 {
			t.Fatalf("prepare: %v", err)
		}
		if !strings.Contains(string(body), "https://example.com/email-media/image-token/photo.png") ||
			(plain && strings.Contains(string(body), "<img")) || (!plain && !strings.Contains(string(body), "<img")) {
			t.Fatalf("incorrect image representation: %s", body)
		}
	}
}

func TestLinkedMediaCannotFallbackToWrongClone(t *testing.T) {
	media := linkedTestMedia()
	body, _, _, err := LinkMediaAttachments([]byte(`<img src="/api/media/file/99/photo.png">`), nil, media, "https://example.com", false)
	if err != nil || !strings.Contains(string(body), `src="/api/media/file/99/photo.png"`) {
		t.Fatalf("rewrote an unrelated exact ID: %s %v", body, err)
	}
	media[0].DeliveryURL = ""
	if _, _, _, err := LinkMediaAttachments(nil, nil, media, "https://example.com", false); err == nil {
		t.Fatal("missing link must fail instead of falling back to MIME")
	}
}

func TestLinkedMediaRewritesBackgroundAndExistingDownload(t *testing.T) {
	body, _, files, err := LinkMediaAttachments([]byte(`<body style="background-image:url('/uploads/photo.png')"><a href="/api/media/file/8/catalog.pdf">Download</a></body>`),
		nil, linkedTestMedia(), "https://example.com", false)
	if err != nil || len(files) != 0 || !strings.Contains(string(body), "url(https://example.com/email-media/image-token/photo.png)") ||
		!strings.Contains(string(body), `href="https://example.com/email-media/pdf-token/catalog.pdf"`) || strings.Count(string(body), "catalog.pdf") != 1 {
		t.Fatalf("background/download replacement failed: %s %v", body, err)
	}
}

func TestLinkedMediaPreservesSystemAttachments(t *testing.T) {
	raw := []models.Attachment{{Name: "data.json", Content: []byte(`{"customer":"example"}`)}}
	_, _, files, err := LinkMediaAttachments(nil, nil, raw, "", false)
	if err != nil || len(files) != 1 || string(files[0].Content) != string(raw[0].Content) {
		t.Fatal("system data export attachment changed")
	}
}

func TestLinkedImageDownloadAlsoDisplaysInBody(t *testing.T) {
	body, _, _, err := LinkMediaAttachments([]byte(`<a href="/api/media/file/7/photo.png">Download photo</a>`),
		nil, linkedTestMedia()[:1], "https://example.com", false)
	if err != nil || !strings.Contains(string(body), `<img src="https://example.com/email-media/image-token/photo.png"`) {
		t.Fatalf("image only appeared as a clickable link: %s %v", body, err)
	}
}

type linkedCaptureMessenger struct{ replyCaptureMessenger }

func (m *linkedCaptureMessenger) Name() string { return "email" }

type linkedSentStore struct{ Store }

func (s *linkedSentStore) MarkCampaignMessageSent(int, int) error { return nil }

func TestWorkerLinksMediaForStartedAndResumedCampaign(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.cfg.RootURL = "https://listmonk.topmax.cn"
	m.store = &linkedSentStore{}
	capture := &linkedCaptureMessenger{replyCaptureMessenger{messages: make(chan models.Message, 2)}}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()
	// A resumed campaign is loaded as a fresh campaign snapshot. Both snapshots
	// pass through the same worker, even if the saved HTML uses a legacy URL.
	for _, source := range []string{"/api/media/file/7/photo.png", "/uploads/photo.png"} {
		camp := &models.Campaign{Messenger: "email", ContentType: models.CampaignContentTypeHTML, Attachments: linkedTestMedia()}
		// Use the pipe's already resolved messenger to exercise the scheduled
		// campaign route without an external SMTP account.
		p := &pipe{messenger: capture, wg: &sync.WaitGroup{}, rate: ratecounter.NewRateCounter(time.Second), done: make(chan struct{})}
		p.wg.Add(1)
		if err := m.PushCampaignMessage(CampaignMessage{Campaign: camp, pipe: p, body: []byte(`<img src="` + source + `">`)}); err != nil {
			t.Fatal(err)
		}
		select {
		case sent := <-capture.messages:
			if len(sent.Attachments) != 0 || !strings.Contains(string(sent.Body), `src="https://listmonk.topmax.cn/email-media/`) {
				t.Fatalf("worker sent embedded media: %+v", sent)
			}
		case <-time.After(time.Second):
			t.Fatal("message was not delivered")
		}
		p.wg.Wait()
	}
}
