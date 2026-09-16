package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestPoolAllocationDepartmentBackfillIsIdempotent(t *testing.T) {
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
		CREATE TEMP TABLE organizations (id bigint PRIMARY KEY, name text NOT NULL, status text NOT NULL);
		CREATE TEMP TABLE pool_contacts (id bigint PRIMARY KEY, allocation_department text NOT NULL);
		CREATE TEMP TABLE pool_members (pool_id integer NOT NULL, contact_id bigint NOT NULL, PRIMARY KEY(pool_id,contact_id));
		CREATE TEMP TABLE pool_segments (id bigint PRIMARY KEY, pool_id integer, organization_id bigint NOT NULL);
		CREATE TEMP TABLE pool_segment_members (
			segment_id bigint NOT NULL,
			contact_id bigint NOT NULL,
			status text NOT NULL,
			PRIMARY KEY(segment_id,contact_id)
		);
		INSERT INTO organizations VALUES (1,'org 测试','active'), (2,'停用组织','archived');
		INSERT INTO pool_contacts VALUES (10,'org 测试'), (11,'停用组织'), (12,'其它组织');
		INSERT INTO pool_members VALUES (20,10), (20,11), (20,12);
		INSERT INTO pool_segments VALUES (30,20,1), (31,20,2);
		INSERT INTO pool_segment_members VALUES (30,12,'removed');
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err = V6_41_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var active int
	if err = db.Get(&active, `SELECT count(*) FROM pool_segment_members WHERE segment_id=30 AND status='active'`); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("expected one department-matched member, got %d", active)
	}

	var removed int
	if err = db.Get(&removed, `SELECT count(*) FROM pool_segment_members WHERE segment_id=30 AND contact_id=12 AND status='removed'`); err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("existing removal was not preserved, got %d", removed)
	}

	var archived int
	if err = db.Get(&archived, `SELECT count(*) FROM pool_segment_members WHERE segment_id=31`); err != nil {
		t.Fatal(err)
	}
	if archived != 0 {
		t.Fatalf("archived organization received %d members", archived)
	}
}
