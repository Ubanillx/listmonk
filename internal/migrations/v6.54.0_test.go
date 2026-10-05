package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestPoolBlocklistMigrationPreservesExistingStatuses(t *testing.T) {
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
	const schema = "pool_blocklist_migration_test"
	if _, err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE; CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE pool_contacts(id SERIAL PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')));
		INSERT INTO pool_contacts(status) VALUES ('active'),('archived');`); err != nil {
		t.Fatal(err)
	}
	if err := V6_54_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pool_contacts(status) VALUES('blocklisted')`); err != nil {
		t.Fatal(err)
	}
	if err := V6_54_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var statuses []string
	if err := db.Select(&statuses, `SELECT status FROM pool_contacts ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 || statuses[0] != "active" || statuses[1] != "archived" || statuses[2] != "blocklisted" {
		t.Fatalf("statuses changed by upgrade: %v", statuses)
	}
	if _, err := db.Exec(`INSERT INTO pool_contacts(status) VALUES('invalid')`); err == nil {
		t.Fatal("invalid status accepted after migration")
	}
}
