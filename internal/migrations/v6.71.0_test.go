package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestReplyBounceMigrationIsAdditiveAndIdempotent(t *testing.T) {
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
	schema := fmt.Sprintf("reply_bounce_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`
		CREATE TABLE campaigns(id INT PRIMARY KEY);
		CREATE TABLE customers(id INT PRIMARY KEY);
		CREATE TABLE reply_mailboxes(id SERIAL PRIMARY KEY);
		INSERT INTO reply_mailboxes DEFAULT VALUES;
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := V6_71_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var enabled bool
	if err := db.Get(&enabled, `SELECT bounce_enabled FROM reply_mailboxes WHERE id=1`); err != nil || enabled {
		t.Fatalf("default bounce_enabled=%v err=%v", enabled, err)
	}
	if _, err := db.Exec(`INSERT INTO reply_bounce_events(reply_mailbox_id,message_key,recipient,bounce_type,outcome,reason_code)
		VALUES(1,'key-1','missing@example.com','hard','blocklisted','blocklisted')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reply_bounce_events(reply_mailbox_id,message_key,recipient,bounce_type,outcome,reason_code)
		VALUES(1,'key-1','missing@example.com','hard','blocklisted','blocklisted')
		ON CONFLICT (reply_mailbox_id,message_key,recipient) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM reply_bounce_events`); err != nil || count != 1 {
		t.Fatalf("ledger rows=%d err=%v", count, err)
	}
}
