package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// Organization SMTP uses the existing transport and quota machinery, with an
// exclusive organization owner. A negative internal owner key is never bound
// from request data; it is derived from an authorized workspace.
func (a *App) organizationSMTPOwner(c echo.Context) (int, error) {
	ws, err := a.requireOrganizationManager(c)
	if err != nil {
		return 0, err
	}
	if ws.Archived {
		return 0, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	setAuditOrganizationID(c, ws.OrganizationID)
	return -ws.OrganizationID, nil
}

func (a *App) GetOrganizationSMTP(c echo.Context) error {
	key, err := a.organizationSMTPPoolOwner(c, false)
	if err != nil {
		return err
	}
	rows, err := a.loadPersonalSMTP(key)
	if err != nil {
		return err
	}
	redactPersonalSMTP(rows)
	return c.JSON(http.StatusOK, okResp{personalSMTPResponse{SMTP: rows}})
}

func (a *App) UpdateOrganizationSMTP(c echo.Context) error {
	key, err := a.organizationSMTPPoolOwner(c, true)
	if err != nil {
		return err
	}
	return a.updateOwnedSMTP(c, key)
}

func (a *App) DeleteOrganizationSMTP(c echo.Context) error {
	key, err := a.organizationSMTPPoolOwner(c, false)
	if err != nil {
		return err
	}
	return a.deleteOwnedSMTP(c, key)
}

func (a *App) TestOrganizationSMTP(c echo.Context) error {
	key, err := a.organizationSMTPPoolOwner(c, false)
	if err != nil {
		return err
	}
	return a.testOwnedSMTP(c, key)
}

type campaignSMTPOverview struct {
	ID               int    `json:"id" db:"id"`
	Name             string `json:"name" db:"name"`
	FromEmail        string `json:"from_email" db:"from_email"`
	DailyLimit       int    `json:"daily_limit" db:"daily_limit"`
	SentToday        int    `json:"sent_today" db:"sent_today"`
	OrganizationID   int    `json:"organization_id,omitempty" db:"organization_id"`
	OrganizationName string `json:"organization_name,omitempty" db:"organization_name"`
}

// GetCampaignSMTPOverview deliberately projects only sender and usage fields;
// campaign editors (including ordinary members) never receive credentials.
func (a *App) GetCampaignSMTPOverview(c echo.Context) error {
	access, err := a.workspaceAccess(c)
	if err != nil {
		return err
	}
	userID := auth.GetUser(c).ID
	orgID := access.OrganizationID
	var camp models.Campaign
	id, _ := c.Get("id").(int)
	if id > 0 {
		_, err = a.requireReadableWorkspaceCampaign(c, access, id)
		if err != nil {
			return err
		}
		camp, err = a.core.GetWorkspaceCampaign(access, id)
		if err != nil {
			return err
		}
		userID = camp.OwnerUserID.Int
		orgID = camp.OrganizationID.Int
	} else if err := requireLegacyPermission(auth.GetUser(c), auth.PermCampaignsManageAll, auth.PermCampaignsManage); err != nil {
		return err
	}
	source := c.QueryParam("source")
	if source == "" {
		source = "personal"
	}
	if source != "personal" && source != "organization" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP source")
	}
	poolID := 0
	if source == "organization" && camp.PoolScope != models.CampaignPoolScopeAllOrganizations && c.QueryParam("pool_scope") != models.CampaignPoolScopeAllOrganizations {
		selection := models.Campaign{SMTPSource: source, SMTPPoolID: camp.SMTPPoolID}
		if value := c.QueryParam("smtp_pool_id"); value != "" {
			requested, err := strconv.Atoi(value)
			if err != nil || requested < 1 {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP pool id")
			}
			selection.SMTPPoolID = null.IntFrom(requested)
		}
		if orgID > 0 {
			if err := a.validateCampaignSMTPPool(&selection, orgID); err != nil {
				return err
			}
			poolID = selection.SMTPPoolID.Int
		}
	}
	rows := make([]campaignSMTPOverview, 0)
	poolIDs := make([]int64, 0)
	allOrgs := camp.PoolScope == models.CampaignPoolScopeAllOrganizations
	if camp.ID == 0 && c.QueryParam("pool_scope") == models.CampaignPoolScopeAllOrganizations {
		user := auth.GetUser(c)
		if !user.HasPerm(auth.PermCampaignsPublicPoolSend) {
			return auth.ErrPermDenied
		}
		var listIDs []int
		for _, value := range strings.Split(c.QueryParam("customer_list_ids"), ",") {
			if value == "" {
				continue
			}
			id, err := strconv.Atoi(value)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid audience id")
			}
			listIDs = append(listIDs, id)
		}
		_, audiences, err := a.splitCampaignAudienceIDs(access, listIDs, true)
		if err != nil {
			return err
		}
		for _, audience := range audiences {
			poolIDs = append(poolIDs, int64(audience.PoolID))
		}
		allOrgs = true
	}
	if allOrgs {
		err = a.db.Select(&rows, `SELECT DISTINCT s.id, s.name, s.from_email, s.daily_limit,
			COALESCE(u.sent_count,0) AS sent_today, o.id AS organization_id, o.name AS organization_name
			FROM organizations o JOIN user_smtp_servers s ON s.enabled AND (
				($3::TEXT='organization' AND s.organization_id=o.id)
				OR ($3::TEXT='personal' AND EXISTS (SELECT 1 FROM organization_members om
					JOIN users usr ON usr.id=om.user_id AND usr.status='enabled'
					WHERE om.organization_id=o.id AND om.user_id=s.user_id AND om.removed_at IS NULL)))
			JOIN org_pool_allocations opa ON opa.organization_id=o.id
			LEFT JOIN user_smtp_daily_usage u ON u.smtp_uuid=s.uuid AND u.usage_date=$2::DATE
			WHERE o.status='active' AND (opa.pool_id=ANY($4::BIGINT[]) OR EXISTS (
				SELECT 1 FROM campaign_customer_lists ccl WHERE ccl.pool_id=opa.pool_id AND ccl.campaign_id=$1))
			ORDER BY s.id`, camp.ID, currentLocalDate(), source, pq.Int64Array(poolIDs))
	} else {
		if source == "organization" && orgID < 1 {
			return c.JSON(http.StatusOK, okResp{rows})
		}
		key := userID
		if source == "organization" {
			key = -poolID
		}
		err = a.db.Select(&rows, `SELECT s.id, s.name, s.from_email, s.daily_limit, COALESCE(u.sent_count,0) AS sent_today
			FROM user_smtp_servers s LEFT JOIN user_smtp_daily_usage u ON u.smtp_uuid=s.uuid AND u.usage_date=$2::DATE
			WHERE (s.user_id=$1 OR s.smtp_pool_id=-$1) AND s.enabled ORDER BY s.id`, key, currentLocalDate())
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{rows})
}

func normalizeCampaignSMTPSource(camp *models.Campaign, orgID int) error {
	if camp.SMTPSource == "" {
		camp.SMTPSource = "personal"
	}
	if camp.SMTPSource != "personal" && camp.SMTPSource != "organization" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid SMTP source")
	}
	if camp.SMTPSource == "organization" && orgID < 1 && camp.PoolScope != models.CampaignPoolScopeAllOrganizations {
		return echo.NewHTTPError(http.StatusBadRequest, "organization SMTP requires an organization campaign")
	}
	return nil
}

func (a *App) requireCampaignSMTPAvailable(camp models.Campaign) error {
	key := camp.OwnerUserID.Int
	if camp.SMTPSource == "organization" {
		key = -camp.SMTPPoolID.Int
	}
	if key == 0 {
		return echo.NewHTTPError(http.StatusConflict, "campaign has no SMTP owner")
	}
	if err := a.requirePersonalSMTPAvailable(key); err != nil {
		if key < 0 {
			return echo.NewHTTPError(http.StatusConflict, "configure at least one enabled organization marketing SMTP in Manage organization")
		}
		return err
	}
	return nil
}
