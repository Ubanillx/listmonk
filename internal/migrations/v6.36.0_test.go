package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestActionPermissionUpgradeIsIdempotent(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err = db.Exec(`
		CREATE TEMP TABLE roles (id integer, type text, permissions text[]);
		INSERT INTO roles(id, type, permissions) VALUES
			(1, 'user', ARRAY['customers:manage']),
			(2, 'user', ARRAY['customers:manage', 'campaigns:manage', 'campaigns:get_analytics', 'bounces:manage', 'users:manage']),
			(3, 'user', ARRAY['customers:get']),
			(4, 'api', ARRAY['customers:manage']);
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err = V6_36_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var permissions []string
	if err = db.Select(&permissions, `SELECT unnest(permissions) FROM roles WHERE id=2 ORDER BY 1`); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"customers:delete": true, "customers:blocklist": true, "customers:membership_manage": true,
		"campaigns:send": true, "campaigns:recipients": true,
		"bounces:delete": true, "bounces:blocklist": true,
		"users:tokens": true, "organizations:platform_manage": true,
	}
	for permission := range want {
		count := 0
		for _, got := range permissions {
			if got == permission {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("permission %q appears %d times, want once", permission, count)
		}
	}
	var untouched []string
	if err = db.Select(&untouched, `SELECT unnest(permissions) FROM roles WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if len(untouched) != 1 || untouched[0] != "customers:get" {
		t.Fatalf("unrelated role changed: %v", untouched)
	}
	var apiRole []string
	if err = db.Select(&apiRole, `SELECT unnest(permissions) FROM roles WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if len(apiRole) != 1 || apiRole[0] != "customers:manage" {
		t.Fatalf("non-user role changed: %v", apiRole)
	}
	var superAdmin []string
	if err = db.Select(&superAdmin, `SELECT unnest(permissions) FROM roles WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if len(superAdmin) != 1 || superAdmin[0] != "customers:manage" {
		t.Fatalf("super admin role changed: %v", superAdmin)
	}
}
