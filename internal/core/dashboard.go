package core

import (
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
		return c.GetDashboardCounts()
	}

	listScope, args := workspaceReadPredicate(access, "l", 1)
	subscriberScope, _ := workspaceSubscriberReadPredicate(access, "s", 1)
	campaignScope, _ := workspaceReadPredicate(access, "c", 1)
	stmt := fmt.Sprintf(`
		WITH visible_lists AS (
			SELECT l.id, l.type, l.optin FROM lists l WHERE (%s)
		), visible_subscribers AS (
			SELECT s.id, s.status FROM subscribers s WHERE (%s)
		), visible_campaigns AS (
			SELECT c.id, c.status, c.sent FROM campaigns c WHERE (%s)
		), campaign_statuses AS (
			SELECT status, COUNT(*)::INT AS count FROM visible_campaigns GROUP BY status
		)
		SELECT JSON_BUILD_OBJECT(
			'subscribers', JSON_BUILD_OBJECT(
				'total', (SELECT COUNT(*) FROM visible_subscribers),
				'blocklisted', (SELECT COUNT(*) FROM visible_subscribers WHERE status = 'blocklisted'),
				'orphans', (SELECT COUNT(*) FROM visible_subscribers s
					WHERE NOT EXISTS (SELECT 1 FROM subscriber_lists sl WHERE sl.subscriber_id = s.id))
			),
			'lists', JSON_BUILD_OBJECT(
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
		)`, listScope, subscriberScope, campaignScope)

	var out types.JSONText
	if err := c.db.Get(&out, stmt, args...); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "workspace dashboard stats", "error", pqErrMsg(err)))
	}
	return out, nil
}
