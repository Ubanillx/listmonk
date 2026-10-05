package core

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// Folder permissions govern the library and direct media URLs. Root media
// retains the established resource policy, including archived cleanup access.
func workspaceMediaReadPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	if access.PlatformAdmin && access.Archived {
		return "TRUE", nil
	}
	root, args := workspaceRootMediaReadPredicate(access, alias, firstArg)
	folder, folderArgs := mediaFolderReadPredicate(access, "permission_folder", firstArg+len(args))
	args = append(args, folderArgs...)
	return fmt.Sprintf(`((%s.folder_id IS NULL AND (%s)) OR
		(%s.transfer_pending_at IS NULL AND %s AND EXISTS (
			SELECT 1 FROM media_folders permission_folder
			WHERE permission_folder.id = %s.folder_id AND (%s)
		)))`, alias, root, alias, activeOrganizationPredicate(alias), alias, folder), args
}

func mediaFolderAccessPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	folder, args := mediaFolderReadPredicate(access, "permission_folder", firstArg)
	return fmt.Sprintf(`(%s.folder_id IS NULL OR EXISTS (
		SELECT 1 FROM media_folders permission_folder
		WHERE permission_folder.id = %s.folder_id AND (%s)
	))`, alias, alias, folder), args
}

func (c *Core) mediaFolderAccessible(access models.WorkspaceAccess, id int) (bool, error) {
	predicate, args := mediaFolderAccessPredicate(access, "m", 2)
	var readable bool
	if err := c.db.Get(&readable, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM media m
		WHERE m.id = $1 AND (%s))`, predicate), append([]any{id}, args...)...); err != nil {
		return false, workspaceQueryError("checking media folder permissions", err)
	}
	return readable, nil
}

// After media row locks, lock its ancestors. Permission changes and moves use
// FOR UPDATE, so they cannot race a campaign/template attachment.
func (c *Core) lockMediaFolderAccess(tx *sqlx.Tx, access models.WorkspaceAccess, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	var locked []int
	if err := tx.Select(&locked, `WITH RECURSIVE ancestors AS (
		SELECT f.id, f.parent_id FROM media_folders f JOIN media m ON m.folder_id = f.id
		WHERE m.id = ANY($1::INT[])
		UNION
		SELECT parent.id, parent.parent_id FROM media_folders parent JOIN ancestors child ON child.parent_id = parent.id
	)
	SELECT f.id FROM media_folders f WHERE f.id IN (SELECT id FROM ancestors) ORDER BY f.id FOR SHARE`, pq.Array(ids)); err != nil {
		return workspaceQueryError("locking media folder permissions", err)
	}
	predicate, args := mediaFolderAccessPredicate(access, "m", 2)
	var allowed int
	if err := tx.Get(&allowed, fmt.Sprintf(`SELECT COUNT(*) FROM media m
		WHERE m.id = ANY($1::INT[]) AND (%s)`, predicate), append([]any{pq.Array(ids)}, args...)...); err != nil {
		return workspaceQueryError("checking locked media folder permissions", err)
	}
	if allowed != len(ids) {
		return workspaceMutationError()
	}
	return nil
}

func (c *Core) lockAssociatedMediaFolderAccess(tx *sqlx.Tx, access models.WorkspaceAccess, groups ...[]mediaAssociation) error {
	var ids []int
	for _, group := range groups {
		for _, ref := range group {
			if ref.MediaID.Valid {
				ids = append(ids, int(ref.MediaID.Int))
			}
		}
	}
	return c.lockMediaFolderAccess(tx, access, uniqueMutationIDs(ids))
}
