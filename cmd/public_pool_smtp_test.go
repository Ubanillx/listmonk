package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestPublicPoolSMTPOrganizationsCoverAllSelectedPools(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	a.queries = prepareQueries(readTestQueries(t), a.db, koanf.New("."))
	seedOrgPoolAllocationFixtures(t, a.db)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(query string, args ...any) int {
		t.Helper()
		var out int
		if err := a.db.Get(&out, query, args...); err != nil {
			t.Fatal(err)
		}
		return out
	}
	firstPool := seedOrgPoolAllocationPool(t, a.db, "First public pool")
	secondPool := seedOrgPoolAllocationPool(t, a.db, "Second public pool")
	allocate := func(pool, org int) int {
		list := id(`INSERT INTO customer_lists(uuid,name,type,organization_id)
            VALUES(gen_random_uuid(),'Allocation','org_pool_allocation',$1) RETURNING id`, org)
		return id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id)
            VALUES($1,$2,$3) RETURNING id`, list, pool, org)
	}
	homeFirst := allocate(firstPool, orgPoolAllocationTestHomeOrgID)
	homeSecond := allocate(secondPool, orgPoolAllocationTestHomeOrgID)
	otherSecond := allocate(secondPool, orgPoolAllocationTestOtherOrgID)
	campaign := id(`INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,pool_scope,smtp_source)
        VALUES(gen_random_uuid(),'Multi pool','Subject','sender@example.invalid','Body','email','all_organizations','organization') RETURNING id`)
	exec(`INSERT INTO campaign_customer_lists(campaign_id,pool_id) VALUES($1,$2),($1,$3)`, campaign, firstPool, secondPool)
	// Each allocation has a deliverable contact with an imported reply address.
	for _, pair := range [][2]int{{firstPool, homeFirst}, {secondPool, homeSecond}, {secondPool, otherSecond}} {
		contact := id(`INSERT INTO pool_contacts(customer_code,name,email,reply_to)
            VALUES('CODE','Customer',gen_random_uuid()::TEXT||'@example.invalid','reply@example.invalid') RETURNING id`)
		exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pair[0], contact)
		exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, pair[1], contact)
	}
	for _, org := range []int{orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestOtherOrgID} {
		pool := id(`INSERT INTO organization_smtp_pools(organization_id,name) VALUES($1,'Sender pool') RETURNING id`, org)
		exec(`INSERT INTO user_smtp_servers(uuid,organization_id,smtp_pool_id,from_email)
            VALUES(gen_random_uuid(),$1,$2,'organization@example.invalid')`, org, pool)
	}
	var orgs []int64
	if err := a.queries.GetCampaignPoolOrgs.Select(&orgs, campaign); err != nil {
		t.Fatal(err)
	}
	want := []int64{orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestOtherOrgID}
	if !slices.Equal(orgs, want) {
		t.Fatalf("organizations=%v want=%v", orgs, want)
	}
	type status struct {
		OrganizationID int64 `db:"organization_id"`
		MailboxReady   bool  `db:"mailbox_ready"`
		SMTPCount      int   `db:"smtp_count"`
	}
	var statuses []status
	if err := a.queries.GetCampaignPoolOrgStatus.Select(&statuses, campaign); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("shared organization was not deduplicated: %+v", statuses)
	}
	for _, row := range statuses {
		if !row.MailboxReady || row.SMTPCount != 1 {
			t.Fatalf("organization source used the wrong accounts or reply routes: %+v", row)
		}
	}
	// A missing reply route in the second allocation must block the home
	// organization even though its first allocation is ready.
	exec(`UPDATE pool_contacts SET reply_to='' WHERE id IN (
        SELECT contact_id FROM org_pool_allocation_members WHERE allocation_id=$1)`, homeSecond)
	if err := a.queries.GetCampaignPoolOrgStatus.Select(&statuses, campaign); err != nil {
		t.Fatal(err)
	}
	if statuses[0].MailboxReady {
		t.Fatal("second allocation's missing reply route was ignored")
	}
	exec(`UPDATE pool_contacts SET reply_to='reply@example.invalid'`)
	// Simulate a previously initialized campaign containing only the first
	// pool's organization, then ensure the missing organization is appended.
	exec(`INSERT INTO campaign_pool_org_orders(campaign_id,organization_id,dispatch_order) VALUES($1,$2,4)`, campaign, orgPoolAllocationTestHomeOrgID)
	s := &store{db: a.db, queries: a.queries, core: a.core}
	tx, err := a.db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	orders, err := s.ensurePoolOrgOrders(tx, campaign)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(orders, want) {
		t.Fatalf("persisted rotation=%v want=%v", orders, want)
	}
	ordersAgain, err := s.ensurePoolOrgOrders(tx, campaign)
	if err != nil || !slices.Equal(ordersAgain, orders) {
		t.Fatalf("rotation changed: %v %v", ordersAgain, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := a.core.EnsurePoolCampaignRecipients(campaign); err != nil {
		t.Fatal(err)
	}
	rows, err := s.nextPoolCustomers(campaign, campaignSendState{PoolScope: models.CampaignPoolScopeAllOrganizations, SMTPSource: "organization"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	more, err := s.nextPoolCustomers(campaign, campaignSendState{PoolScope: models.CampaignPoolScopeAllOrganizations, SMTPSource: "organization"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	rows = append(rows, more...)
	gotOrgs := make(map[int64]bool)
	for _, row := range rows {
		gotOrgs[row.PoolOrganizationID] = true
		if row.PoolSenderUserID != 0 || row.PoolSenderSMTPUUID == "" {
			t.Fatalf("organization SMTP assignment is invalid: %+v", row)
		}
	}
	if len(rows) != 3 || len(gotOrgs) != 2 {
		t.Fatalf("allocator skipped a selected pool: recipients=%d organizations=%v", len(rows), gotOrgs)
	}
}

func TestPublicPoolSMTPOverviewRequiresPermissionToExpandSavedAudience(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	savedPool := seedOrgPoolAllocationPool(t, a.db, "Saved pool")
	otherPool := seedOrgPoolAllocationPool(t, a.db, "Other pool")
	var campaign int
	if err := a.db.Get(&campaign, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,pool_scope,smtp_source,
		organization_id,owner_user_id)
		VALUES(gen_random_uuid(),'Preview','Subject','sender@example.invalid','Body','email','all_organizations','organization',$1,$2)
		RETURNING id`, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestManagerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_customer_lists(campaign_id,pool_id) VALUES($1,$2)`, campaign, savedPool); err != nil {
		t.Fatal(err)
	}
	user := auth.User{Base: auth.Base{ID: orgPoolAllocationTestManagerUser}, Status: auth.UserStatusEnabled,
		PermissionsMap: map[string]struct{}{auth.PermCampaignsGet: {}, auth.PermMailboxesUse: {}}}
	for _, tc := range []struct {
		name      string
		query     string
		permitted bool
		wantErr   bool
	}{
		{"saved audience", "customer_list_ids=" + strconv.Itoa(savedPool), false, false},
		{"empty subset", "customer_list_ids=", false, false},
		{"omitted audience", "", false, false},
		{"unrelated pool rejected", "customer_list_ids=" + strconv.Itoa(otherPool), false, true},
		{"authorized editor may expand", "customer_list_ids=" + strconv.Itoa(otherPool), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := user
			if tc.permitted {
				actor.PermissionsMap = map[string]struct{}{auth.PermCampaignsGet: {}, auth.PermMailboxesUse: {}, auth.PermCampaignsPublicPoolSend: {}}
			}
			req := httptest.NewRequest(http.MethodGet, "/api/campaigns/1/smtp-overview?source=organization&"+tc.query, nil)
			req.Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
			ctx := echo.New().NewContext(req, httptest.NewRecorder())
			ctx.Set(auth.UserHTTPCtxKey, actor)
			ctx.Set("id", campaign)
			err := a.GetCampaignSMTPOverview(ctx)
			if tc.wantErr {
				if err != auth.ErrPermDenied {
					t.Fatalf("expanded preview error=%v want permission denied", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
