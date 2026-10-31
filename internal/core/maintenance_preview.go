package core

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// MaintenancePreview counts primary records, using the same predicates and
// platform-wide scope as the maintenance deletes. Related cascade deletions
// are deliberately not added to the primary-record count.
type MaintenancePreview struct {
	Count  int `json:"count"`
	Views  int `json:"views"`
	Clicks int `json:"clicks"`
}

func (c *Core) PreviewMaintenance(dataset, typ string, before time.Time) (MaintenancePreview, error) {
	var out MaintenancePreview
	var query string
	var args []any
	switch dataset + "/" + typ {
	case "customers/orphan":
		query = `SELECT COUNT(*) FROM customers a WHERE NOT EXISTS
			(SELECT 1 FROM customer_list_memberships b WHERE b.customer_id = a.id)`
	case "customers/blocklisted":
		query = `SELECT COUNT(*) FROM customers WHERE status = 'blocklisted'`
	case "subscriptions/unconfirmed":
		query = `SELECT COUNT(*) FROM customer_list_memberships
			WHERE status = 'unconfirmed' AND customer_list_id IN
			(SELECT id FROM customer_lists WHERE optin = 'double') AND created_at < $1`
		args = []any{before}
	case "analytics/all", "analytics/views", "analytics/clicks":
		if err := c.db.Get(&out, `SELECT
			CASE WHEN $2 IN ('all', 'views') THEN (SELECT COUNT(*) FROM campaign_views WHERE created_at < $1) ELSE 0 END AS views,
			CASE WHEN $2 IN ('all', 'clicks') THEN (SELECT COUNT(*) FROM link_clicks WHERE created_at < $1) ELSE 0 END AS clicks`, before, typ); err != nil {
			return out, c.organizationDBErr("counting maintenance analytics", err)
		}
		out.Count = out.Views + out.Clicks
		return out, nil
	default:
		return out, echo.NewHTTPError(http.StatusBadRequest, "invalid maintenance dataset")
	}
	if err := c.db.Get(&out.Count, query, args...); err != nil {
		return out, c.organizationDBErr("counting maintenance records", err)
	}
	return out, nil
}
