package migrations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/knadh/goyesql/v2"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

const trackingLegacySchema = `
	CREATE TYPE campaign_recipient_status AS ENUM ('pending', 'queued', 'sent');
	CREATE TABLE campaigns (id SERIAL PRIMARY KEY, uuid UUID NOT NULL, name TEXT NOT NULL DEFAULT '',
		subject TEXT NOT NULL DEFAULT '', organization_id BIGINT,
		owner_user_id INT, tracking_links_mapped BOOLEAN NOT NULL DEFAULT TRUE);
	CREATE TABLE customers (id SERIAL PRIMARY KEY, uuid UUID NOT NULL, email TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '', organization_id BIGINT, owner_user_id INT);
	CREATE TABLE customer_uuid_aliases (customer_id INT NOT NULL, uuid UUID NOT NULL);
	CREATE TABLE customer_lists (id SERIAL PRIMARY KEY, type TEXT NOT NULL);
	CREATE TABLE campaign_customer_lists (campaign_id INT NOT NULL, customer_list_id INT NOT NULL);
	CREATE TABLE customer_list_memberships (customer_id INT NOT NULL, customer_list_id INT NOT NULL);
	CREATE TABLE pool_contacts (id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL);
	CREATE TABLE campaign_recipients (campaign_id INT NOT NULL, customer_id INT NOT NULL,
		status campaign_recipient_status NOT NULL DEFAULT 'sent', sent_at TIMESTAMPTZ);
	CREATE TABLE campaign_pool_recipients (
		campaign_id INT NOT NULL, pool_contact_id BIGINT NOT NULL,
		organization_id BIGINT, email_snapshot TEXT NOT NULL DEFAULT '', name_snapshot TEXT NOT NULL DEFAULT '',
		status campaign_recipient_status NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
	CREATE TABLE campaign_views (id BIGSERIAL PRIMARY KEY, campaign_id INT, customer_id INT, created_at TIMESTAMPTZ DEFAULT NOW());
	CREATE TABLE links (id SERIAL PRIMARY KEY, uuid UUID NOT NULL, url TEXT NOT NULL);
	CREATE TABLE campaign_links (campaign_id INT NOT NULL, link_id INT NOT NULL);
	CREATE TABLE link_clicks (id BIGSERIAL PRIMARY KEY, campaign_id INT, customer_id INT, link_id INT, created_at TIMESTAMPTZ DEFAULT NOW());
	CREATE TABLE bounces (id SERIAL PRIMARY KEY, campaign_id INT, customer_id INT, pool_contact_id BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW());
`

func trackingOrdinaryRecipientRule(t *testing.T) string {
	t.Helper()
	_, path, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating schema.sql")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(path), "..", "..", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const start = "CREATE FUNCTION resolve_campaign_recipient(campaign_uuid UUID, customer_ref TEXT)"
	const end = "$$ LANGUAGE SQL STABLE;"
	begin := strings.Index(string(body), start)
	if begin < 0 {
		t.Fatal("ordinary recipient rule not found")
	}
	finish := strings.Index(string(body)[begin:], end)
	if finish < 0 {
		t.Fatal("ordinary recipient rule has no terminator")
	}
	return string(body)[begin : begin+finish+len(end)]
}

func trackingQuery(t *testing.T, file, name string) string {
	t.Helper()
	_, path, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating query files")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(path), "..", "..", "queries", file))
	if err != nil {
		t.Fatal(err)
	}
	queries, err := goyesql.ParseBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	return queries[name].Query
}

func TestMigrationV6_46_0Tracking(t *testing.T) {
	db := migrationV6_43_0TestDSN(t)
	t.Cleanup(func() { _ = db.Close() })
	migrationV6_44_0SeedSchema(t, db, "migration_v6_46_0_tracking", trackingLegacySchema)
	if _, err := db.Exec(trackingOrdinaryRecipientRule(t)); err != nil {
		t.Fatalf("install ordinary recipient rule: %v", err)
	}
	const campaign = "73500ab1-e85e-4dea-b2b6-cb977b74358e"
	const otherCampaign = "9e1a81db-3463-4385-adfd-91fc85dc541e"
	const poolUUID = "1fbee598-0490-4594-bf48-3194834fd17b"
	const customerUUID = "359c875e-e64f-479d-9fe8-17e3c1907ae7"
	const linkUUID = "0fb53351-2be7-4944-9254-a7e85c1d44d7"
	const otherLinkUUID = "a3eb8c48-c780-4954-8a74-af28961c29ea"
	seeds := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO campaigns(id, uuid, name, subject, organization_id, owner_user_id) VALUES
			(1, $1, 'one', 'subject one', 7, 9), (2, $2, 'two', 'subject two', 7, 9)`, []any{campaign, otherCampaign}},
		{`INSERT INTO customers(id, uuid, email, name, organization_id, owner_user_id)
			VALUES (1, $1, 'ordinary@example.com', 'Ordinary', 7, 9)`, []any{customerUUID}},
		{`INSERT INTO customer_lists(id, type) VALUES (1, 'private')`, nil},
		{`INSERT INTO campaign_customer_lists VALUES (1, 1), (2, 1)`, nil},
		{`INSERT INTO customer_list_memberships VALUES (1, 1)`, nil},
		{`INSERT INTO pool_contacts(id, uuid) VALUES (1, $1)`, []any{poolUUID}},
		{`INSERT INTO campaign_recipients(campaign_id, customer_id, sent_at) VALUES (1, 1, NOW())`, nil},
		{`INSERT INTO campaign_pool_recipients(campaign_id, pool_contact_id, organization_id, email_snapshot, name_snapshot, status, updated_at)
			VALUES (1, 1, 7, 'pool@example.com', 'Pool', 'sent', NOW() - INTERVAL '1 hour')`, nil},
		{`INSERT INTO links(id, uuid, url) VALUES (1, $1, 'https://example.com/valid'), (2, $2, 'https://example.com/other')`, []any{linkUUID, otherLinkUUID}},
		{`INSERT INTO campaign_links VALUES (1, 1), (2, 1)`, nil},
	}
	for _, seed := range seeds {
		if _, err := db.Exec(seed.query, seed.args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := V6_46_0(db, nil, nil, nil); err != nil {
			t.Fatalf("migration attempt %d: %v", i+1, err)
		}
	}
	var sentAtPresent bool
	if err := db.Get(&sentAtPresent, `SELECT sent_at = updated_at FROM campaign_pool_recipients WHERE campaign_id=1`); err != nil || !sentAtPresent {
		t.Fatalf("historical pool send timestamp was not backfilled: %v", err)
	}

	view := trackingQuery(t, "campaigns.sql", "register-campaign-view")
	click := trackingQuery(t, "links.sql", "register-link-click")
	for _, recipient := range []string{poolUUID, customerUUID, ""} {
		if _, err := db.Exec(view, campaign, recipient, "", "", "", "", nil, nil); err != nil {
			t.Fatalf("view for %q: %v", recipient, err)
		}
		var url string
		if err := db.Get(&url, click, linkUUID, campaign, recipient); err != nil || url != "https://example.com/valid" {
			t.Fatalf("click for %q: url=%q err=%v", recipient, url, err)
		}
	}
	var ordinaryViews, ordinaryClicks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM campaign_views WHERE campaign_id=1 AND customer_id=1`).Scan(&ordinaryViews); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM link_clicks WHERE campaign_id=1 AND customer_id=1`).Scan(&ordinaryClicks); err != nil {
		t.Fatal(err)
	}
	if ordinaryViews != 1 || ordinaryClicks != 1 {
		t.Fatalf("private-list tracking rows: views=%d clicks=%d", ordinaryViews, ordinaryClicks)
	}
	// A campaign predating recipient snapshots still resolves a private-list member.
	if _, err := db.Exec(view, otherCampaign, customerUUID, "", "", "", "", nil, nil); err != nil {
		t.Fatalf("legacy private-list view: %v", err)
	}
	var legacyURL string
	if err := db.Get(&legacyURL, click, linkUUID, otherCampaign, customerUUID); err != nil || legacyURL != "https://example.com/valid" {
		t.Fatalf("legacy private-list click: url=%q err=%v", legacyURL, err)
	}
	for _, invalid := range [][3]string{
		{linkUUID, otherCampaign, poolUUID},
		{otherLinkUUID, campaign, poolUUID},
		{linkUUID, campaign, "abf98bf3-065d-48dd-8300-2a2ece63d461"},
	} {
		var url string
		if err := db.Get(&url, click, invalid[0], invalid[1], invalid[2]); err == nil {
			t.Fatalf("invalid click unexpectedly resolved %q", url)
		}
	}
	if _, err := db.Exec(view, otherCampaign, poolUUID, "", "", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	var poolViews, poolClicks, invalidViews int
	if err := db.QueryRow(`SELECT COUNT(*) FROM campaign_views WHERE campaign_id=1 AND pool_contact_id=1`).Scan(&poolViews); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM link_clicks WHERE campaign_id=1 AND pool_contact_id=1`).Scan(&poolClicks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM campaign_views WHERE campaign_id=2`).Scan(&invalidViews); err != nil {
		t.Fatal(err)
	}
	if poolViews != 1 || poolClicks != 1 || invalidViews != 1 {
		t.Fatalf("pool tracking rows: views=%d clicks=%d invalid=%d", poolViews, poolClicks, invalidViews)
	}
	var sent, uniqueViewers, uniqueClickers int
	if err := db.QueryRow(trackingQuery(t, "campaigns.sql", "get-campaign-report-summary"), 1,
		"2000-01-01", "2100-01-01").Scan(new(int), &sent, new(int), new(int), new(int), &uniqueViewers, &uniqueClickers); err != nil {
		t.Fatal(err)
	}
	if sent != 2 || uniqueViewers != 2 || uniqueClickers != 2 {
		t.Fatalf("report: sent=%d unique viewers=%d unique clickers=%d", sent, uniqueViewers, uniqueClickers)
	}

	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{"one campaign", "query-campaign-report-recipients", []any{1, "2000-01-01", "2100-01-01", "", "all", "all", "all", 0, 0, 20, true, int64(7)}},
		{"multiple campaigns", "query-campaigns-report-recipients", []any{pq.Array([]int{1}), "2000-01-01", "2100-01-01", "", "all", "all", "all", 0, 0, 20, true, int64(7)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := strings.ReplaceAll(trackingQuery(t, "campaigns.sql", tc.query), "%order%", "last_engaged_at DESC NULLS LAST")
			if tc.name == "one campaign" {
				var rows []models.CampaignReportRecipientRow
				if err := db.Select(&rows, query, tc.args...); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 2 || rows[0].Total != 2 || rows[1].Total != 2 || rows[0].ViewCount != 1 || rows[1].ViewCount != 1 {
					t.Fatalf("unexpected recipient detail: %+v", rows)
				}
				if err := db.Select(&rows, query, 1, "2000-01-01", "2100-01-01", "", "all", "all", "all", 0, 0, 20, true, int64(8)); err != nil || len(rows) != 1 || rows[0].PoolContactID != 0 {
					t.Fatalf("organization scope: rows=%+v err=%v", rows, err)
				}
				if err := db.Select(&rows, query, 1, "2000-01-01", "2100-01-01", "", "all", "all", "all", 0, 0, 20, false, int64(7)); err != nil || len(rows) != 1 || rows[0].PoolContactID != 0 {
					t.Fatalf("pool permission gate: rows=%+v err=%v", rows, err)
				}
			} else {
				var rows []models.CampaignsReportRecipientRow
				if err := db.Select(&rows, query, tc.args...); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 2 || rows[0].Total != 2 || rows[1].Total != 2 {
					t.Fatalf("unexpected cross-campaign recipient detail: %+v", rows)
				}
				if err := db.Select(&rows, query, pq.Array([]int{1}), "2000-01-01", "2100-01-01", "", "all", "all", "all", 0, 0, 20, true, int64(8)); err != nil || len(rows) != 1 || rows[0].PoolContactID != 0 {
					t.Fatalf("cross-campaign organization scope: rows=%+v err=%v", rows, err)
				}
			}
		})
	}
}
