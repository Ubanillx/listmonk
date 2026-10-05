package core

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx/types"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// GetDashboardCharts returns chart data points to render on the dashboard.
func (c *Core) GetDashboardCharts() (types.JSONText, error) {
	_ = c.refreshCache(matDashboardCharts, false)

	var out types.JSONText
	if err := c.q.GetDashboardCharts.Get(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "dashboard charts", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetDashboardCounts returns stats counts to show on the dashboard.
func (c *Core) GetDashboardCounts() (types.JSONText, error) {
	_ = c.refreshCache(matDashboardCounts, false)

	var out types.JSONText
	if err := c.q.GetDashboardCounts.Get(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "dashboard stats", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetWorkspaceDashboardCharts returns chart data restricted to the resources
// the caller can view in the selected workspace. The materialized global view
// remains useful for platform administrators, while organization and personal
// workspaces must never receive its unfiltered data.
func (c *Core) GetWorkspaceDashboardCharts(access models.WorkspaceAccess) (types.JSONText, error) {
	if access.PlatformAdmin {
		return c.GetDashboardCharts()
	}

	scope, args := workspaceReadPredicate(access, "c", 1)
	stmt := fmt.Sprintf(`
		WITH visible_campaigns AS (
			SELECT c.id FROM campaigns c WHERE (%s)
		), click_dates AS (
			SELECT MAX(lc.created_at)::DATE AS to_date
			FROM link_clicks lc JOIN visible_campaigns c ON c.id = lc.campaign_id
		), view_dates AS (
			SELECT MAX(cv.created_at)::DATE AS to_date
			FROM campaign_views cv JOIN visible_campaigns c ON c.id = cv.campaign_id
		), clicks AS (
			SELECT COUNT(*)::INT AS count, lc.created_at::DATE AS date
			FROM link_clicks lc
			JOIN visible_campaigns c ON c.id = lc.campaign_id
			CROSS JOIN click_dates d
			WHERE d.to_date IS NOT NULL
				AND lc.created_at >= d.to_date - INTERVAL '30 days'
				AND lc.created_at < d.to_date + INTERVAL '1 day'
			GROUP BY lc.created_at::DATE
		), views AS (
			SELECT COUNT(*)::INT AS count, cv.created_at::DATE AS date
			FROM campaign_views cv
			JOIN visible_campaigns c ON c.id = cv.campaign_id
			CROSS JOIN view_dates d
			WHERE d.to_date IS NOT NULL
				AND cv.created_at >= d.to_date - INTERVAL '30 days'
				AND cv.created_at < d.to_date + INTERVAL '1 day'
			GROUP BY cv.created_at::DATE
		)
		SELECT JSON_BUILD_OBJECT(
			'link_clicks', COALESCE((
				SELECT JSON_AGG(JSON_BUILD_OBJECT('count', count, 'date', date) ORDER BY date)
				FROM clicks
			), '[]'::JSON),
			'campaign_views', COALESCE((
				SELECT JSON_AGG(JSON_BUILD_OBJECT('count', count, 'date', date) ORDER BY date)
				FROM views
			), '[]'::JSON)
		)`, scope)

	var out types.JSONText
	if err := c.db.Get(&out, stmt, args...); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "workspace dashboard charts", "error", pqErrMsg(err)))
	}
	return out, nil
}

// GetWorkspaceDashboardCounts returns aggregate counts for the active
// workspace. It deliberately uses the same ownership predicates as resource
// listing, so a member cannot infer another organization's data through the
// dashboard.
func (c *Core) GetWorkspaceDashboardCounts(access models.WorkspaceAccess) (types.JSONText, error) {
	if access.PlatformAdmin {
		out, err := c.GetDashboardCounts()
		if err != nil {
			return nil, err
		}
		return c.withDashboardCustomerCounts(out, access)
	}

	listScope, args := workspaceOwnerScopedReadPredicate(access, "l", 1)
	customerScope, _ := workspaceCustomerReadPredicate(access, "s", 1)
	campaignScope, _ := workspaceReadPredicate(access, "c", 1)
	stmt := fmt.Sprintf(`
		WITH visible_lists AS (
			SELECT l.id, l.type, l.optin FROM customer_lists l WHERE (%s)
		), visible_customers AS (
			SELECT s.id, s.status FROM customers s WHERE (%s)
		), visible_campaigns AS (
			SELECT c.id, c.status, c.sent FROM campaigns c WHERE (%s)
		), campaign_statuses AS (
			SELECT status, COUNT(*)::INT AS count FROM visible_campaigns GROUP BY status
		)
		SELECT JSON_BUILD_OBJECT(
			'customers', JSON_BUILD_OBJECT(
				'total', (SELECT COUNT(*) FROM visible_customers),
				'blocklisted', (SELECT COUNT(*) FROM visible_customers WHERE status = 'blocklisted'),
				'orphans', (SELECT COUNT(*) FROM visible_customers s
					WHERE NOT EXISTS (SELECT 1 FROM customer_list_memberships sl WHERE sl.customer_id = s.id))
			),
			'customer_lists', JSON_BUILD_OBJECT(
				'total', (SELECT COUNT(*) FROM visible_lists),
				'public', (SELECT COUNT(*) FROM visible_lists WHERE type = 'public'),
				'private', (SELECT COUNT(*) FROM visible_lists WHERE type = 'private'),
				'optin_single', (SELECT COUNT(*) FROM visible_lists WHERE optin = 'single'),
				'optin_double', (SELECT COUNT(*) FROM visible_lists WHERE optin = 'double')
			),
			'campaigns', JSON_BUILD_OBJECT(
				'total', (SELECT COUNT(*) FROM visible_campaigns),
				'by_status', COALESCE((SELECT JSON_OBJECT_AGG(status, count) FROM campaign_statuses), '{}'::JSON)
			),
			'messages', COALESCE((SELECT SUM(sent) FROM visible_campaigns), 0)
		)`, listScope, customerScope, campaignScope)

	var out types.JSONText
	if err := c.db.Get(&out, stmt, args...); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "workspace dashboard stats", "error", pqErrMsg(err)))
	}
	return c.withDashboardCustomerCounts(out, access)
}

type dashboardPoolCounts struct {
	Total   int `db:"total" json:"total"`
	Active  int `db:"active" json:"active"`
	Removed int `db:"removed" json:"removed"`
}

type dashboardPoolListCounts struct {
	Total         int `db:"total" json:"total"`
	Bound         int `db:"bound" json:"bound"`
	Unbound       int `db:"unbound" json:"unbound"`
	Allocations   int `db:"allocations" json:"allocations"`
	Organizations int `db:"organizations" json:"organizations"`
}

// Pool totals use the same pool-membership row unit as /api/pool-contacts.
// One contact in two first-level pools therefore counts twice. A removed
// allocation is still a row, but moves from active to removed for that scope.
func (c *Core) getWorkspacePoolDashboardCounts(access models.WorkspaceAccess) (dashboardPoolCounts, error) {
	var out dashboardPoolCounts
	if !access.PlatformAdmin && access.OrganizationID <= 0 {
		return out, nil
	}

	if access.PlatformAdmin {
		err := c.db.Get(&out, `SELECT COUNT(*) AS total,
			COUNT(*) FILTER (WHERE pool_exception.contact_id IS NULL) AS active,
			COUNT(*) FILTER (WHERE pool_exception.contact_id IS NOT NULL) AS removed
			FROM pool_contacts pc
			JOIN pool_members pm ON pm.contact_id=pc.id
			JOIN customer_lists cl ON cl.id=pm.pool_id AND cl.type='pool'`+allPoolExceptionJoin)
		return out, err
	}

	err := c.db.Get(&out, `SELECT COUNT(*) AS total,
		COUNT(*) FILTER (WHERE om.status='active' AND ex.contact_id IS NULL) AS active,
		COUNT(*) FILTER (WHERE om.status='removed' OR ex.contact_id IS NOT NULL) AS removed
		FROM pool_contacts pc
		JOIN pool_members pm ON pm.contact_id=pc.id
		JOIN customer_lists cl ON cl.id=pm.pool_id AND cl.type='pool'
		JOIN org_pool_allocations oa ON oa.pool_id=pm.pool_id AND oa.organization_id=$1
		JOIN org_pool_allocation_members om ON om.allocation_id=oa.id AND om.contact_id=pc.id
		LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=pm.pool_id
			AND ex.organization_id=oa.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL`, access.OrganizationID)
	return out, err
}

// getWorkspacePoolListDashboardCounts returns active first-level pool lists.
// The total keeps its first-level pool unit. Bindings count active allocation
// lists in active organizations and are restricted to the selected organization
// for non-administrators, even when another organization shares the same pool.
func (c *Core) getWorkspacePoolListDashboardCounts(access models.WorkspaceAccess) (dashboardPoolListCounts, error) {
	var out dashboardPoolListCounts
	if !access.PlatformAdmin && access.OrganizationID <= 0 {
		return out, nil
	}

	poolScope := ""
	allocationScope := ""
	args := []any{}
	if !access.PlatformAdmin {
		poolScope = ` AND (EXISTS (
			SELECT 1 FROM pool_organization_permissions p
			WHERE p.pool_id=l.id AND p.organization_id=$1
		) OR EXISTS (
			SELECT 1 FROM org_pool_allocations a
			WHERE a.pool_id=l.id AND a.organization_id=$1
		))`
		allocationScope = ` AND a.organization_id=$1`
		args = append(args, access.OrganizationID)
	}
	query := `WITH visible_pools AS (
		SELECT l.id FROM customer_lists l WHERE l.type='pool' AND l.status='active'` + poolScope + `
	), bindings AS (
		SELECT a.pool_id, a.organization_id
		FROM org_pool_allocations a
		JOIN visible_pools p ON p.id=a.pool_id
		JOIN customer_lists al ON al.id=a.list_id AND al.status='active'
		JOIN organizations o ON o.id=a.organization_id AND o.status='active'
		WHERE TRUE` + allocationScope + `
	)
	SELECT COUNT(*) AS total,
		COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM bindings b WHERE b.pool_id=p.id)) AS bound,
		COUNT(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM bindings b WHERE b.pool_id=p.id)) AS unbound,
		(SELECT COUNT(*) FROM bindings) AS allocations,
		(SELECT COUNT(DISTINCT organization_id) FROM bindings) AS organizations
	FROM visible_pools p`
	if err := c.db.Get(&out, query, args...); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Core) withDashboardCustomerCounts(out types.JSONText, access models.WorkspaceAccess) (types.JSONText, error) {
	poolCounts, err := c.getWorkspacePoolDashboardCounts(access)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "pool customers", "error", pqErrMsg(err)))
	}
	poolListCounts, err := c.getWorkspacePoolListDashboardCounts(access)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "pool lists", "error", pqErrMsg(err)))
	}
	var base map[string]any
	if err := json.Unmarshal(out, &base); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "dashboard stats", "error", err.Error()))
	}
	base["private_customers"] = base["customers"] // Keep customers for existing API clients.
	base["pool_customers"] = poolCounts
	base["pool_lists"] = poolListCounts
	merged, err := json.Marshal(base)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "dashboard stats", "error", err.Error()))
	}
	return types.JSONText(merged), nil
}
