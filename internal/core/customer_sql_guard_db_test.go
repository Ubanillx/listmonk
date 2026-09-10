package core

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// TestCustomerSQLGuardAgainstPostgres runs the advanced customer SQL boundary
// against a real PostgreSQL server, which is the only way to validate the
// rendered query plans the guard analyzes. Like the other database backed tests
// in this package it is gated on an environment variable, e.g.
//
//	CUSTOMER_SQL_GUARD_TEST_DSN='postgres://user:pass@localhost:5432/listmonk?sslmode=disable' go test ./internal/core/ -run CustomerSQLGuard
//
// The test only issues EXPLAIN and SELECT statements; it never mutates rows.
func TestCustomerSQLGuardAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_SQL_GUARD_TEST_DSN")
	if dsn == "" {
		t.Skip("CUSTOMER_SQL_GUARD_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	core := &Core{db: db}
	access := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}}

	t.Run("expression boundary", func(t *testing.T) {
		tests := []struct {
			name    string
			expr    string
			wantErr string
		}{
			// The reported defect: a boolean oracle over the global users table
			// recovers the plaintext legacy password token character by character.
			{
				name:    "cross workspace credential recovery",
				expr:    `EXISTS(SELECT 1 FROM users u2 WHERE u2.username = 'admin' AND u2.password LIKE 'a%')`,
				wantErr: "column 'password' is not allowed",
			},
			{
				name:    "owner credential read through the workspace join alias",
				expr:    `u.password LIKE 'a%'`,
				wantErr: "column 'password' is not allowed",
			},
			{
				name:    "credential read in a scalar subquery",
				expr:    `(SELECT u2.password FROM users u2 WHERE u2.id = customers.owner_user_id) IS NOT NULL`,
				wantErr: "column 'password' is not allowed",
			},
			{
				name:    "credential read inside a function argument",
				expr:    `EXISTS(SELECT 1 FROM users u2 WHERE lower(u2.password) LIKE 'a%')`,
				wantErr: "column 'password' is not allowed",
			},
			{
				name:    "two factor seed",
				expr:    `EXISTS(SELECT 1 FROM users u2 WHERE u2.twofa_key IS NOT NULL)`,
				wantErr: "column 'twofa_key' is not allowed",
			},
			{
				name:    "whole row serialization of the owner join",
				expr:    `to_jsonb(u) ->> 'password' LIKE 'a%'`,
				wantErr: "whole row reference 'users.*' is not allowed",
			},
			{
				name:    "whole row serialization of an activity table",
				expr:    `EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id AND to_jsonb(b) ->> 'source' = 'x')`,
				wantErr: "whole row reference 'bounces.*' is not allowed",
			},
			{
				name:    "unicode escaped credential identifier",
				expr:    `U&"pass\0077ord" LIKE 'a%'`,
				wantErr: "column 'users.password' is not allowed",
			},
			{
				name:    "flattened user email oracle",
				expr:    `customers.email IN (SELECT email FROM users)`,
				wantErr: "table 'users' may only be referenced through the workspace join",
			},
			{
				name:    "user existence oracle",
				expr:    `EXISTS(SELECT 1 FROM users)`,
				wantErr: "table 'users' may only be referenced through the workspace join",
			},
			{
				name:    "campaign metadata oracle",
				expr:    `EXISTS(SELECT 1 FROM campaigns c WHERE c.name LIKE 'a%')`,
				wantErr: "table 'campaigns' is not allowed in an advanced customer expression",
			},
			{
				name:    "link url oracle",
				expr:    `EXISTS(SELECT 1 FROM links l WHERE l.url LIKE '%reset-password%')`,
				wantErr: "table 'links' is not allowed in an advanced customer expression",
			},
			{
				name:    "customer_list name oracle",
				expr:    `customers.name = ANY(SELECT cl.name FROM customer_lists cl)`,
				wantErr: "table 'customer_lists' is not allowed in an advanced customer expression",
			},
			{
				name:    "cross workspace customer enumeration",
				expr:    `EXISTS(SELECT 1 FROM customers c2 WHERE c2.email LIKE 'a%')`,
				wantErr: "table 'customers' may only be referenced through the workspace join",
			},
			{
				name:    "unlisted table",
				expr:    `EXISTS(SELECT 1 FROM templates t)`,
				wantErr: "table 'templates' is not allowed",
			},
			{
				name:    "uncorrelated campaign view oracle",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.campaign_id = 42)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "disjunction oracle over the caller's activity table",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id OR cv.campaign_id = 42)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "negated correlation is not a correlation",
				expr:    `EXISTS(SELECT 1 FROM bounces b WHERE NOT (b.customer_id = customers.id) AND b.campaign_id = 42)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "hashed subplan over a global set",
				expr:    `customers.id NOT IN (SELECT customer_id FROM link_clicks WHERE link_id = 3)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "cartesian scan of two activity tables",
				expr:    `EXISTS(SELECT 1 FROM bounces b, link_clicks lc WHERE b.customer_id = customers.id AND b.campaign_id = 42)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "correlated sublink does not launder an uncorrelated sibling",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id AND cv.campaign_id = 42) OR EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id OR b.campaign_id = 43)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "correlated decoy next to an uncorrelated table",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id) OR EXISTS(SELECT 1 FROM campaign_views cv2 WHERE cv2.campaign_id = 42)`,
				wantErr: "must be correlated with the caller's workspace",
			},
			{
				name:    "sleep amplifier",
				expr:    `pg_sleep(30) IS NOT NULL`,
				wantErr: "function 'pg_sleep' is not allowed",
			},
			{
				name:    "set returning amplifier",
				expr:    `EXISTS(SELECT 1 FROM generate_series(1, 100000000) g)`,
				wantErr: "set returning functions are not allowed",
			},
			{
				name:    "row locking",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id FOR UPDATE)`,
				wantErr: "row locking clauses are not allowed",
			},

			// Legitimate advanced expressions must keep working: the caller's own
			// customers filtered by their own campaign, link and bounce activity.
			{
				name: "activity existence",
				expr: `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id)`,
			},
			{
				name: "activity with an inner disjunction",
				expr: `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id AND (cv.campaign_id = 1 OR cv.campaign_id = 2))`,
			},
			{
				name: "recent activity",
				expr: `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id AND cv.created_at > NOW() - INTERVAL '30 days')`,
			},
			{
				name: "link click activity",
				expr: `EXISTS(SELECT 1 FROM link_clicks lc WHERE lc.customer_id = customers.id AND lc.campaign_id = 42)`,
			},
			{
				name: "hard bounce exclusion",
				expr: `NOT EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id AND b.type = 'hard')`,
			},
			{
				name: "membership state",
				expr: `EXISTS(SELECT 1 FROM customer_list_memberships m WHERE m.customer_id = customers.id AND m.status = 'unsubscribed')`,
			},
			{
				name: "scalar activity count",
				expr: `(SELECT count(*) FROM bounces b WHERE b.customer_id = customers.id) > 3`,
			},
			{
				name: "nested scalar subquery",
				expr: `EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id AND b.campaign_id = (SELECT cv.campaign_id FROM campaign_views cv WHERE cv.customer_id = customers.id LIMIT 1))`,
			},
			{
				name: "disjunction of correlated sublinks",
				expr: `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id) OR EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id AND b.type = 'hard')`,
			},
			{
				name: "owner filter",
				expr: `u.username = 'admin'`,
			},
			{
				name: "attribute filter",
				expr: `customers.attribs->>'city' = 'Bengaluru'`,
			},
			{
				name: "scalar filter",
				expr: `customers.email ILIKE '%@example.com' AND customers.status = 'enabled'`,
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				err := validateCustomerSQLExpressionBoundary(db, test.expr)
				if test.wantErr == "" {
					if err != nil {
						t.Fatalf("validateCustomerSQLExpressionBoundary(%q) = %v, want no error", test.expr, err)
					}
					return
				}
				if err == nil {
					t.Fatalf("validateCustomerSQLExpressionBoundary(%q) = nil, want error containing %q", test.expr, test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("validateCustomerSQLExpressionBoundary(%q) = %q, want error containing %q", test.expr, err.Error(), test.wantErr)
				}
			})
		}
	})

	// The pre-existing statement level protection must stay intact: the allowlist
	// still rejects unlisted tables through validateQueryTablesWithArgs, with the
	// message the API returns today.
	t.Run("statement allowlist is unchanged", func(t *testing.T) {
		unlisted := `SELECT customers.id FROM customers WHERE (EXISTS(SELECT 1 FROM templates t))`
		err := validateQueryTablesWithArgs(db, unlisted, allowedSubQueryTables)
		if err == nil || !strings.Contains(err.Error(), "table 'templates' is not allowed") {
			t.Fatalf("validateQueryTablesWithArgs(unlisted) = %v, want table 'templates' is not allowed", err)
		}

		allowed := `SELECT customers.id FROM customers WHERE (EXISTS(
			SELECT 1 FROM customer_list_memberships m WHERE m.customer_id = customers.id))`
		if err := validateQueryTablesWithArgs(db, allowed, allowedSubQueryTables); err != nil {
			t.Fatalf("validateQueryTablesWithArgs(allowed) = %v, want no error", err)
		}
	})

	t.Run("feature still runs legitimate queries", func(t *testing.T) {
		const expr = `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id)
			OR EXISTS(SELECT 1 FROM bounces b WHERE b.customer_id = customers.id AND b.type = 'hard')`

		if _, _, err := core.QueryWorkspaceCustomersWithSQL(access, "", expr, nil, "", "", "", 0, 5); err != nil {
			t.Fatalf("QueryWorkspaceCustomersWithSQL() = %v, want no error", err)
		}
		if _, err := core.GetWorkspaceCustomerIDsWithSQL(access, "", expr, nil, ""); err != nil {
			t.Fatalf("GetWorkspaceCustomerIDsWithSQL() = %v, want no error", err)
		}
		batch, err := core.ExportWorkspaceCustomersWithSQL(access, "", expr, nil, []int{-1}, "", 10)
		if err != nil {
			t.Fatalf("ExportWorkspaceCustomersWithSQL() = %v, want no error", err)
		}
		if _, err := batch(); err != nil {
			t.Fatalf("export batch = %v, want no error", err)
		}
	})

	t.Run("API error contract", func(t *testing.T) {
		tests := []struct {
			name    string
			expr    string
			message string
		}{
			{
				name:    "credential oracle",
				expr:    `EXISTS(SELECT 1 FROM users WHERE username = 'admin' AND password LIKE 'a%')`,
				message: "invalid customer SQL expression: column 'password' is not allowed in an advanced customer expression",
			},
			{
				name:    "unlisted table",
				expr:    `EXISTS(SELECT 1 FROM templates t)`,
				message: "invalid customer SQL expression: table 'templates' is not allowed",
			},
			{
				name:    "cross workspace inference",
				expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.campaign_id = 42)`,
				message: "invalid customer SQL expression: subquery over table 'campaign_views' must be correlated with the caller's workspace",
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				_, _, err := core.QueryWorkspaceCustomersWithSQL(access, "", test.expr, nil, "", "", "", 0, 1)
				var httpErr *echo.HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("QueryWorkspaceCustomersWithSQL(%q) = %v, want an *echo.HTTPError", test.expr, err)
				}
				if httpErr.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d", httpErr.Code, http.StatusBadRequest)
				}
				if got := fmt.Sprint(httpErr.Message); !strings.HasPrefix(got, test.message) {
					t.Fatalf("message = %q, want prefix %q", got, test.message)
				}
			})
		}
	})
}
