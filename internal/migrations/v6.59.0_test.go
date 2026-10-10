package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignSendErrorMigrationPreservesProgressAndRecordedErrors(t *testing.T) {
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
	schema := fmt.Sprintf("campaign_errors_migration_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE campaigns(id INT, sent INT, status TEXT); INSERT INTO campaigns VALUES(1,12,'deferred')`); err != nil {
		t.Fatal(err)
	}
	if err := V6_59_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Get(&count, `SELECT send_errors FROM campaigns`); err != nil || count != 0 {
		t.Fatalf("historical default=%d: %v", count, err)
	}
	if _, err := db.Exec(`UPDATE campaigns SET send_errors=4`); err != nil {
		t.Fatal(err)
	}
	if err := V6_59_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := db.Get(&preserved, `SELECT send_errors=4 AND sent=12 AND status='deferred' FROM campaigns`); err != nil || !preserved {
		t.Fatalf("migration changed progress: %v", err)
	}
	if _, err := db.Exec(`UPDATE campaigns SET send_errors=-1`); err == nil {
		t.Fatal("negative error count accepted")
	}
}
