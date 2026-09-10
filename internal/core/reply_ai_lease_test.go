package core

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/knadh/listmonk/models"
)

// The tests below need a real PostgreSQL server: lease safety is defined by row
// locking, READ COMMITTED re-checks, and ON CONFLICT semantics that no stub can
// reproduce. Run them with REPLY_AI_LEASE_TEST_DSN pointing at a disposable
// database. They create and drop their own throwaway schema and never touch the
// public schema or an existing row.
const (
	replyAITestDSNEnv = "REPLY_AI_LEASE_TEST_DSN"

	// replyAITestGate is evaluated by both the harness and the test-only
	// trigger, so the parked worker and the releasing session agree on a key.
	replyAITestGate = `hashtext('listmonk_reply_ai_lease_test_gate')::BIGINT`

	replyAITestOrganizationID = 10
	replyAITestPoolID         = 42
	replyAITestPoolContactID  = 7
	replyAITestSegmentID      = 3

	replyAITestRequeueToken = "22222222-2222-2222-2222-222222222222"
)

var replyAITestAccess = models.WorkspaceAccess{
	Workspace: models.Workspace{OrganizationID: replyAITestOrganizationID, Role: models.OrganizationMemberRoleManager},
	UserID:    1,
}

// replyAITestDDL mirrors the production shapes of the three tables the pool
// reply-AI path touches. reply_ai_events matches schema.sql plus the pool
// columns from internal/migrations/v6.27.0; bounces carries the same
// reply_ai_event_id idempotency key added by v6.25.0.
const replyAITestDDL = `
CREATE TABLE reply_ai_events (
    id                     BIGSERIAL PRIMARY KEY,
    reply_mailbox_id       INTEGER NOT NULL,
    customer_id            INTEGER NULL,
    pool_contact_id        BIGINT NULL,
    pool_id                INTEGER NULL,
    source_segment_id      BIGINT NULL,
    source_organization_id BIGINT NULL,
    message_key            TEXT NOT NULL,
    from_email             TEXT NOT NULL DEFAULT '',
    subject                TEXT NOT NULL DEFAULT '',
    body                   TEXT NOT NULL DEFAULT '',
    body_hash              TEXT NOT NULL DEFAULT '',
    intent                 TEXT NOT NULL DEFAULT 'other' CHECK (intent IN ('unsubscribe','complaint','other')),
    confidence             DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason_code            TEXT NOT NULL DEFAULT '',
    model                  TEXT NOT NULL DEFAULT '',
    action                 TEXT NOT NULL DEFAULT 'pending' CHECK (action IN ('pending','ignored','blocklisted')),
    status                 TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','processed','ignored','failed')),
    attempts               INTEGER NOT NULL DEFAULT 0,
    last_error             TEXT NOT NULL DEFAULT '',
    received_at            TIMESTAMPTZ NULL,
    classified_at          TIMESTAMPTZ NULL,
    actioned_at            TIMESTAMPTZ NULL,
    next_attempt_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_expires_at       TIMESTAMPTZ NULL,
    lease_token            UUID NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reply_mailbox_id, message_key)
);
CREATE INDEX idx_reply_ai_events_claim ON reply_ai_events(status, next_attempt_at, created_at);

CREATE TABLE bounces (
    id                     SERIAL PRIMARY KEY,
    customer_id            INTEGER NULL,
    pool_contact_id        BIGINT NULL,
    source_pool_id         INTEGER NULL,
    source_segment_id      BIGINT NULL,
    source_organization_id BIGINT NULL,
    campaign_id            INTEGER NULL,
    type                   TEXT NOT NULL DEFAULT 'hard',
    source                 TEXT NOT NULL DEFAULT '',
    meta                   JSONB NOT NULL DEFAULT '{}',
    reply_ai_event_id      BIGINT NULL,
    created_at             TIMESTAMPTZ DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_bounces_reply_ai_event ON bounces(reply_ai_event_id);

CREATE TABLE pool_segment_exclusions (
    pool_id         INTEGER NOT NULL,
    organization_id BIGINT NOT NULL,
    contact_id      BIGINT NOT NULL,
    segment_id      BIGINT NULL,
    reason          TEXT NOT NULL DEFAULT 'manual',
    source          TEXT NOT NULL DEFAULT 'segment',
    removed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    restored_at     TIMESTAMPTZ,
    PRIMARY KEY (pool_id, organization_id, contact_id)
);

-- Test-only gate. A session that opts in with
-- SET listmonk_test.pause_reply_ai_bounce = 'on' parks inside the complaint
-- insert until the harness releases the advisory lock. That freezes the
-- lease-check -> side-effect window so the two-worker race is deterministic
-- instead of timing dependent. No production code is involved.
CREATE FUNCTION reply_ai_test_gate() RETURNS trigger LANGUAGE plpgsql AS $gate$
BEGIN
    IF current_setting('listmonk_test.pause_reply_ai_bounce', TRUE) = 'on' THEN
        PERFORM pg_advisory_lock(` + replyAITestGate + `);
    END IF;
    RETURN NEW;
END $gate$;
CREATE TRIGGER reply_ai_test_gate BEFORE INSERT ON bounces
    FOR EACH ROW EXECUTE FUNCTION reply_ai_test_gate();
`

// replyAITestEnv owns one throwaway schema and the control session used for
// fixtures, assertions, and the concurrency gate.
type replyAITestEnv struct {
	t          *testing.T
	schema     string
	admin      *sqlx.DB
	claimQuery string
}

func newReplyAITestEnv(t *testing.T) *replyAITestEnv {
	t.Helper()
	dsn := os.Getenv(replyAITestDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set", replyAITestDSNEnv)
	}

	env := &replyAITestEnv{t: t, schema: fmt.Sprintf("reply_ai_lease_%d", time.Now().UnixNano())}
	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	admin.SetMaxOpenConns(1)
	admin.SetMaxIdleConns(1)
	env.admin = admin
	// Cleanups run last-in-first-out, so this runs after every worker session
	// has been closed and any still parked worker has been released.
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP SCHEMA IF EXISTS ` + env.schema + ` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		_ = admin.Close()
	})
	if _, err := admin.Exec(`CREATE SCHEMA ` + env.schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := admin.Exec(`SET search_path TO ` + env.schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := admin.Exec(replyAITestDDL); err != nil {
		t.Fatalf("install test schema: %v", err)
	}
	env.claimQuery = replyAITestClaimQuery(t)
	return env
}

// replyAITestSession is a dedicated single-connection session. MaxOpenConns(1)
// keeps session state (search_path and the test-only pause setting) stable for
// every statement the simulated worker issues.
type replyAITestSession struct {
	*sqlx.DB
	pid int
}

func (env *replyAITestEnv) session(pauseComplaintInsert bool) *replyAITestSession {
	env.t.Helper()
	db, err := sqlx.Connect("postgres", os.Getenv(replyAITestDSNEnv))
	if err != nil {
		env.t.Fatalf("connect worker session: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	env.t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`SET search_path TO ` + env.schema); err != nil {
		env.t.Fatalf("set worker search path: %v", err)
	}
	session := &replyAITestSession{DB: db}
	if err := db.Get(&session.pid, `SELECT pg_backend_pid()`); err != nil {
		env.t.Fatalf("read worker backend pid: %v", err)
	}
	if pauseComplaintInsert {
		if _, err := db.Exec(`SET listmonk_test.pause_reply_ai_bounce = 'on'`); err != nil {
			env.t.Fatalf("arm the complaint insert gate: %v", err)
		}
	}
	return session
}

// claim runs the shipped claim-reply-ai-event statement so reclaim semantics
// (FOR UPDATE SKIP LOCKED plus lease rotation) are exercised as deployed.
func (env *replyAITestEnv) claim(session *replyAITestSession) (models.ReplyAIEvent, error) {
	env.t.Helper()
	var event models.ReplyAIEvent
	err := session.Get(&event, env.claimQuery)
	return event, err
}

func (env *replyAITestEnv) seedPendingEvent() int {
	env.t.Helper()
	var id int
	if err := env.admin.Get(&id, `
		INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
		VALUES (1, 'msg-1', 'pool@example.invalid', 'Re: quote', 'stop emailing me', 'hash', NOW())
		RETURNING id`); err != nil {
		env.t.Fatalf("seed reply AI event: %v", err)
	}
	return id
}

func (env *replyAITestEnv) expireLease(eventID int) {
	env.t.Helper()
	if _, err := env.admin.Exec(`
		UPDATE reply_ai_events SET lease_expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, eventID); err != nil {
		env.t.Fatalf("expire lease: %v", err)
	}
}

func (env *replyAITestEnv) apply(session *replyAITestSession, action ReplyAIAction) error {
	env.t.Helper()
	return (&Core{db: session.DB}).ApplyReplyAIAction(replyAITestAccess, action)
}

// countComplaintBounces counts the complaint side effect by its own columns so
// a duplicate written without an event id is still visible.
func (env *replyAITestEnv) countComplaintBounces() int {
	env.t.Helper()
	var n int
	if err := env.admin.Get(&n, `SELECT COUNT(*) FROM bounces WHERE type = 'complaint' AND source = $1`, models.ReplyAISource); err != nil {
		env.t.Fatalf("count complaint bounces: %v", err)
	}
	return n
}

func (env *replyAITestEnv) countEventBounces(eventID int) int {
	env.t.Helper()
	var n int
	if err := env.admin.Get(&n, `SELECT COUNT(*) FROM bounces WHERE reply_ai_event_id = $1`, eventID); err != nil {
		env.t.Fatalf("count event bounces: %v", err)
	}
	return n
}

func (env *replyAITestEnv) countExclusions() int {
	env.t.Helper()
	var n int
	if err := env.admin.Get(&n, `SELECT COUNT(*) FROM pool_segment_exclusions`); err != nil {
		env.t.Fatalf("count exclusions: %v", err)
	}
	return n
}

type replyAITestEventState struct {
	Status     string         `db:"status"`
	Action     string         `db:"action"`
	Attempts   int            `db:"attempts"`
	ActionedAt sql.NullTime   `db:"actioned_at"`
	LeaseToken sql.NullString `db:"lease_token"`
}

func (env *replyAITestEnv) eventState(eventID int) replyAITestEventState {
	env.t.Helper()
	var state replyAITestEventState
	if err := env.admin.Get(&state, `
		SELECT status, action, attempts, actioned_at, lease_token FROM reply_ai_events WHERE id = $1`, eventID); err != nil {
		env.t.Fatalf("read event state: %v", err)
	}
	return state
}

// requeueEvent puts a terminal event back into processing with a fresh lease,
// as a retry or a deliberate re-run of the same durable event would.
func (env *replyAITestEnv) requeueEvent(eventID int, token string) {
	env.t.Helper()
	if _, err := env.admin.Exec(`
		UPDATE reply_ai_events
		SET status = 'processing', action = 'pending', intent = 'other', confidence = 0,
			reason_code = '', model = '', actioned_at = NULL, attempts = 1,
			body = 'stop emailing me', lease_expires_at = NOW() + INTERVAL '2 minutes',
			lease_token = $2::UUID, updated_at = NOW()
		WHERE id = $1`, eventID, token); err != nil {
		env.t.Fatalf("requeue event: %v", err)
	}
}

func replyAITestAction(eventID int, token string) ReplyAIAction {
	return ReplyAIAction{
		EventID:              eventID,
		LeaseToken:           token,
		PoolContactID:        replyAITestPoolContactID,
		PoolID:               replyAITestPoolID,
		SourceSegmentID:      replyAITestSegmentID,
		SourceOrganizationID: int64(replyAITestOrganizationID),
		Intent:               models.ReplyAIIntentComplaint,
		Confidence:           0.97,
		ReasonCode:           "explicit_complaint",
		Model:                "test-model",
		OccurredAt:           time.Now().UTC(),
	}
}

func replyAITestClaimQuery(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "queries", "replies.sql"))
	if err != nil {
		t.Fatalf("read queries/replies.sql: %v", err)
	}
	queries, err := goyesql.ParseBytes(body)
	if err != nil {
		t.Fatalf("parse queries/replies.sql: %v", err)
	}
	claim, ok := queries["claim-reply-ai-event"]
	if !ok || claim.Query == "" {
		t.Fatal("claim-reply-ai-event query is missing")
	}
	return claim.Query
}

// A worker whose lease expired while the classifier ran and whose event was
// reclaimed since must not apply the complaint or touch the event row.
func TestApplyReplyAIActionPoolStaleLeaseCannotCommit(t *testing.T) {
	env := newReplyAITestEnv(t)
	eventID := env.seedPendingEvent()
	claimer := env.session(false)

	stale, err := env.claim(claimer)
	if err != nil {
		t.Fatalf("claim event: %v", err)
	}
	// The classifier outlives the lease, so the event becomes claimable again.
	env.expireLease(eventID)
	reclaimed, err := env.claim(claimer)
	if err != nil {
		t.Fatalf("reclaim expired event: %v", err)
	}
	if reclaimed.LeaseToken == "" || reclaimed.LeaseToken == stale.LeaseToken {
		t.Fatalf("reclaim did not rotate the lease token: %q -> %q", stale.LeaseToken, reclaimed.LeaseToken)
	}

	worker := env.session(false)
	err = env.apply(worker, replyAITestAction(eventID, stale.LeaseToken))
	if !errors.Is(err, ErrReplyAILeaseLost) {
		t.Fatalf("stale worker error = %v, want %v", err, ErrReplyAILeaseLost)
	}
	if n := env.countComplaintBounces(); n != 0 {
		t.Fatalf("stale worker wrote %d complaint bounces, want 0", n)
	}
	if n := env.countEventBounces(eventID); n != 0 {
		t.Fatalf("stale worker wrote %d event bounces, want 0", n)
	}
	if n := env.countExclusions(); n != 0 {
		t.Fatalf("stale worker wrote %d exclusions, want 0", n)
	}
	state := env.eventState(eventID)
	if state.Status != models.ReplyAIEventStatusProcessing || state.LeaseToken.String != reclaimed.LeaseToken ||
		state.Action != models.ReplyAIActionPending || state.ActionedAt.Valid {
		t.Fatalf("stale worker changed the reclaimed event: %+v", state)
	}
}

// Two workers race on one expired-lease event. Exactly one of them may commit
// the terminal transition and its complaint side effect.
func TestApplyReplyAIActionPoolConcurrentWorkersCommitOnce(t *testing.T) {
	env := newReplyAITestEnv(t)
	eventID := env.seedPendingEvent()
	claimer := env.session(false)

	stale, err := env.claim(claimer)
	if err != nil {
		t.Fatalf("claim event: %v", err)
	}
	env.expireLease(eventID)

	parked := env.session(true)
	fresh := env.session(false)
	// Registered last so it runs first: releasing the gate must happen before
	// the parked worker's session is closed.
	t.Cleanup(func() {
		_, _ = env.admin.Exec(`SELECT pg_advisory_unlock(` + replyAITestGate + `)`)
		_, _ = env.admin.Exec(`SELECT pg_cancel_backend($1)`, parked.pid)
	})

	// Hold the gate, then let the stale worker run until it is parked inside
	// the complaint insert: past its lease check, before any write.
	if _, err := env.admin.Exec(`SELECT pg_advisory_lock(` + replyAITestGate + `)`); err != nil {
		t.Fatalf("hold the concurrency gate: %v", err)
	}
	staleResult := make(chan error, 1)
	go func() {
		staleResult <- env.apply(parked, replyAITestAction(eventID, stale.LeaseToken))
	}()
	env.waitForParkedWorker(parked)

	// The second worker claims and applies while the first one is parked.
	var freshErr error
	reclaimed, claimErr := env.claim(fresh)
	if claimErr == nil {
		freshErr = env.apply(fresh, replyAITestAction(eventID, reclaimed.LeaseToken))
	} else if !errors.Is(claimErr, sql.ErrNoRows) {
		t.Fatalf("second claim: %v", claimErr)
	}

	if _, err := env.admin.Exec(`SELECT pg_advisory_unlock(` + replyAITestGate + `)`); err != nil {
		t.Fatalf("release the concurrency gate: %v", err)
	}
	staleErr := <-staleResult

	committed := 0
	if staleErr == nil {
		committed++
	}
	if claimErr == nil && freshErr == nil {
		committed++
	}
	if committed != 1 {
		t.Fatalf("workers that committed the action = %d, want 1 (stale=%v, second claim=%v, second apply=%v)",
			committed, staleErr, claimErr, freshErr)
	}
	if n := env.countComplaintBounces(); n != 1 {
		t.Fatalf("complaint bounces = %d, want exactly 1", n)
	}
	if n := env.countEventBounces(eventID); n != 1 {
		t.Fatalf("bounces tied to the event = %d, want exactly 1", n)
	}
	if n := env.countExclusions(); n != 1 {
		t.Fatalf("pool exclusions = %d, want exactly 1", n)
	}
	state := env.eventState(eventID)
	if state.Status != models.ReplyAIEventStatusProcessed || state.Action != models.ReplyAIActionBlocklisted ||
		!state.ActionedAt.Valid || state.LeaseToken.Valid {
		t.Fatalf("event did not reach exactly one terminal transition: %+v", state)
	}
	if _, err := env.claim(fresh); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("terminal event is still claimable: %v", err)
	}
}

// Processing the same durable event twice must not double the complaint count.
func TestApplyReplyAIActionPoolRepeatedEventKeepsComplaintIdempotent(t *testing.T) {
	env := newReplyAITestEnv(t)
	eventID := env.seedPendingEvent()
	claimer := env.session(false)
	worker := env.session(false)

	first, err := env.claim(claimer)
	if err != nil {
		t.Fatalf("claim event: %v", err)
	}
	if err := env.apply(worker, replyAITestAction(eventID, first.LeaseToken)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if n := env.countComplaintBounces(); n != 1 {
		t.Fatalf("complaint bounces after first apply = %d, want 1", n)
	}

	// The same event reaches a worker a second time under a fresh lease.
	env.requeueEvent(eventID, replyAITestRequeueToken)
	if err := env.apply(worker, replyAITestAction(eventID, replyAITestRequeueToken)); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if n := env.countComplaintBounces(); n != 1 {
		t.Fatalf("complaint bounces after second apply = %d, want exactly 1", n)
	}
	if n := env.countEventBounces(eventID); n != 1 {
		t.Fatalf("bounces tied to the event = %d, want exactly 1", n)
	}
	if n := env.countExclusions(); n != 1 {
		t.Fatalf("pool exclusions = %d, want exactly 1", n)
	}
	state := env.eventState(eventID)
	if state.Status != models.ReplyAIEventStatusProcessed || state.Action != models.ReplyAIActionBlocklisted || !state.ActionedAt.Valid {
		t.Fatalf("unexpected terminal state after the second apply: %+v", state)
	}
}

// waitForParkedWorker blocks until the gated session is waiting on the
// advisory lock, which proves it is past its lease check and has not yet
// written the complaint insert.
func (env *replyAITestEnv) waitForParkedWorker(session *replyAITestSession) {
	env.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var waiting int
		if err := env.admin.Get(&waiting, `
			SELECT COUNT(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted AND pid = $1`, session.pid); err != nil {
			env.t.Fatalf("inspect parked worker locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			env.t.Fatal("worker never reached the complaint insert gate")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
