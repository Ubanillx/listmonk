package core

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

func TestUserDeletionDBErrMapsForeignKeyViolation(t *testing.T) {
	translator, err := i18n.New([]byte(`{
		"_.code": "en",
		"_.name": "English",
		"users.cantDeleteReferenced": "Related data changed."
	}`))
	if err != nil {
		t.Fatalf("load translations: %v", err)
	}

	err = (&Core{i18n: translator}).userDeletionDBErr(&pq.Error{Code: "23503"})
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *echo.HTTPError", err)
	}
	if httpErr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", httpErr.Code, http.StatusConflict)
	}
	if httpErr.Message != "Related data changed." {
		t.Fatalf("message = %q, want %q", httpErr.Message, "Related data changed.")
	}
}

// Run with LISTMONK_TEST_DATABASE_URL set to an otherwise empty Postgres
// database. The schema is recreated, so this must never point at live data.
func TestDeleteUsersCleansFormerOrganizationMembership(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DATABASE_URL is not set")
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating test fixtures")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	schema, err := os.ReadFile(filepath.Join(root, "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("install schema: %v", err)
	}

	translations, err := os.ReadFile(filepath.Join(root, "i18n", "en.json"))
	if err != nil {
		t.Fatalf("read translations: %v", err)
	}
	translator, err := i18n.New(translations)
	if err != nil {
		t.Fatalf("load translations: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO roles (id, type, name) VALUES (1, 'user', 'Super Admin')`); err != nil {
		t.Fatalf("create user role: %v", err)
	}

	var superAdminID, formerMemberID int
	if err := db.Get(&superAdminID, `
		INSERT INTO users (username, email, name, type, user_role_id, status)
		VALUES ('super-admin', 'super-admin@example.com', 'Super Admin', 'user', 1, 'enabled')
		RETURNING id`); err != nil {
		t.Fatalf("create super admin: %v", err)
	}
	if err := db.Get(&formerMemberID, `
		INSERT INTO users (username, email, name, type, user_role_id, status)
		VALUES ('former-member', 'former-member@example.com', 'Former member', 'user', 1, 'enabled')
		RETURNING id`); err != nil {
		t.Fatalf("create former member: %v", err)
	}

	var organizationID int
	if err := db.Get(&organizationID, `
		INSERT INTO organizations (name, created_by_user_id)
		VALUES ('Former member organization', $1)
		RETURNING id`, formerMemberID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO organization_members (organization_id, user_id, role, removed_at)
		VALUES ($1, $2, 'member', NOW())`, organizationID, formerMemberID); err != nil {
		t.Fatalf("create former membership: %v", err)
	}

	deleteUsers, err := db.Preparex(`
		WITH u AS (
			SELECT COUNT(*) AS num FROM users
			WHERE NOT(id = ANY($1)) AND user_role_id = 1 AND type = 'user' AND status = 'enabled'
		)
		DELETE FROM users WHERE id = ALL($1) AND (SELECT num FROM u) > 0`)
	if err != nil {
		t.Fatalf("prepare user deletion: %v", err)
	}
	t.Cleanup(func() { _ = deleteUsers.Close() })

	c := New(&Opt{
		DB:   db,
		I18n: translator,
		Queries: &models.Queries{
			DeleteUsers: deleteUsers,
		},
	}, nil)
	if err := c.DeleteUsers([]int{formerMemberID}); err != nil {
		t.Fatalf("delete former member: %v", err)
	}

	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM users WHERE id = $1`, formerMemberID); err != nil {
		t.Fatalf("check deleted user: %v", err)
	}
	if count != 0 {
		t.Fatalf("deleted user still exists")
	}
	if err := db.Get(&count, `SELECT COUNT(*) FROM organization_members WHERE user_id = $1`, formerMemberID); err != nil {
		t.Fatalf("check former membership cleanup: %v", err)
	}
	if count != 0 {
		t.Fatalf("former membership still exists")
	}

	var creatorID int
	if err := db.Get(&creatorID, `SELECT created_by_user_id FROM organizations WHERE id = $1`, organizationID); err != nil {
		t.Fatalf("check organization creator: %v", err)
	}
	if creatorID != superAdminID {
		t.Fatalf("organization creator = %d, want %d", creatorID, superAdminID)
	}
}
