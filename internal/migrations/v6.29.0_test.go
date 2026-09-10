package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestNameFallbackUpgradePreservesData(t *testing.T) {
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
	// Temporary tables shadow real application tables; no existing rows are modified.
	if _, err := db.Exec(`CREATE TEMP TABLE templates (id int, body text); CREATE TEMP TABLE campaigns (id int, body text);
		INSERT INTO templates VALUES (1, 'original template'); INSERT INTO campaigns VALUES (1, 'original campaign');`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_29_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		var body, fallback string
		if err := db.QueryRow(`SELECT body, name_fallback FROM templates WHERE id = 1`).Scan(&body, &fallback); err != nil {
			t.Fatal(err)
		}
		if body != "original template" || fallback != "{}" {
			t.Fatalf("changed existing data: %s %s", body, fallback)
		}
	}
}
