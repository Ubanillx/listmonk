package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/knadh/goyesql/v2"
)

func TestPublicCampaignSQLGuards(t *testing.T) {
	queries := loadPublicCampaignQueryFiles(t)

	requireQueryTerms(t, queries, "get-public-campaign-recipient",
		"campaign_recipients",
		"customer_uuid_aliases",
		"snapshot_recipient",
		"legacy_recipient",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id",
	)
	requireQueryTerms(t, queries, "register-campaign-view",
		"campaign_recipients",
		"customer_uuid_aliases",
		"snapshot_recipient",
		"legacy_recipient",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
	)
	requireQueryTerms(t, queries, "register-link-click",
		"campaign_recipients",
		"campaign_links",
		"customer_uuid_aliases",
		"snapshot_recipient",
		"legacy_recipient",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
	)
	requireQueryTerms(t, queries, "unsubscribe-by-campaign",
		"campaign_recipients",
		"customer_uuid_aliases",
		"snapshot_recipient",
		"legacy_recipient",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id",
	)

	for _, name := range []string{
		"get-public-campaign-recipient",
		"register-campaign-view",
		"register-link-click",
		"unsubscribe-by-campaign",
	} {
		requireSnapshotRecipientOwnershipIndependent(t, queries, name)
	}

	// First-class public pools must re-check organization exclusions and retain
	// immutable snapshot fields at queue time; this protects the primary pool
	// when a secondary list is edited during an in-flight campaign.
	requireQueryTerms(t, queries, "queue-campaign-pool-customers",
		"campaign_pool_recipients",
		"pool_segment_exclusions",
		"restored_at IS NULL",
		"email_snapshot",
		"organization_id",
		"FOR UPDATE OF cpr SKIP LOCKED",
	)
	// A campaign's persisted totals drive completed-state reporting. They must
	// include pool rows as well as conventional customer rows; otherwise a
	// public-pool delivery can finish with sent > 0 but to_send = 0.
	requireQueryTerms(t, queries, "sync-campaign-progress",
		"pool_counts",
		"campaign_pool_recipients",
		"cpr.organization_id IS NOT DISTINCT FROM c.organization_id",
	)
	// The recipient snapshot's composite key is the final de-duplication
	// boundary when a campaign selects both a first-level pool and an explicit
	// secondary list.  Every expansion path uses ON CONFLICT against this key.
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
