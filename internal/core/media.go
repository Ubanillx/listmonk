package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"gopkg.in/volatiletech/null.v6"
)

// QueryMedia returns media entries optionally filtered by a query string.
func (c *Core) QueryMedia(provider string, s media.Store, query string, offset, limit int) ([]media.Media, int, error) {
	out := []media.Media{}

	if query != "" {
		query = strings.ToLower(query)
	}

	if err := c.q.QueryMedia.Select(&out, fmt.Sprintf("%%%s%%", query), provider, offset, limit); err != nil {
		return out, 0, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching",
				"name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}

	total := 0
	if len(out) > 0 {
		total = out[0].Total

		for i := 0; i < len(out); i++ {
			out[i].URL = s.GetURL(out[i].Filename)

			if out[i].Thumb != "" {
				out[i].ThumbURL = null.String{Valid: true, String: s.GetURL(out[i].Thumb)}
			}
		}
	}

	return out, total, nil
}

// GetMedia returns a media item.
func (c *Core) GetMedia(id int, uuid, fileName string, s media.Store) (media.Media, error) {
	var uu any
	if uuid != "" {
		uu = uuid
	}

	var out media.Media
	if err := c.q.GetMedia.Get(&out, id, uu, fileName); err != nil {
		// If it's ` sql: no rows in result set`, return a 404.
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}

		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}

	out.URL = s.GetURL(out.Filename)
	if out.Thumb != "" {
		out.ThumbURL = null.String{Valid: true, String: s.GetURL(out.Thumb)}
	}

	return out, nil
}

// MediaFilenameExists checks for a provider object name without returning a
// media row.  Provider storage is shared by all workspaces, so names must be
// unique globally to prevent one upload from overwriting another workspace's
// binary.  Keeping this as an existence-only query avoids using the legacy
// ID/filename lookup as an accidental cross-workspace read during uploads.
func (c *Core) MediaFilenameExists(provider, filename string) (bool, error) {
	provider = strings.TrimSpace(provider)
	filename = strings.TrimSpace(filename)
	if provider == "" || filename == "" {
		return false, nil
	}
	var exists bool
	if err := c.db.Get(&exists, `
		SELECT EXISTS (
			SELECT 1 FROM media
			WHERE provider = $1 AND (filename = $2 OR thumb = $2)
		)`, provider, filename); err != nil {
		return false, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}
	return exists, nil
}

// InsertMedia inserts a new media file into the DB.
func (c *Core) InsertMedia(fileName, thumbName, contentType string, meta models.JSON, provider string, scope models.ResourceScope, s media.Store) (media.Media, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}

	// Write to the DB.
	var newID int
	if err := c.q.InsertMedia.Get(&newID, uu, fileName, thumbName, contentType, provider, meta,
		scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
		c.log.Printf("error inserting uploaded file to db: %v", err)
		return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}

	return c.GetMedia(newID, "", "", s)
}

// InsertMediaInWorkspace inserts a media row only while the selected
// organization is locked and the caller's membership is still active. The
// binary itself is written by the handler before this method; a failed insert
// is reported so the handler can remove that unreferenced object.
func (c *Core) InsertMediaInWorkspace(access models.WorkspaceAccess, fileName, thumbName, contentType string, meta models.JSON, provider string, scope models.ResourceScope, s media.Store) (media.Media, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	var out media.Media
	err = c.withWorkspaceCreation(access, func(tx *sqlx.Tx) error {
		var newID int
		if err := tx.Stmtx(c.q.InsertMedia).Get(&newID, uu, fileName, thumbName, contentType, provider, meta,
			scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
		}
		// Read the row before committing the creation transaction.  A
		// post-commit workspace-authorized lookup can fail if membership or
		// organization state changes in the meantime, leaving the freshly
		// uploaded binary paired with an orphaned database row.  The insert
		// transaction already holds the organization lock, so this direct
		// read is both race-free and guaranteed to roll back together with the
		// insert on any error.
		if err := tx.Get(&out, `SELECT * FROM media WHERE id = $1`, newID); err != nil {
			return workspaceQueryError("fetching newly uploaded media", err)
		}
		return nil
	})
	if err != nil {
		return media.Media{}, err
	}
	// Preserve the provider URLs returned by the legacy insert path.
	out.URL = s.GetURL(out.Filename)
	if out.Thumb != "" {
		out.ThumbURL = null.String{Valid: true, String: s.GetURL(out.Thumb)}
	}
	return out, nil
}

// MediaDeletion describes the physical objects that became unreferenced when
// a media row was deleted. Campaign/template clones intentionally share the
// same provider object, so removing one record must not delete a binary still
// used by another media row.
type MediaDeletion struct {
	Filename       string
	Thumb          string
	DeleteFilename bool
	DeleteThumb    bool
}

// DeleteMedia deletes a media row and reports only provider objects that no
// remaining row references. The caller performs the actual store deletion
// after this transaction commits.
func (c *Core) DeleteMedia(id int) (MediaDeletion, error) {
	var out MediaDeletion
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}
	defer tx.Rollback()
	if err := c.deleteMediaTx(tx, id, &out); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return out, nil
}

// DeleteMediaInWorkspace keeps media deletion behind the same ownership and
// organization lifecycle locks as all other workspace mutations. This matters
// for inline MIME media: a late deletion must not remove a record that was
// transferred to a former member cleanup queue after the request was checked.
func (c *Core) DeleteMediaInWorkspace(access models.WorkspaceAccess, id int) (MediaDeletion, error) {
	var out MediaDeletion
	err := c.withWorkspaceResourceMutation(access, resourceMedia, []int{id}, func(tx *sqlx.Tx) error {
		return c.deleteMediaTx(tx, id, &out)
	})
	return out, err
}

func (c *Core) deleteMediaTx(tx *sqlx.Tx, id int, out *MediaDeletion) error {

	var provider string
	if err := tx.QueryRowx(`SELECT provider, filename, thumb FROM media WHERE id = $1 FOR UPDATE`, id).
		Scan(&provider, &out.Filename, &out.Thumb); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}
	if _, err := tx.Exec(`DELETE FROM media WHERE id = $1`, id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}

	var filenameRefs, thumbRefs int
	if err := tx.Get(&filenameRefs, `
		SELECT COUNT(*) FROM media
		WHERE provider = $1 AND (filename = $2 OR thumb = $2)`, provider, out.Filename); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if out.Thumb != "" && out.Thumb != out.Filename {
		if err := tx.Get(&thumbRefs, `
			SELECT COUNT(*) FROM media
			WHERE provider = $1 AND (filename = $2 OR thumb = $2)`, provider, out.Thumb); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
		}
	}
	out.DeleteFilename = filenameRefs == 0
	out.DeleteThumb = out.Thumb != "" && out.Thumb != out.Filename && thumbRefs == 0
	return nil
}
