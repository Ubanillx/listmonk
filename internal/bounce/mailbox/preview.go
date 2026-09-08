package mailbox

import (
	"bytes"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message"
)

type PreviewRecipient struct {
	Address string `json:"address"`
	Source  string `json:"source"`
	Status  string `json:"status,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
type MessagePreview struct {
	From       string             `json:"from"`
	Subject    string             `json:"subject"`
	Date       string             `json:"date"`
	DateSource string             `json:"date_source"`
	MessageID  string             `json:"message_id"`
	IsBounce   bool               `json:"is_bounce"`
	Recipients []PreviewRecipient `json:"recipients"`
	BounceType string             `json:"bounce_type,omitempty"`
	Reason     string             `json:"reason,omitempty"`
}

var previewField = regexp.MustCompile(`(?im)^(Final-Recipient|Original-Recipient|X-Failed-Recipients|Status|Diagnostic-Code):[ \t]*(.*)$`)
var previewFold = regexp.MustCompile(`\n[ \t]+`)
var previewAddress = regexp.MustCompile(`[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
var previewFailureLine = regexp.MustCompile(`(?im)^.*(?:failed|undeliverable|unknown user|user unknown|recipient.*reject|delivery.*failure|无法投递|投递失败|退信).*$`)

// parsePreview keeps the outer message's sender/subject separate from the
// attached original message. To/From on the original are not failed recipients.
func parsePreview(raw []byte) (*MessagePreview, error) {
	m, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	from, _ := m.Header.Text("From")
	subject, _ := m.Header.Text("Subject")
	out := &MessagePreview{From: from, Subject: subject, MessageID: m.Header.Get("Message-ID"), Recipients: []PreviewRecipient{}}
	for it := m.Header.FieldsByKey("Received"); it.Next(); {
		v := it.Value()
		if i := strings.LastIndex(v, ";"); i >= 0 {
			if d, e := mail.ParseDate(strings.TrimSpace(v[i+1:])); e == nil {
				out.Date = d.Format(time.RFC3339)
				out.DateSource = "received"
				break
			}
		}
	}
	if out.Date == "" {
		if d, e := mail.ParseDate(m.Header.Get("Date")); e == nil {
			out.Date = d.Format(time.RFC3339)
			out.DateSource = "date"
		}
	}
	var textParts []string
	var reports []string
	var walk func(*message.Entity, int) error
	walk = func(e *message.Entity, depth int) error {
		if depth > 20 {
			return fmt.Errorf("MIME nesting exceeds 20 levels")
		}
		ct, _, _ := e.Header.ContentType()
		if ct == "message/rfc822" || ct == "text/rfc822-headers" {
			return nil
		}
		if mr := e.MultipartReader(); mr != nil {
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err = walk(p, depth+1); err != nil {
					return err
				}
			}
		}
		if ct != "message/delivery-status" && ct != "message/global-delivery-status" && ct != "text/plain" && ct != "text/html" {
			return nil
		}
		b, err := io.ReadAll(io.LimitReader(e.Body, maxTestMessage+1))
		if err != nil {
			return err
		}
		if len(b) > maxTestMessage {
			return fmt.Errorf("decoded message too large")
		}
		if strings.Contains(ct, "delivery-status") {
			reports = append(reports, string(b))
		} else {
			textParts = append(textParts, string(b))
		}
		return nil
	}
	if err = walk(m, 0); err != nil {
		return out, err
	}
	// Each delivery-status block belongs to one recipient; preserve its reason.
	for _, report := range reports {
		for _, block := range strings.Split(strings.ReplaceAll(report, "\r\n", "\n"), "\n\n") {
			out.Recipients = append(out.Recipients, parseRecipientFields(block)...)
		}
	}
	if len(out.Recipients) == 0 {
		var headers strings.Builder
		for _, key := range []string{"Final-Recipient", "Original-Recipient", "X-Failed-Recipients", "Status", "Diagnostic-Code"} {
			for it := m.Header.FieldsByKey(key); it.Next(); {
				fmt.Fprintf(&headers, "%s: %s\n", key, it.Value())
			}
		}
		out.Recipients = parseRecipientFields(headers.String())
	}
	body := strings.Join(textParts, "\n")
	if len(out.Recipients) == 0 {
		out.Recipients = parseRecipientFields(body)
	}
	if len(out.Recipients) == 0 {
		for _, line := range previewFailureLine.FindAllString(body, -1) {
			for _, addr := range previewAddress.FindAllString(line, -1) {
				out.Recipients = append(out.Recipients, PreviewRecipient{Address: addr, Source: "body_inferred", Reason: strings.TrimSpace(line)})
			}
		}
	}
	seen := map[string]bool{}
	unique := []PreviewRecipient{}
	for _, r := range out.Recipients {
		if !seen[strings.ToLower(r.Address)] {
			seen[strings.ToLower(r.Address)] = true
			unique = append(unique, r)
		}
	}
	out.Recipients = unique
	out.IsBounce = len(unique) > 0 || len(reports) > 0
	if out.IsBounce {
		out.BounceType = "unknown"
		for _, r := range unique {
			if strings.HasPrefix(r.Status, "5.") {
				out.BounceType = "hard"
			} else if strings.HasPrefix(r.Status, "4.") && out.BounceType != "hard" {
				out.BounceType = "soft"
			}
			if out.Reason == "" {
				out.Reason = r.Reason
			}
		}
		if out.Reason == "" {
			out.Reason = strings.TrimSpace(previewFailureLine.FindString(body))
		}
	}
	return out, nil
}

func parseRecipientFields(s string) []PreviewRecipient {
	s = previewFold.ReplaceAllString(strings.ReplaceAll(s, "\r\n", "\n"), " ")
	fields := map[string][]string{}
	for _, m := range previewField.FindAllStringSubmatch(s, -1) {
		key := strings.ToLower(m[1])
		fields[key] = append(fields[key], strings.TrimSpace(m[2]))
	}
	first := func(k string) string {
		if len(fields[k]) > 0 {
			return fields[k][0]
		}
		return ""
	}
	for _, key := range []string{"final-recipient", "original-recipient", "x-failed-recipients"} {
		var recipients []PreviewRecipient
		for _, v := range fields[key] {
			if i := strings.Index(v, ";"); i >= 0 {
				v = v[i+1:]
			}
			for _, addr := range previewAddress.FindAllString(v, -1) {
				recipients = append(recipients, PreviewRecipient{Address: addr, Source: key, Status: first("status"), Reason: first("diagnostic-code")})
			}
		}
		if len(recipients) > 0 {
			return recipients
		}
	}
	return nil
}
