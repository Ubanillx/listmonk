package main

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// requireLegacyPermission preserves the pre-workspace role model. Workspace
// checks always happen in the caller before this helper is used, so a global
// role can never widen a personal or organization resource boundary.
func requireLegacyPermission(user auth.User, permissions ...string) error {
	for _, permission := range permissions {
		if user.HasPerm(permission) {
			return nil
		}
	}
	if len(permissions) == 0 {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied")
	}
	return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("permission denied: %s", permissions[0]))
}

func hasLegacyPermission(user auth.User, permissions ...string) bool {
	return requireLegacyPermission(user, permissions...) == nil
}

// workspaceReadException is limited to the read-only sharing rules confirmed
// for this deployment. Globally published resources remain viewable/copyable
// by logged-in users, and organization managers may inspect organization
// resources. Organization-shared resources remain readable to members of the
// current organization, as required by their publication scope.
// Neither exception is valid for mutations, exports, imports, bulk actions,
// or sends.
func workspaceReadException(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return true
	}
	if !scope.OrganizationID.Valid {
		return scope.Visibility == models.ResourceVisibilityGlobal && !scope.TransferPendingAt.Valid
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return scope.Visibility == models.ResourceVisibilityGlobal && !scope.TransferPendingAt.Valid
	}
	if scope.TransferPendingAt.Valid {
		return access.IsOrganizationManager()
	}
	if scope.Visibility == models.ResourceVisibilityGlobal ||
		scope.Visibility == models.ResourceVisibilityOrganization {
		return true
	}
	return access.IsOrganizationManager()
}

// workspaceCopyException intentionally excludes the manager-only inspection
// path for resources left behind by former members. Published organization and
// global resources may be copied by their audience, but pending-transfer rows
// may only be inspected and transferred by an organization manager.
func workspaceCopyException(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if scope.TransferPendingAt.Valid {
		return false
	}
	if scope.Visibility == models.ResourceVisibilityGlobal {
		return true
	}
	return scope.Visibility == models.ResourceVisibilityOrganization &&
		scope.OrganizationID.Valid && access.IsOrganization() &&
		int(scope.OrganizationID.Int) == access.OrganizationID
}

// canCopyWorkspaceResource gives the resource owner the normal private-copy
// path while preventing an organization manager's inspection-only access from
// becoming a way to duplicate another member's private work.
func canCopyWorkspaceResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if workspaceCopyException(access, scope) {
		return true
	}
	if access.PlatformAdmin {
		return !scope.TransferPendingAt.Valid
	}
	if scope.TransferPendingAt.Valid || !scope.OwnerUserID.Valid || int(scope.OwnerUserID.Int) != access.UserID {
		return false
	}
	if !scope.OrganizationID.Valid {
		return access.Personal
	}
	return access.IsOrganization() && int(scope.OrganizationID.Int) == access.OrganizationID
}

// legacyReadableListIDs returns every list that the caller may read under the
// pre-existing list role model. A manage grant implies read capability; this
// is necessary for a user to work with a list they are permitted to manage.
func legacyReadableListIDs(user auth.User) (bool, []int) {
	if user.IsPlatformAdmin() || user.HasPerm(auth.PermListGetAll) || user.HasPerm(auth.PermListManageAll) {
		return true, nil
	}

	set := make(map[int]struct{}, len(user.GetListIDs)+len(user.ManageListIDs))
	for _, id := range user.GetListIDs {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	for _, id := range user.ManageListIDs {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	ids := make([]int, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return false, ids
}

// legacyReadableSubscriberListIDs applies the historical subscriber-wide
// grant before falling back to per-list grants. A user with
// subscribers:get_all may read every subscriber in its active workspace;
// narrower roles are limited to the lists explicitly granted to them.
func legacyReadableSubscriberListIDs(user auth.User) (bool, []int) {
	if user.IsPlatformAdmin() || user.HasPerm(auth.PermSubscribersGetAll) {
		return true, nil
	}
	return legacyReadableListIDs(user)
}

func legacyManageableListIDs(user auth.User) (bool, []int) {
	if user.IsPlatformAdmin() || user.HasPerm(auth.PermListManageAll) {
		return true, nil
	}
	ids := make([]int, 0, len(user.ManageListIDs))
	for _, id := range user.ManageListIDs {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return false, ids
}

// managedWorkspaceLegacyListIDs returns the caller's mutable lists after
// applying both workspace ownership and the legacy per-list role. The SQL
// used by UpdateSubscriberWithLists treats an empty permitted-list slice as
// unrestricted, so a caller with no matching legacy grant must receive a
// sentinel instead of an empty slice.
func (a *App) managedWorkspaceLegacyListIDs(c echo.Context, access models.WorkspaceAccess) ([]int, error) {
	workspaceIDs, err := a.core.ListManagedWorkspaceResources(access, resourceLists)
	if err != nil {
		return nil, err
	}

	hasAll, legacyIDs := legacyManageableListIDs(auth.GetUser(c))
	return intersectManagedWorkspaceLegacyListIDs(workspaceIDs, hasAll, legacyIDs), nil
}

func intersectManagedWorkspaceLegacyListIDs(workspaceIDs []int, hasAll bool, legacyIDs []int) []int {
	if hasAll {
		if len(workspaceIDs) == 0 {
			return []int{-1}
		}
		return workspaceIDs
	}
	allowed := make(map[int]struct{}, len(legacyIDs))
	for _, id := range legacyIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]int, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		if _, ok := allowed[id]; ok {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return []int{-1}
	}
	return filtered
}

func requireLegacyListPermission(user auth.User, id int, manage bool) error {
	if id < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid list id")
	}
	if manage {
		if user.IsPlatformAdmin() || user.HasPerm(auth.PermListManageAll) ||
			user.HasListPerm(auth.PermTypeManage, id) == nil {
			return nil
		}
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: list:manage")
	}

	if user.IsPlatformAdmin() || user.HasPerm(auth.PermListGetAll) || user.HasPerm(auth.PermListManageAll) ||
		user.HasListPerm(auth.PermTypeGet, id) == nil || user.HasListPerm(auth.PermTypeManage, id) == nil {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden, "permission denied: list:get")
}

func (a *App) requireReadableWorkspaceResource(c echo.Context, access models.WorkspaceAccess, resource string, id int, permissions ...string) (models.ResourceScope, error) {
	scope, err := a.core.RequireReadResource(access, resource, id)
	if err != nil {
		return scope, err
	}
	if workspaceReadException(access, scope) {
		return scope, nil
	}
	if err := requireLegacyPermission(auth.GetUser(c), permissions...); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireManagedWorkspaceResource(c echo.Context, access models.WorkspaceAccess, resource string, id int, permissions ...string) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resource, id)
	if err != nil {
		return scope, err
	}
	if err := requireLegacyPermission(auth.GetUser(c), permissions...); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireManagedWorkspaceTemplate keeps the documented global-template
// publishing rule narrow. Any signed-in creator may maintain a template that
// remains globally shared, while private and organization templates still use
// the existing templates:manage role after Core has established ownership.
func (a *App) requireManagedWorkspaceTemplate(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceTemplates, id)
	if err != nil {
		return scope, err
	}
	if scope.Visibility == models.ResourceVisibilityGlobal {
		return scope, nil
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermTemplatesManage); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireUsableWorkspaceResource is the sending/attachment counterpart to a
// readable resource check. Globally and organization-published resources are
// explicitly usable by their audience; private resources still require the
// legacy resource grant after Core has verified the active owner/workspace.
func (a *App) requireUsableWorkspaceResource(c echo.Context, access models.WorkspaceAccess, resource string, id int, permissions ...string) (models.ResourceScope, error) {
	scope, err := a.core.RequireUseResource(access, resource, id)
	if err != nil {
		return scope, err
	}
	if workspaceReadException(access, scope) {
		return scope, nil
	}
	if err := requireLegacyPermission(auth.GetUser(c), permissions...); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireReadableWorkspaceList(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireReadResource(access, resourceLists, id)
	if err != nil {
		return scope, err
	}
	if workspaceReadException(access, scope) {
		return scope, nil
	}
	if err := requireLegacyListPermission(auth.GetUser(c), id, false); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireManagedWorkspaceList(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceLists, id)
	if err != nil {
		return scope, err
	}
	if err := requireLegacyListPermission(auth.GetUser(c), id, true); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireReadableWorkspaceSubscriber(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.requireReadableWorkspaceResource(c, access, resourceSubscribers, id,
		auth.PermSubscribersGetAll, auth.PermSubscribersGet)
	if err != nil || workspaceReadException(access, scope) {
		return scope, err
	}
	user := auth.GetUser(c)
	if user.HasPerm(auth.PermSubscribersGetAll) {
		return scope, nil
	}
	if err := a.hasSubPerm(user, []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireManagedWorkspaceSubscriber(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.requireManagedWorkspaceResource(c, access, resourceSubscribers, id, auth.PermSubscribersManage)
	if err != nil {
		return scope, err
	}
	if err := a.hasManagedSubPerm(auth.GetUser(c), []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireExportableWorkspaceSubscriber is intentionally stricter than a
// normal read: a CSV or profile export contains personal data, so it follows
// the mutable owner boundary. Organization managers cannot export another
// member's audience merely because they can inspect it.
func (a *App) requireExportableWorkspaceSubscriber(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceSubscribers, id)
	if err != nil {
		return scope, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermSubscribersGetAll, auth.PermSubscribersGet); err != nil {
		return scope, err
	}
	if user.HasPerm(auth.PermSubscribersGetAll) {
		return scope, nil
	}
	if err := a.hasSubPerm(user, []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

// campaignHasLegacyListAccess retains the old campaign-to-list entitlement
// model after ownership has already been checked. Freshly cloned drafts have
// no lists by design, and remain editable by their owner with campaign manage
// permission so a recipient list can be selected afterwards.
func (a *App) campaignHasLegacyListAccess(user auth.User, id int, manage bool) error {
	ok, err := a.hasLegacyCampaignListAccess(user, id, manage)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: campaign lists")
	}
	return nil
}

func (a *App) hasLegacyCampaignListAccess(user auth.User, id int, manage bool) (bool, error) {
	if manage && user.HasPerm(auth.PermCampaignsManageAll) {
		return true, nil
	}
	if !manage && user.HasPerm(auth.PermCampaignsGetAll) {
		return true, nil
	}
	hasAll, listIDs := legacyReadableListIDs(user)
	if hasAll {
		return true, nil
	}
	campaignListIDs, err := a.core.GetCampaignListIDs(id)
	if err != nil {
		return false, err
	}
	if len(campaignListIDs) == 0 {
		return true, nil
	}
	ok, err := a.core.CampaignHasLists(id, listIDs)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (a *App) requireReadableWorkspaceCampaign(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireReadResource(access, resourceCampaigns, id)
	if err != nil {
		return scope, err
	}
	if workspaceReadException(access, scope) {
		return scope, nil
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsGetAll, auth.PermCampaignsGet); err != nil {
		return scope, err
	}
	if err := a.campaignHasLegacyListAccess(user, id, false); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireManagedWorkspaceCampaign(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceCampaigns, id)
	if err != nil {
		return scope, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
		return scope, err
	}
	if err := a.campaignHasLegacyListAccess(user, id, true); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireSensitiveWorkspaceCampaign protects recipient identities. Aggregate
// analytics can be inspected by organization managers, but recipient details
// remain available only to the campaign owner with the pre-existing analytics
// and list grants.
func (a *App) requireSensitiveWorkspaceCampaign(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceCampaigns, id)
	if err != nil {
		return scope, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsGetAnalytics); err != nil {
		return scope, err
	}
	if err := a.campaignHasLegacyListAccess(user, id, false); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireCampaignAnalytics(c echo.Context, access models.WorkspaceAccess, id int) error {
	scope, err := a.core.RequireReadResource(access, resourceCampaigns, id)
	if err != nil {
		return err
	}
	if access.IsOrganization() && access.IsOrganizationManager() && scope.OrganizationID.Valid &&
		int(scope.OrganizationID.Int) == access.OrganizationID {
		return nil
	}
	if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsGetAnalytics); err != nil {
		return err
	}
	return a.campaignHasLegacyListAccess(auth.GetUser(c), id, false)
}

// queryReadableWorkspaceLists filters legacy list grants before pagination.
// Workspace predicates run in Core first, so list-role IDs can only narrow a
// caller's active workspace and can never grant access across an organization.
func (a *App) queryReadableWorkspaceLists(c echo.Context, access models.WorkspaceAccess, search, typ, optin, status string, tags []string, orderBy, order string, offset, limit int) ([]models.List, int, error) {
	all, _, err := a.core.QueryWorkspaceLists(access, search, typ, optin, status, tags, orderBy, order, 0, 0)
	if err != nil {
		return nil, 0, err
	}

	user := auth.GetUser(c)
	hasAll, ids := legacyReadableListIDs(user)
	permitted := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		permitted[id] = struct{}{}
	}
	filtered := make([]models.List, 0, len(all))
	for _, list := range all {
		if workspaceReadException(access, list.ResourceScope) || hasAll {
			filtered = append(filtered, list)
			continue
		}
		if _, ok := permitted[list.ID]; ok {
			filtered = append(filtered, list)
		}
	}

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []models.List{}, total, nil
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return filtered[offset:end], total, nil
}

// queryReadableWorkspaceCampaigns filters after the fixed workspace query but
// before pagination. This prevents restricted role/list grants from learning
// about inaccessible campaign rows through total counts or page boundaries.
func (a *App) queryReadableWorkspaceCampaigns(c echo.Context, access models.WorkspaceAccess, search string, statuses, tags []string, orderBy, order string, offset, limit int) (models.Campaigns, int, error) {
	all, _, err := a.core.QueryWorkspaceCampaigns(access, search, statuses, tags, orderBy, order, 0, 0)
	if err != nil {
		return nil, 0, err
	}

	user := auth.GetUser(c)
	canReadByRole := hasLegacyPermission(user, auth.PermCampaignsGetAll, auth.PermCampaignsGet)
	filtered := make(models.Campaigns, 0, len(all))
	for _, campaign := range all {
		if workspaceReadException(access, campaign.ResourceScope) {
			filtered = append(filtered, campaign)
			continue
		}
		if !canReadByRole {
			continue
		}
		ok, err := a.hasLegacyCampaignListAccess(user, campaign.ID, false)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			filtered = append(filtered, campaign)
		}
	}

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return models.Campaigns{}, total, nil
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return filtered[offset:end], total, nil
}
