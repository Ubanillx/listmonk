package manager

import (
	"mime"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestInlineMediaImages(t *testing.T) {
	const (
		mediaURL = "http://intranet:9173/uploads/table.jpg?size=large&mode=cover"
		cid      = "media-42@listmonk"
	)

	image := models.Attachment{
		Name:      "table.jpg",
		MediaID:   42,
		SourceURL: mediaURL,
		Header:    MakeAttachmentHeader("table.jpg", "base64", "image/jpeg"),
		Content:   []byte("image bytes"),
	}
	pdf := models.Attachment{
		Name:      "catalog.pdf",
		MediaID:   43,
		SourceURL: "http://intranet:9173/uploads/catalog.pdf",
		Header:    MakeAttachmentHeader("catalog.pdf", "base64", "application/pdf"),
		Content:   []byte("pdf bytes"),
	}
	body := []byte(`<a href="http://intranet:9173/uploads/table.jpg?size=large&amp;mode=cover">Keep this link</a><img src="http://intranet:9173/uploads/table.jpg?size=large&amp;mode=cover"><img src="https://example.com/external.jpg">`)

	gotBody, gotAttachments := InlineMediaImages(body, []models.Attachment{image, pdf})

	if !strings.Contains(string(gotBody), `src="cid:`+cid+`"`) {
		t.Fatalf("expected inline CID image source, got %q", gotBody)
	}
	if !strings.Contains(string(gotBody), `href="http://intranet:9173/uploads/table.jpg?size=large&amp;mode=cover"`) {
		t.Fatalf("non-image URLs must remain unchanged, got %q", gotBody)
	}
	if !strings.Contains(string(gotBody), `src="https://example.com/external.jpg"`) {
		t.Fatalf("external image URLs must remain unchanged, got %q", gotBody)
	}

	if !gotAttachments[0].Inline {
		t.Fatal("referenced image should be marked inline")
	}
	if gotAttachments[1].Inline {
		t.Fatal("non-image attachment must not be marked inline")
	}
	if got := gotAttachments[0].Header.Get("Content-ID"); got != "<"+cid+">" {
		t.Fatalf("unexpected content ID %q", got)
	}
	disposition, params, err := mime.ParseMediaType(gotAttachments[0].Header.Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse inline content disposition: %v", err)
	}
	if disposition != "inline" || params["filename"] != image.Name {
		t.Fatalf("unexpected inline content disposition %q %v", disposition, params)
	}

	if image.Inline || image.Header.Get("Content-ID") != "" {
		t.Fatal("preloaded campaign attachment must not be mutated")
	}
}
