package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestPoolReplyMigrationPreservesHistoryAndIsIdempotent(t *testing.T) {
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
	const schema = "pool_reply_migration_test"
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE pool_contacts(id INT); CREATE TABLE campaigns(id INT);
		CREATE TABLE reply_mailboxes(id INT,email TEXT);
		CREATE TABLE campaign_pool_recipients(id INT,reply_mailbox_id INT,status TEXT);
		INSERT INTO pool_contacts VALUES(1); INSERT INTO campaigns VALUES(1);
		INSERT INTO reply_mailboxes VALUES(1,'history@example.com');
		INSERT INTO campaign_pool_recipients VALUES(1,1,'sent');`); err != nil {
		t.Fatal(err)
	}
	if err := V6_55_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.Get(&value, `SELECT reply_to_snapshot FROM campaign_pool_recipients`); err != nil || value != "history@example.com" {
		t.Fatalf("backfilled history=%q, %v", value, err)
	}
	if err := db.Get(&value, `SELECT pool_reply_priority FROM campaigns`); err != nil || value != "contact_first" {
		t.Fatalf("default priority=%q, %v", value, err)
	}
	if _, err := db.Exec(`UPDATE campaigns SET pool_reply_priority='organization_first';
		UPDATE pool_contacts SET reply_to='customer@example.com';
		UPDATE campaign_pool_recipients SET reply_to_snapshot='frozen@example.com',reply_to_source='contact';`); err != nil {
		t.Fatal(err)
	}
	if err := V6_55_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&value, `SELECT reply_to_snapshot FROM campaign_pool_recipients`); err != nil || value != "frozen@example.com" {
		t.Fatalf("history=%q, %v", value, err)
	}
	if err := db.Get(&value, `SELECT pool_reply_priority FROM campaigns`); err != nil || value != "organization_first" {
		t.Fatalf("priority=%q, %v", value, err)
	}
	if _, err := db.Exec(`UPDATE campaigns SET pool_reply_priority='invalid'`); err == nil {
		t.Fatal("invalid priority accepted")
	}
}
