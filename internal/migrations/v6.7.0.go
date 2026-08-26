package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_7_0 adds organization tenancy and ownership metadata to resources. It is
// deliberately additive so existing installations retain every resource as a
// personal resource owned by the earliest platform super administrator.
func V6_7_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS organizations (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
			created_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			archived_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_name_lower ON organizations (LOWER(name));
		CREATE INDEX IF NOT EXISTS idx_organizations_status ON organizations(status);

		CREATE TABLE IF NOT EXISTS organization_members (
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'manager')),
			joined_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			removed_at TIMESTAMP WITH TIME ZONE,
			removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			PRIMARY KEY (organization_id, user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_organization_members_user_active
			ON organization_members(user_id, organization_id) WHERE removed_at IS NULL;
		CREATE INDEX IF NOT EXISTS idx_organization_members_org_active
			ON organization_members(organization_id, role) WHERE removed_at IS NULL;

		CREATE TABLE IF NOT EXISTS organization_join_requests (
			id BIGSERIAL PRIMARY KEY,
			requested_name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
			requested_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			reviewed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			reviewed_at TIMESTAMP WITH TIME ZONE,
			review_note TEXT NOT NULL DEFAULT '',
			organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_join_requests_pending_name
			ON organization_join_requests (LOWER(requested_name)) WHERE status = 'pending';
		CREATE INDEX IF NOT EXISTS idx_organization_join_requests_status ON organization_join_requests(status, created_at DESC);

		CREATE TABLE IF NOT EXISTS organization_invites (
			id BIGSERIAL PRIMARY KEY,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
			name TEXT NOT NULL DEFAULT '',
			code_hash TEXT NOT NULL UNIQUE,
			created_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			expires_at TIMESTAMP WITH TIME ZONE,
			revoked_at TIMESTAMP WITH TIME ZONE,
			max_uses INTEGER,
			use_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			CHECK (max_uses IS NULL OR max_uses > 0)
		);
		CREATE INDEX IF NOT EXISTS idx_organization_invites_org_active
			ON organization_invites(organization_id, created_at DESC) WHERE revoked_at IS NULL;

		ALTER TABLE lists ADD COLUMN IF NOT EXISTS organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT;
		ALTER TABLE lists ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE lists ADD COLUMN IF NOT EXISTS original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE lists ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global'));
		ALTER TABLE lists ADD COLUMN IF NOT EXISTS transfer_pending_at TIMESTAMP WITH TIME ZONE;

		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT;
		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global'));
		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS transfer_pending_at TIMESTAMP WITH TIME ZONE;

		ALTER TABLE templates ADD COLUMN IF NOT EXISTS organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT;
		ALTER TABLE templates ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE templates ADD COLUMN IF NOT EXISTS original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE templates ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global'));
		ALTER TABLE templates ADD COLUMN IF NOT EXISTS transfer_pending_at TIMESTAMP WITH TIME ZONE;

		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT;
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global'));
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS transfer_pending_at TIMESTAMP WITH TIME ZONE;

		ALTER TABLE media ADD COLUMN IF NOT EXISTS organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT;
		ALTER TABLE media ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE media ADD COLUMN IF NOT EXISTS original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
		ALTER TABLE media ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global'));
		ALTER TABLE media ADD COLUMN IF NOT EXISTS transfer_pending_at TIMESTAMP WITH TIME ZONE;
		ALTER TABLE media ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW();

		CREATE INDEX IF NOT EXISTS idx_lists_workspace_owner ON lists(organization_id, owner_user_id);
		CREATE INDEX IF NOT EXISTS idx_subscribers_workspace_owner ON subscribers(organization_id, owner_user_id);
		CREATE INDEX IF NOT EXISTS idx_templates_workspace_owner_visibility ON templates(organization_id, owner_user_id, visibility);
		CREATE INDEX IF NOT EXISTS idx_campaigns_workspace_owner_visibility ON campaigns(organization_id, owner_user_id, visibility);
		CREATE INDEX IF NOT EXISTS idx_media_workspace_owner ON media(organization_id, owner_user_id);

		-- The legacy schema allowed exactly one default template for the whole
		-- installation. Defaults are now per owner and workspace.
		DROP INDEX IF EXISTS templates_is_default_idx;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_templates_workspace_default
			ON templates ((COALESCE(organization_id, 0)), owner_user_id)
			WHERE is_default AND owner_user_id IS NOT NULL;

		ALTER TABLE subscribers DROP CONSTRAINT IF EXISTS subscribers_email_key;
		DROP INDEX IF EXISTS idx_subs_email;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_subscribers_scope_owner_email
			ON subscribers ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
			WHERE owner_user_id IS NOT NULL;
	`)
	if err != nil {
		return err
	}

	// Existing installs have a known platform administrator. Keep historical
	// resources personal and make that account the explicit owner. New installs
	// without a user retain NULL ownership until first-time setup claims them.
	_, err = db.Exec(`
		WITH admin AS (
			SELECT id FROM users WHERE user_role_id = 1 ORDER BY id LIMIT 1
		)
		UPDATE lists SET owner_user_id = (SELECT id FROM admin), original_owner_user_id = (SELECT id FROM admin)
			WHERE owner_user_id IS NULL AND EXISTS (SELECT 1 FROM admin);
		WITH admin AS (
			SELECT id FROM users WHERE user_role_id = 1 ORDER BY id LIMIT 1
		)
		UPDATE subscribers SET owner_user_id = (SELECT id FROM admin), original_owner_user_id = (SELECT id FROM admin)
			WHERE owner_user_id IS NULL AND EXISTS (SELECT 1 FROM admin);
		WITH admin AS (
			SELECT id FROM users WHERE user_role_id = 1 ORDER BY id LIMIT 1
		)
		UPDATE templates SET owner_user_id = (SELECT id FROM admin), original_owner_user_id = (SELECT id FROM admin)
			WHERE owner_user_id IS NULL AND EXISTS (SELECT 1 FROM admin);
		WITH admin AS (
			SELECT id FROM users WHERE user_role_id = 1 ORDER BY id LIMIT 1
		)
		UPDATE campaigns SET owner_user_id = (SELECT id FROM admin), original_owner_user_id = (SELECT id FROM admin)
			WHERE owner_user_id IS NULL AND EXISTS (SELECT 1 FROM admin);
		WITH admin AS (
			SELECT id FROM users WHERE user_role_id = 1 ORDER BY id LIMIT 1
		)
		UPDATE media SET owner_user_id = (SELECT id FROM admin), original_owner_user_id = (SELECT id FROM admin)
			WHERE owner_user_id IS NULL AND EXISTS (SELECT 1 FROM admin);
	`)
	return err
}
