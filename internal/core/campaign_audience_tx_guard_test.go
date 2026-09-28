package core

import (
	"strings"
	"testing"
)

// TestCampaignAudienceWritesStayInsideOneTransaction guards the transaction
// boundary this package promises for campaign create/update.
//
// The campaign row, its customer_list/media/visibility relations, its reply
// mailbox and its pool audience (relation rows plus recipient snapshots) are
// written by one Core transaction. That only holds while every statement of the
// shared write bodies runs on the caller's *sqlx.Tx: a single c.db call inside
// the closure would silently escape the transaction and re-open exactly the
// partial-update hole this refactor closed. The check is deliberately textual —
// the same spirit as TestPublicCampaignSQLGuards — because a compiler cannot
// see the difference between tx.Exec and c.db.Exec.
func TestCampaignAudienceWritesStayInsideOneTransaction(t *testing.T) {
	const audienceFile = "internal/core/campaign_audience_tx.go"

	// The two entry points write the campaign and its audience in exactly one
	// callback, so their closure is the transaction boundary under test.
	entryPoints := []struct {
		file     string
		name     string
		requires []string
	}{
		{
			file: audienceFile,
			name: "UpdateCampaignWithAudienceInWorkspace",
			requires: []string{
				"withWorkspaceResourceMutation(access, resourceCampaigns, []int{id}",
				"c.updateCampaignTx(tx, access, id, o, customerListIDs, mediaIDs, visibility)",
				"c.updateCampaignReplyMailboxTx(tx, id, access.UserID, o.ReplyMailboxID)",
				"c.syncCampaignAudienceTx(tx, id, access, audience)",
			},
		},
		{
			file: audienceFile,
			name: "CreateCampaignWithAudienceInWorkspace",
			requires: []string{
				"c.withWorkspaceCreation(access,",
				"c.createCampaignTx(tx, access, o, customerListIDs, mediaIDs, scope, uuidValue)",
				"c.updateCampaignReplyMailboxTx(tx, newID, access.UserID, o.ReplyMailboxID)",
				"c.syncCampaignAudienceTx(tx, newID, access, audience)",
			},
		},
	}
	for _, tc := range entryPoints {
		source := readRepoFile(t, tc.file)
		body := requireFunctionBody(t, tc.file, tc.name, source)
		for _, required := range tc.requires {
			if !strings.Contains(body, required) {
				t.Errorf("%s must perform %q inside its single write transaction", tc.name, required)
			}
		}
		closure := requireTxClosureBody(t, tc.file, tc.name, body)
		if strings.Contains(closure, "c.db") {
			t.Errorf("%s calls c.db inside its transaction callback; every statement must run on tx", tc.name)
		}
		// Bonus strictness: no part of the entry point may touch the pool
		// directly, only through the tx helpers.
		if strings.Contains(body, "c.db") {
			t.Errorf("%s uses c.db; the campaign audience write must stay on the caller's transaction", tc.name)
		}
	}

	// The shared bodies and tx helpers run inside a transaction owned by
	// somebody else, so they must never open their own connection.
	for _, tc := range []struct{ file, name string }{
		{audienceFile, "syncCampaignAudienceTx"},
		{audienceFile, "updateCampaignReplyMailboxTx"},
		{"internal/core/workspace_resource_writes.go", "updateCampaignTx"},
		{"internal/core/campaigns.go", "createCampaignTx"},
		{"internal/core/pools_tx.go", "ensurePoolTx"},
		{"internal/core/pools_tx.go", "resolvePoolRecipientsTx"},
		{"internal/core/pools_tx.go", "organizationReplyMailboxIDTx"},
		{"internal/core/pools_tx.go", "refreshPoolCampaignRecipientsTx"},
		{"internal/core/pools_tx.go", "refreshAllOrgPoolCampaignRecipientsTx"},
		{"internal/core/pools_tx.go", "attachPoolToCampaignTx"},
	} {
		body := requireFunctionBody(t, tc.file, tc.name, readRepoFile(t, tc.file))
		if strings.Contains(body, "c.db") {
			t.Errorf("%s (%s) uses c.db; a tx-aware helper must run every statement on tx", tc.name, tc.file)
		}
	}
}

// TestCampaignAudienceSQLHasOneHome guards the other half of the refactor: the
// statements exist once in Core and the HTTP handler no longer drives the
// audience rebuild with its own auto-commit statements.
func TestCampaignAudienceSQLHasOneHome(t *testing.T) {
	handler := readRepoFile(t, "cmd/campaigns.go")
	for _, forbidden := range []string{
		"DELETE FROM campaign_pool_recipients",
		"DELETE FROM campaign_customer_lists",
		"DELETE FROM campaign_pool_org_orders",
		"UPDATE campaigns SET reply_mailbox_id",
		"persistCampaignReplyMailbox",
		"AttachPoolToCampaign",
	} {
		if strings.Contains(handler, forbidden) {
			t.Errorf("cmd/campaigns.go still performs %q; the campaign write must go through the single Core transaction", forbidden)
		}
	}

	// The rebuild and the mailbox update live in exactly one place.
	audience := readRepoFile(t, "internal/core/campaign_audience_tx.go")
	for _, statement := range []string{
		"DELETE FROM campaign_pool_recipients WHERE campaign_id=$1`",
		"DELETE FROM campaign_customer_lists WHERE campaign_id=$1 AND pool_id IS NOT NULL`",
		"DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`",
		"UPDATE campaigns SET reply_mailbox_id = $1, updated_at = NOW() WHERE id = $2 AND owner_user_id = $3",
	} {
		if got := strings.Count(audience, statement); got != 1 {
			t.Errorf("the campaign audience write must own %q exactly once, found %d", statement, got)
		}
	}

	// The tx-aware pool helpers own the SQL; the single-call forms in pools.go
	// must delegate instead of keeping a second copy.
	pools := readRepoFile(t, "internal/core/pools.go")
	for _, duplicated := range []string{
		"DELETE FROM campaign_pool_org_orders WHERE campaign_id=$1`",
		"VALUES($1,NULL,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING",
		"invalid pool allocation",
	} {
		if strings.Contains(pools, duplicated) {
			t.Errorf("internal/core/pools.go keeps its own copy of %q; the SQL must live in pools_tx.go", duplicated)
		}
	}
}

// requireFunctionBody returns one method body, from its opening brace through
// the matching closing brace.
func requireFunctionBody(t *testing.T, file, name, source string) string {
	t.Helper()

	start := strings.Index(source, "func (c *Core) "+name+"(")
	if start < 0 {
		t.Fatalf("%s no longer defines %s", file, name)
	}
	open := strings.Index(source[start:], "{")
	if open < 0 {
		t.Fatalf("%s: %s has no body", file, name)
	}
	open += start
	end := matchingBrace(source, open)
	if end < 0 {
		t.Fatalf("%s: %s has an unterminated body", file, name)
	}
	return source[open : end+1]
}

// requireTxClosureBody returns the body of the first func(*sqlx.Tx) error
// callback inside a method body.
func requireTxClosureBody(t *testing.T, file, name, body string) string {
	t.Helper()

	const needle = "func(tx *sqlx.Tx) error {"
	start := strings.Index(body, needle)
	if start < 0 {
		t.Fatalf("%s: %s no longer performs its writes inside a tx callback", file, name)
	}
	open := start + strings.Index(body[start:], "{")
	end := matchingBrace(body, open)
	if end < 0 {
		t.Fatalf("%s: %s has an unterminated tx callback", file, name)
	}
	return body[open : end+1]
}

// matchingBrace returns the index of the brace closing the one at open,
// ignoring braces inside comments, string literals and rune literals.
func matchingBrace(source string, open int) int {
	const (
		codeState = iota
		lineCommentState
		blockCommentState
		interpretedState
		rawState
		runeState
	)
	state := codeState
	depth := 0
	for i := open; i < len(source); i++ {
		c := source[i]
		switch state {
		case codeState:
			switch {
			case c == '/' && i+1 < len(source) && source[i+1] == '/':
				state = lineCommentState
				i++
			case c == '/' && i+1 < len(source) && source[i+1] == '*':
				state = blockCommentState
				i++
			case c == '"':
				state = interpretedState
			case c == '`':
				state = rawState
			case c == '\'':
				state = runeState
			case c == '{':
				depth++
			case c == '}':
				depth--
				if depth == 0 {
					return i
				}
			}
		case lineCommentState:
			if c == '\n' {
				state = codeState
			}
		case blockCommentState:
			if c == '*' && i+1 < len(source) && source[i+1] == '/' {
				state = codeState
				i++
			}
		case interpretedState:
			if c == '\\' {
				i++
			} else if c == '"' {
				state = codeState
			}
		case rawState:
			if c == '`' {
				state = codeState
			}
		case runeState:
			if c == '\\' {
				i++
			} else if c == '\'' {
				state = codeState
			}
		}
	}
	return -1
}
