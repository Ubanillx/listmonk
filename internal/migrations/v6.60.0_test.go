package migrations

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"os"
	"testing"
	"time"
)

func TestCampaignSendErrorDetailsMigrationPreservesHistoricalCounts(t *testing.T) {
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
	schema := fmt.Sprintf("send_error_details_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + ";SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE campaigns(id INT PRIMARY KEY,send_errors INT);INSERT INTO campaigns VALUES(1,8)`); err != nil {
		t.Fatal(err)
	}
	if err := V6_60_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM campaign_send_errors`); err != nil || n != 0 {
		t.Fatalf("migration invented historical details: %d %v", n, err)
	}
	if _, err := db.Exec(`INSERT INTO campaign_send_errors(campaign_id,recipient_type,recipient_id,stage,category,error) VALUES(1,'private',7,'send','smtp_rejected','550 rejected')`); err != nil {
		t.Fatal(err)
	}
	if err := V6_60_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&n, `SELECT send_errors FROM campaigns`); err != nil || n != 8 {
		t.Fatalf("count changed: %d %v", n, err)
	}
	if err := db.Get(&n, `SELECT count(*) FROM campaign_send_errors`); err != nil || n != 1 {
		t.Fatalf("history changed: %d %v", n, err)
	}
	if _, err := db.Exec(`DELETE FROM campaigns WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&n, `SELECT count(*) FROM campaign_send_errors`); err != nil || n != 0 {
		t.Fatalf("campaign delete left errors: %d %v", n, err)
	}
}
