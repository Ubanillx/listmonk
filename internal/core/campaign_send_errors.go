package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/knadh/listmonk/models"
)

// Identity snapshots remain bound to the original campaign owner/workspace.
// A shared/manager campaign read does not grant recipient-detail access; moved
// private customers are excluded even if a historical snapshot still exists.
func campaignSendErrorCTE(access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters) (string, []any) {
	scope, scopeArgs := workspaceSensitiveCustomerPredicate(access, "c", 10)
	args := []any{id, strings.TrimSpace(f.Search), f.Category, f.IncludePrivate, f.IncludePool,
		access.OrganizationID, access.PlatformAdmin, f.SensitivePrivate, f.SensitivePool}
	args = append(args, scopeArgs...)
	sql := fmt.Sprintf(`WITH scoped_campaign AS (
		SELECT c.* FROM campaigns c WHERE c.id=$1 AND (%s) AND c.transfer_pending_at IS NULL
		AND (c.organization_id IS NULL OR EXISTS(SELECT 1 FROM organizations o WHERE o.id=c.organization_id AND o.status='active'))
	), visible AS (
		SELECT e.recipient_type,e.recipient_id,e.recipient_organization_id,e.customer_code,e.name,e.email,
			e.stage,e.category,e.smtp_code,e.created_at,
			CASE WHEN (e.recipient_type='private' AND $8::BOOL) OR (e.recipient_type='pool' AND $9::BOOL) THEN e.error ELSE '' END AS error
		FROM campaign_send_errors e JOIN scoped_campaign c ON c.id=e.campaign_id
		LEFT JOIN customers cu ON e.recipient_type='private' AND cu.id=e.recipient_id
		WHERE ($7::BOOL OR (e.campaign_owner_user_id IS NOT DISTINCT FROM c.owner_user_id
			AND e.campaign_organization_id IS NOT DISTINCT FROM c.organization_id))
		AND ((e.recipient_type='private' AND $4::BOOL AND ($7::BOOL OR cu.id IS NULL OR
			(cu.organization_id IS NOT DISTINCT FROM c.organization_id AND cu.owner_user_id=c.owner_user_id AND cu.transfer_pending_at IS NULL)))
			OR (e.recipient_type='pool' AND $5::BOOL AND ($7::BOOL OR e.recipient_organization_id=$6)))
	), filtered AS (
		SELECT * FROM visible WHERE ($3::TEXT='' OR category=$3)
		AND ($2::TEXT='' OR name ILIKE '%%'||$2||'%%' OR customer_code ILIKE '%%'||$2||'%%'
			OR (((recipient_type='private' AND $8::BOOL) OR (recipient_type='pool' AND $9::BOOL))
				AND (email ILIKE '%%'||$2||'%%' OR error ILIKE '%%'||$2||'%%')))
	), grouped AS (
		SELECT recipient_type,recipient_id,recipient_organization_id,customer_code,name,email,stage,category,smtp_code,error,
			COUNT(*)::INT AS count,MIN(created_at) AS first_at,MAX(created_at) AS last_at
		FROM filtered GROUP BY recipient_type,recipient_id,recipient_organization_id,customer_code,name,email,stage,category,smtp_code,error
	) `, scope)
	return sql, args
}

const campaignSendErrorOrder = ` ORDER BY last_at DESC,recipient_type,recipient_id,recipient_organization_id,customer_code,name,email,stage,category,smtp_code,error`

func (c *Core) GetCampaignSendErrors(access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters, offset, limit int) (models.CampaignSendErrorReport, error) {
	out := models.CampaignSendErrorReport{Results: []models.CampaignSendErrorRow{}, Reasons: []models.CampaignSendErrorReason{}}
	if _, err := c.RequireManageResource(access, resourceCampaigns, id); err != nil {
		return out, err
	}
	stmt, args := campaignSendErrorCTE(access, id, f)
	if err := c.db.Get(&out, stmt+`SELECT (SELECT COUNT(*)::INT FROM grouped) AS total,
		(SELECT COUNT(*)::INT FROM filtered) AS recorded_errors,
		GREATEST(COALESCE((SELECT send_errors FROM scoped_campaign),0)-(SELECT COUNT(*)::INT FROM campaign_send_errors WHERE campaign_id=$1),0) AS historical_errors,
		EXISTS(SELECT 1 FROM filtered WHERE recipient_type='private') AS has_private,
		EXISTS(SELECT 1 FROM filtered WHERE recipient_type='pool') AS has_pool`, args...); err != nil {
		return out, workspaceQueryError("fetching send error totals", err)
	}
	if err := c.db.Select(&out.Reasons, stmt+`SELECT category,COUNT(*)::INT AS count FROM filtered GROUP BY category ORDER BY count DESC,category`, args...); err != nil {
		return out, workspaceQueryError("fetching send error reasons", err)
	}
	pageArgs := append(append([]any{}, args...), max(offset, 0), min(max(limit, 1), 100))
	if err := c.db.Select(&out.Results, stmt+`SELECT * FROM grouped`+campaignSendErrorOrder+fmt.Sprintf(" OFFSET $%d LIMIT $%d", len(args)+1, len(args)+2), pageArgs...); err != nil {
		return out, workspaceQueryError("fetching send error recipients", err)
	}
	return out, nil
}

func (c *Core) StreamCampaignSendErrors(ctx context.Context, access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters, write func(models.CampaignSendErrorRow) error) error {
	if _, err := c.RequireManageResource(access, resourceCampaigns, id); err != nil {
		return err
	}
	stmt, args := campaignSendErrorCTE(access, id, f)
	rows, err := c.db.QueryxContext(ctx, stmt+`SELECT * FROM grouped`+campaignSendErrorOrder, args...)
	if err != nil {
		return workspaceQueryError("exporting send errors", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row models.CampaignSendErrorRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if err := write(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
