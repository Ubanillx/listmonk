package migrations

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	_ "github.com/lib/pq"
)

func TestSMTPDeliveryMigrationPreservesEffectiveConfiguration(t *testing.T) {
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
	const schema = "smtp_delivery_migration_test"
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	if _, err := db.Exec(`
		CREATE TABLE campaigns(id INT PRIMARY KEY, smtp_source TEXT, messenger TEXT);
		INSERT INTO campaigns VALUES(1,'personal','email'),(2,'organization','email'),(3,'personal','webhook');
		CREATE TABLE user_smtp_servers(id INT PRIMARY KEY, uuid TEXT, password TEXT, tls_type TEXT, tls_skip_verify BOOLEAN);
		INSERT INTO user_smtp_servers VALUES(1,'sender-uuid','saved-password','none',FALSE);
		CREATE TABLE settings(key TEXT PRIMARY KEY, value JSONB, updated_at TIMESTAMPTZ DEFAULT NOW());
		INSERT INTO settings(key,value) VALUES
			('smtp','[{"uuid":"system-uuid","password":"system-password","tls_type":"none"}]'),
			('smtp_delivery','{"max_conns":10,"max_msg_retries":2,"idle_timeout":"15s","wait_timeout":"5s",
			"tls_type":"STARTTLS","tls_skip_verify":true,"send_delay_min":"2s","send_delay_max":"5s","email_headers":[]}');
	`); err != nil {
		t.Fatal(err)
	}
	if err := V6_53_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var rates []int
	if err := db.Select(&rates, `SELECT smtp_rate_limit FROM campaigns ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(rates) != 3 || rates[0] != 20 || rates[1] != 100 || rates[2] != 0 {
		t.Fatalf("incorrect campaign defaults: %v", rates)
	}
	var valid bool
	if err := db.Get(&valid, `SELECT EXISTS(SELECT 1 FROM user_smtp_servers WHERE uuid='sender-uuid'
		AND password='saved-password' AND tls_type='STARTTLS' AND tls_skip_verify)`); err != nil || !valid {
		t.Fatalf("sender effective TLS/credentials changed: %v %v", valid, err)
	}
	if err := db.Get(&valid, `SELECT value->0->>'uuid'='system-uuid' AND value->0->>'password'='system-password'
		AND value->0->>'tls_type'='STARTTLS' AND (value->0->>'tls_skip_verify')::BOOLEAN FROM settings WHERE key='smtp'`); err != nil || !valid {
		t.Fatalf("system effective TLS/credentials changed: %v %v", valid, err)
	}
	var raw []byte
	if err := db.Get(&raw, `SELECT value FROM settings WHERE key='smtp_delivery'`); err != nil {
		t.Fatal(err)
	}
	var delivery models.SMTPDeliverySettings
	if err := json.Unmarshal(raw, &delivery); err != nil || delivery.SendDelayMin != 2000 || delivery.SendDelayMax != 5000 {
		t.Fatalf("delay duration changed: %+v %v", delivery, err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["tls_type"]; ok {
		t.Fatal("TLS remained a shared delivery option")
	}
	if _, err := db.Exec(`UPDATE campaigns SET smtp_rate_limit=37 WHERE id=2;
		UPDATE user_smtp_servers SET tls_type='TLS',tls_skip_verify=FALSE;
		UPDATE settings SET value=JSONB_SET(value,'{0,tls_type}','"TLS"') WHERE key='smtp'`); err != nil {
		t.Fatal(err)
	}
	if err := V6_53_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&valid, `SELECT (SELECT smtp_rate_limit=37 FROM campaigns WHERE id=2)
		AND (SELECT tls_type='TLS' AND NOT tls_skip_verify FROM user_smtp_servers WHERE id=1)
		AND (SELECT value->0->>'tls_type'='TLS' FROM settings WHERE key='smtp')`); err != nil || !valid {
		t.Fatalf("rerun overwrote per-server TLS or campaign rate: %v %v", valid, err)
	}
}
