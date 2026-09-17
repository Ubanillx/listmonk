package main

import (
	"bytes"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	// stdInputMaxLen is the maximum allowed length for a standard input field.
	stdInputMaxLen = 2000

	// URIs.
	uriAdmin = "/admin"
)

type okResp struct {
	Data any `json:"data"`
}

var (
	reUUID = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
)

// registerHandlers registers HTTP handlers.
func initHTTPHandlers(e *echo.Echo, a *App) {
	// Bound request bodies for every route, including the ones registered
	// outside this function. Without this, Echo accepts bodies of any size and
	// handlers buffer them in memory or spill them to disk.
	e.Use(bodyLimitMiddleware())

	// Default error handler.
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		// Generic, non-echo error. Log it.
		if _, ok := err.(*echo.HTTPError); !ok {
			a.log.Println(err.Error())
		}
		e.DefaultHTTPErrorHandler(err, c)
	}

	// Configure CORS middleware if domains are configured.
	if len(a.cfg.Security.CorsOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: a.cfg.Security.CorsOrigins,
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, workspaceHeader},
		}))
	}

	// =================================================================
	// Authenticated non /api handlers.
	{
		// Attach a middleware to the group that checks for auth.
		g := e.Group("", a.auth.Middleware, func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				u := c.Get(auth.UserHTTPCtxKey)

				// On no-auth, redirect to login page
				if _, ok := u.(*echo.HTTPError); ok {
					u, _ := url.Parse(a.urlCfg.LoginURL)
					q := url.Values{}
					q.Set("next", c.Request().RequestURI)
					u.RawQuery = q.Encode()
					return c.Redirect(http.StatusTemporaryRedirect, u.String())
				}

				return next(c)
			}
		})

		// Authenticated endpoints.
		g.GET(path.Join(uriAdmin, ""), a.AdminPage)
		g.GET(path.Join(uriAdmin, "/select-workspace"), a.SelectWorkspacePage)
		g.GET(path.Join(uriAdmin, "/custom.css"), serveCustomAppearance("admin.custom_css"))
		g.GET(path.Join(uriAdmin, "/custom.js"), serveCustomAppearance("admin.custom_js"))
		g.GET(path.Join(uriAdmin, "/*"), a.AdminPage)
	}

	// =================================================================
	// Authenticated /api/* handlers.
	{
		var (
			// Permission check middleware.
			pm = a.auth.Perm

			// Attach a middleware to the group that checks for auth.
			g = e.Group("", a.auth.Middleware, rejectUnsupportedPersonalAPIKey, func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					u := c.Get(auth.UserHTTPCtxKey)

					// On no-auth, respond with a JSON error.
					if err, ok := u.(*echo.HTTPError); ok {
						return err
					}

					return next(c)
				}
			})
		)
		g.Use(a.auditMiddleware)

		// API endpoints.
		g.GET("/api/health", a.HealthCheck)
		g.GET("/api/config", a.GetServerConfig)
		g.GET("/api/lang/:lang", a.GetI18nLang)
		g.GET("/api/dashboard/charts", a.GetDashboardCharts)
		g.GET("/api/dashboard/counts", a.GetDashboardCounts)

		g.GET("/api/settings", pm(a.GetSettings, "settings:get"))
		g.PUT("/api/settings", pm(a.UpdateSettings, "settings:manage"))
		g.PUT("/api/settings/:key", pm(a.UpdateSettingsByKey, "settings:manage"))
		g.POST("/api/settings/smtp/test", pm(a.TestSMTPSettings, "settings:manage"))
		g.POST("/api/settings/bounce/mailbox/test", pm(a.TestBounceMailbox, "settings:manage"))
		// Reply-AI gateway probing: model discovery and a full model test
		// against the unsaved settings form. Both never persist credentials.
		g.POST("/api/settings/reply-ai/models", pm(a.ListReplyAIModels, "settings:manage"))
		g.POST("/api/settings/reply-ai/test", pm(a.TestReplyAIModel, "settings:manage"))
		g.POST("/api/admin/reload", pm(a.ReloadApp, "settings:manage"))
		g.GET("/api/logs", pm(a.GetLogs, "settings:get"))
		g.GET("/api/events", pm(a.EventStream, "settings:get"))
		g.GET("/api/audit-events", pm(a.GetAuditEvents, auth.PermAuditGet))
		g.GET("/api/audit-events/export", pm(a.ExportAuditEvents, auth.PermAuditGet))
		g.GET("/api/audit-events/:id", pm(a.GetAuditEvent, auth.PermAuditGet))
		g.GET("/api/about", a.GetAboutInfo)
		g.GET("/api/custom-fields", a.GetCustomFields)
		g.POST("/api/custom-fields", a.CreateCustomField)
		g.PUT("/api/custom-fields/:key", a.UpdateCustomField)
		g.DELETE("/api/custom-fields/:key", a.DeleteCustomField)

		// Workspace-scoped customer handlers enforce owner and organization
		// boundaries themselves. Global role middleware here would reject a
		// member before it can access the customers it owns.
		g.GET("/api/customers", apiKeyScope(a.QueryCustomers, apiKeyScopeCustomersRead))
		g.GET("/api/customers/:id", apiKeyScope(hasID(a.GetCustomer), apiKeyScopeCustomersRead))
		g.GET("/api/customers/:id/activity", apiKeyScope(hasID(a.GetCustomerActivity), apiKeyScopeCustomersRead))
		g.GET("/api/customers/:id/export", apiKeyScope(hasID(a.ExportCustomerData), apiKeyScopeCustomersRead))
		g.GET("/api/customers/:id/bounces", apiKeyScope(hasID(a.GetCustomerBounces), apiKeyScopeCustomersRead))
		g.DELETE("/api/customers/:id/bounces", apiKeyScope(hasID(a.DeleteCustomerBounces), apiKeyScopeCustomersWrite))
		g.POST("/api/customers", apiKeyScope(a.CreateCustomer, apiKeyScopeCustomersWrite))
		g.PUT("/api/customers/:id", apiKeyScope(hasID(a.UpdateCustomer), apiKeyScopeCustomersWrite))
		g.POST("/api/customers/:id/optin", apiKeyScope(hasID(a.CustomerSendOptin), apiKeyScopeCustomersWrite))
		g.PUT("/api/customers/blocklist", apiKeyScope(a.BlocklistCustomers, apiKeyScopeCustomersWrite))
		g.PUT("/api/customers/:id/blocklist", apiKeyScope(hasID(a.BlocklistCustomer), apiKeyScopeCustomersWrite))
		g.PUT("/api/customers/customer-lists/:id", apiKeyScope(a.ManageCustomerListMemberships, apiKeyScopeCustomersWrite))
		g.PUT("/api/customers/customer-lists", apiKeyScope(a.ManageCustomerListMemberships, apiKeyScopeCustomersWrite))
		g.DELETE("/api/customers/:id", apiKeyScope(hasID(a.DeleteCustomer), apiKeyScopeCustomersWrite))
		g.DELETE("/api/customers", apiKeyScope(a.DeleteCustomers, apiKeyScopeCustomersWrite))

		g.GET("/api/bounces", apiKeyScope(a.GetBounces, apiKeyScopeBouncesRead))
		g.PUT("/api/bounces/blocklist", apiKeyScope(a.BlocklistBouncedCustomers, apiKeyScopeBouncesWrite))
		g.GET("/api/bounces/:id", apiKeyScope(hasID(a.GetBounce), apiKeyScopeBouncesRead))
		g.DELETE("/api/bounces", apiKeyScope(a.DeleteBounces, apiKeyScopeBouncesWrite))
		g.DELETE("/api/bounces/:id", apiKeyScope(hasID(a.DeleteBounce), apiKeyScopeBouncesWrite))

		// Customer operations based on arbitrary SQL queries.
		// These aren't very REST-like.
		g.POST("/api/customers/query/delete", a.DeleteCustomersByQuery)
		g.PUT("/api/customers/query/blocklist", a.BlocklistCustomersByQuery)
		g.PUT("/api/customers/query/customer-lists", a.ManageCustomerListMembershipsByQuery)
		g.GET("/api/customers/export",
			middleware.GzipWithConfig(middleware.GzipConfig{Level: 9})(a.ExportCustomers))

		g.GET("/api/import/customers", apiKeyScope(a.GetImportCustomers, apiKeyScopeCustomersImport))
		g.GET("/api/import/customers/logs", apiKeyScope(a.GetImportCustomerStats, apiKeyScopeCustomersImport))
		g.POST("/api/import/customers", apiKeyScope(a.ImportCustomers, apiKeyScopeCustomersImport))
		g.DELETE("/api/import/customers", apiKeyScope(a.StopImportCustomers, apiKeyScopeCustomersImport))

		// CustomerList handlers enforce the active workspace and owner boundary directly.
		g.GET("/api/customer-lists", apiKeyScope(a.GetLists, apiKeyScopeListsRead))
		g.GET("/api/customer-lists/:id", apiKeyScope(hasID(a.GetList), apiKeyScopeListsRead))
		g.POST("/api/customer-lists", apiKeyScope(a.CreateList, apiKeyScopeListsWrite))
		g.PUT("/api/customer-lists/:id", apiKeyScope(hasID(a.UpdateList), apiKeyScopeListsWrite))
		g.DELETE("/api/customer-lists", apiKeyScope(a.DeleteLists, apiKeyScopeListsWrite))
		g.DELETE("/api/customer-lists/:id", apiKeyScope(hasID(a.DeleteList), apiKeyScopeListsWrite))
		g.GET("/api/customer-lists/:id/pool-contacts", apiKeyScope(hasID(a.GetPoolContacts), apiKeyScopeListsRead))
		g.GET("/api/customer-lists/:id/pool-contacts/export", apiKeyScope(hasID(a.ExportPoolContacts), apiKeyScopeListsRead))
		g.GET("/api/customer-lists/:id/org-pool-allocations", apiKeyScope(hasID(a.GetOrgPoolAllocations), apiKeyScopeListsRead))
		g.POST("/api/customer-lists/:id/pool-contacts", apiKeyScope(hasID(a.CreatePoolContact), apiKeyScopeListsWrite))
		g.DELETE("/api/customer-lists/:id/pool-contacts/:contact_id/email", apiKeyScope(a.ClearPoolContactEmail, apiKeyScopeListsWrite))
		g.POST("/api/org-pool-allocations", apiKeyScope(a.CreateOrgPoolAllocation, apiKeyScopeListsWrite))
		g.POST("/api/org-pool-allocations/members", apiKeyScope(a.AssignPoolContact, apiKeyScopeListsWrite))
		g.POST("/api/org-pool-allocations/:id/import-members", apiKeyScope(a.ImportOrgPoolAllocationMembers, apiKeyScopeListsWrite))
		g.DELETE("/api/org-pool-allocations/members", apiKeyScope(a.RemovePoolContact, apiKeyScopeListsWrite))
		g.PUT("/api/org-pool-allocations/members", apiKeyScope(a.RestorePoolContact, apiKeyScopeListsWrite))
		// Explicit pool aliases keep the public-pool API independent from legacy
		// customer-list endpoints while retaining backwards-compatible routing.
		g.GET("/api/pools/:id/contacts", apiKeyScope(hasID(a.GetPoolContacts), apiKeyScopeListsRead))
		g.GET("/api/pools/:id/contacts/export", apiKeyScope(hasID(a.ExportPoolContacts), apiKeyScopeListsRead))
		g.GET("/api/pools/:id/allocations", apiKeyScope(hasID(a.GetOrgPoolAllocations), apiKeyScopeListsRead))
		g.GET("/api/pools/:id/management-target", apiKeyScope(hasID(a.GetPoolManagementTarget), apiKeyScopeListsRead))
		g.GET("/api/pools/:id/import-conflicts", apiKeyScope(hasID(a.GetPoolImportConflicts), apiKeyScopeListsRead))
		g.POST("/api/pools/permissions", apiKeyScope(a.GrantPoolOrganization, apiKeyScopeListsWrite))
		g.DELETE("/api/pools/permissions", apiKeyScope(a.RevokePoolOrganization, apiKeyScopeListsWrite))
		g.POST("/api/pools/:id/contacts", apiKeyScope(hasID(a.CreatePoolContact), apiKeyScopeListsWrite))
		g.DELETE("/api/pools/:id/contacts/:contact_id/email", apiKeyScope(a.ClearPoolContactEmail, apiKeyScopeListsWrite))
		g.POST("/api/pools/allocations", apiKeyScope(a.CreateOrgPoolAllocation, apiKeyScopeListsWrite))
		g.POST("/api/pools/allocations/members", apiKeyScope(a.AssignPoolContact, apiKeyScopeListsWrite))
		g.POST("/api/pools/allocations/:id/import-members", apiKeyScope(a.ImportOrgPoolAllocationMembers, apiKeyScopeListsWrite))
		g.DELETE("/api/pools/allocations/members", apiKeyScope(a.RemovePoolContact, apiKeyScopeListsWrite))
		g.PUT("/api/pools/allocations/members", apiKeyScope(a.RestorePoolContact, apiKeyScopeListsWrite))
		g.POST("/api/pools/import", apiKeyScope(a.ImportListIntoPool, apiKeyScopeListsWrite))

		// Read access is resolved in handlers using workspace scope. Do not put
		// legacy global permissions in front of these routes: a logged-in user
		// must be able to view a campaign published globally.
		g.GET("/api/campaigns", apiKeyScope(a.GetCampaigns, apiKeyScopeCampaignsRead))
		g.GET("/api/campaigns/running/stats", apiKeyScope(a.GetRunningCampaignStats, apiKeyScopeCampaignsRead))
		g.GET("/api/campaigns/report/summary", apiKeyScope(a.GetCampaignsReportSummary, apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/report/timeseries", apiKeyScope(a.GetCampaignsReportSeries, apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/report/links", apiKeyScope(a.GetCampaignsReportLinks, apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/report/recipients", apiKeyScope(a.GetCampaignsReportRecipients, apiKeyScopeCampaignsRecipients))
		g.GET("/api/campaigns/:id", apiKeyScope(hasID(a.GetCampaign), apiKeyScopeCampaignsRead))
		g.GET("/api/campaigns/analytics/:type", apiKeyScope(a.GetCampaignViewAnalytics, apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/:id/report/summary", apiKeyScope(hasID(a.GetCampaignReportSummary), apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/:id/report/timeseries", apiKeyScope(hasID(a.GetCampaignReportSeries), apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/:id/report/links", apiKeyScope(hasID(a.GetCampaignReportLinks), apiKeyScopeCampaignsAnalytics))
		g.GET("/api/campaigns/:id/report/recipients", apiKeyScope(hasID(a.GetCampaignReportRecipients), apiKeyScopeCampaignsRecipients))
		g.GET("/api/campaigns/:id/preview", apiKeyScope(hasID(a.PreviewCampaign), apiKeyScopeCampaignsRead))
		g.POST("/api/campaigns/:id/preview/archive", apiKeyScope(hasID(a.PreviewCampaignArchive), apiKeyScopeCampaignsRead))
		g.POST("/api/campaigns/:id/preview", apiKeyScope(hasID(a.PreviewCampaign), apiKeyScopeCampaignsRead))
		g.POST("/api/campaigns/:id/content", apiKeyScope(hasID(a.CampaignContent), apiKeyScopeCampaignsRead))
		g.POST("/api/campaigns/:id/text", apiKeyScope(hasID(a.PreviewCampaign), apiKeyScopeCampaignsRead))
		g.POST("/api/campaigns/:id/test", apiKeyScope(hasID(a.TestCampaign), apiKeyScopeCampaignsSend))
		g.POST("/api/campaigns", apiKeyScope(a.CreateCampaign, apiKeyScopeCampaignsWrite))
		g.POST("/api/campaigns/:id/clone", apiKeyScope(hasID(a.CloneCampaign), apiKeyScopeCampaignsWrite))
		g.PUT("/api/campaigns/:id", apiKeyScope(hasID(a.UpdateCampaign), apiKeyScopeCampaignsWrite))
		g.PUT("/api/campaigns/:id/status", apiKeyScope(hasID(a.UpdateCampaignStatus), apiKeyScopeCampaignsWrite))
		g.PUT("/api/campaigns/:id/archive", apiKeyScope(hasID(a.UpdateCampaignArchive), apiKeyScopeCampaignsWrite))
		g.POST("/api/campaigns/:id/pools", apiKeyScope(hasID(a.AttachCampaignPool), apiKeyScopeCampaignsWrite))
		g.GET("/api/campaigns/:id/pool-send-status", apiKeyScope(hasID(a.GetCampaignPoolSendStatus), apiKeyScopeCampaignsRead))
		g.DELETE("/api/campaigns", apiKeyScope(a.DeleteCampaigns, apiKeyScopeCampaignsWrite))
		g.DELETE("/api/campaigns/:id", apiKeyScope(hasID(a.DeleteCampaign), apiKeyScopeCampaignsWrite))

		g.GET("/api/media/folders", apiKeyScope(a.GetMediaFolders, apiKeyScopeMediaRead))
		g.POST("/api/media/folders", apiKeyScope(a.CreateMediaFolder, apiKeyScopeMediaWrite))
		g.PUT("/api/media/folders/:id", apiKeyScope(hasID(a.RenameMediaFolder), apiKeyScopeMediaWrite))
		g.PUT("/api/media/folders/:id/move", apiKeyScope(hasID(a.MoveMediaFolder), apiKeyScopeMediaWrite))
		g.DELETE("/api/media/folders/:id", apiKeyScope(hasID(a.DeleteMediaFolder), apiKeyScopeMediaWrite))
		g.PUT("/api/media/:id/folder", apiKeyScope(hasID(a.MoveMediaToFolder), apiKeyScopeMediaWrite))
		g.GET("/api/media", apiKeyScope(a.GetAllMedia, apiKeyScopeMediaRead))
		g.GET("/api/media/:id", apiKeyScope(hasID(a.GetMedia), apiKeyScopeMediaRead))
		g.POST("/api/media", apiKeyScope(a.UploadMedia, apiKeyScopeMediaWrite))
		g.DELETE("/api/media/:id", apiKeyScope(hasID(a.DeleteMedia), apiKeyScopeMediaWrite))

		// Global templates are intentionally usable and copyable by every
		// authenticated user. The handlers still enforce scope for stored data.
		g.GET("/api/templates", apiKeyScope(a.GetTemplates, apiKeyScopeTemplatesRead))
		g.GET("/api/templates/:id", apiKeyScope(hasID(a.GetTemplate), apiKeyScopeTemplatesRead))
		g.GET("/api/templates/:id/preview", apiKeyScope(hasID(a.PreviewTemplate), apiKeyScopeTemplatesRead))
		g.POST("/api/templates/preview", apiKeyScope(a.PreviewTemplateBody, apiKeyScopeTemplatesRead))
		g.POST("/api/templates", apiKeyScope(a.CreateTemplate, apiKeyScopeTemplatesWrite))
		g.POST("/api/templates/:id/clone", apiKeyScope(hasID(a.CloneTemplate), apiKeyScopeTemplatesWrite))
		// Every authenticated user may publish and maintain templates they own.
		// The handlers enforce the workspace/owner boundary directly; legacy
		// template roles must not prevent the documented global-template flow.
		g.PUT("/api/templates/:id", apiKeyScope(hasID(a.UpdateTemplate), apiKeyScopeTemplatesWrite))
		g.PUT("/api/templates/:id/default", apiKeyScope(hasID(a.TemplateSetDefault), apiKeyScopeTemplatesWrite))
		g.DELETE("/api/templates/:id", apiKeyScope(hasID(a.DeleteTemplate), apiKeyScopeTemplatesWrite))

		g.DELETE("/api/maintenance/customers/:type", pm(a.GCCustomers, "settings:maintain"))
		g.DELETE("/api/maintenance/analytics/:type", pm(a.GCCampaignAnalytics, "settings:maintain"))
		g.DELETE("/api/maintenance/subscriptions/unconfirmed", pm(a.GCSubscriptions, "settings:maintain"))

		g.POST("/api/tx", apiKeyScope(a.SendTxMessage, apiKeyScopeTransactionalSend))

		g.GET("/api/profile", a.GetUserProfile)
		g.PUT("/api/profile", a.UpdateUserProfile)
		g.GET("/api/profile/api-key-scopes", a.GetPersonalAPIKeyScopes)
		g.GET("/api/profile/api-keys", a.GetPersonalAPIKeys)
		g.POST("/api/profile/api-keys", a.CreatePersonalAPIKey)
		g.PUT("/api/profile/api-keys/:id", hasID(a.UpdatePersonalAPIKey))
		g.POST("/api/profile/api-keys/:id/rotate", hasID(a.RotatePersonalAPIKey))
		g.DELETE("/api/profile/api-keys/:id", hasID(a.DeletePersonalAPIKey))
		g.GET("/api/profile/smtp", a.GetPersonalSMTP)
		g.PUT("/api/profile/smtp", a.UpdatePersonalSMTP)
		g.DELETE("/api/profile/smtp/:id", hasID(a.DeletePersonalSMTP))
		g.POST("/api/profile/smtp/test", a.TestPersonalSMTP)
		g.GET("/api/users/:id/smtp", hasID(a.GetUserPersonalSMTP))
		g.GET("/api/profile/reply-mailboxes", a.GetReplyMailboxes)
		g.POST("/api/profile/reply-mailboxes", a.CreateReplyMailbox)
		g.PUT("/api/profile/reply-mailboxes/:id", hasID(a.UpdateReplyMailbox))
		g.DELETE("/api/profile/reply-mailboxes/:id", hasID(a.DisableReplyMailbox))
		g.PUT("/api/profile/reply-mailboxes/:id/enable", hasID(a.EnableReplyMailbox))
		g.POST("/api/profile/reply-mailboxes/:id/delete", hasID(a.DeleteReplyMailbox))
		g.POST("/api/profile/reply-mailboxes/test", a.TestReplyMailbox)

		// Organization workspaces. Workspace-scoped endpoints use the
		// X-Listmonk-Organization-ID request header; a missing header selects
		// the caller's personal workspace.
		g.GET("/api/workspace", a.GetCurrentWorkspace)
		g.GET("/api/organizations/me", a.GetMyOrganizations)
		g.POST("/api/organizations/requests", a.CreateOrganizationRequest)
		g.GET("/api/organizations/requests/mine", a.GetMyOrganizationRequests)
		g.DELETE("/api/organizations/requests/:id", hasID(a.WithdrawOrganizationRequest))
		g.POST("/api/organizations/join", a.JoinOrganizationByInvite)
		g.POST("/api/organizations/leave", a.LeaveOrganization)
		g.POST("/api/organizations/resources/migrate", a.MigratePersonalResourcesToOrganization)
		g.POST("/api/organizations/resources/customer-lists/migrate", a.MigratePersonalListsToOrganization)
		g.GET("/api/organizations/members", a.GetOrganizationMembers)
		g.POST("/api/organizations/members", a.AddOrganizationMember)
		g.PUT("/api/organizations/members/:user_id", a.UpdateOrganizationMember)
		g.DELETE("/api/organizations/members/:user_id", a.RemoveOrganizationMember)
		g.POST("/api/organizations/resources/transfer", a.TransferPendingOrganizationResources)
		g.GET("/api/organizations/:id/members", pm(hasID(a.GetOrganizationMembersForPlatform), auth.PermOrganizationsPlatformManage))
		g.POST("/api/organizations/:id/members/bulk", pm(hasID(a.AddOrganizationMembersBulk), auth.PermOrganizationsPlatformManage))
		g.POST("/api/organizations/:id/resources/transfer", pm(hasID(a.TransferArchivedOrganizationResources), auth.PermOrganizationsPlatformManage))
		g.POST("/api/organizations/templates/:id/transfer", hasID(a.TransferOrganizationTemplate))
		g.POST("/api/organizations/templates/:id/unpublish", hasID(a.UnpublishOrganizationTemplate))
		g.GET("/api/organizations/reply-forwarding", a.GetReplyForwardRules)
		g.PUT("/api/organizations/reply-forwarding/:id", hasID(a.UpdateReplyForwardRule))
		g.DELETE("/api/organizations/reply-forwarding/:id", hasID(a.DeleteReplyForwardRule))
		g.GET("/api/organizations/invites", a.GetOrganizationInvites)
		g.POST("/api/organizations/invites", a.CreateOrganizationInvite)
		g.DELETE("/api/organizations/invites/:id", hasID(a.RevokeOrganizationInvite))
		// The organization's single unified reply mailbox replaces the former
		// per-allocation reply mailbox. Only the organization workspace itself
		// may configure it; nothing is set on an organization's behalf.
		g.PUT("/api/organizations/:id/reply-mailbox", hasID(a.SetOrganizationReplyMailbox))
		g.GET("/api/organizations", pm(a.GetOrganizations, auth.PermOrganizationsPlatformManage))
		g.POST("/api/organizations", pm(a.CreateOrganization, auth.PermOrganizationsPlatformManage))
		g.GET("/api/organizations/requests", pm(a.GetOrganizationRequests, auth.PermOrganizationsPlatformManage))
		g.PUT("/api/organizations/requests/:id", pm(hasID(a.ReviewOrganizationRequest), auth.PermOrganizationsPlatformManage))
		g.POST("/api/organizations/:id/archive", pm(hasID(a.ArchiveOrganization), auth.PermOrganizationsPlatformManage))
		g.DELETE("/api/organizations/:id", pm(hasID(a.PurgeArchivedOrganization), auth.PermOrganizationsPlatformManage))

		g.GET("/api/users", pm(a.GetUsers, "users:get"))
		g.POST("/api/users/bulk", pm(a.CreateUsers, "users:manage"))
		g.GET("/api/users/:id/integration-tokens", pm(hasID(a.GetUserIntegrationTokens), auth.PermUsersTokens))
		g.POST("/api/users/:id/integration-tokens", pm(hasID(a.CreateUserIntegrationToken), auth.PermUsersTokens))
		g.DELETE("/api/users/:id/integration-tokens/:token_id", pm(hasID(a.DeleteUserIntegrationToken), auth.PermUsersTokens))
		g.GET("/api/users/:id", pm(hasID(a.GetUser), "users:get"))
		g.POST("/api/users", pm(a.CreateUser, "users:manage"))
		g.PUT("/api/users/:id", pm(hasID(a.UpdateUser), "users:manage"))
		g.DELETE("/api/users", pm(a.DeleteUsers, "users:manage"))
		g.DELETE("/api/users/:id", pm(hasID(a.DeleteUser), "users:manage"))
		g.POST("/api/logout", a.Logout)

		// TOTP 2FA endpoints
		g.GET("/api/users/:id/twofa/totp", hasID(a.GenerateTOTPQR))
		g.PUT("/api/users/:id/twofa", hasID(a.EnableTOTP))
		g.DELETE("/api/users/:id/twofa", hasID(a.DisableTOTP))

		g.GET("/api/roles/users", pm(a.GetUserRoles, "roles:get"))
		g.GET("/api/roles/customer-lists", pm(a.GeListRoles, "roles:get"))
		g.POST("/api/roles/users", pm(a.CreateUserRole, "roles:manage"))
		g.POST("/api/roles/customer-lists", pm(a.CreateListRole, "roles:manage"))
		g.PUT("/api/roles/users/:id", pm(hasID(a.UpdateUserRole), "roles:manage"))
		g.PUT("/api/roles/customer-lists/:id", pm(hasID(a.UpdateListRole), "roles:manage"))
		g.DELETE("/api/roles/:id", pm(hasID(a.DeleteRole), "roles:manage"))

		if a.cfg.BounceWebhooksEnabled {
			// Private authenticated bounce endpoint.
			g.POST("/webhooks/bounce", pm(a.BounceWebhook, "webhooks:post_bounce"))
		}
	}

	// =================================================================
	// Public API endpoints.
	{
		// Public unauthenticated endpoints.
		g := e.Group("")
		g.Use(a.auditMiddleware)

		if a.cfg.BounceWebhooksEnabled {
			// Public bounce endpoints for webservices like SES.
			g.POST("/webhooks/service/:service", a.BounceWebhook)
		}

		// Landing page.
		g.GET("/", func(c echo.Context) error {
			return c.Render(http.StatusOK, "home", publicTpl{Title: "listmonk"})
		})

		// Public admin endpoints (login page, OIDC endpoints, password reset).
		g.GET(path.Join(uriAdmin, "/login"), a.LoginPage)
		g.POST(path.Join(uriAdmin, "/login"), a.LoginPage)
		g.GET(path.Join(uriAdmin, "/login/twofa"), a.TwofaPage)
		g.POST(path.Join(uriAdmin, "/login/twofa"), a.TwofaPage)
		g.GET(path.Join(uriAdmin, "/forgot"), a.ForgotPage)
		g.POST(path.Join(uriAdmin, "/forgot"), a.ForgotPage)
		g.GET(path.Join(uriAdmin, "/reset"), a.ResetPage)
		g.POST(path.Join(uriAdmin, "/reset"), a.ResetPage)

		if a.cfg.Security.OIDC.Enabled {
			g.POST("/auth/oidc", a.OIDCLogin)
			g.GET("/auth/oidc", a.OIDCFinish)
		}

		// Public APIs.
		g.GET("/api/public/customer-lists", a.GetPublicLists)
		g.POST("/api/public/subscription", a.PublicSubscription)
		g.GET("/api/public/captcha/altcha", a.AltchaChallenge)
		if a.cfg.EnablePublicArchive {
			g.GET("/api/public/archive", a.GetCampaignArchives)
		}

		// /public/static/* file server is registered in initHTTPServer().
		// Public customer facing views.
		g.GET("/subscription/form", a.SubscriptionFormPage)
		g.POST("/subscription/form", a.SubscriptionForm)
		g.GET("/subscription/:campUUID/:subUUID", noIndex(a.hasUUID(a.SubscriptionPage, "campUUID", "subUUID")))
		g.POST("/subscription/:campUUID/:subUUID", a.hasUUID(a.SubscriptionPrefs, "campUUID", "subUUID"))
		g.GET("/subscription/optin/:subUUID", noIndex(a.hasUUID(a.hasSub(a.OptinPage), "subUUID")))
		g.POST("/subscription/optin/:subUUID", a.hasUUID(a.hasSub(a.OptinPage), "subUUID"))
		g.POST("/subscription/export/:subUUID", a.hasUUID(a.hasSub(a.SelfExportCustomerData), "subUUID"))
		g.POST("/subscription/wipe/:subUUID", a.hasUUID(a.hasSub(a.WipeCustomerData), "subUUID"))
		g.GET("/link/:linkUUID/:campUUID/:subUUID", noIndex(a.hasUUID(a.LinkRedirect, "linkUUID", "campUUID", "subUUID")))
		g.GET("/campaign/:campUUID/:subUUID", noIndex(a.hasUUID(a.ViewCampaignMessage, "campUUID", "subUUID")))
		g.GET("/campaign/:campUUID/:subUUID/px.png", noIndex(a.hasUUID(a.RegisterCampaignView, "campUUID", "subUUID")))

		if a.cfg.EnablePublicArchive {
			g.GET("/archive", a.CampaignArchivesPage)
			g.GET("/archive.xml", a.GetCampaignArchivesFeed)
			g.GET("/archive/:id", a.CampaignArchivePage)
			g.GET("/archive/latest", a.CampaignArchivePageLatest)
		}

		g.GET("/public/custom.css", serveCustomAppearance("public.custom_css"))
		g.GET("/public/custom.js", serveCustomAppearance("public.custom_js"))

		// Public health API endpoint.
		g.GET("/health", a.HealthCheck)

		// 404 pages.
		g.RouteNotFound("/*", func(c echo.Context) error {
			return c.Render(http.StatusNotFound, tplMessage,
				makeMsgTpl("404 - "+a.i18n.T("public.notFoundTitle"), "", ""))
		})
		g.RouteNotFound("/api/*", func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusNotFound, "404 unknown endpoint")
		})
		g.RouteNotFound("/admin/*", func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusNotFound, "404 page not found")
		})
	}
}

// AdminPage is the root handler that renders the Javascript admin frontend.
func (a *App) AdminPage(c echo.Context) error {
	b, err := a.fs.Read(path.Join(uriAdmin, "/index.html"))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	b = bytes.ReplaceAll(b, []byte("asset_version"), []byte(a.cfg.AssetVersion))

	return c.HTMLBlob(http.StatusOK, b)
}

// HealthCheck is a healthcheck endpoint that returns a 200 response.
func (a *App) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, okResp{true})
}

// serveCustomAppearance serves the given custom CSS/JS appearance blob
// meant for customizing public and admin pages from the admin settings UI.
func serveCustomAppearance(name string) echo.HandlerFunc {
	return func(c echo.Context) error {
		var (
			app = c.Get("app").(*App)

			out []byte
			hdr string
		)

		switch name {
		case "admin.custom_css":
			out = app.cfg.Appearance.AdminCSS
			hdr = "text/css; charset=utf-8"

		case "admin.custom_js":
			out = app.cfg.Appearance.AdminJS
			hdr = "application/javascript; charset=utf-8"

		case "public.custom_css":
			out = app.cfg.Appearance.PublicCSS
			hdr = "text/css; charset=utf-8"

		case "public.custom_js":
			out = app.cfg.Appearance.PublicJS
			hdr = "application/javascript; charset=utf-8"
		}

		return c.Blob(http.StatusOK, hdr, out)
	}
}

// hasUUID middleware validates the UUID string format for a given set of params.
func (a *App) hasUUID(next echo.HandlerFunc, params ...string) echo.HandlerFunc {
	return func(c echo.Context) error {
		for _, p := range params {
			if !reUUID.MatchString(c.Param(p)) {
				return c.Render(http.StatusBadRequest, tplMessage, makeMsgTpl(a.i18n.T("public.errorTitle"), "",
					a.i18n.T("globals.messages.invalidUUID")))
			}
		}
		return next(c)
	}
}

// hasID middleware validates the :id param in the URL and sets its int value in the context.
func hasID(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, _ := strconv.Atoi(c.Param("id"))
		if id < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid ID")
		}

		c.Set("id", id)
		return next(c)
	}
}

// hasSub middleware checks if a customer exists given the UUID
// param in a request.
func (a *App) hasSub(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		subUUID := c.Param("subUUID")

		customer, err := a.core.GetCustomer(0, subUUID, "")
		if err != nil {
			if er, ok := err.(*echo.HTTPError); ok && er.Code == http.StatusBadRequest {
				return c.Render(http.StatusNotFound, tplMessage,
					makeMsgTpl(a.i18n.T("public.notFoundTitle"), "", er.Message.(string)))
			}

			a.log.Printf("error checking customer existence: %v", err)
			return c.Render(http.StatusInternalServerError, tplMessage,
				makeMsgTpl(a.i18n.T("public.errorTitle"), "", a.i18n.T("public.errorProcessingRequest")))
		}
		organizationID := 0
		if customer.OrganizationID.Valid {
			organizationID = customer.OrganizationID.Int
		}
		setAuditCustomerContext(c, organizationID, customer.UUID, map[string]any{
			"channel": "public_self_service",
		})

		return next(c)
	}
}

// noIndex adds the HTTP header requesting robots to not crawl the page.
func noIndex(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("X-Robots-Tag", "noindex")
		return next(c)
	}
}

// getID returns the :id param from the URL parsed and stored as an int by the hasID middleware.
func getID(c echo.Context) int {
	return c.Get("id").(int)
}
