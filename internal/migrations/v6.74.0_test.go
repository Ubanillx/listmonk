package migrations

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestSendErrorAlertDefaultsMatchSchema(t *testing.T) {
	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"('app.send_error_alert_enabled', 'true')",
		`('app.send_error_alert_window', '"5m"')`,
		"('app.send_error_alert_threshold', '50')",
		`('app.send_error_alert_cooldown', '"30m"')`,
	} {
		if !strings.Contains(string(schema), line) {
			t.Fatalf("schema.sql is missing %s", line)
		}
		if !strings.Contains(SendErrorAlertSQL, line) {
			t.Fatalf("migration is missing %s", line)
		}
	}
}

func TestSendErrorAlertMigrationAddsDefaultsAndTable(t *testing.T) {
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
	schema := fmt.Sprintf("send_error_alert_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`
		CREATE TABLE campaigns(id INT PRIMARY KEY);
		CREATE TABLE settings(key TEXT PRIMARY KEY, value JSONB);
		INSERT INTO settings(key,value) VALUES('app.max_send_errors','1000');
	`); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := V6_74_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	got := map[string]string{}
	rows, err := db.Queryx(`SELECT key, value::TEXT FROM settings WHERE key LIKE 'app.send_error_alert%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		got[key] = value
	}
	want := map[string]string{
		"app.send_error_alert_enabled":   "true",
		"app.send_error_alert_window":    `"5m"`,
		"app.send_error_alert_threshold": "50",
		"app.send_error_alert_cooldown":  `"30m"`,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("alert setting %s = %q, want %q (all: %v)", key, got[key], value, got)
		}
	}
	var legacy string
	if err := db.Get(&legacy, `SELECT value::TEXT FROM settings WHERE key='app.max_send_errors'`); err != nil || legacy != "1000" {
		t.Fatalf("legacy setting = %q, err = %v", legacy, err)
	}

	var campaignID int
	if err := db.Get(&campaignID, `INSERT INTO campaigns(id) VALUES(7) RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO campaign_send_error_alerts(campaign_id) VALUES($1)`, campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM campaigns WHERE id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	var alerts int
	if err := db.Get(&alerts, `SELECT COUNT(*) FROM campaign_send_error_alerts`); err != nil || alerts != 0 {
		t.Fatalf("alert rows after campaign delete = %d, err = %v", alerts, err)
	}
}
