package migrations

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestPersonalSMTPRateDefaultMigrationPreservesSavedRates(t *testing.T) {
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
	schema := fmt.Sprintf("personal_rate_default_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE campaigns(id INT PRIMARY KEY, smtp_source TEXT,
		smtp_rate_limit INT NOT NULL DEFAULT 20);
		INSERT INTO campaigns VALUES (1,'personal',20),(2,'personal',45),(3,'organization',100);`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := V6_64_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var rates []int
	if err := db.Select(&rates, `SELECT smtp_rate_limit FROM campaigns ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rates, []int{20, 45, 100}) {
		t.Fatalf("migration changed saved rates: %v", rates)
	}
	var rate int
	if err := db.Get(&rate, `INSERT INTO campaigns(id,smtp_source) VALUES(4,'personal') RETURNING smtp_rate_limit`); err != nil {
		t.Fatal(err)
	}
	if rate != 5 {
		t.Fatalf("new personal campaign default = %d, want 5", rate)
	}
}
