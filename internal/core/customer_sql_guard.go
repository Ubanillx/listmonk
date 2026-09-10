package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Advanced customer SQL boundary.
//
// The advanced customer query feature splices a caller supplied boolean
// expression into the WHERE clause of a workspace scoped statement
// (internal/core/workspace_subscriber_sql.go). The statement level allowlist
// (allowedSubQueryTables) and its EXPLAIN relation check only prove that every
// relation in the final statement is an allowlisted table. They prove nothing
// about the workspace boundary: a subquery that is not correlated with the
// outer customer row evaluates identically for every row of the result set, so
// the row count becomes a boolean oracle over a global table, e.g.
//
//	EXISTS (SELECT 1 FROM users WHERE username = 'admin' AND password LIKE 'a%')
//
// In this fork users.password holds a plaintext legacy token, so that oracle
// recovers credentials one character at a time. The rules below close it while
// keeping the documented purpose of the feature (filtering the caller's own
// customers by their campaign views, link clicks, bounces and customer_list
// memberships) working:
//
//  1. Credential columns (users.password, users.twofa_key, ...) are rejected
//     wherever they appear in the expression: as a bare column, qualified with
//     an alias, passed to a function or read inside a subquery.
//  2. The expression allowlist for subqueries is narrowed to tables that carry
//     a customer_id foreign key. A reference to one of those tables is only
//     accepted when the query plan shows its customer_id compared for equality
//     with the outer row, which makes every subquery row scoped instead of
//     global. All other relations (users, campaigns, links, customer_lists,
//     campaign_customer_lists, a second customers access, ...) are rejected.
//  3. Constructs that turn the endpoint into a resource amplifier or a
//     filesystem/statistics probe (pg_sleep, advisory locks, FOR UPDATE,
//     set returning functions, ...) are rejected.
//  4. Whole row references (to_jsonb(u) ->> 'password') are rejected, because
//     they read every column of a relation without naming any of them.
//
// The expression is validated by PostgreSQL itself: the caller's expression is
// EXPLAINed inside the exact outer scope the production statement uses, and the
// rendered plan (which resolves aliases, function arguments and subqueries) is
// analyzed. The statement level allowlist check in validateQueryTablesWithArgs
// still runs afterwards on the final statement and keeps its existing
// behaviour and error message.

// customerSQLServerRelations maps the relations the server itself puts in scope
// for the caller expression to the alias the server gives them (: the outer
// customer row and the owner join). The probe below always plans exactly one
// access to each, under exactly these aliases, so a second access, or an access
// under another alias, can only come from the caller's expression.
var customerSQLServerRelations = map[string]string{
	"customers": "customers",
	"users":     "u",
}

// customerSQLExpressionTables maps a relation the expression may reference to
// the column that ties one of its rows to the caller's own customer. A
// reference is accepted only when the plan shows that column compared for
// equality with the outer customer row.
var customerSQLExpressionTables = map[string]string{
	"campaign_views":            "customer_id",
	"link_clicks":               "customer_id",
	"bounces":                   "customer_id",
	"customer_list_memberships": "customer_id",
}

// customerSQLForbiddenColumns are credential or secret columns. The names are
// rejected whatever their qualifier is, because no allowlisted table other than
// users owns such a column and users must not be referenced by the expression.
// users.password is a plaintext legacy token in this fork and users.twofa_key is
// a TOTP shared secret.
var customerSQLForbiddenColumns = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"pwd":           {},
	"password_hash": {},
	"twofa_key":     {},
	"otp_secret":    {},
	"secret":        {},
	"client_secret": {},
	"api_key":       {},
	"apikey":        {},
	"token":         {},
	"token_hash":    {},
	"access_token":  {},
	"refresh_token": {},
	"session_token": {},
	"private_key":   {},
}

// customerSQLForbiddenFunctions are functions that block the connection, mutate
// session state, read the filesystem, or answer questions about data the caller
// must not see (row counts and sizes of global tables). None of them is needed
// to filter customers by their own activity.
var customerSQLForbiddenFunctions = map[string]struct{}{
	// Blocking / DoS.
	"pg_sleep":                     {},
	"pg_sleep_for":                 {},
	"pg_sleep_until":               {},
	"pg_advisory_lock":             {},
	"pg_advisory_xact_lock":        {},
	"pg_advisory_lock_shared":      {},
	"pg_advisory_xact_lock_shared": {},
	"pg_try_advisory_lock":         {},
	"pg_try_advisory_xact_lock":    {},
	"repeat":                       {},
	"set_config":                   {},
	"pg_notify":                    {},
	"pg_terminate_backend":         {},
	"pg_cancel_backend":            {},
	"pg_reload_conf":               {},
	"pg_rotate_logfile":            {},
	"pg_switch_wal":                {},
	"pg_logical_emit_message":      {},
	// Filesystem and large object access.
	"pg_read_file":        {},
	"pg_read_binary_file": {},
	"pg_ls_dir":           {},
	"pg_stat_file":        {},
	"lo_import":           {},
	"lo_export":           {},
	// Statistics / size oracles over global relations.
	"pg_relation_size":       {},
	"pg_total_relation_size": {},
	"pg_table_size":          {},
	"pg_indexes_size":        {},
	"pg_database_size":       {},
	"current_setting":        {},
}

// customerSQLForbiddenFunctionPrefixes rejects whole families of the functions
// above (dblink*, pg_stat_get_*, pg_ls_*).
var customerSQLForbiddenFunctionPrefixes = []string{
	"dblink",
	"pg_advisory",
	"pg_try_advisory",
	"pg_stat_get",
	"pg_ls_",
}

// customerSQLPlanExprKeys are the EXPLAIN (VERBOSE, FORMAT JSON) fields that
// hold a rendered expression. "Output" is handled separately because leaf scan
// output lists project whole rows and would produce false positives.
var customerSQLPlanExprKeys = []string{
	"Filter", "Index Cond", "Recheck Cond", "Hash Cond", "Merge Cond",
	"Join Filter", "One-Time Filter", "Index Recheck", "Group Key",
	"Sort Key", "Cache Key", "Tid Cond", "Presorted Key",
}

// customerSQLRejectedNodeTypes are plan node types that only appear for
// constructs the expression must not use.
var customerSQLRejectedNodeTypes = map[string]string{
	"LockRows":        "row locking clauses are not allowed in an advanced customer expression",
	"Recursive Union": "recursive queries are not allowed in an advanced customer expression",
	"WorkTable Scan":  "recursive queries are not allowed in an advanced customer expression",
	"ModifyTable":     "data modification is not allowed in an advanced customer expression",
	"Function Scan":   "set returning functions are not allowed in an advanced customer expression",
}

// validateCustomerSQLExpressionBoundary enforces rules 1-4 above for a caller
// supplied advanced customer expression. It runs before the statement is
// assembled, keeps the caller's error surface (a wrapped 400) and never weakens
// the existing checks.
func validateCustomerSQLExpressionBoundary(db *sqlx.DB, expr string) error {
	tokens := tokenizeCustomerSQL(expr)
	if err := checkCustomerSQLLexical(tokens); err != nil {
		return err
	}

	// The plan pass is required for every expression, not only for ones with a
	// subquery: it is what resolves aliases, function arguments and whole row
	// references (to_jsonb(u) ->> 'password') to real columns.
	plan, err := explainCustomerSQLExpression(db, expr)
	if err != nil {
		return err
	}
	return analyzeCustomerSQLExpressionPlan(plan)
}

// customerSQLExpressionProbe mirrors the outer scope of the production
// statement (workspace_subscriber_sql.go) so that name resolution, and therefore
// every error and every rendered expression, matches what the caller's
// expression sees at execution time. The owner columns are selected so that the
// baseline owner join is always part of the plan: that keeps the baseline
// relation accesses identifiable by alias even when the expression does not
// reference the owner.
func customerSQLExpressionProbe(expr string) string {
	return `SELECT COALESCE(u.username, ''), COALESCE(u.name, ''), 1 FROM customers
		LEFT JOIN users u ON u.id = COALESCE(customers.owner_user_id, customers.original_owner_user_id)
		WHERE (` + expr + `)`
}

// explainCustomerSQLExpression plans the caller expression in a read-only
// transaction, exactly like the statement level pre-validation does. EXPLAIN
// does not execute the expression.
func explainCustomerSQLExpression(db *sqlx.DB, expr string) (string, error) {
	tx, err := db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var plan string
	if err = tx.QueryRow("EXPLAIN (VERBOSE, FORMAT JSON) " + customerSQLExpressionProbe(expr)).Scan(&plan); err != nil {
		return "", err
	}
	return plan, nil
}

// checkCustomerSQLLexical rejects forbidden identifiers and constructs in the
// caller's raw expression. It is deliberately fail-closed: it also catches
// spells of a forbidden name that the planner could fold away, and it runs even
// when the expression has no subquery.
func checkCustomerSQLLexical(tokens customerSQLTokens) error {
	for i, tok := range tokens {
		if tok.kind != customerSQLTokenIdent {
			continue
		}

		if _, ok := customerSQLForbiddenColumns[tok.text]; ok {
			return fmt.Errorf("column '%s' is not allowed in an advanced customer expression", tok.text)
		}

		if _, ok := customerSQLForbiddenFunctions[tok.text]; ok {
			return fmt.Errorf("function '%s' is not allowed in an advanced customer expression", tok.text)
		}
		for _, prefix := range customerSQLForbiddenFunctionPrefixes {
			if strings.HasPrefix(tok.text, prefix) {
				return fmt.Errorf("function '%s' is not allowed in an advanced customer expression", tok.text)
			}
		}

		switch tok.text {
		case "for":
			// FOR UPDATE / FOR NO KEY UPDATE / FOR SHARE / FOR KEY SHARE.
			if i+1 < len(tokens) {
				switch tokens[i+1].text {
				case "update", "share", "no", "key":
					return fmt.Errorf("row locking clauses are not allowed in an advanced customer expression")
				}
			}
		case "into":
			if i > 0 && tokens[i-1].kind == customerSQLTokenIdent && tokens[i-1].text == "select" {
				return fmt.Errorf("SELECT INTO is not allowed in an advanced customer expression")
			}
		}
	}

	return nil
}

// analyzeCustomerSQLExpressionPlan applies the boundary rules to the rendered
// plan of the caller's expression.
func analyzeCustomerSQLExpressionPlan(planJSON string) error {
	var plans []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &plans); err != nil {
		return fmt.Errorf("error parsing the customer SQL query plan: %v", err)
	}
	if len(plans) == 0 {
		return fmt.Errorf("error parsing the customer SQL query plan: empty plan")
	}

	raw, _ := plans[0]["Plan"].(map[string]any)
	if raw == nil {
		return fmt.Errorf("error parsing the customer SQL query plan: missing plan")
	}
	root := buildCustomerSQLPlanNode(raw)
	nodes := root.flatten()

	// Node level rejections (locking, recursion, set returning functions).
	for _, n := range nodes {
		if msg, ok := customerSQLRejectedNodeTypes[n.nodeType]; ok {
			return fmt.Errorf("%s", msg)
		}
		// A relationless function scan is the only way a table function
		// (generate_series, ...) can enter the expression; the lexical check
		// already rejects the dangerous ones, this rejects the rest.
		if n.function != "" {
			return fmt.Errorf("function '%s' is not allowed in an advanced customer expression", n.function)
		}
	}

	// Relation level checks.
	accesses := make([]*customerSQLPlanNode, 0, len(nodes))
	counts := map[string]int{}
	aliasRelation := map[string]string{}
	outerAliases := map[string]struct{}{}
	for _, n := range nodes {
		if n.relation == "" {
			continue
		}
		accesses = append(accesses, n)
		counts[n.relation]++
		if n.alias != "" {
			aliasRelation[n.alias] = n.relation
		}
	}

	for _, n := range accesses {
		if _, ok := allowedSubQueryTables[n.relation]; !ok {
			// Same message and status code the statement level allowlist check
			// returns today.
			return fmt.Errorf("table '%s' is not allowed", n.relation)
		}

		if baseAlias, ok := customerSQLServerRelations[n.relation]; ok {
			if n.alias != baseAlias || counts[n.relation] > 1 {
				return fmt.Errorf("table '%s' may only be referenced through the workspace join, not from an advanced customer expression", n.relation)
			}
			outerAliases[n.alias] = struct{}{}
			continue
		}

		if _, ok := customerSQLExpressionTables[n.relation]; !ok {
			return fmt.Errorf("table '%s' is not allowed in an advanced customer expression: only the caller's own customer activity tables (%s) may be referenced",
				n.relation, strings.Join(customerSQLExpressionTableNames(), ", "))
		}
	}

	// Credential columns, resolved against the plan's alias to relation map.
	for _, n := range nodes {
		for _, e := range n.credentialExprs() {
			if err := checkCustomerSQLPlanExprForColumns(tokenizeCustomerSQL(e), aliasRelation); err != nil {
				return err
			}
		}
	}

	// Correlation: every customer activity table in the expression must be
	// restricted by the outer customer row, otherwise its rows are a global set
	// and its truth value leaks information about other workspaces.
	exempt := customerSQLAlternativesExemptions(nodes, outerAliases)
	for _, n := range accesses {
		col, ok := customerSQLExpressionTables[n.relation]
		if !ok || n.alias == "" || exempt[n] {
			continue
		}
		if !customerSQLAccessIsCorrelated(n, col, outerAliases) {
			return fmt.Errorf("subquery over table '%s' must be correlated with the caller's workspace: expected %s.%s to be compared with the outer customer row",
				n.relation, n.alias, col)
		}
	}

	return nil
}

// customerSQLAlternativesExemptions handles the way PostgreSQL renders a sublink
// it may evaluate either per row or once: "(alternatives: SubPlan 1 or hashed
// SubPlan 2)". The hashed twin of a correlated sublink has no correlation
// predicate of its own because the executor hashes the correlated column and
// probes it with the outer value, so it is exactly as row scoped as its sibling.
// An access inside such a group is therefore exempt from the correlation
// requirement when at least one access in the same group is correlated; a group
// without a correlated member (a genuinely uncorrelated subquery) stays subject
// to it.
func customerSQLAlternativesExemptions(nodes []*customerSQLPlanNode, outerAliases map[string]struct{}) map[*customerSQLPlanNode]bool {
	exempt := map[*customerSQLPlanNode]bool{}

	for _, n := range nodes {
		for _, expr := range n.conditions {
			keys := customerSQLAlternatives(expr)
			if len(keys) == 0 {
				continue
			}

			group := make([]*customerSQLPlanNode, 0, len(keys))
			for _, sub := range nodes {
				if sub.subplanKey == "" || !containsString(keys, sub.subplanKey) {
					continue
				}
				for _, inner := range sub.flatten() {
					if _, ok := customerSQLExpressionTables[inner.relation]; ok && inner.alias != "" {
						group = append(group, inner)
					}
				}
			}

			correlated := false
			for _, a := range group {
				col := customerSQLExpressionTables[a.relation]
				if customerSQLAccessIsCorrelated(a, col, outerAliases) {
					correlated = true
					break
				}
			}
			if !correlated {
				continue
			}
			for _, a := range group {
				exempt[a] = true
			}
		}
	}

	return exempt
}

// customerSQLAlternatives extracts the subplan keys of the group introduced by
// one "alternatives:" rendering, e.g. "(alternatives: SubPlan 1 or hashed
// SubPlan 2)". Only the subplans inside that group are returned: a different
// sublink rendered next to it ("... OR (SubPlan 3)") must be validated on its
// own, otherwise a correlated decoy would launder an uncorrelated neighbour.
func customerSQLAlternatives(expr string) []string {
	tokens := tokenizeCustomerSQL(expr)

	var out []string
	for i, tok := range tokens {
		if tok.kind != customerSQLTokenIdent || tok.text != "alternatives" {
			continue
		}
		// The group ends at the parenthesis closing the one that holds
		// "alternatives".
		for j := i + 1; j < len(tokens); j++ {
			cur := tokens[j]
			if cur.text == ")" && cur.depth < tok.depth {
				break
			}
			if cur.kind != customerSQLTokenIdent || (cur.text != "subplan" && cur.text != "initplan") {
				continue
			}
			if j+1 < len(tokens) && tokens[j+1].kind == customerSQLTokenNumber {
				out = append(out, cur.text+tokens[j+1].text)
			}
		}
	}
	return out
}

// customerSQLSubplanKey normalizes a "Subplan Name" into the key used by
// customerSQLAlternatives: "SubPlan 1" and "InitPlan 1 (returns $0)" become
// "subplan1" and "initplan1".
func customerSQLSubplanKey(name string) string {
	tokens := tokenizeCustomerSQL(name)
	if len(tokens) < 2 || tokens[0].kind != customerSQLTokenIdent || tokens[1].kind != customerSQLTokenNumber {
		return ""
	}
	return tokens[0].text + tokens[1].text
}

func customerSQLExpressionTableNames() []string {
	out := make([]string, 0, len(customerSQLExpressionTables))
	for name := range customerSQLExpressionTables {
		out = append(out, name)
	}
	return out
}

// customerSQLAccessIsCorrelated reports whether the plan restricts the rows of
// the given relation access with the outer customer row. A qualifying condition
// is an equality conjunct that mentions both the accessing alias' ownership
// column and one of the outer aliases. It must be attached to a node whose
// subtree contains that access only, and it must not live under a BitmapOr /
// BitmapAnd node, because those subtrees are alternatives (OR) or combinations
// (AND) of index conditions whose rendered form does not prove that the
// correlation is a conjunct of the relation's restriction.
func customerSQLAccessIsCorrelated(access *customerSQLPlanNode, col string, outerAliases map[string]struct{}) bool {
	if len(outerAliases) == 0 {
		return false
	}
	for _, n := range access.root().flatten() {
		if n.inBitmapBranch {
			continue
		}
		if !n.subtreeContains(access) || n.countAccessesWithAlias(access.alias) != 1 {
			continue
		}
		for _, e := range n.conditionExprs() {
			for _, conjunct := range topLevelConjuncts(tokenizeCustomerSQL(e)) {
				if customerSQLConjunctCorrelates(conjunct, access.alias, col, outerAliases) {
					return true
				}
			}
		}
	}
	return false
}

// customerSQLConjunctCorrelates checks a single conjunct token stream, which
// topLevelConjuncts has already stripped and rebased.
func customerSQLConjunctCorrelates(conjunct customerSQLTokens, alias, col string, outerAliases map[string]struct{}) bool {
	body := stripWrappingParens(conjunct)
	if hasTopLevelKeyword(body, "or") || hasTopLevelKeyword(body, "not") {
		// A disjunction (or a negation) does not restrict the relation to the
		// outer row.
		return false
	}

	var equals bool
	for _, tok := range body {
		if tok.kind == customerSQLTokenOp && tok.text == "=" {
			equals = true
			break
		}
	}
	if !equals {
		return false
	}

	if !referencesQualifiedColumn(body, alias, col) {
		return false
	}
	for outer := range outerAliases {
		if referencesQualifier(body, outer) {
			return true
		}
	}
	return false
}

// checkCustomerSQLPlanExprForColumns rejects credential columns that the plan
// resolved to a relation, so the error names the table (users.password), and
// whole row references, which serialize every column of a relation without
// naming any of them (to_jsonb(u) ->> 'password' renders as
// "(to_jsonb(u.*) ->> 'password'::text)").
func checkCustomerSQLPlanExprForColumns(tokens customerSQLTokens, aliasRelation map[string]string) error {
	for i, tok := range tokens {
		if tok.kind != customerSQLTokenIdent {
			continue
		}

		// Whole row reference: alias.*
		if i+2 < len(tokens) && tokens[i+1].text == "." && tokens[i+2].text == "*" {
			relation := tok.text
			if rel, ok := aliasRelation[tok.text]; ok {
				relation = rel
			}
			return fmt.Errorf("whole row reference '%s.*' is not allowed in an advanced customer expression", relation)
		}

		if _, ok := customerSQLForbiddenColumns[tok.text]; !ok {
			continue
		}
		if i >= 2 && tokens[i-1].text == "." && tokens[i-2].kind == customerSQLTokenIdent {
			if rel, ok := aliasRelation[tokens[i-2].text]; ok {
				return fmt.Errorf("column '%s.%s' is not allowed in an advanced customer expression", rel, tok.text)
			}
			return fmt.Errorf("column '%s.%s' is not allowed in an advanced customer expression", tokens[i-2].text, tok.text)
		}
		return fmt.Errorf("column '%s' is not allowed in an advanced customer expression", tok.text)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

type customerSQLTokenKind int

const (
	customerSQLTokenIdent customerSQLTokenKind = iota
	customerSQLTokenOp
	customerSQLTokenPunct
	customerSQLTokenNumber
)

type customerSQLToken struct {
	text  string
	kind  customerSQLTokenKind
	depth int
}

type customerSQLTokens []customerSQLToken

// tokenizeCustomerSQL splits SQL text into lower-cased identifiers (including
// the content of quoted identifiers), operators and punctuation, skipping
// string literals, dollar quoted strings and comments. Identifier case folding
// mirrors PostgreSQL: unquoted identifiers are lower-cased, quoted identifiers
// keep their content, which is what makes "password" detectable too.
func tokenizeCustomerSQL(text string) customerSQLTokens {
	var (
		out       customerSQLTokens
		depth     int
		i         int
		escapes   bool // E'' string: backslash escapes are honoured
		prevIdent byte
	)

	for i < len(text) {
		ch := text[i]

		// Whitespace.
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}

		// Comments (defensive; the expression validator rejects them).
		if ch == '-' && i+1 < len(text) && text[i+1] == '-' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(text) && text[i+1] == '*' {
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}

		// String literal.
		if ch == '\'' || ((ch == 'e' || ch == 'E') && i+1 < len(text) && text[i+1] == '\'' && !isIdentByte(prevIdent)) {
			if ch != '\'' {
				escapes = true
				i++
			}
			i++
			for i < len(text) {
				if escapes && text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '\'' {
					if i+1 < len(text) && text[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			escapes, prevIdent = false, 0
			continue
		}

		// Dollar quoted string.
		if ch == '$' {
			if end, tag, ok := readDollarTag(text, i); ok {
				if close := strings.Index(text[end:], tag); close >= 0 {
					i = end + close + len(tag)
					prevIdent = 0
					continue
				}
			}
		}

		// Quoted identifier.
		if ch == '"' {
			var sb strings.Builder
			i++
			for i < len(text) {
				if text[i] == '"' {
					if i+1 < len(text) && text[i+1] == '"' {
						sb.WriteByte('"')
						i += 2
						continue
					}
					i++
					break
				}
				sb.WriteByte(text[i])
				i++
			}
			out = append(out, customerSQLToken{text: strings.ToLower(sb.String()), kind: customerSQLTokenIdent, depth: depth})
			prevIdent = 'a'
			continue
		}

		// Identifier or keyword.
		if isIdentStart(ch) {
			start := i
			for i < len(text) && isIdentByte(text[i]) {
				i++
			}
			out = append(out, customerSQLToken{text: strings.ToLower(text[start:i]), kind: customerSQLTokenIdent, depth: depth})
			prevIdent = 'a'
			continue
		}

		// Number.
		if ch >= '0' && ch <= '9' {
			start := i
			for i < len(text) && ((text[i] >= '0' && text[i] <= '9') || text[i] == '.') {
				if text[i] == '.' && i+1 < len(text) && text[i+1] == '.' {
					break
				}
				i++
			}
			out = append(out, customerSQLToken{text: text[start:i], kind: customerSQLTokenNumber, depth: depth})
			prevIdent = 0
			continue
		}

		// Multi character operators.
		if i+1 < len(text) {
			two := text[i : i+2]
			switch two {
			case "::", "<=", ">=", "<>", "!=", "||", "->", "#>", "@>", "<@", "~~":
				out = append(out, customerSQLToken{text: two, kind: customerSQLTokenOp, depth: depth})
				i += 2
				prevIdent = 0
				continue
			}
		}

		switch ch {
		case '(', '[':
			out = append(out, customerSQLToken{text: string(ch), kind: customerSQLTokenPunct, depth: depth})
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
			out = append(out, customerSQLToken{text: string(ch), kind: customerSQLTokenPunct, depth: depth})
		case '.', ',':
			out = append(out, customerSQLToken{text: string(ch), kind: customerSQLTokenPunct, depth: depth})
		default:
			out = append(out, customerSQLToken{text: string(ch), kind: customerSQLTokenOp, depth: depth})
		}
		prevIdent = 0
		i++
	}

	return out
}

// readDollarTag reads a dollar quote tag such as $tag$ starting at i. Tag
// characters may not contain a dollar sign, which is what closes the tag.
func readDollarTag(text string, i int) (int, string, bool) {
	end := i + 1
	for end < len(text) && (isIdentStart(text[end]) || (text[end] >= '0' && text[end] <= '9')) {
		end++
	}
	if end < len(text) && text[end] == '$' {
		return end + 1, text[i : end+1], true
	}
	return 0, "", false
}

func isIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch >= 0x80
}

func isIdentByte(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '$'
}

// stripWrappingParens removes parentheses that wrap a whole token stream and
// rebases the recorded depths so that top level operators sit at depth 0.
func stripWrappingParens(tokens customerSQLTokens) customerSQLTokens {
	removed := 0
	for len(tokens) >= 2 && tokens[0].text == "(" {
		// Find the token index that closes the first parenthesis.
		depth, closeAt := 0, -1
		for i, tok := range tokens {
			switch tok.text {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 {
					closeAt = i
				}
			}
			if closeAt >= 0 {
				break
			}
		}
		if closeAt != len(tokens)-1 {
			break
		}
		tokens = tokens[1 : len(tokens)-1]
		removed++
	}
	for i := range tokens {
		tokens[i].depth -= removed
	}
	return tokens
}

// topLevelConjuncts splits a rendered expression into its top level AND
// operands.
func topLevelConjuncts(tokens customerSQLTokens) []customerSQLTokens {
	body := stripWrappingParens(tokens)
	var (
		out   []customerSQLTokens
		start int
	)
	for i, tok := range body {
		if tok.kind == customerSQLTokenIdent && tok.text == "and" && tok.depth == 0 {
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	out = append(out, body[start:])
	return out
}

func hasTopLevelKeyword(tokens customerSQLTokens, kw string) bool {
	for _, tok := range tokens {
		if tok.kind == customerSQLTokenIdent && tok.text == kw && tok.depth == 0 {
			return true
		}
	}
	return false
}

func referencesQualifier(tokens customerSQLTokens, qualifier string) bool {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind == customerSQLTokenIdent && tokens[i].text == qualifier && tokens[i+1].text == "." {
			return true
		}
	}
	return false
}

func referencesQualifiedColumn(tokens customerSQLTokens, qualifier, column string) bool {
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].kind == customerSQLTokenIdent && tokens[i].text == qualifier &&
			tokens[i+1].text == "." &&
			tokens[i+2].kind == customerSQLTokenIdent && tokens[i+2].text == column {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Query plan tree
// ---------------------------------------------------------------------------

type customerSQLPlanNode struct {
	nodeType       string
	relation       string
	alias          string
	function       string
	subplan        bool
	subplanKey     string
	inBitmapBranch bool
	parent         *customerSQLPlanNode
	children       []*customerSQLPlanNode
	conditions     []string
	output         []string
}

func buildCustomerSQLPlanNode(raw map[string]any) *customerSQLPlanNode {
	n := &customerSQLPlanNode{}
	n.nodeType, _ = raw["Node Type"].(string)
	n.relation, _ = raw["Relation Name"].(string)
	n.alias, _ = raw["Alias"].(string)
	n.function, _ = raw["Function Name"].(string)

	switch raw["Parent Relationship"].(type) {
	case string:
		rel := raw["Parent Relationship"].(string)
		n.subplan = rel == "InitPlan" || rel == "SubPlan"
	}
	if name, ok := raw["Subplan Name"].(string); ok {
		n.subplanKey = customerSQLSubplanKey(name)
		if strings.HasPrefix(name, "SubPlan ") || strings.HasPrefix(name, "InitPlan ") {
			n.subplan = true
		}
	}

	for _, key := range customerSQLPlanExprKeys {
		if v, ok := raw[key].(string); ok && v != "" {
			n.conditions = append(n.conditions, v)
		}
	}
	if v, ok := raw["Output"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				n.output = append(n.output, s)
			}
		}
	}

	if kids, ok := raw["Plans"].([]any); ok {
		for _, kid := range kids {
			if m, ok := kid.(map[string]any); ok {
				child := buildCustomerSQLPlanNode(m)
				child.parent = n
				if n.nodeType == "BitmapOr" || n.nodeType == "BitmapAnd" || n.inBitmapBranch {
					child.inBitmapBranch = true
				}
				n.children = append(n.children, child)
			}
		}
	}
	return n
}

// root returns the root of the plan tree the node belongs to.
func (n *customerSQLPlanNode) root() *customerSQLPlanNode {
	for n.parent != nil {
		n = n.parent
	}
	return n
}

func (n *customerSQLPlanNode) flatten() []*customerSQLPlanNode {
	out := []*customerSQLPlanNode{n}
	for _, c := range n.children {
		out = append(out, c.flatten()...)
	}
	return out
}

func (n *customerSQLPlanNode) subtreeContains(target *customerSQLPlanNode) bool {
	if n == target {
		return true
	}
	for _, c := range n.children {
		if c.subtreeContains(target) {
			return true
		}
	}
	return false
}

func (n *customerSQLPlanNode) countAccessesWithAlias(alias string) int {
	count := 0
	for _, node := range n.flatten() {
		if node.relation != "" && node.alias == alias {
			count++
		}
	}
	return count
}

// conditionExprs are the rendered restriction expressions attached to a node.
func (n *customerSQLPlanNode) conditionExprs() []string {
	return n.conditions
}

// credentialExprs are the expressions that can consume a column: the node's
// restrictions plus the output list of subplan roots. Leaf scan output lists are
// excluded on purpose: they project whole rows and are not references by the
// caller's expression.
func (n *customerSQLPlanNode) credentialExprs() []string {
	out := append([]string{}, n.conditions...)
	if n.subplan {
		out = append(out, n.output...)
	}
	return out
}
