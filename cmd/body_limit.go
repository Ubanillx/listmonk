package main

import (
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Request body size limits.
//
// Echo applies no default request body limit, and previously no handler wrapped
// its reads with io.LimitReader or http.MaxBytesReader either. Every endpoint
// therefore accepted bodies of any size: the anonymous bounce webhook read the
// entire payload before authenticating it, JSON handlers buffered it in memory,
// and multipart handlers spilled it to disk (>32 MB, Echo's in-memory
// threshold) where the temporary files were never removed. All of those are a
// cheap denial of service against a single process. This middleware is the one
// place that bounds request bodies.
//
// Limits are grouped by route family because legitimate payload sizes differ by
// orders of magnitude: a bounce notification is a few KB, while a customer
// import may legitimately be a large CSV/XLSX/ZIP. The longest matching prefix
// wins; anything unmatched gets defaultBodyLimit. These are transport-level
// backstops, not the authoritative per-feature caps (attachment counts and
// sizes, import row counts and unzipped sizes are enforced in the handlers).
//
// Limits use Echo's suffix parsing, which reads them as decimal SI units
// ("1M" is 1,000,000 bytes, not 1 MiB).
const defaultBodyLimit = "16M"

var bodyLimitRules = []struct {
	prefix string
	limit  string
}{
	// Inbound bounce/complaint webhooks. Notifications are small (SNS caps them
	// at 256 KB), and this payload is read before authentication, so keep the
	// allowance tight.
	{"/webhooks/", "1M"},

	// Transactional messages carry attachments as base64 inline in the JSON body
	// or as multipart files. Allow 32M so that the code-level attachment caps
	// (10M per file, 20M total) bind before the transport does, including the
	// 4/3 base64 inflation of the JSON path.
	{"/api/tx", "32M"},

	// Campaign and template content: rich HTML plus the visual editor's JSON
	// source document.
	{"/api/campaigns", "16M"},
	{"/api/templates", "16M"},

	// Media library uploads.
	{"/api/media", "32M"},

	// Pool allocation member imports (CSV/XLSX).
	{"/api/org-pool-allocations/", "32M"},
	{"/api/pools/allocations/", "32M"},

	// Bulk customer import: CSV, XLSX, or a ZIP archive of either.
	{"/api/import/customers", "64M"},
}

// bodyLimitForPath returns the transport body limit that applies to path. The
// longest matching prefix wins so that adding a more specific rule never
// requires reordering the table.
func bodyLimitForPath(path string) string {
	limit, matched := defaultBodyLimit, -1
	for _, r := range bodyLimitRules {
		if len(r.prefix) > matched && pathHasRoutePrefix(path, r.prefix) {
			limit, matched = r.limit, len(r.prefix)
		}
	}

	return limit
}

// pathHasRoutePrefix reports whether path belongs to the route family named by
// prefix. Matching on a segment boundary keeps an unrelated route that merely
// shares a string prefix ("/api/tx-report") from inheriting a looser limit.
func pathHasRoutePrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(path, prefix)
	}

	return strings.HasPrefix(path, prefix+"/")
}

// bodyLimitMiddleware enforces the per-route-family request body limits. Echo's
// limiter rejects a declared Content-Length over the limit before any read, and
// also counts bytes actually read so chunked bodies cannot bypass it.
func bodyLimitMiddleware() echo.MiddlewareFunc {
	// Parse and validate each distinct limit once at startup instead of per
	// request.
	byLimit := map[string]echo.MiddlewareFunc{}
	for _, r := range bodyLimitRules {
		if _, ok := byLimit[r.limit]; !ok {
			byLimit[r.limit] = middleware.BodyLimit(r.limit)
		}
	}
	if _, ok := byLimit[defaultBodyLimit]; !ok {
		byLimit[defaultBodyLimit] = middleware.BodyLimit(defaultBodyLimit)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			limit := byLimit[bodyLimitForPath(c.Request().URL.Path)]
			return limit(next)(c)
		}
	}
}
