package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignHistoryPreservationMigration(t *testing.T) {
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
	schema := fmt.Sprintf("campaign_history_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`
		CREATE TYPE campaign_status AS ENUM ('draft','running','scheduled','paused','deferred','cancelled','finished');
		CREATE TYPE campaign_recipient_status AS ENUM ('pending','queued','deferred','sent','cancelled','failed');
		CREATE TABLE campaigns (
			id INT PRIMARY KEY,
			status campaign_status NOT NULL DEFAULT 'draft',
			to_send INT NOT NULL DEFAULT 0,
			sent INT NOT NULL DEFAULT 0,
			pool_scope TEXT NOT NULL DEFAULT 'organization',
			organization_id BIGINT,
			owner_user_id INT
		);
		CREATE TABLE customers (
			id INT PRIMARY KEY,
			organization_id BIGINT,
			owner_user_id INT,
			transfer_pending_at TIMESTAMPTZ
		);
		CREATE TABLE customer_lists (
			id INT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'active',
			transfer_pending_at TIMESTAMPTZ
		);
		CREATE TABLE pool_contacts (
			id BIGINT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'active'
		);
		CREATE TABLE campaign_recipients (
			campaign_id INT NOT NULL,
			customer_id INT NOT NULL,
			status campaign_recipient_status NOT NULL DEFAULT 'pending',
			CONSTRAINT campaign_recipients_subscriber_id_fkey
				FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
		);
		CREATE TABLE campaign_pool_recipients (
			campaign_id INT NOT NULL,
			pool_contact_id BIGINT NOT NULL,
			pool_id INT NOT NULL,
			organization_id BIGINT,
			status campaign_recipient_status NOT NULL DEFAULT 'pending',
			CONSTRAINT campaign_pool_recipients_pool_contact_id_fkey
				FOREIGN KEY (pool_contact_id) REFERENCES pool_contacts(id) ON DELETE CASCADE,
			CONSTRAINT campaign_pool_recipients_pool_id_fkey
				FOREIGN KEY (pool_id) REFERENCES customer_lists(id) ON DELETE CASCADE
		);
		INSERT INTO campaigns(id,status,to_send,sent,organization_id,owner_user_id)
			VALUES(1,'finished',5,2,101,3);
		INSERT INTO customers(id,organization_id,owner_user_id) VALUES(1,101,3);
		INSERT INTO customer_lists(id) VALUES(1);
		INSERT INTO pool_contacts(id) VALUES(1);
		INSERT INTO campaign_recipients(campaign_id,customer_id,status) VALUES(1,1,'sent');
		INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,organization_id,status)
			VALUES(1,1,1,101,'pending');
	`); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := V6_72_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var planned int
	if err := db.Get(&planned, `SELECT planned_total FROM campaigns WHERE id=1`); err != nil || planned != 5 {
		t.Fatalf("planned_total=%d err=%v, want 5", planned, err)
	}
	var fks int
	if err := db.Get(&fks, `SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid IN ('campaign_recipients'::regclass,'campaign_pool_recipients'::regclass)
			AND contype='f'
			AND conname IN ('campaign_recipients_subscriber_id_fkey',
				'campaign_pool_recipients_pool_contact_id_fkey',
				'campaign_pool_recipients_pool_id_fkey')`); err != nil || fks != 0 {
		t.Fatalf("cascading foreign keys=%d err=%v, want 0", fks, err)
	}

	if _, err := db.Exec(`DELETE FROM customers WHERE id=1`); err != nil {
		t.Fatalf("deleting a customer still cascaded: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM pool_contacts WHERE id=1`); err != nil {
		t.Fatalf("deleting a pool contact still cascaded: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM customer_lists WHERE id=1`); err != nil {
		t.Fatalf("deleting a pool list still cascaded: %v", err)
	}
	var privateRows, poolRows int
	if err := db.Get(&privateRows, `SELECT COUNT(*) FROM campaign_recipients WHERE campaign_id=1`); err != nil || privateRows != 1 {
		t.Fatalf("private snapshot rows=%d err=%v, want 1", privateRows, err)
	}
	if err := db.Get(&poolRows, `SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=1`); err != nil || poolRows != 1 {
		t.Fatalf("pool snapshot rows=%d err=%v, want 1", poolRows, err)
	}

	var counts struct {
		Total       int  `db:"total_count"`
		Sent        int  `db:"sent_count"`
		Unsent      int  `db:"unsent_count"`
		HasSnapshot bool `db:"has_snapshot"`
	}
	if err := db.Get(&counts, `SELECT total_count,sent_count,unsent_count,has_snapshot
		FROM campaign_send_counts WHERE campaign_id=1`); err != nil {
		t.Fatal(err)
	}
	if counts.Total != 2 || counts.Sent != 1 || counts.Unsent != 1 || !counts.HasSnapshot {
		t.Fatalf("send counts=(%d,%d,%d,%t), want (2,1,1,true)",
			counts.Total, counts.Sent, counts.Unsent, counts.HasSnapshot)
	}
}
