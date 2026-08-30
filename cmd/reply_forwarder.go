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

	var eventID int64
	err = a.db.QueryRow(`
		INSERT INTO reply_forward_messages (rule_id, message_key, from_email, subject, status, received_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW())
		ON CONFLICT (rule_id, message_key) DO NOTHING
		RETURNING id`, source.RuleID, key, from, subject).Scan(&eventID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
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
		_, _ = a.db.Exec(`UPDATE reply_forward_messages SET status = 'failed', attempts = attempts + 1, last_error = $2, updated_at = NOW() WHERE id = $1`, eventID, err.Error())
		return err
	}
	_, _ = a.db.Exec(`UPDATE reply_forward_messages SET status = 'forwarded', attempts = attempts + 1, forwarded_at = NOW(), updated_at = NOW(), last_error = '' WHERE id = $1`, eventID)
	_, _ = a.db.Exec(`UPDATE reply_mailboxes SET forward_count = forward_count + 1, updated_at = NOW() WHERE id = $1`, source.MailboxID)
	_, _ = a.db.Exec(`UPDATE reply_forward_rules SET last_forward_at = NOW(), last_error = '', updated_at = NOW() WHERE id = $1`, source.RuleID)
	return nil
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
