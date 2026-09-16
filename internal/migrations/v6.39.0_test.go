package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestObsoleteExportCleanupIsIdempotent(t *testing.T) {
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
		CREATE TEMP TABLE data_export_jobs (id integer);
		CREATE TEMP TABLE data_export_chunks (id integer);
		INSERT INTO roles(id, type, permissions) VALUES
			(1, 'user', ARRAY['customers:get', 'exports:create']),
			(2, 'user', ARRAY['customers:get']),
			(3, 'user', ARRAY['customers:get_all', 'exports:download']),
			(4, 'user', ARRAY['customers:manage']),
			(5, 'customer_list', ARRAY['customers:get']);
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err = V6_39_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	checks := map[int]map[string]bool{
		1: {"customers:export": false, "exports:create": false},
		2: {"customers:export": true, "exports:create": false},
		3: {"customers:export": true, "exports:download": false},
		4: {"customers:export": false},
		5: {"customers:export": false},
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

	var jobs, chunks int
	if err = db.Get(&jobs, `SELECT count(*) FROM pg_class WHERE relname='data_export_jobs' AND relnamespace=pg_my_temp_schema()`); err != nil {
		t.Fatal(err)
	}
	if err = db.Get(&chunks, `SELECT count(*) FROM pg_class WHERE relname='data_export_chunks' AND relnamespace=pg_my_temp_schema()`); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || chunks != 0 {
		t.Fatalf("obsolete temporary export tables remain: jobs=%d chunks=%d", jobs, chunks)
	}
}
