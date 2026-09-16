package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net"
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

	// replyAIMailboxTimeout is the hard cap for one entire mailbox scan,
	// including every message download.
	replyAIMailboxTimeout = 2 * time.Minute

	// replyAIMaxConcurrent is the number of mailboxes scanned at a time. One
	// slow server can therefore only ever occupy a single worker.
	replyAIMaxConcurrent = 4

	// replyAIScanCloseGrace bounds how long a scan that exhausted its budget
	// waits for the forced connection close to unwind the reader.
	replyAIScanCloseGrace = 5 * time.Second
)

// The dial and per-operation read deadlines are variables so tests can shrink
// them; production runs with the values below. The go-pop3 client exposes no
// read deadline and no context (NewConn, Auth and RetrRaw block inside
// bufio.Reader.ReadLine), so both bounds are installed through its Opt.Dialer
// hook and a watchdog that force-closes the connection.
var (
	replyAIMailboxDialTimeout = 10 * time.Second
	replyAIMailboxReadTimeout = 30 * time.Second
)

// errReplyAIScanTimeout marks a mailbox scan that exceeded its budget and had
// its connection torn down: the server is hung rather than failing.
var errReplyAIScanTimeout = errors.New("mailbox scan timed out")

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
	runReplyAIMailboxPool(sources, replyAIMaxConcurrent, a.scanReplyAIMailbox)
}

// runReplyAIMailboxPool scans the mailboxes with a fixed number of workers, so
// an unresponsive server occupies at most one worker and the remaining
// mailboxes keep being scanned in the same round. No goroutine is created per
// mailbox.
func runReplyAIMailboxPool(sources []replyAIMailboxSource, workers int, scan func(replyAIMailboxSource)) {
	if workers > len(sources) {
		workers = len(sources)
	}
	if workers < 1 {
		return
	}
	jobs := make(chan replyAIMailboxSource)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for source := range jobs {
				scan(source)
			}
		}()
	}
	for _, source := range sources {
		jobs <- source
	}
	close(jobs)
	wg.Wait()
}

// scanReplyAIMailbox scans one mailbox and records the outcome for operators.
func (a *App) scanReplyAIMailbox(source replyAIMailboxSource) {
	budget := replyAIDefaultScanBudget()
	err := a.scanOneReplyAIMailbox(source, budget)
	if err == nil {
		_, _ = a.queries.UpdateReplyAIMailboxSync.Exec(source.ID, "")
		return
	}
	var orgID *int64
	if source.OrganizationID.Valid && source.OrganizationID.Int > 0 {
		id := int64(source.OrganizationID.Int)
		orgID = &id
	}
	a.recordBackgroundAuditResult("system", "reply_ai.mailbox_scan_failed", "reply_mailbox", fmt.Sprintf("%d", source.ID), orgID, "failed", "mailbox_scan_failed", map[string]any{
		"host": source.Host,
		"port": source.Port,
	})
	a.log.Printf("%s", replyAIMailboxFailureMessage(source, err, budget.scan))
	_, _ = a.queries.UpdateReplyAIMailboxSync.Exec(source.ID, replyAIErrorText(err))
}

// replyAIMailboxFailureMessage names the mailbox and the server it points at,
// so a hung server is distinguishable from a failing one in the logs.
func replyAIMailboxFailureMessage(source replyAIMailboxSource, err error, budget time.Duration) string {
	if errors.Is(err, errReplyAIScanTimeout) {
		return fmt.Sprintf("reply AI mailbox %d (%s:%d) scan timed out after %s and its connection was closed; the server is unresponsive",
			source.ID, source.Host, source.Port, budget)
	}
	return fmt.Sprintf("reply AI mailbox %d (%s:%d) scan failed: %v", source.ID, source.Host, source.Port, err)
}

// scanOneReplyAIMailbox keeps the mailbox semantics unchanged: it never deletes
// source messages, and every message goes through the durable event queue so
// deduplication and leasing stay in the database.
func (a *App) scanOneReplyAIMailbox(source replyAIMailboxSource, budget replyAIScanBudget) error {
	return fetchReplyAIMailbox(source, budget, func(id int, raw []byte) {
		if err := a.ingestReplyAIMessage(source, raw); err != nil {
			a.log.Printf("reply AI mailbox %d message %d ingest failed: %v", source.ID, id, err)
		}
	})
}

// replyAIScanBudget bounds a single mailbox scan.
type replyAIScanBudget struct {
	dial time.Duration // TCP connect budget
	read time.Duration // idle deadline re-armed before every connection read and write
	scan time.Duration // hard cap for the whole scan
}

func replyAIDefaultScanBudget() replyAIScanBudget {
	return replyAIScanBudget{dial: replyAIMailboxDialTimeout, read: replyAIMailboxReadTimeout, scan: replyAIMailboxTimeout}
}

// fetchReplyAIMailbox scans one mailbox within the given budget. The client
// calls run in their own goroutine so the caller can abandon a hung exchange:
// once the budget expires the connection is force-closed, which unblocks the
// pending read and releases the worker instead of leaking it.
func fetchReplyAIMailbox(source replyAIMailboxSource, budget replyAIScanBudget, handle func(id int, raw []byte)) error {
	dialer := &replyAIDialer{dialTimeout: budget.dial, readTimeout: budget.read}
	done := make(chan error, 1)
	go func() { done <- fetchReplyAIMessages(source, dialer, handle) }()

	if budget.scan <= 0 {
		return <-done
	}
	timer := time.NewTimer(budget.scan)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
	}

	_ = dialer.Close()
	grace := time.NewTimer(replyAIScanCloseGrace)
	defer grace.Stop()
	select {
	case <-done:
	case <-grace.C:
	}
	return fmt.Errorf("%w after %s", errReplyAIScanTimeout, budget.scan)
}

// fetchReplyAIMessages dials the mailbox and hands every message that may be
// processed to handle, preserving the previous per-mailbox behaviour (no
// deletion, newest replyAIMaxMessages messages, size cap, individual read
// failures skipped).
func fetchReplyAIMessages(source replyAIMailboxSource, dialer *replyAIDialer, handle func(id int, raw []byte)) error {
	host, port := source.Host, source.Port
	if strings.HasPrefix(strings.ToLower(host), "imap.") {
		host = "pop." + strings.TrimPrefix(host, "imap.")
		port = 995
	}
	if port == 0 {
		port = 995
	}
	client := pop3.New(pop3.Opt{Host: host, Port: port, TLSEnabled: source.TLSEnabled, Dialer: dialer})
	conn, err := client.NewConn()
	if err != nil {
		return err
	}
	defer conn.Quit()
	if err := conn.Auth(source.Username, source.Password); err != nil {
		return err
	}
	if _, _, err := conn.Stat(); err != nil {
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
		if msg.Size > replyAIMaxMessageBytes {
			continue
		}
		raw, err := conn.RetrRaw(msg.ID)
		if err != nil {
			continue
		}
		handle(msg.ID, raw.Bytes())
	}
	return nil
}

// replyAIDeadlineConn re-arms an idle deadline before every read and write, so
// a server that stops talking mid-exchange fails with a timeout instead of
// blocking the scan forever. The library keeps the net.Conn returned by the
// dialer (wrapping it for TLS, which forwards deadlines to the same conn),
// which makes this wrapper the only place a deadline can be installed.
type replyAIDeadlineConn struct {
	net.Conn
	idle time.Duration
}

func (c *replyAIDeadlineConn) Read(b []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}

func (c *replyAIDeadlineConn) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

// replyAIDialer dials a connection with bounded deadlines and remembers it, so
// a scan that overruns its budget can tear the connection down while the client
// is parked in a read.
type replyAIDialer struct {
	dialTimeout time.Duration
	readTimeout time.Duration

	mu   sync.Mutex
	conn net.Conn
}

func (d *replyAIDialer) Dial(network, address string) (net.Conn, error) {
	// dialMailbox resolves the target, refuses blocked addresses (loopback,
	// link-local, cloud metadata) and dials the address it validated. Enforcing
	// the policy at the socket means the background scanner obeys it even for a
	// mailbox row that predates the check or was written straight to the
	// database.
	conn, err := dialMailbox(context.Background(), network, address, d.dialTimeout)
	if err != nil {
		return nil, err
	}
	if d.readTimeout > 0 {
		conn = &replyAIDeadlineConn{Conn: conn, idle: d.readTimeout}
	}
	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()
	return conn, nil
}

// Close force-closes the connection established by this dialer.
func (d *replyAIDialer) Close() error {
	d.mu.Lock()
	conn := d.conn
	d.conn = nil
	d.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
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
	if err == nil {
		var orgID *int64
		if source.OrganizationID.Valid && source.OrganizationID.Int > 0 {
			value := int64(source.OrganizationID.Int)
			orgID = &value
		}
		a.recordBackgroundAudit("system", "reply_ai.received", "reply_ai_event", fmt.Sprintf("%d", id), orgID, map[string]any{
			"mailbox_id": source.ID,
		})
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
			a.recordBackgroundAuditResult("system", "reply_ai.processing_failed", "reply_ai_event", fmt.Sprintf("%d", event.ID), replyAIEventOrganizationID(event), "failed", "processing_failed", map[string]any{
				"mailbox_id": event.ReplyMailboxID,
			})
			_, _ = a.queries.FailReplyAIEvent.Exec(event.ID, replyAIErrorText(err), event.LeaseToken)
		} else {
			a.recordBackgroundAudit("system", "reply_ai.processed", "reply_ai_event", fmt.Sprintf("%d", event.ID), replyAIEventOrganizationID(event), map[string]any{
				"mailbox_id": event.ReplyMailboxID,
			})
		}
	}
}

func replyAIEventOrganizationID(event models.ReplyAIEvent) *int64 {
	if event.SourceOrganizationID.Valid && event.SourceOrganizationID.Int > 0 {
		id := int64(event.SourceOrganizationID.Int)
		return &id
	}
	return nil
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
	if decision.Intent == models.ReplyAIIntentOther || decision.Intent == models.ReplyAIIntentProductComplaint {
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
