package migrations

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// migrationV6_43_0TestDSN connects to the scratch database used by the
// migration tests. Every case below creates its own schema and points
// search_path at it only, so the shared public schema is never touched.
func migrationV6_43_0TestDSN(t *testing.T) *sqlx.DB {
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

// migrationV6_43_0LegacySchema is the pre-v6.43 shape: the reply mailbox lives
// on org_pool_allocations and organizations has no column of its own.
const migrationV6_43_0LegacySchema = `
	CREATE TABLE reply_mailboxes (
		id              SERIAL PRIMARY KEY,
		user_id         INTEGER,
		organization_id BIGINT,
		email           TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'pending',
		verified_at     TIMESTAMPTZ
	);

	CREATE TABLE organizations (
		id     BIGSERIAL PRIMARY KEY,
		name   TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active'
	);

	CREATE TABLE org_pool_allocations (
		id                 BIGSERIAL PRIMARY KEY,
		list_id            INTEGER,
		pool_id            INTEGER,
		organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		reply_mailbox_id   INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
		created_by_user_id INTEGER,
		created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE UNIQUE INDEX idx_org_pool_allocations_pool_org ON org_pool_allocations(pool_id, organization_id);
`

// migrationV6_43_0CurrentSchema mirrors the column sets of the current
// schema.sql: organizations already carries the unified reply mailbox and
// org_pool_allocations carries no mailbox column.
const migrationV6_43_0CurrentSchema = `
	CREATE TABLE reply_mailboxes (
		id              SERIAL PRIMARY KEY,
		user_id         INTEGER,
		organization_id BIGINT,
		email           TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'pending',
		verified_at     TIMESTAMPTZ
	);

	CREATE TABLE organizations (
		id               BIGSERIAL PRIMARY KEY,
		name             TEXT NOT NULL,
		status           TEXT NOT NULL DEFAULT 'active',
		reply_mailbox_id INTEGER NULL REFERENCES reply_mailboxes(id) ON DELETE SET NULL
	);

	CREATE TABLE org_pool_allocations (
		id                 BIGSERIAL PRIMARY KEY,
		list_id            INTEGER,
		pool_id            INTEGER,
		organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		created_by_user_id INTEGER,
		created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE UNIQUE INDEX idx_org_pool_allocations_pool_org ON org_pool_allocations(pool_id, organization_id);
`

// migrationV6_43_0Objects are the tables the migration touches, kept in one
// place so the catalog snapshot covers all of them.
var migrationV6_43_0Objects = []string{"organizations", "reply_mailboxes", "org_pool_allocations"}

func migrationV6_43_0SeedSchema(t *testing.T, db *sqlx.DB, schema, ddl string) {
	t.Helper()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
}

func migrationV6_43_0SeedOrganization(t *testing.T, db *sqlx.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.Get(&id, `INSERT INTO organizations(name) VALUES($1) RETURNING id`, name); err != nil {
		t.Fatal(err)
	}
	return id
}

func migrationV6_43_0SeedMailbox(t *testing.T, db *sqlx.DB, organizationID int64, email, status string, verified bool) int64 {
	t.Helper()
	var id int64
	if err := db.Get(&id, `INSERT INTO reply_mailboxes(organization_id,email,status,verified_at)
		VALUES($1,$2,$3,CASE WHEN $4 THEN NOW() ELSE NULL END) RETURNING id`, organizationID, email, status, verified); err != nil {
		t.Fatal(err)
	}
	return id
}

func migrationV6_43_0SeedAllocation(t *testing.T, db *sqlx.DB, organizationID int64, poolID int, mailboxID *int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,reply_mailbox_id) VALUES(NULL,$1,$2,$3)`, poolID, organizationID, mailboxID); err != nil {
		t.Fatal(err)
	}
}

// migrationV6_43_0OrgMailbox reads the migrated organizations.reply_mailbox_id,
// NULL when the organization has no unified reply mailbox.
func migrationV6_43_0OrgMailbox(t *testing.T, db *sqlx.DB, organizationID int64) *int64 {
	t.Helper()
	var mailbox *int64
	if err := db.Get(&mailbox, `SELECT reply_mailbox_id FROM organizations WHERE id=$1`, organizationID); err != nil {
		t.Fatal(err)
	}
	return mailbox
}

func migrationV6_43_0AssertMailbox(t *testing.T, db *sqlx.DB, organizationID int64, want *int64) {
	t.Helper()
	got := migrationV6_43_0OrgMailbox(t, db, organizationID)
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("organization %d reply_mailbox_id = %v, want %v", organizationID, got, want)
		}
		return
	}
	if *got != *want {
		t.Errorf("organization %d reply_mailbox_id = %d, want %d", organizationID, *got, *want)
	}
}

// TestOrganizationReplyMailboxBackfillFromAllocations proves backfill step 1:
// an unambiguous per-allocation mailbox moves to the organization column even
// when the mailbox is not yet verified, an ambiguous organization stays NULL,
// the allocation column is dropped, and a second run is a no-op.
func TestOrganizationReplyMailboxBackfillFromAllocations(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	defer db.Close()

	const schema = "migration_v6_43_0_allocation_backfill_test"
	migrationV6_43_0SeedSchema(t, db, schema, migrationV6_43_0LegacySchema)
	defer db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`)

	// One allocation mailbox plus a different usable mailbox: the allocation
	// wins because step 1 runs first and step 2 only fills NULLs.
	orgAllocationWins := migrationV6_43_0SeedOrganization(t, db, "org-allocation-wins")
	allocationMailbox := migrationV6_43_0SeedMailbox(t, db, orgAllocationWins, "allocation@example.invalid", "pending", false)
	migrationV6_43_0SeedMailbox(t, db, orgAllocationWins, "usable@example.invalid", "active", true)
	migrationV6_43_0SeedAllocation(t, db, orgAllocationWins, 1, &allocationMailbox)

	// Two allocations that share one mailbox are still unambiguous.
	orgSameMailbox := migrationV6_43_0SeedOrganization(t, db, "org-same-mailbox")
	sameMailbox := migrationV6_43_0SeedMailbox(t, db, orgSameMailbox, "same@example.invalid", "pending", false)
	migrationV6_43_0SeedAllocation(t, db, orgSameMailbox, 2, &sameMailbox)
	migrationV6_43_0SeedAllocation(t, db, orgSameMailbox, 3, &sameMailbox)

	// Two distinct allocation mailboxes are ambiguous; the mailboxes are left
	// unverified so step 2 cannot adopt one behind step 1's back.
	orgAmbiguous := migrationV6_43_0SeedOrganization(t, db, "org-ambiguous")
	ambiguousA := migrationV6_43_0SeedMailbox(t, db, orgAmbiguous, "ambiguous-a@example.invalid", "pending", false)
	ambiguousB := migrationV6_43_0SeedMailbox(t, db, orgAmbiguous, "ambiguous-b@example.invalid", "pending", false)
	migrationV6_43_0SeedAllocation(t, db, orgAmbiguous, 4, &ambiguousA)
	migrationV6_43_0SeedAllocation(t, db, orgAmbiguous, 5, &ambiguousB)

	// The adoption is independent of the mailbox state: even an unverified
	// allocation mailbox becomes the organization's setting.
	orgAllocationUnverified := migrationV6_43_0SeedOrganization(t, db, "org-allocation-unverified")
	unverifiedMailbox := migrationV6_43_0SeedMailbox(t, db, orgAllocationUnverified, "unverified@example.invalid", "pending", false)
	migrationV6_43_0SeedAllocation(t, db, orgAllocationUnverified, 6, &unverifiedMailbox)

	for i := 0; i < 2; i++ {
		if err := V6_43_0(db, nil, nil, nil); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}

	migrationV6_43_0AssertMailbox(t, db, orgAllocationWins, &allocationMailbox)
	migrationV6_43_0AssertMailbox(t, db, orgSameMailbox, &sameMailbox)
	migrationV6_43_0AssertMailbox(t, db, orgAmbiguous, nil)
	migrationV6_43_0AssertMailbox(t, db, orgAllocationUnverified, &unverifiedMailbox)

	if !columnExists(t, db, schema, "organizations", "reply_mailbox_id") {
		t.Errorf("organizations.reply_mailbox_id is missing after the migration")
	}
	if columnExists(t, db, schema, "org_pool_allocations", "reply_mailbox_id") {
		t.Errorf("org_pool_allocations.reply_mailbox_id still exists after the migration")
	}

	// The new organization column keeps the ON DELETE SET NULL contract.
	if _, err := db.Exec(`DELETE FROM reply_mailboxes WHERE id=$1`, allocationMailbox); err != nil {
		t.Fatal(err)
	}
	migrationV6_43_0AssertMailbox(t, db, orgAllocationWins, nil)
}

// TestOrganizationReplyMailboxBackfillFromSingleUsableMailbox proves backfill
// step 2: an organization with exactly one active+verified mailbox adopts it,
// while two candidates, a pending mailbox, a disabled mailbox and an
// active-but-unverified mailbox all stay NULL.
func TestOrganizationReplyMailboxBackfillFromSingleUsableMailbox(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	defer db.Close()

	const schema = "migration_v6_43_0_mailbox_backfill_test"
	migrationV6_43_0SeedSchema(t, db, schema, migrationV6_43_0LegacySchema)
	defer db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`)

	orgSingle := migrationV6_43_0SeedOrganization(t, db, "org-single-usable")
	singleMailbox := migrationV6_43_0SeedMailbox(t, db, orgSingle, "single@example.invalid", "active", true)

	// Two candidates must never be guessed between.
	orgTwo := migrationV6_43_0SeedOrganization(t, db, "org-two-usable")
	migrationV6_43_0SeedMailbox(t, db, orgTwo, "two-a@example.invalid", "active", true)
	migrationV6_43_0SeedMailbox(t, db, orgTwo, "two-b@example.invalid", "active", true)

	// Neither a pending mailbox nor an active-but-unverified one counts.
	orgUnverified := migrationV6_43_0SeedOrganization(t, db, "org-unverified")
	migrationV6_43_0SeedMailbox(t, db, orgUnverified, "pending@example.invalid", "pending", false)
	migrationV6_43_0SeedMailbox(t, db, orgUnverified, "active-unverified@example.invalid", "active", false)

	// A disabled mailbox keeps its last verification but is still not usable.
	orgDisabled := migrationV6_43_0SeedOrganization(t, db, "org-disabled")
	migrationV6_43_0SeedMailbox(t, db, orgDisabled, "disabled@example.invalid", "disabled", true)

	// An allocation without a mailbox does not block the single-mailbox
	// backfill.
	orgNullAllocation := migrationV6_43_0SeedOrganization(t, db, "org-null-allocation")
	nullAllocationMailbox := migrationV6_43_0SeedMailbox(t, db, orgNullAllocation, "null-allocation@example.invalid", "active", true)
	migrationV6_43_0SeedAllocation(t, db, orgNullAllocation, 10, nil)

	// An allocation mailbox adopted by step 1 is never overwritten by step 2.
	orgAllocationWins := migrationV6_43_0SeedOrganization(t, db, "org-allocation-wins")
	allocationMailbox := migrationV6_43_0SeedMailbox(t, db, orgAllocationWins, "allocation@example.invalid", "pending", false)
	migrationV6_43_0SeedMailbox(t, db, orgAllocationWins, "usable@example.invalid", "active", true)
	migrationV6_43_0SeedAllocation(t, db, orgAllocationWins, 11, &allocationMailbox)

	for i := 0; i < 2; i++ {
		if err := V6_43_0(db, nil, nil, nil); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}

	migrationV6_43_0AssertMailbox(t, db, orgSingle, &singleMailbox)
	migrationV6_43_0AssertMailbox(t, db, orgTwo, nil)
	migrationV6_43_0AssertMailbox(t, db, orgUnverified, nil)
	migrationV6_43_0AssertMailbox(t, db, orgDisabled, nil)
	migrationV6_43_0AssertMailbox(t, db, orgNullAllocation, &nullAllocationMailbox)
	migrationV6_43_0AssertMailbox(t, db, orgAllocationWins, &allocationMailbox)

	if columnExists(t, db, schema, "org_pool_allocations", "reply_mailbox_id") {
		t.Errorf("org_pool_allocations.reply_mailbox_id still exists after the migration")
	}
}

// TestOrganizationReplyMailboxMigrationIsNoopOnNewSchema proves the migration
// leaves a database that already uses the new organization column untouched: it
// neither adds nor drops anything, and it does not backfill even though the
// fixture has exactly one active+verified mailbox.
func TestOrganizationReplyMailboxMigrationIsNoopOnNewSchema(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	defer db.Close()

	const schema = "migration_v6_43_0_install_test"
	migrationV6_43_0SeedSchema(t, db, schema, migrationV6_43_0CurrentSchema)
	defer db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE;`)

	org := migrationV6_43_0SeedOrganization(t, db, "org-new-schema")
	migrationV6_43_0SeedMailbox(t, db, org, "unified@example.invalid", "active", true)

	before := migrationV6_43_0CatalogSnapshot(t, db, schema)

	for i := 0; i < 2; i++ {
		if err := V6_43_0(db, nil, nil, nil); err != nil {
			t.Fatalf("run %d on the current schema: %v", i+1, err)
		}
	}

	after := migrationV6_43_0CatalogSnapshot(t, db, schema)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the migration changed a database that already has the new shape:\nbefore=%v\nafter=%v", before, after)
	}

	// The backfill is about data left on the dropped allocation column, so a
	// fresh schema keeps the organization unconfigured.
	migrationV6_43_0AssertMailbox(t, db, org, nil)

	if !columnExists(t, db, schema, "organizations", "reply_mailbox_id") {
		t.Errorf("organizations.reply_mailbox_id disappeared on the current schema")
	}
	if columnExists(t, db, schema, "org_pool_allocations", "reply_mailbox_id") {
		t.Errorf("org_pool_allocations.reply_mailbox_id was added to the current schema")
	}
}

// migrationV6_43_0CatalogSnapshot renders the columns and indexes of every
// table the migration touches so two snapshots can be compared for equality.
func migrationV6_43_0CatalogSnapshot(t *testing.T, db *sqlx.DB, schema string) []string {
	t.Helper()
	quoted := make([]string, 0, len(migrationV6_43_0Objects))
	for _, table := range migrationV6_43_0Objects {
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
