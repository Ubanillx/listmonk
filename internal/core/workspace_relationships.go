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
		) OR EXISTS(
			SELECT 1
			FROM campaigns camp
			JOIN campaign_pool_recipients cpr ON cpr.campaign_id = camp.id
			WHERE camp.id = $%d AND (%s)
		)`, idArg, scope, idArg, scope)
	args = append(args, id)
	var has bool
	if err := c.db.Get(&has, stmt, args...); err != nil {
		return false, workspaceQueryError("checking campaign recipients", err)
	}
	return has, nil
}

// GetCampaignCustomerListIDsInWorkspace returns the historical campaign-customer_list
// snapshot after constraining the campaign itself to the active workspace.
// Deleted customer_lists are represented as zero, matching the legacy helper.
func (c *Core) GetCampaignCustomerListIDsInWorkspace(access models.WorkspaceAccess, id int) ([]int, error) {
	if id < 1 {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	scope, args := workspaceReadPredicate(access, "camp", 1)
	idArg := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT COALESCE(cl.customer_list_id,
			CASE WHEN cl.pool_segment_id IS NOT NULL THEN (
				SELECT ps.list_id FROM pool_segments ps WHERE ps.id = cl.pool_segment_id
			) ELSE cl.pool_id END,
			0) AS id
		FROM campaign_customer_lists cl
		JOIN campaigns camp ON camp.id = cl.campaign_id
		WHERE camp.id = $%d AND (%s)
		ORDER BY cl.customer_list_id NULLS FIRST`, idArg, scope)
	args = append(args, id)
	out := []int{}
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, workspaceQueryError("fetching campaign customer_lists", err)
	}
	return out, nil
}

// CampaignHasListsInWorkspace checks a campaign/customer_list relationship only after
// constraining the campaign row to the selected workspace.  It is used for
// the legacy per-customer_list permission model, which otherwise could authorize a
// campaign through a stale cross-organization relationship row.
func (c *Core) CampaignHasListsInWorkspace(access models.WorkspaceAccess, id int, customerListIDs []int) (bool, error) {
	if id < 1 {
		return false, echo.NewHTTPError(http.StatusBadRequest,
			c.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	scope, args := workspaceReadPredicate(access, "camp", 1)
	idArg := len(args) + 1
	listArg := idArg + 1
	stmt := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1
			FROM campaigns camp
			JOIN campaign_customer_lists cl ON cl.campaign_id = camp.id
			WHERE camp.id = $%d AND (%s) AND cl.customer_list_id = ANY($%d::INT[])
		)`, idArg, scope, listArg)
	args = append(args, id, pq.Array(customerListIDs))
	var has bool
	if err := c.db.Get(&has, stmt, args...); err != nil {
		return false, workspaceQueryError("checking campaign customer_lists", err)
	}
	return has, nil
}

// HasCustomerListMembershipsInWorkspace is the workspace-aware counterpart to the
// legacy customer_list-permission query.  It initializes every requested customer to
// false, including rows hidden by the workspace predicate, so callers cannot
// mistake a missing row for an authorized one.
func (c *Core) HasCustomerListMembershipsInWorkspace(access models.WorkspaceAccess, subIDs []int, customerListIDs []int) (map[int]bool, error) {
	if subIDs == nil {
		subIDs = []int{}
	}
	if customerListIDs == nil {
		customerListIDs = []int{}
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
		SELECT s.id AS customer_id,
			EXISTS(
				SELECT 1
				FROM customer_list_memberships sl
				JOIN customer_lists l ON l.id = sl.customer_list_id
				WHERE sl.customer_id = s.id
					AND sl.customer_list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM s.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
					AND l.transfer_pending_at IS NULL
			) AS has
		FROM customers s
		WHERE s.id = ANY($%d::INT[]) AND (%s)`, listArg, subArg, scope)
	args = append(args, pq.Array(subIDs), pq.Array(customerListIDs))
	var rows []struct {
		CustomerID int  `db:"customer_id"`
		Has        bool `db:"has"`
	}
	if err := c.db.Select(&rows, stmt, args...); err != nil {
		return nil, workspaceQueryError("checking customer customer_lists", err)
	}
	for _, row := range rows {
		if _, requested := out[row.CustomerID]; requested {
			out[row.CustomerID] = row.Has
		}
	}
	return out, nil
}

// GetListsByOptinInWorkspace resolves the double-opt-in customer_lists used to build
// an opt-in campaign body.  Unlike the historical ID-only query, this method
// binds every returned customer_list to the campaign creator's selected workspace and
// owner.  That remains true for platform administrators, whose broad read
// access must not change the destination workspace of a newly-created
// campaign.
func (c *Core) GetListsByOptinInWorkspace(access models.WorkspaceAccess, ids []int, optinType string) ([]models.CustomerList, error) {
	if ids == nil {
		ids = []int{}
	}
	var organization any
	if access.IsOrganization() {
		organization = access.OrganizationID
	}
	stmt := `
		SELECT l.*
		FROM customer_lists l
		WHERE l.id = ANY($1::INT[])
		  AND ($2 = '' OR l.optin = $2::customer_list_optin)
		  AND l.organization_id IS NOT DISTINCT FROM $3::BIGINT
		  AND l.owner_user_id = $4
		  AND l.transfer_pending_at IS NULL
		  AND (l.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organizations o
			WHERE o.id = l.organization_id AND o.status = 'active'
		))
		ORDER BY l.name`
	var out []models.CustomerList
	if err := c.db.Select(&out, stmt, pq.Array(ids), optinType, organization, access.UserID); err != nil {
		return nil, workspaceQueryError("fetching opt-in customer_lists", err)
	}
	return out, nil
}
