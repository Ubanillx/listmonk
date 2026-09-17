package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// These tests drive CreateOrgPoolAllocation through the HTTP handler against a real
// PostgreSQL server, because the policy they pin spans two layers: the handler
// separates a platform administrator from an organization manager, and the
// core re-verifies the target organization's active state and membership while
// holding the organization lock. They follow the database-backed test pattern
// of this package: the DSN comes from ADMIN_AUTH_TEST_DSN (falling back to
// MIGRATION_TEST_DSN) and the tests are skipped when neither is set.
//
//	$env:ADMIN_AUTH_TEST_DSN = "postgres://user:pass@host:5432/db?sslmode=disable"   # PowerShell
//	export ADMIN_AUTH_TEST_DSN="postgres://user:pass@host:5432/db?sslmode=disable"   # POSIX
//	go test ./cmd/ -run 'TestCreateOrgPoolAllocation' -v

const (
	orgPoolAllocationTestAdminUser    = 1
	orgPoolAllocationTestManagerUser  = 2
	orgPoolAllocationTestMemberUser   = 3
	orgPoolAllocationTestOutsiderUser = 4

	orgPoolAllocationTestHomeOrgID     = 101
	orgPoolAllocationTestOtherOrgID    = 102
	orgPoolAllocationTestArchivedOrgID = 103
)

// newOrgPoolAllocationTestApp wires the real core over a throwaway database installed
// from schema.sql so authorization runs through the actual handler.
func newOrgPoolAllocationTestApp(t *testing.T) *App {
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

func seedOrgPoolAllocationFixtures(t *testing.T, db *sqlx.DB) {
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
		{orgPoolAllocationTestAdminUser, "platform-admin"},
		{orgPoolAllocationTestManagerUser, "manager"},
		{orgPoolAllocationTestMemberUser, "member"},
		{orgPoolAllocationTestOutsiderUser, "outsider"},
	} {
		if _, err := db.Exec(`INSERT INTO users (id, username, email, name, user_role_id) VALUES ($1, $2, $3, $4, 1)`,
			u.id, u.name, u.name+"@example.com", u.name); err != nil {
			t.Fatalf("seeding user %d: %v", u.id, err)
		}
	}

	for _, org := range []struct {
		id     int
		name   string
		status string
	}{
		{orgPoolAllocationTestHomeOrgID, "Pool Allocation Home", models.OrganizationStatusActive},
		{orgPoolAllocationTestOtherOrgID, "Pool Allocation Other", models.OrganizationStatusActive},
		{orgPoolAllocationTestArchivedOrgID, "Pool Allocation Archived", models.OrganizationStatusArchived},
	} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, status, created_by_user_id) VALUES ($1, $2, $3, $4)`,
			org.id, org.name, org.status, orgPoolAllocationTestAdminUser); err != nil {
			t.Fatalf("seeding organization %d: %v", org.id, err)
		}
	}

	// The platform administrator deliberately receives no membership row.
	for _, m := range []struct {
		orgID  int
		userID int
		role   string
	}{
		{orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestManagerUser, models.OrganizationMemberRoleManager},
		{orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestMemberUser, models.OrganizationMemberRoleMember},
		{orgPoolAllocationTestArchivedOrgID, orgPoolAllocationTestManagerUser, models.OrganizationMemberRoleManager},
	} {
		if _, err := db.Exec(`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
			m.orgID, m.userID, m.role); err != nil {
			t.Fatalf("seeding membership (%d, %d): %v", m.orgID, m.userID, err)
		}
	}
}

func seedOrgPoolAllocationPool(t *testing.T, db *sqlx.DB, name string) int {
	t.Helper()

	var id int
	if err := db.Get(&id, `INSERT INTO customer_lists (uuid, name, type) VALUES (gen_random_uuid(), $1, 'pool') RETURNING id`, name); err != nil {
		t.Fatalf("seeding pool %q: %v", name, err)
	}
	return id
}

func newOrgPoolAllocationTestContext(t *testing.T, e *echo.Echo, user auth.User, workspaceOrgID, poolID int, targetOrgID int64, name string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"pool_id":         poolID,
		"organization_id": targetOrgID,
		"name":            name,
	})
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pools/allocations", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if workspaceOrgID > 0 {
		req.Header.Set(workspaceHeader, strconv.Itoa(workspaceOrgID))
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/pools/allocations")
	c.Set(auth.UserHTTPCtxKey, user)
	return c, rec
}

func requireOrgPoolAllocationRejection(t *testing.T, err error, wantCode int) {
	t.Helper()

	if err == nil {
		t.Fatal("expected the request to be rejected, got no error")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("rejection error = %v (%T), want *echo.HTTPError", err, err)
	}
	if httpErr.Code != wantCode {
		t.Fatalf("rejection status = %d, want %d (%v)", httpErr.Code, wantCode, err)
	}
}

// requireOrgPoolAllocationCreated asserts that one split created the pool allocation,
// its parent binding and the delivery grant together, and returns the allocation ID.
func requireOrgPoolAllocationCreated(t *testing.T, db *sqlx.DB, poolID int, orgID int64) int64 {
	t.Helper()

	var allocation struct {
		ID     int64 `db:"id"`
		ListID int   `db:"list_id"`
	}
	if err := db.Get(&allocation, `SELECT id, list_id FROM org_pool_allocations WHERE pool_id = $1 AND organization_id = $2`, poolID, orgID); err != nil {
		t.Fatalf("no pool allocation was created for pool %d and organization %d: %v", poolID, orgID, err)
	}

	var list struct {
		Type           string `db:"type"`
		PoolParentID   *int   `db:"pool_parent_id"`
		OrganizationID *int64 `db:"organization_id"`
	}
	if err := db.Get(&list, `SELECT type::text, pool_parent_id, organization_id FROM customer_lists WHERE id = $1`, allocation.ListID); err != nil {
		t.Fatalf("reading the pool allocation %d: %v", allocation.ListID, err)
	}
	if list.Type != models.CustomerListTypeOrgPoolAllocation || list.PoolParentID == nil || *list.PoolParentID != poolID {
		t.Fatalf("pool allocation type/parent = %q/%v, want %q/%d", list.Type, list.PoolParentID, models.CustomerListTypeOrgPoolAllocation, poolID)
	}
	if list.OrganizationID == nil || *list.OrganizationID != orgID {
		t.Fatalf("pool allocation organization = %v, want %d", list.OrganizationID, orgID)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM pool_organization_permissions WHERE pool_id = $1 AND organization_id = $2`, poolID, orgID); n != 1 {
		t.Fatalf("delivery grant rows = %d, want 1", n)
	}
	return allocation.ID
}

func requireNoOrgPoolAllocation(t *testing.T, db *sqlx.DB, poolID int, orgID int64) {
	t.Helper()

	if n := countRows(t, db, `SELECT COUNT(*) FROM org_pool_allocations WHERE pool_id = $1 AND organization_id = $2`, poolID, orgID); n != 0 {
		t.Fatalf("rejected request left %d pool allocation row(s)", n)
	}
}

// TestCreateOrgPoolAllocationAuthorization pins the two-tiered split policy: the
// highest administrator may split a first-level pool for any active
// organization without being a member of it, an organization manager may only
// split a pool for its own organization, and ordinary members, non-members and
// archived organizations are rejected without leaving a pool allocation behind.
func TestCreateOrgPoolAllocationAuthorization(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	db := a.db
	seedOrgPoolAllocationFixtures(t, db)

	// The platform administrator must succeed without a membership row.
	if n := countRows(t, db, `SELECT COUNT(*) FROM organization_members WHERE user_id = $1`, orgPoolAllocationTestAdminUser); n != 0 {
		t.Fatalf("test fixture is wrong: the platform administrator has %d membership row(s)", n)
	}

	platformAdmin := auth.User{Base: auth.Base{ID: orgPoolAllocationTestAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	manager := auth.User{Base: auth.Base{ID: orgPoolAllocationTestManagerUser}}
	ordinaryMember := auth.User{Base: auth.Base{ID: orgPoolAllocationTestMemberUser}}
	outsider := auth.User{Base: auth.Base{ID: orgPoolAllocationTestOutsiderUser}}

	e := echo.New()
	tests := []struct {
		name         string
		user         auth.User
		workspaceOrg int
		targetOrg    int64
		wantStatus   int // 0 means the split must succeed.
	}{
		{"platform administrator splits any active organization without membership", platformAdmin, 0, orgPoolAllocationTestOtherOrgID, 0},
		{"organization manager splits its own organization", manager, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestHomeOrgID, 0},
		{"organization manager cannot split another organization", manager, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestOtherOrgID, http.StatusForbidden},
		{"ordinary member cannot split its own organization", ordinaryMember, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestHomeOrgID, http.StatusForbidden},
		{"non-member cannot split the organization", outsider, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestHomeOrgID, http.StatusForbidden},
		{"manager of an archived organization cannot split it", manager, orgPoolAllocationTestArchivedOrgID, orgPoolAllocationTestArchivedOrgID, http.StatusConflict},
		{"platform administrator cannot split an archived organization", platformAdmin, 0, orgPoolAllocationTestArchivedOrgID, http.StatusConflict},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			poolID := seedOrgPoolAllocationPool(t, db, fmt.Sprintf("Pool %d", i))
			c, rec := newOrgPoolAllocationTestContext(t, e, test.user, test.workspaceOrg, poolID, test.targetOrg, fmt.Sprintf("Pool allocation %d", i))
			err := a.CreateOrgPoolAllocation(c)

			if test.wantStatus != 0 {
				requireOrgPoolAllocationRejection(t, err, test.wantStatus)
				requireNoOrgPoolAllocation(t, db, poolID, test.targetOrg)
				return
			}

			if err != nil {
				t.Fatalf("CreateOrgPoolAllocation() = %v, want success", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("response status = %d, want %d", rec.Code, http.StatusOK)
			}
			allocationID := requireOrgPoolAllocationCreated(t, db, poolID, test.targetOrg)

			var payload struct {
				Data models.OrgPoolAllocation `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decoding the response body %q: %v", rec.Body.String(), err)
			}
			if payload.Data.ID != allocationID || payload.Data.OrganizationID != test.targetOrg {
				t.Fatalf("response allocation = %+v, want id %d in organization %d", payload.Data, allocationID, test.targetOrg)
			}
		})
	}

	// The handler only authorizes; the core must still re-verify active
	// membership for a non-platform-admin caller while holding the
	// organization lock, so a stale handler decision cannot create a allocation.
	t.Run("core re-verifies active membership for non-platform-admin callers", func(t *testing.T) {
		poolID := seedOrgPoolAllocationPool(t, db, "Pool core recheck")

		_, err := a.core.CreateOrgPoolAllocation(poolID, orgPoolAllocationTestHomeOrgID, "Pool allocation core recheck", nil, orgPoolAllocationTestOutsiderUser, false)
		requireOrgPoolAllocationRejection(t, err, http.StatusConflict)
		requireNoOrgPoolAllocation(t, db, poolID, orgPoolAllocationTestHomeOrgID)

		if _, err := a.core.CreateOrgPoolAllocation(poolID, orgPoolAllocationTestHomeOrgID, "Pool allocation core recheck", nil, orgPoolAllocationTestManagerUser, false); err != nil {
			t.Fatalf("core rejected an active organization manager: %v", err)
		}
		requireOrgPoolAllocationCreated(t, db, poolID, orgPoolAllocationTestHomeOrgID)
	})
}
