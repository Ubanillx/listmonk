package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
	"log"
)

// V6_69_0 adds read-only recipient state counts without changing sender state.
func V6_69_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(CampaignDeliveryCountsSQL)
	return err
}

const CampaignDeliveryCountsSQL = `CREATE OR REPLACE VIEW campaign_delivery_counts AS
SELECT c.id AS campaign_id,
    CASE WHEN r.total > 0 THEN r.total ELSE GREATEST(c.to_send, c.sent) END AS delivery_total,
    CASE WHEN r.total > 0 THEN r.sent ELSE c.sent END AS delivery_sent,
    CASE WHEN r.total > 0 THEN r.pending ELSE GREATEST(c.to_send - c.sent, 0) END AS delivery_pending,
    r.unavailable AS delivery_unavailable
FROM campaigns c
CROSS JOIN LATERAL (
    SELECT COUNT(*)::INT AS total,
        COUNT(*) FILTER (WHERE status = 'sent')::INT AS sent,
        COUNT(*) FILTER (WHERE status IN ('pending', 'queued', 'deferred'))::INT AS pending,
        COUNT(*) FILTER (WHERE status IN ('failed', 'cancelled'))::INT AS unavailable
    FROM (
        SELECT cr.status FROM campaign_recipients cr
        JOIN customers cu ON cu.id = cr.customer_id
        WHERE cr.campaign_id = c.id
            AND cu.organization_id IS NOT DISTINCT FROM c.organization_id
            AND cu.owner_user_id = c.owner_user_id AND cu.transfer_pending_at IS NULL
        UNION ALL
        SELECT pr.status FROM campaign_pool_recipients pr WHERE pr.campaign_id = c.id
            AND (c.pool_scope = 'all_organizations' OR pr.organization_id IS NOT DISTINCT FROM c.organization_id)
    ) recipients
) r;`
