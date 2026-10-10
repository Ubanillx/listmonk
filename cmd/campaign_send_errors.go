package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func (a *App) campaignSendErrorRequest(c echo.Context) (models.WorkspaceAccess, models.CampaignSendErrorFilters, error) {
	access, err := a.workspaceAccess(c)
	f := models.CampaignSendErrorFilters{Search: c.QueryParam("search"), Category: c.QueryParam("category")}
	if err != nil {
		return access, f, err
	}
	if _, err := a.requireSensitiveWorkspaceCampaign(c, access, getID(c)); err != nil {
		return access, f, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsRecipients); err != nil {
		return access, f, err
	}
	f.IncludePrivate = hasLegacyPermission(user, auth.PermCustomersGetAll, auth.PermCustomersGet) && requireAPIKeyScope(c, apiKeyScopeCustomersRead) == nil
	f.IncludePool = hasLegacyPermission(user, auth.PermPoolsGet) && requireAPIKeyScope(c, apiKeyScopeListsRead) == nil
	f.SensitivePrivate = user.IsPlatformAdmin() || user.HasPerm(auth.PermCustomersSensitiveRead)
	f.SensitivePool = user.IsPlatformAdmin()
	if !f.IncludePrivate && !f.IncludePool {
		return access, f, echo.NewHTTPError(http.StatusForbidden, "customer read permission required")
	}
	if len(f.Search) > 500 || len(f.Category) > 50 {
		return access, f, echo.NewHTTPError(http.StatusBadRequest, "invalid error filter")
	}
	return access, f, nil
}

func redactCampaignSendErrorRow(row *models.CampaignSendErrorRow, f models.CampaignSendErrorFilters) {
	if (row.RecipientType == "pool" && !f.SensitivePool) || (row.RecipientType == "private" && !f.SensitivePrivate) {
		row.Email = maskEmail(row.Email)
		// Raw diagnostics may embed recipient addresses or account data.
		row.Error = ""
	}
}

func canExportCampaignSendErrors(user auth.User, r models.CampaignSendErrorReport) bool {
	return (!r.HasPrivate || hasLegacyPermission(user, auth.PermCustomersExport)) && (!r.HasPool || hasLegacyPermission(user, auth.PermPoolsExport))
}

func (a *App) GetCampaignSendErrors(c echo.Context) error {
	access, f, err := a.campaignSendErrorRequest(c)
	if err != nil {
		return err
	}
	pg := a.pg.NewFromURL(c.QueryParams())
	// Keep the returned page size aligned with the query's bound.
	limit := min(max(pg.Limit, 1), 100)
	out, err := a.core.GetCampaignSendErrors(access, getID(c), f, (pg.Page-1)*limit, limit)
	if err != nil {
		return err
	}
	for i := range out.Results {
		redactCampaignSendErrorRow(&out.Results[i], f)
	}
	out.Page = pg.Page
	out.PerPage = limit
	out.CanExport = hasLegacyPermission(auth.GetUser(c), auth.PermCustomersExport, auth.PermPoolsExport) && canExportCampaignSendErrors(auth.GetUser(c), out)
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ExportCampaignSendErrors(c echo.Context) error {
	access, f, err := a.campaignSendErrorRequest(c)
	if err != nil {
		return err
	}
	report, err := a.core.GetCampaignSendErrors(access, getID(c), f, 0, 1)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if !hasLegacyPermission(user, auth.PermCustomersExport, auth.PermPoolsExport) || !canExportCampaignSendErrors(user, report) {
		return echo.NewHTTPError(http.StatusForbidden, "customer export permission required")
	}
	// Intersect again for the streamed query in case new failures arrive after
	// the authorization probe; no newly appearing source can bypass its grant.
	f.IncludePrivate = f.IncludePrivate && hasLegacyPermission(user, auth.PermCustomersExport)
	f.IncludePool = f.IncludePool && hasLegacyPermission(user, auth.PermPoolsExport)
	book, err := a.newExportWorkbook(c)
	if err != nil {
		return err
	}
	defer book.file.Close()
	sheet, err := book.addSheet("campaigns.sendErrorDetails", []exportColumn{
		book.column("customers.customerCode", 24, ""), book.column("globals.fields.name", 24, ""), book.column("customers.email", 36, ""),
		book.column("campaigns.sendErrorReason", 32, ""), book.column("exports.sendStage", 24, ""), book.column("exports.smtpCode", 16, "number"),
		book.column("campaigns.sendErrorMessage", 70, ""), book.column("campaigns.sendErrors", 18, "number"),
		book.column("exports.firstErrorUTC", 23, "date"), book.column("exports.lastErrorUTC", 23, "date"),
		book.column("exports.recipientType", 24, ""), book.column("exports.recipientID", 18, ""),
		book.column("exports.organizationID", 18, ""), book.column("exports.campaignID", 18, ""), book.column("exports.reasonCode", 24, ""),
	})
	if err != nil {
		return err
	}
	reasonCounts := make(map[string]int)
	err = a.core.StreamCampaignSendErrors(c.Request().Context(), access, getID(c), f, func(row models.CampaignSendErrorRow) error {
		reasonCounts[row.Category] += row.Count
		redactCampaignSendErrorRow(&row, f)
		var smtpCode any
		if row.SMTPCode != 0 {
			smtpCode = row.SMTPCode
		}
		message := row.Error
		if message == "" && ((row.RecipientType == "pool" && !f.SensitivePool) || (row.RecipientType == "private" && !f.SensitivePrivate)) {
			message = book.lang.T("campaigns.sendErrorMasked")
		}
		return sheet.addRow(row.CustomerCode, row.Name, row.Email,
			book.translated("campaigns.sendErrorReasons."+row.Category, row.Category), book.translated("campaigns.sendErrorStages."+row.Stage, row.Stage),
			smtpCode, message, row.Count, row.FirstAt, row.LastAt, book.translated("customer_lists.types."+row.RecipientType, row.RecipientType),
			strconv.FormatInt(row.RecipientID, 10), strconv.Itoa(row.OrganizationID), strconv.Itoa(getID(c)), row.Category)
	})
	if err != nil {
		return err
	}
	summary, err := book.addSheet("campaigns.sendErrorReasonStats", []exportColumn{
		book.column("campaigns.sendErrorReason", 38, ""), book.column("campaigns.sendErrors", 20, "number"),
	})
	if err != nil {
		return err
	}
	for _, category := range []string{"smtp_auth", "smtp_rejected", "smtp_temporary", "timeout", "network", "smtp_unavailable", "reply_unavailable", "render", "other"} {
		if count := reasonCounts[category]; count > 0 {
			if err := summary.addRow(book.translated("campaigns.sendErrorReasons."+category, category), count); err != nil {
				return err
			}
		}
	}
	if report.HistoricalErrors > 0 {
		if err := summary.addRow(book.lang.T("campaigns.sendErrorHistoryLabel"), report.HistoricalErrors); err != nil {
			return err
		}
	}
	return book.download(c, fmt.Sprintf("campaign-%d-send-errors.xlsx", getID(c)))
}
