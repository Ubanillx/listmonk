package manager

import (
	"bytes"
	"cmp"
	"fmt"
	"html"
	"html/template"
	"mime"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	nethtml "golang.org/x/net/html"
)

// LinkMediaAttachments keeps images in place as remote <img> sources and
// represents other library files with links. Raw, one-off system attachments
// are preserved. No media-library binary is sent as a MIME part. lang selects
// the attachment-section labels; unsupported or empty values use English.
func LinkMediaAttachments(body, altBody []byte, attachments []models.Attachment, rootURL string, plain bool, lang string) ([]byte, []byte, []models.Attachment, error) {
	return linkMediaAttachments(body, altBody, attachments, rootURL, plain, true, false, lang, "")
}

func linkMediaAttachments(body, altBody []byte, attachments []models.Attachment, rootURL string, plain, validate, relative bool, lang, slot string) ([]byte, []byte, []models.Attachment, error) {
	hasMedia := false
	for _, a := range attachments {
		if a.MediaID > 0 {
			hasMedia = true
			break
		}
	}
	if !hasMedia {
		if !plain {
			body = stripCampaignAttachmentSlot(body, slot)
			if validate {
				if err := validateRecipientMedia(body, attachments, rootURL); err != nil {
					return nil, nil, nil, err
				}
			}
		}
		return body, altBody, attachments, nil
	}
	root, err := url.Parse(strings.TrimRight(rootURL, "/") + "/")
	if err != nil {
		root = &url.URL{}
	}
	if !relative && (root.Host == "" || (root.Scheme != "https" && root.Scheme != "http")) {
		return nil, nil, nil, fmt.Errorf("a valid public root URL is required for email media")
	}
	linkURL := func(deliveryURL string) string {
		if relative {
			if strings.HasPrefix(deliveryURL, "/") {
				return deliveryURL
			}
			return "/" + deliveryURL
		}
		return strings.TrimRight(rootURL, "/") + "/" + strings.TrimLeft(deliveryURL, "/")
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
		urls[i] = linkURL(a.DeliveryURL)
	}
	if !plain {
		body, err = media.TransformReferences(body, func(ref media.Reference) (string, error) {
			i := linkedMediaIndex(ref.Source, attachments, root)
			if i < 0 || urls[i] == "" {
				return ref.Source, nil
			}
			// An image download link alone does not display the image. Still add
			// an <img> below unless a src/background reference renders it.
			if !isImageAttachment(attachments[i]) || ref.Image {
				used[i] = true
			}
			return urls[i], nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	var extra bytes.Buffer
	var files []linkedFile
	for i, a := range attachments {
		if urls[i] == "" {
			continue
		}
		textLink := "\n" + a.Name + ": " + urls[i] + "\n"
		if plain {
			extra.WriteString(textLink)
		} else if !used[i] {
			if isImageAttachment(a) {
				extra.WriteString(`<p><img src="` + html.EscapeString(urls[i]) + `" alt="` + html.EscapeString(a.Name) + `" style="display:block;max-width:100%;height:auto"></p>`)
			} else {
				viewURL, _ := linkedFileViewURL(a.Name, urls[i])
				files = append(files, linkedFile{
					id: a.MediaID, Name: a.Name, Type: linkedFileType(a),
					DownloadURL: linkedFileDownloadURL(urls[i]), ViewURL: viewURL,
				})
			}
		}
		if len(altBody) > 0 {
			altBody = append(append([]byte(nil), altBody...), textLink...)
		}
	}
	if len(files) > 0 {
		// The test request can arrive in a different order from the saved media graph.
		slices.SortStableFunc(files, func(a, b linkedFile) int { return cmp.Compare(a.id, b.id) })
		language, ok := NormalizeAttachmentLang(lang)
		if !ok {
			language = models.CampaignAttachmentLangDefault
		}
		labels := attachmentLabelsFor(language)
		if err := linkedAttachmentSection.Execute(&extra, struct {
			Language, Title, Count, View, Download string
			Cover                                  linkedFile
			Files                                  []linkedFile
		}{language, labels.title, labels.count(len(files)), labels.view, labels.download, files[0], files[1:]}); err != nil {
			return nil, nil, nil, err
		}
	}
	if extra.Len() == 0 {
		if !plain {
			body = stripCampaignAttachmentSlot(body, slot)
		}
	} else if plain {
		body = append(bytes.Clone(body), extra.Bytes()...)
	} else {
		insert := linkedMediaInsertOffset(body, slot)
		out := make([]byte, 0, len(body)+extra.Len())
		out = append(out, stripCampaignAttachmentSlot(body[:insert], slot)...)
		out = append(out, extra.Bytes()...)
		body = append(out, stripCampaignAttachmentSlot(body[insert:], slot)...)
	}
	if validate && !plain {
		if err := validateRecipientMedia(body, remaining, rootURL); err != nil {
			return nil, nil, nil, err
		}
	}
	return body, altBody, remaining, nil
}

type linkedFile struct {
	id                               int
	Name, Type, DownloadURL, ViewURL string
}

func (f linkedFile) NameURL() string {
	if f.ViewURL != "" {
		return f.ViewURL
	}
	return f.DownloadURL
}

var linkedAttachmentSection = template.Must(template.New("linked-attachments").Parse(`<table class="listmonk-attachments" lang="{{.Language}}" role="presentation" width="100%" align="center" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:525px;margin:28px auto 0;border:0;border-collapse:collapse;table-layout:fixed;font-family:'Helvetica Neue','Segoe UI',Helvetica,Arial,sans-serif;overflow-wrap:anywhere;word-wrap:break-word">
  <tr>
    <td valign="middle" style="border:0;padding:0 8px 12px 0;text-align:left">
      <h3 style="margin:0;color:#262D2D;font-family:Georgia,'Songti SC',serif;font-size:23px;line-height:29px;font-weight:400">{{.Title}}</h3>
    </td>
    <td width="104" align="right" valign="middle" style="width:104px;border:0;padding:0 0 12px;text-align:right;color:#535D57;font-family:inherit;font-size:13px;line-height:19px">{{.Count}}</td>
  </tr>
  <tr>
    <td colspan="2" style="border:0;padding:0;font-family:inherit">
      <table class="listmonk-attachment-cover" role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;margin:0;border:0;border-collapse:separate!important;border-spacing:0;table-layout:fixed;font-family:inherit">
        <tr>
          <td width="24%" align="center" valign="middle" bgcolor="#EFE4CF" style="border:0;padding:12px 6px;text-align:center;background-color:#EFE4CF;border-radius:6px 0 0 6px">
            <table role="presentation" width="60" align="center" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:60px;margin:0 auto;border:0;border-collapse:collapse;table-layout:fixed">
              <tr><td style="border:0;border-top:2px solid #6C5940;padding:9px 0 7px;text-align:left;color:#5C492F;font-family:Consolas,'Courier New',monospace;font-size:18px;line-height:23px;font-weight:700;letter-spacing:.08em">{{.Cover.Type}}</td></tr>
              <tr><td style="border:0;padding:0;text-align:left">
                <table aria-hidden="true" role="presentation" width="40" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:40px;margin:0;border:0;border-collapse:collapse">
                  <tr><td height="2" bgcolor="#9E8563" style="height:2px;border:0;padding:0;font-size:0;line-height:0">&nbsp;</td></tr>
                  <tr><td height="5" style="height:5px;border:0;padding:0;font-size:0;line-height:0">&nbsp;</td></tr>
                  <tr><td height="2" bgcolor="#9E8563" style="height:2px;border:0;padding:0;font-size:0;line-height:0">&nbsp;</td></tr>
                </table>
              </td></tr>
            </table>
          </td>
          <td valign="middle" bgcolor="#FBF7EE" style="border:0;padding:18px 18px 17px;text-align:left;background-color:#FBF7EE;border-radius:0 6px 6px 0;font-family:inherit">
            <div style="font-size:19px;line-height:26px;font-weight:600"><a href="{{.Cover.NameURL}}" style="color:#262D2D;text-decoration:none">{{.Cover.Name}}</a></div>
            <div style="margin-top:10px;font-size:14px;line-height:20px">
              <a href="{{.Cover.DownloadURL}}" aria-label="{{.Download}}: {{.Cover.Name}}" style="display:inline-block;box-sizing:border-box;max-width:100%;padding:12px 15px;margin-right:15px;border-radius:4px;background-color:#0055D4;color:#FFFFFF;font-weight:700;text-decoration:none">{{.Download}}</a>
              {{if .Cover.ViewURL}}<a href="{{.Cover.ViewURL}}" aria-label="{{.View}}: {{.Cover.Name}}" style="display:inline-block;max-width:100%;padding:12px 0;color:#394854;text-decoration:underline">{{.View}}</a>{{end}}
            </div>
          </td>
        </tr>
      </table>
    </td>
  </tr>
  {{range $i, $file := .Files}}
  <tr class="listmonk-attachment-file">
    <td valign="middle" style="border:0;padding:{{if $i}}5px{{else}}13px{{end}} 8px 5px 0;text-align:left;font-family:inherit">
      <div style="font-size:15px;line-height:22px;font-weight:600"><a href="{{$file.NameURL}}" style="color:#262D2D;text-decoration:none">{{$file.Name}}</a></div>
      {{if $file.ViewURL}}<a href="{{$file.ViewURL}}" aria-label="{{$.View}}: {{$file.Name}}" style="display:inline-block;max-width:100%;padding:4px 0;color:#445560;font-size:14px;line-height:20px;text-decoration:underline">{{$.View}}</a>{{end}}
    </td>
    <td align="right" valign="middle" style="border:0;padding:{{if $i}}5px{{else}}13px{{end}} 2px 5px 0;text-align:right;font-family:inherit">
      <a href="{{$file.DownloadURL}}" aria-label="{{$.Download}}: {{$file.Name}}" style="display:inline-block;max-width:100%;padding:9px 0;color:#0055D4;font-size:14px;line-height:20px;font-weight:600;text-decoration:underline">{{$.Download}}</a>
    </td>
  </tr>
  {{end}}
</table>`))

const attachmentSlotPrefix = "listmonk-attachment-tail-"

func attachmentSlotComment(slot string) string {
	if slot == "" {
		return ""
	}
	return "<!--" + slot + "-->"
}

func stripCampaignAttachmentSlot(body []byte, slot string) []byte {
	if slot != "" && bytes.Contains(body, []byte(slot)) {
		return bytes.ReplaceAll(body, []byte(slot), nil)
	}
	return body
}

func linkedMediaInsertOffset(body []byte, slot string) int {
	if slot != "" {
		if start := bytes.Index(body, []byte(slot)); start >= 0 {
			return start
		}
	}
	return emailBodyEnd(body)
}

func placeRenderedAttachmentSlot(body []byte, slot, unsubscribeURL string) []byte {
	offset := renderedAttachmentOffset(body, slot, unsubscribeURL)
	token := []byte(slot)
	out := make([]byte, 0, len(body)+7)
	out = append(out, bytes.ReplaceAll(body[:offset], token, nil)...)
	out = append(out, attachmentSlotComment(slot)...)
	return append(out, bytes.ReplaceAll(body[offset:], token, nil)...)
}

func renderedAttachmentOffset(body []byte, slot, unsubscribeURL string) int {
	doc, elements, documentEnd := parseEmailSource(body, slot)
	if doc == nil {
		return emailBodyEnd(body)
	}
	column, visual := emailContentColumn(doc)
	if column == nil {
		return documentEnd
	}
	end := emailContentEnd(column, visual, elements, documentEnd, unsubscribeURL)
	for node := range column.Descendants() {
		if node.Type != nethtml.CommentNode || !insideEmailContent(node.Parent, column) || !attachmentSlotAllowsBlock(node.Parent) {
			continue
		}
		index, ok := strings.CutPrefix(node.Data, slot+" ")
		if ok {
			if at, err := strconv.Atoi(index); err == nil && at >= 0 && at <= end && bytes.HasPrefix(body[at:], []byte(slot)) {
				return at
			}
		}
	}
	return end
}

func attachmentSlotAllowsBlock(parent *nethtml.Node) bool {
	for ; parent != nil; parent = parent.Parent {
		switch parent.Data {
		case "td", "th", "caption":
			return true
		case "table", "thead", "tbody", "tfoot", "tr", "colgroup":
			return false
		}
	}
	return true
}

func emailBodyEnd(body []byte) int {
	tokens := nethtml.NewTokenizer(bytes.NewReader(body))
	offset, templateDepth := 0, 0
	for kind := tokens.Next(); kind != nethtml.ErrorToken; kind = tokens.Next() {
		start := offset
		offset += len(tokens.Raw())
		switch kind {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			if tag, _ := tokens.TagName(); string(tag) == "template" {
				templateDepth++
			}
		case nethtml.EndTagToken:
			tag, _ := tokens.TagName()
			if templateDepth > 0 {
				if string(tag) == "template" {
					templateDepth--
				}
			} else if string(tag) == "body" || string(tag) == "html" {
				return start
			}
		}
	}
	return len(body)
}

const emailSourceIndexAttr = "data-listmonk-source-offset"

type emailSourceElement struct {
	name       string
	start, end int
}

func parseEmailSource(content []byte, slot string) (*nethtml.Node, []emailSourceElement, int) {
	var annotated bytes.Buffer
	annotated.Grow(len(content))
	var elements []emailSourceElement
	var stack []int
	tokens := nethtml.NewTokenizer(bytes.NewReader(content))
	needle := []byte(slot)
	offset, documentEnd := 0, len(content)
	writeWithoutSlot := func(raw []byte) {
		if len(needle) > 0 && bytes.Contains(raw, needle) {
			raw = bytes.ReplaceAll(raw, needle, nil)
		}
		annotated.Write(raw)
	}
	// Source offsets are only annotations; the authored HTML is never serialized from this tree.
	for kind := tokens.Next(); kind != nethtml.ErrorToken; kind = tokens.Next() {
		start := offset
		raw := tokens.Raw()
		offset += len(raw)
		switch kind {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			tag, _ := tokens.TagName()
			name := string(tag)
			if strings.Contains(" area base br col embed hr img input link meta param source track wbr ", " "+name+" ") {
				break
			}
			stack = append(stack, len(elements))
			elements = append(elements, emailSourceElement{name: name, start: start, end: -1})
			annotated.Write(raw[:len(tag)+1])
			fmt.Fprintf(&annotated, ` %s="%d"`, emailSourceIndexAttr, len(elements)-1)
			writeWithoutSlot(raw[len(tag)+1:])
			continue
		case nethtml.EndTagToken:
			tag, _ := tokens.TagName()
			for i := len(stack) - 1; i >= 0; i-- {
				name := elements[stack[i]].name
				if name == string(tag) {
					for _, index := range stack[i:] {
						elements[index].end = start
					}
					stack = stack[:i]
					if name == "html" {
						documentEnd = start
					}
					break
				}
				if name == "template" {
					break
				}
			}
		case nethtml.TextToken:
			if len(needle) == 0 || (len(stack) > 0 && strings.Contains(" iframe noembed noframes noscript plaintext script style textarea title xmp ",
				" "+elements[stack[len(stack)-1]].name+" ")) {
				break
			}
			// A comment stays in its parsed table cell or row, unlike a foster-parented element.
			for at := bytes.Index(raw, needle); at >= 0; at = bytes.Index(raw, needle) {
				annotated.Write(raw[:at])
				fmt.Fprintf(&annotated, "<!--%s %d-->", slot, start+at)
				start += at + len(needle)
				raw = raw[at+len(needle):]
			}
		}
		writeWithoutSlot(raw)
	}
	doc, _ := nethtml.Parse(&annotated)
	return doc, elements, documentEnd
}

func emailSourceIndex(node *nethtml.Node, elements []emailSourceElement) int {
	index, err := strconv.Atoi(emailHTMLAttr(node, emailSourceIndexAttr))
	if err != nil || index < 0 || index >= len(elements) || elements[index].name != node.Data {
		return -1
	}
	return index
}

func emailSourceOffsets(node *nethtml.Node, elements []emailSourceElement) (int, int) {
	index := emailSourceIndex(node, elements)
	if index < 0 {
		return -1, -1
	}
	return elements[index].start, elements[index].end
}

func emailContentColumn(doc *nethtml.Node) (*nethtml.Node, bool) {
	var body *nethtml.Node
	for node := range doc.Descendants() {
		if node.Type == nethtml.ElementNode && node.Data == "body" {
			body = node
			break
		}
	}
	if body == nil {
		return nil, false
	}
	var main *nethtml.Node
	for child := range body.Descendants() {
		if !isEmailMain(child) || !insideEmailContent(child, body) || !attachmentSlotAllowsBlock(child) {
			continue
		}
		if main != nil {
			main = nil
			break
		}
		main = child
	}
	if main != nil {
		return main, false
	}
	if cell := visualEmailColumn(body); cell != nil && insideEmailContent(cell, body) {
		return cell, true
	}
	return body, false
}

func emailContentEnd(column *nethtml.Node, visual bool, elements []emailSourceElement, documentEnd int, unsubscribeURL string) int {
	var footer *nethtml.Node
	for child := range column.Descendants() {
		if isEmailFooter(child) && !isHiddenEmailElement(child) && insideEmailContent(child.Parent, column) && attachmentSlotAllowsBlock(child.Parent) {
			footer = child
		}
	}
	if footer == nil && visual && unsubscribeURL != "" {
		for last := column.LastChild; last != nil; last = last.PrevSibling {
			if last.Type != nethtml.ElementNode || isHiddenEmailElement(last) {
				continue
			}
			for child := range last.Descendants() {
				if child.Type == nethtml.ElementNode && child.Data == "a" && emailHTMLAttr(child, "href") == unsubscribeURL {
					footer = last
					break
				}
			}
			break
		}
	}
	if footer != nil {
		if start, _ := emailSourceOffsets(footer, elements); start >= 0 {
			return start
		}
	}
	start, _ := emailSourceOffsets(column, elements)
	end := documentEnd
	for node := column; node != nil; node = node.Parent {
		if _, closeAt := emailSourceOffsets(node, elements); closeAt >= 0 {
			end = min(end, closeAt)
		}
		// A following sibling can close a cell or row without a written end tag.
		for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
			if at, _ := emailSourceOffsets(sibling, elements); at > start {
				end = min(end, at)
			}
		}
	}
	return end
}

func isEmailMain(node *nethtml.Node) bool {
	return node.Type == nethtml.ElementNode && (node.Data == "main" || emailHTMLAttr(node, "role") == "main")
}

func insideEmailContent(node, column *nethtml.Node) bool {
	for ; node != nil; node = node.Parent {
		switch node.Data {
		case "a", "aside", "button", "header", "select", "template":
			return false
		}
		if node.Namespace != "" || isEmailFooter(node) || isHiddenEmailElement(node) {
			return false
		}
		if node == column {
			return true
		}
	}
	return false
}

func isEmailFooter(node *nethtml.Node) bool {
	return node.Type == nethtml.ElementNode && (node.Data == "footer" || emailHTMLAttr(node, "role") == "contentinfo" ||
		slices.Contains(strings.Fields(emailHTMLAttr(node, "class")), "footer"))
}

func isHiddenEmailElement(node *nethtml.Node) bool {
	for _, attr := range node.Attr {
		if attr.Key == "hidden" || (attr.Key == "aria-hidden" && strings.EqualFold(attr.Val, "true")) {
			return true
		}
	}
	for style := range strings.SplitSeq(strings.ToLower(emailHTMLAttr(node, "style")), ";") {
		property, value, _ := strings.Cut(style, ":")
		value, _, _ = strings.Cut(value, "!")
		property, value = strings.TrimSpace(property), strings.TrimSpace(value)
		if (property == "display" && value == "none") || (property == "visibility" && value == "hidden") {
			return true
		}
	}
	return false
}

func visualEmailColumn(body *nethtml.Node) *nethtml.Node {
	soleChild := func(parent *nethtml.Node) *nethtml.Node {
		var only *nethtml.Node
		for node := range parent.ChildNodes() {
			if node.Type == nethtml.CommentNode || (node.Type == nethtml.TextNode && strings.TrimSpace(node.Data) == "") {
				continue
			}
			if only != nil || node.Type != nethtml.ElementNode {
				return nil
			}
			only = node
		}
		return only
	}
	canvas := soleChild(body)
	if canvas != nil && canvas.Data == "div" {
		canvas = soleChild(canvas)
	}
	if canvas == nil || canvas.Data != "table" || emailHTMLAttr(canvas, "role") != "presentation" || emailHTMLAttr(canvas, "align") != "center" {
		return nil
	}
	group := soleChild(canvas)
	if group == nil || group.Data != "tbody" {
		return nil
	}
	row := soleChild(group)
	if row == nil || row.Data != "tr" {
		return nil
	}
	cell := soleChild(row)
	if cell != nil && cell.Data == "td" {
		return cell
	}
	return nil
}

func emailHTMLAttr(node *nethtml.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
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

func linkedFileType(attachment models.Attachment) string {
	ext := strings.ToLower(path.Ext(attachment.Name))
	contentType, _, _ := mime.ParseMediaType(attachment.Header.Get("Content-Type"))
	switch {
	case ext == ".pdf" || contentType == "application/pdf":
		return "PDF"
	case ext == ".doc" || ext == ".docx" || ext == ".docm" || contentType == "application/msword" || strings.Contains(contentType, "wordprocessingml"):
		return "DOC"
	case ext == ".csv" || contentType == "text/csv":
		return "CSV"
	case ext == ".xlsx":
		return "XLSX"
	case ext == ".xls" || ext == ".xlsm" || strings.Contains(contentType, "spreadsheetml") || contentType == "application/vnd.ms-excel":
		return "XLS"
	case ext == ".ppt" || ext == ".pptx" || ext == ".pptm" || contentType == "application/vnd.ms-powerpoint" || strings.Contains(contentType, "presentationml"):
		return "PPT"
	case ext == ".rar":
		return "RAR"
	case ext == ".7z":
		return "7Z"
	case ext == ".tar" || ext == ".gz":
		return "TAR"
	case ext == ".zip" || strings.Contains(contentType, "zip") || strings.Contains(contentType, "compressed"):
		return "ZIP"
	case ext == ".json" || contentType == "application/json":
		return "JSON"
	case ext == ".xml" || contentType == "application/xml" || contentType == "text/xml":
		return "XML"
	case ext == ".html" || ext == ".htm" || contentType == "text/html":
		return "HTML"
	case ext == ".txt" || ext == ".md" || strings.HasPrefix(contentType, "text/"):
		return "TXT"
	default:
		return "FILE"
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
