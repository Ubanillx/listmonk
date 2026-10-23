package manager

import (
	"net/url"
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
			`>PDF</td>`, `>catalog.pdf</a>`,
			`href="https://listmonk.topmax.cn/email-media/pdf-token/catalog.pdf"`} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("missing %q in %s", want, body)
			}
		}
		if !strings.Contains(string(body), "附件资料") || !strings.Contains(string(body), "1 个文件") ||
			!strings.HasSuffix(string(body), "</table></body></html>") {
			t.Fatalf("file card was not appended before the closing body: %s", body)
		}
		if !strings.Contains(string(alt), "catalog.pdf: https://") || string(media[0].Content) != "image binary" {
			t.Fatal("alternative text omitted links or cached media was mutated")
		}
	}
}

func TestLinkedMediaFileCardsKeepEachNameAndType(t *testing.T) {
	files := []struct {
		name, contentType, label, color string
	}{
		{"产品目录.pdf", "application/pdf", "PDF", "#c83d32"},
		{"报价表.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "XLS", "#217a46"},
		{"客户清单.csv", "text/csv", "CSV", "#217a46"},
		{"使用说明.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "DOC", "#245bc2"},
		{"介绍.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "PPT", "#b8511d"},
		{"素材包.zip", "application/zip", "ZIP", "#7049a6"},
		{"数据.json", "application/json", "JSON", "#53677d"},
		{"无扩展名", "application/octet-stream", "FILE", "#4756a9"},
		{"a<&b.txt", "text/plain", "TXT", "#53677d"},
	}
	attachments := make([]models.Attachment, 0, len(files))
	for i, file := range files {
		attachments = append(attachments, models.Attachment{
			Name: file.name, MediaID: i + 1, DeliveryURL: "/email-media/token/file-" + string(rune('a'+i)),
			Header: MakeAttachmentHeader(file.name, "base64", file.contentType),
		})
	}
	body, alt, remaining, err := LinkMediaAttachments([]byte(`<html><body><p>Message</p></body></html>`), []byte("Message"), attachments, "https://example.com", false)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("prepare: %v %+v", err, remaining)
	}
	htmlBody := string(body)
	if strings.Contains(htmlBody, "border-left") {
		t.Fatalf("attachment section should use type colors without vertical rules: %s", body)
	}
	if strings.Count(htmlBody, `role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%"`) != len(files)+2 ||
		strings.Count(htmlBody, "附件资料") != 1 || !strings.Contains(htmlBody, "9 个文件") {
		t.Fatalf("expected one card per document: %s", body)
	}
	for i, file := range files {
		name := file.name
		if i == len(files)-1 {
			name = "a&lt;&amp;b.txt"
		}
		nameAt := strings.Index(htmlBody, ">"+name+"</a>")
		if nameAt < 0 {
			t.Fatalf("missing actual filename %q in %s", file.name, body)
		}
		cardStart := strings.LastIndex(htmlBody[:nameAt], `<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%"`)
		cardEndOffset := strings.Index(htmlBody[nameAt:], `</tr></table>`)
		if cardStart < 0 || cardEndOffset < 0 {
			t.Fatalf("incomplete card for %q: %s", file.name, body)
		}
		cardEnd := nameAt + cardEndOffset + len(`</tr></table>`)
		card := htmlBody[cardStart:cardEnd]
		directURL := "https://example.com/email-media/token/file-" + string(rune('a'+i))
		downloadURL := directURL + "?download=1"
		if !strings.Contains(card, ">"+file.label+"</td>") || !strings.Contains(card, file.color) ||
			!strings.Contains(card, `href="`+downloadURL+`"`) || !strings.Contains(string(alt), file.name+": "+directURL) {
			t.Fatalf("missing file type, real filename, color or plain-text link for %q: %s", file.name, body)
		}
		viewURL := ""
		switch file.label {
		case "PDF":
			viewURL = directURL
		case "XLS", "DOC", "PPT":
			viewURL = "https://view.officeapps.live.com/op/view.aspx?src=" + url.QueryEscape(directURL)
		}
		if viewURL != "" {
			if strings.Count(card, `href="`+viewURL+`"`) != 2 || strings.Count(card, `href="`+downloadURL+`"`) != 1 || !strings.Contains(card, "查看&nbsp;") {
				t.Fatalf("wrong preview URL for %q: %s", file.name, card)
			}
		} else if strings.Contains(card, "查看&nbsp;") || strings.Count(card, `href="`+downloadURL+`"`) != 2 {
			t.Fatalf("non-previewable file should link only to download: %s", card)
		}
	}
	if strings.Contains(htmlBody, "a<&b.txt") {
		t.Fatalf("unescaped filename in mail HTML: %s", body)
	}
}

func TestOfficePreviewEncodesRecipientURL(t *testing.T) {
	direct := "https://example.com/email-media/token/" + url.PathEscape("产品选型表.xlsx")
	view, ok := linkedFileViewURL("产品选型表.xlsx", direct)
	if !ok {
		t.Fatal("Excel file has no Office preview")
	}
	parsed, err := url.Parse(view)
	if err != nil || parsed.Host != "view.officeapps.live.com" || parsed.Query().Get("src") != direct {
		t.Fatalf("Office viewer did not receive exact recipient URL: %q %v", view, err)
	}
	if linkedFileDownloadURL(direct) != direct+"?download=1" {
		t.Fatal("download did not keep the recipient file URL")
	}
	for _, item := range []struct{ name, badge string }{{"report.docm", "DOC"}, {"model.xlsm", "XLS"}, {"slides.pptm", "PPT"}} {
		if _, ok := linkedFileViewURL(item.name, direct); !ok {
			t.Fatalf("%s has no Office preview", item.name)
		}
		if badge, _, _ := linkedFileBadge(models.Attachment{Name: item.name}); badge != item.badge {
			t.Fatalf("%s has badge %q, want %q", item.name, badge, item.badge)
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
	_, _, _, err := LinkMediaAttachments([]byte(`<img src="/api/media/file/99/photo.png">`), nil, media, "https://example.com", false)
	if err == nil {
		t.Fatal("an unrelated exact ID must be rejected before SMTP")
	}
	media[0].DeliveryURL = ""
	if _, _, _, err := LinkMediaAttachments(nil, nil, media, "https://example.com", false); err == nil {
		t.Fatal("missing link must fail instead of falling back to MIME")
	}
}

func TestLinkedMediaRewritesBackgroundAndExistingDownload(t *testing.T) {
	body, _, files, err := LinkMediaAttachments([]byte(`<body style="background-image:url('/uploads/photo.png')"><a href="/api/media/file/8/catalog.pdf">Download</a></body>`),
		nil, linkedTestMedia(), "https://example.com", false)
	if err != nil || len(files) != 0 || !strings.Contains(string(body), "https://example.com/email-media/image-token/photo.png") ||
		!strings.Contains(string(body), `href="https://example.com/email-media/pdf-token/catalog.pdf"`) || strings.Count(string(body), "catalog.pdf") != 1 ||
		strings.Contains(string(body), "附件资料") {
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
			if len(sent.Attachments) != 0 || !strings.Contains(string(sent.Body), `src="https://listmonk.topmax.cn/email-media/`) ||
				!strings.Contains(string(sent.Body), `>PDF</td>`) || !strings.Contains(string(sent.Body), `>catalog.pdf</a>`) {
				t.Fatalf("worker sent embedded media: %+v", sent)
			}
		case <-time.After(time.Second):
			t.Fatal("message was not delivered")
		}
		p.wg.Wait()
	}
}
