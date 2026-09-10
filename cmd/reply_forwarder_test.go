package main

import (
	"database/sql"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
)

// The forwarding tests drive whole scan rounds directly. One round is what
// scanOneReplyForwardSource does for every message in a source mailbox: read the
// message again and call forwardOneReply with its raw bytes. The POP3 transport
// in between is not exercised: the forwarder only ever reaches it over TLS,
// which a local test server cannot satisfy, and the retry policy lives entirely
// in forwardOneReply. The database-backed cases run against a throwaway database
// installed from schema.sql (see newAdminTestDB) and are gated on the same DSN
// environment variables as the other database tests.

// replyForwardTestApp is an App wired for forwarding: a throwaway database and a
// real campaign manager whose "email" messenger is a recording stub, so a
// forward can be counted at the boundary where it becomes a delivery.
type replyForwardTestApp struct {
	app     *App
	db      *sqlx.DB
	msgr    *recordingMessenger
	manager *manager.Manager
	source  replyForwardSource
}

// recordingMessenger stands in for the platform SMTP messenger. Every message
// the manager's worker hands to it is recorded, which is the delivery evidence
// the retry tests assert on.
type recordingMessenger struct {
	mu   sync.Mutex
	sent []models.Message
}

func (m *recordingMessenger) Name() string { return "email" }

func (m *recordingMessenger) Push(msg models.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *recordingMessenger) Flush() error { return nil }
func (m *recordingMessenger) Close() error { return nil }

func (m *recordingMessenger) messages() []models.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]models.Message(nil), m.sent...)
}

func (m *recordingMessenger) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// testLogger returns a logger that keeps the test output clean.
func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// newForwardingManager starts a real manager with msgr registered as the
// platform "email" messenger. The manager is closed when the test ends.
func newForwardingManager(t *testing.T, msgr manager.Messenger) *manager.Manager {
	t.Helper()

	// A nil store and i18n are never touched on the arbitrary-message path.
	mgr := manager.New(manager.Config{Concurrency: 1, MessageRate: 1}, nil, nil, testLogger())
	if err := mgr.AddMessenger(msgr); err != nil {
		t.Fatalf("registering the test messenger: %v", err)
	}
	go mgr.Run()
	t.Cleanup(mgr.Close)

	return mgr
}

// closedForwardingManager returns a manager that has already shut down, so
// PushMessage fails the way it does during a restart or while the queue is
// unavailable. Nothing it is handed is ever delivered.
func closedForwardingManager(t *testing.T) *manager.Manager {
	t.Helper()

	mgr := manager.New(manager.Config{Concurrency: 1, MessageRate: 1}, nil, nil, testLogger())
	mgr.Close()

	return mgr
}

// newReplyForwardTestApp seeds a retained mailbox with an active forwarding rule
// and returns the app plus the source to forward from.
func newReplyForwardTestApp(t *testing.T) *replyForwardTestApp {
	t.Helper()

	db := newAdminTestDB(t)

	// No SMTP servers: the app only needs the system sender address here, and
	// deliveries are counted at the messenger.
	msgr, err := email.New("email")
	if err != nil {
		t.Fatalf("creating the system email messenger: %v", err)
	}

	recorder := &recordingMessenger{}
	mgr := newForwardingManager(t, recorder)

	return &replyForwardTestApp{
		app:     &App{db: db, manager: mgr, emailMsgr: msgr, log: testLogger()},
		db:      db,
		msgr:    recorder,
		manager: mgr,
		source:  seedReplyForwardRule(t, db),
	}
}

// seedReplyForwardRule creates the user/organization/mailbox/rule graph a
// forwarding source needs and returns it as the forwarder would load it.
func seedReplyForwardRule(t *testing.T, db *sqlx.DB) replyForwardSource {
	t.Helper()

	var roleID int
	if err := db.Get(&roleID, `INSERT INTO roles (type, name, permissions) VALUES ('user', 'Super Admin', '{}') RETURNING id`); err != nil {
		t.Fatalf("seeding a role: %v", err)
	}

	var userID int
	if err := db.Get(&userID, `INSERT INTO users (username, email, name, user_role_id, status)
		VALUES ('departed', 'departed@example.com', 'Departed Member', $1, 'enabled') RETURNING id`, roleID); err != nil {
		t.Fatalf("seeding the departed member: %v", err)
	}

	var orgID int
	if err := db.Get(&orgID, `INSERT INTO organizations (name, created_by_user_id) VALUES ('Acme', $1) RETURNING id`, userID); err != nil {
		t.Fatalf("seeding the organization: %v", err)
	}

	var mailboxID int
	if err := db.Get(&mailboxID, `INSERT INTO reply_mailboxes (user_id, organization_id, email, status)
		VALUES ($1, $2, 'replies@company.example', 'retained') RETURNING id`, userID, orgID); err != nil {
		t.Fatalf("seeding the retained reply mailbox: %v", err)
	}

	var ruleID int
	if err := db.Get(&ruleID, `INSERT INTO reply_forward_rules (reply_mailbox_id, organization_id, target_user_id, target_email)
		VALUES ($1, $2, $3, 'manager@company.example') RETURNING id`, mailboxID, orgID, userID); err != nil {
		t.Fatalf("seeding the forwarding rule: %v", err)
	}

	return replyForwardSource{
		RuleID:       ruleID,
		MailboxID:    mailboxID,
		Organization: orgID,
		Email:        "replies@company.example",
		TargetEmail:  "manager@company.example",
	}
}

// replyForwardTestMessage returns one raw customer reply, the way POP3 RETR
// hands it to the forwarder.
func replyForwardTestMessage(messageID, subject string) []byte {
	return []byte("From: Customer <customer@example.net>\r\n" +
		"To: replies@company.example\r\n" +
		"Subject: " + subject + "\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"请问这批货什么时候到？\r\n")
}

// replyForwardRow is the forwarding queue row the tests inspect. It is the only
// persisted trace of a forward, so every assertion is made against it.
type replyForwardRow struct {
	Status      string       `db:"status"`
	Attempts    int          `db:"attempts"`
	LastError   string       `db:"last_error"`
	ForwardedAt sql.NullTime `db:"forwarded_at"`
	UpdatedAt   time.Time    `db:"updated_at"`
}

// replyForwardRows loads every queue row of a rule. The dedupe key guarantees
// there is at most one row per source message, so most tests require exactly one.
func replyForwardRows(t *testing.T, db *sqlx.DB, ruleID int) []replyForwardRow {
	t.Helper()

	rows := []replyForwardRow{}
	if err := db.Select(&rows, `SELECT status, attempts, last_error, forwarded_at, updated_at
		FROM reply_forward_messages WHERE rule_id = $1 ORDER BY id`, ruleID); err != nil {
		t.Fatalf("loading the forwarding rows of rule %d: %v", ruleID, err)
	}

	return rows
}

// singleReplyForwardRow requires exactly one dedupe row for the rule, which is
// the property that keeps one source message from being forwarded twice.
func singleReplyForwardRow(t *testing.T, db *sqlx.DB, ruleID int) replyForwardRow {
	t.Helper()

	rows := replyForwardRows(t, db, ruleID)
	if len(rows) != 1 {
		t.Fatalf("rule %d has %d forwarding rows, want exactly one dedupe row", ruleID, len(rows))
	}

	return rows[0]
}

// forwardCount returns the mailbox counter that must move exactly once per
// successfully forwarded reply.
func forwardCount(t *testing.T, db *sqlx.DB, mailboxID int) int {
	t.Helper()

	var n int
	if err := db.Get(&n, `SELECT forward_count FROM reply_mailboxes WHERE id = $1`, mailboxID); err != nil {
		t.Fatalf("loading forward_count of mailbox %d: %v", mailboxID, err)
	}

	return n
}

// ageReplyForwardRow moves a row's lease and backoff anchor into the past,
// standing in for the scan rounds that happen later in wall-clock time.
func ageReplyForwardRow(t *testing.T, db *sqlx.DB, ruleID int, age time.Duration) {
	t.Helper()

	if _, err := db.Exec(`UPDATE reply_forward_messages
		SET updated_at = NOW() - INTERVAL '1 second' * $2::float8 WHERE rule_id = $1`, ruleID, age.Seconds()); err != nil {
		t.Fatalf("ageing the forwarding row of rule %d: %v", ruleID, err)
	}
}

// waitForDeliveries waits until the recording messenger has seen at least n
// forwards, so the test never races the manager's worker.
func waitForDeliveries(t *testing.T, msgr *recordingMessenger, n int) []models.Message {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		got := msgr.messages()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d forwarded messages, got %d", n, len(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settleForwarding waits for the manager's worker to go idle. It is only used to
// prove the negative: that no further forward arrives.
func settleForwarding() {
	time.Sleep(250 * time.Millisecond)
}

// shrinkReplyForwardRetry shortens the retry policy so a test can drive several
// rounds without waiting for the production backoff, and keeps the claim lease
// long enough that a live claim is never stolen by mistake. The remaining
// backoff is still wide enough that a round run immediately after a failure is
// reliably inside it.
func shrinkReplyForwardRetry(t *testing.T) {
	t.Helper()

	prevMax, prevBase, prevCap, prevLease := replyForwardMaxAttempts, replyForwardRetryBackoff, replyForwardRetryMaxBackoff, replyForwardClaimLease
	replyForwardMaxAttempts = 3
	replyForwardRetryBackoff = 500 * time.Millisecond
	replyForwardRetryMaxBackoff = time.Second
	replyForwardClaimLease = time.Minute

	t.Cleanup(func() {
		replyForwardMaxAttempts, replyForwardRetryBackoff, replyForwardRetryMaxBackoff, replyForwardClaimLease = prevMax, prevBase, prevCap, prevLease
	})
}

// TestReplyForwardRetryPolicyIsBounded guards the policy itself: a reply may be
// retried, but the number of attempts is finite, the backoff grows, and the claim
// lease stays far longer than a single attempt can take (the manager gives up on
// a push after a few seconds), so an expired lease can only mean a dead owner.
func TestReplyForwardRetryPolicyIsBounded(t *testing.T) {
	if replyForwardMaxAttempts < 2 {
		t.Fatalf("replyForwardMaxAttempts = %d, want at least one retry", replyForwardMaxAttempts)
	}
	if replyForwardRetryBackoff <= 0 {
		t.Fatalf("replyForwardRetryBackoff = %s, want a positive delay before a retry", replyForwardRetryBackoff)
	}
	if replyForwardRetryMaxBackoff < replyForwardRetryBackoff {
		t.Fatalf("replyForwardRetryMaxBackoff = %s, want it to cap a backoff of %s",
			replyForwardRetryMaxBackoff, replyForwardRetryBackoff)
	}
	if replyForwardClaimLease <= 30*time.Second {
		t.Fatalf("replyForwardClaimLease = %s, want it to exceed the 3s push timeout by a wide margin",
			replyForwardClaimLease)
	}
	if cap := replyForwardBackoffCap(); cap < 1 {
		t.Fatalf("replyForwardBackoffCap() = %f, want at least 1", cap)
	}
	// The cap is expressed as a factor of the base delay, so a zero base delay
	// must not produce a division result that breaks the claim query.
	prev := replyForwardRetryBackoff
	replyForwardRetryBackoff = 0
	t.Cleanup(func() { replyForwardRetryBackoff = prev })
	if cap := replyForwardBackoffCap(); cap != 1 {
		t.Fatalf("replyForwardBackoffCap() with no base delay = %f, want 1", cap)
	}
}

// TestReplyForwardRetriesAFailedHandOff is the regression for the defect: a
// reply whose first attempt failed used to be swallowed by its own dedupe row, so
// a queue backlog or a restart lost the customer's reply for good. The next round
// must retry it and forward it exactly once.
func TestReplyForwardRetriesAFailedHandOff(t *testing.T) {
	shrinkReplyForwardRetry(t)
	h := newReplyForwardTestApp(t)
	raw := replyForwardTestMessage("retry@example.net", "Re: order 42")

	// Round 1: the platform queue is unavailable (restart, backlog, shutdown).
	h.app.manager = closedForwardingManager(t)
	if err := h.app.forwardOneReply(h.source, raw); err == nil {
		t.Fatal("a forward whose hand-off failed reported success")
	}

	failed := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if failed.Status != "failed" || failed.Attempts != 1 {
		t.Fatalf("after the failed attempt the row is %s with %d attempts, want failed with 1", failed.Status, failed.Attempts)
	}
	if failed.LastError == "" {
		t.Fatal("the failed attempt recorded no error")
	}
	if failed.ForwardedAt.Valid {
		t.Fatal("a failed attempt marked the reply forwarded")
	}
	if n := forwardCount(t, h.db, h.source.MailboxID); n != 0 {
		t.Fatalf("forward_count = %d after a failed attempt, want 0", n)
	}
	if n := h.msgr.count(); n != 0 {
		t.Fatalf("a failed hand-off still delivered %d messages", n)
	}

	// The very next round must not hot-retry the reply: a failed row is only
	// claimable once its backoff has elapsed.
	h.app.manager = h.manager
	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("a round inside the retry backoff returned an error: %v", err)
	}
	settleForwarding()

	if cooling := singleReplyForwardRow(t, h.db, h.source.RuleID); cooling.Attempts != 1 || cooling.Status != "failed" {
		t.Fatalf("a round inside the retry backoff re-attempted the reply (%s with %d attempts)", cooling.Status, cooling.Attempts)
	}
	if n := h.msgr.count(); n != 0 {
		t.Fatalf("a round inside the retry backoff delivered %d messages", n)
	}

	// Round 3: the backoff has elapsed and the queue is available again, so the
	// same reply scanned again is forwarded.
	ageReplyForwardRow(t, h.db, h.source.RuleID, time.Hour)
	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("the retry of a failed forward failed: %v", err)
	}

	forwarded := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if forwarded.Status != "forwarded" || forwarded.Attempts != 2 {
		t.Fatalf("after the retry the row is %s with %d attempts, want forwarded with 2", forwarded.Status, forwarded.Attempts)
	}
	if !forwarded.ForwardedAt.Valid || forwarded.LastError != "" {
		t.Fatalf("the retried forward left forwarded_at=%v last_error=%q", forwarded.ForwardedAt, forwarded.LastError)
	}
	if n := forwardCount(t, h.db, h.source.MailboxID); n != 1 {
		t.Fatalf("forward_count = %d after one successful retry, want 1", n)
	}

	sent := waitForDeliveries(t, h.msgr, 1)
	if len(sent) != 1 {
		t.Fatalf("the retry delivered %d messages, want exactly 1", len(sent))
	}
	if got := sent[0].Subject; got != "[客户回信] Re: order 42" {
		t.Fatalf("forwarded subject = %q", got)
	}
	if got := sent[0].To; len(got) != 1 || got[0] != h.source.TargetEmail {
		t.Fatalf("forwarded to %v, want %s", got, h.source.TargetEmail)
	}
	if got := sent[0].Headers.Get("X-Listmonk-Forwarded-Reply"); got != "true" {
		t.Fatalf("forwarded message carries X-Listmonk-Forwarded-Reply=%q, want true", got)
	}
	if got := sent[0].Headers.Get("Reply-To"); got != "customer@example.net" {
		t.Fatalf("forwarded message carries Reply-To=%q", got)
	}
	if len(sent[0].Attachments) != 1 || sent[0].Attachments[0].Name != "original-reply.eml" {
		t.Fatalf("forwarded message lost the untouched source message: %+v", sent[0].Attachments)
	}
}

// TestReplyForwardNeverForwardsTheSameReplyTwice keeps the dedupe guarantee: once
// a reply is forwarded it is skipped by every later round, no matter how old the
// row is, while a different reply from the same mailbox is still forwarded.
func TestReplyForwardNeverForwardsTheSameReplyTwice(t *testing.T) {
	h := newReplyForwardTestApp(t)
	raw := replyForwardTestMessage("twice@example.net", "Re: invoice")
	other := replyForwardTestMessage("other@example.net", "Re: delivery")

	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("forwarding the reply: %v", err)
	}
	waitForDeliveries(t, h.msgr, 1)

	first := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if first.Status != "forwarded" || first.Attempts != 1 {
		t.Fatalf("the first forward left the row %s with %d attempts, want forwarded with 1", first.Status, first.Attempts)
	}

	// Every later round re-reads the same message from the mailbox. The row is
	// aged past both the retry backoff and the claim lease, so nothing but the
	// dedupe key can be stopping a second send.
	for i := 0; i < 5; i++ {
		ageReplyForwardRow(t, h.db, h.source.RuleID, 24*time.Hour)
		if err := h.app.forwardOneReply(h.source, raw); err != nil {
			t.Fatalf("round %d of an already forwarded reply returned an error: %v", i+2, err)
		}
	}
	settleForwarding()

	if n := h.msgr.count(); n != 1 {
		t.Fatalf("the same reply was forwarded %d times, want exactly 1", n)
	}
	again := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if again.Status != "forwarded" || again.Attempts != first.Attempts {
		t.Fatalf("a repeated round changed the row to %s with %d attempts", again.Status, again.Attempts)
	}
	if !again.ForwardedAt.Time.Equal(first.ForwardedAt.Time) {
		t.Fatalf("a repeated round moved forwarded_at from %s to %s", first.ForwardedAt.Time, again.ForwardedAt.Time)
	}
	if n := forwardCount(t, h.db, h.source.MailboxID); n != 1 {
		t.Fatalf("forward_count = %d after repeated rounds, want 1", n)
	}

	// A different reply from the same mailbox must still be forwarded: the guard
	// is the per-message dedupe key, not the mailbox.
	if err := h.app.forwardOneReply(h.source, other); err != nil {
		t.Fatalf("forwarding a second, different reply: %v", err)
	}
	if got := waitForDeliveries(t, h.msgr, 2); len(got) != 2 {
		t.Fatalf("the mailbox delivered %d messages, want 2", len(got))
	}
	if rows := replyForwardRows(t, h.db, h.source.RuleID); len(rows) != 2 {
		t.Fatalf("the mailbox has %d dedupe rows for 2 replies", len(rows))
	}
	if n := forwardCount(t, h.db, h.source.MailboxID); n != 2 {
		t.Fatalf("forward_count = %d after two forwarded replies, want 2", n)
	}
}

// TestReplyForwardStopsAtTheAttemptCeiling proves retries are bounded: a reply
// that keeps failing is attempted replyForwardMaxAttempts times and is then left
// as a terminal, inspectable failure instead of being retried forever.
func TestReplyForwardStopsAtTheAttemptCeiling(t *testing.T) {
	shrinkReplyForwardRetry(t)
	h := newReplyForwardTestApp(t)
	raw := replyForwardTestMessage("ceiling@example.net", "Re: undeliverable")

	// Every round runs while the platform queue is unavailable, so every attempt
	// fails. The row is aged between rounds so the backoff never masks the
	// ceiling this test is about.
	h.app.manager = closedForwardingManager(t)
	for i := 1; i <= replyForwardMaxAttempts; i++ {
		ageReplyForwardRow(t, h.db, h.source.RuleID, time.Hour)
		if err := h.app.forwardOneReply(h.source, raw); err == nil {
			t.Fatalf("attempt %d reported success while the queue was unavailable", i)
		}
		if got := singleReplyForwardRow(t, h.db, h.source.RuleID).Attempts; got != i {
			t.Fatalf("after attempt %d the counter is %d", i, got)
		}
	}

	// Past the ceiling a round must not claim the reply at all: it reports no
	// error because there is nothing left to attempt.
	for i := 0; i < 5; i++ {
		ageReplyForwardRow(t, h.db, h.source.RuleID, time.Hour)
		if err := h.app.forwardOneReply(h.source, raw); err != nil {
			t.Fatalf("round %d after the ceiling returned an error: %v", i+1, err)
		}
	}

	terminal := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if terminal.Attempts != replyForwardMaxAttempts {
		t.Fatalf("the reply was attempted %d times, want the ceiling of %d", terminal.Attempts, replyForwardMaxAttempts)
	}
	if terminal.Status != "failed" || terminal.LastError == "" {
		t.Fatalf("the exhausted reply is %s with last_error=%q, want a failed row with an error", terminal.Status, terminal.LastError)
	}
	if n := h.msgr.count(); n != 0 {
		t.Fatalf("a reply that never reached the queue delivered %d messages", n)
	}

	// The terminal state is the documented operator query: a failed row whose
	// attempt counter reached the ceiling.
	if n := countRows(t, h.db, `SELECT count(*) FROM reply_forward_messages
		WHERE rule_id = $1 AND status = 'failed' AND attempts >= $2`, h.source.RuleID, replyForwardMaxAttempts); n != 1 {
		t.Fatalf("the terminal failure is not inspectable as a failed row at the ceiling (%d rows)", n)
	}

	// Even a healthy queue must not resurrect it: the retry budget is spent.
	h.app.manager = h.manager
	ageReplyForwardRow(t, h.db, h.source.RuleID, time.Hour)
	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("a round after the ceiling returned an error: %v", err)
	}
	settleForwarding()

	after := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if after.Attempts != replyForwardMaxAttempts {
		t.Fatalf("a round after the ceiling raised attempts to %d, want it to stay at %d", after.Attempts, replyForwardMaxAttempts)
	}
	if after.Status != "failed" {
		t.Fatalf("a round after the ceiling changed the row to %s", after.Status)
	}
	if n := h.msgr.count(); n != 0 {
		t.Fatalf("a terminally failed reply was forwarded after all (%d messages)", n)
	}
}

// TestReplyForwardRecoversARowLeftMidFlight covers the crash window between
// claiming a reply and recording the outcome. The claim is the lease, so a row
// whose claim is still fresh belongs to a live attempt and must not be touched,
// while a row whose claim outlived the lease must be picked up by the next round
// instead of being stranded as a permanently pending row.
func TestReplyForwardRecoversARowLeftMidFlight(t *testing.T) {
	h := newReplyForwardTestApp(t)
	raw := replyForwardTestMessage("midflight@example.net", "Re: shipment")

	// Create the dedupe row through the production claim, with an unavailable
	// queue, and then rewind it to exactly what that claim alone leaves behind
	// when the process dies before the outcome is recorded: claimed, one attempt
	// spent, no error stored.
	h.app.manager = closedForwardingManager(t)
	if err := h.app.forwardOneReply(h.source, raw); err == nil {
		t.Fatal("a forward whose hand-off failed reported success")
	}
	if _, err := h.db.Exec(`UPDATE reply_forward_messages
		SET status = 'pending', last_error = '', forwarded_at = NULL, updated_at = NOW()
		WHERE rule_id = $1`, h.source.RuleID); err != nil {
		t.Fatalf("rewinding the row to its mid-flight state: %v", err)
	}

	midFlight := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if midFlight.Status != "pending" || midFlight.Attempts != 1 || midFlight.LastError != "" {
		t.Fatalf("the mid-flight row is %s with %d attempts and last_error=%q",
			midFlight.Status, midFlight.Attempts, midFlight.LastError)
	}

	// A live claim: the lease has not expired, so another scan round must leave
	// the row alone. Taking it over here is what would double-send a reply.
	h.app.manager = h.manager
	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("a round during a live claim returned an error: %v", err)
	}
	settleForwarding()

	live := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if live.Status != "pending" || live.Attempts != 1 {
		t.Fatalf("a round took over a live claim: row is %s with %d attempts", live.Status, live.Attempts)
	}
	if n := h.msgr.count(); n != 0 {
		t.Fatalf("a round during a live claim sent %d messages", n)
	}

	// The owner died: the lease expires and the next round owns the reply again.
	ageReplyForwardRow(t, h.db, h.source.RuleID, 2*replyForwardClaimLease)
	if err := h.app.forwardOneReply(h.source, raw); err != nil {
		t.Fatalf("recovering the interrupted reply failed: %v", err)
	}

	recovered := singleReplyForwardRow(t, h.db, h.source.RuleID)
	if recovered.Status != "forwarded" || recovered.Attempts != 2 {
		t.Fatalf("the recovered row is %s with %d attempts, want forwarded with 2", recovered.Status, recovered.Attempts)
	}
	if !recovered.ForwardedAt.Valid {
		t.Fatal("the recovered forward was not marked forwarded")
	}
	if n := forwardCount(t, h.db, h.source.MailboxID); n != 1 {
		t.Fatalf("forward_count = %d after recovering one reply, want 1", n)
	}
	if got := waitForDeliveries(t, h.msgr, 1); len(got) != 1 {
		t.Fatalf("recovering the interrupted reply delivered %d messages, want 1", len(got))
	}
}
