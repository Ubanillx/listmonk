package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_20_0 introduces self-service, workspace-bound personal API keys. Existing
// API-user tokens are retained as service keys without expiry for compatibility.
func V6_20_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		ALTER TABLE integration_tokens ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'service';
		ALTER TABLE integration_tokens ADD COLUMN IF NOT EXISTS workspace_organization_id BIGINT NULL;
		ALTER TABLE integration_tokens ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}';
		ALTER TABLE integration_tokens ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP WITH TIME ZONE NULL;
		UPDATE integration_tokens SET kind = 'service' WHERE kind IS NULL OR kind = '';
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'integration_tokens_kind_check') THEN
				ALTER TABLE integration_tokens ADD CONSTRAINT integration_tokens_kind_check
					CHECK (kind IN ('service', 'personal'));
			END IF;
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'integration_tokens_personal_expiry_check') THEN
				ALTER TABLE integration_tokens ADD CONSTRAINT integration_tokens_personal_expiry_check
					CHECK (kind = 'service' OR expires_at IS NOT NULL);
			END IF;
		END $$;
		CREATE INDEX IF NOT EXISTS idx_integration_tokens_personal_workspace
			ON integration_tokens(user_id, workspace_organization_id, expires_at)
			WHERE kind = 'personal' AND revoked_at IS NULL;
	`)
	return err
}
