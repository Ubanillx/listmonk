package core

import (
	"fmt"
	"net/http"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// poolAllocationReadPredicate is shared by department directories and contact
// queries. alias is a code-owned SQL table alias, never request input.
func poolAllocationReadPredicate(alias string) string {
	return fmt.Sprintf(`%[1]s.state<>'detached' AND EXISTS (
		SELECT 1 FROM customer_lists allocation_list
		JOIN customer_lists source_list ON source_list.id=%[1]s.pool_id AND source_list.type='pool'
		WHERE allocation_list.id=%[1]s.list_id AND allocation_list.type='org_pool_allocation'
			AND allocation_list.transfer_pending_at IS NULL AND source_list.transfer_pending_at IS NULL
	)`, alias)
}

// QueryAllocatedPoolLists is the public-pool directory of one workspace.
// Functional privileges and delivery grants do not expand this read scope.
func (c *Core) QueryAllocatedPoolLists(organizationID int, status string) ([]models.CustomerList, error) {
	return c.queryAllocatedPoolLists(organizationID, status, 0)
}

func (c *Core) queryAllocatedPoolLists(organizationID int, status string, listID int) ([]models.CustomerList, error) {
	rows := make([]models.CustomerList, 0)
	if organizationID <= 0 {
		return rows, nil
	}
	err := c.db.Select(&rows, `
		SELECT l.*, COALESCE(o.name,'') AS organization_name,
			COALESCE(u.username,'') AS owner_username, COALESCE(u.name,'') AS owner_name,
			(SELECT COUNT(DISTINCT am.contact_id)
			 FROM org_pool_allocations a JOIN org_pool_allocation_members am ON am.allocation_id=a.id
			 JOIN pool_members pm ON pm.pool_id=a.pool_id AND pm.contact_id=am.contact_id
			 WHERE a.organization_id=$1 AND `+poolAllocationReadPredicate("a")+` AND am.status='active'
				AND ((l.type='pool' AND a.pool_id=l.id) OR (l.type='org_pool_allocation' AND a.list_id=l.id))) AS customer_count
		FROM customer_lists l
		LEFT JOIN organizations o ON o.id=l.organization_id
		LEFT JOIN users u ON u.id=COALESCE(l.owner_user_id,l.original_owner_user_id)
		WHERE l.type IN ('pool','org_pool_allocation') AND l.transfer_pending_at IS NULL
			AND ($2='' OR l.status::text=$2) AND ($3=0 OR l.id=$3) AND EXISTS (
				SELECT 1 FROM org_pool_allocations a
				WHERE a.organization_id=$1 AND `+poolAllocationReadPredicate("a")+`
					AND ((l.type='pool' AND a.pool_id=l.id) OR (l.type='org_pool_allocation' AND a.list_id=l.id)))
		ORDER BY l.name,l.id`, organizationID, status, listID)
	for i := range rows {
		rows[i].PoolDeliveryAllowed = true
	}
	return rows, err
}

// GetAllocatedPoolList uses the same metadata and counts as the directory.
func (c *Core) GetAllocatedPoolList(organizationID, listID int) (models.CustomerList, error) {
	rows, err := c.queryAllocatedPoolLists(organizationID, "", listID)
	if err != nil {
		return models.CustomerList{}, err
	}
	for _, row := range rows {
		if row.ID == listID {
			return row, nil
		}
	}
	return models.CustomerList{}, echo.NewHTTPError(http.StatusForbidden, "public pool is not allocated to the active organization")
}
