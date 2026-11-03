package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_70_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(ProviderAutomationSQL)
	return err
}

const ProviderAutomationSQL = `
CREATE TABLE IF NOT EXISTS provider_blocklist_rules (
    scope TEXT PRIMARY KEY CHECK (scope IN ('private', 'pool')),
    version BIGINT NOT NULL DEFAULT 1,
    config JSONB NOT NULL,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ
);
INSERT INTO provider_blocklist_rules(scope, config)
SELECT scope, '{"enabled":false,"types":[1,3,6,8,9],"schedule":"daily","daily_time":"02:00","interval_hours":24,"lookback_days":7,"excluded_emails":[]}'::JSONB
FROM (VALUES ('private'), ('pool')) scopes(scope)
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS provider_blocklist_runs (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL REFERENCES provider_blocklist_rules(scope),
    version BIGINT NOT NULL,
    config JSONB NOT NULL,
    trigger TEXT NOT NULL,
    status TEXT NOT NULL,
    date_start TEXT NOT NULL,
    date_end TEXT NOT NULL,
    counts JSONB NOT NULL DEFAULT '{}',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_provider_blocklist_runs_scope ON provider_blocklist_runs(scope,id DESC);
CREATE TABLE IF NOT EXISTS provider_blocklist_items (
    run_id BIGINT NOT NULL REFERENCES provider_blocklist_runs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    target_id BIGINT NOT NULL,
    email TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL,
    outcome TEXT NOT NULL,
    PRIMARY KEY(run_id, position)
);
`
