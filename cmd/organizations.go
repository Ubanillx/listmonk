package main

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	workspaceHeader = "X-Listmonk-Organization-ID"

	// Resource identifiers are kept local to cmd so handlers do not need to
	// depend on core's internal implementation constants.
	resourceLists       = "lists"
	resourceSubscribers = "subscribers"
	resourceTemplates   = "templates"
	resourceCampaigns   = "campaigns"
	resourceMedia       = "media"
)

type organizationRequestInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type organizationReviewInput struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note"`
}

type organizationMemberInput struct {
	UserID  int    `json:"user_id"`
	Account string `json:"account"`
	Role    string `json:"role"`
}

type organizationInviteInput struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
	MaxUses   *int   `json:"max_uses"`
}

type organizationJoinInput struct {
	Code string `json:"code"`
}

type organizationTransferInput struct {
	TargetUserID int `json:"target_user_id"`
}

type organizationListMigrationInput struct {
	ListIDs              []int  `json:"list_ids"`
	Mode                 string `json:"mode"`
	TargetOrganizationID *int   `json:"target_organization_id"`
}

// workspaceFromRequest resolves an explicit request header first, then the
// organization_id query parameter. API token clients must send the header on
// every request; browser clients keep their active workspace in localStorage.
func (a *App) workspaceFromRequest(c echo.Context) (models.Workspace, error) {
	raw := strings.TrimSpace(c.Request().Header.Get(workspaceHeader))
	if raw == "" {
		raw = strings.TrimSpace(c.QueryParam("organization_id"))
	}
	if raw == "" || raw == "0" || strings.EqualFold(raw, "personal") {
		access, err := a.workspaceAccessForOrganization(c, 0)
		return access.Workspace, err
	}

	orgID, err := strconv.Atoi(raw)
	if err != nil || orgID < 1 {
		return models.Workspace{}, echo.NewHTTPError(http.StatusBadRequest, "invalid organization workspace")
	}
	access, err := a.workspaceAccessForOrganization(c, orgID)
	return access.Workspace, err
}

func (a *App) workspaceAccess(c echo.Context) (models.WorkspaceAccess, error) {
	ws, err := a.workspaceFromRequest(c)
	if err != nil {
		return models.WorkspaceAccess{}, err
	}
	return models.WorkspaceAccess{Workspace: ws, UserID: auth.GetUser(c).ID}, nil
}

// workspaceAccessForOrganization resolves an explicit clone or migration
// target without trusting an organization ID from the client body.
func (a *App) workspaceAccessForOrganization(c echo.Context, orgID int) (models.WorkspaceAccess, error) {
	user := auth.GetUser(c)
	if orgID == 0 {
		return models.WorkspaceAccess{
			Workspace: models.Workspace{Personal: true, PlatformAdmin: user.UserRoleID == auth.SuperAdminRoleID},
			UserID:    user.ID,
		}, nil
	}
	if orgID < 0 {
		return models.WorkspaceAccess{}, echo.NewHTTPError(http.StatusBadRequest, "invalid organization workspace")
	}
	org, err := a.core.GetOrganization(orgID)
	if err != nil {
		return models.WorkspaceAccess{}, err
	}
	ws := models.Workspace{
		OrganizationID:   orgID,
		OrganizationName: org.Name,
		PlatformAdmin:    user.UserRoleID == auth.SuperAdminRoleID,
		Archived:         org.Status == models.OrganizationStatusArchived,
	}
	if ws.PlatformAdmin {
		// Archived organizations are intentionally unavailable to members, but
		// platform administrators must be able to select one to transfer or
		// clean up resources before any eventual permanent deletion.
		return models.WorkspaceAccess{Workspace: ws, UserID: user.ID}, nil
	}
	if org.Status != models.OrganizationStatusActive {
		return models.WorkspaceAccess{}, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	membership, err := a.core.GetOrganizationMembership(orgID, user.ID)
	if err != nil {
		return models.WorkspaceAccess{}, err
	}
	ws.Role = membership.Role
	return models.WorkspaceAccess{Workspace: ws, UserID: user.ID}, nil
}

func normalizeWorkspaceVisibility(access models.WorkspaceAccess, value string) (string, error) {
	if value == "" {
		return models.ResourceVisibilityPrivate, nil
	}
	if value != models.ResourceVisibilityPrivate &&
		value != models.ResourceVisibilityOrganization &&
		value != models.ResourceVisibilityGlobal {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid resource visibility")
	}
	if value == models.ResourceVisibilityOrganization && !access.IsOrganization() {
		return "", echo.NewHTTPError(http.StatusBadRequest, "organization visibility requires an organization workspace")
	}
	return value, nil
}

// normalizeResourceVisibility additionally constrains the resource types that
// can be published. Lists and subscribers are always owned by one user. An
// organization manager can inspect them, but ordinary members must never gain
// access through a visibility flag.
func normalizeResourceVisibility(access models.WorkspaceAccess, resource, value string) (string, error) {
	visibility, err := normalizeWorkspaceVisibility(access, value)
	if err != nil {
		return "", err
	}
	if (resource == resourceLists || resource == resourceSubscribers) &&
		visibility != models.ResourceVisibilityPrivate {
		return "", echo.NewHTTPError(http.StatusBadRequest, "lists and subscribers must remain private to their owner")
	}
	if visibility == models.ResourceVisibilityGlobal &&
		(resource == resourceLists || resource == resourceSubscribers || resource == resourceMedia) {
		return "", echo.NewHTTPError(http.StatusBadRequest, "this resource cannot be globally visible")
	}
	return visibility, nil
}

func (a *App) requireOrganizationManager(c echo.Context) (models.Workspace, error) {
	ws, err := a.workspaceFromRequest(c)
	if err != nil {
		return ws, err
	}
	if ws.OrganizationID == 0 {
		return ws, echo.NewHTTPError(http.StatusBadRequest, "select an organization workspace")
	}
	if ws.Archived {
		return ws, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	if !ws.PlatformAdmin && ws.Role != models.OrganizationMemberRoleManager {
		return ws, echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	return ws, nil
}

// requireOrganizationTransferManager keeps former-member resource transfer
// available to active organization managers. Once an organization is
// archived, only a platform administrator may use this limited cleanup path;
// all ordinary resource writes remain blocked.
func (a *App) requireOrganizationTransferManager(c echo.Context) (models.Workspace, error) {
	ws, err := a.workspaceFromRequest(c)
	if err != nil {
		return ws, err
	}
	if ws.OrganizationID == 0 {
		return ws, echo.NewHTTPError(http.StatusBadRequest, "select an organization workspace")
	}
	if ws.Archived && !ws.PlatformAdmin {
		return ws, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	if !ws.PlatformAdmin && ws.Role != models.OrganizationMemberRoleManager {
		return ws, echo.NewHTTPError(http.StatusForbidden, "organization manager permission required")
	}
	return ws, nil
}

func requireWritableWorkspace(access models.WorkspaceAccess) error {
	if access.Archived {
		return echo.NewHTTPError(http.StatusConflict, "organization is archived; only resource transfer or cleanup is allowed")
	}
	return nil
}

func (a *App) requirePlatformAdmin(c echo.Context) error {
	if auth.GetUser(c).UserRoleID != auth.SuperAdminRoleID {
		return echo.NewHTTPError(http.StatusForbidden, "platform administrator permission required")
	}
	return nil
}

func (a *App) GetMyOrganizations(c echo.Context) error {
	user := auth.GetUser(c)
	out, err := a.core.GetUserOrganizations(user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetOrganizations(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	includeArchived := c.QueryParam("include_archived") == "true"
	out, err := a.core.GetOrganizations(includeArchived)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetCurrentWorkspace(c echo.Context) error {
	ws, err := a.workspaceFromRequest(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{ws})
}

func (a *App) CreateOrganizationRequest(c echo.Context) error {
	var req organizationRequestInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if !strHasLen(req.Name, 2, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, "organization name must be between 2 and 2000 characters")
	}
	if len(req.Description) > stdInputMaxLen {
		return echo.NewHTTPError(http.StatusBadRequest, "organization description is too long")
	}
	out, err := a.core.CreateOrganizationRequest(auth.GetUser(c).ID, req.Name, req.Description)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, okResp{out})
}

func (a *App) GetOrganizationRequests(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	out, err := a.core.GetOrganizationRequests(c.QueryParam("include_resolved") == "true")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ReviewOrganizationRequest(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	id := getID(c)
	var req organizationReviewInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	out, err := a.core.ReviewOrganizationRequest(id, auth.GetUser(c).ID, req.Approve, req.Note)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ArchiveOrganization(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	stopped, err := a.core.ArchiveOrganization(getID(c))
	if err != nil {
		return err
	}
	a.stopOrganizationImport(getID(c), 0)
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused {
			a.manager.StopCampaign(campaign.ID, models.CampaignStatusPaused)
		}
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// PurgeArchivedOrganization permanently removes organization metadata only
// after all scoped resources have been transferred or cleaned up.
func (a *App) PurgeArchivedOrganization(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	if err := a.core.PurgeArchivedOrganization(getID(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) JoinOrganizationByInvite(c echo.Context) error {
	var req organizationJoinInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Code) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "invitation code is required")
	}
	out, err := a.core.JoinOrganizationByInvite(auth.GetUser(c).ID, a.coreHashInvite(req.Code))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) GetOrganizationMembers(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	out, err := a.core.GetOrganizationMembers(ws.OrganizationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) AddOrganizationMember(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	var req organizationMemberInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.UserID < 1 {
		userID, err := a.core.FindUserIDByAccount(req.Account)
		if err != nil {
			return err
		}
		req.UserID = userID
	}
	if req.Role == "" {
		req.Role = models.OrganizationMemberRoleMember
	}
	out, err := a.core.AddOrganizationMember(ws.OrganizationID, req.UserID, req.Role)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, okResp{out})
}

func (a *App) UpdateOrganizationMember(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid user id")
	}
	var req organizationMemberInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := a.core.UpdateOrganizationMemberRole(ws.OrganizationID, userID, req.Role); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) RemoveOrganizationMember(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid user id")
	}
	stopped, err := a.core.RemoveOrganizationMember(ws.OrganizationID, userID, auth.GetUser(c).ID)
	if err != nil {
		return err
	}
	a.stopOrganizationImport(ws.OrganizationID, userID)
	// Stop manager goroutines after the transaction committed. The records are
	// already paused in the DB, so they cannot be picked up by a new worker.
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused {
			a.manager.StopCampaign(campaign.ID, models.CampaignStatusPaused)
		}
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) LeaveOrganization(c echo.Context) error {
	ws, err := a.workspaceFromRequest(c)
	if err != nil {
		return err
	}
	if ws.OrganizationID == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "select an organization workspace")
	}
	if ws.Archived {
		return echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	stopped, err := a.core.RemoveOrganizationMember(ws.OrganizationID, auth.GetUser(c).ID, auth.GetUser(c).ID)
	if err != nil {
		return err
	}
	a.stopOrganizationImport(ws.OrganizationID, auth.GetUser(c).ID)
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused {
			a.manager.StopCampaign(campaign.ID, models.CampaignStatusPaused)
		}
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) TransferPendingOrganizationResources(c echo.Context) error {
	ws, err := a.requireOrganizationTransferManager(c)
	if err != nil {
		return err
	}
	var req organizationTransferInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.TargetUserID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "target user is required")
	}
	if err := a.core.TransferPendingOrganizationResources(ws.OrganizationID, req.TargetUserID); err != nil {
		return err
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{true})
}

// TransferArchivedOrganizationResources moves the complete pending resource
// set out of an archived organization. This is intentionally a path-based
// platform-admin operation because archived organizations cannot be selected
// as ordinary workspaces.
func (a *App) TransferArchivedOrganizationResources(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	var req organizationTransferInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.TargetUserID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "target user is required")
	}
	if err := a.core.TransferArchivedOrganizationResourcesToPersonal(getID(c), req.TargetUserID); err != nil {
		return err
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{true})
}

// GetOrganizationMembersForPlatform exposes the transfer target list for an
// archived organization only to platform administrators. Active organization
// managers continue to use the workspace-scoped member endpoint.
func (a *App) GetOrganizationMembersForPlatform(c echo.Context) error {
	if err := a.requirePlatformAdmin(c); err != nil {
		return err
	}
	if _, err := a.core.GetOrganization(getID(c)); err != nil {
		return err
	}
	out, err := a.core.GetOrganizationMembers(getID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) TransferOrganizationTemplate(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	var req organizationTransferInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.TargetUserID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "target user is required")
	}
	if err := a.core.TransferOrganizationTemplate(ws.OrganizationID, getID(c), req.TargetUserID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) UnpublishOrganizationTemplate(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	if err := a.core.UnpublishOrganizationTemplate(ws.OrganizationID, getID(c)); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// MigratePersonalListsToOrganization copies or moves caller-owned personal
// lists into a selected organization. The target defaults to the active
// workspace, allowing clients to offer a direct "move to current organization"
// action while still supporting a picker from personal space.
func (a *App) MigratePersonalListsToOrganization(c echo.Context) error {
	user := auth.GetUser(c)
	active, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req organizationListMigrationInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.ListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one list is required")
	}
	if req.Mode != "copy" && req.Mode != "move" {
		return echo.NewHTTPError(http.StatusBadRequest, "migration mode must be copy or move")
	}
	target := active
	if req.TargetOrganizationID != nil {
		target, err = a.workspaceAccessForOrganization(c, *req.TargetOrganizationID)
		if err != nil {
			return err
		}
	}
	if !target.IsOrganization() {
		return echo.NewHTTPError(http.StatusBadRequest, "select an organization destination")
	}
	if err := requireWritableWorkspace(target); err != nil {
		return err
	}
	listIDs, err := a.core.MigratePersonalListsToOrganization(user.ID, target.OrganizationID, user.ID, req.ListIDs, req.Mode == "move")
	if err != nil {
		return err
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{struct {
		ListIDs []int  `json:"list_ids"`
		Mode    string `json:"mode"`
	}{ListIDs: listIDs, Mode: req.Mode}})
}

func (a *App) GetOrganizationInvites(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	out, err := a.core.GetOrganizationInvites(ws.OrganizationID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) CreateOrganizationInvite(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	var req organizationInviteInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.MaxUses != nil && *req.MaxUses < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "maximum uses must be greater than zero")
	}

	var expiresAt null.Time
	if strings.TrimSpace(req.ExpiresAt) != "" {
		at, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil || !at.After(time.Now()) {
			return echo.NewHTTPError(http.StatusBadRequest, "expiration must be a future RFC3339 timestamp")
		}
		expiresAt = null.Time{Time: at, Valid: true}
	}
	var maxUses null.Int
	if req.MaxUses != nil {
		maxUses = null.Int{Int: *req.MaxUses, Valid: true}
	}
	code, err := newOrganizationInviteCode()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not generate invitation code")
	}
	out, err := a.core.CreateOrganizationInvite(ws.OrganizationID, auth.GetUser(c).ID, req.Name, a.coreHashInvite(code), expiresAt, maxUses)
	if err != nil {
		return err
	}
	out.Code = code
	return c.JSON(http.StatusCreated, okResp{out})
}

func (a *App) RevokeOrganizationInvite(c echo.Context) error {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return err
	}
	id := getID(c)
	if err := a.core.RevokeOrganizationInvite(ws.OrganizationID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

func (a *App) coreHashInvite(code string) string {
	return core.HashOrganizationInviteCode(code)
}

// stopOrganizationImport terminates the singleton importer only when its
// active session belongs to the organization (and, when supplied, the member)
// whose access was just revoked.
func (a *App) stopOrganizationImport(organizationID, ownerUserID int) {
	if a.importer == nil {
		return
	}
	status := a.importer.GetStats()
	if status.Status != subimporter.StatusImporting || status.OrganizationID != organizationID {
		return
	}
	if ownerUserID != 0 && status.OwnerUserID != ownerUserID {
		return
	}
	a.importer.Stop()
}

func newOrganizationInviteCode() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
