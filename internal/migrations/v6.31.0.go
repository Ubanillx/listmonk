package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// CampaignSendCountsSchema installs the single definition of a campaign's
// effective recipient set and the counters derived from it.
//
// The rules previously existed in four places: the list/detail/scheduler
// projections counted customer snapshot rows only (skipping the ownership and
// transfer checks on some paths), the send-state query that drives the sender
// added pool recipients, and sync-campaign-progress had its own copy of both
// rules. A campaign whose audience includes pools therefore showed one unsent
// count in the UI and a different one to the sender.
//
// The body must stay identical to the definition in schema.sql so a fresh
// install and an upgraded install agree.
const CampaignSendCountsSchema = `
DROP VIEW IF EXISTS campaign_send_counts CASCADE;
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
        AND cpr.organization_id IS NOT DISTINCT FROM c.organization_id
) po ON TRUE;
`

// V6_31_0 installs the campaign send-count view.
func V6_31_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(CampaignSendCountsSchema)
	return err
}
