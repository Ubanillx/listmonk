package manager

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/knadh/listmonk/models"
)

var reMediaReference = regexp.MustCompile("(?is)(<[a-z][^>]*?\\s+(?:src|href|background)\\s*=\\s*)(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")
var reCSSMediaReference = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^\s)]+))\s*\)`)
var reMediaHref = regexp.MustCompile(`(?i)\bhref\s*=\s*$`)

// LinkMediaAttachments keeps images in place as remote <img> sources and
// represents other library files with links. Raw, one-off system attachments
// are preserved. No media-library binary is sent as a MIME part.
func LinkMediaAttachments(body, altBody []byte, attachments []models.Attachment, rootURL string, plain bool) ([]byte, []byte, []models.Attachment, error) {
	hasMedia := false
	for _, a := range attachments {
		if a.MediaID > 0 {
			hasMedia = true
			break
		}
	}
	if !hasMedia {
		return body, altBody, attachments, nil
	}
	root, err := url.Parse(strings.TrimRight(rootURL, "/") + "/")
	if err != nil || root.Host == "" || (root.Scheme != "https" && root.Scheme != "http") {
		return nil, nil, nil, fmt.Errorf("a valid public root URL is required for email media")
	}
	urls := make([]string, len(attachments))
	used := make([]bool, len(attachments))
	var remaining []models.Attachment
	for i, a := range attachments {
		if a.MediaID < 1 {
			remaining = append(remaining, a)
			continue
		}
		if a.DeliveryURL == "" {
			return nil, nil, nil, fmt.Errorf("media %d has no recipient link", a.MediaID)
		}
		urls[i] = strings.TrimRight(rootURL, "/") + "/" + strings.TrimLeft(a.DeliveryURL, "/")
	}
	if !plain {
		body = reMediaReference.ReplaceAllFunc(body, func(tag []byte) []byte {
			match := reMediaReference.FindSubmatchIndex(tag)
			start, end := imageSourceIndex(match)
			if start < 0 {
				return tag
			}
			source := html.UnescapeString(string(tag[start:end]))
			i := linkedMediaIndex(source, attachments, root)
			if i < 0 || urls[i] == "" {
				return tag
			}
			// An image download link alone does not display the image. Still add
			// an <img> below unless a src/background reference renders it.
			if !isImageAttachment(attachments[i]) || !reMediaHref.Match(tag[:match[3]]) {
				used[i] = true
			}
			// Always quote the new source: legacy editor HTML may use unquoted URLs.
			return []byte(string(tag[:match[3]]) + `"` + html.EscapeString(urls[i]) + `"`)
		})
		body = reCSSMediaReference.ReplaceAllFunc(body, func(css []byte) []byte {
			match := reCSSMediaReference.FindSubmatch(css)
			source := ""
			for _, group := range match[1:] {
				if len(group) > 0 {
					source = html.UnescapeString(string(group))
					break
				}
			}
			i := linkedMediaIndex(source, attachments, root)
			if i < 0 || urls[i] == "" {
				return css
			}
			used[i] = true
			// Issued URLs have escaped filenames and no whitespace/quotes.
			return []byte("url(" + html.EscapeString(urls[i]) + ")")
		})
	}
	var extra bytes.Buffer
	for i, a := range attachments {
		if urls[i] == "" {
			continue
		}
		textLink := "\n" + a.Name + ": " + urls[i] + "\n"
		if plain {
			extra.WriteString(textLink)
		} else if !used[i] {
			if isImageAttachment(a) {
				extra.WriteString(`<p><img src="` + html.EscapeString(urls[i]) + `" alt="` + html.EscapeString(a.Name) + `"></p>`)
			} else {
				extra.WriteString(`<p><a href="` + html.EscapeString(urls[i]) + `">` + html.EscapeString(a.Name) + `</a></p>`)
			}
		}
		if len(altBody) > 0 {
			altBody = append(append([]byte(nil), altBody...), textLink...)
		}
	}
	if extra.Len() > 0 {
		// Insert before the closing body so links remain visible in full HTML documents.
		insert := len(body)
		if !plain {
			if idx := strings.LastIndex(strings.ToLower(string(body)), "</body>"); idx >= 0 {
				insert = idx
			}
		}
		out := make([]byte, 0, len(body)+extra.Len())
		out = append(out, body[:insert]...)
		out = append(out, extra.Bytes()...)
		body = append(out, body[insert:]...)
	}
	return body, altBody, remaining, nil
}

func linkedMediaIndex(source string, attachments []models.Attachment, root *url.URL) int {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return -1
	}
	// Only local/provider references may be matched by path or filename. Exact
	// source URLs are also allowed for S3 providers on a separate domain.
	local := u.Host == "" || strings.EqualFold(u.Host, root.Host) || isLocalMediaHost(u.Hostname())
	for i, a := range attachments {
		if a.MediaID > 0 && normalizeImageSource(source) == normalizeImageSource(a.SourceURL) {
			return i
		}
	}
	if !local {
		return -1
	}
	const prefix = "/api/media/file/"
	if strings.HasPrefix(u.Path, prefix) {
		parts := strings.Split(strings.TrimPrefix(u.Path, prefix), "/")
		if len(parts) == 2 {
			// An ID-qualified reference must never fall back to a same-name clone.
			for i, a := range attachments {
				if fmt.Sprint(a.MediaID) == parts[0] && a.Name == parts[1] {
					return i
				}
			}
			return -1
		}
	}
	for i, a := range attachments {
		if a.MediaID < 1 {
			continue
		}
		if source == inlineContentID(a.MediaID) || source == "cid:"+inlineContentID(a.MediaID) {
			return i
		}
		if (strings.HasPrefix(u.Path, prefix) || strings.HasPrefix(u.Path, "/uploads/")) &&
			path.Base(u.Path) == a.Name {
			return i
		}
		if imageSourcePath(source) == imageSourcePath(a.SourceURL) {
			return i
		}
	}
	return -1
}
