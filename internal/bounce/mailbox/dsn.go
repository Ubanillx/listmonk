package mailbox

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/emersion/go-message"
)

// DSNRecipient is one failed recipient parsed from a delivery-status report.
// Only report fields are exposed; body inference is deliberately not used for
// automatic decisions.
type DSNRecipient struct {
	Address     string
	Source      string
	Status      string
	Diagnostic  string
	Action      string
	BounceClass string // hard, soft or unknown
}

// DSN is the strict, outer-message view of a delivery status notification.
type DSN struct {
	IsBounce          bool
	MessageID         string
	From              string
	Subject           string
	Date              string
	Recipients        []DSNRecipient
	OriginalMessageID string
	OriginalHeaders   map[string]string
}

var dsnField = regexp.MustCompile(`(?im)^(Final-Recipient|Original-Recipient|X-Failed-Recipients|Status|Diagnostic-Code|Action):[ \t]*(.*)$`)

var dsnHeaderNames = []string{
	"Message-ID",
	"X-Listmonk-Customer",
	"X-Listmonk-Campaign",
	"To",
}

// ParseDSN parses a delivery status notification. Only the outer message may
// be a report: a DSN nested inside message/rfc822 (a forwarded bounce or an
// attachment) is not treated as a bounce, because reprocessing an old DSN
// would wrongly blocklist a customer.
func ParseDSN(raw []byte) (DSN, error) {
	m, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return DSN{}, err
	}

	out := DSN{
		MessageID:       strings.TrimSpace(m.Header.Get("Message-ID")),
		From:            strings.TrimSpace(m.Header.Get("From")),
		Subject:         strings.TrimSpace(m.Header.Get("Subject")),
		Date:            strings.TrimSpace(m.Header.Get("Date")),
		OriginalHeaders: map[string]string{},
	}

	ct, params, _ := m.Header.ContentType()
	ct = strings.ToLower(strings.TrimSpace(ct))
	topReport := ct == "multipart/report" && strings.EqualFold(strings.TrimSpace(params["report-type"]), "delivery-status")
	directStatus := ct == "message/delivery-status" || ct == "message/global-delivery-status"

	var reports []string
	if directStatus {
		body, err := readDSNPart(m.Body)
		if err != nil {
			return out, err
		}
		reports = append(reports, body)
	} else if ct == "multipart/report" {
		mr := m.MultipartReader()
		if mr != nil {
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					return out, err
				}
				partType, _, _ := part.Header.ContentType()
				switch strings.ToLower(strings.TrimSpace(partType)) {
				case "message/delivery-status", "message/global-delivery-status":
					body, err := readDSNPart(part.Body)
					if err != nil {
						return out, err
					}
					reports = append(reports, body)
				case "message/rfc822", "text/rfc822-headers":
					body, err := readDSNPart(part.Body)
					if err != nil {
						return out, err
					}
					headers := parseEmbeddedHeaders([]byte(body), partType)
					if out.OriginalMessageID == "" {
						out.OriginalMessageID = headers["Message-ID"]
					}
					for _, key := range []string{"X-Listmonk-Customer", "X-Listmonk-Campaign"} {
						if out.OriginalHeaders[key] == "" {
							out.OriginalHeaders[key] = headers[key]
						}
					}
				}
			}
		}
	}

	// Some MTAs put the report fields on the outer headers instead of a
	// message/delivery-status part.
	if len(reports) == 0 {
		var headers strings.Builder
		for _, key := range []string{"Final-Recipient", "Original-Recipient", "X-Failed-Recipients", "Status", "Diagnostic-Code", "Action"} {
			for it := m.Header.FieldsByKey(key); it.Next(); {
				fmt.Fprintf(&headers, "%s: %s\n", key, it.Value())
			}
		}
		if headers.Len() > 0 {
			reports = append(reports, headers.String())
		}
	}

	for _, report := range reports {
		out.Recipients = append(out.Recipients, parseDSNBlocks(report)...)
	}
	out.Recipients = uniqueDSNRecipients(out.Recipients)
	out.IsBounce = topReport || directStatus || len(reports) > 0
	return out, nil
}

func readDSNPart(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxTestMessage+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxTestMessage {
		return "", fmt.Errorf("decoded message too large")
	}
	return string(b), nil
}

func parseEmbeddedHeaders(raw []byte, contentType string) map[string]string {
	out := map[string]string{}
	data := raw
	// text/rfc822-headers has no blank line separating headers from a body.
	if strings.EqualFold(strings.TrimSpace(contentType), "text/rfc822-headers") &&
		!bytes.Contains(data, []byte("\r\n\r\n")) && !bytes.Contains(data, []byte("\n\n")) {
		data = append(append([]byte{}, data...), []byte("\r\n\r\n")...)
	}
	m, err := message.Read(bytes.NewReader(data))
	if err != nil {
		return out
	}
	for _, key := range dsnHeaderNames {
		out[key] = strings.TrimSpace(m.Header.Get(key))
	}
	return out
}

// parseDSNBlocks keeps each delivery-status block separate, so a multi-recipient
// DSN can report a different Status for each recipient. Fields are folded before
// matching because Diagnostic-Code is commonly split over lines.
func parseDSNBlocks(report string) []DSNRecipient {
	normalized := strings.ReplaceAll(report, "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	out := make([]DSNRecipient, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		folded := previewFold.ReplaceAllString(block, " ")
		fields := map[string]string{}
		for _, match := range dsnField.FindAllStringSubmatch(folded, -1) {
			key := strings.ToLower(match[1])
			if fields[key] == "" {
				fields[key] = strings.TrimSpace(match[2])
			}
		}
		address, source := "", ""
		for _, key := range []string{"final-recipient", "original-recipient", "x-failed-recipients"} {
			value := fields[key]
			if value == "" {
				continue
			}
			if i := strings.Index(value, ";"); i >= 0 {
				value = value[i+1:]
			}
			if addr := previewAddress.FindString(value); addr != "" {
				address, source = strings.ToLower(addr), key
				break
			}
		}
		if address == "" {
			continue
		}
		status := strings.TrimSpace(fields["status"])
		action := strings.TrimSpace(fields["action"])
		out = append(out, DSNRecipient{
			Address:     address,
			Source:      source,
			Status:      status,
			Diagnostic:  fields["diagnostic-code"],
			Action:      action,
			BounceClass: classifyDSN(status, action),
		})
	}
	return out
}

func classifyDSN(status, action string) string {
	status = strings.TrimSpace(status)
	switch {
	case strings.HasPrefix(status, "5."):
		return "hard"
	case strings.HasPrefix(status, "4."):
		return "soft"
	case strings.EqualFold(strings.TrimSpace(action), "delayed"):
		return "soft"
	default:
		return "unknown"
	}
}

// uniqueDSNRecipients keeps one row per address. When the same address appears
// more than once, a hard classification wins over soft/unknown.
func uniqueDSNRecipients(in []DSNRecipient) []DSNRecipient {
	seen := map[string]int{}
	out := make([]DSNRecipient, 0, len(in))
	for _, recipient := range in {
		index, ok := seen[recipient.Address]
		if !ok {
			seen[recipient.Address] = len(out)
			out = append(out, recipient)
			continue
		}
		if recipient.BounceClass == "hard" {
			out[index] = recipient
		}
	}
	return out
}
