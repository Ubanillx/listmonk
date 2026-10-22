package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func redactFailedRecipient(row *models.CampaignFailedRecipient, f models.CampaignSendErrorFilters) {
	if (row.RecipientType == "pool" && !f.SensitivePool) || (row.RecipientType == "private" && !f.SensitivePrivate) {
		row.Email = maskEmail(row.Email)
	}
	if (row.RecipientType == "pool" && !f.SensitivePool) || (row.RecipientType == "private" && !f.PrivateDiagnostics) {
		row.Error = ""
	}
}

func (a *App) GetCampaignDeliveryOverview(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	if err := a.requireCampaignAnalytics(c, access, getID(c)); err != nil {
		return err
	}
	out, err := a.core.GetCampaignDeliveryOverview(access, getID(c))
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCampaignFailedRecipients(c echo.Context) error {
	access, f, err := a.campaignSendErrorRequest(c)
	if err != nil {
		return err
	}
	pg := a.pg.NewFromURL(c.QueryParams())
	limit := min(max(pg.Limit, 1), 100)
	out, err := a.core.GetCampaignFailedRecipients(access, getID(c), f, (pg.Page-1)*limit, limit)
	if err != nil {
		return err
	}
	for i := range out.Results {
		redactFailedRecipient(&out.Results[i], f)
	}
	out.Page, out.PerPage = pg.Page, limit
	user := auth.GetUser(c)
	out.CanExport = hasLegacyPermission(user, auth.PermCustomersExport, auth.PermPoolsExport) &&
		canExportCampaignSendErrors(user, models.CampaignSendErrorReport{HasPrivate: out.HasPrivate, HasPool: out.HasPool}) &&
		(!out.HasPrivate || hasLegacyPermission(user, auth.PermCustomersExport)) &&
		(!out.HasPool || hasLegacyPermission(user, auth.PermPoolsExport))
	camp, err := a.core.GetWorkspaceCampaign(access, getID(c))
	if err != nil {
		return err
	}
	out.CanRetry = camp.Status == models.CampaignStatusPaused && hasLegacyPermission(user, auth.PermCampaignsSend) &&
		requireCampaignSendOwnership(user, camp) == nil && requireAPIKeyScope(c, apiKeyScopeCampaignsSend) == nil
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ExportCampaignFailedRecipients(c echo.Context) error {
	access, f, err := a.campaignSendErrorRequest(c)
	if err != nil {
		return err
	}
	report, err := a.core.GetCampaignFailedRecipients(access, getID(c), f, 0, 1)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if (report.HasPrivate && !hasLegacyPermission(user, auth.PermCustomersExport)) ||
		(report.HasPool && !hasLegacyPermission(user, auth.PermPoolsExport)) ||
		!hasLegacyPermission(user, auth.PermCustomersExport, auth.PermPoolsExport) {
		return echo.NewHTTPError(http.StatusForbidden, "customer export permission required")
	}
	f.IncludePrivate = f.IncludePrivate && hasLegacyPermission(user, auth.PermCustomersExport)
	f.IncludePool = f.IncludePool && hasLegacyPermission(user, auth.PermPoolsExport)
	book, err := a.newExportWorkbook(c)
	if err != nil {
		return err
	}
	defer book.file.Close()
	sheet, err := book.addSheet("campaigns.failedRecipients", []exportColumn{
		book.column("customers.customerCode", 24, ""), book.column("globals.fields.name", 24, ""), book.column("customers.email", 36, ""),
		book.column("campaigns.sendErrorReason", 32, ""), book.column("exports.sendStage", 24, ""),
		book.column("exports.smtpCode", 16, "number"), book.column("campaigns.sendErrorMessage", 70, ""),
		book.column("campaigns.sendErrors", 18, "number"), book.timeColumn("exports.lastError", 23),
		book.column("exports.recipientType", 24, ""), book.column("exports.recipientID", 18, ""),
		book.column("exports.organizationID", 18, ""), book.column("campaigns.failedRecipientStatus", 20, ""),
		book.column("campaigns.failedRecipientRetryable", 18, ""),
	})
	if err != nil {
		return err
	}
	err = a.core.StreamCampaignFailedRecipients(c.Request().Context(), access, getID(c), f, func(row models.CampaignFailedRecipient) error {
		redactFailedRecipient(&row, f)
		message := row.Error
		if message == "" {
			message = book.lang.T("campaigns.sendErrorMasked")
		}
		retryLabel := book.translated("campaigns.failedRecipientNo", "No")
		if row.Retryable {
			retryLabel = book.translated("campaigns.failedRecipientYes", "Yes")
		}
		return sheet.addRow(row.CustomerCode, row.Name, row.Email,
			book.translated("campaigns.sendErrorReasons."+row.Category, row.Category),
			book.translated("campaigns.sendErrorStages."+row.Stage, row.Stage), row.SMTPCode, message, row.Count, row.LastAt,
			book.translated("customer_lists.types."+row.RecipientType, row.RecipientType),
			strconv.FormatInt(row.RecipientID, 10), strconv.Itoa(row.OrganizationID),
			book.translated("campaigns.failedRecipientStatuses."+row.Status, row.Status),
			retryLabel)
	})
	if err != nil {
		return err
	}
	return book.download(c, fmt.Sprintf("campaign-%d-failed-recipients.xlsx", getID(c)))
}

func (a *App) RetryCampaignFailedRecipients(c echo.Context) error {
	access, f, err := a.campaignSendErrorRequest(c)
	if err != nil {
		return err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsSend); err != nil {
		return err
	}
	if err := requireAPIKeyScope(c, apiKeyScopeCampaignsSend); err != nil {
		return err
	}
	if err := requireAPIKeyScope(c, apiKeyScopeCampaignsRecipients); err != nil {
		return err
	}
	camp, err := a.core.GetWorkspaceCampaign(access, getID(c))
	if err != nil {
		return err
	}
	if err := requireCampaignSendOwnership(user, camp); err != nil {
		return err
	}
	if email.IsMessengerName(camp.Messenger) && camp.PoolScope != models.CampaignPoolScopeAllOrganizations {
		if err := a.requireCampaignSMTPAvailable(camp); err != nil {
			return err
		}
	}
	if err := a.validateCampaignReplyMailbox(access, &camp); err != nil {
		return err
	}
	if err := a.core.ValidatePoolCampaignAudience(getID(c)); err != nil {
		return err
	}
	var req struct {
		Recipients []models.CampaignRetryRecipient `json:"recipients"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	var count int
	err = a.manager.WithCampaignStatusChange(func() error {
		var changeErr error
		count, changeErr = a.core.RetryCampaignFailedRecipients(access, getID(c), f, req.Recipients)
		return changeErr
	})
	if err != nil {
		return err
	}
	setAuditObjectDetails(c, auditCampaignDetails(camp))
	setAuditMetadata(c, map[string]any{"recipient_count": count})
	return c.JSON(http.StatusOK, okResp{map[string]any{"campaign_id": getID(c), "selected": count, "status": models.CampaignStatusRunning}})
}
