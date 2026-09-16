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
	workspaceCookie = "listmonk_workspace_organization_id"

	// Resource identifiers are kept local to cmd so handlers do not need to
	// depend on core's internal implementation constants.
	resourceLists     = "customer_lists"
	resourceCustomers = "customers"
	resourceTemplates = "templates"
	resourceCampaigns = "campaigns"
	resourceMedia     = "media"
)

type organizationRequestInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type organizationCreateMemberInput struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
}

type organizationCreateInput struct {
	Name          string                          `json:"name"`
	Description   string                          `json:"description"`
	ManagerUserID int                             `json:"manager_user_id"`
	MemberUserIDs []int                           `json:"member_user_ids"`
	Members       []organizationCreateMemberInput `json:"members"`
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

type organizationMemberBulkRow struct {
	Account  string `json:"account"`
	Username string `json:"username"`
	UserID   int    `json:"user_id"`
	Role     string `json:"role"`
}

type organizationMemberBulkRequest struct {
	Members []organizationMemberBulkRow `json:"members"`
	Users   []organizationMemberBulkRow `json:"users"`
}

type organizationMemberBulkIssue struct {
	Row   int    `json:"row"`
	Field string `json:"field"`
	Code  string `json:"code"`
}

type organizationMemberBulkResponse struct {
	Added  int                           `json:"added"`
	Errors []organizationMemberBulkIssue `json:"errors"`
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
	CustomerListIDs      []int  `json:"customer_list_ids"`
	Mode                 string `json:"mode"`
	TargetOrganizationID *int   `json:"target_organization_id"`
}

type organizationResourceMigrationInput struct {
	Resource             string `json:"resource"`
	IDs                  []int  `json:"ids"`
	Mode                 string `json:"mode"`
	TargetOrganizationID *int   `json:"target_organization_id"`
}

// workspaceFromRequest resolves an explicit request header first, then the
// organization_id query parameter. API token clients must send the header on
// every request; browser clients keep their active workspace in localStorage.
func (a *App) workspaceFromRequest(c echo.Context) (models.Workspace, error) {
	raw := strings.TrimSpace(c.Request().Header.Get(workspaceHeader))
	if token, ok := auth.GetIntegrationTokenContext(c); ok && token.Kind == auth.IntegrationTokenKindPersonal && raw == "" {
		if token.WorkspaceOrganizationID.Valid {
			raw = strconv.Itoa(token.WorkspaceOrganizationID.Int)
		} else {
			raw = "0"
		}
	}
	if raw == "" && strings.HasPrefix(c.Path(), "/api/media/file/") {
		// A copied HTML body can retain an old query string. For the protected
		// browser media endpoint, the current browser workspace is authoritative
		// unless a caller explicitly supplied the request header.
		if cookie, err := c.Cookie(workspaceCookie); err == nil {
			raw = strings.TrimSpace(cookie.Value)
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(c.QueryParam("organization_id"))
	}
	if raw == "" {
		// Browser image requests do not include the workspace header. The
		// frontend mirrors its selected organization in this non-sensitive
		// cookie; membership is still resolved and enforced below.
		if cookie, err := c.Cookie(workspaceCookie); err == nil {
			raw = strings.TrimSpace(cookie.Value)
		}
	}
	if raw == "" || raw == "0" || strings.EqualFold(raw, "personal") {
		if !personalAPIKeyWorkspaceMatches(c, 0) {
			return models.Workspace{}, personalAPIKeyWorkspaceError()
		}

		// The four customer_list endpoints backing the personal-resource migration UI
		// are exempt from the personal workspace capability so retained
		// personal resources stay listable after the capability has been
		// revoked. The personal workspace only ever exposes the caller's own
		// resources, and detail reads, exports, every mutation, and any entry
		// into the personal workspace still require the capability.
		requireCapability := true
		if isReadOnlyMethod(c) && isPersonalMigrationListPath(c.Path()) {
			requireCapability = false
		}
		access, err := a.workspaceAccessForOrganizationWithPersonal(c, 0, requireCapability)
		return access.Workspace, err
	}

	orgID, err := strconv.Atoi(raw)
	if err != nil || orgID < 1 {
		return models.Workspace{}, echo.NewHTTPError(http.StatusBadRequest, "invalid organization workspace")
	}
	if !personalAPIKeyWorkspaceMatches(c, orgID) {
		return models.Workspace{}, personalAPIKeyWorkspaceError()
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
	return a.workspaceAccessForOrganizationWithPersonal(c, orgID, true)
}

// workspaceAccessForOrganizationWithPersonal resolves the workspace with an
// explicit choice of whether the personal workspace requires the
// workspaces:personal capability. The resource-migration endpoints call with
// requirePersonalCapability=false so retained personal resources stay
// migratable after the capability has been revoked; ownership checks in the
// migration handlers still keep the access strictly personal.
func (a *App) workspaceAccessForOrganizationWithPersonal(c echo.Context, orgID int, requirePersonalCapability bool) (models.WorkspaceAccess, error) {
	if !personalAPIKeyWorkspaceMatches(c, orgID) {
		return models.WorkspaceAccess{}, personalAPIKeyWorkspaceError()
	}
	user := auth.GetUser(c)
	if orgID == 0 {
		if requirePersonalCapability {
			if err := requirePersonalWorkspace(user); err != nil {
				return models.WorkspaceAccess{}, err
			}
		}
		return models.WorkspaceAccess{
			Workspace: models.Workspace{Personal: true, PlatformAdmin: user.IsPlatformAdmin()},
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
		PlatformAdmin:    user.IsPlatformAdmin(),
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
	// Platform organization operators may manage an organization selected by
	// the management screen without becoming a member or receiving access to
	// its campaigns, customers, templates, or other workspace resources.
	if user.HasPerm(auth.PermOrganizationsPlatformManage) && isOrganizationManagementPath(c.Path()) {
		return models.WorkspaceAccess{Workspace: ws, UserID: user.ID}, nil
	}
	membership, err := a.core.GetOrganizationMembership(orgID, user.ID)
	if err != nil {
		return models.WorkspaceAccess{}, err
	}
	ws.Role = membership.Role
	return models.WorkspaceAccess{Workspace: ws, UserID: user.ID}, nil
}

func isOrganizationManagementPath(path string) bool {
	switch path {
	case "/api/organizations/members",
		"/api/organizations/members/:user_id",
		"/api/organizations/resources/transfer",
		"/api/organizations/templates/:id/transfer",
		"/api/organizations/templates/:id/unpublish",
		"/api/organizations/reply-forwarding",
		"/api/organizations/reply-forwarding/:id",
		"/api/organizations/invites",
		"/api/organizations/invites/:id":
		return true
	default:
		return false
	}
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
// can be published. Ordinary customer lists and customers are always owned by
// one user. First-level public pools are the explicit exception: they are
// global platform resources whose organization delivery permissions are stored
// separately. An organization manager can inspect ordinary lists, but members
// must never gain access through a visibility flag.
func normalizeResourceVisibility(access models.WorkspaceAccess, resource, value string) (string, error) {
	visibility, err := normalizeWorkspaceVisibility(access, value)
	if err != nil {
		return "", err
	}
	if (resource == resourceLists || resource == resourceCustomers) &&
		visibility != models.ResourceVisibilityPrivate {
		return "", echo.NewHTTPError(http.StatusBadRequest, "customer_lists and customers must remain private to their owner")
	}
	if visibility == models.ResourceVisibilityGlobal &&
		(resource == resourceLists || resource == resourceCustomers || resource == resourceMedia) {
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
	user := auth.GetUser(c)
	if !ws.PlatformAdmin && ws.Role != models.OrganizationMemberRoleManager &&
		!user.HasPerm(auth.PermOrganizationsPlatformManage) {
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
	user := auth.GetUser(c)
	if !ws.PlatformAdmin && ws.Role != models.OrganizationMemberRoleManager &&
		!user.HasPerm(auth.PermOrganizationsPlatformManage) {
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

func (a *App) requirePlatformOrganizationAdmin(c echo.Context) error {
	user := auth.GetUser(c)
	if !user.IsPlatformAdmin() && !user.HasPerm(auth.PermOrganizationsPlatformManage) {
		return echo.NewHTTPError(http.StatusForbidden, "platform organization management permission required")
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
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	includeArchived := c.QueryParam("include_archived") == "true"
	out, err := a.core.GetOrganizations(includeArchived)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// CreateOrganization provisions an organization directly from the platform
// management screen. The caller chooses the first organization manager and
// may add initial members in the same transaction.
func (a *App) CreateOrganization(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	var req organizationCreateInput
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
	if req.ManagerUserID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization manager is required")
	}

	roles := make(map[int]string, len(req.MemberUserIDs)+len(req.Members)+1)
	roles[req.ManagerUserID] = models.OrganizationMemberRoleManager
	for _, userID := range req.MemberUserIDs {
		if userID < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "organization member user id is required")
		}
		if userID != req.ManagerUserID {
			roles[userID] = models.OrganizationMemberRoleMember
		}
	}
	for _, member := range req.Members {
		if member.UserID < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "organization member user id is required")
		}
		if member.UserID == req.ManagerUserID {
			continue
		}
		roles[member.UserID] = member.Role
	}
	assignments := make([]models.OrganizationMemberAssignment, 0, len(roles))
	for userID, role := range roles {
		assignments = append(assignments, models.OrganizationMemberAssignment{UserID: userID, Role: role})
	}

	out, err := a.core.CreateOrganizationWithMembers(auth.GetUser(c).ID, req.Name, req.Description, assignments)
	if err != nil {
		return err
	}
	setAuditOrganizationID(c, out.ID)
	setAuditObjectID(c, strconv.Itoa(out.ID))
	setAuditMetadata(c, map[string]any{"member_count": len(assignments)})
	return c.JSON(http.StatusCreated, okResp{out})
}

// AddOrganizationMembersBulk imports existing platform accounts into a
// selected organization. It resolves usernames/e-mail addresses server-side
// and commits the complete batch only after every row is valid.
func (a *App) AddOrganizationMembersBulk(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil || orgID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid organization id")
	}
	var req organizationMemberBulkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	rows := req.Members
	if len(rows) == 0 {
		rows = req.Users
	}
	if len(rows) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization member import is empty")
	}

	assignments := make([]models.OrganizationMemberAssignment, 0, len(rows))
	issues := make([]organizationMemberBulkIssue, 0)
	seen := make(map[int]struct{}, len(rows))
	for i, row := range rows {
		rowNumber := i + 2
		role := strings.ToLower(strings.TrimSpace(row.Role))
		if role == "" {
			role = models.OrganizationMemberRoleMember
		}
		if role != models.OrganizationMemberRoleMember && role != models.OrganizationMemberRoleManager {
			issues = append(issues, organizationMemberBulkIssue{Row: rowNumber, Field: "role", Code: "invalid_role"})
			continue
		}

		account := strings.TrimSpace(row.Account)
		if account == "" {
			account = strings.TrimSpace(row.Username)
		}
		userID := row.UserID
		if userID < 1 && account == "" {
			issues = append(issues, organizationMemberBulkIssue{Row: rowNumber, Field: "account", Code: "missing_account"})
			continue
		}
		if userID < 1 {
			userID, err = a.core.FindUserIDByAccount(account)
			if err != nil {
				if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code == http.StatusNotFound {
					issues = append(issues, organizationMemberBulkIssue{Row: rowNumber, Field: "account", Code: "account_not_found"})
					continue
				}
				return err
			}
		}
		if _, ok := seen[userID]; ok {
			issues = append(issues, organizationMemberBulkIssue{Row: rowNumber, Field: "account", Code: "duplicate_account"})
			continue
		}
		seen[userID] = struct{}{}
		assignments = append(assignments, models.OrganizationMemberAssignment{UserID: userID, Role: role})
	}
	if len(issues) > 0 {
		return c.JSON(http.StatusOK, okResp{organizationMemberBulkResponse{Errors: issues}})
	}

	added, err := a.core.AddOrganizationMembersBulk(orgID, assignments)
	if err != nil {
		return err
	}
	setAuditOrganizationID(c, orgID)
	setAuditMetadata(c, map[string]any{"added_count": added})
	return c.JSON(http.StatusOK, okResp{organizationMemberBulkResponse{Added: added, Errors: []organizationMemberBulkIssue{}}})
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
	setAuditObjectID(c, strconv.Itoa(out.ID))
	return c.JSON(http.StatusCreated, okResp{out})
}

func (a *App) GetOrganizationRequests(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	out, err := a.core.GetOrganizationRequests(c.QueryParam("include_resolved") == "true")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// GetMyOrganizationRequests exposes the current user's full creation-request
// history without granting access to requests made by other accounts.
func (a *App) GetMyOrganizationRequests(c echo.Context) error {
	out, err := a.core.GetUserOrganizationRequests(auth.GetUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) WithdrawOrganizationRequest(c echo.Context) error {
	out, err := a.core.WithdrawOrganizationRequest(getID(c), auth.GetUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ReviewOrganizationRequest(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
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
	if out.OrganizationID.Valid {
		setAuditOrganizationID(c, out.OrganizationID.Int)
	}
	return c.JSON(http.StatusOK, okResp{out})
}

func (a *App) ArchiveOrganization(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	orgID := getID(c)
	setAuditOrganizationID(c, orgID)
	stopped, err := a.core.ArchiveOrganization(orgID)
	if err != nil {
		return err
	}
	// An archived organization has no interactive member workspace, but the
	// former members' 263 inboxes must keep receiving customer replies. Retain
	// every mailbox referenced by an organization campaign and create the same
	// durable forwarding rule used when one member leaves.
	var replyMailboxOwners []int
	if err := a.db.Select(&replyMailboxOwners, `
		SELECT DISTINCT m.user_id
		FROM campaigns c
		JOIN reply_mailboxes m ON m.id = c.reply_mailbox_id
		WHERE c.organization_id = $1`, orgID); err != nil {
		return err
	}
	for _, ownerID := range replyMailboxOwners {
		if err := a.activateReplyForwardingForMember(orgID, ownerID); err != nil {
			// Archiving has already committed. Keep the operation successful and
			// surface a recoverable operational error rather than reporting a
			// false rollback to the platform administrator.
			a.log.Printf("unable to activate reply forwarding for archived organization %d, member %d: %v", orgID, ownerID, err)
		}
	}
	a.stopOrganizationImport(orgID, 0)
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused && a.manager != nil {
			a.manager.StopCampaign(campaign.ID, models.CampaignStatusPaused)
		}
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// PurgeArchivedOrganization permanently removes organization metadata only
// after all scoped resources have been transferred or cleaned up.
func (a *App) PurgeArchivedOrganization(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	orgID := getID(c)
	setAuditOrganizationID(c, orgID)
	if err := a.core.PurgeArchivedOrganization(orgID); err != nil {
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
	setAuditOrganizationID(c, out.ID)
	setAuditObjectID(c, strconv.Itoa(out.ID))
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
	setAuditOrganizationID(c, ws.OrganizationID)
	setAuditObjectID(c, strconv.Itoa(req.UserID))
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
	setAuditOrganizationID(c, ws.OrganizationID)
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
	if err := a.activateReplyForwardingForMember(ws.OrganizationID, userID); err != nil {
		return err
	}
	setAuditOrganizationID(c, ws.OrganizationID)
	// Stop manager goroutines after the transaction committed. The records are
	// already paused in the DB, so they cannot be picked up by a new worker.
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused && a.manager != nil {
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
	if err := a.activateReplyForwardingForMember(ws.OrganizationID, auth.GetUser(c).ID); err != nil {
		return err
	}
	setAuditOrganizationID(c, ws.OrganizationID)
	for _, campaign := range stopped {
		if campaign.Status == models.CampaignStatusPaused && a.manager != nil {
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
	setAuditOrganizationID(c, ws.OrganizationID)
	setAuditMetadata(c, map[string]any{"target_user_id": req.TargetUserID})
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
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
		return err
	}
	var req organizationTransferInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.TargetUserID < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "target user is required")
	}
	setAuditOrganizationID(c, getID(c))
	setAuditMetadata(c, map[string]any{"target_user_id": req.TargetUserID})
	if err := a.core.TransferArchivedOrganizationResourcesToPersonal(getID(c), req.TargetUserID); err != nil {
		return err
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{true})
}

// GetOrganizationMembersForPlatform exposes the transfer target customer_list for an
// archived organization only to platform administrators. Active organization
// managers continue to use the workspace-scoped member endpoint.
func (a *App) GetOrganizationMembersForPlatform(c echo.Context) error {
	if err := a.requirePlatformOrganizationAdmin(c); err != nil {
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
	setAuditOrganizationID(c, ws.OrganizationID)
	setAuditMetadata(c, map[string]any{"target_user_id": req.TargetUserID})
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
	setAuditOrganizationID(c, ws.OrganizationID)
	return c.JSON(http.StatusOK, okResp{true})
}

// MigratePersonalListsToOrganization copies or moves caller-owned personal
// customer_lists into a selected organization. The target defaults to the active
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
	if len(req.CustomerListIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one customer_list is required")
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
	setAuditOrganizationID(c, target.OrganizationID)
	if err := requireWritableWorkspace(target); err != nil {
		return err
	}
	if err := requireLegacyPermission(user, auth.PermListManageAll); err != nil {
		return err
	}
	// Migrating a retained personal resource is allowed even when the
	// workspaces:personal capability has been revoked, so users can move their
	// hidden personal data into an organization.
	personal, err := a.workspaceAccessForOrganizationWithPersonal(c, 0, false)
	if err != nil {
		return err
	}
	for _, id := range req.CustomerListIDs {
		if _, err := a.requireManagedWorkspaceList(c, personal, id); err != nil {
			return err
		}
	}
	customerListIDs, err := a.core.MigratePersonalListsToOrganization(user.ID, target.OrganizationID, user.ID, req.CustomerListIDs, req.Mode == "move")
	if err != nil {
		return err
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{struct {
		CustomerListIDs []int  `json:"customer_list_ids"`
		Mode            string `json:"mode"`
	}{CustomerListIDs: customerListIDs, Mode: req.Mode}})
}

// MigratePersonalResourcesToOrganization handles the common copy/move flow
// for private templates, draft campaigns, and media files. CustomerLists retain their
// existing endpoint for API compatibility, but use the same core migration
// implementation and subscription-merge guarantees.
func (a *App) MigratePersonalResourcesToOrganization(c echo.Context) error {
	user := auth.GetUser(c)
	active, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	var req organizationResourceMigrationInput
	if err := c.Bind(&req); err != nil {
		return err
	}
	req.Resource = strings.ToLower(strings.TrimSpace(req.Resource))
	if req.Resource != resourceLists && req.Resource != resourceTemplates &&
		req.Resource != resourceCampaigns && req.Resource != resourceMedia {
		return echo.NewHTTPError(http.StatusBadRequest, "unsupported personal resource type")
	}
	if req.Resource == resourceMedia {
		setAuditAction(c, "media.migrated")
	}
	if len(req.IDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one resource is required")
	}
	if req.Mode != "copy" && req.Mode != "move" {
		return echo.NewHTTPError(http.StatusBadRequest, "migration mode must be copy or move")
	}
	setAuditMetadata(c, map[string]any{
		"resource":       req.Resource,
		"resource_count": len(req.IDs),
		"mode":           req.Mode,
	})

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
	setAuditOrganizationID(c, target.OrganizationID)
	if err := requireWritableWorkspace(target); err != nil {
		return err
	}
	// Migrating a retained personal resource is allowed even when the
	// workspaces:personal capability has been revoked, so users can move their
	// hidden personal data into an organization.
	personal, err := a.workspaceAccessForOrganizationWithPersonal(c, 0, false)
	if err != nil {
		return err
	}
	for _, id := range req.IDs {
		switch req.Resource {
		case resourceLists:
			if _, err := a.requireManagedWorkspaceList(c, personal, id); err != nil {
				return err
			}
		case resourceTemplates:
			if _, err := a.requireManagedWorkspaceTemplate(c, personal, id); err != nil {
				return err
			}
		case resourceCampaigns:
			if _, err := a.requireManagedWorkspaceCampaign(c, personal, id); err != nil {
				return err
			}
		case resourceMedia:
			if _, err := a.requireManagedWorkspaceResource(c, personal, resourceMedia, id, auth.PermMediaManage); err != nil {
				return err
			}
		}
	}

	// A migration creates a resource in the target workspace, so the legacy
	// creation capability applies in addition to ownership of the source.
	switch req.Resource {
	case resourceLists:
		err = requireLegacyPermission(user, auth.PermListManageAll)
	case resourceTemplates:
		err = requireLegacyPermission(user, auth.PermTemplatesManage)
	case resourceCampaigns:
		err = requireLegacyPermission(user, auth.PermCampaignsManageAll, auth.PermCampaignsManage)
	case resourceMedia:
		err = requireLegacyPermission(user, auth.PermMediaManage)
	}
	if err != nil {
		return err
	}

	ids, err := a.core.MigratePersonalResourcesToOrganization(
		user.ID, target.OrganizationID, user.ID, req.Resource, req.IDs, req.Mode == "move",
	)
	if err != nil {
		return err
	}
	if req.Resource == resourceTemplates {
		a.cacheMigratedTransactionalTemplates(target, ids)
	}
	a.core.RefreshMatViews(true)
	return c.JSON(http.StatusOK, okResp{struct {
		Resource string `json:"resource"`
		IDs      []int  `json:"ids"`
		Mode     string `json:"mode"`
	}{Resource: req.Resource, IDs: ids, Mode: req.Mode}})
}

func (a *App) cacheMigratedTransactionalTemplates(access models.WorkspaceAccess, ids []int) {
	if a.manager == nil {
		return
	}
	for _, id := range ids {
		tpl, err := a.core.GetWorkspaceTemplate(access, id, false)
		if err != nil || tpl.Type != models.TemplateTypeTx {
			continue
		}
		if err := tpl.Compile(a.manager.GenericTemplateFuncs()); err != nil {
			a.log.Printf("error compiling migrated transactional template %d: %v", id, err)
			continue
		}
		a.manager.CacheTpl(id, &tpl)
	}
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
	setAuditOrganizationID(c, ws.OrganizationID)
	setAuditObjectID(c, strconv.Itoa(out.ID))
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
	setAuditOrganizationID(c, ws.OrganizationID)
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
