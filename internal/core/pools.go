package core

import (
	"database/sql"
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
type PoolRecipient struct {
	models.PoolContact
	AllocationID   int64 `db:"allocation_id" json:"allocation_id"`
	OrganizationID int64 `db:"organization_id" json:"organization_id"`
	ReplyMailboxID *int  `db:"reply_mailbox_id" json:"reply_mailbox_id,omitempty"`
}

func normalizePoolAllocationDepartment(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// UpdateOrgPoolAllocationReplyMailbox changes the internal reply destination for an
// organization allocation. The mailbox must belong to the same organization as
// the allocation; customer addresses are never involved in this operation.
func (c *Core) UpdateOrgPoolAllocationReplyMailbox(allocationID int64, replyMailboxID *int) error {
	var organizationID int64
	if err := c.db.Get(&organizationID, `SELECT organization_id FROM org_pool_allocations WHERE id=$1`, allocationID); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "pool allocation not found")
		}
		return err
	}
	if replyMailboxID != nil {
		var mailboxOrg sql.NullInt64
		if err := c.db.Get(&mailboxOrg, `SELECT organization_id FROM reply_mailboxes WHERE id=$1`, *replyMailboxID); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid reply mailbox")
		}
		if !mailboxOrg.Valid || mailboxOrg.Int64 != organizationID {
			return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must belong to allocation organization")
		}
	}
	_, err := c.db.Exec(`UPDATE org_pool_allocations SET reply_mailbox_id=$2 WHERE id=$1`, allocationID, replyMailboxID)
	return err
}

func (c *Core) QueryOrgPoolAllocations(poolID int, organizationID int64, platformAdmin bool) ([]models.OrgPoolAllocation, error) {
	if err := c.ensurePool(poolID); err != nil {
		return nil, err
	}
	q := `SELECT s.id,s.list_id,COALESCE(l.name,'') AS list_name,s.pool_id,s.organization_id,COALESCE(o.name,'') AS organization_name,s.reply_mailbox_id,COALESCE(r.email,'') AS reply_mailbox_email FROM org_pool_allocations s JOIN customer_lists l ON l.id=s.list_id JOIN organizations o ON o.id=s.organization_id LEFT JOIN reply_mailboxes r ON r.id=s.reply_mailbox_id WHERE s.pool_id=$1`
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
			COALESCE(o.name,'') AS organization_name,s.reply_mailbox_id,
			COALESCE(r.email,'') AS reply_mailbox_email
		FROM org_pool_allocations s
		JOIN customer_lists l ON l.id=s.list_id
		JOIN organizations o ON o.id=s.organization_id
		LEFT JOIN reply_mailboxes r ON r.id=s.reply_mailbox_id
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
		if replyMailboxID != nil {
			var mailboxOrg sql.NullInt64
			if err := tx.Get(&mailboxOrg, `SELECT organization_id FROM reply_mailboxes WHERE id=$1`, *replyMailboxID); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid reply mailbox")
			}
			if !mailboxOrg.Valid || mailboxOrg.Int64 != organizationID {
				return echo.NewHTTPError(http.StatusForbidden, "reply mailbox must belong to organization")
			}
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
		if err := tx.Get(&out, `INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,reply_mailbox_id,created_by_user_id)
			VALUES($1,$2,$3,$4,$5)
			RETURNING id,list_id,(SELECT name FROM customer_lists WHERE id=org_pool_allocations.list_id) AS list_name,
				pool_id,organization_id,(SELECT name FROM organizations WHERE id=org_pool_allocations.organization_id) AS organization_name,reply_mailbox_id`,
			listID, poolID, organizationID, replyMailboxID, userID); err != nil {
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
		if replyMailboxID != nil {
			return tx.Get(&out.ReplyMailboxEmail, `SELECT email FROM reply_mailboxes WHERE id=$1`, *replyMailboxID)
		}
		return nil
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
// organization's pool allocation mailbox is configured; in that case the
// relation keeps a NULL resolved mailbox. Re-reading the current allocation
// configuration here makes an existing draft usable after the administrator
// finishes the configuration, without changing the first-level/pool-allocation
// audience selection stored in org_pool_allocation_id.
func (c *Core) refreshPoolCampaignAudienceRoutes(campaignID int) error {
	_, err := c.db.Exec(`
		UPDATE campaign_customer_lists ccl
		SET resolved_reply_mailbox_id = (
			SELECT CASE
				WHEN COUNT(DISTINCT s.id) = 1 AND COUNT(DISTINCT rm.id) = 1 THEN MAX(rm.id)
				ELSE NULL
			END
			FROM org_pool_allocations s
			LEFT JOIN reply_mailboxes rm ON rm.id=s.reply_mailbox_id
				AND rm.status='active' AND rm.verified_at IS NOT NULL
			WHERE ccl.source_organization_id IS NOT NULL
				AND s.pool_id=ccl.pool_id
				AND s.organization_id=ccl.source_organization_id
				AND (ccl.org_pool_allocation_id IS NULL OR s.id=ccl.org_pool_allocation_id)
		)
		WHERE ccl.campaign_id=$1 AND ccl.pool_id IS NOT NULL`, campaignID)
	return err
}

// ValidatePoolCampaignAudience is called by preview/send paths. Drafts may
// retain an unresolved pool audience, but sending is blocked until every pool
// row resolves to an organization allocation and an internal reply mailbox.
func (c *Core) ValidatePoolCampaignAudience(campaignID int) error {
	if err := c.refreshPoolCampaignAudienceRoutes(campaignID); err != nil {
		return err
	}
	var unresolved int
	if err := c.db.Get(&unresolved, `SELECT COUNT(*) FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL AND (source_organization_id IS NULL OR resolved_reply_mailbox_id IS NULL)`, campaignID); err != nil {
		return err
	}
	if unresolved > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "public-pool audience requires an organization allocation and reply mailbox before previewing or sending")
	}
	return nil
}

// poolRecipientMembershipSQL is the single definition of a deliverable pool
// recipient: an active pool member whose organization allocation is still
// active, which that organization has not excluded, and whose pool contact row
// is still active. The first-level resolve, the pool-allocation resolve and the
// campaign snapshot writer all embed exactly this fragment, so a new exclusion
// source or contact status cannot be added to one audience path while another
// silently keeps sending to the contact.
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
	JOIN org_pool_allocation_members sm ON sm.allocation_id=s.id AND sm.status='active'
	JOIN pool_members pm ON pm.pool_id=s.pool_id AND pm.contact_id=sm.contact_id
	JOIN pool_contacts pc ON pc.id=sm.contact_id
	LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=s.pool_id AND ex.organization_id=s.organization_id AND ex.contact_id=pc.id AND ex.restored_at IS NULL
	WHERE s.pool_id=$1 AND s.organization_id=$2 AND ($3::BIGINT IS NULL OR s.id=$3::BIGINT) AND ex.contact_id IS NULL AND pc.status='active'`

// poolRecipientSelectSQL reads the deliverable members of one pool audience. It
// is the read half of the shared rule and is deduplicated by the pool contact's
// stable internal ID, which is also the snapshot's primary key.
const poolRecipientSelectSQL = `SELECT DISTINCT ON (pc.id) pc.id,pc.customer_code,pc.company_name,pc.email,pc.name,pc.status,s.id AS allocation_id,s.organization_id,s.reply_mailbox_id` + poolRecipientMembershipSQL + ` ORDER BY pc.id,s.id`

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
//	$5 = audience reply mailbox, or NULL to keep the per-pool-allocation mailbox
//	     of each resolved row
const poolRecipientSnapshotUpsertSQL = `INSERT INTO campaign_pool_recipients AS cpr(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,reply_mailbox_id,email_snapshot,name_snapshot)
	SELECT $4,pc.id,$1,s.id,$2,COALESCE($5::INT,s.reply_mailbox_id),pc.email,pc.name` + poolRecipientMembershipSQL + `
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
		PoolID         int           `db:"pool_id"`
		AllocationID   sql.NullInt64 `db:"org_pool_allocation_id"`
		OrganizationID sql.NullInt64 `db:"source_organization_id"`
		MailboxID      sql.NullInt64 `db:"resolved_reply_mailbox_id"`
	}
	// Audiences are refreshed in a stable order. A campaign may select the same
	// first-level pool and its pool allocation, or two pools that share a contact;
	// because the snapshot keeps one row per campaign and pool contact, a stable
	// order keeps that row's audience label deterministic across ticks.
	if err := c.db.Select(&rows, `SELECT pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL ORDER BY pool_id,org_pool_allocation_id NULLS FIRST`, campaignID); err != nil {
		return err
	}
	for _, row := range rows {
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

// AttachPoolToCampaign records a pool/allocation audience on a draft campaign and
// refreshes that audience's recipient snapshot from the shared membership rule.
// Saving a draft audience again is a refresh: a contact that was excluded since
// the previous save loses its snapshot row instead of keeping a stale one.
func (c *Core) AttachPoolToCampaign(campaignID, poolID int, allocationID *int64, organizationID int64) error {
	var err error
	if err := c.ensurePool(poolID); err != nil {
		return err
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
	var mailbox *int64
	// Selecting a first-level pool resolves to the single effective pool allocation
	// allocation for the target organization. If none (or more than one) exists,
	// retain an unresolved relation so drafts can be saved but preview/send will
	// be blocked until an administrator fixes the assignment.
	// Keep the audience selection semantics (pool vs explicit allocation) separate
	// from the resolved delivery allocation used for recipients. This lets an
	// activity remain editable with the original first-level pool ID even after
	// a unique pool allocation is resolved.
	selectedAllocationID := allocationID
	unresolvedFirstLevel := false
	if allocationID == nil {
		var candidates []struct {
			ID        int64         `db:"id"`
			MailboxID sql.NullInt64 `db:"reply_mailbox_id"`
		}
		if err := c.db.Select(&candidates, `SELECT id,reply_mailbox_id FROM org_pool_allocations WHERE pool_id=$1 AND organization_id=$2 ORDER BY id`, poolID, organizationID); err != nil {
			return err
		}
		if len(candidates) == 1 {
			id := candidates[0].ID
			allocationID = &id
			if candidates[0].MailboxID.Valid {
				mailboxID := candidates[0].MailboxID.Int64
				mailbox = &mailboxID
			} else {
				unresolvedFirstLevel = true
			}
		} else {
			unresolvedFirstLevel = true
		}
	}
	if allocationID != nil {
		var m sql.NullInt64
		if err := c.db.Get(&m, `SELECT reply_mailbox_id FROM org_pool_allocations WHERE id=$1 AND pool_id=$2 AND organization_id=$3`, *allocationID, poolID, organizationID); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid pool allocation")
		}
		if m.Valid && mailbox == nil {
			mailboxID := m.Int64
			mailbox = &mailboxID
		}
	} else if !unresolvedFirstLevel {
		// The audience selected the first-level pool and resolved to the single
		// pool allocation of the organization. It expands through the same
		// snapshot refresh as every other path, with the reply mailbox of each
		// resolved row.
		if err := c.refreshPoolCampaignRecipients(campaignID, poolAudience{poolID: poolID, organizationID: organizationID}); err != nil {
			return err
		}
	}
	_, err = c.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id)
		VALUES($1,NULL,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, campaignID, name, poolID, selectedAllocationID, organizationID, mailbox)
	if err != nil {
		return err
	}
	if allocationID != nil {
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
