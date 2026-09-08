package main

import (
	"net/http"
	"net/url"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/utils"
	"github.com/labstack/echo/v4"
)

// uriWorkspaceSelect is the authenticated post-login page where a caller picks
// the space (personal or organization) to enter before the admin UI loads.
const uriWorkspaceSelect = "/admin/select-workspace"

// selectWorkspaceTpl is the data model for the workspace selection page.
type selectWorkspaceTpl struct {
	Title       string
	Description string
	NextURI     string
	Spaces      []selectWorkspaceEntry
	AutoEnter   bool
	AutoOrgID   int
	AutoName    string
	Error       string
}

// selectWorkspaceEntry is a single selectable space on the selection page.
type selectWorkspaceEntry struct {
	OrganizationID int
	Name           string
	Personal       bool
	Role           string
}

// workspaceSelectURI builds the post-authentication redirect target so the
// caller picks the space to enter before the admin UI loads. The original
// destination is preserved in the `next` query parameter so deep links
// survive the selection step.
func (a *App) workspaceSelectURI(next string) string {
	next = utils.SanitizeURI(next)
	if next == "" || next == "/" {
		next = uriAdmin
	}
	return uriWorkspaceSelect + "?next=" + url.QueryEscape(next)
}

// SelectWorkspacePage renders the post-login workspace picker. The page
// auto-enters the single available space and shows a blocking error when no
// space is accessible.
func (a *App) SelectWorkspacePage(c echo.Context) error {
	user := auth.GetUser(c)
	next := utils.SanitizeURI(c.QueryParam("next"))
	if next == "" || next == "/" {
		next = uriAdmin
	}

	spaces := []selectWorkspaceEntry{}
	if canUsePersonalWorkspace(user) {
		spaces = append(spaces, selectWorkspaceEntry{
			Personal: true,
			Name:     a.i18n.T("users.personalSpace"),
		})
	}
	orgs, err := a.core.GetUserOrganizations(user.ID)
	if user.IsPlatformAdmin() {
		// Platform administrators can administer public-pool segments in any
		// active organization, so the post-login selector must expose the same
		// organization set as the SPA workspace switcher.
		orgs, err = a.core.GetOrganizations(false)
	}
	if err != nil {
		return err
	}
	for _, org := range orgs {
		spaces = append(spaces, selectWorkspaceEntry{
			OrganizationID: org.ID,
			Name:           org.Name,
			Role:           org.MyRole,
		})
	}

	out := selectWorkspaceTpl{
		Title:   a.i18n.T("users.selectWorkspace"),
		NextURI: next,
	}
	switch len(spaces) {
	case 0:
		// No personal space capability and no organization membership. Rendered
		// as a blocking state so the caller cannot enter the admin UI.
		out.Error = a.i18n.T("users.noWorkspace")
	case 1:
		// A single available space is entered automatically.
		out.AutoEnter = true
		out.AutoOrgID = spaces[0].OrganizationID
		out.AutoName = spaces[0].Name
	default:
		out.Spaces = spaces
	}
	return c.Render(http.StatusOK, "admin-select-workspace", out)
}
