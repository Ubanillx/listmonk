package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// AuditEventsSchema is the durable business-operation audit log. It excludes
// high-frequency delivery/open/click facts, which remain in their dedicated
// tables, and stores only privacy-safe metadata.
const AuditEventsSchema = `
CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 0 is the personal workspace. Organization IDs are retained even when
    -- an organization is later purged so history cannot fall into personal
    -- workspace queries.
    organization_id BIGINT NOT NULL DEFAULT 0,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user','api_key','system','webhook','customer','anonymous')),
    actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id INTEGER REFERENCES integration_tokens(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT 'success' CHECK (result IN ('success','failed','denied')),
    reason_code TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    ip INET,
    user_agent TEXT NOT NULL DEFAULT ''
);
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_organization_id_fkey;
UPDATE audit_events SET organization_id = 0 WHERE organization_id IS NULL;
ALTER TABLE audit_events ALTER COLUMN organization_id SET DEFAULT 0;
ALTER TABLE audit_events ALTER COLUMN organization_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_events_org_time
    ON audit_events(organization_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action_time
    ON audit_events(action, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_object
    ON audit_events(object_type, object_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor
    ON audit_events(actor_user_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_request
    ON audit_events(request_id) WHERE request_id <> '';
`

// V6_32_0 installs the durable business-operation audit log.
func V6_32_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(AuditEventsSchema)
	return err
}
