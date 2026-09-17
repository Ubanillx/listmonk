package migrations

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// migrationV6_44_0LegacySchema is the pre-v6.44 shape: campaigns has neither
// the audience scope nor the rotation index, campaign_pool_recipients has no
// sender provenance, and neither the organization rotation nor the
// organization SMTP cursor exists.
const migrationV6_44_0LegacySchema = `
	CREATE TYPE campaign_recipient_status AS ENUM ('pending','queued','deferred','sent','cancelled');
	CREATE TABLE campaigns (
		id              SERIAL PRIMARY KEY,
		organization_id BIGINT,
		owner_user_id   INTEGER,
		to_send         INT NOT NULL DEFAULT 0,
		sent            INT NOT NULL DEFAULT 0
	);
	CREATE TABLE organizations (
		id     BIGSERIAL PRIMARY KEY,
		name   TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active'
	);
	CREATE TABLE customers (
		id              SERIAL PRIMARY KEY,
		organization_id BIGINT,
		owner_user_id   INTEGER,
		transfer_pending_at TIMESTAMPTZ
	);
	CREATE TABLE campaign_recipients (
		campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
		status      campaign_recipient_status NOT NULL DEFAULT 'pending',
		PRIMARY KEY (campaign_id, customer_id)
	);
	CREATE TABLE campaign_pool_recipients (
		campaign_id     INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		pool_contact_id BIGINT NOT NULL,
		pool_id         INTEGER NOT NULL,
		organization_id BIGINT,
		status          campaign_recipient_status NOT NULL DEFAULT 'pending',
		email_snapshot  TEXT NOT NULL,
		name_snapshot   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (campaign_id, pool_contact_id)
	);
`

// migrationV6_44_0SeedSchema installs one throwaway schema and registers its
// removal, so a test run leaves the shared scratch database clean.
func migrationV6_44_0SeedSchema(t *testing.T, db *sqlx.DB, schema, ddl string) {
	t.Helper()
	migrationV6_43_0SeedSchema(t, db, schema, ddl)
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
}

// TestMigrationV6_44_0AddsPlatformPoolDeliveryShape covers the upgrade path: an
// existing database gains the audience scope, the rotation index, the sender
// provenance columns, the two new tables and the recreated send-count view.
func TestMigrationV6_44_0AddsPlatformPoolDeliveryShape(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	// Registered before the schema cleanup below so it runs last (LIFO).
	t.Cleanup(func() { _ = db.Close() })
	schema := "migration_v6_44_0_upgrade"
	migrationV6_44_0SeedSchema(t, db, schema, migrationV6_44_0LegacySchema)

	if _, err := db.Exec(`INSERT INTO campaigns(id) VALUES(1)`); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}

	if err := V6_44_0(db, nil, nil, nil); err != nil {
		t.Fatalf("V6_44_0: %v", err)
	}

	for _, column := range []string{"pool_scope", "pool_next_org_index"} {
		if !columnExists(t, db, schema, "campaigns", column) {
			t.Errorf("campaigns.%s is missing after the migration", column)
		}
	}
	for _, column := range []string{"sender_smtp_uuid", "sender_user_id", "sender_from_snapshot", "sender_assigned_at"} {
		if !columnExists(t, db, schema, "campaign_pool_recipients", column) {
			t.Errorf("campaign_pool_recipients.%s is missing after the migration", column)
		}
	}
	for _, table := range []string{"campaign_pool_org_orders", "org_pool_smtp_cursors"} {
		var regclass *string
		if err := db.Get(&regclass, `SELECT to_regclass($1)::TEXT`, schema+"."+table); err != nil {
			t.Fatalf("look up %s: %v", table, err)
		}
		if regclass == nil {
			t.Errorf("%s is missing after the migration", table)
		}
	}

	// Existing rows adopt the legacy single-organization scope.
	var scope string
	if err := db.Get(&scope, `SELECT pool_scope FROM campaigns WHERE id=1`); err != nil {
		t.Fatalf("read migrated scope: %v", err)
	}
	if scope != "organization" {
		t.Errorf("migrated campaign pool_scope = %q, want %q", scope, "organization")
	}

	// The view is recreated with the platform-level pool predicate.
	var view string
	if err := db.Get(&view, `SELECT pg_get_viewdef($1::regclass, TRUE)`, schema+".campaign_send_counts"); err != nil {
		t.Fatalf("read recreated view: %v", err)
	}
	for _, term := range []string{"pool_scope", "all_organizations"} {
		if !strings.Contains(view, term) {
			t.Errorf("campaign_send_counts definition %q does not contain %q", view, term)
		}
	}
}

// TestMigrationV6_44_0IsIdempotentAndNoOpOnCurrentSchema pins that a second run
// and a run against the current schema.sql shape change nothing.
func TestMigrationV6_44_0IsIdempotentAndNoOpOnCurrentSchema(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	// Registered before the schema cleanup below so it runs last (LIFO).
	t.Cleanup(func() { _ = db.Close() })

	// Current shape: everything the migration installs already exists.
	current := migrationV6_44_0LegacySchema + `
		ALTER TABLE campaigns ADD COLUMN pool_scope TEXT NOT NULL DEFAULT 'organization';
		ALTER TABLE campaigns ADD CONSTRAINT campaigns_pool_scope_check CHECK (pool_scope IN ('organization','all_organizations'));
		ALTER TABLE campaigns ADD COLUMN pool_next_org_index INT NOT NULL DEFAULT 0;
		ALTER TABLE campaign_pool_recipients ADD COLUMN sender_smtp_uuid UUID;
		ALTER TABLE campaign_pool_recipients ADD COLUMN sender_user_id INTEGER;
		ALTER TABLE campaign_pool_recipients ADD COLUMN sender_from_snapshot TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_pool_recipients ADD COLUMN sender_assigned_at TIMESTAMPTZ;
		CREATE INDEX idx_campaign_pool_recipients_sender ON campaign_pool_recipients(sender_smtp_uuid) WHERE sender_smtp_uuid IS NOT NULL;
		CREATE TABLE campaign_pool_org_orders (
			campaign_id     INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			dispatch_order  INTEGER NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (campaign_id, organization_id)
		);
		CREATE INDEX idx_campaign_pool_org_orders_campaign ON campaign_pool_org_orders(campaign_id, dispatch_order);
		CREATE TABLE org_pool_smtp_cursors (
			organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
			next_smtp_uuid  UUID,
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	schema := "migration_v6_44_0_current"
	migrationV6_44_0SeedSchema(t, db, schema, current)

	before := migrationV6_43_0CatalogSnapshot(t, db, schema)
	for i := 0; i < 2; i++ {
		if err := V6_44_0(db, nil, nil, nil); err != nil {
			t.Fatalf("V6_44_0 run %d: %v", i+1, err)
		}
		after := migrationV6_43_0CatalogSnapshot(t, db, schema)
		if len(before) != len(after) {
			t.Fatalf("catalog changed on run %d: %d -> %d entries", i+1, len(before), len(after))
		}
		for j := range before {
			if before[j] != after[j] {
				t.Errorf("catalog entry changed on run %d:\n before %s\n after  %s", i+1, before[j], after[j])
			}
		}
	}
}

var _ = sqlx.DB{}
