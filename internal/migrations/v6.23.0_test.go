package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// Run against a disposable PostgreSQL database with MIGRATION_TEST_DSN set.
func TestV623LegacyStatsUpgrade(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DSN is not set")
	}
	for _, view := range []string{"mat_list_subscriber_stats", "mat_list_customer_stats", "mat_customer_list_customer_stats", ""} {
		t.Run("view_"+view, func(t *testing.T) {
			db, err := sqlx.Connect("postgres", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
			if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
				t.Fatal(err)
			}
			defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
			if _, err := db.Exec("SET search_path TO " + schema); err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`
CREATE TYPE role_type AS ENUM ('list');
CREATE TABLE subscribers (id integer PRIMARY KEY);
CREATE TABLE lists (id integer PRIMARY KEY);
CREATE TABLE subscriber_lists (subscriber_id integer REFERENCES subscribers(id), list_id integer REFERENCES lists(id), status text);
CREATE TABLE roles (list_id integer, type role_type, permissions text[]);
CREATE TABLE integration_tokens (scopes text[]);
CREATE TABLE settings (key text);
INSERT INTO subscribers VALUES (1), (2);
INSERT INTO lists VALUES (10);
INSERT INTO subscriber_lists VALUES (1, 10, 'confirmed'), (2, 10, 'confirmed');
`)
			if err != nil {
				t.Fatal(err)
			}
			if view != "" {
				_, err = db.Exec("CREATE MATERIALIZED VIEW " + view + ` AS SELECT now() AS updated_at, list_id, status, count(*) AS subscriber_count FROM subscriber_lists GROUP BY list_id, status`)
				if err != nil {
					t.Fatal(err)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := V6_23_0(db, nil, nil, nil); err != nil {
					t.Fatalf("upgrade attempt %d: %v", attempt, err)
				}
				var count int
				if err := db.Get(&count, "SELECT customer_count FROM mat_customer_list_customer_stats WHERE customer_list_id=10 AND status='confirmed'"); err != nil {
					t.Fatal(err)
				}
				if count != 2 {
					t.Fatalf("customer count = %d, want 2", count)
				}
				if err := db.Get(&count, "SELECT count(*) FROM customer_list_memberships WHERE customer_list_id=10"); err != nil {
					t.Fatal(err)
				}
				if count != 2 {
					t.Fatalf("memberships = %d, want 2", count)
				}
				if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mat_customer_list_customer_stats"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
