package main

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// canUsePersonalWorkspace reports whether the caller may enter their personal
// workspace. Platform administrators always retain it; every other user needs
// the workspaces:personal capability granted by their user role.
func canUsePersonalWorkspace(user auth.User) bool {
	return user.IsPlatformAdmin() || user.HasPerm(auth.PermWorkspacesPersonal)
}

// requirePersonalWorkspace enforces the personal workspace capability for
// paths that resolved the personal workspace independently of the standard
// workspace resolver.
func requirePersonalWorkspace(user auth.User) error {
	if canUsePersonalWorkspace(user) {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden, "personal workspace is disabled for this account")
}

// isReadOnlyMethod reports whether the request method cannot change state.
func isReadOnlyMethod(c echo.Context) bool {
	switch c.Request().Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// isPersonalMigrationListPath reports whether the request targets one of the
// resource customer_list endpoints that back the personal-resource migration UI. Only
// these exact paths may resolve the caller's personal workspace without the
// workspaces:personal capability, and only for read-only requests.
func isPersonalMigrationListPath(path string) bool {
	switch path {
	case "/api/customer-lists", "/api/templates", "/api/campaigns", "/api/media":
		return true
	}
	return false
}

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
	// Archived organizations are only exposed through the platform cleanup
	// workflow.  Keep this helper defensive because it is also called after a
	// resource-level read check and may see a row changed concurrently.
	if scope.OrganizationArchived {
		return false
	}
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
	if scope.OrganizationArchived {
		return false
	}
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
// becoming a way to duplicate another member's private work. Campaigns have a
// deliberately broader rule (see canCopyWorkspaceCampaign) and are kept out
// of this generic helper so template/media permissions do not widen with them.
func canCopyWorkspaceResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if scope.OrganizationArchived {
		return false
	}
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

// canCopyWorkspaceCampaign mirrors Core.CanCopyCampaign for the HTTP boundary.
// Keeping an explicit helper here lets the handler decide whether the legacy
// campaigns:manage role is still needed without changing the generic copy
// policy used by other resources.
func canCopyWorkspaceCampaign(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if scope.OrganizationArchived || scope.TransferPendingAt.Valid {
		return false
	}
	if access.PlatformAdmin || scope.Visibility == models.ResourceVisibilityGlobal {
		return true
	}
	if !scope.OrganizationID.Valid {
		return access.Personal && scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	if scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID {
		return true
	}
	return access.IsOrganizationManager()
}

// legacyReadableCustomerListIDs returns every customer_list that the caller may read under the
// pre-existing customer_list role model. A manage grant implies read capability; this
// is necessary for a user to work with a customer_list they are permitted to manage.
func legacyReadableCustomerListIDs(user auth.User) (bool, []int) {
	if user.IsPlatformAdmin() || user.HasPerm(auth.PermCustomersGetAll) || user.HasPerm(auth.PermListGetAll) || user.HasPerm(auth.PermListManageAll) {
		return true, nil
	}

	set := make(map[int]struct{}, len(user.GetCustomerListIDs)+len(user.ManageCustomerListIDs))
	for _, id := range user.GetCustomerListIDs {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	for _, id := range user.ManageCustomerListIDs {
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

func legacyManageableCustomerListIDs(user auth.User) (bool, []int) {
	if user.IsPlatformAdmin() || user.HasPerm(auth.PermListManageAll) {
		return true, nil
	}
	ids := make([]int, 0, len(user.ManageCustomerListIDs))
	for _, id := range user.ManageCustomerListIDs {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return false, ids
}

// managedWorkspaceLegacyCustomerListIDs returns the caller's mutable customer_lists after
// applying both workspace ownership and the legacy per-customer_list role. The SQL
// used by UpdateCustomerWithLists treats an empty permitted-customer_list slice as
// unrestricted, so a caller with no matching legacy grant must receive a
// sentinel instead of an empty slice.
func (a *App) managedWorkspaceLegacyCustomerListIDs(c echo.Context, access models.WorkspaceAccess) ([]int, error) {
	workspaceIDs, err := a.core.CustomerListManagedWorkspaceResources(access, resourceLists)
	if err != nil {
		return nil, err
	}

	hasAll, legacyIDs := legacyManageableCustomerListIDs(auth.GetUser(c))
	return intersectManagedWorkspaceLegacyCustomerListIDs(workspaceIDs, hasAll, legacyIDs), nil
}

func intersectManagedWorkspaceLegacyCustomerListIDs(workspaceIDs []int, hasAll bool, legacyIDs []int) []int {
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
		return echo.NewHTTPError(http.StatusBadRequest, "invalid customer_list id")
	}
	if manage {
		if user.IsPlatformAdmin() || user.HasPerm(auth.PermListManageAll) ||
			user.HasListPerm(auth.PermTypeManage, id) == nil {
			return nil
		}
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: customer_list:manage")
	}

	if user.IsPlatformAdmin() || user.HasPerm(auth.PermListGetAll) || user.HasPerm(auth.PermListManageAll) ||
		user.HasListPerm(auth.PermTypeGet, id) == nil || user.HasListPerm(auth.PermTypeManage, id) == nil {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden, "permission denied: customer_list:get")
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

// customerListInActiveWorkspace is the final boundary for customer-list IDs
// supplied to customer, import, bulk, and campaign requests. Platform admins
// can still inspect/manage resources broadly through the resource APIs, but a
// customer-list target must belong to the selected workspace. A caller-owned
// check is additionally required by customer creation/import because the new
// customer is stamped with the caller's owner identity.
func customerListInActiveWorkspace(access models.WorkspaceAccess, scope models.ResourceScope, ownerOnly bool) bool {
	if scope.TransferPendingAt.Valid || !scope.OwnerUserID.Valid {
		return false
	}
	if !access.IsOrganization() {
		return !scope.OrganizationID.Valid && int(scope.OwnerUserID.Int) == access.UserID
	}
	if !scope.OrganizationID.Valid || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	return !ownerOnly || int(scope.OwnerUserID.Int) == access.UserID
}

func customerListOutsideActiveWorkspaceError() error {
	return echo.NewHTTPError(http.StatusForbidden, "a selected customer_list is outside the active workspace")
}

func (a *App) requireReadableWorkspaceCustomer(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.requireReadableWorkspaceResource(c, access, resourceCustomers, id,
		auth.PermCustomersGetAll, auth.PermCustomersGet)
	if err != nil || workspaceReadException(access, scope) {
		return scope, err
	}
	user := auth.GetUser(c)
	if user.HasPerm(auth.PermCustomersGetAll) {
		return scope, nil
	}
	if err := a.hasSubPerm(access, user, []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireManagedWorkspaceCustomer(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	return a.requireManagedWorkspaceCustomerWithPermissions(c, access, id, auth.PermCustomersManage)
}

// requireManagedWorkspaceCustomerWithPermissions keeps the workspace and
// per-customer ownership checks in one place while allowing each destructive
// customer action to use its own business permission. Callers must pass the
// dedicated action permission; customer manage is reserved for create/edit.
func (a *App) requireManagedWorkspaceCustomerWithPermissions(c echo.Context, access models.WorkspaceAccess, id int, permissions ...string) (models.ResourceScope, error) {
	scope, err := a.requireManagedWorkspaceResource(c, access, resourceCustomers, id, permissions...)
	if err != nil {
		return scope, err
	}
	if err := a.hasManagedSubPerm(access, auth.GetUser(c), []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireExportableWorkspaceCustomer is intentionally stricter than a
// normal read: a CSV or profile export contains personal data, so it follows
// the mutable owner boundary. Organization managers cannot export another
// member's audience merely because they can inspect it.
func (a *App) requireExportableWorkspaceCustomer(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceCustomers, id)
	if err != nil {
		return scope, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCustomersGetAll, auth.PermCustomersGet); err != nil {
		return scope, err
	}
	if user.HasPerm(auth.PermCustomersGetAll) {
		return scope, nil
	}
	if err := a.hasSubPerm(access, user, []int{id}); err != nil {
		return scope, err
	}
	return scope, nil
}

// campaignHasLegacyListAccess retains the old campaign-to-customer_list entitlement
// model after ownership has already been checked. Freshly cloned drafts have
// no customer_lists by design, and remain editable by their owner with campaign manage
// permission so a recipient customer_list can be selected afterwards.
func (a *App) campaignHasLegacyListAccess(access models.WorkspaceAccess, user auth.User, id int, manage bool) error {
	ok, err := a.hasLegacyCampaignListAccess(access, user, id, manage)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied: campaign customer_lists")
	}
	return nil
}

func (a *App) hasLegacyCampaignListAccess(access models.WorkspaceAccess, user auth.User, id int, manage bool) (bool, error) {
	if manage && user.HasPerm(auth.PermCampaignsManageAll) {
		return true, nil
	}
	if !manage && user.HasPerm(auth.PermCampaignsGetAll) {
		return true, nil
	}
	hasAll, customerListIDs := legacyReadableCustomerListIDs(user)
	if hasAll {
		return true, nil
	}
	campaignCustomerListIDs, err := a.core.GetCampaignCustomerListIDsInWorkspace(access, id)
	if err != nil {
		return false, err
	}
	if len(campaignCustomerListIDs) == 0 {
		return true, nil
	}
	ok, err := a.core.CampaignHasListsInWorkspace(access, id, customerListIDs)
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
	if err := a.campaignHasLegacyListAccess(access, user, id, false); err != nil {
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
	if err := a.campaignHasLegacyListAccess(access, user, id, true); err != nil {
		return scope, err
	}
	return scope, nil
}

// requireSensitiveWorkspaceCampaign protects recipient identities. Aggregate
// analytics can be inspected by organization managers, but recipient details
// remain available only to the campaign owner with the pre-existing analytics
// and customer_list grants.
func (a *App) requireSensitiveWorkspaceCampaign(c echo.Context, access models.WorkspaceAccess, id int) (models.ResourceScope, error) {
	scope, err := a.core.RequireManageResource(access, resourceCampaigns, id)
	if err != nil {
		return scope, err
	}
	user := auth.GetUser(c)
	if err := requireLegacyPermission(user, auth.PermCampaignsGetAnalytics); err != nil {
		return scope, err
	}
	if err := a.campaignHasLegacyListAccess(access, user, id, false); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) requireCampaignAnalytics(c echo.Context, access models.WorkspaceAccess, id int) error {
	scope, err := a.core.RequireReadResource(access, resourceCampaigns, id)
	if err != nil {
		return err
	}

	// Platform administrators retain their global oversight capability.  An
	// organization manager may inspect aggregate statistics only for campaigns
	// that belong to the currently selected organization (including a pending
	// transfer row, which is intentionally kept visible for hand-off).  A
	// public/global campaign being readable is not, by itself, an analytics
	// grant for an ordinary member.
	if access.PlatformAdmin {
		return nil
	}
	if access.IsOrganization() && access.IsOrganizationManager() && scope.OrganizationID.Valid &&
		int(scope.OrganizationID.Int) == access.OrganizationID {
		return nil
	}

	// Outside the manager path, analytics are owner-scoped.  This check is
	// deliberately made after RequireReadResource so a global campaign cannot
	// turn an owner ID into a cross-workspace read, and a pending personal row
	// cannot be resurrected by a stale request.
	user := auth.GetUser(c)
	// The owner check must also honor the selected workspace.  Without this
	// second guard, a globally published campaign owned by the caller could be
	// reported while an unrelated organization workspace is active, bypassing
	// the organization + owner isolation used by every other mutation.
	if !a.core.CanManageResource(access, scope) || !scope.OwnerUserID.Valid || int(scope.OwnerUserID.Int) != user.ID {
		return echo.NewHTTPError(http.StatusForbidden, "campaign analytics are limited to the campaign owner")
	}
	if err := requireLegacyPermission(user, auth.PermCampaignsGetAnalytics); err != nil {
		return err
	}
	return a.campaignHasLegacyListAccess(access, user, id, false)
}

// queryReadableWorkspaceLists filters legacy customer_list grants before pagination.
// Workspace predicates run in Core first, so customer_list-role IDs can only narrow a
// caller's active workspace and can never grant access across an organization.
func (a *App) queryReadableWorkspaceLists(c echo.Context, access models.WorkspaceAccess, search, typ, optin, status string, tags []string, orderBy, order string, offset, limit int) ([]models.CustomerList, int, error) {
	all, _, err := a.core.QueryWorkspaceLists(access, search, typ, optin, status, tags, orderBy, order, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	// First-class public pools have an independent delivery grant and may be
	// stored in the platform administrator's workspace. Add only minimal pool
	// metadata for organizations that can deliver them; contact endpoints apply
	// the separate masking/detail policy.
	if typ == "" || typ == models.CustomerListTypePool || typ == models.CustomerListTypeOrgPoolAllocation {
		poolLists, poolErr := a.core.QueryAuthorizedPoolLists(access)
		if poolErr != nil {
			return nil, 0, poolErr
		}
		known := make(map[int]struct{}, len(all))
		for _, l := range all {
			known[l.ID] = struct{}{}
		}
		for _, l := range poolLists {
			if _, ok := known[l.ID]; !ok && (typ == "" || l.Type == typ) {
				all = append(all, l)
			}
		}
	}

	user := auth.GetUser(c)
	hasAll, ids := legacyReadableCustomerListIDs(user)
	permitted := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		permitted[id] = struct{}{}
	}
	filtered := make([]models.CustomerList, 0, len(all))
	for _, customer_list := range all {
		if customer_list.PoolDeliveryAllowed {
			filtered = append(filtered, customer_list)
			continue
		}
		if workspaceReadException(access, customer_list.ResourceScope) || hasAll {
			filtered = append(filtered, customer_list)
			continue
		}
		if _, ok := permitted[customer_list.ID]; ok {
			filtered = append(filtered, customer_list)
		}
	}

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []models.CustomerList{}, total, nil
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return filtered[offset:end], total, nil
}

// queryReadableWorkspaceCampaigns filters after the fixed workspace query but
// before pagination. This prevents restricted role/customer_list grants from learning
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
		ok, err := a.hasLegacyCampaignListAccess(access, user, campaign.ID, false)
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
