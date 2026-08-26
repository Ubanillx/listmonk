package core

import (
	"database/sql"
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// GetTemplates retrieves all templates.
func (c *Core) GetTemplates(status string, noBody bool) ([]models.Template, error) {
	out := []models.Template{}
	if err := c.q.GetTemplates.Select(&out, 0, noBody, status); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.templates}", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetTemplate retrieves a given template.
func (c *Core) GetTemplate(id int, noBody bool) (models.Template, error) {
	var out []models.Template
	if err := c.q.GetTemplates.Select(&out, id, noBody, ""); err != nil {
		return models.Template{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "{globals.terms.templates}", "error", pqErrMsg(err)))
	}

	if len(out) == 0 {
		return models.Template{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
	}

	return out[0], nil
}

// CreateTemplate creates a new template.
func (c *Core) CreateTemplate(name, typ, subject string, body []byte, bodySource null.String, mediaIDs pq.Int64Array, scope models.ResourceScope) (models.Template, error) {
	var newID int
	if err := c.q.CreateTemplate.Get(&newID, name, typ, subject, body, bodySource, pq.Array(mediaIDs),
		scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
		return models.Template{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.template}", "error", pqErrMsg(err)))
	}

	return c.GetTemplate(newID, false)
}

// UpdateTemplate updates a given template.
func (c *Core) UpdateTemplate(id int, name, subject string, body []byte, bodySource null.String, mediaIDs pq.Int64Array) (models.Template, error) {
	var updatedID int
	err := c.q.UpdateTemplate.Get(&updatedID, id, name, subject, body, bodySource, pq.Array(mediaIDs))
	if err != nil {
		return models.Template{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUpdating", "name", "{globals.terms.template}", "error", pqErrMsg(err)))
	}

	if updatedID == 0 {
		return models.Template{}, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
	}

	return c.GetTemplate(id, false)
}

// SetWorkspaceDefaultTemplate sets one caller-owned campaign template as the
// default within its own personal or organization workspace. Defaults must
// never be reset across another owner's resources.
func (c *Core) SetWorkspaceDefaultTemplate(id int, access models.WorkspaceAccess) error {
	scope := ApplyWorkspaceScope(access, models.ResourceVisibilityPrivate)
	tx, err := c.db.Beginx()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`
		SELECT id FROM templates
		WHERE id = $1 AND type = 'campaign'
			AND organization_id IS NOT DISTINCT FROM $2::BIGINT
			AND owner_user_id = $3 AND transfer_pending_at IS NULL
		FOR UPDATE`, id, scope.OrganizationID, access.UserID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusForbidden, "only an owned campaign template can be the workspace default")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	// Lock all local defaults before swapping them. The old schema enforced a
	// single global default; the scoped unique index now enforces one per owner
	// and workspace, so the previous default must be cleared first.
	var localIDs []int
	if err := tx.Select(&localIDs, `
		SELECT id FROM templates
		WHERE type = 'campaign' AND organization_id IS NOT DISTINCT FROM $1::BIGINT
			AND owner_user_id = $2
		FOR UPDATE`, scope.OrganizationID, access.UserID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if _, err := tx.Exec(`
		UPDATE templates SET is_default = FALSE, updated_at = NOW()
		WHERE type = 'campaign' AND organization_id IS NOT DISTINCT FROM $1::BIGINT
			AND owner_user_id = $2`, scope.OrganizationID, access.UserID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if _, err := tx.Exec(`UPDATE templates SET is_default = TRUE, updated_at = NOW() WHERE id = $1`, exists); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return nil
}

// DeleteTemplate deletes a non-default template and only changes campaigns
// that referenced it. Fallback resolution is performed per campaign scope so
// deleting a shared template cannot assign another owner's private default.
func (c *Core) DeleteTemplate(id int) error {
	tx, err := c.db.Beginx()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	defer tx.Rollback()

	var tpl struct {
		Type      string `db:"type"`
		IsDefault bool   `db:"is_default"`
	}
	if err := tx.Get(&tpl, `SELECT type, is_default FROM templates WHERE id = $1 FOR UPDATE`, id); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
		}
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if tpl.IsDefault {
		return echo.NewHTTPError(http.StatusBadRequest, c.i18n.T("templates.cantDeleteDefault"))
	}

	if _, err := tx.Exec(`
		UPDATE campaigns c
		SET template_id = (
			SELECT fallback.id FROM templates fallback
			WHERE fallback.is_default = TRUE AND fallback.type = 'campaign'
				AND fallback.transfer_pending_at IS NULL
				AND (
					(fallback.organization_id IS NOT DISTINCT FROM c.organization_id
						AND fallback.owner_user_id = c.owner_user_id)
					OR fallback.visibility = 'global'
				)
			ORDER BY CASE WHEN fallback.organization_id IS NOT DISTINCT FROM c.organization_id
				AND fallback.owner_user_id = c.owner_user_id THEN 0 ELSE 1 END, fallback.id
			LIMIT 1
		)
		WHERE c.template_id = $1`, id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	if _, err := tx.Exec(`DELETE FROM templates WHERE id = $1`, id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorDeleting", "name", "{globals.terms.template}", "error", pqErrMsg(err)))
	}
	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, pqErrMsg(err))
	}
	return nil
}
