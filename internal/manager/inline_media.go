package manager

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"net"
	"net/textproto"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/models"
)

var reImageSource = regexp.MustCompile("(?is)(<img\\b[^>]*?\\s+src\\s*=\\s*)(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")

// InlineMediaImages replaces image URLs pointing at media-library files with
// content IDs and marks their MIME parts as HTML-related inline images.
func InlineMediaImages(body []byte, attachments []models.Attachment) ([]byte, []models.Attachment) {
	if len(body) == 0 || len(attachments) == 0 {
		return body, attachments
	}

	byURL := make(map[string]int, len(attachments))
	byPath := make(map[string]int, len(attachments))
	byFilename := make(map[string]int, len(attachments))
	byID := make(map[int]int, len(attachments))
	for i, attachment := range attachments {
		if attachment.MediaID < 1 || !isImageAttachment(attachment) {
			continue
		}
		byID[attachment.MediaID] = i
		if filename := normalizeMediaFilename(attachment.Name); filename != "" {
			byFilename[filename] = i
		}

		if sourceURL := normalizeImageSource(attachment.SourceURL); sourceURL != "" {
			byURL[sourceURL] = i
			if sourcePath := imageSourcePath(sourceURL); sourcePath != "" {
				byPath[sourcePath] = i
			}
		}
	}
	if len(byURL) == 0 && len(byPath) == 0 && len(byFilename) == 0 {
		return body, attachments
	}

	updated := append([]models.Attachment(nil), attachments...)
	used := make([]bool, len(updated))
	var (
		out      bytes.Buffer
		last     int
		replaced bool
	)

	for _, match := range reImageSource.FindAllSubmatchIndex(body, -1) {
		start, end := imageSourceIndex(match)
		if start < 0 {
			continue
		}

		rawSource := string(body[start:end])
		source := normalizeImageSource(rawSource)
		// ID-qualified protected URLs are authoritative. This is what keeps
		// cloned media rows with the same filename from being matched to the
		// wrong attachment.
		attachmentIndex, ok := -1, false
		if mediaID, hasID := protectedMediaIDFromSource(rawSource); hasID {
			attachmentIndex, ok = byID[mediaID]
		}
		if !ok {
			attachmentIndex, ok = byURL[source]
		}
		// Relative image sources don't carry the app's host. Match them by
		// path, but never use this fallback for an absolute external URL.
		if !ok {
			candidatePath := imageSourcePath(source)
			if candidatePath != "" {
				if candidate, found := byPath[candidatePath]; found {
					// A path fallback is useful for cloned messages whose internal
					// root URL changed, but must not turn an unrelated external image
					// into a CID reference merely because its filename is identical.
					if isRelativeImageSource(rawSource) || isLikelyLocalMediaURL(rawSource, attachments[candidate].SourceURL) {
						attachmentIndex, ok = candidate, true
					}
				}
			}
		}
		// Historical editor insertions use the filename-only protected route.
		// Match only this controlled route by filename; legacy upload paths still
		// use the more precise URL/path matching above.
		if !ok && isProtectedMediaSource(rawSource) {
			if candidate, found := byFilename[mediaFilenameFromSource(rawSource)]; found {
				attachmentIndex, ok = candidate, true
			}
		}
		if !ok {
			continue
		}

		if !used[attachmentIndex] {
			updated[attachmentIndex] = makeInlineImageAttachment(updated[attachmentIndex])
			used[attachmentIndex] = true
		}

		out.Write(body[last:start])
		out.WriteString("cid:")
		out.WriteString(inlineContentID(updated[attachmentIndex].MediaID))
		last = end
		replaced = true
	}

	if !replaced {
		return body, attachments
	}

	out.Write(body[last:])
	return out.Bytes(), updated
}

func imageSourceIndex(match []int) (int, int) {
	for group := 2; group <= 4; group++ {
		start, end := match[group*2], match[group*2+1]
		if start >= 0 {
			return start, end
		}
	}

	return -1, -1
}

func isImageAttachment(attachment models.Attachment) bool {
	contentType, _, err := mime.ParseMediaType(attachment.Header.Get("Content-Type"))
	return err == nil && strings.HasPrefix(strings.ToLower(contentType), "image/")
}

func normalizeImageSource(source string) string {
	source = strings.TrimSpace(html.UnescapeString(source))
	if source == "" {
		return ""
	}

	u, err := url.Parse(source)
	if err != nil {
		return source
	}

	// URL escaping may be introduced while html/template renders an image
	// attribute (for example, parentheses become percent-escaped). Compare
	// the decoded path and a canonical query instead of the raw spelling.
	decodedPath, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		decodedPath = u.Path
	}
	decodedPath = path.Clean("/" + strings.TrimSpace(decodedPath))
	query := u.Query().Encode()
	if u.Host == "" && u.Scheme == "" {
		return "relative:" + decodedPath + "?" + query
	}

	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + decodedPath + "?" + query
}

func isRelativeImageSource(source string) bool {
	u, err := url.Parse(strings.TrimSpace(html.UnescapeString(source)))
	return err == nil && u.Scheme == "" && u.Host == ""
}

func isProtectedMediaSource(source string) bool {
	u, err := url.Parse(strings.TrimSpace(html.UnescapeString(source)))
	if err != nil {
		return false
	}
	if u.Host != "" && !isLocalMediaHost(u.Hostname()) {
		return false
	}
	return strings.HasPrefix(path.Clean("/"+u.Path), "/api/media/file/")
}

// protectedMediaIDFromSource extracts the optional numeric media ID from the
// canonical /api/media/file/:id/:filename route. Filename-only historical URLs
// return (0, false). Hosts are restricted in the same way as
// isProtectedMediaSource so an external image cannot be promoted to a CID just
// because its path happens to contain this prefix.
func protectedMediaIDFromSource(source string) (int, bool) {
	u, err := url.Parse(strings.TrimSpace(html.UnescapeString(source)))
	if err != nil {
		return 0, false
	}
	if u.Host != "" && !isLocalMediaHost(u.Hostname()) {
		return 0, false
	}
	cleanPath := path.Clean("/" + u.Path)
	const prefix = "/api/media/file/"
	if !strings.HasPrefix(cleanPath, prefix) {
		return 0, false
	}
	remainder := strings.TrimPrefix(cleanPath, prefix)
	idPart := remainder
	if idx := strings.IndexByte(idPart, '/'); idx >= 0 {
		idPart = idPart[:idx]
	}
	id, err := strconv.Atoi(idPart)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

func mediaFilenameFromSource(source string) string {
	u, err := url.Parse(strings.TrimSpace(html.UnescapeString(source)))
	if err != nil {
		return ""
	}
	decodedPath, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		decodedPath = u.Path
	}
	return normalizeMediaFilename(decodedPath)
}

func normalizeMediaFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(value); err == nil {
		value = decoded
	}
	return strings.ToLower(path.Base(value))
}

func isLikelyLocalMediaURL(source, mediaSource string) bool {
	sourceURL, sourceErr := url.Parse(strings.TrimSpace(html.UnescapeString(source)))
	mediaURL, mediaErr := url.Parse(strings.TrimSpace(html.UnescapeString(mediaSource)))
	if sourceErr != nil || mediaErr != nil || sourceURL.Host == "" || mediaURL.Host == "" {
		return false
	}

	return isLocalMediaHost(sourceURL.Hostname()) && isLocalMediaHost(mediaURL.Hostname())
}

func isLocalMediaHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

// imageSourcePath returns a stable URL path for matching relative media URLs.
// Query and fragment components are intentionally ignored.
func imageSourcePath(source string) string {
	u, err := url.Parse(source)
	if err != nil || u.Path == "" {
		return ""
	}

	return path.Clean("/" + strings.TrimSpace(u.Path))
}

func makeInlineImageAttachment(attachment models.Attachment) models.Attachment {
	header := cloneMIMEHeader(attachment.Header)
	header.Set("Content-ID", "<"+inlineContentID(attachment.MediaID)+">")
	header.Set("Content-Disposition", inlineContentDisposition(attachment.Name))
	attachment.Header = header
	attachment.Inline = true

	return attachment
}

func inlineContentID(mediaID int) string {
	return fmt.Sprintf("media-%d@listmonk", mediaID)
}

func inlineContentDisposition(filename string) string {
	filename = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(filename, "\r", ""), "\n", ""))
	if filename == "" {
		return "inline"
	}

	if disposition := mime.FormatMediaType("inline", map[string]string{"filename": filename}); disposition != "" {
		return disposition
	}

	return "inline"
}

func cloneMIMEHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	cloned := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}

	return cloned
}
