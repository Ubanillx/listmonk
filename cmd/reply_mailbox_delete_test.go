package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// POST /api/profile/reply-mailboxes/:id/delete removes a mailbox for good. The
// owner may always remove their own mailbox, and a manager of the mailbox's
// owning organization may clean up a stale one; every other caller receives the
// same 404 the rest of the reply-mailbox surface returns. Two still-in-use cases
// are refused with 409 instead of silently breaking routing: the organization's
// unified reply mailbox (organizations.reply_mailbox_id) and a mailbox an active
// reply forwarding rule references.
//
// These tests drive the real handler over a throwaway database installed from
// schema.sql, following the database-backed pattern of this package: the DSN
// comes from ADMIN_AUTH_TEST_DSN (falling back to MIGRATION_TEST_DSN) and the
// tests are skipped when neither is set.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	go test ./cmd/ -run 'TestDeleteReplyMailbox' -v

// newReplyMailboxDeleteTestApp wires the handler the way the running app does.
// DeleteReplyMailbox resolves the caller's workspace, and therefore their
// organization membership, through the real core, while the deletion itself
// runs the prepared delete-reply-mailbox statement; the core-wired test app is
// reused and the prepared queries are attached here, because neither existing
// constructor provides both.
func newReplyMailboxDeleteTestApp(t *testing.T) *App {
	t.Helper()

	a := newOrgPoolAllocationTestApp(t)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	return a
}

// replyMailboxDeleteTestContext builds the authenticated
// POST /api/profile/reply-mailboxes/:id/delete request the handler sees,
// including the workspace header a browser sends for its active organization.
// The role id is part of the caller because the loaded user carries it: a
// platform administrator counts as an organization manager, so a context
// without it would model a caller without platform rights. The route's hasID
// middleware normally parses the path id into the context; calling the handler
// directly stores it here, the same way replyForwardRuleTestContext does.
func replyMailboxDeleteTestContext(t *testing.T, e *echo.Echo, userID, roleID, workspaceOrgID, mailboxID int) echo.Context {
	t.Helper()
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/profile/reply-mailboxes/"+strconv.Itoa(mailboxID)+"/delete", nil)
	if workspaceOrgID > 0 {
		req.Header.Set(workspaceHeader, strconv.Itoa(workspaceOrgID))
	}
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/api/profile/reply-mailboxes/:id/delete")
	c.Set("id", mailboxID)
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: userID}, UserRoleID: roleID})
	return c
}

// replyMailboxDeleteStatus digs the HTTP status out of an echo error the
// handler returned, so the cases read as the API contract the UI sees.
func replyMailboxDeleteStatus(t *testing.T, err error) int {
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

// seedReplyMailboxDeleteMailbox inserts one active mailbox the deletion cases
// act on, with an explicit owner and organization.
func seedReplyMailboxDeleteMailbox(t *testing.T, db *sqlx.DB, userID, organizationID int, email string) int {
	t.Helper()

	var id int
	if err := db.Get(&id, `INSERT INTO reply_mailboxes (user_id, organization_id, email, status, verified_at)
		VALUES ($1, $2, $3, 'active', NOW()) RETURNING id`,
		userID, organizationID, email); err != nil {
		t.Fatalf("seeding reply mailbox %q: %v", email, err)
	}
	return id
}

// TestDeleteReplyMailbox locks the deletion contract: the owner may delete
// their own mailbox, an organization manager may delete a member's mailbox of
// the same organization, a mailbox of another organization is out of scope for
// the home workspace, and the unified-mailbox and active-forwarding-rule guards
// answer 409 without removing the row. Every case asserts both the HTTP status
// and whether the row survived.
func TestDeleteReplyMailbox(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	db := a.db
	f := seedReplyMailboxCampaignFixture(t, db)

	// Case 1 deletes memberOwn, so the manager's case needs a second
	// member-owned mailbox in the home organization. The two other-organization
	// cases cover both halves of the workspace scope: a foreign member's mailbox
	// and the manager's own mailbox of that organization, because a mailbox is
	// addressed from its own workspace even for its owner. The plain-member case
	// needs a caller without the Super Admin role the fixture's users carry,
	// because a platform administrator counts as an organization manager.
	memberSecond := seedReplyMailboxDeleteMailbox(t, db, replyMailboxCampaignMemberUser, replyMailboxCampaignHomeOrgID, "member-second@example.com")
	foreignMember := seedReplyMailboxDeleteMailbox(t, db, replyMailboxCampaignMemberUser, replyMailboxCampaignOtherOrgID, "other-org-member@example.com")

	const (
		plainMemberRoleID = 2
		plainMemberUser   = 102
	)
	if _, err := db.Exec(`INSERT INTO roles (id, name, permissions) VALUES ($1, 'Reply Mailbox Member', '{}')`,
		plainMemberRoleID); err != nil {
		t.Fatalf("seeding the plain member role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, email, name, status, user_role_id)
		VALUES ($1, 'reply-mailbox-member-102', 'reply-mailbox-member-102@example.com', 'Reply Mailbox Member', 'enabled', $2)`,
		plainMemberUser, plainMemberRoleID); err != nil {
		t.Fatalf("seeding the plain member: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
		replyMailboxCampaignHomeOrgID, plainMemberUser, models.OrganizationMemberRoleMember); err != nil {
		t.Fatalf("seeding the plain member's membership: %v", err)
	}

	e := echo.New()
	cases := []struct {
		name       string
		userID     int
		roleID     int
		mailboxID  int
		setup      func(t *testing.T)
		wantStatus int
		wantGone   bool
	}{
		{
			name:       "the owner deletes their own organization mailbox",
			userID:     replyMailboxCampaignMemberUser,
			roleID:     auth.SuperAdminRoleID,
			mailboxID:  f.memberOwn,
			wantStatus: http.StatusOK,
			wantGone:   true,
		},
		{
			name:       "the home organization manager deletes a member's mailbox",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     auth.SuperAdminRoleID,
			mailboxID:  memberSecond,
			wantStatus: http.StatusOK,
			wantGone:   true,
		},
		{
			name:       "the home organization manager cannot delete another organization's mailbox",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     auth.SuperAdminRoleID,
			mailboxID:  foreignMember,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "the manager cannot delete their own mailbox of another organization from this workspace",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     auth.SuperAdminRoleID,
			mailboxID:  f.otherOrg,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a plain member cannot delete the manager's mailbox",
			userID:     plainMemberUser,
			roleID:     plainMemberRoleID,
			mailboxID:  f.shared,
			wantStatus: http.StatusNotFound,
		},
		{
			name:      "the organization's unified reply mailbox is refused",
			userID:    replyMailboxCampaignOwnerUser,
			roleID:    auth.SuperAdminRoleID,
			mailboxID: f.shared,
			setup: func(t *testing.T) {
				t.Helper()
				if _, err := db.Exec(`UPDATE organizations SET reply_mailbox_id=$1 WHERE id=$2`,
					f.shared, replyMailboxCampaignHomeOrgID); err != nil {
					t.Fatalf("making mailbox %d the unified reply mailbox of organization %d: %v",
						f.shared, replyMailboxCampaignHomeOrgID, err)
				}
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:      "a mailbox with an active forwarding rule is refused",
			userID:    replyMailboxCampaignOwnerUser,
			roleID:    auth.SuperAdminRoleID,
			mailboxID: f.pending,
			setup: func(t *testing.T) {
				t.Helper()
				if _, err := db.Exec(`INSERT INTO reply_forward_rules
					(reply_mailbox_id, organization_id, target_user_id, target_email)
					VALUES ($1, $2, $3, $4)`,
					f.pending, replyMailboxCampaignHomeOrgID, replyMailboxCampaignOwnerUser, "rules@example.com"); err != nil {
					t.Fatalf("seeding the active forwarding rule for mailbox %d: %v", f.pending, err)
				}
			},
			wantStatus: http.StatusConflict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}

			c := replyMailboxDeleteTestContext(t, e, tc.userID, tc.roleID, replyMailboxCampaignHomeOrgID, tc.mailboxID)
			err := a.DeleteReplyMailbox(c)
			if got := replyMailboxDeleteStatus(t, err); got != tc.wantStatus {
				t.Fatalf("DeleteReplyMailbox() = %d (%v), want %d", got, err, tc.wantStatus)
			}
			if rows := countRows(t, db, `SELECT COUNT(*) FROM reply_mailboxes WHERE id=$1`, tc.mailboxID); (rows == 0) != tc.wantGone {
				t.Fatalf("mailbox %d rows after the request = %d, want gone=%v", tc.mailboxID, rows, tc.wantGone)
			}
		})
	}
}

// replyMailboxListTestContext builds the authenticated
// GET /api/profile/reply-mailboxes request the listing handler sees, including
// the workspace header a browser sends for its selected organization. The role
// id is part of the caller because the loaded user carries it: a platform
// administrator is recognised by the role and counts as an organization
// manager, so a context without it would test a different caller.
func replyMailboxListTestContext(e *echo.Echo, userID, roleID, workspaceOrgID int) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/api/profile/reply-mailboxes", nil)
	if workspaceOrgID > 0 {
		req.Header.Set(workspaceHeader, strconv.Itoa(workspaceOrgID))
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/profile/reply-mailboxes")
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: userID}, UserRoleID: roleID})
	return c, rec
}

// replyMailboxListingByID runs the listing for one caller and returns its rows
// keyed by mailbox id, so every case can name the mailbox it is about.
func replyMailboxListingByID(t *testing.T, a *App, e *echo.Echo, userID, roleID, workspaceOrgID int) map[int]models.ReplyMailbox {
	t.Helper()

	c, rec := replyMailboxListTestContext(e, userID, roleID, workspaceOrgID)
	if err := a.GetReplyMailboxes(c); err != nil {
		t.Fatalf("GetReplyMailboxes() = %v, want 200", err)
	}

	var body struct {
		Data []models.ReplyMailbox `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the listing: %v (%s)", err, rec.Body.String())
	}
	byID := make(map[int]models.ReplyMailbox, len(body.Data))
	for _, row := range body.Data {
		byID[row.ID] = row
	}
	return byID
}

// TestReplyMailboxListingDeletionRights pins the listing's `deletable` flag to
// the rules DeleteReplyMailbox enforces, because the management UI renders a
// delete button only for rows the API marked deletable: the owner always gets
// one, an organization manager also gets one on a member's stale mailbox, and a
// plain member only on their own. The flag has to come from the server because
// the right follows the workspace the request selected rather than the caller's
// memberships, and a platform administrator counts as a manager of it.
func TestReplyMailboxListingDeletionRights(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	db := a.db
	f := seedReplyMailboxCampaignFixture(t, db)

	// A plain member of the home organization, deliberately without the Super
	// Admin role (auth.SuperAdminRoleID) the fixture's other users carry: a
	// platform administrator is treated as an organization manager, which would
	// mask the member case. A second caller keeps the Super Admin role while
	// joining only as a member, which is the case that proves the flag has to
	// come from the server.
	const (
		plainMemberRoleID = 2
		plainMemberUser   = 102
	)
	if _, err := db.Exec(`INSERT INTO roles (id, name, permissions) VALUES ($1, 'Reply Mailbox Member', '{}')`,
		plainMemberRoleID); err != nil {
		t.Fatalf("seeding the plain member role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, email, name, status, user_role_id)
		VALUES ($1, 'reply-mailbox-member-102', 'reply-mailbox-member-102@example.com', 'Reply Mailbox Member', 'enabled', $2)`,
		plainMemberUser, plainMemberRoleID); err != nil {
		t.Fatalf("seeding the plain member: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
		replyMailboxCampaignHomeOrgID, plainMemberUser, models.OrganizationMemberRoleMember); err != nil {
		t.Fatalf("seeding the plain member's membership: %v", err)
	}
	memberMailbox := seedReplyMailboxDeleteMailbox(t, db, plainMemberUser, replyMailboxCampaignHomeOrgID, "member-102@example.com")

	// A platform administrator who belongs to the organization as an ordinary
	// member: the deletion right follows the platform role, not the membership
	// role, which is exactly why the flag cannot be derived in the browser.
	const platformAdminMemberUser = 103
	if _, err := db.Exec(`INSERT INTO users (id, username, email, name, status, user_role_id)
		VALUES ($1, 'reply-mailbox-admin-103', 'reply-mailbox-admin-103@example.com', 'Reply Mailbox Admin', 'enabled', 1)`,
		platformAdminMemberUser); err != nil {
		t.Fatalf("seeding the platform administrator member: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
		replyMailboxCampaignHomeOrgID, platformAdminMemberUser, models.OrganizationMemberRoleMember); err != nil {
		t.Fatalf("seeding the platform administrator's membership: %v", err)
	}

	e := echo.New()
	cases := []struct {
		name       string
		userID     int
		roleID     int
		workspace  int
		mailboxID  int
		wantListed bool
		wantDelete bool
		wantManage bool
	}{
		{
			name:       "the manager may delete a member's mailbox of the organization",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     plainMemberRoleID,
			workspace:  replyMailboxCampaignHomeOrgID,
			mailboxID:  f.memberOwn,
			wantListed: true,
			wantDelete: true,
		},
		{
			name:       "the manager's own mailbox stays manageable and deletable",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     plainMemberRoleID,
			workspace:  replyMailboxCampaignHomeOrgID,
			mailboxID:  f.shared,
			wantListed: true,
			wantDelete: true,
			wantManage: true,
		},
		{
			name:       "a plain member may delete their own mailbox",
			userID:     plainMemberUser,
			roleID:     plainMemberRoleID,
			workspace:  replyMailboxCampaignHomeOrgID,
			mailboxID:  memberMailbox,
			wantListed: true,
			wantDelete: true,
			wantManage: true,
		},
		{
			name:       "a plain member sees the manager's mailbox without a delete right",
			userID:     plainMemberUser,
			roleID:     plainMemberRoleID,
			workspace:  replyMailboxCampaignHomeOrgID,
			mailboxID:  f.shared,
			wantListed: true,
		},
		{
			name:       "a platform administrator who joined as a member still gets the delete right",
			userID:     platformAdminMemberUser,
			roleID:     auth.SuperAdminRoleID,
			workspace:  replyMailboxCampaignHomeOrgID,
			mailboxID:  f.shared,
			wantListed: true,
			wantDelete: true,
		},
		{
			name:       "the personal workspace lists the caller's own mailbox as deletable",
			userID:     replyMailboxCampaignOwnerUser,
			roleID:     auth.SuperAdminRoleID,
			workspace:  0,
			mailboxID:  f.ownerPersonal,
			wantListed: true,
			wantDelete: true,
			wantManage: true,
		},
		{
			name:      "the personal workspace does not list the caller's organization mailbox",
			userID:    replyMailboxCampaignOwnerUser,
			roleID:    auth.SuperAdminRoleID,
			workspace: 0,
			mailboxID: f.shared,
		},
		{
			name:      "the personal workspace does not list another member's personal mailbox",
			userID:    replyMailboxCampaignOwnerUser,
			roleID:    auth.SuperAdminRoleID,
			workspace: 0,
			mailboxID: f.memberPersonal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := replyMailboxListingByID(t, a, e, tc.userID, tc.roleID, tc.workspace)
			row, listed := rows[tc.mailboxID]
			if listed != tc.wantListed {
				t.Fatalf("mailbox %d listed = %v, want %v (user %d, workspace %d, listing %v)",
					tc.mailboxID, listed, tc.wantListed, tc.userID, tc.workspace, rows)
			}
			if !listed {
				return
			}
			if row.Deletable != tc.wantDelete {
				t.Errorf("deletable = %v, want %v (mailbox %d, user %d, workspace %d)",
					row.Deletable, tc.wantDelete, tc.mailboxID, tc.userID, tc.workspace)
			}
			if row.Manageable != tc.wantManage {
				t.Errorf("manageable = %v, want %v (mailbox %d, user %d, workspace %d)",
					row.Manageable, tc.wantManage, tc.mailboxID, tc.userID, tc.workspace)
			}
		})
	}

	// The listings themselves are part of the flag's meaning: a mailbox is only
	// ever deletable from the workspace that lists it, and a caller who is not a
	// member of an organization cannot address that organization's workspace at
	// all, so no flag is ever computed for a foreign organization's mailboxes.
	homeListing := replyMailboxListingByID(t, a, e, plainMemberUser, plainMemberRoleID, replyMailboxCampaignHomeOrgID)
	if len(homeListing) != 4 {
		t.Errorf("home-organization listing returned %d rows, want 4 (shared, memberOwn, pending, member-102): %v",
			len(homeListing), homeListing)
	}
	personalListing := replyMailboxListingByID(t, a, e, replyMailboxCampaignOwnerUser, auth.SuperAdminRoleID, 0)
	if len(personalListing) != 1 {
		t.Errorf("personal listing returned %d rows, want only the caller's own mailbox: %v",
			len(personalListing), personalListing)
	}
	nonMemberCtx, _ := replyMailboxListTestContext(e, plainMemberUser, plainMemberRoleID, replyMailboxCampaignOtherOrgID)
	if err := a.GetReplyMailboxes(nonMemberCtx); replyMailboxDeleteStatus(t, err) != http.StatusForbidden {
		t.Errorf("listing another organization's mailboxes = %v, want 403", err)
	}
}
