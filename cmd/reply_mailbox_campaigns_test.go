package main

import (
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// The campaign reply-mailbox rule is a workspace rule, not an ownership rule.
// An organization workspace shares its customer reply mailboxes, so any member
// may attach a mailbox the organization owns to a campaign of their own (the
// listing marks the caller's own rows as `manageable`), while editing,
// disabling, re-enabling and testing stay with the mailbox owner in
// cmd/reply_mailboxes.go. A personal mailbox, in contrast, stays private to its
// owner.
//
// Before the fix, validateCampaignReplyMailbox required
// `owner_id = access.UserID` for every mailbox. An organization member without
// any organization-management permission therefore could not select the
// organization's reply mailbox: the listing could return it (the campaign
// editor offered it) while every save answered 403 "reply mailbox is not owned
// by this account", and with the owner-filtered listing the editor showed no
// option at all.
//
// These tests drive the real validation over a throwaway database installed
// from schema.sql, following the database-backed pattern of this package: the
// DSN comes from ADMIN_AUTH_TEST_DSN (falling back to MIGRATION_TEST_DSN) and
// the tests are skipped when neither is set.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	go test ./cmd/ -run 'TestReplyMailboxCampaign' -v
const (
	// replyMailboxCampaignOwnerUser owns the organization's shared mailbox and
	// manages the organization.
	replyMailboxCampaignOwnerUser = 11
	// replyMailboxCampaignMemberUser is an ordinary member with no
	// organization-management permission; it is the account from the report.
	replyMailboxCampaignMemberUser = 12

	replyMailboxCampaignHomeOrgID  = 201
	replyMailboxCampaignOtherOrgID = 202
)

// newReplyMailboxCampaignTestApp wires the same database and prepared queries the
// running app uses, over a throwaway database installed from schema.sql. The
// fixture is local to this file so the tests do not depend on the constants of
// other test files.
func newReplyMailboxCampaignTestApp(t *testing.T) *App {
	t.Helper()

	db := newAdminTestDB(t)
	return &App{db: db, queries: prepareQueries(readTestQueries(t), db, koanf.New("."))}
}

// replyMailboxCampaignFixture is the set of mailboxes the campaign rule is
// decided on.
type replyMailboxCampaignFixture struct {
	// shared is owned by the organization manager in the home organization and
	// is the mailbox a plain member wants to select.
	shared int
	// memberOwn is another mailbox of the same organization, owned by the
	// ordinary member.
	memberOwn int
	// memberPersonal belongs to the ordinary member and has no organization.
	memberPersonal int
	// ownerPersonal belongs to the organization manager and has no organization.
	ownerPersonal int
	// otherOrg belongs to the home organization's manager but to a second
	// organization, so it must never be selectable from the home organization.
	otherOrg int
	// pending belongs to the ordinary member in the home organization and has
	// not passed a connection test yet.
	pending int
}

func seedReplyMailboxCampaignFixture(t *testing.T, db *sqlx.DB) replyMailboxCampaignFixture {
	t.Helper()

	// schema.sql leaves role rows to the first-time setup; role 1 exists here
	// only as the users.user_role_id reference.
	if _, err := db.Exec(`INSERT INTO roles (id, name, permissions) VALUES (1, 'Super Admin', '{}')`); err != nil {
		t.Fatalf("seeding the super admin role: %v", err)
	}
	for _, u := range []struct {
		id   int
		name string
	}{
		{replyMailboxCampaignOwnerUser, "reply-mailbox-owner"},
		{replyMailboxCampaignMemberUser, "reply-mailbox-member"},
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
		{replyMailboxCampaignHomeOrgID, "Reply Mailbox Home"},
		{replyMailboxCampaignOtherOrgID, "Reply Mailbox Other"},
	} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, status, created_by_user_id) VALUES ($1, $2, $3, $4)`,
			org.id, org.name, models.OrganizationStatusActive, replyMailboxCampaignOwnerUser); err != nil {
			t.Fatalf("seeding organization %d: %v", org.id, err)
		}
	}
	// The ordinary member belongs to the home organization; the manager role of
	// the owner is irrelevant to the selection rule but keeps the fixture
	// realistic.
	for _, m := range []struct {
		orgID  int
		userID int
		role   string
	}{
		{replyMailboxCampaignHomeOrgID, replyMailboxCampaignOwnerUser, models.OrganizationMemberRoleManager},
		{replyMailboxCampaignHomeOrgID, replyMailboxCampaignMemberUser, models.OrganizationMemberRoleMember},
	} {
		if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
			m.orgID, m.userID, m.role); err != nil {
			t.Fatalf("seeding membership (%d, %d): %v", m.orgID, m.userID, err)
		}
	}

	var f replyMailboxCampaignFixture
	seed := func(target *int, userID, organizationID int, email, status string) {
		t.Helper()

		var id int
		var org any
		if organizationID > 0 {
			org = organizationID
		}
		if err := db.Get(&id, `INSERT INTO reply_mailboxes (user_id, organization_id, email, status, verified_at)
			VALUES ($1, $2, $3, $4, CASE WHEN $4 = 'active' THEN NOW() ELSE NULL END) RETURNING id`,
			userID, org, email, status); err != nil {
			t.Fatalf("seeding reply mailbox %q: %v", email, err)
		}
		*target = id
	}

	seed(&f.shared, replyMailboxCampaignOwnerUser, replyMailboxCampaignHomeOrgID, "shared-replies@example.com", models.ReplyMailboxStatusActive)
	seed(&f.memberOwn, replyMailboxCampaignMemberUser, replyMailboxCampaignHomeOrgID, "member-replies@example.com", models.ReplyMailboxStatusActive)
	seed(&f.memberPersonal, replyMailboxCampaignMemberUser, 0, "member-personal@example.com", models.ReplyMailboxStatusActive)
	seed(&f.ownerPersonal, replyMailboxCampaignOwnerUser, 0, "owner-personal@example.com", models.ReplyMailboxStatusActive)
	seed(&f.otherOrg, replyMailboxCampaignOwnerUser, replyMailboxCampaignOtherOrgID, "other-org-replies@example.com", models.ReplyMailboxStatusActive)
	seed(&f.pending, replyMailboxCampaignMemberUser, replyMailboxCampaignHomeOrgID, "pending-replies@example.com", models.ReplyMailboxStatusPending)

	return f
}

// replyMailboxCampaignStatus digs the HTTP status out of an echo error the
// validation returned, so the cases read as the API contract the campaign
// editor sees.
func replyMailboxCampaignStatus(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return http.StatusOK
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("validation returned a non-HTTP error: %v", err)
	}
	return httpErr.Code
}

// TestReplyMailboxCampaignSelectionIsWorkspaceScoped locks the relaxation:
// a member of the organization may select any active mailbox of that
// organization, including one owned by somebody else, while a mailbox of
// another organization, a personal mailbox of another user and an unverified
// mailbox stay rejected.
func TestReplyMailboxCampaignSelectionIsWorkspaceScoped(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	f := seedReplyMailboxCampaignFixture(t, a.db)

	memberWorkspace := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: replyMailboxCampaignHomeOrgID, OrganizationName: "Reply Mailbox Home"},
		UserID:    replyMailboxCampaignMemberUser,
	}
	personalWorkspace := models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true},
		UserID:    replyMailboxCampaignMemberUser,
	}

	cases := []struct {
		name     string
		access   models.WorkspaceAccess
		mailbox  null.Int
		wantCode int
	}{
		{name: "organization member selects the organization's shared mailbox", access: memberWorkspace, mailbox: null.IntFrom(f.shared), wantCode: http.StatusOK},
		{name: "organization member selects their own organization mailbox", access: memberWorkspace, mailbox: null.IntFrom(f.memberOwn), wantCode: http.StatusOK},
		{name: "organization member selects a mailbox of another organization", access: memberWorkspace, mailbox: null.IntFrom(f.otherOrg), wantCode: http.StatusForbidden},
		{name: "organization member selects their own personal mailbox", access: memberWorkspace, mailbox: null.IntFrom(f.memberPersonal), wantCode: http.StatusForbidden},
		{name: "organization member selects an unverified organization mailbox", access: memberWorkspace, mailbox: null.IntFrom(f.pending), wantCode: http.StatusConflict},
		{name: "organization member clears the selection", access: memberWorkspace, mailbox: null.Int{}, wantCode: http.StatusOK},
		{name: "member selects their own personal mailbox", access: personalWorkspace, mailbox: null.IntFrom(f.memberPersonal), wantCode: http.StatusOK},
		{name: "member selects somebody else's personal mailbox", access: personalWorkspace, mailbox: null.IntFrom(f.ownerPersonal), wantCode: http.StatusForbidden},
		{name: "member selects an organization mailbox from the personal workspace", access: personalWorkspace, mailbox: null.IntFrom(f.shared), wantCode: http.StatusForbidden},
		{name: "member selects an unknown mailbox", access: memberWorkspace, mailbox: null.IntFrom(999999), wantCode: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.validateCampaignReplyMailbox(tc.access, &models.Campaign{ReplyMailboxID: tc.mailbox})
			if got := replyMailboxCampaignStatus(t, err); got != tc.wantCode {
				t.Fatalf("validateCampaignReplyMailbox() = %d (%v), want %d", got, err, tc.wantCode)
			}
		})
	}
}

// TestReplyMailboxCampaignSelectionKeepsManagementOwnerOnly covers the other
// half of the rule: being able to select a shared organization mailbox does not
// hand the member any management capability. The listing exposes it with
// `manageable=false` and every management lookup stays owner-scoped, so the
// member cannot load, edit, disable or test somebody else's mailbox.
func TestReplyMailboxCampaignSelectionKeepsManagementOwnerOnly(t *testing.T) {
	a := newReplyMailboxCampaignTestApp(t)
	f := seedReplyMailboxCampaignFixture(t, a.db)

	var listed []models.ReplyMailbox
	if err := a.queries.GetReplyMailboxes.Select(&listed,
		replyMailboxCampaignMemberUser,
		nullableOrganizationID(replyMailboxCampaignHomeOrgID)); err != nil {
		t.Fatalf("listing the organization mailboxes: %v", err)
	}

	byID := make(map[int]models.ReplyMailbox, len(listed))
	for _, row := range listed {
		byID[row.ID] = row
	}

	shared, ok := byID[f.shared]
	if !ok {
		t.Fatalf("the organization's shared mailbox %d is missing from the listing the campaign editor uses: %v", f.shared, byID)
	}
	if shared.Manageable {
		t.Errorf("shared mailbox manageable = true, want false for a member who does not own it")
	}
	own, ok := byID[f.memberOwn]
	if !ok {
		t.Fatalf("the member's own mailbox %d is missing from the listing", f.memberOwn)
	}
	if !own.Manageable {
		t.Errorf("own mailbox manageable = false, want true")
	}
	if _, ok := byID[f.otherOrg]; ok {
		t.Errorf("a mailbox of another organization leaked into the organization listing")
	}
	if _, ok := byID[f.memberPersonal]; ok {
		t.Errorf("a personal mailbox leaked into the organization listing")
	}

	// Management stays owner-scoped: the owner's own mailbox resolves, the
	// shared one does not.
	var row models.ReplyMailbox
	if err := a.queries.GetReplyMailbox.Get(&row, f.memberOwn, replyMailboxCampaignMemberUser,
		nullableOrganizationID(replyMailboxCampaignHomeOrgID)); err != nil {
		t.Fatalf("the member cannot load their own mailbox: %v", err)
	}
	if err := a.queries.GetReplyMailbox.Get(&row, f.shared, replyMailboxCampaignMemberUser,
		nullableOrganizationID(replyMailboxCampaignHomeOrgID)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("loading the shared mailbox as a non-owner = %v, want sql.ErrNoRows", err)
	}

	// The email is the only thing a member needs for a campaign, and it is the
	// organization's company address rather than the member's own.
	if shared.Email != "shared-replies@example.com" {
		t.Errorf("shared mailbox email = %q, want the organization's company address", shared.Email)
	}
}
