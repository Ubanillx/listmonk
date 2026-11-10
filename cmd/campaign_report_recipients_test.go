package main

import (
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
)

// TestCampaignsReportRecipientsAggregatePoolStats locks in the set-based pool
// aggregation: views, clicks, last click and bounces must match the former
// per-recipient LATERAL query, and every tri-state filter must still apply.
func TestCampaignsReportRecipientsAggregatePoolStats(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility)
		VALUES(gen_random_uuid(),'Recipients report','Subject','sender@example.test','Body','email','finished',$1,$2,'organization') RETURNING id`,
		orgPoolAllocationTestMemberUser, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}

	newCustomer := func(email string) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,owner_user_id,organization_id)
			VALUES(gen_random_uuid(),$1,$1,$2,$3) RETURNING id`,
			email, orgPoolAllocationTestMemberUser, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	privateA := newCustomer("private-a@example.test")
	privateB := newCustomer("private-b@example.test")
	for _, id := range []int{privateA, privateB} {
		exec(`INSERT INTO campaign_recipients(campaign_id,customer_id,status,sent_at)
			VALUES($1,$2,'sent',NOW() - INTERVAL '10 minutes')`, campaignID, id)
	}

	var poolListID int
	if err := a.db.Get(&poolListID, `INSERT INTO customer_lists(uuid,name,type,organization_id)
		VALUES(gen_random_uuid(),'Report pool','pool',$1) RETURNING id`, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	newPoolContact := func(email string) int64 {
		t.Helper()
		var id int64
		if err := a.db.Get(&id, `INSERT INTO pool_contacts(email,name) VALUES($1,$1) RETURNING id`, email); err != nil {
			t.Fatal(err)
		}
		return id
	}
	addPoolRecipient := func(email string, orgID int) int64 {
		t.Helper()
		id := newPoolContact(email)
		exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,organization_id,email_snapshot,name_snapshot,status,sent_at)
			VALUES($1,$2,$3,$4,$5,$5,'sent',NOW() - INTERVAL '10 minutes')`, campaignID, id, poolListID, orgID, email)
		return id
	}
	poolA := addPoolRecipient("pool-a@example.test", orgPoolAllocationTestHomeOrgID)
	poolB := addPoolRecipient("pool-b@example.test", orgPoolAllocationTestHomeOrgID)
	addPoolRecipient("pool-c@example.test", orgPoolAllocationTestHomeOrgID)  // no events
	addPoolRecipient("pool-d@example.test", orgPoolAllocationTestOtherOrgID) // other organization, must be hidden

	link := func(url string) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO links(uuid,url) VALUES(gen_random_uuid(),$1) RETURNING id`, url); err != nil {
			t.Fatal(err)
		}
		return id
	}
	const link1URL = "https://example.test/offer"
	const link2URL = "https://example.test/follow-up"
	link1 := link("https://example.test/offer")
	link2 := link(link2URL)

	exec(`INSERT INTO campaign_views(campaign_id,customer_id,created_at)
		VALUES($1,$2,NOW() - INTERVAL '5 minutes')`, campaignID, privateA)
	exec(`INSERT INTO campaign_views(campaign_id,pool_contact_id,created_at) VALUES
		($1,$2,NOW() - INTERVAL '5 minutes'), ($1,$2,NOW() - INTERVAL '4 minutes')`, campaignID, poolA)
	exec(`INSERT INTO campaign_views(campaign_id,pool_contact_id,created_at)
		VALUES($1,$2,NOW() - INTERVAL '2 hours')`, campaignID, poolA)
	exec(`INSERT INTO link_clicks(campaign_id,link_id,customer_id,created_at)
		VALUES($1,$2,$3,NOW() - INTERVAL '3 minutes')`, campaignID, link1, privateB)
	exec(`INSERT INTO link_clicks(campaign_id,link_id,pool_contact_id,created_at) VALUES
		($1,$2,$3,NOW() - INTERVAL '2 minutes'), ($1,$4,$3,NOW() - INTERVAL '1 minute')`,
		campaignID, link1, poolB, link2)
	exec(`INSERT INTO bounces(campaign_id,pool_contact_id,type,created_at)
		VALUES($1,$2,'hard',NOW() - INTERVAL '1 minute')`, campaignID, poolA)

	access := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: orgPoolAllocationTestHomeOrgID},
		UserID:    orgPoolAllocationTestMemberUser,
	}
	// Use the database clock so the range cannot drift from NOW()-based fixtures.
	var dbNow time.Time
	if err := a.db.Get(&dbNow, `SELECT NOW()`); err != nil {
		t.Fatal(err)
	}
	from := dbNow.Add(-time.Hour).Format(time.RFC3339)
	to := dbNow.Add(time.Hour).Format(time.RFC3339)
	query := func(filters models.CampaignReportRecipientFilters, includePool bool) (map[string]models.CampaignsReportRecipientRow, int) {
		t.Helper()
		filters.SortBy = "email"
		filters.Order = "asc"
		rows, total, err := a.core.QueryWorkspaceCampaignsReportRecipients(access, []int{campaignID}, from, to, filters, 0, 20, includePool)
		if err != nil {
			t.Fatal(err)
		}
		byEmail := make(map[string]models.CampaignsReportRecipientRow, len(rows))
		for _, row := range rows {
			byEmail[row.Email] = row
		}
		return byEmail, total
	}

	rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "all"}, true)
	if total != 5 || len(rows) != 5 {
		t.Fatalf("rows=%d total=%d, want 5/5", len(rows), total)
	}
	if row := rows["pool-a@example.test"]; row.PoolContactID != poolA || row.ViewCount != 2 || row.BounceCount != 1 || row.ClickCount != 0 {
		t.Fatalf("pool-a stats = %+v", row)
	}
	if row := rows["pool-b@example.test"]; row.PoolContactID != poolB || row.ClickCount != 2 ||
		!row.LastLinkURL.Valid || row.LastLinkURL.String != link2URL || row.LastLinkID.Int != link2 {
		t.Fatalf("pool-b stats = %+v", row)
	}
	if row := rows["pool-c@example.test"]; row.ViewCount != 0 || row.ClickCount != 0 || row.BounceCount != 0 {
		t.Fatalf("pool-c stats = %+v", row)
	}
	if _, ok := rows["pool-d@example.test"]; ok {
		t.Fatal("another organization's pool recipient leaked into the report")
	}
	if row := rows["private-a@example.test"]; row.ViewCount != 1 || row.PoolContactID != 0 {
		t.Fatalf("private-a stats = %+v", row)
	}
	if row := rows["private-b@example.test"]; row.ClickCount != 1 || !row.LastLinkURL.Valid || row.LastLinkURL.String != link1URL {
		t.Fatalf("private-b stats = %+v", row)
	}

	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "yes", Clicked: "all", Bounced: "all"}, true); total != 2 || len(rows) != 2 {
		t.Fatalf("opened=yes rows=%d total=%d, want 2/2", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "no", Clicked: "all", Bounced: "all"}, true); total != 3 || len(rows) != 3 {
		t.Fatalf("opened=no rows=%d total=%d, want 3/3", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "yes", Bounced: "all"}, true); total != 2 || len(rows) != 2 {
		t.Fatalf("clicked=yes rows=%d total=%d, want 2/2", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "no", Bounced: "all"}, true); total != 3 || len(rows) != 3 {
		t.Fatalf("clicked=no rows=%d total=%d, want 3/3", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "yes"}, true); total != 1 || len(rows) != 1 {
		t.Fatalf("bounced=yes rows=%d total=%d, want 1/1", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "no"}, true); total != 4 || len(rows) != 4 {
		t.Fatalf("bounced=no rows=%d total=%d, want 4/4", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "all", LinkID: link1}, true); total != 2 || len(rows) != 2 {
		t.Fatalf("link filter rows=%d total=%d, want 2/2", len(rows), total)
	}
	if rows, total := query(models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "all"}, false); total != 2 || len(rows) != 2 {
		t.Fatalf("includePool=false rows=%d total=%d, want 2/2", len(rows), total)
	}

	// The single-campaign variant shares the rewritten pool CTEs.
	single, total, err := a.core.QueryWorkspaceCampaignReportRecipients(access, campaignID, from, to,
		models.CampaignReportRecipientFilters{Opened: "all", Clicked: "all", Bounced: "all", SortBy: "email", Order: "asc"}, 0, 20, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(single) != 5 {
		t.Fatalf("single rows=%d total=%d, want 5/5", len(single), total)
	}
	for _, row := range single {
		if row.Email == "pool-b@example.test" && (!row.LastLinkURL.Valid || row.LastLinkURL.String != link2URL) {
			t.Fatalf("single pool-b stats = %+v", row)
		}
	}
}
