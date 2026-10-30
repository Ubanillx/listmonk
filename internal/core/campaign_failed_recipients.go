package core

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// failedRecipientCTE deliberately starts from the authorization-filtered event
// stream, then joins live recipient state. A later successful delivery removes
// a person from this current-failure view without deleting their audit history.
func failedRecipientCTE(access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters) (string, []any) {
	base, args := campaignSendErrorCTE(access, id, f)
	args = append(args, f.OrganizationID)
	return base + fmt.Sprintf(`, latest AS (
		SELECT DISTINCT ON (v.recipient_type,v.recipient_id) v.* FROM visible v JOIN scoped_campaign camp ON TRUE
		WHERE v.campaign_owner_user_id IS NOT DISTINCT FROM camp.owner_user_id
		AND v.campaign_organization_id IS NOT DISTINCT FROM camp.organization_id
		ORDER BY v.recipient_type,v.recipient_id,v.created_at DESC,v.id DESC
	), attempts AS (
		SELECT v.recipient_type,v.recipient_id,COUNT(*)::INT AS count FROM visible v JOIN scoped_campaign camp ON TRUE
		WHERE v.campaign_owner_user_id IS NOT DISTINCT FROM camp.owner_user_id
		AND v.campaign_organization_id IS NOT DISTINCT FROM camp.organization_id
		GROUP BY v.recipient_type,v.recipient_id
	), current_failed AS (
		SELECT e.recipient_type,e.recipient_id,COALESCE(cpr.organization_id,e.recipient_organization_id)::INT AS recipient_organization_id,e.customer_code,e.name,e.email,
			e.stage,e.category,e.smtp_code,e.error,a.count,e.created_at AS last_at,
			COALESCE(cr.status::TEXT,cpr.status::TEXT) AS status,
			CASE WHEN e.recipient_type='private' THEN
				cr.status IN ('pending','deferred') AND cu.status <> 'blocklisted'
				AND EXISTS(SELECT 1 FROM campaign_customer_lists cl
					JOIN customer_lists l ON l.id=cl.customer_list_id
					JOIN customer_list_memberships m ON m.customer_list_id=l.id AND m.customer_id=cu.id
					WHERE cl.campaign_id=$1 AND l.organization_id IS NOT DISTINCT FROM camp.organization_id
					AND l.owner_user_id=camp.owner_user_id AND l.transfer_pending_at IS NULL
					AND ((camp.type='optin' AND m.status='unconfirmed' AND l.optin='double')
					OR (camp.type<>'optin' AND ((l.optin='double' AND m.status='confirmed')
					OR (l.optin<>'double' AND m.status<>'unsubscribed')))))
			ELSE cpr.status IN ('pending','deferred') AND pc.status='active'
				AND NOT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions ex WHERE ex.pool_id=cpr.pool_id
					AND ex.organization_id=cpr.organization_id AND ex.contact_id=cpr.pool_contact_id AND ex.restored_at IS NULL)
				AND %s END AND NOT campaign_recipient_retry_exhausted($1,e.recipient_type,e.recipient_id) AS retryable
		FROM latest e JOIN attempts a USING(recipient_type,recipient_id)
		JOIN scoped_campaign camp ON TRUE
		LEFT JOIN campaign_recipients cr ON e.recipient_type='private' AND cr.campaign_id=$1 AND cr.customer_id=e.recipient_id
		LEFT JOIN customers cu ON cu.id=cr.customer_id AND cu.organization_id IS NOT DISTINCT FROM camp.organization_id
			AND cu.owner_user_id=camp.owner_user_id AND cu.transfer_pending_at IS NULL
		LEFT JOIN campaign_pool_recipients cpr ON e.recipient_type='pool' AND cpr.campaign_id=$1
			AND cpr.pool_contact_id=e.recipient_id
			AND (e.recipient_organization_id=0 OR cpr.organization_id=e.recipient_organization_id)
		LEFT JOIN pool_contacts pc ON pc.id=cpr.pool_contact_id
		WHERE (e.recipient_type='private' AND cr.status IN ('pending','queued','deferred','failed') AND cu.id IS NOT NULL)
			OR (e.recipient_type='pool' AND cpr.status IN ('pending','queued','deferred','failed') AND pc.id IS NOT NULL)
	), failed AS (
		SELECT * FROM current_failed WHERE ($3::TEXT='' OR category=$3)
		AND ($%d::INT=0 OR (recipient_type='pool' AND recipient_organization_id=$%d))
		AND ($2::TEXT='' OR name ILIKE '%%'||$2||'%%' OR customer_code ILIKE '%%'||$2||'%%'
			OR (((recipient_type='private' AND $8::BOOL) OR (recipient_type='pool' AND $9::BOOL)) AND email ILIKE '%%'||$2||'%%')
			OR (((recipient_type='private' AND $10::BOOL) OR (recipient_type='pool' AND $9::BOOL)) AND error ILIKE '%%'||$2||'%%'))
	) `, PoolRecipientAvailableSQL, len(args), len(args)), args
}

const failedRecipientOrder = ` ORDER BY last_at DESC,recipient_type,recipient_id`

func (c *Core) GetCampaignFailedRecipients(access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters, offset, limit int) (models.CampaignFailedRecipientReport, error) {
	out := models.CampaignFailedRecipientReport{Results: []models.CampaignFailedRecipient{}}
	if _, err := c.RequireManageResource(access, resourceCampaigns, id); err != nil {
		return out, err
	}
	stmt, args := failedRecipientCTE(access, id, f)
	var totals struct {
		Total      int  `db:"total"`
		HasPrivate bool `db:"has_private"`
		HasPool    bool `db:"has_pool"`
	}
	if err := c.db.Get(&totals, stmt+`SELECT COUNT(*)::INT AS total,
		COALESCE(BOOL_OR(recipient_type='private'),false) AS has_private,
		COALESCE(BOOL_OR(recipient_type='pool'),false) AS has_pool FROM failed`, args...); err != nil {
		return out, workspaceQueryError("counting failed recipients", err)
	}
	out.Total, out.HasPrivate, out.HasPool = totals.Total, totals.HasPrivate, totals.HasPool
	pageArgs := append(append([]any{}, args...), max(offset, 0), min(max(limit, 1), 100))
	if err := c.db.Select(&out.Results, stmt+`SELECT * FROM failed`+failedRecipientOrder+
		fmt.Sprintf(" OFFSET $%d LIMIT $%d", len(args)+1, len(args)+2), pageArgs...); err != nil {
		return out, workspaceQueryError("fetching failed recipients", err)
	}
	return out, nil
}

func (c *Core) StreamCampaignFailedRecipients(ctx context.Context, access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters, write func(models.CampaignFailedRecipient) error) error {
	if _, err := c.RequireManageResource(access, resourceCampaigns, id); err != nil {
		return err
	}
	stmt, args := failedRecipientCTE(access, id, f)
	rows, err := c.db.QueryxContext(ctx, stmt+`SELECT * FROM failed`+failedRecipientOrder, args...)
	if err != nil {
		return workspaceQueryError("exporting failed recipients", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row models.CampaignFailedRecipient
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if err := write(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

// RetryCampaignFailedRecipients atomically selects only current, deliverable
// failed recipients and starts a paused campaign. The normal sender remains
// responsible for every later quota, transport and lifecycle decision.
func (c *Core) RetryCampaignFailedRecipients(access models.WorkspaceAccess, id int, f models.CampaignSendErrorFilters, selected []models.CampaignRetryRecipient) (int, error) {
	if len(selected) == 0 || len(selected) > 500 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "select 1 to 500 failed recipients")
	}
	privateIDs, poolIDs := []int64{}, []int64{}
	seen := make(map[string]bool, len(selected))
	for _, item := range selected {
		if item.ID < 1 || (item.Type != "private" && item.Type != "pool") {
			return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid recipient selection")
		}
		key := fmt.Sprintf("%s:%d", item.Type, item.ID)
		if seen[key] {
			return 0, echo.NewHTTPError(http.StatusBadRequest, "duplicate recipient selection")
		}
		seen[key] = true
		if item.Type == "private" {
			privateIDs = append(privateIDs, item.ID)
		} else {
			poolIDs = append(poolIDs, item.ID)
		}
	}
	if (len(privateIDs) > 0 && !f.IncludePrivate) || (len(poolIDs) > 0 && !f.IncludePool) {
		return 0, echo.NewHTTPError(http.StatusForbidden, "recipient source permission required")
	}
	err := c.withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}, func(tx *sqlx.Tx) error {
		var status string
		if err := tx.Get(&status, `SELECT status FROM campaigns WHERE id=$1`, id); err != nil {
			return workspaceMutationError()
		}
		if status != models.CampaignStatusPaused {
			return echo.NewHTTPError(http.StatusConflict, "pause the campaign before retrying selected recipients")
		}
		f.Search, f.Category, f.OrganizationID = "", "", 0
		stmt, args := failedRecipientCTE(access, id, f)
		args = append(args, pq.Array(privateIDs), pq.Array(poolIDs))
		var rows []struct {
			Type      string `db:"recipient_type"`
			ID        int64  `db:"recipient_id"`
			Retryable bool   `db:"retryable"`
		}
		query := stmt + fmt.Sprintf(`SELECT recipient_type,recipient_id,retryable FROM failed
			WHERE (recipient_type='private' AND recipient_id=ANY($%d::BIGINT[]))
			OR (recipient_type='pool' AND recipient_id=ANY($%d::BIGINT[]))`, len(args)-1, len(args))
		if err := tx.Select(&rows, query, args...); err != nil {
			return workspaceQueryError("validating retry selection", err)
		}
		if len(rows) != len(selected) {
			return echo.NewHTTPError(http.StatusConflict, "selected failed recipients changed; refresh the list")
		}
		for _, row := range rows {
			if !row.Retryable {
				return echo.NewHTTPError(http.StatusConflict, "a selected recipient is no longer deliverable")
			}
		}
		if _, err := tx.Exec(`UPDATE campaign_recipients SET retry_selected=false WHERE campaign_id=$1 AND retry_selected`, id); err != nil {
			return workspaceQueryError("clearing prior retry selection", err)
		}
		if _, err := tx.Exec(`UPDATE campaign_pool_recipients SET retry_selected=false WHERE campaign_id=$1 AND retry_selected`, id); err != nil {
			return workspaceQueryError("clearing prior pool retry selection", err)
		}
		if _, err := tx.Exec(`UPDATE campaign_recipients SET retry_selected=true WHERE campaign_id=$1 AND customer_id=ANY($2::BIGINT[])`, id, pq.Array(privateIDs)); err != nil {
			return workspaceQueryError("selecting private retries", err)
		}
		if _, err := tx.Exec(`UPDATE campaign_pool_recipients SET retry_selected=true WHERE campaign_id=$1 AND pool_contact_id=ANY($2::BIGINT[])`, id, pq.Array(poolIDs)); err != nil {
			return workspaceQueryError("selecting pool retries", err)
		}
		res, err := tx.Exec(`UPDATE campaigns SET retry_mode=true,status='running',next_resume_at=NULL,updated_at=NOW() WHERE id=$1 AND status='paused'`, id)
		if err != nil {
			return workspaceQueryError("starting selected retries", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return workspaceMutationError()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(selected), nil
}
