package manager

import (
	"net/url"
	"slices"
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
			[]byte("Hello"), media, "https://listmonk.topmax.cn", false, "zh-CN")
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

func TestLinkedMediaPreviewUsesLibraryLinksWithoutRecipientValidation(t *testing.T) {
	// A preview has no per-recipient delivery links; the library URL stands in
	// for them, so the resulting protected references are expected and must not
	// fail the send-time recipient check. Links stay site-relative so the admin
	// UI can inline them even when app.root_url names another origin.
	media := linkedTestMedia()
	for i := range media {
		media[i].DeliveryURL = media[i].SourceURL
	}
	msg := CampaignMessage{
		Campaign: &models.Campaign{AttachmentLang: "zh-CN", Attachments: media},
		body:     []byte(`<html><body><img src="/api/media/file/7/photo.png"><img src="https://example.com/api/media/file/7/photo.png"><img src="/api/media/file/99/unselected.png"></body></html>`),
		altBody:  []byte("Hello"),
	}
	body, alt, remaining, err := msg.linkAttachments("https://example.com", true)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("preview linking: %v %+v", err, remaining)
	}
	for _, want := range []string{
		`src="/api/media/file/7/photo.png"><img src="/api/media/file/7/photo.png"`,
		`src="/api/media/file/99/unselected.png"`,
		`href="/api/media/file/8/catalog.pdf"`,
		`href="/api/media/file/8/catalog.pdf?download=1"`,
		">catalog.pdf</a>",
		"附件资料",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %q in preview body: %s", want, body)
		}
	}
	if strings.Contains(string(body), `https://example.com/api/media/file/`) {
		t.Fatalf("preview links must stay site-relative: %s", body)
	}
	if !strings.Contains(string(alt), "catalog.pdf: /api/media/file/8/catalog.pdf") {
		t.Fatalf("preview alt body lost the file link: %s", alt)
	}
}

func TestLinkedMediaAttachmentCoverKeepsFileLinks(t *testing.T) {
	files := []struct {
		name, contentType, label string
	}{
		{"产品目录.pdf", "application/pdf", "PDF"},
		{"报价表.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "XLSX"},
		{"客户清单.csv", "text/csv", "CSV"},
		{"使用说明.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "DOC"},
		{"介绍.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "PPT"},
		{"素材包.zip", "application/zip", "ZIP"},
		{"数据.json", "application/json", "JSON"},
		{"无扩展名", "application/octet-stream", "FILE"},
		{"a<&b.txt", "text/plain", "TXT"},
	}
	attachments := make([]models.Attachment, 0, len(files))
	for i, file := range files {
		attachment := models.Attachment{
			Name: file.name, MediaID: i + 1, DeliveryURL: "/email-media/token/file-" + string(rune('a'+i)),
			Header: MakeAttachmentHeader(file.name, "base64", file.contentType),
		}
		if got := linkedFileType(attachment); got != file.label {
			t.Fatalf("type for %q = %q, want %q", file.name, got, file.label)
		}
		attachments = append(attachments, attachment)
	}
	slices.Reverse(attachments)
	body, alt, remaining, err := LinkMediaAttachments([]byte(`<html><body><p>Message</p></body></html>`), []byte("Message"), attachments, "https://example.com", false, "zh-CN")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("prepare: %v %+v", err, remaining)
	}
	if attachments[0].Name != files[len(files)-1].name {
		t.Fatal("rendering reordered the caller's media")
	}
	htmlBody := string(body)
	rows := strings.Split(htmlBody, `<tr class="listmonk-attachment-file">`)
	if len(rows) != len(files) || strings.Count(htmlBody, `class="listmonk-attachment-cover"`) != 1 ||
		!strings.Contains(rows[0], ">PDF</td>") || !strings.Contains(rows[0], "9 个文件") ||
		strings.Contains(htmlBody, "查看或下载随附的文件") {
		t.Fatalf("expected one cover and compact rows without duplicate copy: %s", body)
	}
	for i, file := range files {
		name := file.name
		if i == len(files)-1 {
			name = "a&lt;&amp;b.txt"
		}
		row := rows[i]
		directURL := "https://example.com/email-media/token/file-" + string(rune('a'+i))
		downloadURL := directURL + "?download=1"
		if !strings.Contains(row, ">"+name+"</a>") || !strings.Contains(row, `href="`+downloadURL+`"`) ||
			!strings.Contains(string(alt), file.name+": "+directURL) {
			t.Fatalf("missing escaped name or link for %q: %s", file.name, row)
		}
		viewURL := ""
		switch file.label {
		case "PDF":
			viewURL = directURL
		case "XLSX", "DOC", "PPT":
			viewURL = "https://view.officeapps.live.com/op/view.aspx?src=" + url.QueryEscape(directURL)
		}
		if viewURL != "" {
			if strings.Count(row, `href="`+viewURL+`"`) != 2 || strings.Count(row, `href="`+downloadURL+`"`) != 1 || !strings.Contains(row, ">查看</a>") {
				t.Fatalf("wrong preview URL for %q: %s", file.name, row)
			}
		} else if strings.Contains(row, ">查看</a>") || strings.Count(row, `href="`+downloadURL+`"`) != 2 {
			t.Fatalf("non-previewable file should link only to download: %s", row)
		}
	}
}

func TestLinkedMediaCoverUsesFirstUnreferencedFile(t *testing.T) {
	attachments := append(linkedTestMedia(), models.Attachment{
		Name: "assets.zip", MediaID: 9, SourceURL: "/api/media/file/9/assets.zip", DeliveryURL: "/email-media/zip-token/assets.zip",
	})
	body, _, _, err := LinkMediaAttachments([]byte(`<a href="/api/media/file/8/catalog.pdf">Catalogue</a>`), nil,
		attachments, "https://example.com", false, "en")
	if err != nil {
		t.Fatal(err)
	}
	section := string(body)
	if !strings.Contains(section, ">ZIP</td>") || !strings.Contains(section, ">1 file</td>") ||
		!strings.Contains(section, ">assets.zip</a>") || strings.Contains(section, ">View</a>") ||
		strings.Contains(section, `class="listmonk-attachment-file"`) || strings.Count(section, "catalog.pdf") != 1 {
		t.Fatalf("image or authored link became the cover: %s", section)
	}
}

func TestStandaloneEmailContentEnd(t *testing.T) {
	for name, marked := range map[string]string{
		"semantic main":                `<html><body><main><p>Message</p>|</main><footer>Footer</footer></body></html>`,
		"footer in the column":         `<html><body><main><p>Message|<footer>Footer</footer></main></body></html>`,
		"footer at body level":         `<html><body><p>Message</p>|<div role="contentinfo">Footer</div></body></html>`,
		"visual footer":                `<html><body><div><table align="center" role="presentation"><tr><td><div>Message</div>|<div><p><a href="https://example.test/unsubscribe">Unsubscribe</a></p></div></td></tr></table></div></body></html>`,
		"visual without footer":        `<html><body><table align="center" role="presentation"><tr><td><div>Message</div>|</td></tr></table></body></html>`,
		"visual implicit cell end":     `<html><body><table align="center" role="presentation"><tr><td>Message|</tr></table></body></html>`,
		"main cell followed by footer": `<html><body><table><tr><td role="main">Message|<td><footer>Footer</footer></td></tr></table></body></html>`,
		"earlier unsubscription":       `<html><body><table align="center" role="presentation"><tr><td><div><a href="https://example.test/unsubscribe">Unsubscribe</a></div><div>More content</div>|</td></tr></table></body></html>`,
		"template and raw text":        `<html><body><template></body></template><script>const tag = "</body>";</script>|</BODY ><!-- </body> --></html>`,
		"standalone without column":    `<html><body><p>Message</p>|</body></html>`,
		"missing body close":           `<html><body><p>Message</p>|</html>`,
		"fragment":                     `<p>Message</p>|`,
	} {
		t.Run(name, func(t *testing.T) {
			prefix, suffix, _ := strings.Cut(marked, "|")
			if got := renderedAttachmentOffset([]byte(prefix+suffix), attachmentSlotPrefix+"test", "https://example.test/unsubscribe"); got != len(prefix) {
				t.Fatalf("insert offset = %d, want %d: %s", got, len(prefix), marked)
			}
		})
	}
}

func TestCampaignMediaRendersAtTheContentSlot(t *testing.T) {
	m := newTestManager()
	m.cfg.UnsubURL = "https://example.test/unsub/%s/%s"
	defer m.Close()
	for _, tc := range []struct{ name, kind, wrapper, content, suffix string }{
		{"custom wrapped", models.CampaignContentTypeHTML,
			`<html><body><header>Brand</header><table><tr><td id="content">{{ template "content" . }}</td></tr></table><footer>Footer</footer></body></html>`,
			`<p>Rendered content</p>`, `</table></td>`},
		{"repeated content", models.CampaignContentTypeMarkdown,
			`<html><body><aside>{{ template "content" . }}</aside><main>{{ template "content" . }}</main><footer>{{ template "content" . }}</footer></body></html>`,
			`Rendered content`, `</table></main>`},
		{"standalone visual", models.CampaignContentTypeVisual, "",
			`<html><body><div><table role="presentation" align="center"><tr><td><p>Rendered content</p><div><a href="{{ UnsubscribeURL }}">Unsubscribe</a></div></td></tr></table></div></body></html>`,
			`</table><div><a href=`},
		{"standalone html", models.CampaignContentTypeHTML, "",
			`<html><body><main><p>Rendered content</p></main><footer>Footer</footer></body></html>`, `</table></main>`},
		{"standalone nested main", models.CampaignContentTypeHTML, "",
			`<html><body><div><main hidden>Hidden content</main><main><p>Rendered content</p></main><footer>Footer</footer></div></body></html>`, `</table></main>`},
		{"hidden only slot", models.CampaignContentTypeHTML,
			`<html><body><div style="display:none">{{ template "content" . }}</div><main><p>Visible letter</p></main><footer>Footer</footer></body></html>`,
			`<p>Rendered content</p>`, `</table></main>`},
		{"table row only slot", models.CampaignContentTypeHTML,
			`<html><body><main><table><tbody>{{ template "content" . }}</tbody></table></main><footer>Footer</footer></body></html>`,
			`<tr><td>Rendered content</td></tr>`, `</table></main>`},
		{"table row with implicit cell close", models.CampaignContentTypeHTML,
			`<main><table><tr>{{ template "content" . }}</tr></table></main><footer>Footer</footer>`,
			`<td>First cell<td>Rendered content</td>`, `</table></main>`},
		{"wrapped title and attribute", models.CampaignContentTypeHTML,
			`<html><head><title>{{ template "content" . }}</title></head><body><main title="{{ template "content" . }}">{{ template "content" . }}</main><footer>Footer</footer></body></html>`,
			`Rendered content`, `</table></main>`},
		{"conditional main", models.CampaignContentTypeHTML, "",
			`<html><body>{{ if .Customer.Email }}<main><p>Greeting</p></main>{{ else }}<main><p>Rendered content</p></main>{{ end }}<footer>Footer</footer></body></html>`, `</table></main>`},
		{"nested footer in main", models.CampaignContentTypeHTML, "",
			`<html><body><main><div><p>Rendered content</p><footer>Footer</footer></div></main></body></html>`, `</table><footer>`},
		{"wrapped hidden repeat", models.CampaignContentTypeHTML,
			`<html><body><div style="display:none">{{ template "content" . }}</div><div>{{ template "content" . }}</div><footer>Footer</footer></body></html>`,
			`<p>Rendered content</p>`, `</table></div><footer>`},
		{"visual template declaration", models.CampaignContentTypeVisual, "",
			`{{$unused := .}}<html><body><table align="center" role="presentation"><tr><td>Rendered content</tr></table></body></html>`, `</table></tr>`},
		{"standalone template expression", models.CampaignContentTypeHTML, "",
			`<html><body><main>{{ printf "<footer>Literal</footer>" }}</main></body></html>`, `</table></main>`},
		{"wrapped named content", models.CampaignContentTypeHTML,
			`<main>{{ template "content" . }}</main><footer>Footer</footer>`,
			`{{ define "content" }}<p>Rendered content</p>{{ end }}`, `</table></main>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attachments := linkedTestMedia()
			for i := range attachments {
				attachments[i].DeliveryURL = attachments[i].SourceURL
			}
			c := &models.Campaign{Messenger: "email", ContentType: tc.kind, Body: tc.content, TemplateBody: tc.wrapper, Attachments: attachments}
			if err := m.CompileCampaignTemplate(c); err != nil {
				t.Fatal(err)
			}
			msg, err := m.NewCampaignMessage(c, models.Customer{})
			if err != nil {
				t.Fatal(err)
			}
			if c.AttachmentSlot == "" || strings.Contains(string(msg.Body()), c.AttachmentSlot) || strings.Count(string(msg.body), c.AttachmentSlot) != 1 {
				t.Fatalf("attachment slot escaped the internal rendering boundary: %s", msg.Body())
			}
			if tc.name == "wrapped title and attribute" &&
				!strings.Contains(string(msg.Body()), `<title>Rendered content</title></head><body><main title="Rendered content">`) {
				t.Fatalf("attachment preparation changed the title or attribute: %s", msg.Body())
			}
			body, err := msg.PreviewBodyWithAttachments("")
			if err != nil {
				t.Fatal(err)
			}
			if htmlBody := string(body); strings.Count(htmlBody, `class="listmonk-attachment-cover"`) != 1 ||
				!strings.Contains(htmlBody, tc.suffix) || strings.Contains(htmlBody, c.AttachmentSlot) {
				t.Fatalf("attachment section was not appended once in the content column: %s", body)
			}
		})
	}
}

func TestCampaignAttachmentSlotLeavesAuthoredMarkers(t *testing.T) {
	m := newTestManager()
	defer m.Close()
	const authored = `<div>Text<wbr data-listmonk-attachment-tail="authored"></div>`
	c := &models.Campaign{Messenger: "email", ContentType: models.CampaignContentTypeHTML, Body: authored}
	if err := m.CompileCampaignTemplate(c); err != nil {
		t.Fatal(err)
	}
	msg, err := m.NewCampaignMessage(c, models.Customer{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(msg.Body()); got != authored {
		t.Fatalf("body without media changed: %q", got)
	}
	body, _, _, err := msg.linkAttachments("https://example.com", false)
	if err != nil || string(body) != authored {
		t.Fatalf("delivery without media changed: %v, %q", err, body)
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
		if badge := linkedFileType(models.Attachment{Name: item.name}); badge != item.badge {
			t.Fatalf("%s has badge %q, want %q", item.name, badge, item.badge)
		}
	}
}

func TestLinkedMediaPlainAndExplicitImage(t *testing.T) {
	for _, plain := range []bool{false, true} {
		body, _, files, err := LinkMediaAttachments([]byte("Hello"), nil, linkedTestMedia(), "https://example.com", plain, "zh-CN")
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
	_, _, _, err := LinkMediaAttachments([]byte(`<img src="/api/media/file/99/photo.png">`), nil, media, "https://example.com", false, "zh-CN")
	if err == nil {
		t.Fatal("an unrelated exact ID must be rejected before SMTP")
	}
	media[0].DeliveryURL = ""
	if _, _, _, err := LinkMediaAttachments(nil, nil, media, "https://example.com", false, "zh-CN"); err == nil {
		t.Fatal("missing link must fail instead of falling back to MIME")
	}
}

func TestLinkedMediaRewritesBackgroundAndExistingDownload(t *testing.T) {
	body, _, files, err := LinkMediaAttachments([]byte(`<body style="background-image:url('/uploads/photo.png')"><a href="/api/media/file/8/catalog.pdf">Download</a></body>`),
		nil, linkedTestMedia(), "https://example.com", false, "zh-CN")
	if err != nil || len(files) != 0 || !strings.Contains(string(body), "https://example.com/email-media/image-token/photo.png") ||
		!strings.Contains(string(body), `href="https://example.com/email-media/pdf-token/catalog.pdf"`) || strings.Count(string(body), "catalog.pdf") != 1 ||
		strings.Contains(string(body), "附件资料") {
		t.Fatalf("background/download replacement failed: %s %v", body, err)
	}
}

func TestLinkedMediaPreservesSystemAttachments(t *testing.T) {
	raw := []models.Attachment{{Name: "data.json", Content: []byte(`{"customer":"example"}`)}}
	_, _, files, err := LinkMediaAttachments(nil, nil, raw, "", false, "")
	if err != nil || len(files) != 1 || string(files[0].Content) != string(raw[0].Content) {
		t.Fatal("system data export attachment changed")
	}
}

func TestLinkedImageDownloadAlsoDisplaysInBody(t *testing.T) {
	body, _, _, err := LinkMediaAttachments([]byte(`<a href="/api/media/file/7/photo.png">Download photo</a>`),
		nil, linkedTestMedia()[:1], "https://example.com", false, "zh-CN")
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
