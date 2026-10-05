package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

type organizationSMTPPool struct {
	ID           int    `json:"id" db:"id"`
	Name         string `json:"name" db:"name"`
	SMTPCount    int    `json:"smtp_count" db:"smtp_count"`
	EnabledCount int    `json:"enabled_count" db:"enabled_count"`
}

func (a *App) listOrganizationSMTPPools(orgID int) ([]organizationSMTPPool, error) {
	rows := make([]organizationSMTPPool, 0)
	err := a.db.Select(&rows, `SELECT p.id, p.name, COUNT(s.id) AS smtp_count,
		COUNT(s.id) FILTER(WHERE s.enabled) AS enabled_count
		FROM organization_smtp_pools p LEFT JOIN user_smtp_servers s ON s.smtp_pool_id=p.id
		WHERE p.organization_id=$1 GROUP BY p.id ORDER BY p.id`, orgID)
	return rows, err
}

func (a *App) GetOrganizationSMTPPools(c echo.Context) error {
	key, err := a.organizationSMTPOwner(c)
	if err != nil {
		return err
	}
	rows, err := a.listOrganizationSMTPPools(-key)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

func smtpPoolName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", echo.NewHTTPError(http.StatusBadRequest, "SMTP pool name must contain 1 to 100 characters")
	}
	return name, nil
}

func (a *App) SaveOrganizationSMTPPool(c echo.Context) error {
	key, err := a.organizationSMTPOwner(c)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	name, err := smtpPoolName(req.Name)
	if err != nil {
		return err
	}
	id, _ := c.Get("id").(int)
	tx, err := a.db.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orgID int
	if err := tx.Get(&orgID, `SELECT id FROM organizations WHERE id=$1 AND status='active' FOR UPDATE`, -key); err != nil {
		return err
	}
	if id == 0 {
		err = tx.Get(&id, `INSERT INTO organization_smtp_pools(organization_id,name) VALUES($1,$2) RETURNING id`, orgID, name)
	} else {
		err = tx.Get(&id, `UPDATE organization_smtp_pools SET name=$3,updated_at=NOW() WHERE id=$1 AND organization_id=$2 RETURNING id`, id, orgID, name)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "SMTP pool not found")
	}
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return echo.NewHTTPError(http.StatusConflict, "SMTP pool name already exists")
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{organizationSMTPPool{ID: id, Name: name}})
}

// A populated or referenced pool cannot be deleted, preventing silent SMTP
// removal or a campaign switching to a different pool.
func (a *App) DeleteOrganizationSMTPPool(c echo.Context) error {
	key, err := a.organizationSMTPOwner(c)
	if err != nil {
		return err
	}
	id := getID(c)
	remove := func() error {
		tx, err := a.db.BeginTxx(c.Request().Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var lockedID int
		if err := tx.Get(&lockedID, `SELECT p.id FROM organizations o JOIN organization_smtp_pools p ON p.organization_id=o.id
			WHERE o.id=$1 AND o.status='active' AND p.id=$2 FOR UPDATE OF o,p`, -key, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "SMTP pool not found")
			}
			return err
		}
		var used bool
		if err := tx.Get(&used, `SELECT EXISTS(SELECT 1 FROM user_smtp_servers WHERE smtp_pool_id=$1)
			OR EXISTS(SELECT 1 FROM campaigns WHERE smtp_pool_id=$1)`, id); err != nil {
			return err
		}
		if used {
			return echo.NewHTTPError(http.StatusConflict, "remove SMTP accounts and campaign references before deleting this pool")
		}
		if _, err := tx.Exec(`DELETE FROM organization_smtp_pools WHERE id=$1 AND organization_id=$2`, id, -key); err != nil {
			return err
		}
		return tx.Commit()
	}
	if a.manager != nil {
		err = a.manager.WithPersonalSMTPUpdate(-id, remove)
	} else {
		err = remove()
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// organizationSMTPPoolOwner derives the signed pool key from an authorized
// organization. Missing pool_id retains the original API's default-pool path.
func (a *App) organizationSMTPPoolOwner(c echo.Context, createDefault bool) (int, error) {
	key, err := a.organizationSMTPOwner(c)
	if err != nil {
		return 0, err
	}
	poolID := 0
	if value := c.QueryParam("pool_id"); value != "" {
		poolID, err = strconv.Atoi(value)
		if err != nil || poolID < 1 {
			return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP pool id")
		}
	} else {
		err = a.db.Get(&poolID, `SELECT COALESCE(MIN(id),0) FROM organization_smtp_pools WHERE organization_id=$1`, -key)
		if err != nil {
			return 0, err
		}
		if poolID == 0 && createDefault {
			// Concurrent legacy requests share one default pool under the org lock.
			tx, err := a.db.Beginx()
			if err != nil {
				return 0, err
			}
			defer tx.Rollback()
			var orgID int
			if err := tx.Get(&orgID, `SELECT id FROM organizations WHERE id=$1 AND status='active' FOR UPDATE`, -key); err != nil {
				return 0, err
			}
			if err := tx.Get(&poolID, `SELECT COALESCE(MIN(id),0) FROM organization_smtp_pools WHERE organization_id=$1`, orgID); err != nil {
				return 0, err
			}
			if poolID == 0 {
				if err := tx.Get(&poolID, `INSERT INTO organization_smtp_pools(organization_id,name) VALUES($1,'默认发件池') RETURNING id`, orgID); err != nil {
					return 0, err
				}
			}
			if err := tx.Commit(); err != nil {
				return 0, err
			}
		}
	}
	if poolID == 0 {
		return 0, nil
	}
	var belongs bool
	if err := a.db.Get(&belongs, `SELECT EXISTS(SELECT 1 FROM organization_smtp_pools WHERE id=$1 AND organization_id=$2)`, poolID, -key); err != nil {
		return 0, err
	}
	if !belongs {
		return 0, echo.NewHTTPError(http.StatusNotFound, "SMTP pool not found")
	}
	return -poolID, nil
}

func (a *App) validateCampaignSMTPPool(camp *models.Campaign, orgID int) error {
	if camp.SMTPSource != "organization" {
		camp.SMTPPoolID = null.Int{}
		return nil
	}
	// The legacy platform-wide path routes by organization, not one pool.
	if camp.PoolScope == models.CampaignPoolScopeAllOrganizations {
		if camp.SMTPPoolID.Valid {
			return echo.NewHTTPError(http.StatusBadRequest, "one SMTP pool cannot be used across all organizations")
		}
		return nil
	}
	if !camp.SMTPPoolID.Valid {
		var id int
		if err := a.db.Get(&id, `SELECT COALESCE(MIN(id),0) FROM organization_smtp_pools WHERE organization_id=$1`, orgID); err != nil {
			return err
		}
		if id > 0 {
			camp.SMTPPoolID = null.IntFrom(id)
		}
	}
	var valid bool
	if err := a.db.Get(&valid, `SELECT EXISTS(SELECT 1 FROM organization_smtp_pools p JOIN organizations o ON o.id=p.organization_id
		WHERE p.id=$1 AND p.organization_id=$2 AND o.status='active')`, camp.SMTPPoolID.Int, orgID); err != nil {
		return err
	}
	if !valid {
		return echo.NewHTTPError(http.StatusBadRequest, "select an SMTP pool belonging to this organization")
	}
	return nil
}

func (a *App) GetCampaignSMTPPools(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	orgID := access.OrganizationID
	id, _ := c.Get("id").(int)
	if id > 0 {
		if _, err := a.requireReadableWorkspaceCampaign(c, access, id); err != nil {
			return err
		}
		camp, err := a.core.GetWorkspaceCampaign(access, id)
		if err != nil {
			return err
		}
		orgID = camp.OrganizationID.Int
	} else if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
		return err
	}
	rows, err := a.listOrganizationSMTPPools(orgID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}
