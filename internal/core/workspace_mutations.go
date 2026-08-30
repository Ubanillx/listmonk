package core

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// workspaceMutationError is deliberately a conflict rather than a not-found
// response. The request may have passed its read-time authorization check, but
// the organization/member/resource state changed before the write acquired
// its locks. Keeping this distinction visible helps clients refresh the active
// workspace instead of retrying a stale mutation indefinitely.
func workspaceMutationError() error {
	return echo.NewHTTPError(http.StatusConflict,
		"the resource is no longer writable in the active workspace")
}

// withWorkspaceCreation serializes creation with organization removal and
// archival. A caller that has already resolved WorkspaceAccess must still hold
// the organization lock while its INSERT runs; otherwise a member could leave
// between the membership check and the insert and leave a new orphaned row in
// an organization that the caller no longer belongs to.
func (c *Core) withWorkspaceCreation(access models.WorkspaceAccess, fn func(*sqlx.Tx) error) error {
	if fn == nil || access.Archived {
		return workspaceMutationError()
	}
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return workspaceQueryError("starting workspace mutation", err)
	}
	defer tx.Rollback()

	if access.IsOrganization() {
		if err := c.lockActiveWorkspaceOrganization(tx, access.OrganizationID); err != nil {
			return err
		}
		if !access.PlatformAdmin && !c.workspaceMembershipActive(tx, access.OrganizationID, access.UserID) {
			return workspaceMutationError()
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceQueryError("committing workspace mutation", err)
	}
	return nil
}

// withWorkspaceResourceMutation locks a complete set of resources before the
// callback executes. Organization lifecycle operations acquire the same
// organization row lock first, so a successful callback is ordered either
// before a member leaves/archival or after it (in which case validation
// rejects the stale request).
func (c *Core) withWorkspaceResourceMutation(access models.WorkspaceAccess, resource string, ids []int, fn func(*sqlx.Tx) error) error {
	if _, ok := workspaceResourceTables[resource]; !ok || fn == nil {
		return workspaceMutationError()
	}
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	// Lock rows in a stable order even when a client supplied IDs in an
	// arbitrary order. The callback may still use the caller's semantic order,
	// but the database locks must not depend on it.
	sort.Ints(ids)
	if access.Archived {
		return workspaceMutationError()
	}

	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return workspaceQueryError("starting workspace mutation", err)
	}
	defer tx.Rollback()

	// A platform administrator may submit IDs from more than one workspace.
	// Resolve and lock all organization rows in a stable order before locking
	// resources, avoiding deadlocks with archive/transfer transactions.
	orgIDs, err := c.resourceOrganizationIDs(tx, resource, ids, access)
	if err != nil {
		return workspaceQueryError("resolving workspace mutation", err)
	}
	lockedStatuses, err := c.lockWorkspaceOrganizations(tx, orgIDs)
	if err != nil {
		return err
	}
	if access.IsOrganization() {
		if !access.PlatformAdmin && !c.workspaceMembershipActive(tx, access.OrganizationID, access.UserID) {
			return workspaceMutationError()
		}
	}

	if err := c.lockWorkspaceMutationResources(tx, access, resource, ids); err != nil {
		return err
	}
	// Re-read organization ownership after the resource row locks. A transfer
	// that committed between the discovery query and the lock must not let a
	// platform-admin bulk mutation continue against a newly archived or
	// cross-workspace row that was outside the lock set.
	if access.PlatformAdmin {
		currentOrgIDs, err := c.resourceOrganizationIDs(tx, resource, ids, access)
		if err != nil {
			return workspaceQueryError("rechecking workspace mutation", err)
		}
		for _, orgID := range currentOrgIDs {
			status, ok := lockedStatuses[orgID]
			if !ok || status != models.OrganizationStatusActive {
				return workspaceMutationError()
			}
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return workspaceQueryError("committing workspace mutation", err)
	}
	return nil
}

// lockActiveWorkspaceOrganization locks and validates one organization. All
// organization lifecycle code uses lockOrganization, which is equivalent to
// the lock acquired here; checking status while holding it closes the archive
// race between authorization and mutation.
func (c *Core) lockActiveWorkspaceOrganization(tx *sqlx.Tx, orgID int) error {
	var status string
	if err := tx.Get(&status, `SELECT status FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		return workspaceMutationError()
	}
	if status != models.OrganizationStatusActive {
		return workspaceMutationError()
	}
	return nil
}

// lockWorkspaceOrganizations acquires every organization lock in ascending
// order and returns the status that was observed while each row was locked.
// Organization lifecycle operations use the same row lock, so callers can
// safely make a decision about an organization's active/archived state for the
// remainder of the transaction. Keeping the lock order deterministic is
// important for operations that touch a source and a destination workspace at
// the same time (copy, migration, and resource transfer).
func (c *Core) lockWorkspaceOrganizations(tx *sqlx.Tx, orgIDs []int) (map[int]string, error) {
	orgIDs = uniqueMutationIDs(orgIDs)
	sort.Ints(orgIDs)
	statuses := make(map[int]string, len(orgIDs))
	if len(orgIDs) == 0 {
		return statuses, nil
	}

	type organizationLock struct {
		ID     int    `db:"id"`
		Status string `db:"status"`
	}
	var rows []organizationLock
	if err := tx.Select(&rows, `
		SELECT id, status FROM organizations
		WHERE id = ANY($1::BIGINT[])
		ORDER BY id
		FOR UPDATE`, pq.Array(orgIDs)); err != nil {
		return nil, workspaceQueryError("locking workspace organizations", err)
	}
	if len(rows) != len(orgIDs) {
		return nil, workspaceMutationError()
	}
	for _, row := range rows {
		statuses[row.ID] = row.Status
	}
	return statuses, nil
}

// lockWorkspaceTarget validates the destination of a cross-workspace copy or
// migration while holding the destination organization row lock. A stale
// membership check from the HTTP layer therefore cannot create a resource
// after the user has left (or after the organization has been archived).
func (c *Core) lockWorkspaceTarget(tx *sqlx.Tx, target models.WorkspaceAccess) error {
	if target.Archived {
		return workspaceMutationError()
	}
	if !target.IsOrganization() {
		return nil
	}
	statuses, err := c.lockWorkspaceOrganizations(tx, []int{target.OrganizationID})
	if err != nil {
		return err
	}
	if statuses[target.OrganizationID] != models.OrganizationStatusActive {
		return workspaceMutationError()
	}
	if !target.PlatformAdmin && !c.workspaceMembershipActive(tx, target.OrganizationID, target.UserID) {
		return workspaceMutationError()
	}
	return nil
}

func (c *Core) workspaceMembershipActive(tx *sqlx.Tx, orgID, userID int) bool {
	var active bool
	if err := tx.Get(&active, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND removed_at IS NULL
		)`, orgID, userID); err != nil {
		return false
	}
	return active
}

// resourceOrganizationIDs determines which organization locks are needed for
// a platform-admin bulk operation. Ordinary members have one explicit target
// organization and do not need this extra read.
func (c *Core) resourceOrganizationIDs(tx *sqlx.Tx, resource string, ids []int, access models.WorkspaceAccess) ([]int, error) {
	if access.IsOrganization() && !access.PlatformAdmin {
		return []int{access.OrganizationID}, nil
	}
	if !access.PlatformAdmin {
		return nil, nil
	}
	table := workspaceResourceTables[resource]
	var orgIDs []int
	if err := tx.Select(&orgIDs, fmt.Sprintf(`
		SELECT DISTINCT organization_id FROM %s
		WHERE id = ANY($1::INT[]) AND organization_id IS NOT NULL
		ORDER BY organization_id`, table), pq.Array(ids)); err != nil {
		return nil, err
	}
	// The query is already ordered, but de-duplicate defensively for database
	// drivers that may return repeated values from a custom plan.
	orgIDs = uniqueMutationIDs(orgIDs)
	sort.Ints(orgIDs)
	return orgIDs, nil
}

// lockWorkspaceMutationResources rechecks the ownership boundary and locks
// every requested row. It is also used for related list IDs by subscriber and
// campaign operations after their primary resource has been locked.
func (c *Core) lockWorkspaceMutationResources(tx *sqlx.Tx, access models.WorkspaceAccess, resource string, ids []int) error {
	ids = uniqueMutationIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	table := workspaceResourceTables[resource]
	where := "id = ANY($1::INT[]) AND transfer_pending_at IS NULL"
	args := []any{pq.Array(ids)}
	if !access.PlatformAdmin {
		if access.IsOrganization() {
			where += " AND organization_id = $2 AND owner_user_id = $3"
			args = append(args, access.OrganizationID, access.UserID)
		} else {
			where += " AND organization_id IS NULL AND owner_user_id = $2"
			args = append(args, access.UserID)
		}
	}
	var locked []int
	sort.Ints(ids)
	if err := tx.Select(&locked, fmt.Sprintf("SELECT id FROM %s WHERE %s ORDER BY id FOR UPDATE", table, where), args...); err != nil {
		return workspaceQueryError("locking workspace resources", err)
	}
	if len(locked) != len(ids) {
		return workspaceMutationError()
	}
	return nil
}

func uniqueMutationIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id < 1 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
