package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// AllocationAccess is a capability for this domain only, never a generic
// workspace/platform privilege. HTTP derives it from authenticated role grants.
type AllocationAccess struct {
	UserID, OrganizationID               int
	Manage, Across, Read, Audit, Highest bool
}

// PoolRecipientAvailableSQL is rechecked at claim time and immediately before
// SMTP. A queued snapshot is not evidence that its allocation is still usable.
// The cpr alias belongs to the caller. All-organization sends retain their
// separate campaigns:public_pool_send policy, not an implicit organization grant.
const PoolRecipientAvailableSQL = `EXISTS(SELECT 1 FROM org_pool_allocations availability
	JOIN organizations ao ON ao.id=availability.organization_id AND ao.status='active'
	JOIN customer_lists al ON al.id=availability.list_id AND al.status='active' AND al.transfer_pending_at IS NULL
	JOIN customer_lists ap ON ap.id=availability.pool_id AND ap.status='active' AND ap.transfer_pending_at IS NULL
	JOIN pool_members avm ON avm.pool_id=availability.pool_id AND avm.contact_id=cpr.pool_contact_id
	JOIN org_pool_allocation_members am ON am.allocation_id=availability.id AND am.contact_id=cpr.pool_contact_id AND am.status='active'
	WHERE availability.id=cpr.allocation_id AND availability.pool_id=cpr.pool_id
	AND availability.organization_id=cpr.organization_id AND availability.state='active')
	AND (EXISTS(SELECT 1 FROM campaigns ac WHERE ac.id=cpr.campaign_id AND ac.pool_scope='all_organizations')
	OR EXISTS(SELECT 1 FROM pool_organization_permissions ag WHERE ag.pool_id=cpr.pool_id AND ag.organization_id=cpr.organization_id))`

type AllocationDetail struct {
	ID               int64     `db:"id" json:"id"`
	ListID           int       `db:"list_id" json:"list_id"`
	PoolID           int       `db:"pool_id" json:"pool_id"`
	OrganizationID   int       `db:"organization_id" json:"organization_id"`
	Name             string    `db:"name" json:"name"`
	PoolName         string    `db:"pool_name" json:"pool_name"`
	OrganizationName string    `db:"organization_name" json:"organization_name"`
	State            string    `db:"state" json:"state"`
	Description      string    `db:"description" json:"description"`
	Version          int64     `db:"version" json:"version"`
	UpdatedAt        time.Time `db:"updated_at" json:"updated_at"`
	DeliveryAllowed  bool      `db:"delivery_allowed" json:"delivery_allowed"`
}

const allocationDetailSQL = `SELECT s.id,s.list_id,s.pool_id,s.organization_id,l.name,
	p.name AS pool_name,o.name AS organization_name,s.state,s.description,s.version,s.updated_at,
	EXISTS(SELECT 1 FROM pool_organization_permissions g WHERE g.pool_id=s.pool_id AND g.organization_id=s.organization_id) AS delivery_allowed
	FROM org_pool_allocations s JOIN customer_lists l ON l.id=s.list_id
	JOIN customer_lists p ON p.id=s.pool_id JOIN organizations o ON o.id=s.organization_id`

func (c *Core) EnrichAllocationListStates(lists []models.CustomerList) error {
	ids := make([]int, 0)
	for _, l := range lists {
		if l.Type == models.CustomerListTypeOrgPoolAllocation {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var states []struct {
		ListID int    `db:"list_id"`
		State  string `db:"state"`
	}
	if err := c.db.Select(&states, `SELECT list_id,state FROM org_pool_allocations WHERE list_id=ANY($1)`, pq.Array(ids)); err != nil {
		return err
	}
	byID := map[int]string{}
	for _, r := range states {
		byID[r.ListID] = r.State
	}
	for i := range lists {
		lists[i].AllocationState = byID[lists[i].ID]
	}
	return nil
}

func (c *Core) allocationAccess(a AllocationAccess, d AllocationDetail) error {
	if !a.Manage || (!a.Across && (a.OrganizationID <= 0 || a.OrganizationID != d.OrganizationID)) {
		return echo.NewHTTPError(http.StatusForbidden, "pool allocation is outside the management scope")
	}
	if !a.Across {
		var member bool
		if err := c.db.Get(&member, `SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=$1 AND user_id=$2 AND removed_at IS NULL)`, d.OrganizationID, a.UserID); err != nil {
			return err
		}
		if !member {
			return echo.NewHTTPError(http.StatusForbidden, "organization membership is required")
		}
	}
	return nil
}

func (c *Core) GetAllocationDetail(a AllocationAccess, id int64) (AllocationDetail, error) {
	var d AllocationDetail
	if err := c.db.Get(&d, allocationDetailSQL+` WHERE s.id=$1`, id); err != nil {
		if err == sql.ErrNoRows {
			return d, ErrNotFound
		}
		return d, err
	}
	return d, c.allocationAccess(a, d)
}

const allocationActivityWhere = ` FROM campaigns c JOIN campaign_customer_lists cl ON cl.campaign_id=c.id
	WHERE cl.pool_id=$1 AND c.status NOT IN ('finished','cancelled')
	AND (c.pool_scope='all_organizations' OR c.organization_id=$2 OR cl.org_pool_allocation_id=$3)`

func (c *Core) AllocationImpact(a AllocationAccess, id int64) (int, error) {
	d, err := c.GetAllocationDetail(a, id)
	if err != nil {
		return 0, err
	}
	var n int
	err = c.db.Get(&n, `SELECT COUNT(DISTINCT c.id)`+allocationActivityWhere, d.PoolID, d.OrganizationID, d.ID)
	return n, err
}

// The organization and source/list locks are shared with lifecycle and resource
// mutations. Re-resolving after locking prevents stale membership/state writes.
func (c *Core) withAllocationMutation(a AllocationAccess, id int64, fn func(*sqlx.Tx, AllocationDetail) error) error {
	d, err := c.GetAllocationDetail(a, id)
	if err != nil {
		return err
	}
	return c.withWorkspaceCreation(models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: d.OrganizationID, PlatformAdmin: a.Across}, UserID: a.UserID,
	}, func(tx *sqlx.Tx) error {
		var rows []struct {
			ID      int    `db:"id"`
			Status  string `db:"status"`
			Pending bool   `db:"pending"`
		}
		if err := tx.Select(&rows, `SELECT id,status::text,transfer_pending_at IS NOT NULL AS pending FROM customer_lists WHERE id=ANY($1) ORDER BY id FOR UPDATE`, pq.Array([]int{d.PoolID, d.ListID})); err != nil {
			return err
		}
		if len(rows) != 2 {
			return ErrNotFound
		}
		for _, r := range rows {
			if r.Status != "active" || r.Pending {
				return workspaceMutationError()
			}
		}
		if err := tx.Get(&d, allocationDetailSQL+` WHERE s.id=$1 FOR UPDATE OF s`, id); err != nil {
			return err
		}
		if !a.Across && d.OrganizationID != a.OrganizationID {
			return workspaceMutationError()
		}
		return fn(tx, d)
	})
}

func (c *Core) recordAllocationTx(tx *sqlx.Tx, a AllocationAccess, d AllocationDetail, action string, metadata map[string]any) error {
	org := int64(d.OrganizationID)
	metadata["pool_id"] = d.PoolID
	metadata["allocation_id"] = d.ID
	return auditlog.New(c.db, c.log).RecordTx(context.Background(), tx, auditlog.Event{
		OrganizationID: &org, ActorType: "user", ActorUserID: &a.UserID,
		Action: "org_pool_allocation." + action, ObjectType: "org_pool_allocation", ObjectID: strconv.FormatInt(d.ID, 10), Metadata: metadata,
	})
}

type AllocationChange struct {
	Action              string `json:"action"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Reason              string `json:"reason"`
	ConfirmOrganization string `json:"confirm_organization"`
	ExpectedVersion     int64  `json:"expected_version"`
}

func (c *Core) ChangeAllocation(a AllocationAccess, id int64, in AllocationChange) error {
	return c.withAllocationMutation(a, id, func(tx *sqlx.Tx, d AllocationDetail) error {
		if in.ExpectedVersion != d.Version {
			return echo.NewHTTPError(http.StatusConflict, "allocation changed; reload before saving")
		}
		state := d.State
		switch in.Action {
		case "update":
			in.Name = strings.TrimSpace(in.Name)
			if d.State == "detached" || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 200 || utf8.RuneCountInString(in.Description) > 1000 {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid allocation information")
			}
			if _, err := tx.Exec(`UPDATE customer_lists SET name=$2,updated_at=NOW() WHERE id=$1`, d.ListID, in.Name); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE org_pool_allocations SET description=$2 WHERE id=$1`, id, in.Description); err != nil {
				return err
			}
		case "disable":
			if d.State != "active" {
				return workspaceMutationError()
			}
			state = "disabled"
		case "enable":
			if d.State != "disabled" {
				return workspaceMutationError()
			}
			state = "active"
		case "detach":
			if !a.Across {
				return echo.NewHTTPError(http.StatusForbidden, "cross-organization allocation management is required")
			}
			if d.State == "detached" || in.ConfirmOrganization != d.OrganizationName || strings.TrimSpace(in.Reason) == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "organization confirmation and reason are required")
			}
			var n int
			if err := tx.Get(&n, `SELECT COUNT(DISTINCT c.id)`+allocationActivityWhere, d.PoolID, d.OrganizationID, d.ID); err != nil {
				return err
			}
			if n > 0 {
				return echo.NewHTTPError(http.StatusConflict, "unfinished campaigns reference this allocation")
			}
			state = "detached"
		case "rebind":
			if !a.Across {
				return echo.NewHTTPError(http.StatusForbidden, "cross-organization allocation management is required")
			}
			if d.State != "detached" {
				return workspaceMutationError()
			}
			state = "disabled"
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "invalid allocation action")
		}
		if utf8.RuneCountInString(in.Reason) > 1000 {
			return echo.NewHTTPError(http.StatusBadRequest, "reason is too long")
		}
		if _, err := tx.Exec(`UPDATE org_pool_allocations SET state=$2,version=version+1,updated_at=NOW() WHERE id=$1`, id, state); err != nil {
			return err
		}
		return c.recordAllocationTx(tx, a, d, in.Action, map[string]any{"previous_state": d.State, "state": state, "reason": in.Reason})
	})
}

// AllocationContacts uses a purpose-built DTO, including for cross-org delegates.
// Candidate browsing never broadens an operator's first-level master-data scope.
func (c *Core) AllocationContacts(a AllocationAccess, id int64, kind, search string, offset, limit int) ([]models.SafePoolContact, int, error) {
	d, err := c.GetAllocationDetail(a, id)
	if err != nil {
		return nil, 0, err
	}
	if !a.Read {
		return nil, 0, echo.NewHTTPError(http.StatusForbidden, "permission denied: pools:get")
	}
	if kind != "active" && kind != "removed" && kind != "candidates" {
		return nil, 0, echo.NewHTTPError(http.StatusBadRequest, "invalid member status")
	}
	where := ` FROM pool_contacts pc JOIN pool_members pm ON pm.contact_id=pc.id AND pm.pool_id=$1
	LEFT JOIN org_pool_allocation_members sm ON sm.contact_id=pc.id AND sm.allocation_id=$2
	LEFT JOIN org_pool_allocation_exclusions ex ON ex.pool_id=$1 AND ex.organization_id=$3 AND ex.contact_id=pc.id AND ex.restored_at IS NULL
	WHERE (pc.customer_code ILIKE $4 OR pc.name ILIKE $4)`
	args := []any{d.PoolID, d.ID, d.OrganizationID, "%" + search + "%"}
	if kind == "candidates" {
		where += ` AND sm.contact_id IS NULL AND ex.contact_id IS NULL`
		if !a.Highest {
			where += ` AND LOWER(TRIM(pc.allocation_department))=LOWER($5)`
			args = append(args, d.OrganizationName)
		}
	} else if kind == "removed" {
		where += ` AND sm.status='removed'`
	} else {
		where += ` AND sm.status='active'`
	}
	var total int
	if err = c.db.Get(&total, `SELECT COUNT(*)`+where, args...); err != nil {
		return nil, 0, err
	}
	var rows []models.PoolContact
	q := fmt.Sprintf(`SELECT pc.*,sm.status='removed' AS excluded,COALESCE(sm.removed_reason,'') AS exclusion_reason`+where+` ORDER BY pc.id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	// COALESCE is necessary for not-yet-assigned candidate rows.
	q = strings.Replace(q, "sm.status='removed' AS excluded", "COALESCE(sm.status='removed',false) AS excluded", 1)
	if err = c.db.Select(&rows, q, append(args, limit, offset)...); err != nil {
		return nil, 0, err
	}
	out := make([]models.SafePoolContact, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Safe())
	}
	return out, total, nil
}

func (c *Core) ChangeAllocationMembers(a AllocationAccess, id int64, action, reason string, ids []int64) error {
	if len(ids) < 1 || len(ids) > 100 {
		return echo.NewHTTPError(http.StatusBadRequest, "select 1 to 100 contacts")
	}
	if action != "assign" && action != "remove" && action != "restore" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid member action")
	}
	if utf8.RuneCountInString(reason) > 1000 || (action == "remove" && strings.TrimSpace(reason) == "") {
		return echo.NewHTTPError(http.StatusBadRequest, "a removal reason of 1 to 1000 characters is required")
	}
	return c.withAllocationMutation(a, id, func(tx *sqlx.Tx, d AllocationDetail) error {
		if d.State != "active" {
			return echo.NewHTTPError(http.StatusConflict, "allocation is not active")
		}
		seen := map[int64]bool{}
		for _, contactID := range ids {
			if contactID < 1 {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid contact id")
			}
			if seen[contactID] {
				continue
			}
			seen[contactID] = true
			var valid bool
			if err := tx.Get(&valid, `SELECT EXISTS(SELECT 1 FROM pool_members WHERE pool_id=$1 AND contact_id=$2)`, d.PoolID, contactID); err != nil {
				return err
			}
			if !valid {
				return echo.NewHTTPError(http.StatusBadRequest, "contact is outside the source pool")
			}
			var status string
			err := tx.Get(&status, `SELECT status FROM org_pool_allocation_members WHERE allocation_id=$1 AND contact_id=$2 FOR UPDATE`, id, contactID)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if action == "assign" {
				if status == "removed" {
					return echo.NewHTTPError(http.StatusConflict, "use restore for removed contacts")
				}
				var eligible bool
				if err := tx.Get(&eligible, `SELECT NOT EXISTS(SELECT 1 FROM org_pool_allocation_exclusions WHERE pool_id=$1 AND organization_id=$2 AND contact_id=$3 AND restored_at IS NULL)
				AND ($4 OR EXISTS(SELECT 1 FROM pool_contacts WHERE id=$3 AND LOWER(TRIM(allocation_department))=LOWER($5)))`, d.PoolID, d.OrganizationID, contactID, a.Highest, d.OrganizationName); err != nil {
					return err
				}
				if !eligible {
					return echo.NewHTTPError(http.StatusForbidden, "contact is outside the candidate scope")
				}
				if _, err := tx.Exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, contactID); err != nil {
					return err
				}
			} else {
				if status == "" {
					return echo.NewHTTPError(http.StatusBadRequest, "contact is not in the allocation")
				}
				if action == "remove" {
					if _, err := tx.Exec(`UPDATE org_pool_allocation_members SET status='removed',removed_reason=$3,removed_at=NOW(),removed_by_user_id=$4,updated_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, id, contactID, reason, a.UserID); err != nil {
						return err
					}
					if _, err := tx.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,removed_by_user_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(pool_id,organization_id,contact_id) DO UPDATE SET allocation_id=EXCLUDED.allocation_id,reason=EXCLUDED.reason,source='allocation',removed_by_user_id=EXCLUDED.removed_by_user_id,removed_at=NOW(),restored_at=NULL`, d.PoolID, d.OrganizationID, contactID, id, reason, a.UserID); err != nil {
						return err
					}
				} else {
					if _, err := tx.Exec(`UPDATE org_pool_allocation_members SET status='active',removed_reason='',removed_at=NULL,updated_at=NOW() WHERE allocation_id=$1 AND contact_id=$2`, id, contactID); err != nil {
						return err
					}
					if _, err := tx.Exec(`UPDATE org_pool_allocation_exclusions SET restored_at=NOW() WHERE pool_id=$1 AND organization_id=$2 AND contact_id=$3 AND restored_at IS NULL`, d.PoolID, d.OrganizationID, contactID); err != nil {
						return err
					}
				}
			}
		}
		return c.recordAllocationTx(tx, a, d, "members_"+action, map[string]any{"count": len(seen), "reason": reason})
	})
}

type AllocationEvent struct {
	ID         int64       `db:"id" json:"id"`
	OccurredAt time.Time   `db:"occurred_at" json:"occurred_at"`
	Action     string      `db:"action" json:"action"`
	Actor      string      `db:"actor" json:"actor"`
	Metadata   models.JSON `db:"metadata" json:"metadata"`
}

func (c *Core) AllocationEvents(a AllocationAccess, id int64, offset, limit int) ([]AllocationEvent, int, error) {
	d, err := c.GetAllocationDetail(a, id)
	if err != nil {
		return nil, 0, err
	}
	if !a.Audit {
		return nil, 0, echo.NewHTTPError(http.StatusForbidden, "permission denied: audit:get")
	}
	where := ` FROM audit_events e LEFT JOIN users u ON u.id=e.actor_user_id WHERE e.organization_id=$1 AND
	((e.object_type='org_pool_allocation' AND e.object_id=$2) OR
	(e.action='pool.organization_grant_changed' AND e.object_id=$3))`
	args := []any{d.OrganizationID, strconv.FormatInt(id, 10), strconv.Itoa(d.PoolID)}
	var total int
	if err := c.db.Get(&total, `SELECT COUNT(*)`+where, args...); err != nil {
		return nil, 0, err
	}
	rows := []AllocationEvent{}
	err = c.db.Select(&rows, `SELECT e.id,e.occurred_at,e.action,COALESCE(u.name,u.username,'') AS actor,e.metadata`+where+` ORDER BY e.id DESC LIMIT $4 OFFSET $5`, append(args, limit, offset)...)
	return rows, total, err
}
