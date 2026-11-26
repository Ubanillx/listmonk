package manager

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/paulbellamy/ratecounter"
)

func TestNormalizeAttachmentLang(t *testing.T) {
	for raw, want := range map[string]string{
		"":        "en",
		"en":      "en",
		"en-US":   "en",
		"zh":      "zh-CN",
		"zh_CN":   "zh-CN",
		"zh-Hans": "zh-CN",
		"zh-HK":   "zh-TW",
		"zh-TW":   "zh-TW",
		"pt_BR":   "pt",
		"ja-JP":   "ja",
	} {
		if got, ok := NormalizeAttachmentLang(raw); !ok || got != want {
			t.Fatalf("NormalizeAttachmentLang(%q) = %q, %t, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"xx", "klingon", "auto", "auto-xx", "en--", "en--US", "de-?", "zh-HK-"} {
		if _, ok := NormalizeAttachmentLang(raw); ok {
			t.Fatalf("unsupported language %q was accepted", raw)
		}
	}
}

func TestAttachmentLangCatalogMatchesLabelsAndSchema(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schema), "attachment_lang TEXT NOT NULL DEFAULT 'en'") {
		t.Fatal("schema.sql does not default the attachment language to English")
	}
	if len(SupportedAttachmentLangs) != len(attachmentLangLabels) {
		t.Fatalf("%d supported languages but %d label sets", len(SupportedAttachmentLangs), len(attachmentLangLabels))
	}
	for _, lang := range SupportedAttachmentLangs {
		if got, ok := NormalizeAttachmentLang(lang); !ok || got != lang {
			t.Fatalf("supported language %q does not normalize to itself", lang)
		}
		if !strings.Contains(string(schema), "'"+lang+"'") {
			t.Fatalf("schema.sql check constraint is missing %q", lang)
		}
		labels := attachmentLangLabels[lang]
		if labels.title == "" || labels.view == "" || labels.download == "" || labels.count == nil {
			t.Fatalf("language %q has incomplete labels: %+v", lang, labels)
		}
		if one, many := labels.count(1), labels.count(2); one == "" || !strings.Contains(many, "2") {
			t.Fatalf("language %q counts = %q / %q", lang, one, many)
		}
	}
	if got := attachmentLabelsFor("xx"); got.title != attachmentLangLabels["en"].title {
		t.Fatalf("unknown language falls back to %q, want English", got.title)
	}
	// The plural strings embed %d that pluralCount replaces; guard against a
	// malformed format slipping through.
	for _, n := range []int{0, 1, 2, 11, 21} {
		for lang, labels := range attachmentLangLabels {
			if strings.Contains(labels.count(n), "%") {
				t.Fatalf("%s count(%d) left a format verb: %q", lang, n, labels.count(n))
			}
			if !strings.Contains(labels.count(n), strconv.Itoa(n)) {
				t.Fatalf("%s count(%d) = %q", lang, n, labels.count(n))
			}
		}
	}
}

func TestLinkedMediaRendersAttachmentSectionPerLanguage(t *testing.T) {
	for _, lang := range SupportedAttachmentLangs {
		labels := attachmentLangLabels[lang]
		body, _, _, err := LinkMediaAttachments([]byte(`<p>Message</p>`), nil, linkedTestMedia(), "https://example.com", false, lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, needle := range []string{`lang="` + lang + `"`, ">" + labels.title + "</h3>", ">" + labels.count(1) + "</td>",
			">" + labels.view + "</a>", ">" + labels.download + "</a>"} {
			if !strings.Contains(string(body), needle) {
				t.Fatalf("%s: missing %q in %s", lang, needle, body)
			}
		}
	}

	// Unknown languages fall back to English instead of failing delivery.
	body, _, _, err := LinkMediaAttachments([]byte(`<p>Message</p>`), nil, linkedTestMedia(), "https://example.com", false, "xx")
	if err != nil || !strings.Contains(string(body), "Attachments") {
		t.Fatalf("unknown language did not fall back to English: %v %s", err, body)
	}
}

func TestLinkedMediaRussianPluralCount(t *testing.T) {
	makeAttachments := func(n int) []models.Attachment {
		attachments := make([]models.Attachment, 0, n)
		for i := 0; i < n; i++ {
			attachments = append(attachments, models.Attachment{
				Name: "file-" + string(rune('a'+i)) + ".pdf", MediaID: i + 1,
				DeliveryURL: "/email-media/token/file",
				Header:      MakeAttachmentHeader("file.pdf", "base64", "application/pdf"),
			})
		}
		return attachments
	}
	for n, want := range map[int]string{
		1:  "1 файл",
		2:  "2 файла",
		5:  "5 файлов",
		11: "11 файлов",
		21: "21 файл",
	} {
		body, _, _, err := LinkMediaAttachments([]byte(`<p>Message</p>`), nil, makeAttachments(n), "https://example.com", false, "ru")
		if err != nil || !strings.Contains(string(body), want) {
			t.Fatalf("%d attachments: want %q, err %v: %s", n, want, err, body)
		}
	}
}

func TestWorkerRendersAttachmentSectionInCampaignLanguage(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	m.cfg.RootURL = "https://example.com"
	m.store = &linkedSentStore{}
	capture := &linkedCaptureMessenger{replyCaptureMessenger{messages: make(chan models.Message, 1)}}
	if err := m.AddMessenger(capture); err != nil {
		t.Fatal(err)
	}
	go m.worker()

	camp := &models.Campaign{
		Messenger: "email", ContentType: models.CampaignContentTypeHTML,
		AttachmentLang: "de", Attachments: linkedTestMedia(),
	}
	p := &pipe{messenger: capture, wg: &sync.WaitGroup{}, rate: ratecounter.NewRateCounter(time.Second), done: make(chan struct{})}
	p.wg.Add(1)
	if err := m.PushCampaignMessage(CampaignMessage{
		Campaign: camp, pipe: p, body: []byte(`<p>Hello</p>`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case sent := <-capture.messages:
		if !strings.Contains(string(sent.Body), "Anhänge") || !strings.Contains(string(sent.Body), "1 Datei") {
			t.Fatalf("worker ignored the campaign attachment language: %s", sent.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("message was not delivered")
	}
	p.wg.Wait()
}
