package migrations

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"os"
	"testing"
	"time"
)

func TestReplyDeliveryMigrationPreservesHistoryAndCursor(t *testing.T) {
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
	schema := fmt.Sprintf("reply_delivery_migration_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE reply_mailboxes(id INT PRIMARY KEY); INSERT INTO reply_mailboxes VALUES(1); CREATE TABLE campaign_recipients(id INT); INSERT INTO campaign_recipients VALUES(1); CREATE TABLE reply_ai_events(id INT); INSERT INTO reply_ai_events VALUES(1);`); err != nil {
		t.Fatal(err)
	}
	if err := V6_57_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var missing bool
	if err := db.Get(&missing, `SELECT reply_to_snapshot IS NULL FROM campaign_recipients WHERE id=1`); err != nil || !missing {
		t.Fatalf("invented historic route: %v %v", missing, err)
	}
	if _, err := db.Exec(`UPDATE campaign_recipients SET reply_to_snapshot='sent@example.com'; UPDATE reply_ai_events SET reply_references='<reference>'; INSERT INTO reply_mailbox_scan_cursors VALUES(1,'ai','hash',42,201);`); err != nil {
		t.Fatal(err)
	}
	if err := V6_57_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var route string
	var uid int
	if err := db.Get(&route, `SELECT reply_to_snapshot FROM campaign_recipients`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors`); err != nil {
		t.Fatal(err)
	}
	if route != "sent@example.com" || uid != 201 {
		t.Fatalf("migration overwrote history: %s %d", route, uid)
	}
}
