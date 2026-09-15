package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/knadh/go-pop3"
	"github.com/knadh/listmonk/models"
)

type replyForwardSource struct {
	RuleID       int    `db:"rule_id"`
	MailboxID    int    `db:"mailbox_id"`
	Organization int    `db:"organization_id"`
	Email        string `db:"email"`
	Username     string `db:"username"`
	Password     string `db:"password"`
	Host         string `db:"imap_host"`
	Port         int    `db:"imap_port"`
	Folder       string `db:"folder"`
	TargetEmail  string `db:"target_email"`
	TargetUserID int    `db:"target_user_id"`
}

// Retry policy for one forwarded customer reply.
//
// Forwarding hands the message to the in-process queue, so a queue backlog, a
// manager that is shutting down or a restart can fail the hand-off without the
// reply ever having been sent. Those failures must be retried, while a reply
// that can never be delivered must stop being retried and stay visible as a
// terminal failure. Both are decided by the row in reply_forward_messages:
//
//	pending  + recent updated_at  -> a live claim, owned by one scan round
//	pending  + stale updated_at   -> the owner died mid-attempt; retryable
//	failed   + attempts < max     -> retried with exponential backoff
//	failed   + attempts >= max    -> terminal, never claimed again
//	forwarded                     -> done, never claimed again
//
// Declared as variables so tests can shrink the intervals.
var (
	// replyForwardMaxAttempts is the total number of send attempts (the first
	// attempt included) before a reply is left terminally failed.
	replyForwardMaxAttempts = 5

	// replyForwardRetryBackoff is the delay before the second attempt. It
	// doubles per failed attempt up to replyForwardRetryMaxBackoff.
	replyForwardRetryBackoff    = time.Minute
	replyForwardRetryMaxBackoff = 16 * time.Minute

	// replyForwardClaimLease bounds how long one attempt owns its row. It is
	// several orders of magnitude longer than an attempt can take (PushMessage
	// gives up after 3s), so a lease can only expire when its owner died
	// between claiming the row and recording the outcome.
	replyForwardClaimLease = 5 * time.Minute
)

// runReplyForwarder keeps retained 263 customer-reply mailboxes usable after
// a member leaves. It deliberately runs independently of bounce processing:
// reply messages are never deleted from the source mailbox.
func runReplyForwarder(a *App) {
	if a == nil || a.db == nil || a.manager == nil {
		return
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		a.scanReplyForwardRules()
		<-ticker.C
	}
}

func (a *App) scanReplyForwardRules() {
	var sources []replyForwardSource
	if err := a.db.Select(&sources, `
		SELECT r.id AS rule_id, m.id AS mailbox_id, r.organization_id,
			m.email, m.username, m.password, m.imap_host, m.imap_port, m.folder,
			r.target_email, r.target_user_id
		FROM reply_forward_rules r
		JOIN reply_mailboxes m ON m.id = r.reply_mailbox_id
		WHERE r.status = 'active' AND m.status = 'retained'
		ORDER BY r.id`); err != nil {
		a.log.Printf("error loading reply forwarding rules: %v", err)
		return
	}
	for _, source := range sources {
		if err := a.scanOneReplyForwardSource(source); err != nil {
			a.log.Printf("reply forwarding rule %d failed: %v", source.RuleID, err)
			_, _ = a.db.Exec(`UPDATE reply_forward_rules SET last_error = $2, updated_at = NOW() WHERE id = $1`, source.RuleID, err.Error())
		}
	}
}

func (a *App) scanOneReplyForwardSource(source replyForwardSource) error {
	host, port := source.Host, source.Port
	if strings.HasPrefix(strings.ToLower(host), "imap.") {
		host = "pop." + strings.TrimPrefix(host, "imap.")
		port = 995
	}
	if port == 0 {
		port = 995
	}
	client := pop3.New(pop3.Opt{Host: host, Port: port, TLSEnabled: true})
	conn, err := client.NewConn()
	if err != nil {
		return err
	}
	defer conn.Quit()
	if err := conn.Auth(source.Username, source.Password); err != nil {
		return err
	}
	count, _, err := conn.Stat()
	if err != nil {
		return err
	}
	for id := 1; id <= count; id++ {
		raw, err := conn.RetrRaw(id)
		if err != nil {
			continue
		}
		if err := a.forwardOneReply(source, raw.Bytes()); err != nil {
			a.log.Printf("reply forwarding rule %d message %d failed: %v", source.RuleID, id, err)
		}
	}
	_, _ = a.db.Exec(`UPDATE reply_mailboxes SET last_sync_at = NOW(), last_sync_error = '', updated_at = NOW() WHERE id = $1`, source.MailboxID)
	return nil
}

// forwardOneReply forwards one source message exactly once.
//
// The dedupe row is claimed by a single statement that is also the duplicate
// guard: (rule_id, message_key) admits one row per source message, and the
// claim is committed before the message is pushed, so any other scan round (in
// this process or another) sees a live claim and cannot send the same reply.
// The outcome is recorded after the push returns. Because a claim is therefore
// the only thing that authorises a send, a claim that outlived its lease means
// the previous attempt died mid-flight and nothing else holds it, so the next
// round may take it over — that is how a failed or interrupted forward becomes
// retryable without ever letting a finished one be sent twice.
func (a *App) forwardOneReply(source replyForwardSource, raw []byte) error {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if strings.EqualFold(entity.Header.Get("X-Listmonk-Forwarded-Reply"), "true") || strings.EqualFold(entity.Header.Get("Auto-Submitted"), "auto-replied") {
		return nil
	}
	from := entity.Header.Get("From")
	subject := entity.Header.Get("Subject")
	messageID := entity.Header.Get("Message-ID")
	hash := sha256.Sum256(raw)
	key := messageID + ":" + hex.EncodeToString(hash[:])

	// Insert the dedupe row, or re-claim an existing one that is allowed to be
	// retried. ON CONFLICT DO UPDATE with a WHERE clause returns no row when the
	// conflicting reply is already forwarded, is owned by a live claim, or has
	// exhausted its attempts, which preserves the old "DO NOTHING" behaviour for
	// every case that must not be sent again.
	var (
		eventID  int64
		attempts int
	)
	err = a.db.QueryRow(`
		INSERT INTO reply_forward_messages
			(rule_id, message_key, from_email, subject, status, attempts, received_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', 1, NOW(), NOW())
		ON CONFLICT (rule_id, message_key) DO UPDATE
		SET status = 'pending',
		    attempts = reply_forward_messages.attempts + 1,
		    updated_at = NOW()
		WHERE (
		        -- A recorded failure is retried with exponential backoff until
		        -- the attempt ceiling is reached.
		        reply_forward_messages.status = 'failed'
		        AND reply_forward_messages.attempts < $5
		        AND reply_forward_messages.updated_at <= NOW() - INTERVAL '1 second' *
		            (LEAST(power(2, reply_forward_messages.attempts - 1), $6::float8) * $7::float8)
		      )
		   OR (
		        -- A claim that outlived its lease was interrupted mid-flight.
		        reply_forward_messages.status = 'pending'
		        AND reply_forward_messages.updated_at <= NOW() - INTERVAL '1 second' * $8::float8
		      )
		RETURNING id, attempts`, source.RuleID, key, from, subject,
		replyForwardMaxAttempts, replyForwardBackoffCap(), replyForwardRetryBackoff.Seconds(), replyForwardClaimLease.Seconds()).Scan(&eventID, &attempts)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if attempts > 1 {
		a.log.Printf("retrying reply forward for rule %d (%s), attempt %d of %d", source.RuleID, key, attempts, replyForwardMaxAttempts)
	}

	body, contentType := inboundBody(entity)
	if len(body) == 0 {
		body = []byte("客户回信已收到，原始邮件请查看附件。")
		contentType = models.CampaignContentTypePlain
	}
	// Attach the untouched source message so no MIME part or attachment is
	// lost by the decoded body conversion.
	msg := models.Message{
		From:        a.emailMsgr.DefaultFromEmail(),
		To:          []string{source.TargetEmail},
		Subject:     "[客户回信] " + subject,
		ContentType: contentType,
		Body:        body,
		Messenger:   "email",
		OwnerUserID: 0,
		Attachments: []models.Attachment{{Name: "original-reply.eml", Content: raw}},
		Headers: textproto.MIMEHeader{
			"Reply-To":                   []string{extractAddress(from)},
			"X-Listmonk-Forwarded-Reply": []string{"true"},
			"X-Listmonk-Organization":    []string{fmt.Sprintf("%d", source.Organization)},
			"X-Listmonk-Source-Mailbox":  []string{source.Email},
		},
	}
	if err := a.manager.PushMessage(msg); err != nil {
		a.recordReplyForwardFailure(eventID, attempts, err)
		orgID := int64(source.Organization)
		a.recordBackgroundAuditResult("system", "reply.forward_failed", "reply_forward_message", fmt.Sprintf("%d", eventID), &orgID, "failed", "push_message_failed", map[string]any{
			"rule_id":    source.RuleID,
			"mailbox_id": source.MailboxID,
			"attempt":    attempts,
			"terminal":   attempts >= replyForwardMaxAttempts,
		})
		if attempts >= replyForwardMaxAttempts {
			a.log.Printf("reply forwarding rule %d gives up on %s after %d attempts: %v", source.RuleID, key, attempts, err)
		}
		return err
	}
	// The reply is only recorded as forwarded after the manager accepted the
	// message, so a 'forwarded' row always means the message was handed over and
	// is never claimed again.
	recorded, err := a.recordReplyForwardSuccess(eventID, attempts)
	if err != nil {
		return err
	}
	if !recorded {
		// A newer attempt re-claimed this row (only possible when the lease of
		// this one expired) and already recorded the outcome.
		a.log.Printf("reply forward %d was taken over by a newer attempt; not counting it twice", eventID)
		return nil
	}
	_, _ = a.db.Exec(`UPDATE reply_mailboxes SET forward_count = forward_count + 1, updated_at = NOW() WHERE id = $1`, source.MailboxID)
	_, _ = a.db.Exec(`UPDATE reply_forward_rules SET last_forward_at = NOW(), last_error = '', updated_at = NOW() WHERE id = $1`, source.RuleID)
	orgID := int64(source.Organization)
	a.recordBackgroundAudit("system", "reply.forwarded", "reply_forward_message", fmt.Sprintf("%d", eventID), &orgID, map[string]any{
		"rule_id":    source.RuleID,
		"mailbox_id": source.MailboxID,
		"attempt":    attempts,
	})
	return nil
}

// recordReplyForwardFailure marks a claimed attempt as failed so the next round
// can retry it once the backoff has elapsed. The update is fenced on the claim
// it belongs to, so an attempt that was overtaken after its lease expired cannot
// overwrite the row the newer attempt is working on. If the update itself fails,
// the row stays 'pending' and the lease recovers it instead of losing the reply.
func (a *App) recordReplyForwardFailure(eventID int64, attempts int, cause error) {
	res, err := a.db.Exec(`UPDATE reply_forward_messages
		SET status = 'failed', last_error = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'pending' AND attempts = $3`, eventID, cause.Error(), attempts)
	if err != nil {
		a.log.Printf("error recording the failed reply forward %d: %v", eventID, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a.log.Printf("reply forward %d was taken over by a newer attempt; not recording this failure", eventID)
	}
}

// recordReplyForwardSuccess records a completed forward. It reports false when
// the row no longer belongs to this attempt, which keeps forward_count and the
// rule bookkeeping consistent with the row that actually reached 'forwarded'.
func (a *App) recordReplyForwardSuccess(eventID int64, attempts int) (bool, error) {
	res, err := a.db.Exec(`UPDATE reply_forward_messages
		SET status = 'forwarded', forwarded_at = NOW(), updated_at = NOW(), last_error = ''
		WHERE id = $1 AND status = 'pending' AND attempts = $2`, eventID, attempts)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// replyForwardBackoffCap returns the multiplier at which the retry backoff stops
// growing, expressed as a factor of replyForwardRetryBackoff.
func replyForwardBackoffCap() float64 {
	if replyForwardRetryBackoff <= 0 {
		return 1
	}
	cap := replyForwardRetryMaxBackoff.Seconds() / replyForwardRetryBackoff.Seconds()
	if cap < 1 {
		return 1
	}
	return cap
}

func inboundBody(entity *message.Entity) ([]byte, string) {
	var htmlBody, textBody []byte
	_ = entity.Walk(func(_ []int, part *message.Entity, err error) error {
		if err != nil {
			return nil
		}
		mediaType, params, _ := part.Header.ContentType()
		if !strings.HasPrefix(mediaType, "text/") || strings.EqualFold(part.Header.Get("Content-Disposition"), "attachment") {
			return nil
		}
		b, readErr := io.ReadAll(part.Body)
		if readErr != nil {
			return nil
		}
		if strings.EqualFold(mediaType, "text/html") && len(htmlBody) == 0 {
			htmlBody = b
		} else if strings.EqualFold(mediaType, "text/plain") && len(textBody) == 0 {
			textBody = b
		}
		_ = params
		return nil
	})
	if len(htmlBody) > 0 {
		return htmlBody, models.CampaignContentTypeHTML
	}
	return textBody, models.CampaignContentTypePlain
}

func extractAddress(from string) string {
	if addr, err := mail.ParseAddress(from); err == nil {
		return addr.Address
	}
	return strings.TrimSpace(from)
}
