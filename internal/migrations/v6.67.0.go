package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_67_0 makes retry rounds durable without rewriting delivery history.
func V6_67_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(campaignRetryQueueSQL)
	return err
}

const campaignRetryQueueSQL = `CREATE OR REPLACE VIEW campaign_retry_queue AS
WITH failures AS (
    SELECT campaign_id,recipient_type,recipient_id,COUNT(*) AS failures
    FROM campaign_send_errors GROUP BY campaign_id,recipient_type,recipient_id
), recipients AS (
    SELECT cr.campaign_id,'private'::TEXT AS recipient_type,cr.customer_id::BIGINT AS recipient_id,cr.status
    FROM campaign_recipients cr JOIN campaigns c ON c.id=cr.campaign_id
    JOIN customers s ON s.id=cr.customer_id
    WHERE cr.status IN ('pending','queued','deferred') AND c.owner_user_id IS NOT NULL
        AND c.transfer_pending_at IS NULL
        AND (c.organization_id IS NULL OR EXISTS(SELECT 1 FROM organizations o WHERE o.id=c.organization_id AND o.status='active'))
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id AND s.owner_user_id=c.owner_user_id
        AND s.transfer_pending_at IS NULL AND (NOT c.retry_mode OR cr.retry_selected)
        AND NOT campaign_recipient_retry_exhausted(cr.campaign_id,'private',cr.customer_id)
        AND EXISTS(SELECT 1 FROM campaign_customer_lists cl
            JOIN customer_lists l ON l.id=cl.customer_list_id
            JOIN customer_list_memberships sl ON sl.customer_list_id=l.id AND sl.customer_id=s.id
            WHERE cl.campaign_id=c.id AND l.organization_id IS NOT DISTINCT FROM c.organization_id
                AND l.owner_user_id=c.owner_user_id AND l.transfer_pending_at IS NULL
                AND ((c.type='optin' AND sl.status='unconfirmed' AND l.optin='double')
                    OR (c.type<>'optin' AND ((l.optin='double' AND sl.status='confirmed')
                        OR (l.optin<>'double' AND sl.status<>'unsubscribed')))))
    UNION ALL
    SELECT cpr.campaign_id,'pool'::TEXT,cpr.pool_contact_id,cpr.status
    FROM campaign_pool_recipients cpr JOIN campaigns c ON c.id=cpr.campaign_id
    JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id
    WHERE cpr.status IN ('pending','queued','deferred') AND pc.status='active'
        AND (NOT c.retry_mode OR cpr.retry_selected)
        AND (c.pool_scope='all_organizations' OR cpr.organization_id IS NOT DISTINCT FROM c.organization_id)
        AND NOT campaign_recipient_retry_exhausted(cpr.campaign_id,'pool',cpr.pool_contact_id)
        AND NOT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions ex
            WHERE ex.pool_id=cpr.pool_id AND ex.organization_id=cpr.organization_id
                AND ex.contact_id=cpr.pool_contact_id AND ex.restored_at IS NULL)
        AND EXISTS(SELECT 1 FROM org_pool_allocations a
            JOIN organizations o ON o.id=a.organization_id AND o.status='active'
            JOIN customer_lists al ON al.id=a.list_id AND al.status='active' AND al.transfer_pending_at IS NULL
            JOIN customer_lists ap ON ap.id=a.pool_id AND ap.status='active' AND ap.transfer_pending_at IS NULL
            JOIN pool_members pm ON pm.pool_id=a.pool_id AND pm.contact_id=cpr.pool_contact_id
            JOIN org_pool_allocation_members am ON am.allocation_id=a.id AND am.contact_id=cpr.pool_contact_id AND am.status='active'
            WHERE a.id=cpr.allocation_id AND a.pool_id=cpr.pool_id
                AND a.organization_id=cpr.organization_id AND a.state='active')
        AND (c.pool_scope='all_organizations' OR EXISTS(SELECT 1 FROM pool_organization_permissions p
            WHERE p.pool_id=cpr.pool_id AND p.organization_id=cpr.organization_id))
)
SELECT r.*,COALESCE(f.failures,0) AS failures FROM recipients r
LEFT JOIN failures f ON f.campaign_id=r.campaign_id AND f.recipient_type=r.recipient_type AND f.recipient_id=r.recipient_id;`
