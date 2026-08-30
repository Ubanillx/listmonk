package core

import (
	"fmt"
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// HasCampaignRecipientsInWorkspace reports whether a campaign has a recipient
// snapshot while constraining the campaign row to the caller's active
// workspace.  The legacy helper only accepts an ID and is retained for public
// delivery/maintenance code; authenticated handlers must use this variant so
// a stale or forged campaign ID cannot be used to inspect another workspace.
func (c *Core) HasCampaignRecipientsInWorkspace(access models.WorkspaceAccess, id int) (bool, error) {
	if id < 1 {
		return false, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	scope, args := workspaceReadPredicate(access, "camp", 1)
	idArg := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1
			FROM campaigns camp
			JOIN campaign_recipients cr ON cr.campaign_id = camp.id
			WHERE camp.id = $%d AND (%s)
		)`, idArg, scope)
	args = append(args, id)
	var has bool
	if err := c.db.Get(&has, stmt, args...); err != nil {
		return false, workspaceQueryError("checking campaign recipients", err)
	}
	return has, nil
}

// GetCampaignListIDsInWorkspace returns the historical campaign-list
// snapshot after constraining the campaign itself to the active workspace.
// Deleted lists are represented as zero, matching the legacy helper.
func (c *Core) GetCampaignListIDsInWorkspace(access models.WorkspaceAccess, id int) ([]int, error) {
	if id < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	scope, args := workspaceReadPredicate(access, "camp", 1)
	idArg := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT COALESCE(cl.list_id, 0) AS id
		FROM campaign_lists cl
		JOIN campaigns camp ON camp.id = cl.campaign_id
		WHERE camp.id = $%d AND (%s)
		ORDER BY cl.list_id NULLS FIRST`, idArg, scope)
	args = append(args, id)
	out := []int{}
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaign lists", err)
	}
	return out, nil
}

// CampaignHasListsInWorkspace checks a campaign/list relationship only after
// constraining the campaign row to the selected workspace.  It is used for
// the legacy per-list permission model, which otherwise could authorize a
// campaign through a stale cross-organization relationship row.
func (c *Core) CampaignHasListsInWorkspace(access models.WorkspaceAccess, id int, listIDs []int) (bool, error) {
	if id < 1 {
		return false, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	if listIDs == nil {
		listIDs = []int{}
	}
	scope, args := workspaceReadPredicate(access, "camp", 1)
	idArg := len(args) + 1
	listArg := idArg + 1
	stmt := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1
			FROM campaigns camp
			JOIN campaign_lists cl ON cl.campaign_id = camp.id
			WHERE camp.id = $%d AND (%s) AND cl.list_id = ANY($%d::INT[])
		)`, idArg, scope, listArg)
	args = append(args, id, pq.Array(listIDs))
	var has bool
	if err := c.db.Get(&has, stmt, args...); err != nil {
		return false, workspaceQueryError("checking campaign lists", err)
	}
	return has, nil
}

// HasSubscriberListsInWorkspace is the workspace-aware counterpart to the
// legacy list-permission query.  It initializes every requested subscriber to
// false, including rows hidden by the workspace predicate, so callers cannot
// mistake a missing row for an authorized one.
func (c *Core) HasSubscriberListsInWorkspace(access models.WorkspaceAccess, subIDs []int, listIDs []int) (map[int]bool, error) {
	if subIDs == nil {
		subIDs = []int{}
	}
	if listIDs == nil {
		listIDs = []int{}
	}
	out := make(map[int]bool, len(subIDs))
	for _, id := range subIDs {
		if id > 0 {
			out[id] = false
		}
	}
	if len(subIDs) == 0 {
		return out, nil
	}
	scope, args := workspaceOwnerScopedReadPredicate(access, "s", 1)
	subArg := len(args) + 1
	listArg := subArg + 1
	stmt := fmt.Sprintf(`
		SELECT s.id AS subscriber_id,
			EXISTS(
				SELECT 1
				FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = s.id
					AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM s.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
					AND l.transfer_pending_at IS NULL
			) AS has
		FROM subscribers s
		WHERE s.id = ANY($%d::INT[]) AND (%s)`, listArg, subArg, scope)
	args = append(args, pq.Array(subIDs), pq.Array(listIDs))
	var rows []struct {
		SubscriberID int  `db:"subscriber_id"`
		Has          bool `db:"has"`
	}
	if err := c.db.Select(&rows, stmt, args...); err != nil {
		return nil, workspaceQueryError("checking subscriber lists", err)
	}
	for _, row := range rows {
		if _, requested := out[row.SubscriberID]; requested {
			out[row.SubscriberID] = row.Has
		}
	}
	return out, nil
}

// GetListsByOptinInWorkspace resolves the double-opt-in lists used to build
// an opt-in campaign body.  Unlike the historical ID-only query, this method
// binds every returned list to the campaign creator's selected workspace and
// owner.  That remains true for platform administrators, whose broad read
// access must not change the destination workspace of a newly-created
// campaign.
func (c *Core) GetListsByOptinInWorkspace(access models.WorkspaceAccess, ids []int, optinType string) ([]models.List, error) {
	if ids == nil {
		ids = []int{}
	}
	var organization any
	if access.IsOrganization() {
		organization = access.OrganizationID
	}
	stmt := `
		SELECT l.*
		FROM lists l
		WHERE l.id = ANY($1::INT[])
		  AND ($2 = '' OR l.optin = $2::list_optin)
		  AND l.organization_id IS NOT DISTINCT FROM $3::BIGINT
		  AND l.owner_user_id = $4
		  AND l.transfer_pending_at IS NULL
		  AND (l.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations o
			WHERE o.id = l.organization_id AND o.status = 'active'
		))
		ORDER BY l.name`
	var out []models.List
	if err := c.db.Select(&out, stmt, pq.Array(ids), optinType, organization, access.UserID); err != nil {
		return nil, workspaceQueryError("fetching opt-in lists", err)
	}
	return out, nil
}
