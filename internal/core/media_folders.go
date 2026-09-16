package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

const maxMediaFolderNameLength = 255

// normalizeMediaFolderName applies the Windows-compatible folder-name rules
// used by the UI and API. Folder names are logical labels, not provider paths,
// so path separators and reserved path punctuation are deliberately rejected.
func normalizeMediaFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "media folder name is required")
	}
	if name == "." || name == ".." {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid media folder name")
	}
	if utf8.RuneCountInString(name) > maxMediaFolderNameLength {
		return "", echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("media folder name cannot exceed %d characters", maxMediaFolderNameLength))
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`\\/:*?"<>|`, r) {
			return "", echo.NewHTTPError(http.StatusBadRequest, "media folder name contains an invalid character")
		}
	}
	return name, nil
}

// mediaFolderWorkspacePredicate limits folders to the selected workspace for
// active sessions, including platform administrators. Archived platform
// administrators retain broad visibility for cleanup flows; ordinary writes
// are still rejected before a folder mutation can commit.
func mediaFolderWorkspacePredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	field := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	if access.PlatformAdmin && access.Archived {
		return "TRUE", nil
	}
	if access.IsOrganization() {
		return fmt.Sprintf("%s = $%d", field("organization_id"), firstArg), []any{access.OrganizationID}
	}
	return fmt.Sprintf("%s IS NULL AND %s = $%d",
		field("organization_id"), field("owner_user_id"), firstArg), []any{access.UserID}
}

func mediaFolderFields(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf(`%sid, %sname, %sparent_id, %sorganization_id, %sowner_user_id,
		%screated_at, %supdated_at`, prefix, prefix, prefix, prefix, prefix, prefix, prefix)
}

func mediaFolderWorkspaceValues(access models.WorkspaceAccess) (any, int) {
	if access.IsOrganization() {
		return access.OrganizationID, access.UserID
	}
	return nil, access.UserID
}

func mediaFolderMatchesSelectedWorkspace(access models.WorkspaceAccess, folder media.MediaFolder) bool {
	if access.IsOrganization() {
		return folder.OrganizationID.Valid && int(folder.OrganizationID.Int) == access.OrganizationID
	}
	return !folder.OrganizationID.Valid && folder.OwnerUserID.Valid && int(folder.OwnerUserID.Int) == access.UserID
}

func mediaFolderMatchesResourceScope(scope models.ResourceScope, folder media.MediaFolder) bool {
	if scope.OrganizationID.Valid || folder.OrganizationID.Valid {
		return scope.OrganizationID.Valid && folder.OrganizationID.Valid &&
			scope.OrganizationID.Int == folder.OrganizationID.Int
	}
	return scope.OwnerUserID.Valid && folder.OwnerUserID.Valid &&
		scope.OwnerUserID.Int == folder.OwnerUserID.Int
}

// workspaceMediaReadPredicate keeps the media library inside the selected
// workspace for active platform-admin sessions. Platform administrators still
// retain broad single-resource and mutation capabilities, but the media page
// is also the source for logical-folder moves; showing media from every
// workspace there would make a valid cross-workspace drag look actionable.
// Archived platform-admin sessions retain broad visibility for cleanup flows,
// while ordinary writes remain blocked by the handler/core mutation guards.
func workspaceMediaReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	if !access.PlatformAdmin || access.Archived {
		return workspaceReadPredicate(access, alias, firstArg)
	}

	field := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	arg := fmt.Sprintf("$%d", firstArg)
	if access.IsOrganization() {
		return withActiveOrganizationPredicate(
			fmt.Sprintf("%sorganization_id = %s", field(""), arg),
			alias,
		), []any{access.OrganizationID}
	}

	return withActiveOrganizationPredicate(
		fmt.Sprintf("(%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
			field(""), field(""), arg, field("")),
		alias,
	), []any{access.UserID}
}

func sameMediaFolderWorkspace(left, right media.MediaFolder) bool {
	if left.OrganizationID.Valid || right.OrganizationID.Valid {
		return left.OrganizationID.Valid && right.OrganizationID.Valid &&
			left.OrganizationID.Int == right.OrganizationID.Int
	}
	return left.OwnerUserID.Valid && right.OwnerUserID.Valid &&
		left.OwnerUserID.Int == right.OwnerUserID.Int
}

// QueryWorkspaceMediaFolders returns all folders visible to the active
// workspace. The frontend builds the current tree and breadcrumbs locally,
// while counts keep empty folders distinguishable from an empty library.
func (c *Core) QueryWorkspaceMediaFolders(access models.WorkspaceAccess) ([]media.MediaFolder, error) {
	scope, args := mediaFolderWorkspacePredicate(access, "f", 1)
	query := fmt.Sprintf(`
		SELECT %s,
			COUNT(DISTINCT m.id) AS media_count,
			COUNT(DISTINCT child.id) AS child_count
		FROM media_folders f
		LEFT JOIN media m ON m.folder_id = f.id
		LEFT JOIN media_folders child ON child.parent_id = f.id
		WHERE (%s)
		GROUP BY f.id, f.name, f.parent_id, f.organization_id, f.owner_user_id,
			f.created_at, f.updated_at
		ORDER BY LOWER(f.name), f.id`, mediaFolderFields("f"), scope)
	var out []media.MediaFolder
	if err := c.db.Select(&out, query, args...); err != nil {
		return nil, workspaceQueryError("fetching media folders", err)
	}
	return out, nil
}

// RequireReadableMediaFolder validates a non-root folder before a list query
// or upload. Folder IDs are never accepted as a workspace selector by
// themselves.
func (c *Core) RequireReadableMediaFolder(access models.WorkspaceAccess, id int) error {
	if id < 1 {
		return nil
	}
	scope, args := mediaFolderWorkspacePredicate(access, "f", 1)
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS(
		SELECT 1 FROM media_folders f WHERE f.id = $%d AND (%s)
	)`, len(args)+1, scope)
	args = append(args, id)
	if err := c.db.Get(&exists, query, args...); err != nil {
		return workspaceQueryError("checking media folder", err)
	}
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "media folder not found")
	}
	return nil
}

func (c *Core) lockMediaFolderForWorkspace(tx *sqlx.Tx, access models.WorkspaceAccess, id int) (media.MediaFolder, error) {
	var out media.MediaFolder
	if id < 1 {
		return out, echo.NewHTTPError(http.StatusBadRequest, "invalid media folder")
	}
	scope, args := mediaFolderWorkspacePredicate(access, "f", 1)
	first := len(args) + 1
	query := fmt.Sprintf(`SELECT %s FROM media_folders f
		WHERE f.id = $%d AND (%s) FOR UPDATE`, mediaFolderFields("f"), first, scope)
	args = append(args, id)
	if err := tx.Get(&out, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, echo.NewHTTPError(http.StatusNotFound, "media folder not found")
		}
		return out, workspaceQueryError("locking media folder", err)
	}
	return out, nil
}

func (c *Core) loadMediaFolderTx(tx *sqlx.Tx, id int) (media.MediaFolder, error) {
	var out media.MediaFolder
	query := fmt.Sprintf(`
		SELECT %s,
			(SELECT COUNT(*) FROM media WHERE folder_id = f.id) AS media_count,
			(SELECT COUNT(*) FROM media_folders child WHERE child.parent_id = f.id) AS child_count
		FROM media_folders f WHERE f.id = $1`, mediaFolderFields("f"))
	if err := tx.Get(&out, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, echo.NewHTTPError(http.StatusNotFound, "media folder not found")
		}
		return out, workspaceQueryError("fetching media folder", err)
	}
	return out, nil
}

// withWorkspaceMediaFoldersMutation locks the organization(s), membership,
// and every requested folder in a stable order before a folder mutation.
func (c *Core) withWorkspaceMediaFoldersMutation(access models.WorkspaceAccess, ids []int, fn func(*sqlx.Tx, map[int]media.MediaFolder) error) error {
	if fn == nil || access.Archived {
		return workspaceMutationError()
	}
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return workspaceMutationError()
	}
	sort.Ints(ids)

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return workspaceQueryError("starting media folder mutation", err)
	}
	defer tx.Rollback()

	var lockedStatuses map[int]string
	if access.PlatformAdmin {
		var orgIDs []int
		if err := tx.Select(&orgIDs, `
			SELECT DISTINCT organization_id FROM media_folders
			WHERE id = ANY($1::INT[]) AND organization_id IS NOT NULL
			ORDER BY organization_id`, pq.Array(ids)); err != nil {
			return workspaceQueryError("resolving media folder workspace", err)
		}
		lockedStatuses, err = c.lockWorkspaceOrganizations(tx, orgIDs)
		if err != nil {
			return err
		}
	} else if access.IsOrganization() {
		if err := c.lockActiveWorkspaceOrganization(tx, access.OrganizationID); err != nil {
			return err
		}
		if !c.workspaceMembershipActive(tx, access.OrganizationID, access.UserID) {
			return workspaceMutationError()
		}
	}

	scope, scopeArgs := mediaFolderWorkspacePredicate(access, "f", 2)
	args := []any{pq.Array(ids)}
	args = append(args, scopeArgs...)
	query := fmt.Sprintf(`SELECT %s FROM media_folders f
		WHERE f.id = ANY($1::INT[]) AND (%s)
		ORDER BY f.id FOR UPDATE`, mediaFolderFields("f"), scope)
	var rows []media.MediaFolder
	if err := tx.Select(&rows, query, args...); err != nil {
		return workspaceQueryError("locking media folders", err)
	}
	if len(rows) != len(ids) {
		return workspaceMutationError()
	}
	if access.PlatformAdmin {
		for _, row := range rows {
			if row.OrganizationID.Valid && lockedStatuses[int(row.OrganizationID.Int)] != models.OrganizationStatusActive {
				return workspaceMutationError()
			}
		}
	}
	locked := make(map[int]media.MediaFolder, len(rows))
	for _, row := range rows {
		locked[row.ID] = row
	}
	if err := fn(tx, locked); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceQueryError("committing media folder mutation", err)
	}
	return nil
}

// CreateMediaFolderInWorkspace creates a folder in the current workspace.
// Organization folders are workspace containers visible to all members;
// personal folders are visible only to their owner.
func (c *Core) CreateMediaFolderInWorkspace(access models.WorkspaceAccess, name string, parentID int) (media.MediaFolder, error) {
	name, err := normalizeMediaFolderName(name)
	if err != nil {
		return media.MediaFolder{}, err
	}
	if parentID < 0 {
		return media.MediaFolder{}, echo.NewHTTPError(http.StatusBadRequest, "invalid media folder")
	}

	organizationID, ownerUserID := mediaFolderWorkspaceValues(access)
	var out media.MediaFolder
	err = c.withWorkspaceCreation(access, func(tx *sqlx.Tx) error {
		if parentID > 0 {
			parent, err := c.lockMediaFolderForWorkspace(tx, access, parentID)
			if err != nil {
				return err
			}
			if !mediaFolderMatchesSelectedWorkspace(access, parent) {
				return echo.NewHTTPError(http.StatusBadRequest, "media folder is outside the active workspace")
			}
		}

		if err := tx.Get(&out, fmt.Sprintf(`INSERT INTO media_folders
			(name, parent_id, organization_id, owner_user_id, created_by_user_id)
			VALUES ($1, NULLIF($2, 0), $3, $4, $4)
			RETURNING %s`, mediaFolderFields("")), name, parentID, organizationID, ownerUserID); err != nil {
			if conflict := mediaFolderNameConflictError(err); conflict != nil {
				return conflict
			}
			return workspaceQueryError("creating media folder", err)
		}
		return nil
	})
	return out, err
}

// RenameMediaFolderInWorkspace changes only the folder label. Moving a folder
// is a separate operation so a drag cannot accidentally rename it.
func (c *Core) RenameMediaFolderInWorkspace(access models.WorkspaceAccess, id int, name string) (media.MediaFolder, error) {
	name, err := normalizeMediaFolderName(name)
	if err != nil {
		return media.MediaFolder{}, err
	}
	var out media.MediaFolder
	err = c.withWorkspaceMediaFoldersMutation(access, []int{id}, func(tx *sqlx.Tx, locked map[int]media.MediaFolder) error {
		if _, ok := locked[id]; !ok {
			return workspaceMutationError()
		}
		if _, err := tx.Exec(`UPDATE media_folders SET name = $2, updated_at = NOW() WHERE id = $1`, id, name); err != nil {
			if conflict := mediaFolderNameConflictError(err); conflict != nil {
				return conflict
			}
			return workspaceQueryError("renaming media folder", err)
		}
		var err error
		out, err = c.loadMediaFolderTx(tx, id)
		return err
	})
	return out, err
}

// DeleteMediaFolderInWorkspace deletes only empty folders. This mirrors the
// safe branch of Windows Explorer behavior: a user cannot accidentally delete
// media or an entire subtree by removing a container.
func (c *Core) DeleteMediaFolderInWorkspace(access models.WorkspaceAccess, id int) error {
	return c.withWorkspaceMediaFoldersMutation(access, []int{id}, func(tx *sqlx.Tx, locked map[int]media.MediaFolder) error {
		if _, ok := locked[id]; !ok {
			return workspaceMutationError()
		}
		var contents int
		if err := tx.Get(&contents, `
			SELECT (SELECT COUNT(*) FROM media WHERE folder_id = $1) +
			       (SELECT COUNT(*) FROM media_folders WHERE parent_id = $1)`, id); err != nil {
			return workspaceQueryError("checking media folder contents", err)
		}
		if contents > 0 {
			return echo.NewHTTPError(http.StatusConflict, "media folder is not empty")
		}
		if _, err := tx.Exec(`DELETE FROM media_folders WHERE id = $1`, id); err != nil {
			return workspaceQueryError("deleting media folder", err)
		}
		return nil
	})
}

// MoveMediaFolderInWorkspace reparents a folder while keeping the entire
// operation in the same workspace transaction. Descendant targets are
// rejected to preserve a valid tree.
func (c *Core) MoveMediaFolderInWorkspace(access models.WorkspaceAccess, id, parentID int) error {
	if id < 1 || parentID < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid media folder")
	}
	if id == parentID {
		return echo.NewHTTPError(http.StatusBadRequest, "a media folder cannot contain itself")
	}
	ids := []int{id}
	if parentID > 0 {
		ids = append(ids, parentID)
	}
	return c.withWorkspaceMediaFoldersMutation(access, ids, func(tx *sqlx.Tx, locked map[int]media.MediaFolder) error {
		source, ok := locked[id]
		if !ok {
			return workspaceMutationError()
		}
		if parentID == 0 {
			if _, err := tx.Exec(`UPDATE media_folders SET parent_id = NULL, updated_at = NOW() WHERE id = $1`, id); err != nil {
				return workspaceQueryError("moving media folder", err)
			}
			return nil
		}
		target, ok := locked[parentID]
		if !ok {
			return workspaceMutationError()
		}
		if !sameMediaFolderWorkspace(source, target) {
			return echo.NewHTTPError(http.StatusBadRequest, "media folder destination is outside the source workspace")
		}
		var descendant bool
		if err := tx.Get(&descendant, `
			WITH RECURSIVE descendants AS (
				SELECT id FROM media_folders WHERE id = $1
				UNION
				SELECT child.id FROM media_folders child
				JOIN descendants parent ON child.parent_id = parent.id
			)
			SELECT EXISTS(SELECT 1 FROM descendants WHERE id = $2)`, id, parentID); err != nil {
			return workspaceQueryError("checking media folder destination", err)
		}
		if descendant {
			return echo.NewHTTPError(http.StatusBadRequest, "a media folder cannot be moved into its own descendant")
		}
		if _, err := tx.Exec(`UPDATE media_folders SET parent_id = $2, updated_at = NOW() WHERE id = $1`, id, parentID); err != nil {
			if conflict := mediaFolderNameConflictError(err); conflict != nil {
				return conflict
			}
			return workspaceQueryError("moving media folder", err)
		}
		return nil
	})
}

// MoveMediaToFolderInWorkspace changes only logical folder membership. The
// media row remains protected by the normal owner/manage resource boundary.
func (c *Core) MoveMediaToFolderInWorkspace(access models.WorkspaceAccess, mediaID, folderID int) error {
	if mediaID < 1 || folderID < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid media folder")
	}
	return c.withWorkspaceResourceMutation(access, resourceMedia, []int{mediaID}, func(tx *sqlx.Tx) error {
		var scope models.ResourceScope
		if err := tx.Get(&scope, `SELECT organization_id, owner_user_id FROM media WHERE id = $1 FOR UPDATE`, mediaID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return workspaceMutationError()
			}
			return workspaceQueryError("locking media for folder move", err)
		}
		if folderID > 0 {
			folder, err := c.lockMediaFolderForWorkspace(tx, access, folderID)
			if err != nil {
				return err
			}
			if !mediaFolderMatchesResourceScope(scope, folder) {
				return echo.NewHTTPError(http.StatusBadRequest, "media folder is outside the media workspace")
			}
		}
		if _, err := tx.Exec(`UPDATE media SET folder_id = NULLIF($2, 0), updated_at = NOW() WHERE id = $1`, mediaID, folderID); err != nil {
			return workspaceQueryError("moving media to folder", err)
		}
		return nil
	})
}

func mediaFolderNameConflictError(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
		return nil
	}
	switch pqErr.Constraint {
	case "idx_media_folders_org_parent_name", "idx_media_folders_personal_parent_name":
		return echo.NewHTTPError(http.StatusConflict, "a media folder with this name already exists here")
	default:
		return nil
	}
}
