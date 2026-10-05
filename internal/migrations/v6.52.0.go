package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_52_0 places existing organization SMTP accounts and campaigns in a
// default pool without changing credentials, UUIDs or quota history.
func V6_52_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS organization_smtp_pools (
			id BIGSERIAL PRIMARY KEY,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name TEXT NOT NULL CHECK (LENGTH(TRIM(name)) BETWEEN 1 AND 100),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(id, organization_id)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_smtp_pool_name ON organization_smtp_pools(organization_id, LOWER(name));
		ALTER TABLE user_smtp_servers ADD COLUMN IF NOT EXISTS smtp_pool_id BIGINT;
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS smtp_pool_id BIGINT REFERENCES organization_smtp_pools(id) ON DELETE RESTRICT;
		INSERT INTO organization_smtp_pools(organization_id, name)
			SELECT o.id, '默认发件池' FROM organizations o WHERE
			(EXISTS(SELECT 1 FROM user_smtp_servers s WHERE s.organization_id=o.id AND s.smtp_pool_id IS NULL)
			OR EXISTS(SELECT 1 FROM campaigns c WHERE c.organization_id=o.id AND c.smtp_source='organization' AND c.smtp_pool_id IS NULL))
			AND NOT EXISTS(SELECT 1 FROM organization_smtp_pools p WHERE p.organization_id=o.id)
			ON CONFLICT DO NOTHING;
		UPDATE user_smtp_servers s SET smtp_pool_id=(SELECT MIN(p.id) FROM organization_smtp_pools p WHERE p.organization_id=s.organization_id)
			WHERE s.organization_id IS NOT NULL AND s.smtp_pool_id IS NULL;
		UPDATE campaigns c SET smtp_pool_id=(SELECT MIN(p.id) FROM organization_smtp_pools p WHERE p.organization_id=c.organization_id)
			WHERE c.organization_id IS NOT NULL AND c.smtp_source='organization' AND c.smtp_pool_id IS NULL;
		DO $$ BEGIN
			IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='smtp_pool_owner' AND conrelid='user_smtp_servers'::regclass) THEN
				ALTER TABLE user_smtp_servers ADD CONSTRAINT smtp_pool_owner
					FOREIGN KEY(smtp_pool_id, organization_id) REFERENCES organization_smtp_pools(id, organization_id) ON DELETE CASCADE;
				ALTER TABLE user_smtp_servers ADD CONSTRAINT smtp_pool_required
					CHECK ((organization_id IS NOT NULL) = (smtp_pool_id IS NOT NULL));
			END IF;
		END $$;
		DO $$ BEGIN
            IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='campaign_smtp_pool_owner' AND conrelid='campaigns'::regclass) THEN
                ALTER TABLE campaigns ADD CONSTRAINT campaign_smtp_pool_owner FOREIGN KEY(smtp_pool_id,organization_id)
                    REFERENCES organization_smtp_pools(id,organization_id) ON DELETE RESTRICT;
            END IF;
        END $$;
        DROP INDEX IF EXISTS idx_smtp_organization_name;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_pool_name ON user_smtp_servers(smtp_pool_id, LOWER(name)) WHERE name <> '';
		CREATE INDEX IF NOT EXISTS idx_smtp_pool_enabled ON user_smtp_servers(smtp_pool_id, enabled);
	`)
	return err
}
