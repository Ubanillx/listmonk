package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_72_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(CampaignHistoryPreservationSQL)
	return err
}

// CampaignHistoryPreservationSQL keeps recipient snapshots and the historical
// planned count when customers or pool contacts are deleted.
const CampaignHistoryPreservationSQL = `
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS planned_total INT NOT NULL DEFAULT 0;
UPDATE campaigns
    SET planned_total = GREATEST(to_send, sent)
    WHERE planned_total < GREATEST(to_send, sent);

-- Recipient snapshot tables intentionally outlive their source rows. Drop the
-- cascading foreign keys while keeping the columns as immutable references.
-- Live send/count queries join customers and pool_contacts explicitly.
DO $$
DECLARE
    con RECORD;
BEGIN
    IF to_regclass('campaign_recipients') IS NOT NULL THEN
        FOR con IN
            SELECT conname FROM pg_constraint
            WHERE conrelid = 'campaign_recipients'::regclass
                AND contype = 'f'
                AND conkey = ARRAY[(
                    SELECT attnum FROM pg_attribute
                    WHERE attrelid = 'campaign_recipients'::regclass
                        AND attname = 'customer_id'
                )]
        LOOP
            EXECUTE format('ALTER TABLE campaign_recipients DROP CONSTRAINT %I', con.conname);
        END LOOP;
    END IF;
    IF to_regclass('campaign_pool_recipients') IS NOT NULL THEN
        FOR con IN
            SELECT conname FROM pg_constraint
            WHERE conrelid = 'campaign_pool_recipients'::regclass
                AND contype = 'f'
                AND conkey = ARRAY[(
                    SELECT attnum FROM pg_attribute
                    WHERE attrelid = 'campaign_pool_recipients'::regclass
                        AND attname = 'pool_contact_id'
                )]
        LOOP
            EXECUTE format('ALTER TABLE campaign_pool_recipients DROP CONSTRAINT %I', con.conname);
        END LOOP;
        FOR con IN
            SELECT conname FROM pg_constraint
            WHERE conrelid = 'campaign_pool_recipients'::regclass
                AND contype = 'f'
                AND conkey = ARRAY[(
                    SELECT attnum FROM pg_attribute
                    WHERE attrelid = 'campaign_pool_recipients'::regclass
                        AND attname = 'pool_id'
                )]
        LOOP
            EXECUTE format('ALTER TABLE campaign_pool_recipients DROP CONSTRAINT %I', con.conname);
        END LOOP;
    END IF;
END $$;

COMMENT ON COLUMN campaign_recipients.customer_id IS
    'Immutable historical recipient reference; snapshots survive customer deletion. Live queries must join customers.';
COMMENT ON COLUMN campaign_pool_recipients.pool_contact_id IS
    'Immutable historical pool recipient reference; snapshots survive pool contact deletion. Live queries must join pool_contacts.';
COMMENT ON COLUMN campaign_pool_recipients.pool_id IS
    'Historical source pool id; the snapshot survives pool list deletion.';

CREATE OR REPLACE VIEW campaign_send_counts AS
SELECT
    c.id AS campaign_id,
    COALESCE(cu.total, 0) + COALESCE(po.total, 0) AS total_count,
    COALESCE(cu.sent, 0) + COALESCE(po.sent, 0) AS sent_count,
    COALESCE(cu.queued, 0) + COALESCE(po.queued, 0) AS queued_count,
    COALESCE(cu.unsent, 0) + COALESCE(po.unsent, 0) AS unsent_snapshot_count,
    CASE
        WHEN COALESCE(cu.total, 0) + COALESCE(po.total, 0) > 0
            THEN COALESCE(cu.unsent, 0) + COALESCE(po.unsent, 0)
        ELSE GREATEST(GREATEST(c.planned_total, c.to_send, c.sent) - c.sent, 0)
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
    WHERE cr.campaign_id = c.id
        AND (
            c.status = ANY('{finished,cancelled}'::campaign_status[])
            OR EXISTS (
                SELECT 1 FROM customers s
                WHERE s.id = cr.customer_id
                    AND s.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND s.owner_user_id = c.owner_user_id
                    AND s.transfer_pending_at IS NULL
            )
        )
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
        AND (
            c.status = ANY('{finished,cancelled}'::campaign_status[])
            OR (
                EXISTS (SELECT 1 FROM pool_contacts pc WHERE pc.id = cpr.pool_contact_id)
                AND EXISTS (SELECT 1 FROM customer_lists pl
                    WHERE pl.id = cpr.pool_id AND pl.status = 'active' AND pl.transfer_pending_at IS NULL)
            )
        )
) po ON TRUE;

CREATE OR REPLACE VIEW campaign_delivery_counts AS
SELECT c.id AS campaign_id,
    CASE WHEN r.total > 0 THEN r.total ELSE GREATEST(c.planned_total, c.to_send, c.sent) END AS delivery_total,
    CASE WHEN r.total > 0 THEN r.sent ELSE c.sent END AS delivery_sent,
    CASE WHEN r.total > 0 THEN r.pending
        ELSE GREATEST(GREATEST(c.planned_total, c.to_send, c.sent) - c.sent, 0) END AS delivery_pending,
    r.unavailable AS delivery_unavailable
FROM campaigns c
CROSS JOIN LATERAL (
    SELECT COUNT(*)::INT AS total,
        COUNT(*) FILTER (WHERE status = 'sent')::INT AS sent,
        COUNT(*) FILTER (WHERE status IN ('pending', 'queued', 'deferred'))::INT AS pending,
        COUNT(*) FILTER (WHERE status IN ('failed', 'cancelled'))::INT AS unavailable
    FROM (
        SELECT cr.status FROM campaign_recipients cr
        WHERE cr.campaign_id = c.id
            AND (
                c.status = ANY('{finished,cancelled}'::campaign_status[])
                OR EXISTS (
                    SELECT 1 FROM customers cu
                    WHERE cu.id = cr.customer_id
                        AND cu.organization_id IS NOT DISTINCT FROM c.organization_id
                        AND cu.owner_user_id = c.owner_user_id
                        AND cu.transfer_pending_at IS NULL
                )
            )
        UNION ALL
        SELECT pr.status FROM campaign_pool_recipients pr WHERE pr.campaign_id = c.id
            AND (c.pool_scope = 'all_organizations' OR pr.organization_id IS NOT DISTINCT FROM c.organization_id)
            AND (
                c.status = ANY('{finished,cancelled}'::campaign_status[])
                OR (
                    EXISTS (SELECT 1 FROM pool_contacts pc WHERE pc.id = pr.pool_contact_id)
                    AND EXISTS (SELECT 1 FROM customer_lists pl
                        WHERE pl.id = pr.pool_id AND pl.status = 'active' AND pl.transfer_pending_at IS NULL)
                )
            )
    ) recipients
) r;
`
