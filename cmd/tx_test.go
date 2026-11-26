package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// txTestApp returns an App sufficient for validateTxMessage's limit checks: a
// real translator for the error messages, and a config so the FromEmail default
// resolves. The checks under test return before the importer or the messenger
// manager are consulted.
func txTestApp(t *testing.T) *App {
	t.Helper()
	a := probeTestApp(t)
	a.cfg = &Config{}
	return a
}

func httpCodeOf(t *testing.T, err error) int {
	t.Helper()
	var he *echo.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected an echo HTTP error, got %v", err)
	}
	return he.Code
}

func attachment(name string, size int) models.Attachment {
	return models.Attachment{Name: name, Content: bytes.Repeat([]byte("a"), size)}
}

func TestTxTemplateMediaAndUploadedFileReachSMTP(t *testing.T) {
	a, received := newAttachmentDeliveryTestApp(t)
	addMedia := func(name, mediaType string) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO media(uuid,filename,content_type,thumb,owner_user_id)
			VALUES(gen_random_uuid(),$1,$2,'',1) RETURNING id`, name, mediaType); err != nil {
			t.Fatal(err)
		}
		if _, err := a.media.Put(name, mediaType, strings.NewReader("template media")); err != nil {
			t.Fatal(err)
		}
		return id
	}
	imgID := addMedia("tx-photo.png", "image/png")
	pdfID := addMedia("tx-catalog.pdf", "application/pdf")
	tpl := models.Template{Subject: "Receipt", Body: fmt.Sprintf(`<html><body><p>Thank you</p><img src="/api/media/file/%d/tx-photo.png"></body></html>`, imgID)}
	if err := a.db.Get(&tpl.ID, `INSERT INTO templates(name,type,subject,body,owner_user_id)
		VALUES('Receipt','tx',$1,$2,1) RETURNING id`, tpl.Subject, tpl.Body); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO template_media(template_id,media_id,filename)
		VALUES($1,$2,'tx-photo.png'),($1,$3,'tx-catalog.pdf')`, tpl.ID, imgID, pdfID); err != nil {
		t.Fatal(err)
	}
	if err := tpl.Compile(a.manager.GenericTemplateFuncs()); err != nil {
		t.Fatal(err)
	}
	a.manager.CacheTpl(tpl.ID, &tpl)

	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	if err := form.WriteField("data", fmt.Sprintf(`{"template_id":%d,"customer_mode":"external",
		"customer_email":"receipt@example.test","content_type":"html"}`, tpl.ID)); err != nil {
		t.Fatal(err)
	}
	const rawContents = "one-off invoice"
	file, err := form.CreateFormFile("file", "invoice.dat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, rawContents); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tx", &payload)
	req.Header.Set(echo.HeaderContentType, form.FormDataContentType())
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(auth.UserHTTPCtxKey, superAdminUser())
	if err := a.SendTxMessage(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("transactional send: HTTP %d, %v", rec.Code, err)
	}

	go a.manager.Run()
	msg := readAttachmentDelivery(t, received)
	if msg.Header.Get("Subject") != "Receipt" {
		t.Fatalf("unexpected subject %q", msg.Header.Get("Subject"))
	}
	contentType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || contentType != "multipart/mixed" {
		t.Fatalf("one-off file did not reach MIME: %q, %v", contentType, err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	var html string
	var files []string
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if filename := part.FileName(); filename != "" {
			files = append(files, filename)
			body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
			if err != nil || string(body) != rawContents {
				t.Fatalf("one-off attachment payload changed: %v", err)
			}
		} else {
			html += resumeMediaHTML(t, part.Header, part)
		}
	}
	if len(files) != 1 || files[0] != "invoice.dat" {
		t.Fatalf("expected only the one-off MIME attachment, got %v", files)
	}
	if !strings.Contains(html, `src="https://example.test/email-media/`) || !strings.Contains(html, `/tx-photo.png"`) ||
		!strings.Contains(html, `href="https://example.test/email-media/`) || !strings.Contains(html, `/tx-catalog.pdf?download=1`) ||
		!strings.Contains(html, ">Attachments<") || strings.Contains(html, "cid:") || strings.Contains(html, "/api/media/file/") {
		t.Fatal("transactional template media did not render exclusively as recipient links")
	}
}

// TestValidateTxMessageAttachmentLimits locks the caps that keep one /api/tx
// request from buffering an unbounded amount of attachment data. The JSON path
// arrives already base64-decoded, so this is the authoritative check for it;
// the multipart path additionally rejects over-sized parts before reading them.
func TestValidateTxMessageAttachmentLimits(t *testing.T) {
	tooMany := make([]models.Attachment, 0, maxTxAttachments+1)
	for i := 0; i <= maxTxAttachments; i++ {
		tooMany = append(tooMany, attachment(fmt.Sprintf("f%d.bin", i), 1))
	}

	overTotal := make([]models.Attachment, 0, 4)
	for i := 0; i < 4; i++ {
		overTotal = append(overTotal, attachment(fmt.Sprintf("f%d.bin", i), (maxTxAttachmentsTotal/4)+1))
	}

	for _, tc := range []struct {
		name        string
		attachments []models.Attachment
		wantCode    int
	}{
		{"too many attachments", tooMany, http.StatusBadRequest},
		{"one attachment over the per-file limit", []models.Attachment{attachment("big.bin", maxTxAttachmentSize+1)}, http.StatusRequestEntityTooLarge},
		{"attachments over the total limit", overTotal, http.StatusRequestEntityTooLarge},
		{"one attachment at the per-file limit", []models.Attachment{attachment("ok.bin", maxTxAttachmentSize)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := txTestApp(t)
			_, err := a.validateTxMessage(models.TxMessage{
				CustomerIDs: []int{1},
				Attachments: tc.attachments,
			})

			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("expected the message to be accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the message to be rejected")
			}
			if got := httpCodeOf(t, err); got != tc.wantCode {
				t.Fatalf("expected HTTP %d, got %d (%v)", tc.wantCode, got, err)
			}
		})
	}
}

// TestValidateTxMessageRecipientLimit bounds the number of queued messages one
// request can create.
func TestValidateTxMessageRecipientLimit(t *testing.T) {
	a := txTestApp(t)

	emails := make([]string, maxTxRecipients+1)
	for i := range emails {
		emails[i] = fmt.Sprintf("customer%d@example.com", i)
	}
	if _, err := a.validateTxMessage(models.TxMessage{CustomerEmails: emails}); err == nil {
		t.Fatal("expected too many recipients to be rejected")
	} else if got := httpCodeOf(t, err); got != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d (%v)", got, err)
	}

	// IDs and emails count against the same budget, so a request that is under
	// the cap in each list separately is still rejected when combined.
	ids := make([]int, maxTxRecipients/2+1)
	for i := range ids {
		ids[i] = i + 1
	}
	half := make([]string, maxTxRecipients/2+1)
	for i := range half {
		half[i] = fmt.Sprintf("customer%d@example.com", i)
	}
	if _, err := a.validateTxMessage(models.TxMessage{CustomerIDs: ids, CustomerEmails: half}); err == nil {
		t.Fatal("expected the combined recipient count to be rejected")
	}
}
