package migrations

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// migrationV6_42_0TestDSN connects to the scratch database used by the
// migration tests. Both cases below create their own schema and point
// search_path at it only, so the shared public schema is never touched.
func migrationV6_42_0TestDSN(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

// migrationV6_42_0Objects are the tables the rename touches. Both cases below
// keep this list in one place so the catalog snapshot covers all of them.
var migrationV6_42_0Objects = []string{
	"org_pool_allocations",
	"org_pool_allocation_members",
	"org_pool_allocation_exclusions",
	"campaign_customer_lists",
	"campaign_recipients",
	"campaign_pool_recipients",
	"bounces",
	"reply_ai_events",
	"audit_events",
}

const migrationV6_42_0LegacySchema = `
	CREATE TYPE customer_list_type AS ENUM ('public', 'private', 'temporary', 'pool', 'pool_segment');
	CREATE TABLE customer_lists (id bigint PRIMARY KEY, type customer_list_type NOT NULL);
	INSERT INTO customer_lists VALUES (1, 'pool_segment'), (2, 'pool');

	CREATE TABLE pool_segments (id bigint PRIMARY KEY, list_id bigint NOT NULL, pool_id integer, organization_id bigint NOT NULL);
	CREATE INDEX idx_pool_segments_pool_org ON pool_segments(pool_id, organization_id);
	CREATE INDEX idx_pool_segments_org ON pool_segments(organization_id);
	INSERT INTO pool_segments VALUES (30, 1, 7, 3);

	CREATE TABLE pool_segment_members (
		segment_id bigint NOT NULL,
		contact_id bigint NOT NULL,
		status text NOT NULL,
		PRIMARY KEY (segment_id, contact_id)
	);
	CREATE INDEX idx_pool_segment_members_status ON pool_segment_members(segment_id, status);
	INSERT INTO pool_segment_members VALUES (30, 11, 'active');

	CREATE TABLE pool_segment_exclusions (
		pool_id integer NOT NULL,
		organization_id bigint NOT NULL,
		contact_id bigint NOT NULL,
		segment_id bigint,
		reason text NOT NULL DEFAULT 'manual',
		source text NOT NULL DEFAULT 'segment',
		PRIMARY KEY (pool_id, organization_id, contact_id)
	);
	INSERT INTO pool_segment_exclusions VALUES (7, 3, 11, 30, 'manual', 'segment');

	CREATE TABLE campaign_customer_lists (campaign_id integer NOT NULL, pool_id integer, pool_segment_id bigint);
	INSERT INTO campaign_customer_lists VALUES (5, 7, 30);

	CREATE TABLE campaign_recipients (id bigint PRIMARY KEY, source_segment_id bigint);
	CREATE TABLE campaign_pool_recipients (campaign_id integer NOT NULL, pool_contact_id bigint NOT NULL, segment_id bigint);
	CREATE TABLE bounces (id bigint PRIMARY KEY, source_segment_id bigint);
	CREATE TABLE reply_ai_events (id bigint PRIMARY KEY, source_segment_id bigint);

	CREATE TABLE audit_events (id bigint PRIMARY KEY, action text NOT NULL, object_type text NOT NULL);
	INSERT INTO audit_events VALUES
		(1, 'pool_segment.created', 'pool_segment'),
		(2, 'pool_segment.reply_mailbox_changed', 'pool_segment'),
		(3, 'organization.updated', 'organization');
`

// TestOrgPoolAllocationRenameIsIdempotent proves the rename reaches the enum
// label, the tables, the referencing columns, the named index and the values
// that older releases persisted, that the rows survive it, and that a second
// run is a no-op.
func TestOrgPoolAllocationRenameIsIdempotent(t *testing.T) {
	db := migrationV6_42_0TestDSN(t)
	defer db.Close()

	const schema = "migration_v6_42_0_upgrade_test"
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`)
	if _, err := db.Exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrationV6_42_0LegacySchema); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := V6_42_0(db, nil, nil, nil); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}

	var labels []string
	if err := db.Select(&labels, `SELECT enumlabel FROM pg_enum WHERE enumtypid=to_regtype('customer_list_type') ORDER BY enumsortorder`); err != nil {
		t.Fatal(err)
	}
	want := "public,private,temporary,pool,org_pool_allocation"
	if got := strings.Join(labels, ","); got != want {
		t.Fatalf("enum labels = %s, want %s", got, want)
	}

	// Existing rows follow the renamed label without a data update.
	var listType string
	if err := db.Get(&listType, `SELECT type::text FROM customer_lists WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if listType != "org_pool_allocation" {
		t.Fatalf("customer_lists.type = %q, want org_pool_allocation", listType)
	}

	for _, table := range []string{"org_pool_allocations", "org_pool_allocation_members", "org_pool_allocation_exclusions"} {
		if !relationExists(t, db, table) {
			t.Errorf("table %s was not created by the rename", table)
		}
	}
	for _, table := range []string{"pool_segments", "pool_segment_members", "pool_segment_exclusions"} {
		if relationExists(t, db, table) {
			t.Errorf("table %s still exists after the rename", table)
		}
	}

	// Renamed columns must be present under the new name and gone under the old
	// one: a rename that silently added a column instead would pass a
	// "new name exists" check while leaving the store inconsistent.
	columns := map[string][2]string{
		"org_pool_allocation_members":    {"segment_id", "allocation_id"},
		"org_pool_allocation_exclusions": {"segment_id", "allocation_id"},
		"campaign_customer_lists":        {"pool_segment_id", "org_pool_allocation_id"},
		"campaign_recipients":            {"source_segment_id", "source_allocation_id"},
		"campaign_pool_recipients":       {"segment_id", "allocation_id"},
		"bounces":                        {"source_segment_id", "source_allocation_id"},
		"reply_ai_events":                {"source_segment_id", "source_allocation_id"},
	}
	for table, names := range columns {
		old, current := names[0], names[1]
		if !columnExists(t, db, schema, table, current) {
			t.Errorf("%s.%s is missing after the rename", table, current)
		}
		if columnExists(t, db, schema, table, old) {
			t.Errorf("%s.%s still exists after the rename", table, old)
		}
	}

	// The rename must not lose the rows it touched.
	var members int
	if err := db.Get(&members, `SELECT count(*) FROM org_pool_allocation_members WHERE allocation_id=30 AND contact_id=11`); err != nil {
		t.Fatal(err)
	}
	if members != 1 {
		t.Errorf("membership row = %d, want 1 through the renamed column", members)
	}
	var exclusionAllocation int64
	if err := db.Get(&exclusionAllocation, `SELECT allocation_id FROM org_pool_allocation_exclusions WHERE contact_id=11`); err != nil {
		t.Fatal(err)
	}
	if exclusionAllocation != 30 {
		t.Errorf("exclusion allocation_id = %d, want 30", exclusionAllocation)
	}
	var campaignAllocation int64
	if err := db.Get(&campaignAllocation, `SELECT org_pool_allocation_id FROM campaign_customer_lists WHERE campaign_id=5`); err != nil {
		t.Fatal(err)
	}
	if campaignAllocation != 30 {
		t.Errorf("campaign relation allocation = %d, want 30", campaignAllocation)
	}
	var listID int64
	if err := db.Get(&listID, `SELECT list_id FROM org_pool_allocations WHERE id=30`); err != nil {
		t.Fatal(err)
	}
	if listID != 1 {
		t.Errorf("allocation list_id = %d, want 1", listID)
	}

	indexes := map[string]string{
		"idx_pool_segments_pool_org":      "idx_org_pool_allocations_pool_org",
		"idx_pool_segments_org":           "idx_org_pool_allocations_org",
		"idx_pool_segment_members_status": "idx_org_pool_allocation_members_status",
	}
	for old, current := range indexes {
		if !relationExists(t, db, current) {
			t.Errorf("index %s is missing after the rename", current)
		}
		if relationExists(t, db, old) {
			t.Errorf("index %s still exists after the rename", old)
		}
	}

	var exclusionSource, exclusionDefault string
	if err := db.Get(&exclusionSource, `SELECT source FROM org_pool_allocation_exclusions`); err != nil {
		t.Fatal(err)
	}
	if exclusionSource != "allocation" {
		t.Errorf("exclusion source = %q, want allocation", exclusionSource)
	}
	if err := db.Get(&exclusionDefault, `SELECT column_default FROM information_schema.columns WHERE table_schema=$1 AND table_name='org_pool_allocation_exclusions' AND column_name='source'`, schema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exclusionDefault, "'allocation'") {
		t.Errorf("source default = %q, want 'allocation' as in schema.sql", exclusionDefault)
	}

	var action, objectType string
	if err := db.QueryRowx(`SELECT action, object_type FROM audit_events WHERE id=1`).Scan(&action, &objectType); err != nil {
		t.Fatal(err)
	}
	if action != "org_pool_allocation.created" || objectType != "org_pool_allocation" {
		t.Errorf("audit row = (%q, %q), want (org_pool_allocation.created, org_pool_allocation)", action, objectType)
	}
	var untouched string
	if err := db.Get(&untouched, `SELECT action FROM audit_events WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if untouched != "organization.updated" {
		t.Errorf("unrelated audit row was rewritten to %q", untouched)
	}
}

// TestOrgPoolAllocationRenameIsNoopOnNewSchema proves the migration leaves a
// database that already uses the new names untouched: the fixture below uses
// the column sets of the current schema.sql, and the catalog snapshot compared
// before and after the run fails if the migration adds, drops or renames
// anything on that path.
func TestOrgPoolAllocationRenameIsNoopOnNewSchema(t *testing.T) {
	db := migrationV6_42_0TestDSN(t)
	defer db.Close()

	const schema = "migration_v6_42_0_install_test"
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`)
	if _, err := db.Exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TYPE customer_list_type AS ENUM ('public', 'private', 'temporary', 'pool', 'org_pool_allocation');
		CREATE TABLE customer_lists (id bigint PRIMARY KEY, type customer_list_type NOT NULL);
		INSERT INTO customer_lists VALUES (1, 'org_pool_allocation');

		CREATE TABLE org_pool_allocations (
			id bigint PRIMARY KEY,
			list_id bigint NOT NULL,
			pool_id integer,
			organization_id bigint NOT NULL,
			reply_mailbox_id integer,
			created_by_user_id integer,
			created_at timestamptz NOT NULL DEFAULT NOW()
		);
		CREATE UNIQUE INDEX idx_org_pool_allocations_pool_org ON org_pool_allocations(pool_id, organization_id);

		CREATE TABLE org_pool_allocation_members (
			allocation_id bigint NOT NULL,
			contact_id bigint NOT NULL,
			status text NOT NULL DEFAULT 'active',
			removed_reason text NOT NULL DEFAULT '',
			removed_at timestamptz,
			removed_by_user_id integer,
			created_at timestamptz NOT NULL DEFAULT NOW(),
			updated_at timestamptz NOT NULL DEFAULT NOW(),
			PRIMARY KEY (allocation_id, contact_id)
		);

		CREATE TABLE org_pool_allocation_exclusions (
			pool_id integer NOT NULL,
			organization_id bigint NOT NULL,
			contact_id bigint NOT NULL,
			allocation_id bigint,
			reason text NOT NULL DEFAULT 'manual',
			source text NOT NULL DEFAULT 'allocation',
			removed_by_user_id integer,
			removed_at timestamptz NOT NULL DEFAULT NOW(),
			restored_at timestamptz,
			PRIMARY KEY (pool_id, organization_id, contact_id)
		);

		CREATE TABLE campaign_customer_lists (
			campaign_id integer NOT NULL,
			pool_id integer,
			org_pool_allocation_id bigint,
			source_organization_id bigint,
			resolved_reply_mailbox_id integer
		);
		CREATE INDEX idx_campaign_customer_lists_pool ON campaign_customer_lists(pool_id, org_pool_allocation_id);

		CREATE TABLE campaign_recipients (id bigint PRIMARY KEY, source_allocation_id bigint, source_organization_id bigint);
		CREATE TABLE campaign_pool_recipients (
			campaign_id integer NOT NULL,
			pool_contact_id bigint NOT NULL,
			pool_id integer,
			allocation_id bigint,
			organization_id bigint,
			reply_mailbox_id integer
		);
		CREATE TABLE bounces (id bigint PRIMARY KEY, source_allocation_id bigint, source_organization_id bigint);
		CREATE TABLE reply_ai_events (id bigint PRIMARY KEY, source_allocation_id bigint, source_organization_id bigint);

		CREATE TABLE audit_events (id bigint PRIMARY KEY, action text NOT NULL, object_type text NOT NULL);
		INSERT INTO audit_events VALUES (1, 'org_pool_allocation.created', 'org_pool_allocation');
	`); err != nil {
		t.Fatal(err)
	}

	before := migrationV6_42_0CatalogSnapshot(t, db, schema)

	if err := V6_42_0(db, nil, nil, nil); err != nil {
		t.Fatalf("migration is not a no-op on the current schema: %v", err)
	}

	after := migrationV6_42_0CatalogSnapshot(t, db, schema)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the migration changed a database that already uses the new names:\nbefore=%v\nafter=%v", before, after)
	}

	var labels []string
	if err := db.Select(&labels, `SELECT enumlabel FROM pg_enum WHERE enumtypid=to_regtype('customer_list_type') ORDER BY enumsortorder`); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(labels, ","); got != "public,private,temporary,pool,org_pool_allocation" {
		t.Errorf("enum labels = %s, want the new label only", got)
	}

	var listType string
	if err := db.Get(&listType, `SELECT type::text FROM customer_lists WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if listType != "org_pool_allocation" {
		t.Errorf("customer_lists.type = %q, want org_pool_allocation", listType)
	}
}

// migrationV6_42_0CatalogSnapshot renders the columns and indexes of every
// table the migration touches so two snapshots can be compared for equality.
func migrationV6_42_0CatalogSnapshot(t *testing.T, db *sqlx.DB, schema string) []string {
	t.Helper()
	quoted := make([]string, 0, len(migrationV6_42_0Objects))
	for _, table := range migrationV6_42_0Objects {
		quoted = append(quoted, "'"+table+"'")
	}
	inList := strings.Join(quoted, ",")

	var columns, indexes []string
	if err := db.Select(&columns, `SELECT table_name || '.' || column_name FROM information_schema.columns WHERE table_schema=$1 AND table_name IN (`+inList+`) ORDER BY 1`, schema); err != nil {
		t.Fatal(err)
	}
	if err := db.Select(&indexes, `SELECT indexname FROM pg_indexes WHERE schemaname=$1 AND tablename IN (`+inList+`) ORDER BY 1`, schema); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(columns)+len(indexes))
	for _, c := range columns {
		out = append(out, "column:"+c)
	}
	for _, i := range indexes {
		out = append(out, "index:"+i)
	}
	return out
}

func relationExists(t *testing.T, db *sqlx.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.Get(&exists, `SELECT to_regclass($1) IS NOT NULL`, name); err != nil {
		t.Fatal(err)
	}
	return exists
}

func columnExists(t *testing.T, db *sqlx.DB, schema, table, column string) bool {
	t.Helper()
	var exists bool
	if err := db.Get(&exists, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name=$3)`, schema, table, column); err != nil {
		t.Fatal(err)
	}
	return exists
}
