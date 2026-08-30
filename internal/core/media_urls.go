package core

import (
	"html"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// protectedMediaIDPattern matches the numeric ID portion of the canonical
// protected media route while leaving the host, filename, query, and fragment
// untouched. It is intentionally narrow: legacy /uploads/<filename> URLs can
// be ambiguous after a clone and must not be rewritten by guessing a record.
var protectedMediaIDPattern = regexp.MustCompile(`(?i)(/api/media/file/)([0-9]+)([/\?#"'<>\s]|$)`)

// legacyMediaURLPattern matches the filename-only media routes emitted by
// older editors.  It intentionally stops at the first query/fragment or HTML
// delimiter so the original suffix can be retained verbatim.
var legacyMediaURLPattern = regexp.MustCompile(`(?i)(https?://[^/"'<>\s]+)?(/(?:uploads|api/media/file)/)([^/?#"'<>\s]+)([?#"'<>\s]|$)`)

// rewriteProtectedMediaReferences updates ID-qualified media URLs after a
// resource clone or migration. Media provider objects retain their filename,
// but the copied database row receives a new ID; without rewriting, the copied
// body would still point at the source row and could resolve to the wrong
// workspace (or fail after the source is removed).
func rewriteProtectedMediaReferences(body []byte, copies map[int]int) []byte {
	return rewriteMediaReferences(body, copies, nil)
}

// rewriteMediaReferences rewrites both ID-qualified and historical
// filename-only media references after a clone.  The filename-only rewrite is
// deliberately driven by the source rows known to belong to this clone; it
// never guesses from an arbitrary filename and it refuses public/external
// hosts.  This keeps old /uploads and /api/media/file/:filename editor content
// working while avoiding accidental changes to unrelated external images.
func rewriteMediaReferences(body []byte, copies map[int]int, sourceNames map[int]string) []byte {
	if len(body) == 0 || len(copies) == 0 {
		return body
	}
	rewritten := protectedMediaIDPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := protectedMediaIDPattern.FindSubmatch(match)
		if len(sub) != 4 {
			return match
		}
		oldID, err := strconv.Atoi(string(sub[2]))
		if err != nil {
			return match
		}
		newID, ok := copies[oldID]
		if !ok || newID < 1 {
			return match
		}
		newIDText := strconv.Itoa(newID)
		out := make([]byte, 0, len(sub[1])+len(newIDText)+len(sub[3]))
		out = append(out, sub[1]...)
		out = append(out, newIDText...)
		out = append(out, sub[3]...)
		return out
	})
	if len(sourceNames) == 0 {
		return rewritten
	}

	// Build a filename map only for unambiguous source rows.  Two cloned media
	// records may intentionally share a filename; rewriting such a legacy URL
	// by guess would be worse than leaving it unchanged.
	type legacyCopy struct {
		id       int
		filename string
	}
	byFilename := make(map[string]legacyCopy, len(sourceNames))
	ambiguous := make(map[string]struct{})
	for oldID, filename := range sourceNames {
		newID, ok := copies[oldID]
		if !ok || newID < 1 {
			continue
		}
		key := normalizeLegacyMediaFilename(filename)
		if key == "" {
			continue
		}
		if previous, exists := byFilename[key]; exists && previous.id != newID {
			delete(byFilename, key)
			ambiguous[key] = struct{}{}
			continue
		}
		if _, exists := ambiguous[key]; !exists {
			byFilename[key] = legacyCopy{id: newID, filename: filename}
		}
	}
	if len(byFilename) == 0 {
		return rewritten
	}

	return legacyMediaURLPattern.ReplaceAllFunc(rewritten, func(match []byte) []byte {
		sub := legacyMediaURLPattern.FindSubmatch(match)
		if len(sub) != 5 {
			return match
		}
		host := string(sub[1])
		route := strings.ToLower(string(sub[2]))
		// The numeric protected route has already been handled above.  Do not
		// reinterpret its numeric segment as a filename-only route.
		segment := string(sub[3])
		if route == "/api/media/file/" {
			if id, err := strconv.Atoi(segment); err == nil && id > 0 {
				return match
			}
		}
		if host != "" && !isLocalMediaRewriteHost(host) {
			return match
		}
		filename := normalizeLegacyMediaFilename(segment)
		copyRow, ok := byFilename[filename]
		if !ok {
			return match
		}
		canonical := "/api/media/file/" + strconv.Itoa(copyRow.id) + "/" + url.PathEscape(path.Base(copyRow.filename))
		out := make([]byte, 0, len(canonical)+len(sub[4]))
		out = append(out, canonical...)
		out = append(out, sub[4]...)
		return out
	})
}

func normalizeLegacyMediaFilename(value string) string {
	value = strings.TrimSpace(html.UnescapeString(value))
	if value == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(value); err == nil {
		value = decoded
	}
	return strings.ToLower(path.Base(value))
}

func isLocalMediaRewriteHost(rawHost string) bool {
	host := strings.TrimSpace(rawHost)
	u, err := url.Parse(host)
	if err == nil && u.Hostname() != "" {
		host = u.Hostname()
	} else {
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "https://")
		if idx := strings.IndexByte(host, ':'); idx >= 0 {
			host = host[:idx]
		}
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}
