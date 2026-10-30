package core

import (
	"github.com/knadh/listmonk/models"
)

// GetCampaignDeliveryOverview uses recipient state for people and the durable
// attempt counter for errors; the two numbers have deliberately different grain.
func (c *Core) GetCampaignDeliveryOverview(access models.WorkspaceAccess, id int) (models.CampaignDeliveryOverview, error) {
	out := models.CampaignDeliveryOverview{Organizations: []models.CampaignDeliveryOrganization{}}
	if _, err := c.RequireReadResource(access, resourceCampaigns, id); err != nil {
		return out, err
	}
	err := c.db.Get(&out, `SELECT c.id AS campaign_id,c.status,c.send_errors,
		CASE WHEN COALESCE(sc.has_snapshot,false) THEN sc.total_count ELSE c.to_send END AS planned,
		CASE WHEN COALESCE(sc.has_snapshot,false) THEN sc.sent_count ELSE c.sent END AS sent,
		COALESCE(sc.unsent_count,0) AS unsent
		FROM campaigns c LEFT JOIN campaign_send_counts sc ON sc.campaign_id=c.id
		WHERE c.id=$1 AND c.transfer_pending_at IS NULL`, id)
	if err != nil {
		return out, workspaceQueryError("fetching campaign delivery overview", err)
	}
	var counts struct {
		Private   int `db:"private_count"`
		Pool      int `db:"pool_count"`
		Failed    int `db:"failed_count"`
		Exhausted int `db:"exhausted_count"`
	}
	err = c.db.Get(&counts, `WITH failure_keys AS (
		SELECT DISTINCT e.recipient_type,e.recipient_id,e.recipient_organization_id
		FROM campaign_send_errors e JOIN campaigns c ON c.id=e.campaign_id WHERE e.campaign_id=$1
		AND e.campaign_owner_user_id IS NOT DISTINCT FROM c.owner_user_id
		AND e.campaign_organization_id IS NOT DISTINCT FROM c.organization_id
	), live_private AS (
		SELECT cr.customer_id,cr.status FROM campaign_recipients cr JOIN campaigns c ON c.id=cr.campaign_id
		JOIN customers cu ON cu.id=cr.customer_id AND cu.organization_id IS NOT DISTINCT FROM c.organization_id
			AND cu.owner_user_id=c.owner_user_id AND cu.transfer_pending_at IS NULL
		WHERE cr.campaign_id=$1 AND cr.status IN ('pending','queued','deferred','failed')
	), live_pool AS (
		SELECT cpr.pool_contact_id,cpr.organization_id,cpr.status FROM campaign_pool_recipients cpr
		JOIN campaigns c ON c.id=cpr.campaign_id
		WHERE cpr.campaign_id=$1 AND cpr.status IN ('pending','queued','deferred','failed')
		AND (c.pool_scope='all_organizations' OR cpr.organization_id IS NOT DISTINCT FROM c.organization_id)
	)
	SELECT (SELECT COUNT(*)::INT FROM campaign_recipients WHERE campaign_id=$1) AS private_count,
		(SELECT COUNT(*)::INT FROM campaign_pool_recipients WHERE campaign_id=$1) AS pool_count,
		(SELECT COUNT(DISTINCT (e.recipient_type,e.recipient_id))::INT FROM failure_keys e WHERE
			(e.recipient_type='private' AND EXISTS(SELECT 1 FROM live_private p WHERE p.customer_id=e.recipient_id AND p.status='failed'))
			OR (e.recipient_type='pool' AND EXISTS(SELECT 1 FROM live_pool p WHERE p.pool_contact_id=e.recipient_id AND p.status='failed'
				AND (e.recipient_organization_id=0 OR p.organization_id=e.recipient_organization_id)))) AS exhausted_count,
		(SELECT COUNT(DISTINCT (e.recipient_type,e.recipient_id))::INT FROM failure_keys e WHERE (e.recipient_type='private' AND EXISTS(SELECT 1 FROM live_private p WHERE p.customer_id=e.recipient_id))
			OR (e.recipient_type='pool' AND EXISTS(SELECT 1 FROM live_pool p WHERE p.pool_contact_id=e.recipient_id
				AND (e.recipient_organization_id=0 OR p.organization_id=e.recipient_organization_id)))) AS failed_count`, id)
	if err != nil {
		return out, workspaceQueryError("counting failed campaign recipients", err)
	}
	out.Failed = counts.Failed
	out.Waiting = max(0, out.Unsent-max(0, out.Failed-counts.Exhausted))
	if out.Planned > 0 {
		out.Progress = float64(out.Sent) * 100 / float64(out.Planned)
	}
	switch {
	case counts.Private > 0 && counts.Pool > 0:
		out.Source = "mixed"
	case counts.Pool > 0:
		out.Source = "pool"
	default:
		out.Source = "private"
	}
	if counts.Pool > 0 {
		err = c.db.Select(&out.Organizations, `WITH failure_keys AS (
			SELECT DISTINCT e.recipient_id,e.recipient_organization_id FROM campaign_send_errors e
			JOIN campaigns c ON c.id=e.campaign_id
			WHERE e.campaign_id=$1 AND e.recipient_type='pool'
			AND e.campaign_owner_user_id IS NOT DISTINCT FROM c.owner_user_id
			AND e.campaign_organization_id IS NOT DISTINCT FROM c.organization_id
		)
		SELECT cpr.organization_id::INT AS organization_id,COALESCE(o.name,'') AS name,
			COUNT(*)::INT AS planned,
			COUNT(*) FILTER(WHERE cpr.status='sent')::INT AS sent,
			COUNT(*) FILTER(WHERE cpr.status IN ('pending','queued','deferred'))::INT AS unsent,
			COUNT(*) FILTER(WHERE cpr.status IN ('pending','queued','deferred','failed') AND EXISTS(
				SELECT 1 FROM failure_keys e WHERE e.recipient_id=cpr.pool_contact_id
				AND (e.recipient_organization_id=0 OR e.recipient_organization_id=cpr.organization_id)))::INT AS failed
		FROM campaign_pool_recipients cpr JOIN campaigns c ON c.id=cpr.campaign_id
		LEFT JOIN organizations o ON o.id=cpr.organization_id
		WHERE cpr.campaign_id=$1 AND (c.pool_scope='all_organizations' OR cpr.organization_id IS NOT DISTINCT FROM c.organization_id)
		GROUP BY cpr.organization_id,o.name ORDER BY o.name,cpr.organization_id`, id)
		if err != nil {
			return out, workspaceQueryError("fetching pool delivery overview", err)
		}
	}
	return out, nil
}
