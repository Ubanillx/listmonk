package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestBodyLimitForPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		// Route families with an explicit limit.
		{"/webhooks/service/ses", "1M"},
		{"/webhooks/bounce", "1M"},
		{"/api/tx", "32M"},
		{"/api/media", "32M"},
		{"/api/media/file/12/logo.png", "32M"},
		{"/api/templates", "16M"},
		{"/api/campaigns/12/preview", "16M"},
		{"/api/org-pool-allocations/3/import-members", "32M"},
		{"/api/pools/allocations/3/import-members", "32M"},
		{"/api/import/customers", "64M"},
		// Everything else falls back to the default.
		{"/api/customers", defaultBodyLimit},
		{"/api/settings", defaultBodyLimit},
		{"/subscription/form", defaultBodyLimit},
		{"/", defaultBodyLimit},
		// A prefix must match on a route segment boundary, not on any string
		// prefix, or an unrelated route inherits a looser limit.
		{"/api/tx-report", defaultBodyLimit},
		{"/api/mediawiki", defaultBodyLimit},
	} {
		if got := bodyLimitForPath(tc.path); got != tc.want {
			t.Errorf("bodyLimitForPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// endlessReader yields a single repeated byte forever, so an over-sized request
// can be described by its Content-Length without allocating the body.
type endlessReader byte

func (r endlessReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

// TestBodyLimitMiddlewareDeclaredLength checks the cheap rejection path: a
// client that announces an over-sized body is refused before anything is read
// or buffered, on both a specifically limited route and an unlisted one.
func TestBodyLimitMiddlewareDeclaredLength(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		length int64
	}{
		{"webhook over its 1M allowance", "/webhooks/service/ses", 2 << 20},
		{"unlisted route over the default", "/api/customers", (16 << 20) + 1},
		{"transactional over its 32M allowance", "/api/tx", (32 << 20) + 1},
		{"media over its 32M allowance", "/api/media", (32 << 20) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			e := echo.New()
			e.Use(bodyLimitMiddleware())
			e.POST(tc.path, func(c echo.Context) error {
				called = true
				return c.NoContent(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, tc.path, io.NopCloser(endlessReader('a')))
			req.ContentLength = tc.length
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413, got %d", rec.Code)
			}
			if called {
				t.Fatal("expected the handler not to run for a rejected body")
			}
		})
	}
}

// TestBodyLimitMiddlewareChunked checks that the limit also counts the bytes
// actually read, so a chunked body without a Content-Length cannot bypass it.
func TestBodyLimitMiddlewareChunked(t *testing.T) {
	e := echo.New()
	e.Use(bodyLimitMiddleware())
	e.POST("/webhooks/service/ses", func(c echo.Context) error {
		if _, err := io.ReadAll(c.Request().Body); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	})

	// No usable Content-Length: the request declares an unknown length, so the
	// limit can only be enforced while reading.
	req := httptest.NewRequest(http.MethodPost, "/webhooks/service/ses",
		io.NopCloser(strings.NewReader(strings.Repeat("a", 2<<20))))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for an over-sized chunked body, got %d", rec.Code)
	}
}

// TestBodyLimitMiddlewareAllowsAllowedBody guards against a limit that rejects
// legitimate traffic: a normal webhook notification and a body exactly at the
// declared limit are both delivered intact. Echo reads the "1M" suffix as a
// decimal SI unit, so the webhook boundary is 1,000,000 bytes.
func TestBodyLimitMiddlewareAllowsAllowedBody(t *testing.T) {
	e := echo.New()
	e.Use(bodyLimitMiddleware())
	e.POST("/webhooks/service/ses", func(c echo.Context) error {
		b, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, string(b))
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"typical notification", `{"notificationType":"Bounce"}`},
		{"exactly at the limit", strings.Repeat("a", 1_000_000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhooks/service/ses", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Fatalf("body was altered: got %d bytes, want %d", len(got), len(tc.body))
			}
		})
	}
}

// TestBodyLimitMiddlewareRejectsOneByteOver pins the boundary from both sides so
// the limit cannot silently drift by a unit-conversion change.
func TestBodyLimitMiddlewareRejectsOneByteOver(t *testing.T) {
	e := echo.New()
	e.Use(bodyLimitMiddleware())
	e.POST("/webhooks/service/ses", func(c echo.Context) error {
		if _, err := io.ReadAll(c.Request().Body); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	})

	// Declared length one byte over the webhook allowance.
	req := httptest.NewRequest(http.MethodPost, "/webhooks/service/ses",
		io.NopCloser(strings.NewReader(strings.Repeat("a", 1_000_001))))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 one byte over the limit, got %d", rec.Code)
	}
}
