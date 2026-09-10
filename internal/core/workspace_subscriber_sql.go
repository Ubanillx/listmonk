package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// QueryWorkspaceCustomersWithSQL preserves the advanced customer-query
// feature while keeping the result set inside the active workspace. The raw
// expression is only one parenthesized boolean condition; it cannot close the
// fixed workspace predicate or add another statement.
func (c *Core) QueryWorkspaceCustomersWithSQL(access models.WorkspaceAccess, search, queryExp string, customerListIDs []int, subscriptionStatus, order, orderBy string, offset, limit int) (models.Customers, int, error) {
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	condition, err := c.workspaceCustomerSQLCondition(queryExp)
	if err != nil {
		return nil, 0, err
	}

	fields := map[string]string{
		"email":      "customers.email",
		"status":     "customers.status",
		"name":       "customers.name",
		"created_at": "customers.created_at",
		"updated_at": "customers.updated_at",
	}
	if _, ok := fields[orderBy]; !ok {
		orderBy = "created_at"
	}

	scope, args := workspaceCustomerReadPredicate(access, "customers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT customers.*, COALESCE(u.username, '') AS owner_username,
			COALESCE(u.name, '') AS owner_name, COUNT(*) OVER() AS total
		FROM customers
		LEFT JOIN users u ON u.id = COALESCE(customers.owner_user_id, customers.original_owner_user_id)
		WHERE (%s)
			AND (%s)
			AND ($%d = '' OR customers.name ~* $%d OR customers.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM customer_list_memberships sl
				JOIN customer_lists l ON l.id = sl.customer_list_id
				WHERE sl.customer_id = customers.id AND sl.customer_list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM customers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM customers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		scope, condition,
		first, first, first,
		first+1, first+1, first+2, first+2,
		workspaceSort(orderBy, order, fields, "customers.created_at"), first+3, first+4, first+4)
	args = append(args, strings.TrimSpace(search), pq.Array(customerListIDs), subscriptionStatus, offset, limit)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, args...); err != nil {
		return nil, 0, rawWorkspaceCustomerQueryError(err)
	}

	var out models.Customers
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching customers", err)
	}
	if err := c.loadWorkspaceCustomerListMemberships(access, out); err != nil {
		return nil, 0, workspaceQueryError("fetching customer customer_lists", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceCustomerIDsWithSQL is used by bulk actions. Callers still
// intersect the result with mutable resources before changing rows, which is
// a second owner-boundary check for destructive operations.
func (c *Core) GetWorkspaceCustomerIDsWithSQL(access models.WorkspaceAccess, search, queryExp string, customerListIDs []int, subscriptionStatus string) ([]int, error) {
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	condition, err := c.workspaceCustomerSQLCondition(queryExp)
	if err != nil {
		return nil, err
	}

	scope, args := workspaceCustomerReadPredicate(access, "customers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT customers.id
		FROM customers
		WHERE (%s)
			AND (%s)
			AND ($%d = '' OR customers.name ~* $%d OR customers.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM customer_list_memberships sl
				JOIN customer_lists l ON l.id = sl.customer_list_id
				WHERE sl.customer_id = customers.id AND sl.customer_list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM customers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM customers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))`,
		scope, condition,
		first, first, first,
		first+1, first+1, first+2, first+2)
	args = append(args, strings.TrimSpace(search), pq.Array(customerListIDs), subscriptionStatus)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, args...); err != nil {
		return nil, rawWorkspaceCustomerQueryError(err)
	}

	var ids []int
	if err := c.db.Select(&ids, stmt, args...); err != nil {
		return nil, workspaceQueryError("selecting customers", err)
	}
	return ids, nil
}

// ExportWorkspaceCustomersWithSQL keeps raw filtering inside the same fixed
// workspace predicate used by normal exports. The requested IDs are supplied
// by the handler after it has checked ownership.
func (c *Core) ExportWorkspaceCustomersWithSQL(access models.WorkspaceAccess, search, queryExp string, customerListIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.CustomerExport, error), error) {
	return c.exportWorkspaceCustomers(access, search, queryExp, customerListIDs, requestedIDs, subscriptionStatus, batchSize)
}

func (c *Core) exportWorkspaceCustomers(access models.WorkspaceAccess, search, queryExp string, customerListIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.CustomerExport, error), error) {
	if batchSize < 1 {
		batchSize = 1000
	}
	if customerListIDs == nil {
		customerListIDs = []int{}
	}
	if requestedIDs == nil {
		requestedIDs = []int{-1}
	}
	condition, err := c.workspaceCustomerSQLCondition(queryExp)
	if err != nil {
		return nil, err
	}

	// Keep the public table name stable in every raw-query operation. Existing
	// advanced expressions often qualify fields as customers.email, and an
	// internal alias would otherwise make exports behave differently from customer_list
	// and bulk-query operations. Exports deliberately use the immutable owner
	// boundary: organization-manager inspection access must not serialize a
	// member's recipient identities when this helper is called directly.
	scope, args := workspaceSensitiveCustomerPredicate(access, "customers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT customers.id, customers.uuid, customers.email, customers.name, customers.attribs,
			customers.status, customers.customer_code, customers.created_at, customers.updated_at
		FROM customers
		WHERE (%s) AND (%s) AND customers.id > $%d
			AND customers.id = ANY($%d::INT[])
			AND ($%d = '' OR customers.name ~* $%d OR customers.email ~* $%d)
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
		scope, condition,
		first,
		first+1,
		first+2, first+2, first+2,
		first+3, first+3, first+4, first+4,
		first+5)
	baseArgs := append([]any{}, args...)
	baseArgs = append(baseArgs, 0, pq.Array(requestedIDs), strings.TrimSpace(search), pq.Array(customerListIDs), subscriptionStatus, batchSize)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, baseArgs...); err != nil {
		return nil, rawWorkspaceCustomerQueryError(err)
	}

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

func rawWorkspaceCustomerQueryError(err error) error {
	return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid customer SQL expression: %s", err))
}

// workspaceCustomerSQLCondition turns a caller supplied boolean expression into
// the parenthesized condition that is spliced into the outer workspace query.
// Every caller expression is validated by the boundary guard in
// customer_sql_guard.go before it reaches the statement.
func (c *Core) workspaceCustomerSQLCondition(queryExp string) (string, error) {
	queryExp = strings.TrimSpace(queryExp)
	if queryExp == "" {
		return "TRUE", nil
	}
	if err := validateWorkspaceCustomerSQLExpression(queryExp); err != nil {
		return "", rawWorkspaceCustomerQueryError(err)
	}
	if err := validateCustomerSQLExpressionBoundary(c.db, queryExp); err != nil {
		return "", rawWorkspaceCustomerQueryError(err)
	}
	return "(" + queryExp + ")", nil
}

// validateWorkspaceCustomerSQLExpression rejects syntax that could escape
// the expression wrapper around a caller-provided condition. Parentheses must
// balance independently, comments and statement separators are forbidden, and
// positional parameters cannot collide with the server-owned placeholders.
func validateWorkspaceCustomerSQLExpression(query string) error {
	var (
		parenDepth int
		quote      byte
		dollarTag  string
	)
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if dollarTag != "" {
			if strings.HasPrefix(query[i:], dollarTag) {
				i += len(dollarTag) - 1
				dollarTag = ""
			}
			continue
		}
		if quote != 0 {
			if ch != quote {
				continue
			}
			if i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			quote = 0
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			parenDepth++
		case ')':
			parenDepth--
			if parenDepth < 0 {
				return fmt.Errorf("unbalanced parentheses")
			}
		case ';':
			return fmt.Errorf("statement separators are not allowed")
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				return fmt.Errorf("SQL comments are not allowed")
			}
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				return fmt.Errorf("SQL comments are not allowed")
			}
		case '*':
			if i+1 < len(query) && query[i+1] == '/' {
				return fmt.Errorf("SQL comments are not allowed")
			}
		case '$':
			if i+1 < len(query) && query[i+1] >= '0' && query[i+1] <= '9' {
				return fmt.Errorf("positional parameters are not allowed")
			}
			end := i + 1
			for end < len(query) && ((query[end] >= 'a' && query[end] <= 'z') ||
				(query[end] >= 'A' && query[end] <= 'Z') ||
				(query[end] >= '0' && query[end] <= '9') || query[end] == '_') {
				end++
			}
			if end < len(query) && query[end] == '$' {
				dollarTag = query[i : end+1]
				i = end
			}
		}
	}
	if quote != 0 || dollarTag != "" {
		return fmt.Errorf("unterminated SQL literal")
	}
	if parenDepth != 0 {
		return fmt.Errorf("unbalanced parentheses")
	}
	return nil
}
