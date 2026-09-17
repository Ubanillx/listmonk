package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/labstack/echo/v4"
)

// An organization's single unified reply mailbox (organizations.reply_mailbox_id)
// replaces the removed per-allocation reply mailbox: every pool audience the
// organization delivers to replies through it, and the organization manager
// configures it from the organization workspace through
// PUT /api/organizations/:id/reply-mailbox. A null reply_mailbox_id clears the
// setting. The handler only authorizes the caller; the core setter re-verifies
// that the mailbox belongs to the organization.
//
// These tests drive the real handler over a throwaway database installed from
// schema.sql, following the database-backed pattern of this package (see
// newAdminTestDB and reply_mailbox_campaigns_test.go): the DSN comes from
// ADMIN_AUTH_TEST_DSN (falling back to MIGRATION_TEST_DSN) and the test is
// skipped when neither is set.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	export ADMIN_AUTH_TEST_DSN="postgres://user:pass@host:5432/db?sslmode=disable"   # POSIX
//	go test ./cmd/ -run 'TestSetOrganizationReplyMailbox' -v
const (
	organizationReplyMailboxAdminUser    = 21
	organizationReplyMailboxManagerUser  = 22
	organizationReplyMailboxMemberUser   = 23
	organizationReplyMailboxOutsiderUser = 24

	organizationReplyMailboxHomeOrgID  = 301
	organizationReplyMailboxOtherOrgID = 302

	organizationReplyMailboxHomeAddress  = "home-replies@example.test"
	organizationReplyMailboxOtherAddress = "other-replies@example.test"
)

// newOrganizationReplyMailboxTestApp wires the real core over a throwaway
// database so authorization and persistence run through the actual handler.
func newOrganizationReplyMailboxTestApp(t *testing.T) *App {
	t.Helper()

	db := newAdminTestDB(t)
	queries := prepareQueries(readTestQueries(t), db, koanf.New("."))

	langB, err := os.ReadFile(filepath.Join("..", "i18n", "en.json"))
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	logger := log.New(io.Discard, "", 0)

	return &App{
		db:   db,
		core: core.New(&core.Opt{DB: db, Queries: queries, I18n: translator, Log: logger}, nil),
		i18n: translator,
		log:  logger,
	}
}

// seedOrganizationReplyMailboxFixtures provisions the two organizations the
// mailbox rule is decided on and one active mailbox per organization. The
// platform administrator deliberately receives no membership row.
func seedOrganizationReplyMailboxFixtures(t *testing.T, db *sqlx.DB) (homeMailboxID, otherMailboxID int) {
	t.Helper()

	// schema.sql leaves role rows to the first-time setup; role 1 exists here
	// only as the users.user_role_id reference (and the Super Admin role id).
	if _, err := db.Exec(`INSERT INTO roles (id, name, permissions) VALUES (1, 'Super Admin', '{}')`); err != nil {
		t.Fatalf("seeding the super admin role: %v", err)
	}
	for _, u := range []struct {
		id   int
		name string
	}{
		{organizationReplyMailboxAdminUser, "organization-reply-mailbox-admin"},
		{organizationReplyMailboxManagerUser, "organization-reply-mailbox-manager"},
		{organizationReplyMailboxMemberUser, "organization-reply-mailbox-member"},
		{organizationReplyMailboxOutsiderUser, "organization-reply-mailbox-outsider"},
	} {
		if _, err := db.Exec(`INSERT INTO users (id, username, email, name, status, user_role_id) VALUES ($1, $2, $3, $4, 'enabled', 1)`,
			u.id, u.name, u.name+"@example.com", u.name); err != nil {
			t.Fatalf("seeding user %d: %v", u.id, err)
		}
	}
	for _, org := range []struct {
		id   int
		name string
	}{
		{organizationReplyMailboxHomeOrgID, "Organization Reply Mailbox Home"},
		{organizationReplyMailboxOtherOrgID, "Organization Reply Mailbox Other"},
	} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, status, created_by_user_id) VALUES ($1, $2, $3, $4)`,
			org.id, org.name, "active", organizationReplyMailboxAdminUser); err != nil {
			t.Fatalf("seeding organization %d: %v", org.id, err)
		}
	}
	for _, m := range []struct {
		orgID  int
		userID int
		role   string
	}{
		{organizationReplyMailboxHomeOrgID, organizationReplyMailboxManagerUser, "manager"},
		{organizationReplyMailboxHomeOrgID, organizationReplyMailboxMemberUser, "member"},
	} {
		if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
			m.orgID, m.userID, m.role); err != nil {
			t.Fatalf("seeding membership (%d, %d): %v", m.orgID, m.userID, err)
		}
	}

	seed := func(target *int, organizationID int, email string) {
		t.Helper()

		if err := db.Get(target, `INSERT INTO reply_mailboxes (user_id, organization_id, email, status, verified_at)
			VALUES ($1, $2, $3, 'active', NOW()) RETURNING id`,
			organizationReplyMailboxAdminUser, organizationID, email); err != nil {
			t.Fatalf("seeding reply mailbox %q: %v", email, err)
		}
	}
	seed(&homeMailboxID, organizationReplyMailboxHomeOrgID, organizationReplyMailboxHomeAddress)
	seed(&otherMailboxID, organizationReplyMailboxOtherOrgID, organizationReplyMailboxOtherAddress)
	return homeMailboxID, otherMailboxID
}

// newOrganizationReplyMailboxContext builds the PUT /api/organizations/:id/reply-mailbox
// request the handler sees, including the workspace header an organization
// manager's browser sends for its active organization.
func newOrganizationReplyMailboxContext(t *testing.T, e *echo.Echo, user auth.User, workspaceOrgID, pathOrgID int, mailboxID *int) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	body, err := json.Marshal(map[string]any{"reply_mailbox_id": mailboxID})
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/organizations/"+strconv.Itoa(pathOrgID)+"/reply-mailbox", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if workspaceOrgID > 0 {
		req.Header.Set(workspaceHeader, strconv.Itoa(workspaceOrgID))
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/organizations/:id/reply-mailbox")
	c.SetParamNames("id")
	c.SetParamValues(strconv.Itoa(pathOrgID))
	c.Set(auth.UserHTTPCtxKey, user)
	return c, rec
}

// organizationReplyMailboxStatus digs the HTTP status out of an echo error so
// the cases read as the API contract the management page sees.
func organizationReplyMailboxStatus(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return http.StatusOK
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("handler returned a non-HTTP error: %v", err)
	}
	return httpErr.Code
}

// organizationReplyMailboxValue reads the persisted column; nil means the
// organization has no unified reply mailbox configured.
func organizationReplyMailboxValue(t *testing.T, db *sqlx.DB, organizationID int) *int {
	t.Helper()

	var value *int
	if err := db.Get(&value, `SELECT reply_mailbox_id FROM organizations WHERE id=$1`, organizationID); err != nil {
		t.Fatalf("reading the unified reply mailbox of organization %d: %v", organizationID, err)
	}
	return value
}

func organizationReplyMailboxPtr(value int) *int {
	return &value
}

// requireOrganizationReplyMailboxPayload asserts the list response the
// management page consumes carries the organization's configured mailbox
// (GET /api/organizations and GET /api/organizations/me).
func requireOrganizationReplyMailboxPayload(t *testing.T, rec *httptest.ResponseRecorder, organizationID, mailboxID int, address string) {
	t.Helper()

	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding the organization list %q: %v", rec.Body.String(), err)
	}
	for _, row := range payload.Data {
		id, _ := row["id"].(float64)
		if int(id) != organizationID {
			continue
		}
		gotID, _ := row["reply_mailbox_id"].(float64)
		if int(gotID) != mailboxID {
			t.Fatalf("organization %d reply_mailbox_id = %v, want %d (response %s)",
				organizationID, row["reply_mailbox_id"], mailboxID, rec.Body.String())
		}
		if row["reply_mailbox_email"] != address {
			t.Fatalf("organization %d reply_mailbox_email = %v, want %q (response %s)",
				organizationID, row["reply_mailbox_email"], address, rec.Body.String())
		}
		return
	}
	t.Fatalf("organization %d is missing from the list response %s", organizationID, rec.Body.String())
}

// TestSetOrganizationReplyMailbox pins the new unified-mailbox contract: the
// active organization's manager may set and clear it, the value is persisted on
// the organization, and every other caller is rejected without mutating the
// column.
func TestSetOrganizationReplyMailbox(t *testing.T) {
	a := newOrganizationReplyMailboxTestApp(t)
	db := a.db
	homeMailbox, otherMailbox := seedOrganizationReplyMailboxFixtures(t, db)

	platformAdmin := auth.User{Base: auth.Base{ID: organizationReplyMailboxAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	manager := auth.User{Base: auth.Base{ID: organizationReplyMailboxManagerUser}}
	ordinaryMember := auth.User{Base: auth.Base{ID: organizationReplyMailboxMemberUser}}
	outsider := auth.User{Base: auth.Base{ID: organizationReplyMailboxOutsiderUser}}

	e := echo.New()

	t.Run("organization manager sets and clears the unified reply mailbox", func(t *testing.T) {
		c, rec := newOrganizationReplyMailboxContext(t, e, manager,
			organizationReplyMailboxHomeOrgID, organizationReplyMailboxHomeOrgID, &homeMailbox)
		if err := a.SetOrganizationReplyMailbox(c); err != nil {
			t.Fatalf("setting the unified reply mailbox = %v, want success", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("response status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Data bool `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || !payload.Data {
			t.Fatalf("response body = %q, want data:true (%v)", rec.Body.String(), err)
		}
		if got := organizationReplyMailboxValue(t, db, organizationReplyMailboxHomeOrgID); got == nil || *got != homeMailbox {
			t.Fatalf("persisted reply_mailbox_id = %v, want %d", got, homeMailbox)
		}

		// A null reply_mailbox_id clears the setting.
		c, rec = newOrganizationReplyMailboxContext(t, e, manager,
			organizationReplyMailboxHomeOrgID, organizationReplyMailboxHomeOrgID, nil)
		if err := a.SetOrganizationReplyMailbox(c); err != nil {
			t.Fatalf("clearing the unified reply mailbox = %v, want success", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("clear response status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := organizationReplyMailboxValue(t, db, organizationReplyMailboxHomeOrgID); got != nil {
			t.Fatalf("persisted reply_mailbox_id = %d, want NULL after clearing", *got)
		}
	})
	t.Run("rejections do not mutate the organization", func(t *testing.T) {
		cases := []struct {
			name        string
			user        auth.User
			workspace   int
			pathOrg     int
			mailbox     *int
			wantStatus  int
			wantMessage string
		}{
			{
				name:        "a mailbox owned by another organization",
				user:        manager,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxHomeOrgID,
				mailbox:     &otherMailbox,
				wantStatus:  http.StatusForbidden,
				wantMessage: "reply mailbox must belong to the organization",
			},
			{
				name:        "a platform administrator",
				user:        platformAdmin,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxHomeOrgID,
				mailbox:     &homeMailbox,
				wantStatus:  http.StatusForbidden,
				wantMessage: "reply mailbox must be configured in the organization workspace",
			},
			{
				name:        "an ordinary organization member",
				user:        ordinaryMember,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxHomeOrgID,
				mailbox:     &homeMailbox,
				wantStatus:  http.StatusForbidden,
				wantMessage: "organization manager permission required",
			},
			{
				name:        "a non-member",
				user:        outsider,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxHomeOrgID,
				mailbox:     &homeMailbox,
				wantStatus:  http.StatusForbidden,
				wantMessage: "not an active organization member",
			},
			{
				name:        "an organization other than the active workspace",
				user:        manager,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxOtherOrgID,
				mailbox:     &homeMailbox,
				wantStatus:  http.StatusForbidden,
				wantMessage: "organization scope mismatch",
			},
			{
				name:        "an unknown organization",
				user:        manager,
				workspace:   999999,
				pathOrg:     999999,
				mailbox:     &homeMailbox,
				wantStatus:  http.StatusNotFound,
				wantMessage: "organization not found",
			},
			{
				// The core setter treats an unknown mailbox id exactly like a
				// mailbox of another organization: both answer 403, asserted
				// deliberately here.
				name:        "an unknown mailbox id",
				user:        manager,
				workspace:   organizationReplyMailboxHomeOrgID,
				pathOrg:     organizationReplyMailboxHomeOrgID,
				mailbox:     organizationReplyMailboxPtr(999999),
				wantStatus:  http.StatusForbidden,
				wantMessage: "reply mailbox must belong to the organization",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c, _ := newOrganizationReplyMailboxContext(t, e, tc.user, tc.workspace, tc.pathOrg, tc.mailbox)
				err := a.SetOrganizationReplyMailbox(c)
				if got := organizationReplyMailboxStatus(t, err); got != tc.wantStatus {
					t.Fatalf("status = %d (%v), want %d", got, err, tc.wantStatus)
				}
				if tc.wantMessage != "" {
					httpErr, _ := err.(*echo.HTTPError)
					if httpErr == nil || httpErr.Message != tc.wantMessage {
						t.Fatalf("error message = %v, want %q", err, tc.wantMessage)
					}
				}
				if got := organizationReplyMailboxValue(t, db, organizationReplyMailboxHomeOrgID); got != nil {
					t.Fatalf("rejected request left reply_mailbox_id = %d, want NULL", *got)
				}
			})
		}
	})

	t.Run("organization list responses expose the configured unified reply mailbox", func(t *testing.T) {
		c, rec := newOrganizationReplyMailboxContext(t, e, manager,
			organizationReplyMailboxHomeOrgID, organizationReplyMailboxHomeOrgID, &homeMailbox)
		if err := a.SetOrganizationReplyMailbox(c); err != nil {
			t.Fatalf("setting the unified reply mailbox = %v, want success", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("response status = %d, want %d", rec.Code, http.StatusOK)
		}

		// The manager's workspace switcher (GET /api/organizations/me).
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/me", nil)
		recMe := httptest.NewRecorder()
		cMe := e.NewContext(req, recMe)
		cMe.SetPath("/api/organizations/me")
		cMe.Set(auth.UserHTTPCtxKey, manager)
		if err := a.GetMyOrganizations(cMe); err != nil {
			t.Fatalf("GetMyOrganizations() = %v, want success", err)
		}
		requireOrganizationReplyMailboxPayload(t, recMe, organizationReplyMailboxHomeOrgID, homeMailbox, organizationReplyMailboxHomeAddress)

		// The platform management list (GET /api/organizations).
		req = httptest.NewRequest(http.MethodGet, "/api/organizations", nil)
		recAll := httptest.NewRecorder()
		cAll := e.NewContext(req, recAll)
		cAll.SetPath("/api/organizations")
		cAll.Set(auth.UserHTTPCtxKey, platformAdmin)
		if err := a.GetOrganizations(cAll); err != nil {
			t.Fatalf("GetOrganizations() = %v, want success", err)
		}
		requireOrganizationReplyMailboxPayload(t, recAll, organizationReplyMailboxHomeOrgID, homeMailbox, organizationReplyMailboxHomeAddress)
	})
}
