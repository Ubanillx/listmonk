package email

import (
	"bytes"
	"net/textproto"
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/knadh/smtppool/v2"
)

func TestToSMTPAttachmentsUsesRelatedPartsForInlineImages(t *testing.T) {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", "image/png")
	header.Set("Content-Disposition", "inline")
	header.Set("Content-ID", "<media-7@listmonk>")
	header.Set("Content-Transfer-Encoding", "base64")

	attachments := toSMTPAttachments([]models.Attachment{{
		Name:    "logo.png",
		Header:  header,
		Content: []byte("png bytes"),
		Inline:  true,
	}})
	if len(attachments) != 1 || !attachments[0].HTMLRelated {
		t.Fatal("inline images must be sent as HTML-related MIME parts")
	}

	raw, err := (&smtppool.Email{
		From:        "from@example.com",
		To:          []string{"to@example.com"},
		Text:        relatedAttachmentsTextBody(nil, attachments),
		HTML:        []byte(`<img src="cid:media-7@listmonk">`),
		Attachments: attachments,
	}).Bytes()
	if err != nil {
		t.Fatalf("render MIME e-mail: %v", err)
	}

	for _, expected := range [][]byte{
		[]byte("multipart/alternative"),
		[]byte("multipart/related"),
		[]byte("Content-Id: <media-7@listmonk>"),
		[]byte("Content-Disposition: inline"),
	} {
		if !bytes.Contains(raw, expected) {
			t.Fatalf("expected MIME output to contain %q, got %q", expected, raw)
		}
	}
	if bytes.Contains(raw, []byte("multipart/mixed")) {
		t.Fatalf("inline images must not be sent as regular attachments: %q", raw)
	}
}
