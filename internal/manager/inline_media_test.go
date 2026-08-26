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

func TestInlineMediaImagesMatchesMediaPathWhenHostDiffers(t *testing.T) {
	image := models.Attachment{
		Name:      "logo.png",
		MediaID:   9,
		SourceURL: "http://current-host:9173/uploads/logo.png",
		Header:    MakeAttachmentHeader("logo.png", "base64", "image/png"),
		Content:   []byte("image bytes"),
	}

	body, attachments := InlineMediaImages(
		[]byte(`<p><img src="http://old-host:9173/uploads/logo.png?cache=1#top"></p>`),
		[]models.Attachment{image},
	)

	if !strings.Contains(string(body), `src="cid:media-9@listmonk"`) {
		t.Fatalf("expected host-independent CID replacement, got %q", body)
	}
	if !attachments[0].Inline {
		t.Fatal("matched image should be marked inline")
	}
}

func TestInlineMediaImagesMatchesRelativeMediaSource(t *testing.T) {
	image := models.Attachment{
		Name:      "logo.png",
		MediaID:   10,
		SourceURL: "http://current-host:9173/uploads/logo.png",
		Header:    MakeAttachmentHeader("logo.png", "base64", "image/png"),
		Content:   []byte("image bytes"),
	}

	body, attachments := InlineMediaImages(
		[]byte(`<img src="/uploads/logo.png?cache=1">`),
		[]models.Attachment{image},
	)

	if !strings.Contains(string(body), `src="cid:media-10@listmonk"`) {
		t.Fatalf("expected relative media source replacement, got %q", body)
	}
	if !attachments[0].Inline {
		t.Fatal("relative media source should mark the image inline")
	}
}

func TestInlineMediaImagesMatchesProtectedMediaRoute(t *testing.T) {
	image := models.Attachment{
		Name:      "logo.png",
		MediaID:   12,
		SourceURL: "http://current-host:9173/uploads/logo.png",
		Header:    MakeAttachmentHeader("logo.png", "base64", "image/png"),
		Content:   []byte("image bytes"),
	}

	body, attachments := InlineMediaImages(
		[]byte(`<img src="/api/media/file/logo.png?organization_id=7">`),
		[]models.Attachment{image},
	)
	if !strings.Contains(string(body), `src="cid:media-12@listmonk"`) {
		t.Fatalf("expected protected media source replacement, got %q", body)
	}
	if !attachments[0].Inline {
		t.Fatal("protected media route should mark the image inline")
	}
}

func TestInlineMediaImagesMatchesEscapedMediaFilename(t *testing.T) {
	image := models.Attachment{
		Name:      "image004(08-25-22-12-43).png",
		MediaID:   11,
		SourceURL: "http://192.168.1.67:9173/uploads/image004(08-25-22-12-43).png",
		Header:    MakeAttachmentHeader("image004(08-25-22-12-43).png", "base64", "image/png"),
		Content:   []byte("image bytes"),
	}

	body, attachments := InlineMediaImages(
		[]byte(`<img src="http://192.168.1.67:9173/uploads/image004%2808-25-22-12-43%29.png">`),
		[]models.Attachment{image},
	)

	if !strings.Contains(string(body), `src="cid:media-11@listmonk"`) {
		t.Fatalf("expected escaped media filename replacement, got %q", body)
	}
	if !attachments[0].Inline {
		t.Fatal("escaped media filename should mark the image inline")
	}
}

func TestInlineMediaImagesDoesNotRewriteUnrelatedImageWithSamePath(t *testing.T) {
	image := models.Attachment{
		Name:      "logo.png",
		MediaID:   9,
		SourceURL: "http://current-host:9173/uploads/logo.png",
		Header:    MakeAttachmentHeader("logo.png", "base64", "image/png"),
		Content:   []byte("image bytes"),
	}

	body, attachments := InlineMediaImages(
		[]byte(`<img src="https://external.example/uploads/logo.png">`),
		[]models.Attachment{image},
	)

	if strings.Contains(string(body), "cid:media-9@listmonk") {
		t.Fatalf("external image must remain unchanged, got %q", body)
	}
	if attachments[0].Inline {
		t.Fatal("unrelated image must not mark the media attachment inline")
	}
}
