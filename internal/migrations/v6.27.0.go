package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_27_0 adds the first-class public customer pool and organization segments.
// The migration is idempotent so interrupted upgrades can be retried safely.
func V6_27_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		DO $$ BEGIN
			ALTER TYPE customer_list_type ADD VALUE IF NOT EXISTS 'pool';
			ALTER TYPE customer_list_type ADD VALUE IF NOT EXISTS 'pool_segment';
		EXCEPTION WHEN duplicate_object THEN NULL; END $$;

		ALTER TABLE customer_lists ADD COLUMN IF NOT EXISTS pool_parent_id INTEGER REFERENCES customer_lists(id) ON DELETE CASCADE;
		ALTER TABLE customer_lists ADD COLUMN IF NOT EXISTS pool_reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
		CREATE INDEX IF NOT EXISTS idx_customer_lists_pool_parent ON customer_lists(pool_parent_id);

		CREATE TABLE IF NOT EXISTS pool_contacts (
			id BIGSERIAL PRIMARY KEY,
			customer_code TEXT NOT NULL DEFAULT '',
			company_name TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			attribs JSONB NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		ALTER TABLE pool_contacts ADD COLUMN IF NOT EXISTS uuid UUID;
		UPDATE pool_contacts SET uuid=gen_random_uuid() WHERE uuid IS NULL;
		ALTER TABLE pool_contacts ALTER COLUMN uuid SET NOT NULL;
		ALTER TABLE pool_contacts ALTER COLUMN uuid SET DEFAULT gen_random_uuid();
		CREATE UNIQUE INDEX IF NOT EXISTS idx_pool_contacts_uuid ON pool_contacts(uuid);
		CREATE INDEX IF NOT EXISTS idx_pool_contacts_code ON pool_contacts(customer_code);
		CREATE INDEX IF NOT EXISTS idx_pool_contacts_email ON pool_contacts(LOWER(email));

		CREATE TABLE IF NOT EXISTS pool_members (
			pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (pool_id, contact_id)
		);

		CREATE TABLE IF NOT EXISTS pool_segments (
			id BIGSERIAL PRIMARY KEY,
			list_id INTEGER NOT NULL UNIQUE REFERENCES customer_lists(id) ON DELETE CASCADE,
			pool_id INTEGER NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
			created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		ALTER TABLE pool_segments ALTER COLUMN pool_id DROP NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_pool_segments_org ON pool_segments(organization_id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_pool_segments_pool_org ON pool_segments(pool_id, organization_id);

		CREATE TABLE IF NOT EXISTS pool_segment_members (
			segment_id BIGINT NOT NULL REFERENCES pool_segments(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','removed')),
			removed_reason TEXT NOT NULL DEFAULT '',
			removed_at TIMESTAMPTZ,
			removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (segment_id, contact_id)
		);
		CREATE INDEX IF NOT EXISTS idx_pool_segment_members_status ON pool_segment_members(segment_id, status);

		CREATE TABLE IF NOT EXISTS pool_organization_permissions (
			pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			granted_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (pool_id, organization_id)
		);

		CREATE TABLE IF NOT EXISTS pool_segment_exclusions (
			pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
			segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
			reason TEXT NOT NULL DEFAULT 'manual',
			source TEXT NOT NULL DEFAULT 'segment',
			removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			removed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			restored_at TIMESTAMPTZ,
			PRIMARY KEY (pool_id, organization_id, contact_id)
		);
		CREATE INDEX IF NOT EXISTS idx_pool_exclusions_org ON pool_segment_exclusions(organization_id, pool_id);

		ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS pool_id INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL;
		ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS pool_segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL;
		ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS source_organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
		ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS resolved_reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
		CREATE INDEX IF NOT EXISTS idx_campaign_customer_lists_pool ON campaign_customer_lists(pool_id, pool_segment_id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_campaign_customer_lists_pool_unique ON campaign_customer_lists(campaign_id, pool_id, COALESCE(pool_segment_id, 0)) WHERE pool_id IS NOT NULL;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT REFERENCES pool_contacts(id) ON DELETE SET NULL;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_pool_id INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
		ALTER TABLE bounces ALTER COLUMN customer_id DROP NOT NULL;
		ALTER TABLE bounces ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT;
		ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_pool_id INTEGER;
		ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_segment_id BIGINT;
		ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_organization_id BIGINT;
		CREATE INDEX IF NOT EXISTS idx_bounces_pool_contact ON bounces(pool_contact_id);
		ALTER TABLE reply_ai_events ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT;
		ALTER TABLE reply_ai_events ADD COLUMN IF NOT EXISTS pool_id INTEGER;
		ALTER TABLE reply_ai_events ADD COLUMN IF NOT EXISTS source_segment_id BIGINT;
		ALTER TABLE reply_ai_events ADD COLUMN IF NOT EXISTS source_organization_id BIGINT;
		CREATE TABLE IF NOT EXISTS campaign_pool_recipients (
			campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			pool_contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
			pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
			segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
			organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
			reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
			status campaign_recipient_status NOT NULL DEFAULT 'pending',
			email_snapshot TEXT NOT NULL,
			name_snapshot TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (campaign_id, pool_contact_id)
		);

		CREATE TABLE IF NOT EXISTS pool_merge_conflicts (
			id BIGSERIAL PRIMARY KEY,
			pool_id INTEGER REFERENCES customer_lists(id) ON DELETE CASCADE,
			contact_id BIGINT REFERENCES pool_contacts(id) ON DELETE SET NULL,
			customer_code TEXT NOT NULL,
			existing_snapshot JSONB NOT NULL DEFAULT '{}',
			incoming_snapshot JSONB NOT NULL DEFAULT '{}',
			created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}
