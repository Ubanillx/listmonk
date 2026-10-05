package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestSinglePlatformSMTPMigration(t *testing.T) {
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
	if _, err := db.Exec(`
		CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value JSONB NOT NULL);
		INSERT INTO settings VALUES ('smtp', '[
			{"enabled":false,"is_primary":false,"host":"unused.example"},
			{"enabled":true,"is_primary":true,"host":"system.example","max_conns":23,"max_msg_retries":4,"idle_timeout":"30s","wait_timeout":"7s","tls_type":"STARTTLS","tls_skip_verify":true,"email_headers":[{"X-Test":"yes"}]},
			{"enabled":true,"is_primary":false,"host":"extra.example"}]');
	`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_49_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var host string
	if err := db.Get(&host, `SELECT value->0->>'host' FROM settings WHERE key='smtp'`); err != nil {
		t.Fatal(err)
	}
	if host != "system.example" {
		t.Fatalf("retained host = %q", host)
	}
	var count int
	if err := db.Get(&count, `SELECT JSONB_ARRAY_LENGTH(value) FROM settings WHERE key='smtp'`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("platform SMTP count = %d", count)
	}
	var maxConns int
	if err := db.Get(&maxConns, `SELECT (value->>'max_conns')::INT FROM settings WHERE key='smtp_delivery'`); err != nil {
		t.Fatal(err)
	}
	if maxConns != 23 {
		t.Fatalf("delivery connection limit = %d", maxConns)
	}
}
