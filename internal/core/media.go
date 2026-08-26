package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
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

	var provider string
	if err := tx.QueryRowx(`SELECT provider, filename, thumb FROM media WHERE id = $1 FOR UPDATE`, id).
		Scan(&provider, &out.Filename, &out.Thumb); err != nil {
		if err == sql.ErrNoRows {
			return out, ErrNotFound
		}
		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}
	if _, err := tx.Exec(`DELETE FROM media WHERE id = $1`, id); err != nil {
		return out, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}

	var filenameRefs, thumbRefs int
	if err := tx.Get(&filenameRefs, `
		SELECT COUNT(*) FROM media
		WHERE provider = $1 AND (filename = $2 OR thumb = $2)`, provider, out.Filename); err != nil {
		return out, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if out.Thumb != "" && out.Thumb != out.Filename {
		if err := tx.Get(&thumbRefs, `
			SELECT COUNT(*) FROM media
			WHERE provider = $1 AND (filename = $2 OR thumb = $2)`, provider, out.Thumb); err != nil {
			return out, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
		}
	}
	out.DeleteFilename = filenameRefs == 0
	out.DeleteThumb = out.Thumb != "" && out.Thumb != out.Filename && thumbRefs == 0

	if err := tx.Commit(); err != nil {
		return out, echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return out, nil
}
