package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"html"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-message"
	"github.com/knadh/go-pop3"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	replyAIPollInterval    = 60 * time.Second
	replyAIMaxRunes        = 6000
	replyAIMaxMessages     = 200
	replyAIMaxMessageBytes = 5 << 20
	replyAIMailboxTimeout  = 2 * time.Minute
	replyAIMaxConcurrent   = 4
)

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

type replyAIMailboxSource struct {
	ID             int      `db:"id"`
	UserID         int      `db:"user_id"`
	OrganizationID null.Int `db:"organization_id"`
	Email          string   `db:"email"`
	Username       string   `db:"username"`
	Password       string   `db:"password"`
	Host           string   `db:"imap_host"`
	Port           int      `db:"imap_port"`
	TLSEnabled     bool     `db:"imap_tls"`
	Folder         string   `db:"folder"`
}

// runReplyAIProcessor scans only mailboxes which both have active global
// configuration and an explicit per-mailbox opt-in. It never deletes source
// messages; durable queue rows make every source message idempotent.
func runReplyAIProcessor(a *App) {
	if a == nil || a.db == nil || a.replyAI == nil || !a.replyAI.Enabled() {
		return
	}
	ticker := time.NewTicker(replyAIPollInterval)
	defer ticker.Stop()
	for {
		a.scanReplyAIMailboxes()
		a.processReplyAIEvents()
		<-ticker.C
	}
}

func (a *App) scanReplyAIMailboxes() {
	var sources []replyAIMailboxSource
	if err := a.queries.GetReplyAIMailboxes.Select(&sources); err != nil {
		a.log.Printf("error loading reply AI mailboxes: %v", err)
		return
	}
	sem := make(chan struct{}, replyAIMaxConcurrent)
	var wg sync.WaitGroup
	for _, source := range sources {
		source := source
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			done := make(chan error, 1)
			go func() { done <- a.scanOneReplyAIMailbox(source) }()
			select {
			case err := <-done:
				if err != nil {
					a.log.Printf("reply AI mailbox %d scan failed: %v", source.ID, err)
					_, _ = a.queries.UpdateReplyAIMailboxSync.Exec(source.ID, replyAIErrorText(err))
				}
			case <-time.After(replyAIMailboxTimeout):
				a.log.Printf("reply AI mailbox %d scan timed out", source.ID)
			}
		}()
	}
	wg.Wait()
}

func (a *App) scanOneReplyAIMailbox(source replyAIMailboxSource) error {
	host, port := source.Host, source.Port
	if strings.HasPrefix(strings.ToLower(host), "imap.") {
		host = "pop." + strings.TrimPrefix(host, "imap.")
		port = 995
	}
	if port == 0 {
		port = 995
	}
	client := pop3.New(pop3.Opt{Host: host, Port: port, TLSEnabled: source.TLSEnabled})
	conn, err := client.NewConn()
	if err != nil {
		return err
	}
	defer conn.Quit()
	if err := conn.Auth(source.Username, source.Password); err != nil {
		return err
	}
	_, _, err = conn.Stat()
	if err != nil {
		return err
	}
	ids, err := conn.List(0)
	if err != nil {
		return err
	}
	start := 0
	if len(ids) > replyAIMaxMessages {
		start = len(ids) - replyAIMaxMessages
	}
	for _, msg := range ids[start:] {
		id := msg.ID
		if msg.Size > replyAIMaxMessageBytes {
			continue
		}
		raw, err := conn.RetrRaw(id)
		if err != nil {
			continue
		}
		if err := a.ingestReplyAIMessage(source, raw.Bytes()); err != nil {
			a.log.Printf("reply AI mailbox %d message %d ingest failed: %v", source.ID, id, err)
		}
	}
	_, _ = a.queries.UpdateReplyAIMailboxSync.Exec(source.ID, "")
	return nil
}

func (a *App) ingestReplyAIMessage(source replyAIMailboxSource, raw []byte) error {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if isAutomatedInboundReply(entity) {
		return nil
	}

	from := normalizeReplyAddress(entity.Header.Get("From"))
	if from == "" {
		return nil
	}
	body := normalizedReplyText(entity)
	if body == "" {
		return nil
	}

	rawHash := sha256.Sum256(raw)
	bodyHash := sha256.Sum256([]byte(body))
	messageKey := entity.Header.Get("Message-ID") + ":" + hex.EncodeToString(rawHash[:])
	var id int
	err = a.queries.InsertReplyAIEvent.Get(&id, source.ID, messageKey, from,
		strings.TrimSpace(entity.Header.Get("Subject")), body, hex.EncodeToString(bodyHash[:]), time.Now())
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}

func (a *App) processReplyAIEvents() {
	for {
		var event models.ReplyAIEvent
		err := a.queries.ClaimReplyAIEvent.Get(&event)
		if err == sql.ErrNoRows {
			return
		}
		if err != nil {
			a.log.Printf("error claiming reply AI event: %v", err)
			return
		}
		if err := a.processReplyAIEvent(event); err != nil {
			if errors.Is(err, core.ErrReplyAILeaseLost) {
				continue
			}
			a.log.Printf("reply AI event %d failed: %v", event.ID, err)
			_, _ = a.queries.FailReplyAIEvent.Exec(event.ID, replyAIErrorText(err), event.LeaseToken)
		}
	}
}

func (a *App) processReplyAIEvent(event models.ReplyAIEvent) error {
	var source replyAIMailboxSource
	if err := a.queries.GetReplyAIMailbox.Get(&source, event.ReplyMailboxID); err != nil {
		if err == sql.ErrNoRows {
			return a.finishReplyAIEvent(event, nil, models.ReplyAIIntentOther, 0,
				"mailbox_not_active", "", models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
		}
		return err
	}

	access := models.WorkspaceAccess{
		Workspace: models.Workspace{
			OrganizationID: int(source.OrganizationID.Int),
			Personal:       !source.OrganizationID.Valid,
		},
		UserID: source.UserID,
	}
	customer, ok, err := a.core.FindReplyAIWorkspaceCustomer(access, event.FromEmail)
	if err != nil {
		return err
	}
	var poolContact models.PoolContact
	var poolRecipient core.PublicPoolRecipient
	if !ok {
		poolContact, poolRecipient, ok, err = a.core.FindReplyAIPoolContact(access, event.FromEmail)
		if err != nil {
			return err
		}
	}
	if !ok {
		return a.finishReplyAIEvent(event, nil, models.ReplyAIIntentOther, 0,
			"unmatched_sender", "", models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
	}
	if strings.TrimSpace(event.Body) == "" {
		if poolContact.ID > 0 {
			return a.finishReplyAIEvent(event, nil, models.ReplyAIIntentOther, 0,
				"empty_reply", "", models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
		}
		return a.finishReplyAIEvent(event, &customer.ID, models.ReplyAIIntentOther, 0,
			"empty_reply", "", models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
	}

	decision, err := a.replyAI.Classify(context.Background(), event.Body)
	if err != nil {
		return err
	}
	if decision.Intent == models.ReplyAIIntentOther {
		var customerID *int
		if customer.ID > 0 {
			customerID = &customer.ID
		}
		return a.finishReplyAIEvent(event, customerID, decision.Intent, decision.Confidence,
			decision.ReasonCode, a.replyAI.Model(), models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
	}
	if decision.Confidence < a.replyAI.MinConfidence() {
		var customerID *int
		if customer.ID > 0 {
			customerID = &customer.ID
		}
		return a.finishReplyAIEvent(event, customerID, models.ReplyAIIntentOther, decision.Confidence,
			"low_confidence", a.replyAI.Model(), models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
	}

	occurredAt := time.Now()
	if event.ReceivedAt.Valid {
		occurredAt = event.ReceivedAt.Time
	}
	action := core.ReplyAIAction{
		EventID:    event.ID,
		LeaseToken: event.LeaseToken,
		CustomerID: customer.ID,
		Intent:     decision.Intent,
		Confidence: decision.Confidence,
		ReasonCode: decision.ReasonCode,
		Model:      a.replyAI.Model(),
		OccurredAt: occurredAt,
	}
	if poolContact.ID > 0 {
		action.PoolContactID = poolContact.ID
		action.SourceSegmentID = int64(poolRecipient.SegmentID.Int)
		action.SourceOrganizationID = int64(poolRecipient.OrganizationID.Int)
		// Recover the parent pool ID from the immutable delivery snapshot.
		_ = a.db.Get(&action.PoolID, `SELECT pool_id FROM campaign_pool_recipients WHERE campaign_id=$1 AND pool_contact_id=$2`, poolRecipient.CampaignID, poolRecipient.PoolContactID)
	}
	if err := a.core.ApplyReplyAIAction(access, action); err != nil {
		if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == 409 {
			return a.finishReplyAIEvent(event, &customer.ID, models.ReplyAIIntentOther, decision.Confidence,
				"workspace_not_writable", a.replyAI.Model(), models.ReplyAIActionIgnored, models.ReplyAIEventStatusIgnored)
		}
		return err
	}
	// ApplyReplyAIAction transitions the queue row to its processed terminal
	// state inside the same transaction as the customer mutation.
	return nil
}

func (a *App) finishReplyAIEvent(event models.ReplyAIEvent, customerID *int, intent string, confidence float64,
	reasonCode, model, action, status string) error {
	_, err := a.queries.FinishReplyAIEvent.Exec(event.ID, customerID, intent, confidence, reasonCode, model, action, status, event.LeaseToken)
	return err
}

func isAutomatedInboundReply(entity *message.Entity) bool {
	if strings.EqualFold(entity.Header.Get("X-Listmonk-Forwarded-Reply"), "true") {
		return true
	}
	auto := strings.ToLower(strings.TrimSpace(entity.Header.Get("Auto-Submitted")))
	if auto != "" && auto != "no" {
		return true
	}
	precedence := strings.ToLower(strings.TrimSpace(entity.Header.Get("Precedence")))
	if precedence == "bulk" || precedence == "junk" || precedence == "list" {
		return true
	}
	from := strings.ToLower(entity.Header.Get("From"))
	subject := strings.ToLower(entity.Header.Get("Subject"))
	return strings.Contains(from, "mailer-daemon") || strings.Contains(subject, "delivery status notification")
}

func normalizeReplyAddress(raw string) string {
	addr, err := mail.ParseAddress(raw)
	if err != nil || !strings.Contains(addr.Address, "@") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr.Address))
}

func normalizedReplyText(entity *message.Entity) string {
	body, contentType := inboundBody(entity)
	text := string(body)
	if strings.EqualFold(contentType, models.CampaignContentTypeHTML) {
		text = html.UnescapeString(htmlTagPattern.ReplaceAllString(text, " "))
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(trimmed, ">") || trimmed == "-----Original Message-----" ||
			(strings.HasPrefix(lower, "on ") && strings.HasSuffix(lower, " wrote:")) {
			break
		}
		kept = append(kept, trimmed)
	}
	text = strings.TrimSpace(strings.Join(kept, "\n"))
	runes := []rune(text)
	if len(runes) > replyAIMaxRunes {
		text = string(runes[:replyAIMaxRunes])
	}
	return text
}

func replyAIErrorText(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	runes := []rune(text)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return text
}
