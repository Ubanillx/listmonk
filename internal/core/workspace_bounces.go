package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// workspaceManagedCustomerPredicate is the write counterpart to
// workspaceCustomerReadPredicate. Organization managers may inspect a
// member's bounces but may only delete or blocklist bounces for customers
// they own themselves.
func workspaceManagedCustomerPredicate(access models.WorkspaceAccess, alias string, firstArg int) (string, []any) {
	field := func(name string) string { return alias + "." + name }
	arg := func(offset int) string { return fmt.Sprintf("$%d", firstArg+offset) }
	if access.PlatformAdmin {
		return "TRUE", nil
	}
	if access.IsOrganization() {
		return fmt.Sprintf("(%sorganization_id = %s AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
			field(""), arg(0), field(""), arg(1), field("")), []any{access.OrganizationID, access.UserID}
	}
	return fmt.Sprintf("(%sorganization_id IS NULL AND %sowner_user_id = %s AND %stransfer_pending_at IS NULL)",
		field(""), field(""), arg(0), field("")), []any{access.UserID}
}

// QueryWorkspaceBounces returns bounce history through the scoped customer
// relationship. The optional bounceID is used for single-record lookups.
func (c *Core) QueryWorkspaceBounces(access models.WorkspaceAccess, bounceID, campaignID, customerID int, source, orderBy, order string, offset, limit int) ([]models.Bounce, int, error) {
	fields := map[string]string{
		"id":            "b.id",
		"email":         "s.email",
		"campaign_name": "campaign_name",
		"source":        "b.source",
		"created_at":    "b.created_at",
		"type":          "b.type",
	}
	if _, ok := fields[orderBy]; !ok {
		orderBy = "created_at"
	}
	if !strings.EqualFold(order, SortAsc) {
		order = SortDesc
	}

	scope, args := workspaceCustomerReadPredicate(access, "s", 1)
	first := len(args) + 1
	poolScope := "FALSE"
	if access.PlatformAdmin {
		poolScope = "TRUE"
	} else if access.IsOrganization() {
		poolScope = fmt.Sprintf("b.source_organization_id = $%d", first)
		args = append(args, access.OrganizationID)
		first++
	}
	stmt := fmt.Sprintf(`
		SELECT COUNT(*) OVER () AS total,
			b.id, b.type, b.source, b.meta, b.created_at, b.customer_id,
			b.pool_contact_id, b.source_pool_id, b.source_segment_id, b.source_organization_id,
			COALESCE(s.uuid, pc.uuid::text, '') AS customer_uuid, COALESCE(s.email, pc.email, '') AS email, COALESCE(s.status, pc.status, '') AS customer_status,
			s.organization_id, s.owner_user_id, s.transfer_pending_at,
			CASE WHEN b.campaign_id IS NOT NULL
				THEN JSON_BUILD_OBJECT('id', b.campaign_id, 'name', c.name)
				ELSE NULL END AS campaign
		FROM bounces b
		LEFT JOIN customers s ON s.id = b.customer_id
		LEFT JOIN pool_contacts pc ON pc.id = b.pool_contact_id
		-- A bounce row is keyed by customer, while campaign_id is supplied by
		-- the delivery provider and may be stale or malformed.  Keep the optional
		-- campaign label inside the customer's same workspace/owner boundary;
		-- otherwise a forged historical relation could disclose another tenant's
		-- campaign name to an organization manager.
		LEFT JOIN campaigns c ON c.id = b.campaign_id
			AND c.organization_id IS NOT DISTINCT FROM s.organization_id
			AND c.owner_user_id IS NOT DISTINCT FROM s.owner_user_id
		WHERE ((s.id IS NOT NULL AND (%s)) OR (b.pool_contact_id IS NOT NULL AND %s))
			AND ($%d = 0 OR b.id = $%d)
			AND ($%d = 0 OR b.campaign_id = $%d)
			AND ($%d = 0 OR b.customer_id = $%d)
			AND ($%d = '' OR b.source = $%d)
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		scope, poolScope,
		first, first,
		first+1, first+1,
		first+2, first+2,
		first+3, first+3,
		workspaceSort(orderBy, order, fields, "b.created_at"), first+4, first+5, first+5)
	args = append(args, bounceID, campaignID, customerID, source, offset, limit)

	out := []models.Bounce{}
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching bounces", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

func (c *Core) GetWorkspaceBounce(access models.WorkspaceAccess, id int) (models.Bounce, error) {
	out, _, err := c.QueryWorkspaceBounces(access, id, 0, 0, "", "id", SortAsc, 0, 1)
	if err != nil {
		return models.Bounce{}, err
	}
	if len(out) == 0 {
		return models.Bounce{}, echo.NewHTTPError(http.StatusNotFound, "bounce is outside the active workspace")
	}
	return out[0], nil
}

// DeleteWorkspaceBounces removes only bounces for customers owned by the
// caller in the selected workspace. An empty explicit ID set is a no-op.
func (c *Core) DeleteWorkspaceBounces(access models.WorkspaceAccess, ids []int, all bool) error {
	if !all && len(ids) == 0 {
		return nil
	}
	scope, args := workspaceManagedCustomerPredicate(access, "s", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		DELETE FROM bounces b
		USING customers s
		WHERE b.customer_id = s.id AND (%s)
			AND ($%d::BOOLEAN OR b.id = ANY($%d::INT[]))`, scope, first, first+1)
	args = append(args, all, pq.Array(ids))
	if _, err := c.db.Exec(stmt, args...); err != nil {
		return workspaceQueryError("deleting bounces", err)
	}
	return nil
}

// BlocklistWorkspaceBouncedCustomers applies the bounce action only to the
// current caller's customer records. This keeps an organization manager's
// read-only access from becoming a member-wide bulk mutation capability.
func (c *Core) BlocklistWorkspaceBouncedCustomers(access models.WorkspaceAccess) error {
	scope, args := workspaceManagedCustomerPredicate(access, "s", 1)
	stmt := fmt.Sprintf(`
		WITH bounced AS (
			SELECT DISTINCT s.id FROM bounces b
			JOIN customers s ON s.id = b.customer_id
			WHERE (%s)
		), updated AS (
			UPDATE customers SET status = 'blocklisted', updated_at = NOW()
			WHERE id IN (SELECT id FROM bounced)
			RETURNING id
		)
		UPDATE customer_list_memberships SET status = 'unsubscribed', updated_at = NOW()
		WHERE customer_id IN (SELECT id FROM updated)`, scope)
	if _, err := c.db.Exec(stmt, args...); err != nil {
		return workspaceQueryError("blocklisting bounced customers", err)
	}
	// Logical pool exclusions are organization-scoped; unlike legacy customers,
	// no global blocklist row is mutated.
	poolStmt := `INSERT INTO pool_segment_exclusions(pool_id,organization_id,contact_id,segment_id,reason,source)
		SELECT source_pool_id,source_organization_id,pool_contact_id,source_segment_id,'bounce','bounce'
		FROM bounces WHERE pool_contact_id IS NOT NULL AND source_pool_id IS NOT NULL AND source_organization_id IS NOT NULL %s
		ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE SET reason='bounce',source='bounce',removed_at=NOW(),restored_at=NULL`
	if access.PlatformAdmin {
		_, err := c.db.Exec(fmt.Sprintf(poolStmt, ""))
		return err
	}
	if access.IsOrganization() {
		_, err := c.db.Exec(fmt.Sprintf(poolStmt, `AND source_organization_id=$1`), access.OrganizationID)
		return err
	}
	return nil
}
