package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
	"log"
)

// V6_68_0 joins verified unsubscribe-link clicks and confirmed opt-outs without
// treating a landing-page visit as consent. Historical confirmations require
// both a successful self-service audit and the matching state-change timestamp.
func V6_68_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(CampaignUnsubscriptionActivitySQL); err != nil {
		return err
	}
	if _, err = tx.Exec(CampaignUnsubscriptionBackfillSQL); err != nil {
		return err
	}
	return tx.Commit()
}

const CampaignUnsubscriptionActivitySQL = `CREATE OR REPLACE VIEW campaign_unsubscription_activity AS
WITH valid_clicks AS (
    SELECT lc.campaign_id,
        CASE WHEN lc.customer_id IS NOT NULL THEN 'private' ELSE 'pool' END AS recipient_type,
        COALESCE(lc.customer_id::BIGINT,lc.pool_contact_id) AS recipient_id,
        c.owner_user_id AS campaign_owner_user_id,c.organization_id AS campaign_organization_id,
        COALESCE(pr.organization_id,c.organization_id,0)::INT AS recipient_organization_id,
        lc.created_at
    FROM link_clicks lc
    JOIN campaigns c ON c.id=lc.campaign_id
    JOIN links l ON l.id=lc.link_id
    JOIN settings root ON root.key='app.root_url'
    LEFT JOIN customers cu ON cu.id=lc.customer_id
    LEFT JOIN pool_contacts pc ON pc.id=lc.pool_contact_id
    LEFT JOIN campaign_pool_recipients pr ON pr.campaign_id=lc.campaign_id AND pr.pool_contact_id=lc.pool_contact_id
    WHERE lc.created_at IS NOT NULL AND (
        (lc.customer_id IS NOT NULL AND (
            split_part(split_part(l.url,'?',1),'#',1)=rtrim(root.value#>>'{}','/')||'/subscription/'||c.uuid::TEXT||'/'||cu.uuid::TEXT
            OR EXISTS(SELECT 1 FROM customer_uuid_aliases ca WHERE ca.customer_id=lc.customer_id
                AND split_part(split_part(l.url,'?',1),'#',1)=rtrim(root.value#>>'{}','/')||'/subscription/'||c.uuid::TEXT||'/'||ca.uuid::TEXT)
        ))
        OR (lc.pool_contact_id IS NOT NULL AND pr.pool_contact_id IS NOT NULL
            AND split_part(split_part(l.url,'?',1),'#',1)=rtrim(root.value#>>'{}','/')||'/subscription/'||c.uuid::TEXT||'/'||pc.uuid::TEXT)
    )
), clicks AS (
    SELECT campaign_id,recipient_type,recipient_id,campaign_owner_user_id,campaign_organization_id,recipient_organization_id,
        MIN(created_at) AS clicked_at,MAX(created_at) AS last_clicked_at,COUNT(*)::INT AS click_count
    FROM valid_clicks
    GROUP BY campaign_id,recipient_type,recipient_id,campaign_owner_user_id,campaign_organization_id,recipient_organization_id
)
SELECT COALESCE(u.campaign_id,cl.campaign_id) AS campaign_id,
    COALESCE(u.recipient_type,cl.recipient_type) AS recipient_type,
    COALESCE(u.recipient_id,cl.recipient_id) AS recipient_id,
    CASE WHEN u.campaign_id IS NOT NULL THEN u.campaign_owner_user_id ELSE cl.campaign_owner_user_id END AS campaign_owner_user_id,
    CASE WHEN u.campaign_id IS NOT NULL THEN u.campaign_organization_id ELSE cl.campaign_organization_id END AS campaign_organization_id,
    COALESCE(u.recipient_organization_id,cl.recipient_organization_id) AS recipient_organization_id,
    COALESCE(cl.clicked_at,u.created_at) AS created_at,
    cl.clicked_at,cl.last_clicked_at,u.created_at AS confirmed_at,COALESCE(cl.click_count,0) AS click_count,
    CASE WHEN u.campaign_id IS NOT NULL THEN 'confirmed' ELSE 'clicked' END AS status
FROM campaign_unsubscriptions u FULL OUTER JOIN clicks cl
    ON cl.campaign_id=u.campaign_id AND cl.recipient_type=u.recipient_type AND cl.recipient_id=u.recipient_id;`

const CampaignUnsubscriptionBackfillSQL = `WITH proof AS (
    SELECT a.object_id,a.occurred_at,c.id AS campaign_id,c.owner_user_id,c.organization_id
    FROM audit_events a JOIN campaigns c ON c.uuid::TEXT=a.metadata->>'campaign_id'
    WHERE a.action='subscription.unsubscribed' AND a.result='success' AND a.object_type='customer'
        AND a.metadata->>'channel'='public_self_service' AND a.metadata->>'http_method'='POST'
        AND a.metadata->>'http_status'='200' AND a.metadata->>'route'='/subscription/:campUUID/:subUUID'
), confirmed AS (
    SELECT p.campaign_id,'pool'::TEXT AS recipient_type,pr.pool_contact_id AS recipient_id,
        p.owner_user_id,p.organization_id,COALESCE(pr.organization_id,0)::INT AS recipient_organization_id,p.occurred_at
    FROM proof p JOIN pool_contacts pc ON pc.uuid::TEXT=p.object_id
    JOIN campaign_pool_recipients pr ON pr.campaign_id=p.campaign_id AND pr.pool_contact_id=pc.id
    JOIN org_pool_allocation_exclusions ex ON ex.pool_id=pr.pool_id AND ex.organization_id=pr.organization_id AND ex.contact_id=pc.id
    WHERE ex.source='unsubscribe' AND ex.removed_at BETWEEN p.occurred_at-INTERVAL '1 minute' AND p.occurred_at+INTERVAL '1 second'
    UNION ALL
    SELECT p.campaign_id,'private',cu.id::BIGINT,p.owner_user_id,p.organization_id,COALESCE(p.organization_id,0)::INT,p.occurred_at
    FROM proof p JOIN customers cu ON cu.uuid::TEXT=p.object_id
        OR EXISTS(SELECT 1 FROM customer_uuid_aliases ca WHERE ca.customer_id=cu.id AND ca.uuid::TEXT=p.object_id)
    JOIN campaign_recipients cr ON cr.campaign_id=p.campaign_id AND cr.customer_id=cu.id
    WHERE cu.owner_user_id=p.owner_user_id AND cu.organization_id IS NOT DISTINCT FROM p.organization_id
        AND cu.transfer_pending_at IS NULL AND EXISTS(
            SELECT 1 FROM campaign_customer_lists cl JOIN customer_list_memberships m ON m.customer_list_id=cl.customer_list_id
            WHERE cl.campaign_id=p.campaign_id AND m.customer_id=cu.id AND m.status='unsubscribed'
                AND m.updated_at BETWEEN p.occurred_at-INTERVAL '1 minute' AND p.occurred_at+INTERVAL '1 second')
)
INSERT INTO campaign_unsubscriptions(campaign_id,recipient_type,recipient_id,campaign_owner_user_id,campaign_organization_id,recipient_organization_id,created_at)
SELECT campaign_id,recipient_type,recipient_id,owner_user_id,organization_id,recipient_organization_id,MIN(occurred_at)
FROM confirmed GROUP BY campaign_id,recipient_type,recipient_id,owner_user_id,organization_id,recipient_organization_id
ON CONFLICT(campaign_id,recipient_type,recipient_id) DO NOTHING;`
