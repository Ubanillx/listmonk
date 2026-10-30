package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignRecipientRetryMigrationUsesHistoryAndIsIdempotent(t *testing.T) {
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
	schema := fmt.Sprintf("recipient_retry_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TYPE campaign_recipient_status AS ENUM('pending','queued','deferred','sent','cancelled');
		CREATE TABLE settings(key TEXT PRIMARY KEY,value JSONB,updated_at TIMESTAMPTZ DEFAULT NOW());
		INSERT INTO settings VALUES('smtp_delivery','{"max_recipient_retries":1,"max_msg_retries":7}',NOW());
		CREATE TABLE campaigns(id INT PRIMARY KEY,type TEXT,messenger TEXT);
		INSERT INTO campaigns VALUES(1,'regular','email'),(2,'regular','email'),(3,'optin','email');
		CREATE TABLE campaign_recipients(campaign_id INT,customer_id BIGINT,status campaign_recipient_status,updated_at TIMESTAMPTZ DEFAULT NOW());
		CREATE TABLE campaign_pool_recipients(campaign_id INT,pool_contact_id BIGINT,status campaign_recipient_status,updated_at TIMESTAMPTZ DEFAULT NOW());
		INSERT INTO campaign_recipients VALUES(1,10,'queued',NOW()),(1,11,'sent',NOW()),(2,10,'pending',NOW()),(3,10,'pending',NOW());
		INSERT INTO campaign_pool_recipients VALUES(1,10,'deferred',NOW());
		CREATE TABLE campaign_send_errors(campaign_id INT,recipient_type TEXT,recipient_id BIGINT);
		INSERT INTO campaign_send_errors VALUES(1,'private',10),(1,'private',10),(1,'private',11),(1,'private',11),
			(1,'pool',10),(1,'pool',10),(2,'private',10),(3,'private',10),(3,'private',10);`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := V6_65_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err := db.Get(&valid, `SELECT
		(SELECT status='failed' FROM campaign_recipients WHERE campaign_id=1 AND customer_id=10)
		AND (SELECT status='sent' FROM campaign_recipients WHERE campaign_id=1 AND customer_id=11)
		AND (SELECT status='pending' FROM campaign_recipients WHERE campaign_id=2)
		AND (SELECT status='pending' FROM campaign_recipients WHERE campaign_id=3)
		AND (SELECT status='failed' FROM campaign_pool_recipients)
		AND (SELECT value->>'max_recipient_retries'='1' AND value->>'max_msg_retries'='7' FROM settings)`); err != nil || !valid {
		t.Fatalf("upgrade reset settings, mixed identities or revived recipients: valid=%v err=%v", valid, err)
	}
	if _, err := db.Exec(`UPDATE settings SET value=value-'max_recipient_retries'`); err != nil {
		t.Fatal(err)
	}
	if err := V6_65_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var limit int
	if err := db.Get(&limit, `SELECT (value->>'max_recipient_retries')::INT FROM settings`); err != nil || limit != 3 {
		t.Fatalf("absent setting default=%d err=%v", limit, err)
	}
}
