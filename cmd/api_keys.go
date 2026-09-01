package main

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

const (
	apiKeyScopeListsRead           = "lists:read"
	apiKeyScopeListsWrite          = "lists:write"
	apiKeyScopeSubscribersRead     = "subscribers:read"
	apiKeyScopeSubscribersWrite    = "subscribers:write"
	apiKeyScopeSubscribersImport   = "subscribers:import"
	apiKeyScopeTemplatesRead       = "templates:read"
	apiKeyScopeTemplatesWrite      = "templates:write"
	apiKeyScopeMediaRead           = "media:read"
	apiKeyScopeMediaWrite          = "media:write"
	apiKeyScopeCampaignsRead       = "campaigns:read"
	apiKeyScopeCampaignsWrite      = "campaigns:write"
	apiKeyScopeCampaignsSend       = "campaigns:send"
	apiKeyScopeCampaignsAnalytics  = "campaigns:analytics"
	apiKeyScopeCampaignsRecipients = "campaigns:recipients"
	apiKeyScopeBouncesRead         = "bounces:read"
	apiKeyScopeBouncesWrite        = "bounces:write"
	apiKeyScopeTransactionalSend   = "transactional:send"
)

type apiKeyScopeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var personalAPIKeyScopeOptions = []apiKeyScopeOption{
	{apiKeyScopeListsRead, "Lists: read"},
	{apiKeyScopeListsWrite, "Lists: create and manage"},
	{apiKeyScopeSubscribersRead, "Subscribers: read and export"},
	{apiKeyScopeSubscribersWrite, "Subscribers: create and manage"},
	{apiKeyScopeSubscribersImport, "Subscribers: bulk import"},
	{apiKeyScopeTemplatesRead, "Templates: read"},
	{apiKeyScopeTemplatesWrite, "Templates: create and manage"},
	{apiKeyScopeMediaRead, "Media: read"},
	{apiKeyScopeMediaWrite, "Media: upload and manage"},
	{apiKeyScopeCampaignsRead, "Campaigns: read"},
	{apiKeyScopeCampaignsWrite, "Campaigns: create and manage drafts"},
	{apiKeyScopeCampaignsSend, "Campaigns: start and schedule sends"},
	{apiKeyScopeCampaignsAnalytics, "Campaigns: aggregate analytics"},
	{apiKeyScopeCampaignsRecipients, "Campaigns: recipient analytics"},
	{apiKeyScopeBouncesRead, "Bounces: read"},
	{apiKeyScopeBouncesWrite, "Bounces: manage"},
	{apiKeyScopeTransactionalSend, "Transactional messages: send"},
}

type personalAPIKeyRequest struct {
	Name                    string   `json:"name"`
	WorkspaceOrganizationID int      `json:"workspace_organization_id"`
	Scopes                  []string `json:"scopes"`
	ExpiresAt               string   `json:"expires_at"`
}

type personalAPIKeyUpdateRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
}

type personalAPIKeyRotateRequest struct {
	ExpiresAt string `json:"expires_at"`
}

// apiKeyScope is a route middleware for endpoints exposed to personal API
// keys. Browser sessions and legacy service tokens retain their existing
// authorization behavior; a personal key needs the declared scope.
func apiKeyScope(next echo.HandlerFunc, scope string) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := requireAPIKeyScope(c, scope); err != nil {
			return err
		}
		return next(c)
	}
}

func requireAPIKeyScope(c echo.Context, scope string) error {
	if auth.HasIntegrationTokenScope(c, scope) {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden, "API key is missing scope: "+scope)
}

// rejectUnsupportedPersonalAPIKey keeps the personal-key surface deliberately
// small. Every supported business route also has a scope middleware below;
// this allowlist prevents a new route from becoming callable accidentally.
func rejectUnsupportedPersonalAPIKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if auth.IsPersonalIntegrationToken(c) && !isPersonalAPIKeyBusinessPath(c.Path()) {
			return echo.NewHTTPError(http.StatusForbidden, "personal API keys cannot access this endpoint")
		}
		return next(c)
	}
}

func isPersonalAPIKeyBusinessPath(path string) bool {
	if strings.HasPrefix(path, "/api/subscribers/query/") || path == "/api/subscribers/export" {
		return false
	}
	for _, prefix := range []string{
		"/api/lists",
		"/api/subscribers",
		"/api/import/subscribers",
		"/api/campaigns",
		"/api/media",
		"/api/templates",
		"/api/bounces",
		"/api/tx",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (a *App) GetPersonalAPIKeyScopes(c echo.Context) error {
	if _, err := a.currentRegularUser(c); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{personalAPIKeyScopeOptions})
}

func (a *App) GetPersonalAPIKeys(c echo.Context) error {
	u, err := a.currentRegularUser(c)
	if err != nil {
		return err
	}
	out, err := a.core.GetPersonalIntegrationTokens(u.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) CreatePersonalAPIKey(c echo.Context) error {
	u, err := a.currentRegularUser(c)
	if err != nil {
		return err
	}
	var req personalAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	name, scopes, expiresAt, err := validatePersonalAPIKeyInput(req.Name, req.Scopes, req.ExpiresAt)
	if err != nil {
		return err
	}
	if _, err := a.workspaceAccessForOrganization(c, req.WorkspaceOrganizationID); err != nil {
		return err
	}

	out, token, err := a.core.CreatePersonalIntegrationToken(u.ID, req.WorkspaceOrganizationID, name, scopes, expiresAt)
	if err != nil {
		return err
	}
	if _, err := cacheUsers(a.core, a.auth); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, okResp{struct {
		auth.IntegrationToken
		Token string `json:"token"`
	}{IntegrationToken: out, Token: token}})
}

func (a *App) UpdatePersonalAPIKey(c echo.Context) error {
	u, err := a.currentRegularUser(c)
	if err != nil {
		return err
	}
	var req personalAPIKeyUpdateRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	name, scopes, expiresAt, err := validatePersonalAPIKeyInput(req.Name, req.Scopes, req.ExpiresAt)
	if err != nil {
		return err
	}
	out, err := a.core.UpdatePersonalIntegrationToken(u.ID, getID(c), name, scopes, expiresAt)
	if err != nil {
		return err
	}
	if _, err := cacheUsers(a.core, a.auth); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) RotatePersonalAPIKey(c echo.Context) error {
	u, err := a.currentRegularUser(c)
	if err != nil {
		return err
	}
	var req personalAPIKeyRotateRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	expiresAt, err := parsePersonalAPIKeyExpiry(req.ExpiresAt)
	if err != nil {
		return err
	}
	out, token, err := a.core.RotatePersonalIntegrationToken(u.ID, getID(c), expiresAt)
	if err != nil {
		return err
	}
	if _, err := cacheUsers(a.core, a.auth); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, okResp{struct {
		auth.IntegrationToken
		Token string `json:"token"`
	}{IntegrationToken: out, Token: token}})
}

func (a *App) DeletePersonalAPIKey(c echo.Context) error {
	u, err := a.currentRegularUser(c)
	if err != nil {
		return err
	}
	if err := a.core.DeletePersonalIntegrationToken(u.ID, getID(c)); err != nil {
		return err
	}
	if _, err := cacheUsers(a.core, a.auth); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) currentRegularUser(c echo.Context) (auth.User, error) {
	if auth.IsPersonalIntegrationToken(c) {
		return auth.User{}, echo.NewHTTPError(http.StatusForbidden, "API keys cannot manage API keys")
	}
	u := auth.GetUser(c)
	if u.Type != auth.UserTypeUser || u.Status != auth.UserStatusEnabled {
		return auth.User{}, echo.NewHTTPError(http.StatusForbidden, "personal API keys require an enabled regular user")
	}
	return u, nil
}

func validatePersonalAPIKeyInput(name string, scopes []string, expiresAt string) (string, []string, time.Time, error) {
	name = strings.TrimSpace(name)
	if !strHasLen(name, 1, stdInputMaxLen) {
		return "", nil, time.Time{}, echo.NewHTTPError(http.StatusBadRequest, "invalid API key name")
	}
	normalizedScopes, err := normalizePersonalAPIKeyScopes(scopes)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	expiry, err := parsePersonalAPIKeyExpiry(expiresAt)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	return name, normalizedScopes, expiry, nil
}

func normalizePersonalAPIKeyScopes(scopes []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(personalAPIKeyScopeOptions))
	all := make([]string, 0, len(personalAPIKeyScopeOptions))
	for _, scope := range personalAPIKeyScopeOptions {
		allowed[scope.ID] = struct{}{}
		all = append(all, scope.ID)
	}
	if scopes == nil {
		return all, nil
	}
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if _, ok := allowed[scope]; !ok {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid API key scope: "+scope)
		}
		if _, ok := seen[scope]; !ok {
			seen[scope] = struct{}{}
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "select at least one API key scope")
	}
	sort.Strings(out)
	return out, nil
}

func parsePersonalAPIKeyExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	month, err := time.ParseInLocation("2006-01", value, time.Local)
	if err != nil || month.Format("2006-01") != value {
		return time.Time{}, echo.NewHTTPError(http.StatusBadRequest, "API key expiry must be in YYYY-MM format")
	}
	now := time.Now().In(time.Local)
	minimum := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.Local)
	maximum := time.Date(now.Year(), now.Month()+25, 1, 0, 0, 0, 0, time.Local)
	if month.Before(minimum) || !month.Before(maximum) {
		return time.Time{}, echo.NewHTTPError(http.StatusBadRequest, "API key expiry must be between 1 and 24 months from now")
	}
	return time.Date(month.Year(), month.Month()+1, 0, 23, 59, 59, 999999999, time.Local), nil
}

func apiKeyWorkspaceOrganizationID(token auth.IntegrationToken) int {
	if token.WorkspaceOrganizationID.Valid {
		return int(token.WorkspaceOrganizationID.Int)
	}
	return 0
}

func personalAPIKeyWorkspaceMatches(c echo.Context, organizationID int) bool {
	token, ok := auth.GetIntegrationTokenContext(c)
	return !ok || token.Kind != auth.IntegrationTokenKindPersonal || apiKeyWorkspaceOrganizationID(token) == organizationID
}

func personalAPIKeyWorkspaceError() error {
	return echo.NewHTTPError(http.StatusForbidden, "API key is bound to a different workspace")
}
