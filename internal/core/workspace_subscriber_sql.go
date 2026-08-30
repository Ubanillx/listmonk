package core

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// QueryWorkspaceSubscribersWithSQL preserves the advanced subscriber-query
// feature while keeping the result set inside the active workspace. The raw
// expression is only one parenthesized boolean condition; it cannot close the
// fixed workspace predicate or add another statement.
func (c *Core) QueryWorkspaceSubscribersWithSQL(access models.WorkspaceAccess, search, queryExp string, listIDs []int, subscriptionStatus, order, orderBy string, offset, limit int) (models.Subscribers, int, error) {
	if listIDs == nil {
		listIDs = []int{}
	}
	condition, err := workspaceSubscriberSQLCondition(queryExp)
	if err != nil {
		return nil, 0, err
	}

	fields := map[string]string{
		"email":      "subscribers.email",
		"status":     "subscribers.status",
		"name":       "subscribers.name",
		"created_at": "subscribers.created_at",
		"updated_at": "subscribers.updated_at",
	}
	if _, ok := fields[orderBy]; !ok {
		orderBy = "created_at"
	}

	scope, args := workspaceSubscriberReadPredicate(access, "subscribers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT subscribers.*, COALESCE(u.username, '') AS owner_username,
			COALESCE(u.name, '') AS owner_name, COUNT(*) OVER() AS total
		FROM subscribers
		LEFT JOIN users u ON u.id = COALESCE(subscribers.owner_user_id, subscribers.original_owner_user_id)
		WHERE (%s)
			AND (%s)
			AND ($%d = '' OR subscribers.name ~* $%d OR subscribers.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = subscribers.id AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM subscribers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM subscribers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))
		ORDER BY %s OFFSET $%d LIMIT (CASE WHEN $%d < 1 THEN NULL ELSE $%d END)`,
		scope, condition,
		first, first, first,
		first+1, first+1, first+2, first+2,
		workspaceSort(orderBy, order, fields, "subscribers.created_at"), first+3, first+4, first+4)
	args = append(args, strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus, offset, limit)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, args...); err != nil {
		return nil, 0, rawWorkspaceSubscriberQueryError(err)
	}

	var out models.Subscribers
	if err := c.db.Select(&out, stmt, args...); err != nil {
		return nil, 0, workspaceQueryError("fetching subscribers", err)
	}
	if err := c.loadWorkspaceSubscriberLists(access, out); err != nil {
		return nil, 0, workspaceQueryError("fetching subscriber lists", err)
	}
	total := 0
	if len(out) > 0 {
		total = out[0].Total
	}
	return out, total, nil
}

// GetWorkspaceSubscriberIDsWithSQL is used by bulk actions. Callers still
// intersect the result with mutable resources before changing rows, which is
// a second owner-boundary check for destructive operations.
func (c *Core) GetWorkspaceSubscriberIDsWithSQL(access models.WorkspaceAccess, search, queryExp string, listIDs []int, subscriptionStatus string) ([]int, error) {
	if listIDs == nil {
		listIDs = []int{}
	}
	condition, err := workspaceSubscriberSQLCondition(queryExp)
	if err != nil {
		return nil, err
	}

	scope, args := workspaceSubscriberReadPredicate(access, "subscribers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT subscribers.id
		FROM subscribers
		WHERE (%s)
			AND (%s)
			AND ($%d = '' OR subscribers.name ~* $%d OR subscribers.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = subscribers.id AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM subscribers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM subscribers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))`,
		scope, condition,
		first, first, first,
		first+1, first+1, first+2, first+2)
	args = append(args, strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, args...); err != nil {
		return nil, rawWorkspaceSubscriberQueryError(err)
	}

	var ids []int
	if err := c.db.Select(&ids, stmt, args...); err != nil {
		return nil, workspaceQueryError("selecting subscribers", err)
	}
	return ids, nil
}

// ExportWorkspaceSubscribersWithSQL keeps raw filtering inside the same fixed
// workspace predicate used by normal exports. The requested IDs are supplied
// by the handler after it has checked ownership.
func (c *Core) ExportWorkspaceSubscribersWithSQL(access models.WorkspaceAccess, search, queryExp string, listIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.SubscriberExport, error), error) {
	return c.exportWorkspaceSubscribers(access, search, queryExp, listIDs, requestedIDs, subscriptionStatus, batchSize)
}

func (c *Core) exportWorkspaceSubscribers(access models.WorkspaceAccess, search, queryExp string, listIDs, requestedIDs []int, subscriptionStatus string, batchSize int) (func() ([]models.SubscriberExport, error), error) {
	if batchSize < 1 {
		batchSize = 1000
	}
	if listIDs == nil {
		listIDs = []int{}
	}
	if requestedIDs == nil {
		requestedIDs = []int{-1}
	}
	condition, err := workspaceSubscriberSQLCondition(queryExp)
	if err != nil {
		return nil, err
	}

	// Keep the public table name stable in every raw-query operation. Existing
	// advanced expressions often qualify fields as subscribers.email, and an
	// internal alias would otherwise make exports behave differently from list
	// and bulk-query operations. Exports deliberately use the immutable owner
	// boundary: organization-manager inspection access must not serialize a
	// member's recipient identities when this helper is called directly.
	scope, args := workspaceSensitiveSubscriberPredicate(access, "subscribers", 1)
	first := len(args) + 1
	stmt := fmt.Sprintf(`
		SELECT subscribers.id, subscribers.uuid, subscribers.email, subscribers.name, subscribers.attribs,
			subscribers.status, subscribers.created_at, subscribers.updated_at
		FROM subscribers
		WHERE (%s) AND (%s) AND subscribers.id > $%d
			AND subscribers.id = ANY($%d::INT[])
			AND ($%d = '' OR subscribers.name ~* $%d OR subscribers.email ~* $%d)
			AND (CARDINALITY($%d::INT[]) = 0 OR EXISTS (
				SELECT 1 FROM subscriber_lists sl
				JOIN lists l ON l.id = sl.list_id
				WHERE sl.subscriber_id = subscribers.id
					AND sl.list_id = ANY($%d::INT[])
					AND l.organization_id IS NOT DISTINCT FROM subscribers.organization_id
					AND l.owner_user_id IS NOT DISTINCT FROM subscribers.owner_user_id
					AND l.transfer_pending_at IS NULL
					AND ($%d = '' OR sl.status = $%d::subscription_status)
			))
		ORDER BY subscribers.id ASC LIMIT $%d`,
		scope, condition,
		first,
		first+1,
		first+2, first+2, first+2,
		first+3, first+3, first+4, first+4,
		first+5)
	baseArgs := append([]any{}, args...)
	baseArgs = append(baseArgs, 0, pq.Array(requestedIDs), strings.TrimSpace(search), pq.Array(listIDs), subscriptionStatus, batchSize)
	if err := validateQueryTablesWithArgs(c.db, stmt, allowedSubQueryTables, baseArgs...); err != nil {
		return nil, rawWorkspaceSubscriberQueryError(err)
	}

	lastID := 0
	return func() ([]models.SubscriberExport, error) {
		callArgs := append([]any{}, baseArgs...)
		callArgs[len(args)] = lastID
		var out []models.SubscriberExport
		if err := c.db.Select(&out, stmt, callArgs...); err != nil {
			return nil, workspaceQueryError("exporting subscribers", err)
		}
		if len(out) > 0 {
			lastID = out[len(out)-1].ID
		}
		return out, nil
	}, nil
}

func rawWorkspaceSubscriberQueryError(err error) error {
	return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid subscriber SQL expression: %s", err))
}

func workspaceSubscriberSQLCondition(queryExp string) (string, error) {
	queryExp = strings.TrimSpace(queryExp)
	if queryExp == "" {
		return "TRUE", nil
	}
	if err := validateWorkspaceSubscriberSQLExpression(queryExp); err != nil {
		return "", rawWorkspaceSubscriberQueryError(err)
	}
	return "(" + queryExp + ")", nil
}

// validateWorkspaceSubscriberSQLExpression rejects syntax that could escape
// the expression wrapper around a caller-provided condition. Parentheses must
// balance independently, comments and statement separators are forbidden, and
// positional parameters cannot collide with the server-owned placeholders.
func validateWorkspaceSubscriberSQLExpression(query string) error {
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
