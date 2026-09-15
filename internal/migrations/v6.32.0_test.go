package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestV6320AuditEventsMigrationIsIdempotentAndPreservesHistory(t *testing.T) {
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

	schema := fmt.Sprintf("audit_migration_test_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec("SET search_path TO public")
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
	}()
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE organizations (id BIGINT PRIMARY KEY);
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE integration_tokens (id INTEGER PRIMARY KEY);
CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id BIGINT REFERENCES organizations(id),
    actor_type TEXT NOT NULL,
    actor_user_id INTEGER REFERENCES users(id),
    actor_token_id INTEGER REFERENCES integration_tokens(id),
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT 'success',
    reason_code TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    ip INET,
    user_agent TEXT NOT NULL DEFAULT ''
);
INSERT INTO audit_events (organization_id, actor_type, action, object_type)
VALUES (NULL, 'system', 'legacy.event', 'legacy');
`); err != nil {
		t.Fatal(err)
	}

	if err := V6_32_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := V6_32_0(db, nil, nil, nil); err != nil {
		t.Fatalf("second migration attempt failed: %v", err)
	}

	var organizationID int64
	if err := db.Get(&organizationID, "SELECT organization_id FROM audit_events WHERE action='legacy.event'"); err != nil {
		t.Fatal(err)
	}
	if organizationID != 0 {
		t.Fatalf("legacy organization_id = %d, want 0", organizationID)
	}

	var nullable string
	if err := db.Get(&nullable, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='audit_events' AND column_name='organization_id'`, schema); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" {
		t.Fatalf("organization_id nullable = %q, want NO", nullable)
	}
	var defaultValue string
	if err := db.Get(&defaultValue, `SELECT column_default FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='audit_events' AND column_name='organization_id'`, schema); err != nil {
		t.Fatal(err)
	}
	if defaultValue != "0" {
		t.Fatalf("organization_id default = %q, want 0", defaultValue)
	}

	var indexCount int
	if err := db.Get(&indexCount, `SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname=$1 AND tablename='audit_events' AND indexname LIKE 'idx_audit_events_%'`, schema); err != nil {
		t.Fatal(err)
	}
	if indexCount != 5 {
		t.Fatalf("business index count = %d, want 5", indexCount)
	}
}
