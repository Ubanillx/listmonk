package core

import (
	"strings"
	"testing"
)

// TestCheckCustomerSQLLexical covers rule 1 (credential columns) and rule 3
// (blocking / amplifying constructs) of the advanced customer SQL boundary. The
// lexical pass runs on the caller's raw expression, so it also catches names
// that the planner could fold away.
func TestCheckCustomerSQLLexical(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		// users.password, however it is spelled.
		{
			name:    "bare column",
			expr:    `password LIKE 'a%'`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "alias qualified column",
			expr:    `u.password LIKE 'a%'`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "doubly quoted identifier",
			expr:    `"password" LIKE 'a%'`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "function argument",
			expr:    `lower(u.password) LIKE 'a%'`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "subquery without alias",
			expr:    `EXISTS(SELECT 1 FROM users WHERE username = 'admin' AND password LIKE 'a%')`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "subquery with alias",
			expr:    `EXISTS(SELECT 1 FROM users u2 WHERE u2.username = 'admin' AND u2.password LIKE 'a%')`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "cast function argument",
			expr:    `EXISTS(SELECT 1 FROM users u2 WHERE substr(u2.password::text, 1, 1) = 'a')`,
			wantErr: "column 'password' is not allowed",
		},
		{
			name:    "two factor seed",
			expr:    `u.twofa_key IS NOT NULL`,
			wantErr: "column 'twofa_key' is not allowed",
		},
		{
			name:    "integration token hash",
			expr:    `EXISTS(SELECT 1 FROM integration_tokens it WHERE it.token_hash = 'x')`,
			wantErr: "column 'token_hash' is not allowed",
		},

		// Rule 3: blocking, stateful, filesystem and statistics constructs.
		{
			name:    "pg_sleep",
			expr:    `pg_sleep(30) IS NOT NULL`,
			wantErr: "function 'pg_sleep' is not allowed",
		},
		{
			name:    "pg_sleep in subquery",
			expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id AND pg_sleep(30) IS NOT NULL)`,
			wantErr: "function 'pg_sleep' is not allowed",
		},
		{
			name:    "advisory lock",
			expr:    `pg_advisory_lock(1) IS NOT NULL`,
			wantErr: "function 'pg_advisory_lock' is not allowed",
		},
		{
			name:    "filesystem read",
			expr:    `pg_read_file('/etc/passwd') IS NOT NULL`,
			wantErr: "function 'pg_read_file' is not allowed",
		},
		{
			name:    "large object import",
			expr:    `lo_import('/etc/passwd') IS NOT NULL`,
			wantErr: "function 'lo_import' is not allowed",
		},
		{
			name:    "dblink family",
			expr:    `dblink('host=remote', 'select 1') IS NOT NULL`,
			wantErr: "function 'dblink' is not allowed",
		},
		{
			name:    "session state mutation",
			expr:    `set_config('search_path', 'public', false) IS NOT NULL`,
			wantErr: "function 'set_config' is not allowed",
		},
		{
			name:    "row count oracle",
			expr:    `pg_stat_get_live_tuples('users'::regclass) > 0`,
			wantErr: "function 'pg_stat_get_live_tuples' is not allowed",
		},
		{
			name:    "relation size oracle",
			expr:    `pg_total_relation_size('users') > 0`,
			wantErr: "function 'pg_total_relation_size' is not allowed",
		},
		{
			name:    "string amplifier",
			expr:    `length(repeat('a', 100000000)) > 0`,
			wantErr: "function 'repeat' is not allowed",
		},
		{
			name:    "row locking clause",
			expr:    `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id FOR UPDATE)`,
			wantErr: "row locking clauses are not allowed",
		},

		// Everything below is a legitimate expression and must stay accepted.
		{
			name: "json key inside a literal is not a column reference",
			expr: `customers.attribs->>'password' = 'x'`,
		},
		{
			name: "function name inside a literal",
			expr: `customers.name = 'pg_sleep'`,
		},
		{
			name: "plain scalar over the outer row",
			expr: `customers.email ILIKE '%@example.com' AND customers.status = 'enabled'`,
		},
		{
			name: "owner column",
			expr: `u.username = 'alice'`,
		},
		{
			name: "correlated activity subquery",
			expr: `EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkCustomerSQLLexical(tokenizeCustomerSQL(test.expr))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("checkCustomerSQLLexical(%q) = %v, want no error", test.expr, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkCustomerSQLLexical(%q) = nil, want error containing %q", test.expr, test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("checkCustomerSQLLexical(%q) = %q, want error containing %q", test.expr, err.Error(), test.wantErr)
			}
		})
	}
}

// TestTokenizeCustomerSQL documents the tokenizer contract the boundary rules
// depend on: literals are skipped, quoted identifiers are kept, and unquoted
// identifiers are folded the way PostgreSQL folds them.
func TestTokenizeCustomerSQL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "literals are skipped",
			in:   `name = 'Password; FROM users'`,
			want: []string{"name", "="},
		},
		{
			name: "quoted identifier is kept",
			in:   `"Password" = 'x'`,
			want: []string{"password", "="},
		},
		{
			name: "escaped quote inside literal",
			in:   `name = 'Ada''s password'`,
			want: []string{"name", "="},
		},
		{
			name: "dollar quoted literal is skipped",
			in:   `name = $tag$password$tag$`,
			want: []string{"name", "="},
		},
		{
			name: "comments are skipped",
			in:   "name = 'x' -- password\n/* password */ AND status = 'y'",
			want: []string{"name", "=", "and", "status", "="},
		},
		{
			name: "qualified reference",
			in:   `u.password LIKE 'a%'`,
			want: []string{"u", ".", "password", "like"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			for _, tok := range tokenizeCustomerSQL(test.in) {
				if tok.kind == customerSQLTokenIdent || tok.text == "=" || tok.text == "." {
					got = append(got, tok.text)
				}
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("tokenizeCustomerSQL(%q) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

// TestAnalyzeCustomerSQLExpressionPlan covers rules 1-3 on the rendered query
// plan. The fixtures are trimmed renderings of real
// `EXPLAIN (VERBOSE, FORMAT JSON)` output; TestCustomerSQLGuardAgainstPostgres
// runs the same cases against a live server so the fixtures cannot drift away
// from what PostgreSQL actually prints.
func TestAnalyzeCustomerSQLExpressionPlan(t *testing.T) {
	tests := []struct {
		name    string
		plan    string
		wantErr string
	}{
		{
			name: "cross workspace user oracle",
			// EXISTS(SELECT 1 FROM users u2 WHERE u2.username = 'x' AND u2.password LIKE 'a%'):
			// the caller adds a second users access next to the server's owner join.
			plan: `[{"Plan":{"Node Type":"Result","One-Time Filter":"$0","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"InitPlan","Subplan Name":"InitPlan 1 (returns $0)",
				 "Relation Name":"users","Alias":"u2","Filter":"((u2.password ~~ 'a%'::text) AND (u2.username = 'x'::text))"},
				{"Node Type":"Hash Join","Parent Relationship":"Outer","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
					{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
						{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}]}}]`,
			wantErr: "table 'users' may only be referenced through the workspace join",
		},
		{
			name: "second customers access (self join)",
			plan: `[{"Plan":{"Node Type":"Nested Loop","Join Type":"Semi","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"customers","Alias":"c2","Filter":"(c2.email ~~ 'a%'::text)"},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
			wantErr: "table 'customers' may only be referenced through the workspace join",
		},
		{
			name: "owner credential column read from the join alias",
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Output":["1"],"Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u","Filter":"(u.password ~~ 'a%'::text)"}]}]}}]`,
			wantErr: "column 'users.password' is not allowed",
		},
		{
			name: "whole row reference hides credential columns",
			// to_jsonb(u) ->> 'password' LIKE 'a%': the column name only appears in
			// a string literal, the row itself is what the expression reads.
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u","Filter":"((to_jsonb(u.*) ->> 'password'::text) ~~ 'a%'::text)"}]}]}}]`,
			wantErr: "whole row reference 'users.*' is not allowed",
		},
		{
			name: "unlisted table",
			plan: `[{"Plan":{"Node Type":"Result","One-Time Filter":"$0","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"InitPlan","Subplan Name":"InitPlan 1 (returns $0)","Relation Name":"templates","Alias":"t"},
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
			wantErr: "table 'templates' is not allowed",
		},
		{
			name: "workspace scoped table outside the expression allowlist",
			plan: `[{"Plan":{"Node Type":"Result","One-Time Filter":"$0","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"InitPlan","Subplan Name":"InitPlan 1 (returns $0)","Relation Name":"campaign_customer_lists","Alias":"ccl"},
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
			wantErr: "table 'campaign_customer_lists' is not allowed in an advanced customer expression",
		},
		{
			name: "uncorrelated subquery over an activity table",
			// EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.campaign_id = 42)
			plan: `[{"Plan":{"Node Type":"Result","One-Time Filter":"$0","Plans":[
				{"Node Type":"Index Only Scan","Parent Relationship":"InitPlan","Subplan Name":"InitPlan 1 (returns $0)",
				 "Relation Name":"campaign_views","Alias":"cv","Index Cond":"(cv.campaign_id = 42)"},
				{"Node Type":"Hash Join","Parent Relationship":"Outer","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
					{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
						{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}]}}]`,
			wantErr: "must be correlated with the caller's workspace",
		},
		{
			name: "boolean inference through a disjunction",
			// EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id OR cv.campaign_id = 42):
			// the correlation only appears inside the OR, so the relation is not restricted.
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
				{"Node Type":"Nested Loop","Parent Relationship":"Outer","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"Inner","Relation Name":"campaign_views","Alias":"cv",
					 "Recheck Cond":"((cv.customer_id = customers.id) OR (cv.campaign_id = 42))","Plans":[
						{"Node Type":"BitmapOr","Parent Relationship":"Outer","Plans":[
							{"Node Type":"Bitmap Index Scan","Parent Relationship":"Member","Index Cond":"(cv.customer_id = customers.id)"},
							{"Node Type":"Bitmap Index Scan","Parent Relationship":"Member","Index Cond":"(cv.campaign_id = 42)"}]}]}]},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}}]`,
			wantErr: "must be correlated with the caller's workspace",
		},
		{
			name: "negated correlation is not a correlation",
			// EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id <> customers.id AND cv.campaign_id = 42)
			plan: `[{"Plan":{"Node Type":"Nested Loop","Join Type":"Semi","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Materialize","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"Outer","Relation Name":"campaign_views","Alias":"cv",
					 "Recheck Cond":"(cv.campaign_id = 42)"}]},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}],
				"Join Filter":"(cv.customer_id <> customers.id)"}}]`,
			wantErr: "must be correlated with the caller's workspace",
		},
		{
			name: "hashed subplan is a global set",
			// customers.id NOT IN (SELECT customer_id FROM link_clicks WHERE link_id = 3)
			plan: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"customers","Alias":"customers","Filter":"(NOT (hashed SubPlan 1))","Plans":[
				{"Node Type":"Bitmap Heap Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 1","Relation Name":"link_clicks","Alias":"link_clicks",
				 "Recheck Cond":"(link_clicks.link_id = 3)","Output":["link_clicks.customer_id"]}],
				"Output":["1"]}}]`,
			wantErr: "must be correlated with the caller's workspace",
		},
		{
			name: "set returning function",
			plan: `[{"Plan":{"Node Type":"Result","One-Time Filter":"$0","Plans":[
				{"Node Type":"Function Scan","Parent Relationship":"InitPlan","Subplan Name":"InitPlan 1 (returns $0)","Function Name":"generate_series","Alias":"g"},
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
			wantErr: "set returning functions are not allowed",
		},
		{
			name: "row locking",
			plan: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"customers","Alias":"customers","Filter":"(SubPlan 1)","Plans":[
				{"Node Type":"LockRows","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 1","Plans":[
					{"Node Type":"Index Scan","Parent Relationship":"Outer","Relation Name":"campaign_views","Alias":"cv","Index Cond":"(cv.customer_id = customers.id)"}]}]}}]`,
			wantErr: "row locking clauses are not allowed",
		},

		// Legitimate workspace scoped plans.
		{
			name: "correlated semijoin over campaign views",
			// EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id)
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
				{"Node Type":"Nested Loop","Join Type":"Semi","Parent Relationship":"Outer","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
					{"Node Type":"Index Only Scan","Parent Relationship":"Inner","Relation Name":"campaign_views","Alias":"cv","Index Cond":"(cv.customer_id = customers.id)"}]},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}}]`,
		},
		{
			name: "correlated subplan with an inner disjunction",
			// EXISTS(SELECT 1 FROM campaign_views cv WHERE cv.customer_id = customers.id AND (cv.campaign_id = 1 OR cv.campaign_id = 2))
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(customers.id = cv.customer_id)","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"Outer","Relation Name":"campaign_views","Alias":"cv",
					 "Recheck Cond":"((cv.campaign_id = 1) OR (cv.campaign_id = 2))","Plans":[
						{"Node Type":"BitmapOr","Parent Relationship":"Outer","Plans":[
							{"Node Type":"Bitmap Index Scan","Parent Relationship":"Member","Index Cond":"(cv.campaign_id = 1)"},
							{"Node Type":"Bitmap Index Scan","Parent Relationship":"Member","Index Cond":"(cv.campaign_id = 2)"}]}]}]},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
		},
		{
			name: "correlated scalar subplan",
			// (SELECT count(*) FROM bounces b WHERE b.customer_id = customers.id) > 3
			plan: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"customers","Alias":"customers","Filter":"((SubPlan 1) > 3)","Plans":[
				{"Node Type":"Aggregate","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 1","Output":["count(*)"],"Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"bounces","Alias":"b","Filter":"(b.customer_id = customers.id)"}]}]}}]`,
		},
		{
			name: "correlated membership subquery",
			// EXISTS(SELECT 1 FROM customer_list_memberships m WHERE m.customer_id = customers.id AND m.status = 'unsubscribed')
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(customers.id = m.customer_id)","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers"},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"Outer","Relation Name":"customer_list_memberships","Alias":"m","Recheck Cond":"(m.status = 'unsubscribed'::subscription_status)"}]},
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u"}]}}]`,
		},
		{
			name: "scalar expression without any subquery",
			plan: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"customers","Alias":"customers","Filter":"((customers.email ~~* '%@example.com'::text) AND (customers.status = 'enabled'::customer_status))","Output":["1"],"Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Inner","Relation Name":"users","Alias":"u","Filter":"(u.username = 'alice'::text)"}]}}]`,
		},
		{
			name: "disjunction of correlated sublinks with hashed alternatives",
			// EXISTS(... cv correlated ...) OR EXISTS(... b correlated ...):
			// PostgreSQL plans each sublink twice and the hashed twin carries no
			// correlation predicate of its own.
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers",
				 "Filter":"((alternatives: SubPlan 1 or hashed SubPlan 2) OR (alternatives: SubPlan 3 or hashed SubPlan 4))","Plans":[
					{"Node Type":"Index Only Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 1","Relation Name":"campaign_views","Alias":"cv","Index Cond":"(cv.customer_id = customers.id)"},
					{"Node Type":"Seq Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 2","Relation Name":"campaign_views","Alias":"cv_1"},
					{"Node Type":"Seq Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 3","Relation Name":"bounces","Alias":"b","Filter":"((b.customer_id = customers.id) AND (b.type = 'hard'::bounce_type))"},
					{"Node Type":"Seq Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 4","Relation Name":"bounces","Alias":"b_1","Filter":"(b_1.type = 'hard'::bounce_type)"}]},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}}]`,
		},
		{
			name: "correlated decoy next to an uncorrelated sublink",
			// The alternatives group ends at its own parenthesis: the uncorrelated
			// bounces sublink that follows it is not laundered by the correlated
			// campaign_views alternative.
			plan: `[{"Plan":{"Node Type":"Hash Join","Hash Cond":"(COALESCE(customers.owner_user_id, customers.original_owner_user_id) = u.id)","Plans":[
				{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"customers","Alias":"customers",
				 "Filter":"((alternatives: SubPlan 1 or hashed SubPlan 2) OR (SubPlan 3))","Plans":[
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 1","Relation Name":"campaign_views","Alias":"cv","Recheck Cond":"((cv.customer_id = customers.id) AND (cv.campaign_id = 42))"},
					{"Node Type":"Bitmap Heap Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 2","Relation Name":"campaign_views","Alias":"cv_1","Recheck Cond":"(cv_1.campaign_id = 42)"},
					{"Node Type":"Seq Scan","Parent Relationship":"SubPlan","Subplan Name":"SubPlan 3","Relation Name":"bounces","Alias":"b","Filter":"((b.customer_id = customers.id) OR (b.campaign_id = 43))"}]},
				{"Node Type":"Hash","Parent Relationship":"Inner","Plans":[
					{"Node Type":"Seq Scan","Parent Relationship":"Outer","Relation Name":"users","Alias":"u"}]}]}}]`,
			wantErr: "must be correlated with the caller's workspace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := analyzeCustomerSQLExpressionPlan(test.plan)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("analyzeCustomerSQLExpressionPlan() = %v, want no error", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("analyzeCustomerSQLExpressionPlan() = nil, want error containing %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("analyzeCustomerSQLExpressionPlan() = %q, want error containing %q", err.Error(), test.wantErr)
			}
		})
	}
}

// TestCustomerSQLExpressionProbe pins the probe to the outer scope of the
// production statement: the same FROM/JOIN and the same owner join condition.
func TestCustomerSQLExpressionProbe(t *testing.T) {
	probe := customerSQLExpressionProbe(`customers.status = 'enabled'`)
	for _, want := range []string{
		"FROM customers",
		"LEFT JOIN users u ON u.id = COALESCE(customers.owner_user_id, customers.original_owner_user_id)",
		"WHERE (customers.status = 'enabled')",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe %q does not contain %q", probe, want)
		}
	}
}
