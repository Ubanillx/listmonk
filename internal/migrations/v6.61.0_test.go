package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestAllocationLifecycleMigration(t *testing.T) {
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
	schema := fmt.Sprintf("allocation_lifecycle_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + ";SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE org_pool_allocations(id BIGINT PRIMARY KEY);INSERT INTO org_pool_allocations VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := V6_61_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	if err := db.Get(&state, `SELECT state FROM org_pool_allocations WHERE id=1`); err != nil || state != "active" {
		t.Fatalf("upgrade default %q %v", state, err)
	}
	if _, err := db.Exec(`UPDATE org_pool_allocations SET state='detached',version=5`); err != nil {
		t.Fatal(err)
	}
	if err := V6_61_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM org_pool_allocations WHERE id=1 AND state='detached' AND version=5`); err != nil || n != 1 {
		t.Fatal("upgrade changed existing lifecycle", err)
	}
	if _, err := db.Exec(`UPDATE org_pool_allocations SET state='invented'`); err == nil {
		t.Fatal("missing state constraint")
	}
}
