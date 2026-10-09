package core

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// DeleteListsWithCustomersInWorkspace optionally removes only contacts whose
// memberships are all in the selected lists. Customer and list writes commit
// together, and customers owned by another workspace/user are preserved.
func (c *Core) DeleteListsWithCustomersInWorkspace(access models.WorkspaceAccess, ids []int, deleteCustomers bool) (int64, error) {
	if !deleteCustomers {
		return 0, c.DeleteListsInWorkspace(access, ids)
	}
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	args := []any{pq.Array(ids)}
	owner := "TRUE"
	if !access.PlatformAdmin {
		if access.IsOrganization() {
			owner = "customer.organization_id=$2 AND customer.owner_user_id=$3"
			args = append(args, access.OrganizationID, access.UserID)
		} else {
			owner = "customer.organization_id IS NULL AND customer.owner_user_id=$2"
			args = append(args, access.UserID)
		}
	}
	customerQuery := fmt.Sprintf(`SELECT customer.id FROM customers customer
		WHERE customer.transfer_pending_at IS NULL AND %s AND EXISTS (
			SELECT 1 FROM customer_list_memberships membership
			JOIN customer_lists list ON list.id=membership.customer_list_id
			WHERE membership.customer_id=customer.id AND membership.customer_list_id=ANY($1::INT[])
			AND list.type NOT IN ('pool','org_pool_allocation')
			AND customer.organization_id IS NOT DISTINCT FROM list.organization_id
		) ORDER BY customer.id`, owner)
	poolQuery := `SELECT contact.id FROM pool_contacts contact WHERE EXISTS (
		SELECT 1 FROM pool_members member JOIN customer_lists list ON list.id=member.pool_id
		WHERE member.contact_id=contact.id AND member.pool_id=ANY($1::INT[]) AND list.type='pool'
	) ORDER BY contact.id`
	var customerIDs, poolIDs []int64
	var deleted int64
	err := c.withPreparedWorkspaceResourceMutation(access, resourceLists, ids, func(tx *sqlx.Tx) error {
		if access.IsOrganization() {
			if err := c.lockActiveWorkspaceOrganization(tx, access.OrganizationID); err != nil {
				return err
			}
		}
		if err := tx.Select(&customerIDs, customerQuery+" FOR UPDATE", args...); err != nil {
			return workspaceQueryError("locking list customers", err)
		}
		if err := tx.Select(&poolIDs, poolQuery+" FOR UPDATE", pq.Array(ids)); err != nil {
			return workspaceQueryError("locking list pool contacts", err)
		}
		return nil
	}, func(tx *sqlx.Tx) error {
		// A new customer might have joined before the list locks were taken.
		// Reject that race instead of taking customer locks in the reverse order.
		for i, query := range []struct {
			sql    string
			args   []any
			locked []int64
		}{
			{customerQuery, args, customerIDs},
			{poolQuery, []any{pq.Array(ids)}, poolIDs},
		} {
			var current []int64
			if err := tx.Select(&current, query.sql, query.args...); err != nil {
				return workspaceQueryError("rechecking list customers", err)
			}
			locked := make(map[int64]bool, len(query.locked))
			for _, id := range query.locked {
				locked[id] = true
			}
			for _, id := range current {
				if !locked[id] {
					return workspaceMutationError()
				}
			}
			if i == 0 {
				customerIDs = current
			} else {
				poolIDs = current
			}
		}
		// Recheck sharing after the contact locks. New membership inserts need
		// a foreign-key lock on the same contacts and wait for this transaction.
		for _, query := range []struct {
			sql string
			ids []int64
		}{
			{`DELETE FROM customers customer WHERE customer.id=ANY($1::BIGINT[])
				AND NOT EXISTS (SELECT 1 FROM customer_list_memberships membership
					WHERE membership.customer_id=customer.id AND
					(membership.customer_list_id IS NULL OR NOT membership.customer_list_id=ANY($2::INT[])))`, customerIDs},
			{`DELETE FROM pool_contacts contact WHERE contact.id=ANY($1::BIGINT[])
				AND NOT EXISTS (SELECT 1 FROM pool_members member
					WHERE member.contact_id=contact.id AND NOT member.pool_id=ANY($2::INT[]))
				AND NOT EXISTS (SELECT 1 FROM org_pool_allocation_members member
					JOIN org_pool_allocations allocation ON allocation.id=member.allocation_id
					WHERE member.contact_id=contact.id AND
					(allocation.pool_id IS NULL OR NOT allocation.pool_id=ANY($2::INT[])))`, poolIDs},
		} {
			if len(query.ids) == 0 {
				continue
			}
			result, err := tx.Exec(query.sql, pq.Array(query.ids), pq.Array(ids))
			if err != nil {
				return workspaceQueryError("deleting unshared list customers", err)
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			deleted += count
		}
		if _, err := tx.Stmtx(c.q.DeleteLists).Exec(pq.Array(ids), "", true, pq.Array(nil)); err != nil {
			return workspaceQueryError("deleting customer_lists", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
