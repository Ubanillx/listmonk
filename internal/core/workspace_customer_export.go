package core

import (
	"fmt"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// exportWorkspaceCustomers streams only customers inside the caller's mutable
// workspace boundary. All filters are passed as SQL parameters.
func (c *Core) exportWorkspaceCustomers(access models.WorkspaceAccess, search string, customerListIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.CustomerExport, error), error) {
	if batchSize < 1 {
		batchSize = 1000
	}
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	if requestedIDs == nil {
		requestedIDs = []int{-1}
	}

	scope, args := workspaceSensitiveCustomerPredicate(access, "customers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT customers.id, customers.uuid, customers.email, customers.name, customers.attribs,
			customers.status, customers.customer_code, customers.created_at, customers.updated_at
		FROM customers
		WHERE (%s) AND customers.id > $%d
			AND customers.id = ANY($%d::INT[])
			AND ($%d = '' OR customers.name ~* $%d OR customers.email ~* $%d OR customers.customer_code ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM customer_list_memberships sl
				JOIN customer_lists l ON l.id = sl.customer_list_id
				WHERE sl.customer_id = customers.id
					AND sl.customer_list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM customers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM customers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))
		ORDER BY customers.id ASC LIMIT $%d`,
		scope,
		first,
		first+1,
		first+2, first+2, first+2, first+2,
		first+3, first+3, first+4, first+4,
		first+5)
	baseArgs := append([]any{}, args...)
	baseArgs = append(baseArgs, 0, pq.Array(requestedIDs), strings.TrimSpace(search), pq.Array(customerListIDs), subscriptionStatus, batchSize)

	lastID := 0
	return func() ([]models.CustomerExport, error) {
		callArgs := append([]any{}, baseArgs...)
		callArgs[len(args)] = lastID
		var out []models.CustomerExport
		if err := c.db.Select(&out, stmt, callArgs...); err != nil {
			return nil, workspaceQueryError("exporting customers", err)
		}
		if len(out) > 0 {
			lastID = out[len(out)-1].ID
		}
		return out, nil
	}, nil
}
