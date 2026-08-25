package manager

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"net/textproto"
	"regexp"
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
	for i, attachment := range attachments {
		if attachment.MediaID < 1 || !isImageAttachment(attachment) {
			continue
		}

		if sourceURL := normalizeImageSource(attachment.SourceURL); sourceURL != "" {
			byURL[sourceURL] = i
		}
	}
	if len(byURL) == 0 {
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

		attachmentIndex, ok := byURL[normalizeImageSource(string(body[start:end]))]
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
	return strings.TrimSpace(html.UnescapeString(source))
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
