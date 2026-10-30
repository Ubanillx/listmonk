package main

import (
	"net/http"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func (a *App) GetCampaignUnsubscriptions(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if _, err := a.requireSensitiveWorkspaceCampaign(c, access, getID(c)); err != nil {
		return err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsRecipients); err != nil {
		return err
	}
	f := models.CampaignUnsubscriptionFilters{Search: c.QueryParam("search"), Status: c.QueryParam("status"),
		IncludePrivate:   hasLegacyPermission(user, auth.PermCustomersGetAll, auth.PermCustomersGet) && requireAPIKeyScope(c, apiKeyScopeCustomersRead) == nil,
		IncludePool:      hasLegacyPermission(user, auth.PermPoolsGet) && requireAPIKeyScope(c, apiKeyScopeListsRead) == nil,
		SensitivePrivate: user.IsPlatformAdmin() || user.HasPerm(auth.PermCustomersSensitiveRead), SensitivePool: user.IsPlatformAdmin()}
	if !f.IncludePrivate && !f.IncludePool {
		return echo.NewHTTPError(http.StatusForbidden, "customer read permission required")
	}
	if len(f.Search) > 500 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid unsubscribe search")
	}
	pg := a.pg.NewFromURL(c.QueryParams())
	limit := min(max(pg.Limit, 1), 100)
	rows, total, err := a.core.GetCampaignUnsubscriptions(access, getID(c), f, (pg.Page-1)*limit, limit)
	if err != nil {
		return err
	}
	for i := range rows {
		if (rows[i].RecipientType == "private" && !f.SensitivePrivate) || (rows[i].RecipientType == "pool" && !f.SensitivePool) {
			rows[i].Email = maskEmail(rows[i].Email)
		}
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, okResp{models.PageResults{Results: rows, Total: total, Page: pg.Page, PerPage: limit}})
}
