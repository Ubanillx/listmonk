package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_50_0 adds independent organization SMTP ownership without moving any
// account credentials or changing existing campaigns' sender selection.
func V6_50_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE user_smtp_servers ALTER COLUMN user_id DROP NOT NULL;
		ALTER TABLE user_smtp_servers ADD COLUMN IF NOT EXISTS organization_id BIGINT
			REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='smtp_owner_exclusive' AND conrelid='user_smtp_servers'::regclass) THEN
				ALTER TABLE user_smtp_servers ADD CONSTRAINT smtp_owner_exclusive
					CHECK ((user_id IS NULL) <> (organization_id IS NULL));
			END IF;
		END $$;
		CREATE INDEX IF NOT EXISTS idx_smtp_organization_enabled ON user_smtp_servers(organization_id, enabled);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_organization_name ON user_smtp_servers(organization_id, LOWER(name)) WHERE name <> '';
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS smtp_source TEXT NOT NULL DEFAULT 'personal'
			CHECK (smtp_source IN ('personal', 'organization'));
	`)
	return err
}
