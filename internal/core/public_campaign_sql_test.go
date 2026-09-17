package core

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/knadh/goyesql/v2"
)

// TestPublicCampaignSQLGuards guards the security-relevant SQL of the public
// campaign paths.
//
// The binding rule for a public bearer (campaign UUID + customer UUID -> an
// actual recipient of that campaign) used to be repeated in four queries and had
// already diverged on empty customer references. It now has exactly one
// definition, resolve_campaign_recipient() in schema.sql, so the guard checks
// that definition once and additionally requires every public path to consume it
// instead of growing its own copy again.
func TestPublicCampaignSQLGuards(t *testing.T) {
	queries := loadPublicCampaignQueryFiles(t)

	definition := loadResolveCampaignRecipientDefinition(t)
	requireQueryTerms(t, goyesql.Queries{
		"resolve_campaign_recipient": {Query: definition},
	}, "resolve_campaign_recipient",
		"campaign_recipients",
		"customer_uuid_aliases",
		"snapshot_recipient",
		"legacy_recipient",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id",
	)
	// An empty customer reference must mean "no customer" everywhere, not an
	// invalid UUID cast on some paths and a no-op on others.
	if !strings.Contains(definition, "NULLIF(customer_ref, '')::UUID") {
		t.Error("the shared recipient rule must treat an empty customer reference as no customer")
	}
	requireSnapshotRecipientOwnershipIndependent(t, goyesql.Queries{
		"resolve_campaign_recipient": {Query: definition},
	}, "resolve_campaign_recipient")

	// A fresh install (schema.sql) and an upgrade (the migration) must install the
	// same rule, otherwise the two deployment paths resolve bearers differently.
	requireSameRuleDefinition(t, definition, loadMigrationRuleDefinition(t))

	// Every public bearer path consumes the shared rule and does not keep a second
	// copy of it.
	for _, name := range []string{
		"get-public-campaign-recipient",
		"register-campaign-view",
		"register-link-click",
		"unsubscribe-by-campaign",
	} {
		requireQueryTerms(t, queries, name, "resolve_campaign_recipient(")
		requireNoLocalCopyOfRecipientRule(t, queries, name)
	}

	// Path-specific guards that are not part of the shared rule.
	requireQueryTerms(t, queries, "register-link-click", "campaign_links", "tracking_links_mapped")
	requireQueryTerms(t, queries, "register-campaign-view", "campaign_views")
	requireQueryTerms(t, queries, "unsubscribe-by-campaign", "campaign_customer_lists", "customer_list_memberships")

	// First-class public pools must re-check organization exclusions and retain
	// immutable snapshot fields at queue time; this protects the primary pool
	// when a pool allocation is edited during an in-flight campaign.
	requireQueryTerms(t, queries, "queue-campaign-pool-customers",
		"campaign_pool_recipients",
		"org_pool_allocation_exclusions",
		"restored_at IS NULL",
		"email_snapshot",
		"organization_id",
		"FOR UPDATE OF cpr SKIP LOCKED",
	)
	// A campaign's persisted totals drive completed-state reporting. They must
	// include pool rows as well as conventional customer rows; otherwise a
	// public-pool delivery can finish with sent > 0 but to_send = 0. The totals
	// are now read from campaign_send_counts, which counts both relations under
	// the ownership rules the sender applies (guarded by
	// TestCampaignSendCountsHaveOneDefinition).
	requireQueryTerms(t, queries, "sync-campaign-progress", "campaign_send_counts")
	// The recipient snapshot's composite key is the final de-duplication
	// boundary when a campaign selects both a first-level pool and an explicit
	// pool allocation.  Every expansion path uses ON CONFLICT against this key.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating schema.sql")
	}
	schema, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "..", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema.sql: %v", err)
	}
	if !strings.Contains(string(schema), "PRIMARY KEY (campaign_id, pool_contact_id)") {
		t.Error("campaign_pool_recipients must be unique per campaign and pool contact")
	}
	poolCore, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "pools.go"))
	if err != nil {
		t.Fatalf("read pools.go: %v", err)
	}
	if !strings.Contains(string(poolCore), "ON CONFLICT(campaign_id,pool_contact_id)") {
		t.Error("pool recipient expansion must upsert by campaign and pool contact")
	}
	// Campaign metadata must identify the effective internal reply mailbox for
	// each pool audience; this is an internal address and is intentionally not
	// subject to customer-email masking.
	requireQueryTerms(t, queries, "get-campaign-stats", "reply_mailbox_email", "reply_mailboxes")
	requireQueryTerms(t, queries, "get-campaign-for-preview", "reply_mailbox_email", "reply_mailboxes")
	requireQueryTerms(t, queries, "get-public-pool-campaign-recipient",
		"campaign_pool_recipients",
		"pool_contacts",
		"pc.uuid=$2::UUID",
		"cpr.status IN ('pending','queued','sent')",
	)
}

// loadResolveCampaignRecipientDefinition returns the shared rule as installed by
// schema.sql.
func loadResolveCampaignRecipientDefinition(t *testing.T) string {
	t.Helper()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating schema.sql")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "..", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema.sql: %v", err)
	}

	return extractRuleDefinition(t, string(body), "schema.sql")
}

// loadMigrationRuleDefinition returns the same rule as installed by the upgrade
// migration.
func loadMigrationRuleDefinition(t *testing.T) string {
	t.Helper()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating the migration")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "migrations", "v6.30.0.go"))
	if err != nil {
		t.Fatalf("read the v6.30.0 migration: %v", err)
	}

	return extractRuleDefinition(t, string(body), "internal/migrations/v6.30.0.go")
}

// extractRuleDefinition cuts the function body out of a file that defines
// resolve_campaign_recipient.
func extractRuleDefinition(t *testing.T, content, source string) string {
	t.Helper()

	start := strings.Index(content, "FUNCTION resolve_campaign_recipient(campaign_uuid UUID, customer_ref TEXT)")
	if start < 0 {
		t.Fatalf("%s does not define resolve_campaign_recipient", source)
	}
	rest := content[start:]
	end := strings.Index(rest, "$$ LANGUAGE SQL STABLE")
	if end < 0 {
		t.Fatalf("%s defines resolve_campaign_recipient without a closed body", source)
	}

	return rest[:end]
}

// TestCampaignSendCountsHaveOneDefinition guards the consolidation of the
// campaign "recipients left" rule. The list/detail/scheduler projections, the
// sender's send-state query and the progress sync each used to compute it
// separately: some counted only customer snapshot rows (and skipped the
// ownership and transfer checks), the sender added pool recipients, and the
// progress sync had its own copy of both rules. A campaign whose audience
// includes pools therefore reported one unsent count in the UI and another one
// to the sender. All of them now read campaign_send_counts, so the guard checks
// that single definition and requires every call site to consume it.
func TestCampaignSendCountsHaveOneDefinition(t *testing.T) {
	queries := loadPublicCampaignQueryFiles(t)

	view := extractSendCountsView(t, readRepoFile(t, "schema.sql"), "schema.sql")
	migrationView := extractSendCountsView(t, readRepoFile(t, "internal/migrations/v6.31.0.go"), "internal/migrations/v6.31.0.go")
	requireSameRuleDefinition(t, view, migrationView)

	requireQueryTerms(t, goyesql.Queries{"campaign_send_counts": {Query: view}}, "campaign_send_counts",
		"campaign_recipients",
		"campaign_pool_recipients",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id = c.owner_user_id",
		"s.transfer_pending_at IS NULL",
		"cpr.organization_id IS NOT DISTINCT FROM c.organization_id",
		"GREATEST(c.to_send - c.sent, 0)",
	)

	// Every projection, the sender and the progress sync consume the view.
	for _, name := range []string{
		"query-campaigns",
		"get-campaign",
		"get-archived-campaigns",
		"get-campaign-for-preview",
		"get-campaign-status",
		"next-campaigns",
		"get-campaign-send-state",
		"sync-campaign-progress",
	} {
		requireQueryTerms(t, queries, name, "campaign_send_counts")
		// The removed local copy of the rule, which is how the counts drifted.
		if strings.Contains(queries[name].Query, "FROM campaign_recipients crx") {
			t.Errorf("query %q keeps a local recipient-count rule; it must read campaign_send_counts", name)
		}
	}

	// The workspace-scoped list/detail/history projections in Go carry the same
	// rule and drifted the furthest (they skipped the ownership checks entirely).
	workspaceQueries := readRepoFile(t, "internal/core/workspace_queries.go")
	if got := strings.Count(workspaceQueries, "FROM campaign_send_counts"); got != 3 {
		t.Errorf("workspace_queries.go must read campaign_send_counts in all 3 projections, found %d", got)
	}
	if strings.Contains(workspaceQueries, "FROM campaign_recipients crx") {
		t.Error("workspace_queries.go keeps a local recipient-count rule; it must read campaign_send_counts")
	}
}

// extractSendCountsView cuts the view definition out of a file that installs it.
func extractSendCountsView(t *testing.T, content, source string) string {
	t.Helper()

	start := strings.Index(content, "VIEW campaign_send_counts AS")
	if start < 0 {
		t.Fatalf("%s does not define campaign_send_counts", source)
	}
	rest := content[start:]
	end := strings.Index(rest, ") po ON TRUE;")
	if end < 0 {
		t.Fatalf("%s defines campaign_send_counts without a closed body", source)
	}

	return rest[:end]
}

// readRepoFile reads a file relative to the repository root (two levels up from
// this package).
func readRepoFile(t *testing.T, name string) string {
	t.Helper()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("locating %s", name)
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return string(body)
}

// requireSameRuleDefinition compares the fresh-install and upgrade definitions.
func requireSameRuleDefinition(t *testing.T, install, upgrade string) {
	t.Helper()

	normalize := func(s string) string {
		// Drop SQL comments and collapse whitespace so only semantics differ.
		var b strings.Builder
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			b.WriteString(regexp.MustCompile(`\s+`).ReplaceAllString(line, " "))
			b.WriteString("\n")
		}
		return b.String()
	}

	if normalize(install) != normalize(upgrade) {
		t.Error("schema.sql and the v6.30.0 migration must install the same recipient rule; a fresh install and an upgrade would otherwise resolve public bearers differently")
	}
}

// requireNoLocalCopyOfRecipientRule fails when a public path grows its own copy
// of the binding rule again, which is exactly how the four copies diverged.
func requireNoLocalCopyOfRecipientRule(t *testing.T, queries goyesql.Queries, name string) {
	t.Helper()

	query, ok := queries[name]
	if !ok {
		t.Fatalf("query %q is not registered", name)
	}
	for _, forbidden := range []string{
		"snapshot_recipient AS (",
		"legacy_recipient AS (",
		"customer_uuid_aliases",
		"campaign_recipients",
	} {
		if strings.Contains(query.Query, forbidden) {
			t.Errorf("query %q keeps a local copy of the recipient rule (%q); it must consume resolve_campaign_recipient instead", name, forbidden)
		}
	}
}

func loadPublicCampaignQueryFiles(t *testing.T) goyesql.Queries {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating public campaign query fixtures")
	}
	queryDir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "queries"))
	queries := goyesql.Queries{}
	for _, name := range []string{"campaigns.sql", "links.sql", "customers.sql"} {
		body, err := os.ReadFile(filepath.Join(queryDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parsed, err := goyesql.ParseBytes(body)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for key, query := range parsed {
			queries[key] = query
		}
	}
	return queries
}

func requireQueryTerms(t *testing.T, queries goyesql.Queries, name string, terms ...string) {
	t.Helper()
	query, ok := queries[name]
	if !ok {
		t.Fatalf("query %q is not registered", name)
	}
	for _, term := range terms {
		if !strings.Contains(query.Query, term) {
			t.Errorf("query %q is missing required guard %q", name, term)
		}
	}
}

func requireSnapshotRecipientOwnershipIndependent(t *testing.T, queries goyesql.Queries, name string) {
	t.Helper()
	query, ok := queries[name]
	if !ok {
		t.Fatalf("query %q is not registered", name)
	}
	start := strings.Index(query.Query, "snapshot_recipient AS (")
	end := strings.Index(query.Query, "legacy_recipient AS (")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("query %q does not separate snapshot and legacy recipient branches", name)
	}
	snapshot := query.Query[start:end]
	for _, forbidden := range []string{"s.organization_id", "s.owner_user_id"} {
		if strings.Contains(snapshot, forbidden) {
			t.Errorf("query %q binds its recipient snapshot to current %s", name, forbidden)
		}
	}
}
