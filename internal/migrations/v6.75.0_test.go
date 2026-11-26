package migrations

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestCampaignAttachmentLangSchemaMatchesMigration(t *testing.T) {
	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"attachment_lang TEXT NOT NULL DEFAULT 'en'",
		"CHECK (attachment_lang IN ('en','zh-CN','zh-TW','es','fr','de','pt','it','ru','ja','ko'))",
	} {
		if !strings.Contains(string(schema), line) {
			t.Fatalf("schema.sql is missing %q", line)
		}
		if !strings.Contains(CampaignAttachmentLangSQL, strings.TrimPrefix(line, "CHECK ")) {
			t.Fatalf("migration is missing %q", line)
		}
	}
	if strings.Contains(string(schema), "'auto'") {
		t.Fatal("schema.sql still accepts the removed automatic mode")
	}
}

func TestCampaignAttachmentLangMigrationIsIdempotent(t *testing.T) {
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
	for _, tc := range []struct {
		name, setup string
		existing    []string
	}{
		{"no language column", `CREATE TABLE campaigns(id INT PRIMARY KEY); INSERT INTO campaigns VALUES(1);`, []string{"en"}},
		{"legacy automatic and explicit languages", `CREATE TABLE campaigns(id INT PRIMARY KEY,
			attachment_lang TEXT NOT NULL DEFAULT 'auto',
			CONSTRAINT campaigns_attachment_lang_check CHECK (attachment_lang IN ('auto','en','de')));
			INSERT INTO campaigns VALUES(1,'auto'),(2,'de');`, []string{"en", "de"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := fmt.Sprintf("campaign_attachment_lang_%d", time.Now().UnixNano())
			if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
				t.Fatal(err)
			}
			defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
			if _, err := db.Exec(tc.setup); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := V6_75_0(db, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				var got []string
				if err := db.Select(&got, `SELECT attachment_lang FROM campaigns ORDER BY id`); err != nil || !slices.Equal(got, tc.existing) {
					t.Fatalf("existing campaign languages = %v, %v; want %v", got, err, tc.existing)
				}
			}
			var lang string
			if err := db.Get(&lang, `INSERT INTO campaigns(id) VALUES(3) RETURNING attachment_lang`); err != nil || lang != "en" {
				t.Fatalf("new campaign language = %q, %v; want en", lang, err)
			}
			for _, unsupported := range []string{"xx", "auto"} {
				if _, err := db.Exec(`UPDATE campaigns SET attachment_lang=$1 WHERE id=3`, unsupported); err == nil {
					t.Fatalf("unsupported language %q passed the check constraint", unsupported)
				}
			}
		})
	}
}
