package core

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// PoolRecipient is the server-side sending snapshot source for a pool contact.
// ReplyMailboxID is the organization's unified reply mailbox, resolved only
// while that mailbox is active and verified.
type PoolRecipient struct {
	models.PoolContact
	AllocationID   int64 `db:"allocation_id" json:"allocation_id"`
	OrganizationID int64 `db:"organization_id" json:"organization_id"`
	ReplyMailboxID *int  `db:"reply_mailbox_id" json:"reply_mailbox_id,omitempty"`
}

func normalizePoolAllocationDepartment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// QueryOrgPoolAllocations lists the pool allocations of one pool. An allocation
// no longer carries a mailbox of its own, so reply_mailbox_id/reply_mailbox_email
// report the mailbox the audience actually resolves: the organization's unified
// reply mailbox.
func (c *Core) QueryOrgPoolAllocations(poolID int, organizationID int64, platformAdmin bool) ([]models.OrgPoolAllocation, error) {
	if err := c.ensurePool(poolID); err != nil {
		return nil, err
	}
	q := `SELECT s.id,s.list_id,COALESCE(l.name,'') AS list_name,s.pool_id,s.organization_id,COALESCE(o.name,'') AS organization_name,o.reply_mailbox_id,COALESCE(r.email,'') AS reply_mailbox_email FROM org_pool_allocations s JOIN customer_lists l ON l.id=s.list_id JOIN organizations o ON o.id=s.organization_id LEFT JOIN reply_mailboxes r ON r.id=o.reply_mailbox_id WHERE s.pool_id=$1`
	args := []any{poolID}
	if !platformAdmin {
		q += ` AND s.organization_id=$2`
		args = append(args, organizationID)
	}
	var out []models.OrgPoolAllocation
	if err := c.db.Select(&out, q, args...); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryPoolManagementTarget loads the target-organization state required to
// allocate a first-level public pool. It is intentionally independent of the
// caller's active workspace: the HTTP handler restricts it to highest
// administrators, who may allocate a pool to an organization without joining
// that organization.
func (c *Core) QueryPoolManagementTarget(poolID, organizationID int) (models.PoolManagementTarget, error) {
	var out models.PoolManagementTarget
	if err := c.ensurePool(poolID); err != nil {
		return out, err
	}
	if organizationID <= 0 {
		return out, echo.NewHTTPError(http.StatusBadRequest, "organization is required")
	}
	org, err := c.GetOrganization(organizationID)
	if err != nil {
		return out, err
	}
	if org.Status != models.OrganizationStatusActive {
		return out, echo.NewHTTPError(http.StatusConflict, "organization is archived")
	}
	out.OrganizationID = org.ID
	out.OrganizationName = org.Name

	var allocation models.OrgPoolAllocation
	err = c.db.Get(&allocation, `
		SELECT s.id,s.list_id,COALESCE(l.name,'') AS list_name,s.pool_id,s.organization_id,
			COALESCE(o.name,'') AS organization_name,o.reply_mailbox_id,
			COALESCE(r.email,'') AS reply_mailbox_email
		FROM org_pool_allocations s
		JOIN customer_lists l ON l.id=s.list_id
		JOIN organizations o ON o.id=s.organization_id
		LEFT JOIN reply_mailboxes r ON r.id=o.reply_mailbox_id
		WHERE s.pool_id=$1 AND s.organization_id=$2`, poolID, organizationID)
	if err == nil {
		out.Allocation = &allocation
	} else if err != sql.ErrNoRows {
		return out, err
	}

	return out, nil
}

func (c *Core) GrantPoolOrganization(poolID int, organizationID int64, userID int) error {
	if err := c.ensurePool(poolID); err != nil {
		return err
	}
	_, err := c.db.Exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id,granted_by_user_id) VALUES($1,$2,$3) ON CONFLICT(pool_id,organization_id) DO UPDATE SET granted_by_user_id=EXCLUDED.granted_by_user_id`, poolID, organizationID, userID)
	return err
}

func (c *Core) RevokePoolOrganization(poolID int, organizationID int64) error {
	_, err := c.db.Exec(`DELETE FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2`, poolID, organizationID)
	return err
}

func (c *Core) HasPoolOrganizationPermission(poolID int, organizationID int64) (bool, error) {
	var ok bool
	err := c.db.Get(&ok, `SELECT EXISTS(SELECT 1 FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2)`, poolID, organizationID)
	return ok, err
}

func (c *Core) GetPoolContactByUUID(rawUUID string) (models.PoolContact, error) {
	var p models.PoolContact
	if err := c.db.Get(&p, `SELECT id,uuid,customer_code,company_name,email,name,allocation_department,attribs,status,created_at,updated_at FROM pool_contacts WHERE uuid=$1::uuid`, rawUUID); err != nil {
		return p, err
	}
	return p, nil
}

func (c *Core) ensurePool(poolID int) error {
	var typ string
	if err := c.db.Get(&typ, `SELECT type::text FROM customer_lists WHERE id=$1`, poolID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "pool not found")
		}
		return err
	}
	if typ != models.CustomerListTypePool {
		return echo.NewHTTPError(http.StatusBadRequest, "customer list is not a public pool")
	}
	return nil
}

// QueryAuthorizedPoolLists returns the minimal list metadata that an
// organization may use as a campaign audience. Delivery authorization is
// deliberately independent from customer-list read/manage grants: callers can
// select a first-level pool without receiving contact details.
func (c *Core) QueryAuthorizedPoolLists(access models.WorkspaceAccess) ([]models.CustomerList, error) {
	if access.PlatformAdmin {
		var out []models.CustomerList
		err := c.db.Select(&out, `
			SELECT l.*, COALESCE(o.name, '') AS organization_name,
				COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
				CASE WHEN l.type='pool' THEN (
					SELECT COUNT(*) FROM pool_members pm WHERE pm.pool_id=l.id
				) ELSE (
					SELECT COUNT(*) FROM org_pool_allocation_members sm
					JOIN org_pool_allocations ps ON ps.id=sm.allocation_id
					WHERE ps.list_id=l.id AND sm.status='active'
				) END AS customer_count
			FROM customer_lists l
			LEFT JOIN organizations o ON o.id = l.organization_id
			LEFT JOIN users u ON u.id = COALESCE(l.owner_user_id, l.original_owner_user_id)
			WHERE l.type IN ('pool','org_pool_allocation') AND l.status='active'
			ORDER BY l.name, l.id`)
		for i := range out {
			out[i].PoolDeliveryAllowed = true
		}
		return out, err
	}
	if !access.IsOrganization() || access.OrganizationID <= 0 {
		return []models.CustomerList{}, nil
	}
	var out []models.CustomerList
	err := c.db.Select(&out, `
		SELECT l.*, COALESCE(o.name, '') AS organization_name,
			COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			CASE WHEN l.type='pool' THEN (
				SELECT COUNT(*) FROM org_pool_allocation_members sm
				JOIN org_pool_allocations ps ON ps.id=sm.allocation_id
				WHERE ps.pool_id=l.id AND ps.organization_id=$1 AND sm.status='active'
			) ELSE (
				SELECT COUNT(*) FROM org_pool_allocation_members sm
				JOIN org_pool_allocations ps ON ps.id=sm.allocation_id
				WHERE ps.list_id=l.id AND sm.status='active'
			) END AS customer_count
		FROM customer_lists l
		LEFT JOIN organizations o ON o.id = l.organization_id
		LEFT JOIN users u ON u.id = COALESCE(l.owner_user_id, l.original_owner_user_id)
		WHERE l.status='active' AND (
			(l.type='pool' AND (EXISTS (
				SELECT 1 FROM pool_organization_permissions p
				WHERE p.pool_id=l.id AND p.organization_id=$1
			) OR EXISTS (
				SELECT 1 FROM org_pool_allocations s
				WHERE s.pool_id=l.id AND s.organization_id=$1
			))) OR
			(l.type='org_pool_allocation' AND EXISTS (
				SELECT 1 FROM org_pool_allocations s
				WHERE s.list_id=l.id AND s.pool_id IS NOT NULL AND s.organization_id=$1
			))
		) ORDER BY l.name, l.id`, access.OrganizationID)
	for i := range out {
		out[i].PoolDeliveryAllowed = true
	}
	return out, err
}

// QueryPlatformPublicPoolLists returns the minimal metadata of every active
// first-level public pool to a caller that holds the platform-level
// public-pool send permission. Platform-level campaigns cover every active
// organization's allocation of the selected pool, so the audience selector
// must not depend on the caller's organization owning a pool delivery grant;
// only metadata is returned and contact details stay behind the separate
// pool-contact policy. The permission itself is re-checked on campaign
// create/update/start.
func (c *Core) QueryPlatformPublicPoolLists() ([]models.CustomerList, error) {
	var out []models.CustomerList
	err := c.db.Select(&out, `
		SELECT l.*, COALESCE(o.name, '') AS organization_name,
			COALESCE(u.username, '') AS owner_username, COALESCE(u.name, '') AS owner_name,
			(SELECT COUNT(*) FROM pool_members pm WHERE pm.pool_id = l.id) AS customer_count
		FROM customer_lists l
		LEFT JOIN organizations o ON o.id = l.organization_id
		LEFT JOIN users u ON u.id = COALESCE(l.owner_user_id, l.original_owner_user_id)
		WHERE l.type = $1 AND l.status = 'active'
		ORDER BY l.name, l.id`, models.CustomerListTypePool)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].PoolDeliveryAllowed = true
	}
	return out, nil
}

type poolListScope struct {
	PoolID         int
	AllocationID   *int64
	OrganizationID int64
}

func (c *Core) getPoolListScope(listID int) (poolListScope, error) {
	var row struct {
		PoolID         sql.NullInt64 `db:"pool_id"`
		AllocationID   sql.NullInt64 `db:"allocation_id"`
		OrganizationID sql.NullInt64 `db:"organization_id"`
	}
	if err := c.db.Get(&row, `
		SELECT CASE WHEN l.type='pool' THEN l.id ELSE ps.pool_id END AS pool_id,
			CASE WHEN l.type='org_pool_allocation' THEN ps.id END AS allocation_id,
			ps.organization_id
		FROM customer_lists l
		LEFT JOIN org_pool_allocations ps ON ps.list_id=l.id
		WHERE l.id=$1 AND l.type IN ('pool','org_pool_allocation')`, listID); err != nil {
		if err == sql.ErrNoRows {
			return poolListScope{}, echo.NewHTTPError(http.StatusNotFound, "public pool list not found")
		}
		return poolListScope{}, err
	}
	if !row.PoolID.Valid {
		return poolListScope{}, echo.NewHTTPError(http.StatusBadRequest, "public pool list is not bound")
	}
	scope := poolListScope{PoolID: int(row.PoolID.Int64)}
	if row.AllocationID.Valid {
		allocationID := row.AllocationID.Int64
		scope.AllocationID = &allocationID
	}
	if row.OrganizationID.Valid {
		scope.OrganizationID = row.OrganizationID.Int64
	}
	return scope, nil
}

// PoolListCustomerCount returns the number of rows represented by a first-level
// pool or one of its pool allocations. Pool contacts intentionally remain outside
// the legacy customer membership tables, so their counts need a dedicated query.
func (c *Core) PoolListCustomerCount(listID int) (int, error) {
	scope, err := c.getPoolListScope(listID)
	if err != nil {
		return 0, err
	}
	var count int
	if scope.AllocationID == nil {
		err = c.db.Get(&count, `SELECT COUNT(*) FROM pool_members WHERE pool_id=$1`, scope.PoolID)
	} else {
		err = c.db.Get(&count, `SELECT COUNT(*) FROM org_pool_allocation_members WHERE allocation_id=$1 AND status='active'`, *scope.AllocationID)
	}
	return count, err
}

// QueryPoolContacts returns complete records only for platform administrators.
// All other callers receive a DTO containing a masked email.
func (c *Core) QueryPoolContacts(poolID int, organizationID int, platformAdmin bool, customerCode string) (any, error) {
	scope, err := c.getPoolListScope(poolID)
	if err != nil {
		return nil, err
	}
	args := []any{scope.PoolID}
	where := ""
	join := ""
	if scope.AllocationID != nil {
		args = append(args, *scope.AllocationID)
		join = ` JOIN org_pool_allocation_members psm ON psm.contact_id=pc.id AND psm.allocation_id=$2`
		where = ` AND psm.status IN ('active','removed')`
	}
	if !platformAdmin {
		if organizationID <= 0 {
			return []models.SafePoolContact{}, nil
		}
		if scope.AllocationID != nil {
			if scope.OrganizationID != int64(organizationID) {
				return []models.SafePoolContact{}, nil
			}
			args = append(args, organizationID)
			join += ` LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=pm.pool_id AND ex.organization_id=$3 AND ex.contact_id=pc.id AND ex.restored_at IS NULL`
			where += ` AND (psm.status IN ('active','removed') OR ex.contact_id IS NOT NULL)`
		} else {
			args = append(args, organizationID)
			join = ` JOIN org_pool_allocation_members psm ON psm.contact_id=pc.id JOIN org_pool_allocations ps ON ps.id=psm.allocation_id AND ps.pool_id=pm.pool_id LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=pm.pool_id AND ex.organization_id=ps.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL`
			where = ` AND ps.organization_id=$2 AND (psm.status IN ('active','removed') OR ex.contact_id IS NOT NULL)`
		}
	}
	if strings.TrimSpace(customerCode) != "" {
		args = append(args, "%"+strings.TrimSpace(customerCode)+"%")
		placeholder := len(args)
		where += " AND pc.customer_code ILIKE $" + strconv.Itoa(placeholder)
	}
	q := `SELECT pc.id, pc.uuid, pc.customer_code, pc.company_name, pc.email, pc.name, pc.allocation_department, pc.status
		FROM pool_contacts pc JOIN pool_members pm ON pm.contact_id=pc.id` + join + `
		WHERE pm.pool_id=$1` + where + ` ORDER BY pc.id`
	if !platformAdmin {
		q = `SELECT DISTINCT ON (pc.id) pc.id, pc.uuid, pc.customer_code, pc.company_name, pc.email, pc.name, pc.allocation_department, pc.status,
			(psm.status='removed' OR ex.contact_id IS NOT NULL) AS excluded, COALESCE(NULLIF(psm.removed_reason,''), ex.reason, '') AS exclusion_reason
			FROM pool_contacts pc JOIN pool_members pm ON pm.contact_id=pc.id` + join + `
			WHERE pm.pool_id=$1` + where + ` ORDER BY pc.id, psm.updated_at DESC`
		var rows []struct {
			models.PoolContact
			Excluded        bool   `db:"excluded"`
			ExclusionReason string `db:"exclusion_reason"`
		}
		if err := c.db.Select(&rows, q, args...); err != nil {
			return nil, err
		}
		out := make([]models.SafePoolContact, 0, len(rows))
		for _, row := range rows {
			s := row.PoolContact.Safe()
			s.Excluded = row.Excluded
			s.ExclusionReason = row.ExclusionReason
			out = append(out, s)
		}
		return out, nil
	}
	var rows []models.PoolContact
	if err := c.db.Select(&rows, q, args...); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		type exclusionRow struct {
			ContactID        int64  `db:"contact_id"`
			OrganizationID   int64  `db:"organization_id"`
			OrganizationName string `db:"organization_name"`
			Reason           string `db:"reason"`
			Source           string `db:"source"`
		}
		var exclusions []exclusionRow
		if err := c.db.Select(&exclusions, `SELECT e.contact_id,e.organization_id,COALESCE(o.name,'') AS organization_name,e.reason,e.source FROM org_pool_allocation_exclusions e LEFT JOIN organizations o ON o.id=e.organization_id WHERE e.pool_id=$1 AND e.restored_at IS NULL ORDER BY e.contact_id,e.organization_id`, poolID); err != nil {
			return nil, err
		}
		byContact := make(map[int64][]models.PoolExclusionSummary)
		for _, e := range exclusions {
			byContact[e.ContactID] = append(byContact[e.ContactID], models.PoolExclusionSummary{OrganizationID: e.OrganizationID, OrganizationName: e.OrganizationName, Reason: e.Reason, Source: e.Source})
		}
		for i := range rows {
			rows[i].Exclusions = byContact[rows[i].ID]
		}
	}
	return rows, nil
}

func (c *Core) CreatePoolContact(poolID int, p models.PoolContact) (models.PoolContact, error) {
	if poolID > 0 {
		if err := c.ensurePool(poolID); err != nil {
			return models.PoolContact{}, err
		}
	}
	p.CustomerCode = strings.TrimSpace(p.CustomerCode)
	p.CompanyName = strings.TrimSpace(p.CompanyName)
	p.Email = strings.TrimSpace(p.Email)
	p.Name = strings.TrimSpace(p.Name)
	p.AllocationDepartment = strings.TrimSpace(p.AllocationDepartment)
	if p.Email == "" {
		return models.PoolContact{}, echo.NewHTTPError(http.StatusBadRequest, "email is required")
	}
	tx, err := c.db.Beginx()
	if err != nil {
		return models.PoolContact{}, err
	}
	defer tx.Rollback()
	if p.AllocationDepartment != "" {
		var exists bool
		if err = tx.Get(&exists, `SELECT EXISTS(
			SELECT 1 FROM organizations WHERE status=$1 AND LOWER(name)=LOWER($2)
		)`, models.OrganizationStatusActive, p.AllocationDepartment); err != nil {
			return models.PoolContact{}, err
		}
		if !exists {
			return models.PoolContact{}, echo.NewHTTPError(http.StatusBadRequest, "allocation department does not exist")
		}
	}
	var id int64
	if err = tx.Get(&id, `INSERT INTO pool_contacts(customer_code,company_name,email,name,allocation_department,attribs) VALUES($1,$2,$3,$4,$5,$6::jsonb) RETURNING id`, p.CustomerCode, p.CompanyName, p.Email, p.Name, p.AllocationDepartment, `{}`); err != nil {
		return models.PoolContact{}, err
	}
	if poolID > 0 {
		if _, err = tx.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, poolID, id); err != nil {
			return models.PoolContact{}, err
		}
		if p.AllocationDepartment != "" {
			var organizationID int64
			if err = tx.Get(&organizationID, `SELECT id FROM organizations WHERE status=$1 AND LOWER(name)=LOWER($2)`, models.OrganizationStatusActive, p.AllocationDepartment); err != nil {
				return models.PoolContact{}, err
			}
			if _, err = tx.Exec(`
				INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status)
				SELECT ps.id,$2,'active'
				FROM org_pool_allocations ps
				WHERE ps.pool_id=$1 AND ps.organization_id=$3
				ON CONFLICT (allocation_id,contact_id) DO NOTHING`, poolID, id, organizationID); err != nil {
				return models.PoolContact{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return models.PoolContact{}, err
	}
	p.ID = id
	_ = c.db.Get(&p.UUID, `SELECT uuid FROM pool_contacts WHERE id=$1`, id)
	p.Status = "active"
	return p, nil
}

// ImportPoolContacts imports the four business fields used by the unified
// public-pool import. Additional source columns are intentionally ignored by
// the HTTP parser. A contact is reused when all imported identity fields
// match; same-code differences are retained as separate contacts and written
// to the pool conflict audit table, matching the existing pool merge policy.
func (c *Core) ImportPoolContacts(poolID, userID int, rows []models.PoolContactImportRow) (models.PoolContactImportResult, error) {
	result := models.PoolContactImportResult{Target: models.CustomerListTypePool, PoolID: poolID, Total: len(rows)}
	if poolID <= 0 {
		return result, echo.NewHTTPError(http.StatusBadRequest, "pool_id is required")
	}
	if err := c.ensurePool(poolID); err != nil {
		return result, err
	}

	tx, err := c.db.Beginx()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	var departments []struct {
		ID   int64  `db:"id"`
		Name string `db:"name"`
	}
	if err := tx.Select(&departments, `SELECT id,name FROM organizations WHERE status=$1`, models.OrganizationStatusActive); err != nil {
		return result, err
	}
	validDepartments := make(map[string]struct{}, len(departments))
	departmentOrganizations := make(map[string]int64, len(departments))
	for _, department := range departments {
		key := normalizePoolAllocationDepartment(department.Name)
		validDepartments[key] = struct{}{}
		departmentOrganizations[key] = department.ID
	}

	seen := make(map[string]struct{}, len(rows))
	addIssue := func(issue models.PoolContactImportIssue) {
		if len(result.Issues) < 200 {
			result.Issues = append(result.Issues, issue)
		}
	}
	for _, row := range rows {
		code := strings.TrimSpace(row.CustomerCode)
		name := strings.TrimSpace(row.Name)
		email := strings.TrimSpace(row.Email)
		department := strings.TrimSpace(row.AllocationDepartment)
		issue := models.PoolContactImportIssue{Row: row.Row, CustomerCode: code, AllocationDepartment: department}
		switch {
		case code == "":
			result.Invalid++
			issue.Reason = "customer_code_required"
			addIssue(issue)
			continue
		case name == "":
			result.Invalid++
			issue.Reason = "name_required"
			addIssue(issue)
			continue
		case email == "":
			result.Invalid++
			issue.Reason = "email_required"
			addIssue(issue)
			continue
		case department == "":
			result.Invalid++
			issue.Reason = "allocation_department_required"
			addIssue(issue)
			continue
		}
		if _, ok := validDepartments[normalizePoolAllocationDepartment(department)]; !ok {
			result.Invalid++
			issue.Reason = "allocation_department_not_found"
			addIssue(issue)
			continue
		}
		parsed, parseErr := mail.ParseAddress(email)
		if parseErr != nil || !strings.EqualFold(parsed.Address, email) || !strings.Contains(parsed.Address, "@") {
			result.Invalid++
			issue.Reason = "invalid_email"
			addIssue(issue)
			continue
		}

		key := code + "\x00" + strings.ToLower(email) + "\x00" + name + "\x00" + department
		if _, ok := seen[key]; ok {
			result.Duplicates++
			continue
		}
		seen[key] = struct{}{}
		result.Valid++

		var contactID int64
		err = tx.Get(&contactID, `SELECT id FROM pool_contacts
			WHERE customer_code=$1 AND LOWER(email)=LOWER($2) AND name=$3 AND allocation_department=$4
			ORDER BY id LIMIT 1`, code, email, name, department)
		if err == nil {
			if _, err = tx.Exec(`UPDATE pool_contacts SET status='active',updated_at=NOW() WHERE id=$1`, contactID); err != nil {
				return result, err
			}
			result.Existing++
		} else if err != sql.ErrNoRows {
			return result, err
		} else {
			var existingID int64
			codeErr := tx.Get(&existingID, `SELECT id FROM pool_contacts WHERE customer_code=$1 ORDER BY id LIMIT 1`, code)
			hasConflict := codeErr == nil
			if codeErr != nil && codeErr != sql.ErrNoRows {
				return result, codeErr
			}
			if err = tx.Get(&contactID, `INSERT INTO pool_contacts(customer_code,company_name,email,name,allocation_department,attribs)
				VALUES($1,'',$2,$3,$4,'{}'::jsonb) RETURNING id`, code, email, name, department); err != nil {
				return result, err
			}
			result.Created++
			if hasConflict {
				if _, err = tx.Exec(`INSERT INTO pool_merge_conflicts(pool_id,contact_id,customer_code,existing_snapshot,incoming_snapshot,created_by_user_id)
					VALUES($1,$2,$3,
						(SELECT jsonb_build_object('email',email,'name',name,'allocation_department',allocation_department) FROM pool_contacts WHERE id=$2),
						jsonb_build_object('email',$4::text,'name',$5::text,'allocation_department',$6::text),$7)`,
					poolID, existingID, code, email, name, department, userID); err != nil {
					return result, err
				}
				result.Conflicts++
			}
		}

		if _, err = tx.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, poolID, contactID); err != nil {
			return result, err
		}
		// The imported department is the allocation target. If that
		// organization already has a pool allocation for this pool, make the
		// contact a member of it immediately. Keep an existing removed row
		// untouched so a deliberate organization-level exclusion is preserved.
		if organizationID, ok := departmentOrganizations[normalizePoolAllocationDepartment(department)]; ok {
			if _, err = tx.Exec(`
				INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status)
				SELECT ps.id,$2,'active'
				FROM org_pool_allocations ps
				WHERE ps.pool_id=$1 AND ps.organization_id=$3
				ON CONFLICT (allocation_id,contact_id) DO NOTHING`, poolID, contactID, organizationID); err != nil {
				return result, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// CreateOrgPoolAllocation splits a first-level pool into one organization-owned
// pool allocation. The list, delivery grant and allocation are created in one
// transaction, so a standalone pool allocation can never be staged for a
// later binding.
//
// replyMailboxID is retained only for callers that predate the organization's
// unified reply mailbox. An allocation no longer carries a mailbox of its own,
// so the parameter is deliberately ignored; every public-pool audience resolves
// the organization's unified reply mailbox instead.
func (c *Core) CreateOrgPoolAllocation(poolID int, organizationID int64, name string, replyMailboxID *int, userID int, platformAdmin bool) (models.OrgPoolAllocation, error) {
	if poolID <= 0 || organizationID <= 0 || strings.TrimSpace(name) == "" {
		return models.OrgPoolAllocation{}, echo.NewHTTPError(http.StatusBadRequest, "pool, organization and pool allocation name are required")
	}

	uuidValue, err := uuid.NewV4()
	if err != nil {
		return models.OrgPoolAllocation{}, err
	}
	target := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: int(organizationID), PlatformAdmin: platformAdmin},
		UserID:    userID,
	}
	scope := ApplyWorkspaceScope(target, models.ResourceVisibilityOrganization)
	var out models.OrgPoolAllocation
	err = c.withWorkspaceCreation(target, func(tx *sqlx.Tx) error {
		var poolType string
		if err := tx.Get(&poolType, `SELECT type::text FROM customer_lists WHERE id=$1 FOR UPDATE`, poolID); err != nil {
			if err == sql.ErrNoRows {
				return echo.NewHTTPError(http.StatusNotFound, "pool not found")
			}
			return err
		}
		if poolType != models.CustomerListTypePool {
			return echo.NewHTTPError(http.StatusBadRequest, "customer list is not a public pool")
		}
		var exists bool
		if err := tx.Get(&exists, `SELECT EXISTS(SELECT 1 FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2)`, poolID, organizationID); err != nil {
			return err
		}
		if exists {
			return echo.NewHTTPError(http.StatusConflict, "organization already has a pool allocation for this public pool")
		}

		var listID int
		if err := tx.Stmtx(c.q.CreateList).Get(&listID,
			uuidValue.String(), strings.TrimSpace(name), models.CustomerListTypeOrgPoolAllocation,
			models.CustomerListOptinSingle, models.CustomerListStatusActive, pq.StringArray{}, "", true,
			scope.OrganizationID, scope.OwnerUserID, scope.OriginalOwnerUserID, scope.Visibility); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE customer_lists SET pool_parent_id=$1 WHERE id=$2`, poolID, listID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id,granted_by_user_id) VALUES($1,$2,$3) ON CONFLICT(pool_id,organization_id) DO UPDATE SET granted_by_user_id=EXCLUDED.granted_by_user_id`, poolID, organizationID, userID); err != nil {
			return err
		}
		if err := tx.Get(&out, `INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id)
			VALUES($1,$2,$3,$4)
			RETURNING id,list_id,(SELECT name FROM customer_lists WHERE id=org_pool_allocations.list_id) AS list_name,
				pool_id,organization_id,(SELECT name FROM organizations WHERE id=org_pool_allocations.organization_id) AS organization_name,
				(SELECT o.reply_mailbox_id FROM organizations o WHERE o.id=org_pool_allocations.organization_id) AS reply_mailbox_id`,
			listID, poolID, organizationID, userID); err != nil {
			return err
		}
		// Existing contacts imported before this pool allocation was created
		// must be allocated from their validated department as well. This keeps
		// the import order independent: create the allocation first or import the
		// pool first, the resulting membership is the same.
		if _, err := tx.Exec(`
			INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status)
			SELECT $1,pm.contact_id,'active'
			FROM pool_members pm
			JOIN pool_contacts pc ON pc.id=pm.contact_id
			JOIN organizations o ON o.id=$2 AND o.status=$4
			WHERE pm.pool_id=$3 AND LOWER(TRIM(pc.allocation_department))=LOWER(TRIM(o.name))
			ON CONFLICT (allocation_id,contact_id) DO NOTHING`, out.ID, organizationID, poolID, models.OrganizationStatusActive); err != nil {
			return err
		}
		// The allocation has no mailbox of its own. Report the organization's
		// unified reply mailbox so the create response matches the allocation
		// listing; the audience resolves through that mailbox.
		return tx.Get(&out.ReplyMailboxEmail, `SELECT COALESCE(rm.email,'') FROM organizations o LEFT JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id WHERE o.id=$1`, organizationID)
	})
	return out, err
}

func (c *Core) AssignPoolContact(allocationID, contactID int64) error {
	res, err := c.db.Exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) SELECT $1,$2 WHERE EXISTS (SELECT 1 FROM org_pool_allocations s JOIN pool_members m ON m.pool_id=s.pool_id AND m.contact_id=$2 WHERE s.id=$1) ON CONFLICT(allocation_id,contact_id) DO UPDATE SET status='active',removed_at=NULL,removed_reason=''`, allocationID, contactID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "contact is not a member of the selected pool")
	}
	return nil
}

// ImportOrgPoolAllocationMembers resolves a batch of customer_code/email pairs
// against the selected first-level pool and upserts the matching contacts into
// its organization allocation. Matching is intentionally performed on both
// normalized fields because customer codes are imported and are not unique.
// The operation is one transaction, while row-level mismatches are reported
// in the result so a large file can be corrected without retrying successes.
func (c *Core) ImportOrgPoolAllocationMembers(allocationID int64, rows []models.PoolImportRow) (models.PoolImportResult, error) {
	var result models.PoolImportResult
	result.Total = len(rows)
	if allocationID <= 0 {
		return result, echo.NewHTTPError(http.StatusBadRequest, "allocation_id is required")
	}

	tx, err := c.db.Beginx()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var poolID int
	if err = tx.Get(&poolID, `SELECT s.pool_id FROM org_pool_allocations s JOIN customer_lists l ON l.id=s.pool_id WHERE s.id=$1 AND l.type='pool'`, allocationID); err != nil {
		if err == sql.ErrNoRows {
			return result, echo.NewHTTPError(http.StatusNotFound, "pool allocation not found")
		}
		return result, err
	}

	matches := make(map[string][]int64)
	var contacts []struct {
		ID           int64  `db:"id"`
		CustomerCode string `db:"customer_code"`
		Email        string `db:"email"`
	}
	if err = tx.Select(&contacts, `SELECT pc.id,pc.customer_code,pc.email
		FROM pool_contacts pc JOIN pool_members pm ON pm.contact_id=pc.id
		WHERE pm.pool_id=$1 AND pc.status='active'`, poolID); err != nil {
		return result, err
	}
	for _, contact := range contacts {
		key := strings.TrimSpace(contact.CustomerCode) + "\x00" + strings.ToLower(strings.TrimSpace(contact.Email))
		matches[key] = append(matches[key], contact.ID)
	}

	var existing []struct {
		ContactID int64  `db:"contact_id"`
		Status    string `db:"status"`
	}
	if err = tx.Select(&existing, `SELECT contact_id,status FROM org_pool_allocation_members WHERE allocation_id=$1`, allocationID); err != nil {
		return result, err
	}
	statusByContact := make(map[int64]string, len(existing))
	for _, member := range existing {
		statusByContact[member.ContactID] = member.Status
	}
	addIssue := func(issue models.PoolImportIssue) {
		// Keep the response bounded for a malformed 100k-row file while still
		// returning complete counters for the operator's audit.
		if len(result.Issues) < 200 {
			result.Issues = append(result.Issues, issue)
		}
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		code := strings.TrimSpace(row.CustomerCode)
		email := strings.ToLower(strings.TrimSpace(row.Email))
		if code == "" || email == "" {
			result.Invalid++
			addIssue(models.PoolImportIssue{Row: row.Row, CustomerCode: code, Reason: "invalid"})
			continue
		}
		key := code + "\x00" + email
		if _, ok := seen[key]; ok {
			result.Duplicates++
			continue
		}
		seen[key] = struct{}{}
		ids := matches[key]
		if len(ids) == 0 {
			result.Unmatched++
			addIssue(models.PoolImportIssue{Row: row.Row, CustomerCode: code, Reason: "not_found"})
			continue
		}
		if len(ids) > 1 {
			result.Ambiguous++
			addIssue(models.PoolImportIssue{Row: row.Row, CustomerCode: code, Reason: "ambiguous"})
			continue
		}
		result.Valid++
		contactID := ids[0]
		switch statusByContact[contactID] {
		case "active":
			result.AlreadyAssigned++
		case "removed":
			if _, err = tx.Exec(`UPDATE org_pool_allocation_members SET status='active',removed_at=NULL,removed_reason='',removed_by_user_id=NULL,updated_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, allocationID, contactID); err != nil {
				return result, err
			}
			if _, err = tx.Exec(`UPDATE org_pool_allocation_exclusions ex SET restored_at=NOW() FROM org_pool_allocations s WHERE ex.allocation_id=s.id AND s.id=$1 AND ex.contact_id=$2 AND ex.restored_at IS NULL`, allocationID, contactID); err != nil {
				return result, err
			}
			result.Reactivated++
		default:
			if _, err = tx.Exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status) VALUES($1,$2,'active') ON CONFLICT(allocation_id,contact_id) DO UPDATE SET status='active',removed_at=NULL,removed_reason='',updated_at=NOW()`, allocationID, contactID); err != nil {
				return result, err
			}
			result.Created++
		}
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// RemovePoolContact performs a logical organization-scoped exclusion. The pool
// member remains available for other organizations and for audit by admins.
func (c *Core) RemovePoolContact(allocationID, contactID int64, userID int, reason string) error {
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var poolID sql.NullInt64
	var orgID int64
	if err = tx.QueryRow(`SELECT pool_id,organization_id FROM org_pool_allocations WHERE id=$1`, allocationID).Scan(&poolID, &orgID); err != nil {
		return err
	}
	var memberExists bool
	if err = tx.Get(&memberExists, `SELECT EXISTS(SELECT 1 FROM org_pool_allocation_members WHERE allocation_id=$1 AND contact_id=$2)`, allocationID, contactID); err != nil {
		return err
	}
	if !memberExists {
		return echo.NewHTTPError(http.StatusBadRequest, "contact is not assigned to the selected pool allocation")
	}
	if _, err = tx.Exec(`UPDATE org_pool_allocation_members SET status='removed',removed_reason=$3,removed_at=NOW(),removed_by_user_id=$4,updated_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, allocationID, contactID, reason, userID); err != nil {
		return err
	}
	if poolID.Valid {
		if _, err = tx.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,removed_by_user_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE SET allocation_id=EXCLUDED.allocation_id,reason=EXCLUDED.reason,source='allocation',removed_by_user_id=EXCLUDED.removed_by_user_id,removed_at=NOW(),restored_at=NULL`, poolID.Int64, orgID, contactID, allocationID, reason, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *Core) RestorePoolContact(allocationID, contactID int64) error {
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var memberExists bool
	if err = tx.Get(&memberExists, `SELECT EXISTS(SELECT 1 FROM org_pool_allocation_members WHERE allocation_id=$1 AND contact_id=$2)`, allocationID, contactID); err != nil {
		return err
	}
	if !memberExists {
		return echo.NewHTTPError(http.StatusBadRequest, "contact is not assigned to the selected pool allocation")
	}
	if _, err = tx.Exec(`UPDATE org_pool_allocation_members SET status='active',removed_reason='',removed_at=NULL,updated_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, allocationID, contactID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE org_pool_allocation_exclusions ex SET restored_at=NOW() FROM org_pool_allocations s WHERE ex.allocation_id=s.id AND s.id=$1 AND ex.contact_id=$2 AND ex.restored_at IS NULL`, allocationID, contactID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearPoolContactEmail removes an invalid address without deleting the pool
// contact row, preserving the imported code and auditability.
func (c *Core) ClearPoolContactEmail(poolID int, contactID int64, organizationID int64, platformAdmin bool) error {
	if err := c.ensurePool(poolID); err != nil {
		return err
	}
	guard := `EXISTS (SELECT 1 FROM pool_members WHERE pool_id=$2 AND contact_id=$1)`
	args := []any{contactID, poolID}
	if !platformAdmin {
		guard += ` AND EXISTS (SELECT 1 FROM org_pool_allocations s JOIN org_pool_allocation_members sm ON sm.allocation_id=s.id WHERE s.pool_id=$2 AND s.organization_id=$3 AND sm.contact_id=$1)`
		args = append(args, organizationID)
	}
	_, err := c.db.Exec(`UPDATE pool_contacts SET email='',status='archived',updated_at=NOW() WHERE id=$1 AND `+guard, args...)
	return err
}

// refreshPoolCampaignAudienceRoutes re-resolves the internal mailbox route of
// every pool audience on a campaign. A draft can be created before its
// organization's unified reply mailbox is configured or usable; in that case
// the relation keeps a NULL resolved mailbox. Re-reading the current
// organization mailbox configuration here makes an existing draft usable after
// the administrator finishes the configuration, without changing the
// first-level/pool-allocation audience selection stored in
// org_pool_allocation_id.
func (c *Core) refreshPoolCampaignAudienceRoutes(campaignID int) error {
	_, err := c.db.Exec(`
		UPDATE campaign_customer_lists ccl
		SET resolved_reply_mailbox_id = (
			SELECT CASE
				WHEN COUNT(DISTINCT s.id) = 1 AND COUNT(DISTINCT rm.id) = 1 THEN MAX(rm.id)
				ELSE NULL
			END
			FROM org_pool_allocations s
			JOIN organizations o ON o.id=s.organization_id
			LEFT JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id
				AND rm.status='active' AND rm.verified_at IS NOT NULL
			WHERE ccl.source_organization_id IS NOT NULL
				AND s.pool_id=ccl.pool_id
				AND s.organization_id=ccl.source_organization_id
				AND (ccl.org_pool_allocation_id IS NULL OR s.id=ccl.org_pool_allocation_id)
		)
		WHERE ccl.campaign_id=$1 AND ccl.pool_id IS NOT NULL`, campaignID)
	return err
}

// PoolAudienceRouteIssue is one unresolved public-pool audience row of one
// campaign. ValidatePoolCampaignAudience renders these rows so the administrator
// sees which configuration step is missing instead of the former opaque
// sentence. This struct only reports a problem; the resolution semantics are
// unchanged and a pool audience still never falls back to a per-allocation,
// personal or default mailbox: the only accepted route is the target
// organization's unified reply mailbox. A first-level pool campaign may be
// saved as a draft, but preview and send must be blocked while the target
// organization has no effective pool allocation or unified reply mailbox.
type PoolAudienceRouteIssue struct {
	PoolID             int    `db:"pool_id"`
	PoolName           string `db:"pool_name"`
	OrganizationID     *int64 `db:"organization_id"`
	OrganizationName   string `db:"organization_name"`
	AllocationID       *int64 `db:"allocation_id"`
	AllocationListID   *int64 `db:"allocation_list_id"`
	AllocationListName string `db:"allocation_list_name"`
	BoundMailboxID     *int   `db:"bound_mailbox_id"`
	BoundMailboxEmail  string `db:"bound_mailbox_email"`
	// Reason is the first failing condition. It is one of the
	// poolAudienceRouteReason* constants below, or the defensive fallback
	// "unresolved" when the row is unresolved although the current allocation and
	// mailbox look usable.
	Reason string `db:"reason"`
}

// Reason codes for PoolAudienceRouteIssue. The order mirrors the resolve: a
// missing organization hides the allocation state and a missing or invalid
// allocation hides the organization mailbox state, exactly as
// refreshPoolCampaignAudienceRoutes cannot resolve through them either.
const (
	poolAudienceRouteReasonOrganizationMissing = "organization_missing"
	poolAudienceRouteReasonAllocationMissing   = "allocation_missing"
	poolAudienceRouteReasonMailboxMissing      = "mailbox_missing"
	poolAudienceRouteReasonMailboxUnavailable  = "mailbox_unavailable"
)

// poolAudienceRouteMessageLimit caps how many audience clauses the block
// message spells out; the rest is summarized as "(+N more)".
const poolAudienceRouteMessageLimit = 5

// poolAudienceRouteMessagePrefix is the legacy prefix existing callers match on.
const poolAudienceRouteMessagePrefix = "public-pool audience requires an organization allocation and reply mailbox before previewing or sending"

// poolAudienceRouteMessageMailboxStep is the actionable step that fixes a
// missing or unusable organization reply mailbox. It names the organization
// workspace on purpose: the setting belongs to the organization, and a platform
// administrator cannot configure it on the organization's behalf.
const poolAudienceRouteMessageMailboxStep = "a manager of that organization opens its workspace and saves a verified mailbox in Manage organizations -> Organization reply mailboxes as the organization's unified reply mailbox"

// poolAudienceRouteMessageAllocationStep is prepended when an audience has no
// organization allocation yet: without the allocation there is no recipient
// set, so the mailbox step alone cannot fix the campaign.
const poolAudienceRouteMessageAllocationStep = "bind the organization's allocation for that pool in Customer lists -> Public pool management"

// poolAudienceRouteMessageRetry is the closing step of every rendered fix.
const poolAudienceRouteMessageRetry = "then retry preview or send."

// poolAudienceRouteIssues lists the audiences of one campaign that cannot be
// previewed or sent yet, with the first failing condition of each row. This is
// read-only diagnostics: the SQL mirrors refreshPoolCampaignAudienceRoutes, so
// it matches the same pool, organization and pinned-allocation triple, resolves
// the mailbox from the organization's unified reply mailbox, accepts it only
// under status='active' AND verified_at IS NOT NULL, and uses the exact
// unresolved predicate ValidatePoolCampaignAudience checks. No mailbox is
// substituted for a missing one; the row is only labelled.
func (c *Core) poolAudienceRouteIssues(campaignID int) ([]PoolAudienceRouteIssue, error) {
	var issues []PoolAudienceRouteIssue
	err := c.db.Select(&issues, `
		SELECT ccl.pool_id,
			ccl.customer_list_name AS pool_name,
			ccl.source_organization_id AS organization_id,
			COALESCE(o.name,'') AS organization_name,
			s.id AS allocation_id,
			s.list_id AS allocation_list_id,
			COALESCE(l.name,'') AS allocation_list_name,
			o.reply_mailbox_id AS bound_mailbox_id,
			COALESCE(rm.email,'') AS bound_mailbox_email,
			CASE
				-- Keep these literals in sync with the poolAudienceRouteReason* constants.
				WHEN ccl.source_organization_id IS NULL THEN 'organization_missing'
				WHEN s.id IS NULL THEN 'allocation_missing'
				WHEN o.reply_mailbox_id IS NULL THEN 'mailbox_missing'
				WHEN rm.status='active' AND rm.verified_at IS NOT NULL THEN 'unresolved'
				ELSE 'mailbox_unavailable'
			END AS reason
		FROM campaign_customer_lists ccl
		LEFT JOIN organizations o ON o.id=ccl.source_organization_id
		LEFT JOIN LATERAL (
			SELECT ss.id,ss.list_id
			FROM org_pool_allocations ss
			WHERE ccl.source_organization_id IS NOT NULL
				AND ss.pool_id=ccl.pool_id
				AND ss.organization_id=ccl.source_organization_id
				AND (ccl.org_pool_allocation_id IS NULL OR ss.id=ccl.org_pool_allocation_id)
			ORDER BY ss.id
			LIMIT 1
		) s ON TRUE
		LEFT JOIN customer_lists l ON l.id=s.list_id
		LEFT JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id
		WHERE ccl.campaign_id=$1
			AND ccl.pool_id IS NOT NULL
			AND (ccl.source_organization_id IS NULL OR ccl.resolved_reply_mailbox_id IS NULL)
		ORDER BY ccl.pool_id,s.id NULLS FIRST`, campaignID)
	if err != nil {
		return nil, err
	}
	return issues, nil
}

// poolAudienceRouteAllocationLabel names the organization allocation of one
// issue, falling back to its internal ID when the allocation list has no name
// or the organization has no allocation at all.
func poolAudienceRouteAllocationLabel(issue PoolAudienceRouteIssue) string {
	if issue.AllocationListName != "" {
		return fmt.Sprintf("%q", issue.AllocationListName)
	}
	if issue.AllocationID != nil {
		return fmt.Sprintf("#%d", *issue.AllocationID)
	}
	return "unbound"
}

// poolAudienceRouteIssueClause renders one issue as a single-line clause naming
// the whole chain the operator has to fix: the first-level pool list, the
// organization allocation under it and the organization that owns the missing
// reply mailbox. The bound mailbox email is only named when the row carries it;
// without it the clause still states the reason.
func poolAudienceRouteIssueClause(issue PoolAudienceRouteIssue) string {
	allocation := poolAudienceRouteAllocationLabel(issue)
	switch issue.Reason {
	case poolAudienceRouteReasonOrganizationMissing:
		return fmt.Sprintf("pool list %q has no target organization, so no reply mailbox can be resolved", issue.PoolName)
	case poolAudienceRouteReasonAllocationMissing:
		return fmt.Sprintf("pool list %q: organization %q has no organization allocation bound to the pool", issue.PoolName, issue.OrganizationName)
	case poolAudienceRouteReasonMailboxMissing:
		return fmt.Sprintf("pool list %q -> organization allocation %s (organization %q): the organization has not configured its unified reply mailbox", issue.PoolName, allocation, issue.OrganizationName)
	case poolAudienceRouteReasonMailboxUnavailable:
		if issue.BoundMailboxEmail == "" {
			return fmt.Sprintf("pool list %q -> organization allocation %s (organization %q): the organization's unified reply mailbox is not verified and active", issue.PoolName, allocation, issue.OrganizationName)
		}
		return fmt.Sprintf("pool list %q -> organization allocation %s (organization %q): the organization's unified reply mailbox %q is not verified and active", issue.PoolName, allocation, issue.OrganizationName, issue.BoundMailboxEmail)
	default:
		// The "unresolved" fallback and any unknown code state the symptom
		// without naming a missing piece.
		if issue.OrganizationName == "" {
			return fmt.Sprintf("pool list %q has an unresolved audience route", issue.PoolName)
		}
		return fmt.Sprintf("pool list %q -> organization allocation %s (organization %q): the audience route is unresolved", issue.PoolName, allocation, issue.OrganizationName)
	}
}

// poolAudienceRouteMessage renders the block message as one line: the legacy
// prefix, one clause per unresolved audience (at most
// poolAudienceRouteMessageLimit, then "(+N more)"), and the fix hint. It only
// reports the configuration gap; it cannot resolve it and does not fall back to
// any other mailbox. It never contains a newline.
func poolAudienceRouteMessage(issues []PoolAudienceRouteIssue) string {
	total := len(issues)
	missingAllocation := false
	for _, issue := range issues {
		if issue.Reason == poolAudienceRouteReasonAllocationMissing {
			missingAllocation = true
			break
		}
	}
	if total > poolAudienceRouteMessageLimit {
		issues = issues[:poolAudienceRouteMessageLimit]
	}
	clauses := make([]string, 0, len(issues))
	for _, issue := range issues {
		clauses = append(clauses, poolAudienceRouteIssueClause(issue))
	}
	msg := poolAudienceRouteMessagePrefix + ": " + strings.Join(clauses, "; ")
	if total > poolAudienceRouteMessageLimit {
		msg += fmt.Sprintf(" (+%d more)", total-poolAudienceRouteMessageLimit)
	}
	steps := make([]string, 0, 2)
	if missingAllocation {
		steps = append(steps, poolAudienceRouteMessageAllocationStep)
	}
	steps = append(steps, poolAudienceRouteMessageMailboxStep)
	return msg + ". Fix: " + strings.Join(steps, "; ") + "; " + poolAudienceRouteMessageRetry
}

// ValidatePoolCampaignAudience is called by preview/send paths. Drafts may
// retain an unresolved pool audience, but sending is blocked until every pool
// row resolves to an organization allocation and the organization's unified
// reply mailbox. The block message names each unresolved audience and the
// concrete missing piece; it never substitutes a per-allocation, personal or
// default mailbox for a missing organization mailbox.
//
// Platform-level ('all_organizations') campaigns are validated across every
// active organization that owns a pool allocation for the campaign's pool:
// each organization needs a usable unified reply mailbox and at least one
// enabled SMTP account belonging to an enabled active member. A single
// unready organization blocks the whole campaign.
func (c *Core) ValidatePoolCampaignAudience(campaignID int) error {
	var poolScope string
	if err := c.db.Get(&poolScope, `SELECT pool_scope FROM campaigns WHERE id=$1`, campaignID); err != nil {
		return err
	}
	if err := c.refreshPoolCampaignAudienceRoutes(campaignID); err != nil {
		return err
	}
	if poolScope == models.CampaignPoolScopeAllOrganizations {
		return c.validateAllOrgPoolCampaignAudience(campaignID)
	}
	issues, err := c.poolAudienceRouteIssues(campaignID)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, poolAudienceRouteMessage(issues))
	}
	return nil
}

// poolOrgStatusRow is one row of the all-organization readiness read.
type poolOrgStatusRow struct {
	OrganizationID   int64  `db:"organization_id"`
	OrganizationName string `db:"organization_name"`
	OrgStatus        string `db:"organization_status"`
	MailboxReady     bool   `db:"mailbox_ready"`
	MailboxEmail     string `db:"reply_mailbox_email"`
	SMTPCount        int    `db:"smtp_count"`
}

const poolAllOrgMessageSMTPStep = "each target organization needs at least one enabled SMTP account belonging to an enabled active member (members add one in Profile -> SMTP)"

// validateAllOrgPoolCampaignAudience blocks preview/send for a platform-level
// campaign until every active organization with a pool allocation for the
// campaign's pool has a usable unified reply mailbox and at least one
// eligible SMTP account. The message names each failing organization so an
// operator can fix them one by one.
func (c *Core) validateAllOrgPoolCampaignAudience(campaignID int) error {
	var rows []poolOrgStatusRow
	if err := c.db.Select(&rows, `SELECT s.organization_id,
			o.name AS organization_name,
			o.status AS organization_status,
			(rm.id IS NOT NULL AND rm.status = 'active' AND rm.verified_at IS NOT NULL) AS mailbox_ready,
			COALESCE(rm.email, '') AS reply_mailbox_email,
			(
				SELECT COUNT(*)
				FROM user_smtp_servers s2
				JOIN organization_members om2 ON om2.organization_id = s.organization_id
					AND om2.user_id = s2.user_id AND om2.removed_at IS NULL
				JOIN users u2 ON u2.id = s2.user_id AND u2.status = 'enabled'
				WHERE s2.enabled = TRUE
			) AS smtp_count
		FROM org_pool_allocations s
		JOIN organizations o ON o.id = s.organization_id
		LEFT JOIN reply_mailboxes rm ON rm.id = o.reply_mailbox_id
		WHERE s.pool_id = (
			SELECT ccl.pool_id
			FROM campaign_customer_lists ccl
			WHERE ccl.campaign_id = $1 AND ccl.pool_id IS NOT NULL
			LIMIT 1
		)
		ORDER BY s.organization_id`, campaignID); err != nil {
		return err
	}
	var poolName string
	if err := c.db.Get(&poolName, `SELECT COALESCE(MAX(ccl.customer_list_name), '')
		FROM campaign_customer_lists ccl WHERE ccl.campaign_id=$1 AND ccl.pool_id IS NOT NULL`, campaignID); err != nil {
		return err
	}
	if poolName == "" {
		var id int
		if err := c.db.Get(&id, `SELECT ccl.pool_id FROM campaign_customer_lists ccl WHERE ccl.campaign_id=$1 AND ccl.pool_id IS NOT NULL LIMIT 1`, campaignID); err != nil {
			return err
		}
		if err := c.db.Get(&poolName, `SELECT name FROM customer_lists WHERE id=$1`, id); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest,
			poolAudienceRouteMessagePrefix+": "+fmt.Sprintf("pool list %q has no active organization pool allocation to send to", poolName)+
				". Fix: "+poolAudienceRouteMessageAllocationStep+"; "+poolAudienceRouteMessageRetry)
	}

	clauses := make([]string, 0, len(rows))
	total := 0
	for _, row := range rows {
		if row.OrgStatus != "active" {
			clauses = append(clauses, fmt.Sprintf("pool list %q -> organization %q: the organization is archived", poolName, row.OrganizationName))
			total++
			continue
		}
		if !row.MailboxReady {
			if row.MailboxEmail != "" {
				clauses = append(clauses, fmt.Sprintf("pool list %q -> organization %q: the organization's unified reply mailbox %q is not verified and active", poolName, row.OrganizationName, row.MailboxEmail))
			} else {
				clauses = append(clauses, fmt.Sprintf("pool list %q -> organization %q: the organization has not configured its unified reply mailbox", poolName, row.OrganizationName))
			}
			total++
			continue
		}
		if row.SMTPCount == 0 {
			clauses = append(clauses, fmt.Sprintf("pool list %q -> organization %q: no enabled SMTP account is available for the organization's active members", poolName, row.OrganizationName))
			total++
		}
	}
	if total == 0 {
		return nil
	}
	if total > poolAudienceRouteMessageLimit {
		clauses = clauses[:poolAudienceRouteMessageLimit]
	}
	msg := poolAudienceRouteMessagePrefix + ": " + strings.Join(clauses, "; ")
	if total > poolAudienceRouteMessageLimit {
		msg += fmt.Sprintf(" (+%d more)", total-poolAudienceRouteMessageLimit)
	}
	return echo.NewHTTPError(http.StatusBadRequest, msg+". Fix: "+poolAudienceRouteMessageMailboxStep+"; "+poolAllOrgMessageSMTPStep+"; "+poolAudienceRouteMessageRetry)
}

// poolRecipientMembershipSQL is the single definition of a deliverable pool
// recipient: an active pool member whose organization allocation is still
// active, which that organization has not excluded, and whose pool contact row
// is still active. The first-level resolve, the pool-allocation resolve and the
// campaign snapshot writer all embed exactly this fragment, so a new exclusion
// source or contact status cannot be added to one audience path while another
// silently keeps sending to the contact.
//
// The fragment also joins the organization's single unified reply mailbox and
// exposes it as rm.id, and only while that mailbox is active and verified.
// Embedders read the per-recipient reply mailbox from rm.id; an organization
// allocation carries no mailbox of its own.
//
// Positional parameter contract for every embedder:
//
//	$1 = first-level pool ID
//	$2 = organization ID
//	$3 = org_pool_allocations ID restricting the read to one pool allocation, or NULL to
//	     accept every pool allocation the organization owns in that pool (the
//	     schema allows at most one per organization)
//
// An embedder that needs further parameters must number them from $4 upwards.
const poolRecipientMembershipSQL = `
	FROM org_pool_allocations s
	JOIN organizations o ON o.id=s.organization_id
	JOIN org_pool_allocation_members sm ON sm.allocation_id=s.id AND sm.status='active'
	JOIN pool_members pm ON pm.pool_id=s.pool_id AND pm.contact_id=sm.contact_id
	JOIN pool_contacts pc ON pc.id=sm.contact_id
	LEFT JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id AND rm.status='active' AND rm.verified_at IS NOT NULL
	LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=s.pool_id AND ex.organization_id=s.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL
	WHERE s.pool_id=$1 AND s.organization_id=$2 AND ($3::BIGINT IS NULL OR s.id=$3::BIGINT) AND ex.contact_id IS NULL AND pc.status='active'`

// poolRecipientSelectSQL reads the deliverable members of one pool audience. It
// is the read half of the shared rule and is deduplicated by the pool contact's
// stable internal ID, which is also the snapshot's primary key. The reply
// mailbox of every row is the organization's unified reply mailbox, exposed by
// the shared fragment as rm.id.
const poolRecipientSelectSQL = `SELECT DISTINCT ON (pc.id) pc.id,pc.customer_code,pc.company_name,pc.email,pc.name,pc.status,s.id AS allocation_id,s.organization_id,rm.id AS reply_mailbox_id` + poolRecipientMembershipSQL + ` ORDER BY pc.id,s.id`

// poolSnapshotRefreshStatuses lists the snapshot statuses a refresh owns: a row
// in one of these states has not been handed to delivery yet, so the refresh may
// still rewrite or remove it. Rows that are already queued for a worker, sent,
// or cancelled by an unsubscribe are delivery history and are never rewritten or
// deleted. The prune and the upsert share this one predicate so they can never
// disagree about which rows the refresh owns.
const poolSnapshotRefreshStatuses = `('pending','deferred')`

// poolRecipientSnapshotUpsertSQL writes the snapshot rows of one pool audience
// from the shared membership rule.
//
// This is DO UPDATE and not DO NOTHING, because both callers are refreshes and
// not first writes: EnsurePoolCampaignRecipients runs on every scheduler tick
// for a campaign that is already scheduled or running, and AttachPoolToCampaign
// re-runs whenever a draft audience is saved. DO NOTHING keeps the older email
// address, name and reply mailbox of a contact that was edited or re-resolved
// after the first snapshot was written, which is exactly the drift this path
// exists to prevent.
//
// The WHERE clause restricts the rewrite to the rows a refresh owns, so the
// snapshot of a row that was already queued or sent stays exactly what was
// handed to delivery. A contact that was excluded after the snapshot was written
// no longer matches the shared rule, so this statement does not write it again;
// its owned rows are removed by poolRecipientSnapshotPruneSQL and its
// already-delivered rows are kept as history.
//
// Embedder positions continue after the rule's parameters:
//
//	$4 = campaign ID
//	$5 = audience reply mailbox, or NULL to use the organization's unified
//	     reply mailbox of each resolved row (rm.id from the shared rule)
const poolRecipientSnapshotUpsertSQL = `INSERT INTO campaign_pool_recipients AS cpr(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,reply_mailbox_id,email_snapshot,name_snapshot)
	SELECT $4,pc.id,$1,s.id,$2,COALESCE($5::INT,rm.id),pc.email,pc.name` + poolRecipientMembershipSQL + `
	ON CONFLICT(campaign_id,pool_contact_id) DO UPDATE SET
		email_snapshot=EXCLUDED.email_snapshot,
		name_snapshot=EXCLUDED.name_snapshot,
		pool_id=EXCLUDED.pool_id,
		allocation_id=EXCLUDED.allocation_id,
		organization_id=EXCLUDED.organization_id,
		reply_mailbox_id=EXCLUDED.reply_mailbox_id,
		updated_at=NOW()
	WHERE cpr.status IN ` + poolSnapshotRefreshStatuses

// poolRecipientSnapshotPruneSQL removes the snapshot rows a refresh still owns
// that its audience no longer delivers to: the contact was excluded, its
// allocation was removed, or its pool contact row was archived. Without this
// step a stale pending row would keep the campaign's unsent count above zero
// forever, because the queue path refuses to hand it over. Rows that were
// already queued, sent, or cancelled are delivery history and are preserved, so
// the status predicate matches poolRecipientSnapshotUpsertSQL exactly.
//
// Parameters: $1 = campaign ID, $2 = pool ID, $3 = organization ID,
// $4 = org_pool_allocations ID or NULL for the whole organization scope,
// $5 = the pool contact IDs that are still deliverable.
const poolRecipientSnapshotPruneSQL = `DELETE FROM campaign_pool_recipients
	WHERE campaign_id=$1 AND pool_id=$2 AND organization_id=$3 AND ($4::BIGINT IS NULL OR allocation_id=$4::BIGINT)
		AND status IN ` + poolSnapshotRefreshStatuses + ` AND NOT (pool_contact_id=ANY($5::BIGINT[]))`

// poolRecipientAllOrgMembershipSQL is the all-organization variant of the
// shared membership rule: it iterates every active organization that owns a
// pool allocation in the first-level pool, keeps only organizations whose
// unified reply mailbox is usable, and deduplicates each contact to the first
// organization in the campaign's persisted rotation order (falling back to the
// organization ID order before the rotation is generated). The membership
// predicates — active allocation member, active pool member, active contact,
// no active exclusion — are identical to poolRecipientMembershipSQL.
//
// Positional parameters:
//
//	$1 = first-level pool ID
//	$2 = campaign ID (joins the campaign's persisted organization order)
const poolRecipientAllOrgMembershipSQL = `
	FROM pool_contacts pc
	JOIN org_pool_allocations s ON s.pool_id=$1
	JOIN organizations o ON o.id=s.organization_id AND o.status='active'
	JOIN org_pool_allocation_members sm ON sm.allocation_id=s.id AND sm.status='active' AND sm.contact_id=pc.id
	JOIN pool_members pm ON pm.pool_id=s.pool_id AND pm.contact_id=pc.id
	JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id AND rm.status='active' AND rm.verified_at IS NOT NULL
	LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=s.pool_id AND ex.organization_id=s.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL
	LEFT JOIN campaign_pool_org_orders oo ON oo.campaign_id=$2 AND oo.organization_id=s.organization_id
	WHERE ex.contact_id IS NULL AND pc.status='active'`

// poolRecipientAllOrgSelectSQL reads the deduplicated, all-organization
// deliverable contact IDs of one first-level pool for a platform-level
// campaign. Every contact appears once, owned by the organization that comes
// first in the campaign's rotation order.
const poolRecipientAllOrgSelectSQL = `SELECT DISTINCT ON (pc.id) pc.id` +
	poolRecipientAllOrgMembershipSQL + `
	ORDER BY pc.id, COALESCE(oo.dispatch_order, 2147483647), s.organization_id`

// poolRecipientAllOrgSnapshotUpsertSQL writes the all-organization snapshot in
// one statement from the shared membership rule. The rewrite is restricted to
// rows the refresh owns (pending/deferred), exactly like the single-org
// upsert, so queued/sent delivery history keeps its original target
// organization, mailbox and sender.
const poolRecipientAllOrgSnapshotUpsertSQL = `INSERT INTO campaign_pool_recipients AS cpr(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,reply_mailbox_id,email_snapshot,name_snapshot)
	SELECT $2,chosen.contact_id,$1,chosen.allocation_id,chosen.organization_id,chosen.reply_mailbox_id,chosen.email,chosen.name
	FROM (
		SELECT DISTINCT ON (pc.id)
			pc.id AS contact_id, pc.email, pc.name,
			s.id AS allocation_id, s.organization_id, rm.id AS reply_mailbox_id` +
	poolRecipientAllOrgMembershipSQL + `
		ORDER BY pc.id, COALESCE(oo.dispatch_order, 2147483647), s.organization_id
	) chosen
	ON CONFLICT(campaign_id,pool_contact_id) DO UPDATE SET
		email_snapshot=EXCLUDED.email_snapshot,
		name_snapshot=EXCLUDED.name_snapshot,
		pool_id=EXCLUDED.pool_id,
		allocation_id=EXCLUDED.allocation_id,
		organization_id=EXCLUDED.organization_id,
		reply_mailbox_id=EXCLUDED.reply_mailbox_id,
		updated_at=NOW()
	WHERE cpr.status IN ` + poolSnapshotRefreshStatuses

// poolRecipientAllOrgSnapshotPruneSQL removes still-refreshable snapshot rows
// that the all-organization rule no longer delivers to. Unlike the
// single-org prune it matches any target organization, because each row
// carries its own resolved target organization.
const poolRecipientAllOrgSnapshotPruneSQL = `DELETE FROM campaign_pool_recipients
	WHERE campaign_id=$1 AND pool_id=$2 AND status IN ` + poolSnapshotRefreshStatuses + ` AND NOT (pool_contact_id=ANY($3::BIGINT[]))`

// poolAudience is the delivery scope of one pool relation on a campaign: the
// first-level pool, the target organization, the explicitly selected pool allocation
// list (nil when the audience selected the first-level pool) and the reply
// mailbox resolved for the audience.
type poolAudience struct {
	poolID         int
	organizationID int64
	allocationID   *int64
	mailboxID      *int64
}

// EnsurePoolCampaignRecipients refreshes the campaign's pool recipient snapshot
// from the current membership of every resolved pool audience. It runs on each
// scheduler tick, which is what stops an in-flight campaign from delivering to a
// contact that was excluded, unassigned or archived after the snapshot was first
// written. The snapshot is keyed by the pool contact's stable internal ID, so a
// refresh never duplicates a recipient.
func (c *Core) EnsurePoolCampaignRecipients(campaignID int) error {
	var rows []struct {
		PoolScope      string        `db:"pool_scope"`
		PoolID         int           `db:"pool_id"`
		AllocationID   sql.NullInt64 `db:"org_pool_allocation_id"`
		OrganizationID sql.NullInt64 `db:"source_organization_id"`
		MailboxID      sql.NullInt64 `db:"resolved_reply_mailbox_id"`
	}
	// Audiences are refreshed in a stable order. A campaign may select the same
	// first-level pool and its pool allocation, or two pools that share a contact;
	// because the snapshot keeps one row per campaign and pool contact, a stable
	// order keeps that row's audience label deterministic across ticks.
	if err := c.db.Select(&rows, `SELECT c.pool_scope,ccl.pool_id,ccl.org_pool_allocation_id,ccl.source_organization_id,ccl.resolved_reply_mailbox_id
		FROM campaign_customer_lists ccl
		JOIN campaigns c ON c.id=ccl.campaign_id
		WHERE ccl.campaign_id=$1 AND ccl.pool_id IS NOT NULL
		ORDER BY ccl.pool_id,ccl.org_pool_allocation_id NULLS FIRST`, campaignID); err != nil {
		return err
	}
	for _, row := range rows {
		if row.PoolScope == models.CampaignPoolScopeAllOrganizations {
			// Platform-level campaign: the audience covers every active
			// organization's pool allocation of the first-level pool. Each
			// contact is deduplicated to its rotation-order organization.
			if err := c.refreshAllOrgPoolCampaignRecipients(campaignID, row.PoolID); err != nil {
				return err
			}
			continue
		}
		if !row.OrganizationID.Valid || !row.MailboxID.Valid {
			continue
		}
		mailboxID := row.MailboxID.Int64
		audience := poolAudience{poolID: row.PoolID, organizationID: row.OrganizationID.Int64, mailboxID: &mailboxID}
		if row.AllocationID.Valid {
			allocationID := row.AllocationID.Int64
			audience.allocationID = &allocationID
		} else {
			// A first-level selection resolves to the single pool allocation the
			// organization owns for the pool. Without exactly one, the audience
			// stays unresolved and its snapshot must not be rewritten.
			var allocationCount int
			if err := c.db.Get(&allocationCount, `SELECT COUNT(*) FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2`, row.PoolID, row.OrganizationID.Int64); err != nil {
				return err
			}
			if allocationCount != 1 {
				continue
			}
		}
		if err := c.refreshPoolCampaignRecipients(campaignID, audience); err != nil {
			return err
		}
	}
	return nil
}

// refreshAllOrgPoolCampaignRecipients refreshes the platform-level snapshot of
// one first-level pool audience: every deliverable contact of every active
// organization appears once, owned by the organization first in the
// campaign's rotation order.
func (c *Core) refreshAllOrgPoolCampaignRecipients(campaignID, poolID int) error {
	var ids []int64
	if err := c.db.Select(&ids, poolRecipientAllOrgSelectSQL, poolID, campaignID); err != nil {
		return err
	}
	if _, err := c.db.Exec(poolRecipientAllOrgSnapshotPruneSQL, campaignID, poolID, pq.Int64Array(ids)); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := c.db.Exec(poolRecipientAllOrgSnapshotUpsertSQL, poolID, campaignID)
	return err
}

// refreshPoolCampaignRecipients makes the campaign snapshot of one pool audience
// equal the deliverable members of that audience. Both the resolve paths and the
// snapshot writer consume poolRecipientMembershipSQL, so the three paths cannot
// produce different recipient sets.
func (c *Core) refreshPoolCampaignRecipients(campaignID int, aud poolAudience) error {
	recipients, err := c.resolvePoolRecipients(aud.poolID, aud.organizationID, aud.allocationID)
	if err != nil {
		return err
	}
	ids := make(pq.Int64Array, 0, len(recipients))
	for _, r := range recipients {
		ids = append(ids, r.ID)
	}
	if _, err := c.db.Exec(poolRecipientSnapshotPruneSQL, campaignID, aud.poolID, aud.organizationID, aud.allocationID, ids); err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	_, err = c.db.Exec(poolRecipientSnapshotUpsertSQL, aud.poolID, aud.organizationID, aud.allocationID, campaignID, aud.mailboxID)
	return err
}

// resolvePoolRecipients reads the deliverable members of one pool audience. A
// nil allocationID accepts every pool allocation the organization owns in the pool,
// which is how a first-level audience selection is resolved.
func (c *Core) resolvePoolRecipients(poolID int, organizationID int64, allocationID *int64) ([]PoolRecipient, error) {
	if err := c.ensurePool(poolID); err != nil {
		return nil, err
	}
	var out []PoolRecipient
	err := c.db.Select(&out, poolRecipientSelectSQL, poolID, organizationID, allocationID)
	return out, err
}

// ResolvePoolRecipients resolves a first-level pool audience for one
// organization. It shares its membership rule with the pool-allocation resolve
// and with the campaign snapshot writer.
func (c *Core) ResolvePoolRecipients(poolID int, organizationID int64) ([]PoolRecipient, error) {
	return c.resolvePoolRecipients(poolID, organizationID, nil)
}

// resolvePoolRecipientsForAllocation resolves one explicitly selected pool allocation
// list for one organization.
func (c *Core) resolvePoolRecipientsForAllocation(poolID int, organizationID, allocationID int64) ([]PoolRecipient, error) {
	return c.resolvePoolRecipients(poolID, organizationID, &allocationID)
}

// organizationReplyMailboxID resolves the organization's single unified reply
// mailbox, but only while it is usable: the mailbox row must be active and
// verified. Public-pool audiences never fall back to a per-allocation, personal
// or default mailbox, so an organization without a usable unified mailbox
// leaves the audience unresolved.
func (c *Core) organizationReplyMailboxID(organizationID int64) (*int64, error) {
	var mailboxID int64
	if err := c.db.Get(&mailboxID, `
		SELECT o.reply_mailbox_id
		FROM organizations o
		JOIN reply_mailboxes rm ON rm.id=o.reply_mailbox_id
			AND rm.status='active' AND rm.verified_at IS NOT NULL
		WHERE o.id=$1`, organizationID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &mailboxID, nil
}

// AttachPoolToCampaign records a pool/allocation audience on a draft campaign and
// refreshes that audience's recipient snapshot from the shared membership rule.
// Saving a draft audience again is a refresh: a contact that was excluded since
// the previous save loses its snapshot row instead of keeping a stale one.
//
// The audience carries no reply mailbox of its own any more: the resolved route
// is the target organization's unified reply mailbox, and only while that
// mailbox is active and verified. A draft is still saved without one so the
// administrator can finish the organization configuration later; preview and
// send stay blocked until then.
func (c *Core) AttachPoolToCampaign(campaignID, poolID int, allocationID *int64, organizationID int64, allOrganizations bool) error {
	var err error
	if err := c.ensurePool(poolID); err != nil {
		return err
	}
	if allOrganizations {
		// Platform-level audience: permission to send to every organization
		// is checked by the campaign API (campaigns:public_pool_send), not by
		// a per-organization pool delivery grant. The relation carries no
		// target organization, allocation or mailbox: recipients resolve per
		// contact to every active organization's allocation and its unified
		// reply mailbox. A previous rotation is discarded so the snapshot
		// refresh recomputes membership for the newly selected pool.
		var name string
		if err := c.db.Get(&name, `SELECT name FROM customer_lists WHERE id=$1`, poolID); err != nil {
			return err
		}
		if _, err = c.db.Exec(`DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`, campaignID); err != nil {
			return err
		}
		if _, err = c.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
			VALUES($1,NULL,$2,$3,NULL,NULL,NULL) ON CONFLICT DO NOTHING`, campaignID, name, poolID); err != nil {
			return err
		}
		return c.refreshAllOrgPoolCampaignRecipients(campaignID, poolID)
	}
	if organizationID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "organization is required for pool audiences")
	}
	var permitted bool
	if err := c.db.Get(&permitted, `SELECT EXISTS(SELECT 1 FROM pool_organization_permissions WHERE pool_id=$1 AND organization_id=$2) OR EXISTS(SELECT 1 FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2)`, poolID, organizationID); err != nil {
		return err
	}
	if !permitted {
		return echo.NewHTTPError(http.StatusForbidden, "pool delivery access has not been granted to organization")
	}
	var name string
	if err := c.db.Get(&name, `SELECT name FROM customer_lists WHERE id=$1`, poolID); err != nil {
		return err
	}
	mailbox, err := c.organizationReplyMailboxID(organizationID)
	if err != nil {
		return err
	}
	// Selecting a first-level pool resolves to the single effective pool allocation
	// for the target organization. If none (or more than one) exists, retain an
	// unresolved relation so drafts can be saved but preview/send will be blocked
	// until an administrator fixes the assignment.
	// Keep the audience selection semantics (pool vs explicit allocation) separate
	// from the resolved delivery allocation used for recipients. This lets an
	// activity remain editable with the original first-level pool ID even after
	// a unique pool allocation is resolved.
	selectedAllocationID := allocationID
	if allocationID == nil {
		var candidates []struct {
			ID int64 `db:"id"`
		}
		if err := c.db.Select(&candidates, `SELECT id FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2 ORDER BY id`, poolID, organizationID); err != nil {
			return err
		}
		if len(candidates) == 1 {
			id := candidates[0].ID
			allocationID = &id
		}
	} else {
		var exists bool
		if err := c.db.Get(&exists, `SELECT EXISTS(SELECT 1 FROM org_pool_allocations WHERE id=$1 AND pool_id=$2 AND organization_id=$3)`, *allocationID, poolID, organizationID); err != nil {
			return err
		}
		if !exists {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid pool allocation")
		}
	}
	_, err = c.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
		VALUES($1,NULL,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, campaignID, name, poolID, selectedAllocationID, organizationID, mailbox)
	if err != nil {
		return err
	}
	if allocationID != nil {
		// The snapshot of every row carries the resolved organization mailbox.
		if err := c.refreshPoolCampaignRecipients(campaignID, poolAudience{poolID: poolID, organizationID: organizationID, allocationID: allocationID, mailboxID: mailbox}); err != nil {
			return err
		}
	}
	return nil
}

// ImportListIntoPool imports an ordinary customer list into a first-class pool.
// Customer code is only a search attribute: records are reused when all
// normalized fields match, while same-code differences are retained and
// written to the conflict audit table.
func (c *Core) ImportListIntoPool(listID, poolID, userID int) error {
	if err := c.ensurePool(poolID); err != nil {
		return err
	}
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Queryx(`SELECT c.customer_code,c.name,c.email,c.attribs FROM customers c JOIN customer_list_memberships m ON m.customer_id=c.id WHERE m.customer_list_id=$1`, listID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var code, name, email string
		var attribs []byte
		if err := rows.Scan(&code, &name, &email, &attribs); err != nil {
			return err
		}
		var id int64
		var existingID int64
		var existingEmail, existingName string
		err = tx.QueryRow(`SELECT id,email,name FROM pool_contacts WHERE customer_code=$1 ORDER BY id LIMIT 1`, code).Scan(&existingID, &existingEmail, &existingName)
		hasCode := err == nil
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		err = tx.Get(&id, `SELECT id FROM pool_contacts WHERE customer_code=$1 AND LOWER(email)=LOWER($2) AND name=$3 AND attribs=$4::jsonb LIMIT 1`, code, email, name, string(attribs))
		if err == sql.ErrNoRows {
			if err = tx.Get(&id, `INSERT INTO pool_contacts(customer_code,company_name,email,name,attribs) VALUES($1,'',$2,$3,$4::jsonb) RETURNING id`, code, email, name, string(attribs)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, poolID, id); err != nil {
			return err
		}
		if hasCode && (strings.ToLower(existingEmail) != strings.ToLower(email) || existingName != name || existingID != id) {
			if _, err = tx.Exec(`INSERT INTO pool_merge_conflicts(pool_id,contact_id,customer_code,existing_snapshot,incoming_snapshot,created_by_user_id) VALUES($1,$2,$3,(SELECT jsonb_build_object('email',email,'name',name,'attribs',attribs) FROM pool_contacts WHERE id=$2),jsonb_build_object('email',$4,'name',$5,'attribs',$6::jsonb),$7)`, poolID, existingID, code, email, name, string(attribs), userID); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
