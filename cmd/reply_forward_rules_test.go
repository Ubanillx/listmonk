package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// PUT and DELETE /api/organizations/reply-forwarding/:id address a rule by its
// own id: that is the id the resume branch filters on (WHERE id = ...), the id
// the audit mapping records and the id the management UI sends for its toggle.
// disableReplyForwardRule used to match reply_mailbox_id instead, so disabling a
// rule from the UI answered 404 unless the rule and mailbox ids happened to be
// equal. The fixture seeds distinct ids, so a regression to the mailbox-based
// match fails here.
//
// The test follows the database-backed pattern of this package: the DSN comes
// from ADMIN_AUTH_TEST_DSN (falling back to MIGRATION_TEST_DSN) and the test is
// skipped when neither is set.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	go test ./cmd/ -run 'TestReplyForwardRule' -v
const (
	// replyForwardRuleTestManagerUser is the org manager seeded by
	// seedPoolSegmentFixtures as a manager of poolSegmentTestHomeOrgID.
	replyForwardRuleTestManagerUser = 2
	replyForwardRuleTestMailboxID   = 7
	replyForwardRuleTestRuleID      = 42
)

// seedReplyForwardRuleFixture seeds a retained mailbox and one active forwarding
// rule with explicit ids, so the rule id and the mailbox id differ. It also makes
// the organization creator (the preferred resume target) an enabled manager
// member: seedPoolSegmentFixtures leaves users at the schema default ('disabled')
// and deliberately gives the platform administrator no membership.
func seedReplyForwardRuleFixture(t *testing.T, db *sqlx.DB, orgID int) {
	t.Helper()

	if _, err := db.Exec(`UPDATE users SET status = 'enabled' WHERE id = ANY($1::INT[])`,
		pq.Array([]int{poolSegmentTestAdminUser, replyForwardRuleTestManagerUser})); err != nil {
		t.Fatalf("enabling the fixture users: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role)
		VALUES ($1, $2, 'manager')`, orgID, poolSegmentTestAdminUser); err != nil {
		t.Fatalf("seeding the creator membership: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO reply_mailboxes (id, user_id, organization_id, email, status)
		VALUES ($1, $2, $3, 'replies@company.example', 'retained')`,
		replyForwardRuleTestMailboxID, replyForwardRuleTestManagerUser, orgID); err != nil {
		t.Fatalf("seeding the retained reply mailbox: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO reply_forward_rules (id, reply_mailbox_id, organization_id, target_user_id, target_email)
		VALUES ($1, $2, $3, $4, 'manager@company.example')`,
		replyForwardRuleTestRuleID, replyForwardRuleTestMailboxID, orgID, replyForwardRuleTestManagerUser); err != nil {
		t.Fatalf("seeding the forwarding rule: %v", err)
	}
}

func replyForwardRuleTestContext(t *testing.T, e *echo.Echo, method string, id int, body string) echo.Context {
	t.Helper()

	path := "/api/organizations/reply-forwarding/" + strconv.Itoa(id)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(workspaceHeader, strconv.Itoa(poolSegmentTestHomeOrgID))

	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/api/organizations/reply-forwarding/:id")
	// hasID normally parses the path parameter into this context value.
	c.Set("id", id)
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: replyForwardRuleTestManagerUser}})
	return c
}

func replyForwardRuleStatus(t *testing.T, db *sqlx.DB) string {
	t.Helper()

	var status string
	if err := db.Get(&status, `SELECT status FROM reply_forward_rules WHERE id = $1`, replyForwardRuleTestRuleID); err != nil {
		t.Fatalf("reading the forwarding rule status: %v", err)
	}
	return status
}

func requireReplyForwardNotFound(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected the request to be rejected, got no error")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("rejection error = %v (%T), want *echo.HTTPError", err, err)
	}
	if httpErr.Code != http.StatusNotFound {
		t.Fatalf("rejection status = %d, want %d (%v)", httpErr.Code, http.StatusNotFound, err)
	}
}

func TestReplyForwardRuleIdSemantics(t *testing.T) {
	app := newPoolSegmentTestApp(t)
	seedPoolSegmentFixtures(t, app.db)
	seedReplyForwardRuleFixture(t, app.db, poolSegmentTestHomeOrgID)

	e := echo.New()

	// Disabling by the rule id must reach the row.
	if err := app.UpdateReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodPut, replyForwardRuleTestRuleID, `{"status":"disabled"}`)); err != nil {
		t.Fatalf("disabling by rule id: %v", err)
	}
	if got := replyForwardRuleStatus(t, app.db); got != "disabled" {
		t.Fatalf("status after disabling by rule id = %q, want %q", got, "disabled")
	}

	// Resuming by the rule id keeps its existing semantics.
	if err := app.UpdateReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodPut, replyForwardRuleTestRuleID, `{"status":"active"}`)); err != nil {
		t.Fatalf("resuming by rule id: %v", err)
	}
	if got := replyForwardRuleStatus(t, app.db); got != "active" {
		t.Fatalf("status after resuming by rule id = %q, want %q", got, "active")
	}

	// The retained mailbox id is not a rule id.
	requireReplyForwardNotFound(t, app.UpdateReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodPut, replyForwardRuleTestMailboxID, `{"status":"disabled"}`)))
	if got := replyForwardRuleStatus(t, app.db); got != "active" {
		t.Fatalf("status changed to %q after a mailbox-id request, want %q", got, "active")
	}

	// DELETE is an alias for disabling and follows the same id.
	if err := app.DeleteReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodDelete, replyForwardRuleTestRuleID, "")); err != nil {
		t.Fatalf("deleting by rule id: %v", err)
	}
	if got := replyForwardRuleStatus(t, app.db); got != "disabled" {
		t.Fatalf("status after deleting by rule id = %q, want %q", got, "disabled")
	}
	requireReplyForwardNotFound(t, app.DeleteReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodDelete, replyForwardRuleTestMailboxID, "")))

	// A rule of another organization is out of scope for this workspace.
	var otherOrgID int
	if err := app.db.Get(&otherOrgID, `SELECT id FROM organizations WHERE id <> $1 ORDER BY id LIMIT 1`, poolSegmentTestHomeOrgID); err != nil {
		t.Fatalf("reading another organization: %v", err)
	}
	if _, err := app.db.Exec(`UPDATE reply_forward_rules SET organization_id = $1 WHERE id = $2`, otherOrgID, replyForwardRuleTestRuleID); err != nil {
		t.Fatalf("moving the rule to another organization: %v", err)
	}
	requireReplyForwardNotFound(t, app.UpdateReplyForwardRule(
		replyForwardRuleTestContext(t, e, http.MethodPut, replyForwardRuleTestRuleID, `{"status":"active"}`)))
}
