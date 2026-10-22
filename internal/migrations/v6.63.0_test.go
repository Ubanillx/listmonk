package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignRetrySelectionMigrationIsIdempotent(t *testing.T) {
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
	schema := fmt.Sprintf("campaign_retry_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + ";SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE campaigns(id INT PRIMARY KEY);
		CREATE TABLE campaign_recipients(campaign_id INT,customer_id INT);
		CREATE TABLE campaign_pool_recipients(campaign_id INT,pool_contact_id BIGINT);
		CREATE TABLE campaign_send_errors(id BIGSERIAL,campaign_id INT,recipient_type TEXT,recipient_id BIGINT,created_at TIMESTAMPTZ);
		INSERT INTO campaigns VALUES(7);INSERT INTO campaign_recipients VALUES(7,11);INSERT INTO campaign_pool_recipients VALUES(7,41);`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_63_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var ready bool
	if err := db.Get(&ready, `SELECT NOT c.retry_mode AND NOT cr.retry_selected AND NOT cpr.retry_selected
		FROM campaigns c JOIN campaign_recipients cr ON cr.campaign_id=c.id
		JOIN campaign_pool_recipients cpr ON cpr.campaign_id=c.id WHERE c.id=7`); err != nil || !ready {
		t.Fatalf("migration changed existing rows: %v %v", ready, err)
	}
}
