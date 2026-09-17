package migrations

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// migrationV6_45_0LegacySchema is the pre-v6.45 shape: pool_contacts still
// carries the abandoned company_name column and roles only hold the previous
// permission grants.
const migrationV6_45_0LegacySchema = `
	CREATE TABLE roles (
		id          SERIAL PRIMARY KEY,
		type        TEXT NOT NULL DEFAULT 'user',
		permissions TEXT[] NOT NULL DEFAULT '{}'
	);
	CREATE TABLE pool_contacts (
		id            BIGSERIAL PRIMARY KEY,
		customer_code TEXT NOT NULL DEFAULT '',
		company_name  TEXT NOT NULL DEFAULT '',
		email         TEXT NOT NULL,
		name          TEXT NOT NULL DEFAULT ''
	);
`

// migrationV6_45_0SeedSchema installs one throwaway schema and registers its
// removal, so a test run leaves the shared scratch database clean.
func migrationV6_45_0SeedSchema(t *testing.T, db *sqlx.DB, schema, ddl string) {
	t.Helper()
	migrationV6_43_0SeedSchema(t, db, schema, ddl)
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
}

// TestMigrationV6_45_0DropsCompanyNameAndBackfillsSuperAdmin covers the upgrade
// path: the column disappears, role 1 gains the three pools permissions exactly
// once while every other role keeps its grants, and existing rows lose only the
// company name.
func TestMigrationV6_45_0DropsCompanyNameAndBackfillsSuperAdmin(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	// Registered before the schema cleanup below so it runs last (LIFO).
	t.Cleanup(func() { _ = db.Close() })
	schema := "migration_v6_45_0_upgrade"
	migrationV6_45_0SeedSchema(t, db, schema, migrationV6_45_0LegacySchema)

	if _, err := db.Exec(`INSERT INTO roles(id,permissions) VALUES(1,ARRAY['customers:get']),(2,ARRAY['customers:get'])`); err != nil {
		t.Fatalf("seed roles: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pool_contacts(customer_code,company_name,email,name) VALUES('C-1','Acme','a@example.invalid','Jane')`); err != nil {
		t.Fatalf("seed pool contact: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := V6_45_0(db, nil, nil, nil); err != nil {
			t.Fatalf("V6_45_0 run %d: %v", i+1, err)
		}
	}

	if columnExists(t, db, schema, "pool_contacts", "company_name") {
		t.Error("pool_contacts.company_name still exists after the migration")
	}

	var contact struct {
		CustomerCode string `db:"customer_code"`
		Email        string `db:"email"`
		Name         string `db:"name"`
	}
	if err := db.Get(&contact, `SELECT customer_code,email,name FROM pool_contacts WHERE id=1`); err != nil {
		t.Fatalf("read migrated contact: %v", err)
	}
	if contact.CustomerCode != "C-1" || contact.Email != "a@example.invalid" || contact.Name != "Jane" {
		t.Errorf("migrated contact = %+v, want the original code/email/name", contact)
	}

	var superAdmin, other string
	if err := db.Get(&superAdmin, `SELECT array_to_string(permissions, ',') FROM roles WHERE id=1`); err != nil {
		t.Fatalf("read role 1 permissions: %v", err)
	}
	if want := "customers:get,pools:get,pools:manage,pools:export"; superAdmin != want {
		t.Errorf("role 1 permissions = %q, want %q", superAdmin, want)
	}
	if err := db.Get(&other, `SELECT array_to_string(permissions, ',') FROM roles WHERE id=2`); err != nil {
		t.Fatalf("read role 2 permissions: %v", err)
	}
	if other != "customers:get" {
		t.Errorf("role 2 permissions = %q, want the untouched %q", other, "customers:get")
	}
	if strings.Count(superAdmin, "pools:get") != 1 || strings.Count(superAdmin, "pools:manage") != 1 || strings.Count(superAdmin, "pools:export") != 1 {
		t.Errorf("role 1 permissions %q contain a duplicate grant", superAdmin)
	}
}

// TestMigrationV6_45_0IsNoOpOnCurrentSchema pins that a run against the current
// schema.sql shape changes nothing.
func TestMigrationV6_45_0IsNoOpOnCurrentSchema(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	// Registered before the schema cleanup below so it runs last (LIFO).
	t.Cleanup(func() { _ = db.Close() })

	current := migrationV6_45_0LegacySchema + `
		ALTER TABLE pool_contacts DROP COLUMN company_name;
	`
	schema := "migration_v6_45_0_current"
	migrationV6_45_0SeedSchema(t, db, schema, current)

	if _, err := db.Exec(`INSERT INTO roles(id,permissions) VALUES(1,ARRAY['customers:get','pools:get','pools:manage','pools:export'])`); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := V6_45_0(db, nil, nil, nil); err != nil {
			t.Fatalf("V6_45_0 run %d: %v", i+1, err)
		}
	}

	if columnExists(t, db, schema, "pool_contacts", "company_name") {
		t.Error("company_name reappeared on the current schema")
	}
	var superAdmin string
	if err := db.Get(&superAdmin, `SELECT array_to_string(permissions, ',') FROM roles WHERE id=1`); err != nil {
		t.Fatalf("read role 1 permissions: %v", err)
	}
	if want := "customers:get,pools:get,pools:manage,pools:export"; superAdmin != want {
		t.Errorf("role 1 permissions = %q, want the unchanged %q", superAdmin, want)
	}
}
