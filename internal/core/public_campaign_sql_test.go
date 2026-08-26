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
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id",
	)
	requireQueryTerms(t, queries, "register-campaign-view",
		"campaign_recipients",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
	)
	requireQueryTerms(t, queries, "register-link-click",
		"campaign_recipients",
		"campaign_links",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
	)
	requireQueryTerms(t, queries, "unsubscribe-by-campaign",
		"campaign_recipients",
		"s.organization_id IS NOT DISTINCT FROM c.organization_id",
		"s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id",
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
	for _, name := range []string{"campaigns.sql", "links.sql", "subscribers.sql"} {
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
