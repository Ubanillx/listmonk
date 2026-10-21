package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// ProviderFailureMatch is one customer or pool contact that a failed provider
// address resolves to inside the caller's workspace. It deliberately carries no
// e-mail address: the provider report already owns that value and callers must
// not be able to use a match to read an address they could not otherwise read.
type ProviderFailureMatch struct {
	Kind         string `json:"kind"`
	ID           int64  `json:"id"`
	PoolID       int64  `json:"pool_id,omitempty"`
	PoolName     string `json:"pool_name,omitempty"`
	CustomerCode string `json:"customer_code"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Blocked      bool   `json:"blocked"`
}

type providerFailureMatchRow struct {
	Kind         string `db:"kind"`
	ID           int64  `db:"id"`
	PoolID       int64  `db:"pool_id"`
	PoolName     string `db:"pool_name"`
	CustomerCode string `db:"customer_code"`
	Name         string `db:"name"`
	Status       string `db:"status"`
	Email        string `db:"email"`
}

func normalizeProviderEmails(emails []string) []string {
	out := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

// MatchProviderFailureEmails resolves failed provider addresses to the private
// customers and pool contacts the caller may see in the active workspace. Only
// owner-scoped customers and organization-scoped pool allocations are matched,
// so the result never reveals another workspace's identifiers. Pool master
// holders and platform administrators see the first-level pools they already
// administer.
func (c *Core) MatchProviderFailureEmails(access models.WorkspaceAccess, emails []string) (map[string][]ProviderFailureMatch, error) {
	out := make(map[string][]ProviderFailureMatch, len(emails))
	normalized := normalizeProviderEmails(emails)
	if len(normalized) == 0 {
		return out, nil
	}

	// Private customers: the owner boundary keeps the match actionable, because
	// blocklisting is only allowed on rows the caller owns.
	scope, args := workspaceOwnerScopedReadPredicate(access, "s", 2)
	args = append([]any{pq.Array(normalized)}, args...)
	rows := make([]providerFailureMatchRow, 0)
	query := fmt.Sprintf(`
		SELECT 'private' AS kind, s.id, 0 AS pool_id, '' AS pool_name,
			s.customer_code, s.name, s.status, LOWER(s.email) AS email
		FROM customers s
		WHERE (%s) AND LOWER(s.email) = ANY($1::TEXT[])
		ORDER BY s.id`, scope)
	if err := c.db.Select(&rows, query, args...); err != nil {
		return nil, workspaceQueryError("matching failed addresses", err)
	}

	poolRows, err := c.matchProviderPoolContacts(access, normalized)
	if err != nil {
		return nil, err
	}
	rows = append(rows, poolRows...)

	seen := make(map[string]map[string]struct{}, len(rows))
	for _, row := range rows {
		key := row.Kind + ":" + fmt.Sprint(row.ID)
		if seen[row.Email] == nil {
			seen[row.Email] = make(map[string]struct{})
		}
		if _, ok := seen[row.Email][key]; ok {
			continue
		}
		seen[row.Email][key] = struct{}{}
		out[row.Email] = append(out[row.Email], ProviderFailureMatch{
			Kind:         row.Kind,
			ID:           row.ID,
			PoolID:       row.PoolID,
			PoolName:     row.PoolName,
			CustomerCode: row.CustomerCode,
			Name:         row.Name,
			Status:       row.Status,
			Blocked:      row.Status == models.CustomerStatusBlockListed,
		})
	}
	return out, nil
}

// matchProviderPoolContacts returns the first-level pool contacts visible to the
// caller. A contact that belongs to several visible pools is returned once per
// pool so the UI can name the pool it will affect.
func (c *Core) matchProviderPoolContacts(access models.WorkspaceAccess, emails []string) ([]providerFailureMatchRow, error) {
	var (
		rows  = make([]providerFailureMatchRow, 0)
		query string
		args  []any
	)
	if access.PlatformAdmin || access.PoolMaster {
		query = `
			SELECT 'pool' AS kind, pc.id, pm.pool_id, cl.name AS pool_name,
				pc.customer_code, pc.name, pc.status, LOWER(pc.email) AS email
			FROM pool_contacts pc
			JOIN pool_members pm ON pm.contact_id = pc.id
			JOIN customer_lists cl ON cl.id = pm.pool_id AND cl.type = 'pool'
			WHERE LOWER(pc.email) = ANY($1::TEXT[])
			ORDER BY pc.id, pm.pool_id`
		args = []any{pq.Array(emails)}
	} else if access.IsOrganization() {
		query = `
			SELECT 'pool' AS kind, pc.id, pm.pool_id, cl.name AS pool_name,
				pc.customer_code, pc.name, pc.status, LOWER(pc.email) AS email
			FROM pool_contacts pc
			JOIN pool_members pm ON pm.contact_id = pc.id
			JOIN customer_lists cl ON cl.id = pm.pool_id AND cl.type = 'pool'
			JOIN org_pool_allocations ps ON ps.pool_id = pm.pool_id AND ps.organization_id = $2
			JOIN org_pool_allocation_members psm ON psm.allocation_id = ps.id
				AND psm.contact_id = pc.id AND psm.status = 'active'
			WHERE LOWER(pc.email) = ANY($1::TEXT[])
			ORDER BY pc.id, pm.pool_id`
		args = []any{pq.Array(emails), access.OrganizationID}
	} else {
		return rows, nil
	}
	if err := c.db.Select(&rows, query, args...); err != nil {
		return nil, workspaceQueryError("matching pool contacts", err)
	}
	return rows, nil
}

// SetPoolContactsBlocklist flips the contact-level blocklist flag shared by every
// organization allocation of the given pool contacts. Archived contacts keep
// their archived status so a cleared address is never re-activated by this
// action.
func (c *Core) SetPoolContactsBlocklist(contactIDs []int64, blocked bool) (int64, error) {
	ids := make([]int64, 0, len(contactIDs))
	seen := make(map[int64]struct{}, len(contactIDs))
	for _, id := range contactIDs {
		if id < 1 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	status := "active"
	if blocked {
		status = models.CustomerStatusBlockListed
	}
	result, err := c.db.Exec(`
		UPDATE pool_contacts
		SET status = $2, updated_at = NOW()
		WHERE id = ANY($1::BIGINT[]) AND status <> 'archived'`,
		pq.Array(ids), status)
	if err != nil {
		return 0, workspaceQueryError("updating pool contact blocklist", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, workspaceQueryError("updating pool contact blocklist", err)
	}
	return n, nil
}

// SetCustomersBlocklistInWorkspace flips private customers between blocklisted
// and enabled inside the caller's workspace. Removing a blocklist entry only
// restores the customer status; list memberships stay as they were, matching the
// existing customer form which never silently re-subscribes an address.
func (c *Core) SetCustomersBlocklistInWorkspace(access models.WorkspaceAccess, subIDs []int, blocked bool) error {
	subIDs = uniqueMutationIDs(subIDs)
	if len(subIDs) == 0 {
		return nil
	}
	if blocked {
		return c.BlocklistCustomersInWorkspace(access, subIDs)
	}
	return c.withWorkspaceResourceMutation(access, resourceCustomers, subIDs, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`UPDATE customers SET status = $2, updated_at = NOW() WHERE id = ANY($1::INT[])`,
			pq.Array(subIDs), models.CustomerStatusEnabled); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError,
				c.i18n.Ts("customers.errorBlocklisting", "error", pqErrMsg(err)))
		}
		return nil
	})
}
