package core

import (
	"fmt"
	"strings"

	"github.com/knadh/listmonk/models"
)

// Opt-out history has no PII snapshot. Current identities can be read only
// under the same owner/workspace and sensitive-field boundaries as recipients.
func (c *Core) GetCampaignUnsubscriptions(access models.WorkspaceAccess, id int, f models.CampaignUnsubscriptionFilters, offset, limit int) ([]models.CampaignUnsubscription, int, error) {
	if _, err := c.RequireManageResource(access, resourceCampaigns, id); err != nil {
		return nil, 0, err
	}
	scope, scopeArgs := workspaceSensitiveCustomerPredicate(access, "c", 9)
	args := []any{id, strings.TrimSpace(f.Search), f.IncludePrivate, f.IncludePool, access.OrganizationID, access.PlatformAdmin, f.SensitivePrivate, f.SensitivePool}
	args = append(args, scopeArgs...)
	query := fmt.Sprintf(`WITH visible AS (
		SELECT u.recipient_type,u.recipient_id,u.recipient_organization_id AS organization_id,u.created_at,
			COALESCE(cu.customer_code,pc.customer_code,'') AS customer_code,
			COALESCE(cu.name,pc.name,'') AS name,COALESCE(cu.email,pc.email,'') AS email
		FROM campaign_unsubscriptions u JOIN campaigns c ON c.id=u.campaign_id
		LEFT JOIN customers cu ON u.recipient_type='private' AND cu.id=u.recipient_id
		LEFT JOIN pool_contacts pc ON u.recipient_type='pool' AND pc.id=u.recipient_id
		WHERE c.id=$1 AND (%s) AND c.transfer_pending_at IS NULL
		AND (c.organization_id IS NULL OR EXISTS(SELECT 1 FROM organizations o WHERE o.id=c.organization_id AND o.status='active'))
		AND ($6::BOOL OR (u.campaign_owner_user_id IS NOT DISTINCT FROM c.owner_user_id
			AND u.campaign_organization_id IS NOT DISTINCT FROM c.organization_id))
		AND ((u.recipient_type='private' AND $3::BOOL AND ($6::BOOL OR cu.id IS NULL
			OR (cu.owner_user_id=c.owner_user_id AND cu.organization_id IS NOT DISTINCT FROM c.organization_id AND cu.transfer_pending_at IS NULL)))
			OR (u.recipient_type='pool' AND $4::BOOL AND ($6::BOOL OR u.recipient_organization_id=$5)))
	), filtered AS (
		SELECT * FROM visible WHERE $2::TEXT='' OR name ILIKE '%%'||$2||'%%' OR customer_code ILIKE '%%'||$2||'%%'
		OR (((recipient_type='private' AND $7::BOOL) OR (recipient_type='pool' AND $8::BOOL)) AND email ILIKE '%%'||$2||'%%')
	) `, scope)
	var total int
	if err := c.db.Get(&total, query+`SELECT COUNT(*)::INT FROM filtered`, args...); err != nil {
		return nil, 0, workspaceQueryError("counting campaign unsubscriptions", err)
	}
	args = append(args, max(offset, 0), min(max(limit, 1), 100))
	rows := []models.CampaignUnsubscription{}
	if err := c.db.Select(&rows, query+fmt.Sprintf(`SELECT * FROM filtered ORDER BY created_at DESC,recipient_type,recipient_id OFFSET $%d LIMIT $%d`, len(args)-1, len(args)), args...); err != nil {
		return nil, 0, workspaceQueryError("reading campaign unsubscriptions", err)
	}
	return rows, total, nil
}
