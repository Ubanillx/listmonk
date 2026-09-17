package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_44_0 introduces platform-level public-pool campaigns
// (campaigns.pool_scope = 'all_organizations') and their delivery plumbing:
//
//   - campaigns.pool_scope / pool_next_org_index select and rotate the fair
//     organization order of an all-organization campaign,
//   - campaign_pool_org_orders persists the randomly generated organization
//     rotation for the campaign's whole life,
//   - org_pool_smtp_cursors persists the organization's shared SMTP
//     round-robin cursor across campaigns and service restarts,
//   - campaign_pool_recipients gains immutable sender provenance columns
//     (SMTP UUID, owning user, From snapshot, assignment time) with no
//     foreign keys so deleting an SMTP row or account keeps delivery history,
//   - campaign_send_counts counts pool rows of all-organization campaigns
//     across every target organization instead of only the campaign's own
//     organization.
//
// Every statement is guarded, so the migration is idempotent and a no-op on
// databases created from the current schema.sql.
func V6_44_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		DO $$ BEGIN
			IF to_regclass('campaigns') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaigns') AND a.attname='pool_scope' AND NOT a.attisdropped) THEN
				ALTER TABLE campaigns ADD COLUMN pool_scope TEXT NOT NULL DEFAULT 'organization';
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaigns') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conrelid=to_regclass('campaigns') AND c.conname='campaigns_pool_scope_check') THEN
				ALTER TABLE campaigns ADD CONSTRAINT campaigns_pool_scope_check CHECK (pool_scope IN ('organization', 'all_organizations'));
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaigns') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaigns') AND a.attname='pool_next_org_index' AND NOT a.attisdropped) THEN
				ALTER TABLE campaigns ADD COLUMN pool_next_org_index INT NOT NULL DEFAULT 0;
			END IF;
		END $$;

		DO $$ BEGIN
			IF to_regclass('campaign_pool_recipients') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaign_pool_recipients') AND a.attname='sender_smtp_uuid' AND NOT a.attisdropped) THEN
				ALTER TABLE campaign_pool_recipients ADD COLUMN sender_smtp_uuid UUID NULL;
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaign_pool_recipients') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaign_pool_recipients') AND a.attname='sender_user_id' AND NOT a.attisdropped) THEN
				ALTER TABLE campaign_pool_recipients ADD COLUMN sender_user_id INTEGER NULL;
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaign_pool_recipients') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaign_pool_recipients') AND a.attname='sender_from_snapshot' AND NOT a.attisdropped) THEN
				ALTER TABLE campaign_pool_recipients ADD COLUMN sender_from_snapshot TEXT NOT NULL DEFAULT '';
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaign_pool_recipients') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('campaign_pool_recipients') AND a.attname='sender_assigned_at' AND NOT a.attisdropped) THEN
				ALTER TABLE campaign_pool_recipients ADD COLUMN sender_assigned_at TIMESTAMPTZ NULL;
			END IF;
		END $$;
		DO $$ BEGIN
			IF to_regclass('campaign_pool_recipients') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_indexes i WHERE i.indexname='idx_campaign_pool_recipients_sender') THEN
				CREATE INDEX idx_campaign_pool_recipients_sender ON campaign_pool_recipients(sender_smtp_uuid) WHERE sender_smtp_uuid IS NOT NULL;
			END IF;
		END $$;

		CREATE TABLE IF NOT EXISTS campaign_pool_org_orders (
			campaign_id     INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
			organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
			dispatch_order  INTEGER NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (campaign_id, organization_id)
		);
		CREATE INDEX IF NOT EXISTS idx_campaign_pool_org_orders_campaign ON campaign_pool_org_orders(campaign_id, dispatch_order);

		CREATE TABLE IF NOT EXISTS org_pool_smtp_cursors (
			organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
			next_smtp_uuid  UUID NULL,
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		-- Recreate the send-count view so pool rows of all-organization
		-- campaigns count for the campaign regardless of the campaign's own
		-- organization. DROP ... CASCADE is safe: the view has no dependents.
		DROP VIEW IF EXISTS campaign_send_counts;
		CREATE VIEW campaign_send_counts AS
		SELECT
			c.id AS campaign_id,
			COALESCE(cu.total, 0) + COALESCE(po.total, 0) AS total_count,
			COALESCE(cu.sent, 0) + COALESCE(po.sent, 0) AS sent_count,
			COALESCE(cu.queued, 0) + COALESCE(po.queued, 0) AS queued_count,
			COALESCE(cu.unsent, 0) + COALESCE(po.unsent, 0) AS unsent_snapshot_count,
			CASE
				WHEN COALESCE(cu.total, 0) + COALESCE(po.total, 0) > 0
					THEN COALESCE(cu.unsent, 0) + COALESCE(po.unsent, 0)
				ELSE GREATEST(c.to_send - c.sent, 0)
			END AS unsent_count,
			(COALESCE(cu.total, 0) + COALESCE(po.total, 0)) > 0 AS has_snapshot
		FROM campaigns c
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE cr.status = 'sent') AS sent,
				COUNT(*) FILTER (WHERE cr.status = 'queued') AS queued,
				COUNT(*) FILTER (WHERE cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])) AS unsent
			FROM campaign_recipients cr
			JOIN customers s ON s.id = cr.customer_id
			WHERE cr.campaign_id = c.id
				AND s.organization_id IS NOT DISTINCT FROM c.organization_id
				AND s.owner_user_id = c.owner_user_id
				AND s.transfer_pending_at IS NULL
		) cu ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE cpr.status = 'sent') AS sent,
				COUNT(*) FILTER (WHERE cpr.status = 'queued') AS queued,
				COUNT(*) FILTER (WHERE cpr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])) AS unsent
			FROM campaign_pool_recipients cpr
			WHERE cpr.campaign_id = c.id
				AND (c.pool_scope = 'all_organizations' OR cpr.organization_id IS NOT DISTINCT FROM c.organization_id)
		) po ON TRUE;
	`)
	return err
}
