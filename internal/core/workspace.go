package core

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	resourceLists       = "lists"
	resourceSubscribers = "subscribers"
	resourceTemplates   = "templates"
	resourceCampaigns   = "campaigns"
	resourceMedia       = "media"
)

var workspaceResourceTables = map[string]string{
	resourceLists:       "lists",
	resourceSubscribers: "subscribers",
	resourceTemplates:   "templates",
	resourceCampaigns:   "campaigns",
	resourceMedia:       "media",
}

// GetResourceScope returns scope metadata without exposing the resource body.
// Every resource endpoint calls this before it reads, changes, exports, or
// sends data so the scope boundary remains server enforced.
func (c *Core) GetResourceScope(resource string, id int) (models.ResourceScope, error) {
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return models.ResourceScope{}, echo.NewHTTPError(http.StatusInternalServerError, "unknown workspace resource")
	}
	var out models.ResourceScope
	q := fmt.Sprintf(`
		SELECT r.organization_id, r.owner_user_id, r.original_owner_user_id, r.visibility, r.transfer_pending_at,
			(r.organization_id IS NOT NULL AND COALESCE(o.status, 'archived') <> 'active') AS organization_archived,
			COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name
		FROM %s r
		LEFT JOIN organizations o ON o.id = r.organization_id
		LEFT JOIN users u ON u.id = COALESCE(r.owner_user_id, r.original_owner_user_id)
		WHERE r.id = $1`, table)
	if err := c.db.Get(&out, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("error fetching resource scope: %s", pqErrMsg(err)))
	}
	return out, nil
}

// CanReadResource checks membership, ownership, public visibility, and the
// special manager read-only access. Personal resources are never visible to a
// non-owner except platform administrators.
func (c *Core) CanReadResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return true
	}
	if scope.OrganizationArchived {
		return false
	}
	if !scope.OrganizationID.Valid {
		if scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID && access.Personal && !scope.TransferPendingAt.Valid {
			return true
		}
		return scope.Visibility == models.ResourceVisibilityGlobal && !scope.TransferPendingAt.Valid
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return scope.Visibility == models.ResourceVisibilityGlobal && scope.TransferPendingAt.Valid == false
	}
	// Resources left behind by a departed member intentionally remain visible
	// to organization managers so they can inspect and transfer them. They are
	// otherwise hidden from every member, including users who could previously
	// read an organization-shared or global resource.
	if scope.TransferPendingAt.Valid {
		return access.IsOrganizationManager()
	}
	if scope.Visibility == models.ResourceVisibilityGlobal || scope.Visibility == models.ResourceVisibilityOrganization {
		return true
	}
	if scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID {
		return true
	}
	return access.IsOrganizationManager()
}

// CanUseResource is the stricter access rule for a resource that will become
// part of a sent message. Organization managers can inspect a member's private
// resources, but that read-only oversight must never let them send with a
// member's private template or media. Shared and global resources remain
// usable by everyone who can access their workspace.
func (c *Core) CanUseResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return true
	}
	if scope.OrganizationArchived {
		return false
	}
	if scope.TransferPendingAt.Valid {
		return false
	}
	if scope.Visibility == models.ResourceVisibilityGlobal {
		return true
	}
	if !scope.OwnerUserID.Valid {
		return false
	}
	if !scope.OrganizationID.Valid {
		return access.Personal && int(scope.OwnerUserID.Int) == access.UserID
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	if int(scope.OwnerUserID.Int) == access.UserID {
		return true
	}
	return scope.Visibility == models.ResourceVisibilityOrganization
}

// CanCopyResource applies the copy rules independently from read and manage
// access. This is the generic rule used by resources such as templates and
// media: organization managers may inspect another member's private resource
// but must not duplicate it; only an owner, an explicitly shared resource, or
// a global resource can be used as a copy source. The core clone operations use
// this check again while holding database locks so a stale HTTP authorization
// result cannot widen the copy boundary.
func (c *Core) CanCopyResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return !scope.OrganizationArchived && !scope.TransferPendingAt.Valid
	}
	if scope.OrganizationArchived || scope.TransferPendingAt.Valid {
		return false
	}
	if scope.Visibility == models.ResourceVisibilityGlobal {
		return true
	}
	if !scope.OrganizationID.Valid {
		// Personal resources can only be copied by their owner while the
		// personal workspace is selected.
		return access.Personal && scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	// Organization-published resources are explicitly copyable by every
	// active member (including managers). Private resources remain owner-only;
	// manager read-only inspection must never become a copy capability.
	if scope.Visibility == models.ResourceVisibilityOrganization {
		return true
	}
	return scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID
}

// CanCopyCampaign is the campaign-specific copy rule. Campaigns are the one
// resource for which the product explicitly allows copying anything the caller
// can inspect. In particular, an organization manager's read-only inspection
// of a member's private campaign is sufficient to create an independent draft.
// Pending-transfer rows remain transfer/cleanup records rather than copy
// sources, and archived organizations are never valid sources.
//
// Keep this separate from CanCopyResource: templates, media, and other
// resources retain their stricter owner/shared-copy semantics. Clone callers
// invoke this again inside their transaction after locking the source row.
func (c *Core) CanCopyCampaign(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return !scope.OrganizationArchived && !scope.TransferPendingAt.Valid
	}
	if scope.OrganizationArchived || scope.TransferPendingAt.Valid {
		return false
	}
	if scope.Visibility == models.ResourceVisibilityGlobal {
		return true
	}
	if !scope.OrganizationID.Valid {
		// Personal campaigns remain private to their owner. A global campaign
		// was handled above and may be copied from any signed-in workspace.
		return access.Personal && scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	// Members can copy their own/private campaigns and published campaigns;
	// managers additionally have copy capability for the private campaigns
	// they are allowed to inspect.
	if scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID {
		return true
	}
	return access.IsOrganizationManager()
}

// CanReadOwnerScopedResource is the access rule for lists and subscribers.
// These resources deliberately do not honor organization/global visibility:
// a member's audience remains private to that member, while organization
// managers retain the documented read-only oversight access.
func (c *Core) CanReadOwnerScopedResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.PlatformAdmin {
		return true
	}
	if scope.OrganizationArchived {
		return false
	}
	if !scope.OrganizationID.Valid {
		return access.Personal && scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID && !scope.TransferPendingAt.Valid
	}
	if !access.IsOrganization() || int(scope.OrganizationID.Int) != access.OrganizationID {
		return false
	}
	if scope.TransferPendingAt.Valid {
		return access.IsOrganizationManager()
	}
	if scope.OwnerUserID.Valid && int(scope.OwnerUserID.Int) == access.UserID {
		return true
	}
	return access.IsOrganizationManager()
}

// CanManageResource grants write access only to the owner in the selected
// workspace, except platform administrators. Organization managers deliberately
// never inherit write access to other members' resources.
func (c *Core) CanManageResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	if access.Archived {
		return false
	}
	// An archived organization is a read/cleanup boundary.  A request may
	// race with organization archival after its workspace membership was
	// resolved, so do not let an owner mutate a row that is now archived.
	if scope.OrganizationArchived {
		return false
	}
	if access.PlatformAdmin {
		return true
	}
	if !scope.OwnerUserID.Valid || int(scope.OwnerUserID.Int) != access.UserID || scope.TransferPendingAt.Valid {
		return false
	}
	if !scope.OrganizationID.Valid {
		return access.Personal
	}
	return access.IsOrganization() && int(scope.OrganizationID.Int) == access.OrganizationID
}

// CanSeeSensitiveResource controls recipient e-mails, sending lists, mail
// headers, sender identity and CSV export. A manager may inspect a member's
// campaign or subscriber detail but never its sensitive delivery data.
func (c *Core) CanSeeSensitiveResource(access models.WorkspaceAccess, scope models.ResourceScope) bool {
	return c.CanManageResource(access, scope) || access.PlatformAdmin
}

func (c *Core) RequireReadResource(access models.WorkspaceAccess, resource string, id int) (models.ResourceScope, error) {
	scope, err := c.GetResourceScope(resource, id)
	if err != nil {
		return scope, err
	}
	canRead := c.CanReadResource(access, scope)
	if resource == resourceLists || resource == resourceSubscribers {
		canRead = c.CanReadOwnerScopedResource(access, scope)
	}
	if !canRead {
		return scope, echo.NewHTTPError(http.StatusForbidden, "resource is outside the active workspace")
	}
	return scope, nil
}

// RequireUseResource enforces the boundary for resources attached to a
// campaign or transactional message. It intentionally differs from read
// access so a manager's inspection rights cannot become a send capability.
func (c *Core) RequireUseResource(access models.WorkspaceAccess, resource string, id int) (models.ResourceScope, error) {
	scope, err := c.GetResourceScope(resource, id)
	if err != nil {
		return scope, err
	}
	if !c.CanUseResource(access, scope) {
		return scope, echo.NewHTTPError(http.StatusForbidden, "resource cannot be used for sending in the active workspace")
	}
	return scope, nil
}

// CanUseTemplateMedia permits a media file to travel with a template whose
// publication scope grants send access. Media rows normally must themselves
// be usable in the active workspace; however, a shared/global template may
// intentionally contain a private image owned by its author. That image is
// part of the template's published payload and must remain sendable to the
// template audience, while an arbitrary private media row must never become
// usable merely because its ID was supplied by a client.
func (c *Core) CanUseTemplateMedia(access models.WorkspaceAccess, templateID, mediaID int) (bool, error) {
	if templateID < 1 || mediaID < 1 {
		return false, nil
	}
	mediaScope, err := c.GetResourceScope(resourceMedia, mediaID)
	if err != nil {
		return false, err
	}
	if c.CanUseResource(access, mediaScope) {
		return true, nil
	}
	// A transferred or archived binary must not be resurrected through a stale
	// template association, even when the template itself remains readable.
	if mediaScope.TransferPendingAt.Valid || mediaScope.OrganizationArchived {
		return false, nil
	}
	templateScope, err := c.GetResourceScope(resourceTemplates, templateID)
	if err != nil {
		return false, err
	}
	if !c.CanUseResource(access, templateScope) || templateScope.TransferPendingAt.Valid || templateScope.OrganizationArchived {
		return false, nil
	}
	// Keep the derived grant within the template's workspace and ownership
	// graph. This allows an author's private media in a shared template, but
	// rejects a forged cross-organization template_media row.
	if mediaScope.OrganizationID.Valid != templateScope.OrganizationID.Valid {
		return false, nil
	}
	if mediaScope.OrganizationID.Valid && mediaScope.OrganizationID.Int != templateScope.OrganizationID.Int {
		return false, nil
	}
	if mediaScope.Visibility != models.ResourceVisibilityOrganization &&
		(!mediaScope.OwnerUserID.Valid || !templateScope.OwnerUserID.Valid ||
			mediaScope.OwnerUserID.Int != templateScope.OwnerUserID.Int) {
		return false, nil
	}
	var linked bool
	if err := c.db.Get(&linked, `
		SELECT EXISTS(
			SELECT 1 FROM template_media
			WHERE template_id = $1 AND media_id = $2
		)`, templateID, mediaID); err != nil {
		return false, workspaceQueryError("checking template media", err)
	}
	return linked, nil
}

func (c *Core) RequireManageResource(access models.WorkspaceAccess, resource string, id int) (models.ResourceScope, error) {
	scope, err := c.GetResourceScope(resource, id)
	if err != nil {
		return scope, err
	}
	if !c.CanManageResource(access, scope) {
		return scope, echo.NewHTTPError(http.StatusForbidden, "only the resource owner can make this change")
	}
	return scope, nil
}

// ApplyWorkspaceScope stamps a newly created resource with the active
// workspace and caller. New resources default to private even in an
// organization; callers must explicitly publish them.
func ApplyWorkspaceScope(access models.WorkspaceAccess, requestedVisibility string) models.ResourceScope {
	visibility := requestedVisibility
	if visibility == "" {
		visibility = models.ResourceVisibilityPrivate
	}
	if visibility != models.ResourceVisibilityPrivate && visibility != models.ResourceVisibilityOrganization && visibility != models.ResourceVisibilityGlobal {
		visibility = models.ResourceVisibilityPrivate
	}
	if !access.IsOrganization() {
		// Personal resources cannot be published into a non-existent org. Global
		// templates and campaigns are allowed from personal space.
		if visibility == models.ResourceVisibilityOrganization {
			visibility = models.ResourceVisibilityPrivate
		}
		return models.ResourceScope{
			OwnerUserID:         nullInt(access.UserID),
			OriginalOwnerUserID: nullInt(access.UserID),
			Visibility:          visibility,
		}
	}
	return models.ResourceScope{
		OrganizationID:      nullInt(access.OrganizationID),
		OwnerUserID:         nullInt(access.UserID),
		OriginalOwnerUserID: nullInt(access.UserID),
		Visibility:          visibility,
	}
}

func nullInt(v int) modelsNullableInt {
	return modelsNullableInt{Int: v, Valid: true}
}

// modelsNullableInt allows the helper to avoid exporting a duplicate null
// constructor. It is converted by the compiler through the matching alias in
// models.ResourceScope fields below.
type modelsNullableInt = null.Int

// ListWorkspaceResources returns only resources visible within the active
// workspace. Managers see member resources in their organization, while
// ordinary members see their own resources plus explicitly shared resources.
func (c *Core) ListWorkspaceResources(access models.WorkspaceAccess, resource string) ([]int, error) {
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "unknown workspace resource")
	}
	var ids []int
	if access.PlatformAdmin {
		if err := c.db.Select(&ids, fmt.Sprintf(`SELECT id FROM %s`, table)); err != nil {
			return nil, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
		}
		return ids, nil
	}

	var q string
	var args []any
	if resource == resourceLists || resource == resourceSubscribers {
		scope, scopeArgs := workspaceOwnerScopedReadPredicate(access, "", 1)
		q = fmt.Sprintf("SELECT id FROM %s WHERE (%s)", table, scope)
		args = scopeArgs
	} else {
		scope, scopeArgs := workspaceReadPredicate(access, "", 1)
		q = fmt.Sprintf("SELECT id FROM %s WHERE (%s)", table, scope)
		args = scopeArgs
	}
	if err := c.db.Select(&ids, q, args...); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return ids, nil
}

// SetResourceVisibility changes only the publication scope. Ownership and
// workspace are never client-controlled after resource creation.
func (c *Core) SetResourceVisibility(resource string, id int, visibility string) error {
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown workspace resource")
	}
	if err := validateResourceVisibility(resource, visibility); err != nil {
		return err
	}
	if _, err := c.db.Exec(fmt.Sprintf(`UPDATE %s SET visibility = $2, updated_at = NOW() WHERE id = $1`, table), id, visibility); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return nil
}

func validateResourceVisibility(resource string, visibility string) error {
	if visibility != models.ResourceVisibilityPrivate &&
		visibility != models.ResourceVisibilityOrganization &&
		visibility != models.ResourceVisibilityGlobal {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid resource visibility")
	}
	if (resource == resourceLists || resource == resourceSubscribers) && visibility != models.ResourceVisibilityPrivate {
		return echo.NewHTTPError(http.StatusBadRequest, "lists and subscribers must remain private to their owner")
	}
	if visibility == models.ResourceVisibilityGlobal &&
		(resource == resourceLists || resource == resourceSubscribers || resource == resourceMedia) {
		return echo.NewHTTPError(http.StatusBadRequest, "this resource cannot be globally visible")
	}
	return nil
}

func (c *Core) CountWorkspaceManagers(orgID int) (int, error) {
	return c.countOrganizationManagers(c.db, orgID)
}

// ListManagedWorkspaceResources returns only resources the active caller may
// mutate. It is used by every bulk endpoint so managers cannot accidentally
// operate on another member's records merely because they can inspect them.
func (c *Core) ListManagedWorkspaceResources(access models.WorkspaceAccess, resource string) ([]int, error) {
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "unknown workspace resource")
	}

	if access.Archived {
		return []int{}, nil
	}
	if access.PlatformAdmin {
		var ids []int
		if err := c.db.Select(&ids, fmt.Sprintf(`SELECT id FROM %s`, table)); err != nil {
			return nil, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
		}
		return ids, nil
	}

	var (
		ids  []int
		args []any
		stmt string
	)
	if access.IsOrganization() {
		stmt = fmt.Sprintf(`SELECT id FROM %s
			WHERE organization_id = $1 AND owner_user_id = $2 AND transfer_pending_at IS NULL`, table)
		args = []any{access.OrganizationID, access.UserID}
	} else {
		stmt = fmt.Sprintf(`SELECT id FROM %s
			WHERE organization_id IS NULL AND owner_user_id = $1 AND transfer_pending_at IS NULL`, table)
		args = []any{access.UserID}
	}
	if err := c.db.Select(&ids, stmt, args...); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return ids, nil
}

// RequireManagedResources validates a bulk target before a destructive or
// sending operation. An empty set is valid and intentionally acts on nothing.
func (c *Core) RequireManagedResources(access models.WorkspaceAccess, resource string, ids []int) error {
	for _, id := range ids {
		if _, err := c.RequireManageResource(access, resource, id); err != nil {
			return err
		}
	}
	return nil
}
