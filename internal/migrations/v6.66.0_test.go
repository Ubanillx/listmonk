package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignUnsubscribeMigrationRetainsDistinctFacts(t *testing.T) {
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
	schema := fmt.Sprintf("campaign_unsubscribe_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE campaigns(id INT PRIMARY KEY);INSERT INTO campaigns VALUES(1);
		CREATE TABLE customers(id INT PRIMARY KEY,status TEXT);INSERT INTO customers VALUES(1,'unsubscribed');`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := V6_66_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscriptions`); err != nil || count != 0 {
		t.Fatalf("migration invented historical clicks: %d %v", count, err)
	}
	for range 2 {
		if _, err := db.Exec(`INSERT INTO campaign_unsubscriptions(campaign_id,recipient_type,recipient_id) VALUES(1,'private',1),(1,'pool',1) ON CONFLICT DO NOTHING`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM customers WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscriptions`); err != nil || count != 2 {
		t.Fatalf("distinct identities or deleted-customer history lost: %d %v", count, err)
	}
	if _, err := db.Exec(`DELETE FROM campaigns WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&count, `SELECT COUNT(*) FROM campaign_unsubscriptions`); err != nil || count != 0 {
		t.Fatalf("deleted campaign left orphan history: %d %v", count, err)
	}
}
