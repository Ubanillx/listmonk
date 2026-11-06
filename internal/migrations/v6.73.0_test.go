package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestReplyMailboxScanMigrationPreservesOptIns(t *testing.T) {
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
	schema := fmt.Sprintf("reply_scan_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE reply_mailboxes(id INT,ai_enabled BOOL,bounce_enabled BOOL);
		INSERT INTO reply_mailboxes VALUES(1,FALSE,FALSE),(2,TRUE,FALSE),(3,FALSE,TRUE),(4,TRUE,TRUE)`); err != nil {
		t.Fatal(err)
	}
	if err := V6_73_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var enabled []int
	if err := db.Select(&enabled, `SELECT id FROM reply_mailboxes WHERE scan_enabled ORDER BY id`); err != nil || fmt.Sprint(enabled) != "[2 3 4]" {
		t.Fatalf("backfill=%v err=%v", enabled, err)
	}
	if _, err := db.Exec(`UPDATE reply_mailboxes SET scan_enabled=FALSE WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if err := V6_73_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Select(&enabled, `SELECT id FROM reply_mailboxes WHERE scan_enabled ORDER BY id`); err != nil || fmt.Sprint(enabled) != "[2 3]" {
		t.Fatalf("rerun re-enabled paused mailbox: %v err=%v", enabled, err)
	}
}
