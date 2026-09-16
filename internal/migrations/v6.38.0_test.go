package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestBusinessActionPermissionUpgradeIsIdempotent(t *testing.T) {
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
			(1, 'user', ARRAY['campaigns:manage', 'exports:create']),
			(2, 'user', ARRAY['campaigns:manage', 'campaigns:send', 'exports:create']),
			(3, 'user', ARRAY['campaigns:manage']),
			(4, 'user', ARRAY['exports:download']),
			(5, 'api', ARRAY['campaigns:manage', 'campaigns:send', 'exports:create']);
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err = V6_38_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	checks := map[int]map[string]bool{
		1: {"campaigns:test": false, "campaigns:schedule": false, "campaigns:control": false, "customers:export": false},
		2: {"campaigns:test": true, "campaigns:schedule": true, "campaigns:control": true, "customers:export": true},
		3: {"campaigns:test": true, "campaigns:schedule": false, "campaigns:control": true, "customers:export": false},
		4: {"campaigns:test": false, "campaigns:schedule": false, "campaigns:control": false, "customers:export": false},
		5: {"campaigns:test": false, "campaigns:schedule": false, "campaigns:control": false, "customers:export": false},
	}
	for id, want := range checks {
		var permissions []string
		if err = db.Select(&permissions, `SELECT unnest(permissions) FROM roles WHERE id=$1 ORDER BY 1`, id); err != nil {
			t.Fatal(err)
		}
		got := make(map[string]bool, len(permissions))
		for _, permission := range permissions {
			got[permission] = true
		}
		for permission, expected := range want {
			if got[permission] != expected {
				t.Fatalf("role %d permission %q = %v, want %v (%v)", id, permission, got[permission], expected, permissions)
			}
		}
	}
}
