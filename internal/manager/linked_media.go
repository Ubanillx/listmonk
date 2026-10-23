package manager

import (
	"bytes"
	"fmt"
	"html"
	"mime"
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
	var extraImages, extraFiles bytes.Buffer
	fileCount := 0
	for i, a := range attachments {
		if urls[i] == "" {
			continue
		}
		textLink := "\n" + a.Name + ": " + urls[i] + "\n"
		if plain {
			extraFiles.WriteString(textLink)
		} else if !used[i] {
			if isImageAttachment(a) {
				extraImages.WriteString(`<p><img src="` + html.EscapeString(urls[i]) + `" alt="` + html.EscapeString(a.Name) + `"></p>`)
			} else {
				writeLinkedFileCard(&extraFiles, a, urls[i])
				fileCount++
			}
		}
		if len(altBody) > 0 {
			altBody = append(append([]byte(nil), altBody...), textLink...)
		}
	}
	if fileCount > 0 {
		var section bytes.Buffer
		section.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="width:100%;table-layout:fixed;margin:28px 0 12px;border:1px solid #e5e9ef;border-radius:12px;background-color:#ffffff;font-family:Arial,'Microsoft YaHei',sans-serif"><tr><td style="padding:20px 20px 8px">`)
		section.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="width:100%;margin:0 0 18px"><tr><td valign="middle" style="color:#243247;font-size:16px;font-weight:bold;line-height:24px">附件资料<br><span style="color:#7b8798;font-size:12px;font-weight:normal;line-height:20px">查看或下载随附的文件</span></td>`)
		fmt.Fprintf(&section, `<td align="right" valign="top" style="padding-top:2px;white-space:nowrap"><span style="display:inline-block;padding:3px 9px;border-radius:12px;background-color:#f1f4f8;color:#6b7789;font-size:12px;line-height:18px">%d 个文件</span></td></tr></table>`, fileCount)
		section.Write(extraFiles.Bytes())
		section.WriteString(`</td></tr></table>`)
		extraFiles = section
	}
	if extraImages.Len()+extraFiles.Len() > 0 {
		// Insert before the closing body so links remain visible in full HTML documents.
		insert := len(body)
		if !plain {
			if idx := strings.LastIndex(strings.ToLower(string(body)), "</body>"); idx >= 0 {
				insert = idx
			}
		}
		out := make([]byte, 0, len(body)+extraImages.Len()+extraFiles.Len())
		out = append(out, body[:insert]...)
		out = append(out, extraImages.Bytes()...)
		out = append(out, extraFiles.Bytes()...)
		body = append(out, body[insert:]...)
	}
	return body, altBody, remaining, nil
}

// writeLinkedFileCard uses only tables and inline styles so the file link is
// recognizable even in email clients that strip stylesheets or remote images.
func writeLinkedFileCard(out *bytes.Buffer, attachment models.Attachment, deliveryURL string) {
	label, bg, color := linkedFileBadge(attachment)
	name := html.EscapeString(attachment.Name)
	downloadURL := html.EscapeString(linkedFileDownloadURL(deliveryURL))
	viewURL, canView := linkedFileViewURL(attachment.Name, deliveryURL)
	nameURL := downloadURL
	if canView {
		nameURL = html.EscapeString(viewURL)
	}
	out.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="width:100%;table-layout:fixed;border-radius:9px;background-color:#f7f9fc;margin:0 0 12px;font-family:Arial,'Microsoft YaHei',sans-serif"><tr>`)
	fmt.Fprintf(out, `<td width="60" valign="middle" style="width:60px;padding:18px 0 18px 16px"><table role="presentation" cellpadding="0" cellspacing="0" border="0" width="42" style="width:42px;background-color:%s;border-radius:5px"><tr><td width="30" height="11" style="width:30px;height:11px;font-size:0;line-height:0">&nbsp;</td><td width="12" height="11" style="width:12px;height:11px;background-color:%s;border-radius:0 5px 0 4px;font-size:0;line-height:0">&nbsp;</td></tr><tr><td colspan="2" align="center" style="padding:5px 2px 4px;color:%s;font-family:Arial,sans-serif;font-size:11px;font-weight:bold;line-height:15px">%s</td></tr><tr><td colspan="2" align="center" style="padding:0 10px 10px"><table role="presentation" cellpadding="0" cellspacing="0" border="0" width="22"><tr><td height="2" style="height:2px;background-color:%s;font-size:0;line-height:0">&nbsp;</td></tr><tr><td height="4" style="height:4px;font-size:0;line-height:0">&nbsp;</td></tr><tr><td height="2" style="height:2px;background-color:%s;font-size:0;line-height:0">&nbsp;</td></tr></table></td></tr></table></td>`, bg, color, color, label, color, color)
	fmt.Fprintf(out, `<td valign="middle" style="padding:16px 16px 16px 4px;word-wrap:break-word;overflow-wrap:anywhere"><a href="%s" style="color:#28374c;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:14px;font-weight:bold;line-height:22px;text-decoration:none">%s</a><br>`, nameURL, name)
	if canView {
		fmt.Fprintf(out, `<a href="%s" style="display:inline-block;margin:9px 14px 0 0;padding:6px 12px;border-radius:5px;background-color:#e8f0fd;color:#2864b4;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:12px;font-weight:bold;line-height:18px;white-space:nowrap;text-decoration:none">查看&nbsp;&#8599;</a>`, html.EscapeString(viewURL))
	}
	fmt.Fprintf(out, `<a href="%s" style="display:inline-block;margin:9px 0 0;padding:6px 0;color:#69788e;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:12px;line-height:18px;white-space:nowrap;text-decoration:none">下载原文件&nbsp;&#8595;</a></td></tr></table>`, downloadURL)
}

func linkedFileViewURL(filename, deliveryURL string) (string, bool) {
	switch strings.ToLower(path.Ext(filename)) {
	case ".doc", ".docx", ".docm", ".xls", ".xlsx", ".xlsm", ".ppt", ".pptx", ".pptm":
		return "https://view.officeapps.live.com/op/view.aspx?src=" + url.QueryEscape(deliveryURL), true
	case ".pdf":
		return deliveryURL, true
	default:
		return "", false
	}
}

func linkedFileDownloadURL(deliveryURL string) string {
	u, err := url.Parse(deliveryURL)
	if err != nil {
		return deliveryURL
	}
	query := u.Query()
	query.Set("download", "1")
	u.RawQuery = query.Encode()
	return u.String()
}

func linkedFileBadge(attachment models.Attachment) (label, background, foreground string) {
	ext := strings.ToLower(path.Ext(attachment.Name))
	contentType, _, _ := mime.ParseMediaType(attachment.Header.Get("Content-Type"))
	switch {
	case ext == ".pdf" || contentType == "application/pdf":
		return "PDF", "#fff0ee", "#c83d32"
	case ext == ".doc" || ext == ".docx" || ext == ".docm" || contentType == "application/msword" || strings.Contains(contentType, "wordprocessingml"):
		return "DOC", "#eaf2ff", "#245bc2"
	case ext == ".csv" || contentType == "text/csv":
		return "CSV", "#e9f7ee", "#217a46"
	case ext == ".xls" || ext == ".xlsx" || ext == ".xlsm" || strings.Contains(contentType, "spreadsheetml") || contentType == "application/vnd.ms-excel":
		return "XLS", "#e9f7ee", "#217a46"
	case ext == ".ppt" || ext == ".pptx" || ext == ".pptm" || contentType == "application/vnd.ms-powerpoint" || strings.Contains(contentType, "presentationml"):
		return "PPT", "#fff1e9", "#b8511d"
	case ext == ".rar":
		return "RAR", "#f2edff", "#7049a6"
	case ext == ".7z":
		return "7Z", "#f2edff", "#7049a6"
	case ext == ".tar" || ext == ".gz":
		return "TAR", "#f2edff", "#7049a6"
	case ext == ".zip" || strings.Contains(contentType, "zip") || strings.Contains(contentType, "compressed"):
		return "ZIP", "#f2edff", "#7049a6"
	case ext == ".json" || contentType == "application/json":
		return "JSON", "#edf2f7", "#53677d"
	case ext == ".xml" || contentType == "application/xml" || contentType == "text/xml":
		return "XML", "#edf2f7", "#53677d"
	case ext == ".html" || ext == ".htm" || contentType == "text/html":
		return "HTML", "#edf2f7", "#53677d"
	case ext == ".txt" || ext == ".md" || strings.HasPrefix(contentType, "text/"):
		return "TXT", "#edf2f7", "#53677d"
	default:
		return "FILE", "#eef2ff", "#4756a9"
	}
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
