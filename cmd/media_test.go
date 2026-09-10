package main

import (
	"bytes"
	"errors"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/disintegration/imaging"
	"github.com/labstack/echo/v4"
)

// multipartFileHeader builds a real *multipart.FileHeader holding content, the
// same way the router would hand one to a handler.
func multipartFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/media", &body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}

	return req.MultipartForm.File["file"][0]
}

// gifWithCanvas returns a GIF whose screen descriptor declares the given canvas
// size. Only the header (and the global color table) is present, so decoding
// pixels would fail: an over-sized canvas has to be refused from the header
// alone, before any decode is attempted.
func gifWithCanvas(width, height int) []byte {
	b := make([]byte, 0, 13+768+1)
	b = append(b, "GIF89a"...)
	b = append(b, byte(width), byte(width>>8), byte(height), byte(height>>8))
	// Global color table present, 256 entries.
	b = append(b, 0xF7, 0x00, 0x00)
	b = append(b, make([]byte, 768)...)
	return append(b, 0x3B)
}

func TestProcessImageAcceptsSmallImage(t *testing.T) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, imaging.New(20, 10, color.NRGBA{A: 255}), imaging.PNG); err != nil {
		t.Fatal(err)
	}

	thumb, width, height, err := processImage(multipartFileHeader(t, "small.png", buf.Bytes()))
	if err != nil {
		t.Fatalf("expected a small image to be accepted: %v", err)
	}
	if width != 20 || height != 10 {
		t.Fatalf("reported dimensions = %d x %d, want 20 x 10", width, height)
	}
	if thumb == nil || thumb.Len() == 0 {
		t.Fatal("expected thumbnail bytes")
	}
}

// TestProcessImageRejectsOversizedCanvas covers the denial-of-service case: a
// tiny file can declare a canvas whose decode would allocate an enormous
// bitmap, so the dimensions are validated before the pixels are read.
func TestProcessImageRejectsOversizedCanvas(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"width over the per-side limit", maxMediaImageDimension + 1, 10},
		{"height over the per-side limit", 10, maxMediaImageDimension + 1},
		{"pixels over the total limit", 10000, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hdr := multipartFileHeader(t, "huge.gif", gifWithCanvas(tc.width, tc.height))

			_, _, _, err := processImage(hdr)
			if !errors.Is(err, errMediaTooLarge) {
				t.Fatalf("expected errMediaTooLarge, got %v", err)
			}
		})
	}
}

// TestProcessImageRejectsOversizedUpload checks the size gate that runs before
// the file is read, using a declared size rather than a real 32 MB payload.
func TestProcessImageRejectsOversizedUpload(t *testing.T) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, imaging.New(4, 4, color.NRGBA{A: 255}), imaging.PNG); err != nil {
		t.Fatal(err)
	}

	hdr := multipartFileHeader(t, "small.png", buf.Bytes())
	hdr.Size = maxMediaUploadSize + 1

	if _, _, _, err := processImage(hdr); !errors.Is(err, errMediaTooLarge) {
		t.Fatalf("expected errMediaTooLarge, got %v", err)
	}
}

// TestProcessImageRejectsNonImage guards the decode error path for a file that
// is not an image at all.
func TestProcessImageRejectsNonImage(t *testing.T) {
	_, _, _, err := processImage(multipartFileHeader(t, "notes.png", []byte("not an image")))
	if err == nil {
		t.Fatal("expected a non-image to be rejected")
	}
	if errors.Is(err, errMediaTooLarge) {
		t.Fatal("a non-image must not be reported as too large")
	}
}
