package mailbox

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/knadh/go-pop3"
	"github.com/knadh/listmonk/models"
)

// POP represents a POP mailbox.
type POP struct {
	opt    Opt
	client *pop3.Client
	lo     *log.Logger
}

// handoffTimeout bounds how long one message waits for the bounce processor.
// The processor writes to the database synchronously, so a full queue means the
// database is behind; waiting a while absorbs a burst, and giving up leaves the
// message on the server for the next scan instead of losing it. It is a variable
// so tests can shrink it.
var handoffTimeout = 30 * time.Second

type bounceHeaders struct {
	Header string
	Regexp *regexp.Regexp
}

type bounceMeta struct {
	From           string   `json:"from"`
	Subject        string   `json:"subject"`
	MessageID      string   `json:"message_id"`
	DeliveredTo    string   `json:"delivered_to"`
	Received       []string `json:"received"`
	ClassifyReason string   `json:"classify_reason"`
}

var (
	// CustomerList of header to look for in the e-mail body, regexp to fall back to if the header is empty.
	headerLookups = []bounceHeaders{
		{models.EmailHeaderCampaignUUID, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderCampaignUUID + `:\s+?)([a-z0-9\-]{36})`)},
		{models.EmailHeaderCustomerUUID, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderCustomerUUID + `:\s+?)([a-z0-9\-]{36})`)},
		{models.EmailHeaderDate, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderDate + `:\s+?)([\w,\,\ ,:,+,-]*(?:\(?:\w*\))?)`)},
		{models.EmailHeaderFrom, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderFrom + `:\s+?)(.*)`)},
		{models.EmailHeaderSubject, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderSubject + `:\s+?)(.*)`)},
		{models.EmailHeaderMessageId, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderMessageId + `:\s+?)(.*)`)},
		{models.EmailHeaderDeliveredTo, regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderDeliveredTo + `:\s+?)(.*)`)},
	}

	reHdrReceived = regexp.MustCompile(`(?m)(?:^` + models.EmailHeaderReceived + `:\s+?)(.*)`)

	// SMTP status code (5.x.x or 4.x.x) to classify hard/soft bounces.
	reSMTPStatus = regexp.MustCompile(`(?m)(?i)^(?:Status:\s*)?(?:\d{3}\s+)?([45]\.\d+\.\d+)`)

	// CustomerList of (conventional) strings to guess hard bounces.
	reHardBounce = regexp.MustCompile(`(?i)(NXDOMAIN|user unknown|address not found|mailbox not found|address.*reject|does not exist|` +
		`invalid recipient|no such user|recipient.*invalid|undeliverable|permanent.*failure|permanent.*error|` +
		`bad.*address|unknown.*user|account.*disabled|address.*disabled)`)
)

// NewPOP returns a new instance of the POP mailbox client.
func NewPOP(opt Opt, lo *log.Logger) *POP {
	clientOpt := pop3.Opt{Host: opt.Host, Port: opt.Port, TLSEnabled: opt.TLSEnabled, TLSSkipVerify: opt.TLSSkipVerify}
	if opt.StartTLS {
		clientOpt.TLSEnabled = false
		clientOpt.Dialer = startTLSDialer{opt: opt}
	}
	return &POP{
		opt:    opt,
		client: pop3.New(clientOpt),
		lo:     lo,
	}
}

// Scan scans the mailbox and pushes the downloaded messages into the given channel.
// The messages that are downloaded are deleted from the server. If limit > 0,
// all messages on the server are downloaded and deleted.
func (p *POP) Scan(limit int, ch chan models.Bounce) error {
	c, err := p.client.NewConn()
	if err != nil {
		return err
	}
	defer c.Quit()

	// Authenticate.
	if p.opt.AuthProtocol != "none" {
		if err := c.Auth(p.opt.Username, p.opt.Password); err != nil {
			return err
		}
	}

	// Get the total number of messages on the server.
	count, _, err := c.Stat()
	if err != nil {
		return err
	}

	// No messages.
	if count == 0 {
		return nil
	}

	if limit > 0 && count > limit {
		count = limit
	}

	// Download messages. accepted records the IDs whose bounce reached the
	// processor; only those are deleted from the server.
	// deletable records the messages that may be removed from the server: those
	// whose bounce reached the processor, plus those that yielded no bounce at
	// all. A message that could not be read or parsed is removed as before, so a
	// single malformed message cannot keep the mailbox from draining; what must
	// never happen is deleting a message whose bounce was dropped because the
	// processor was behind.
	var deletable []int
	for id := 1; id <= count; id++ {
		// Retrieve the raw bytes of the message.
		b, err := c.RetrRaw(id)
		if err != nil {
			p.lo.Printf("error retrieving bounce message %d: %v", id, err)
			deletable = append(deletable, id)
			continue
		}

		// Parse the message.
		m, err := message.Read(b)
		if err != nil {
			p.lo.Printf("error parsing bounce message %d: %v", id, err)
			deletable = append(deletable, id)
			continue
		}

		h := m

		// If this is a multipart message, find the last part.
		if mr := m.MultipartReader(); mr != nil {
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					p.lo.Printf("error reading multipart bounce message %d: %v", id, err)
					continue
				}
				h = part
			}
		}

		// Reset the "unread portion" pointer of the message buffer.
		// If you don't do this, you can't read the entire body because the pointer will not point to the beginning.
		b, _ = c.RetrRaw(id)

		// Lookup headers in the e-mail. If a header isn't found, fall back to regexp lookups.
		hdr := make(map[string]string, 7)
		for _, l := range headerLookups {
			v := h.Header.Get(l.Header)

			// Not in the header. Try regexp.
			if v == "" {
				if m := l.Regexp.FindAllSubmatch(b.Bytes(), -1); m != nil {
					v = string(m[len(m)-1][1])
				}
			}

			hdr[l.Header] = strings.TrimSpace(v)
		}

		// Received is a []string header.
		msgReceived := h.Header.Map()[models.EmailHeaderReceived]
		if len(msgReceived) == 0 {
			if u := reHdrReceived.FindAllSubmatch(b.Bytes(), -1); u != nil {
				for i := range u {
					msgReceived = append(msgReceived, string(u[i][1]))
				}
			}
		}

		date, _ := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", hdr[models.EmailHeaderDate])
		if date.IsZero() {
			date = time.Now()
		}

		// Classify the bounce type based on message content.
		bounceType, bounceReason := classifyBounce(b.Bytes())

		// Additional bounce e-mail metadata.
		meta, _ := json.Marshal(bounceMeta{
			From:           hdr[models.EmailHeaderFrom],
			Subject:        hdr[models.EmailHeaderSubject],
			MessageID:      hdr[models.EmailHeaderMessageId],
			DeliveredTo:    hdr[models.EmailHeaderDeliveredTo],
			Received:       msgReceived,
			ClassifyReason: bounceReason,
		})

		// Hand the bounce to the processor. The send blocks (up to a timeout)
		// rather than dropping when the queue is full, and the message is only
		// deleted from the server once the processor has accepted it: a slow
		// database used to silently discard the evidence and then delete the
		// source message anyway.
		bounce := models.Bounce{
			Type:         bounceType,
			CampaignUUID: hdr[models.EmailHeaderCampaignUUID],
			CustomerUUID: hdr[models.EmailHeaderCustomerUUID],
			Source:       p.opt.Host,
			CreatedAt:    date,
			Meta:         meta,
		}
		select {
		case ch <- bounce:
			deletable = append(deletable, id)
		case <-time.After(handoffTimeout):
			// Stop the scan and leave this and the remaining messages in the
			// mailbox for the next round. They are not deleted.
			p.lo.Printf("timeout handing bounce message %d to the processor; leaving it and the remaining messages in the mailbox", id)
			return p.deleteProcessed(c, deletable)
		}
	}

	return p.deleteProcessed(c, deletable)
}

// deleteProcessed marks the given messages for deletion, so a scan that stops
// early cannot remove a message whose bounce was never handled.
func (p *POP) deleteProcessed(c *pop3.Conn, ids []int) error {
	for _, id := range ids {
		if err := c.Dele(id); err != nil {
			return err
		}
	}

	return nil
}

// classifyBounce analyzes the bounce message content and determines if it's a hard or soft bounce.
// It checks SMTP status codes, diagnostic headers, and bounce keywords (using string heuristics).
// soft is the default preference.
// Returns the bounce type and a classification reason containing context about what matched.
func classifyBounce(b []byte) (string, string) {
	if matches := reSMTPStatus.FindAllSubmatch(b, -1); matches != nil {
		for _, m := range matches {
			if len(m) >= 2 && len(m[0]) > 1 {
				// Full status code (e.g., "5.1.1").
				status := m[1]

				// 5.x.x is hard bounce.
				if status[0] == '5' {
					return models.BounceTypeHard, fmt.Sprintf("smtp_status=%s", status)
				}

				// 4.x.x  is soft bounce.
				if status[0] == '4' {
					return models.BounceTypeSoft, fmt.Sprintf("smtp_status=%s", status)
				}
			}
		}
	}

	// Check for explicit hard bounce keywords.
	if match := reHardBounce.FindSubmatch(b); match != nil {
		return models.BounceTypeHard, fmt.Sprintf("body_match=%s", match[1])
	}

	return models.BounceTypeSoft, "default"
}
