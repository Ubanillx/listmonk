package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestPoolSecondaryMetadataRepairIsIdempotent(t *testing.T) {
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
	db.SetMaxIdleConns(1)

	if _, err = db.Exec(`
		CREATE TEMP TABLE customer_lists (
			id integer PRIMARY KEY,
			type text NOT NULL,
			organization_id bigint,
			pool_parent_id integer,
			visibility text NOT NULL,
			status text NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT NOW()
		);
		CREATE TEMP TABLE pool_segments (
			list_id integer PRIMARY KEY,
			pool_id integer,
			organization_id bigint NOT NULL
		);
		INSERT INTO customer_lists(id, type, organization_id, pool_parent_id, visibility, status) VALUES
			(1, 'pool_segment', NULL, NULL, 'private', 'active'),
			(2, 'pool_segment', 9, NULL, 'private', 'active'),
			(3, 'pool_segment', 9, 22, 'organization', 'active'),
			(4, 'pool_segment', 9, NULL, 'private', 'active'),
			(5, 'pool', 8, NULL, 'private', 'active');
		INSERT INTO pool_segments(list_id, pool_id, organization_id) VALUES
			(1, 22, 7),
			(2, NULL, 9),
			(3, 22, 9);
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err = V6_40_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var repaired struct {
		OrganizationID int64  `db:"organization_id"`
		PoolParentID   int    `db:"pool_parent_id"`
		Visibility     string `db:"visibility"`
		Status         string `db:"status"`
	}
	if err = db.Get(&repaired, `SELECT organization_id,pool_parent_id,visibility,status FROM customer_lists WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if repaired.OrganizationID != 7 || repaired.PoolParentID != 22 || repaired.Visibility != "organization" || repaired.Status != "active" {
		t.Fatalf("valid secondary list was not repaired: %+v", repaired)
	}

	var archived int
	if err = db.Get(&archived, `SELECT count(*) FROM customer_lists WHERE id IN (2,4) AND status='archived'`); err != nil {
		t.Fatal(err)
	}
	if archived != 2 {
		t.Fatalf("expected unbound secondary lists to be archived, got %d", archived)
	}

	var unchanged struct {
		OrganizationID int64  `db:"organization_id"`
		PoolParentID   int    `db:"pool_parent_id"`
		Visibility     string `db:"visibility"`
		Status         string `db:"status"`
	}
	if err = db.Get(&unchanged, `SELECT organization_id,pool_parent_id,visibility,status FROM customer_lists WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if unchanged.OrganizationID != 9 || unchanged.PoolParentID != 22 || unchanged.Visibility != "organization" || unchanged.Status != "active" {
		t.Fatalf("already valid secondary list changed unexpectedly: %+v", unchanged)
	}

	var pool struct {
		OrganizationID int64  `db:"organization_id"`
		Visibility     string `db:"visibility"`
		Status         string `db:"status"`
	}
	if err = db.Get(&pool, `SELECT organization_id,visibility,status FROM customer_lists WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	if pool.OrganizationID != 0 || pool.Visibility != "global" || pool.Status != "active" {
		t.Fatalf("first-level pool was not normalized: %+v", pool)
	}
}
