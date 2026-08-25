package manager

import (
	"mime"
	"testing"
)

func TestMakeAttachmentHeaderFormatsNonASCIIName(t *testing.T) {
	const filename = "报价单 2026.pdf"
	h := MakeAttachmentHeader(filename, "", "application/pdf")

	disposition, dispositionParams, err := mime.ParseMediaType(h.Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse Content-Disposition: %v", err)
	}
	if disposition != "attachment" || dispositionParams["filename"] != filename {
		t.Fatalf("unexpected Content-Disposition: %q %v", disposition, dispositionParams)
	}

	contentType, contentTypeParams, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if contentType != "application/pdf" || contentTypeParams["name"] != filename {
		t.Fatalf("unexpected Content-Type: %q %v", contentType, contentTypeParams)
	}
	if h.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("expected base64 transfer encoding, got %q", h.Get("Content-Transfer-Encoding"))
	}
}

func TestMakeAttachmentHeaderRemovesNewlinesFromFilename(t *testing.T) {
	h := MakeAttachmentHeader("report.pdf\r\nX-Injected: true", "base64", "")

	_, params, err := mime.ParseMediaType(h.Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse Content-Disposition: %v", err)
	}
	if params["filename"] != "report.pdfX-Injected: true" {
		t.Fatalf("unexpected sanitized filename: %q", params["filename"])
	}
	if h.Get("X-Injected") != "" {
		t.Fatal("filename must not create an injected MIME header")
	}
}
