package main

import (
	"slices"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
)

// TestReadableWorkspaceCampaignsApplyLegacyGrantsBeforePagination locks in the
// old Go-side filtering rules: a restricted user must not learn about hidden
// campaigns through the total or through page boundaries, while published,
// manager-readable and audience-less rows stay readable.
func TestReadableWorkspaceCampaignsApplyLegacyGrantsBeforePagination(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	newList := func(name string, owner int) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO customer_lists(uuid,name,type,status,organization_id,owner_user_id,visibility)
			VALUES(gen_random_uuid(),$1,'private','active',$2,$3,'private') RETURNING id`,
			name, orgPoolAllocationTestHomeOrgID, owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newCampaign := func(name, body, visibility string, owner int, age string, lists ...int) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility,created_at)
			VALUES(gen_random_uuid(),$1,'Subject','sender@example.test',$2,'email','draft',$3,$4,$5,now() - $6::interval) RETURNING id`,
			name, body, owner, orgPoolAllocationTestHomeOrgID, visibility, age); err != nil {
			t.Fatal(err)
		}
		for _, listID := range lists {
			exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name) VALUES($1,$2,$3)`,
				id, listID, "audience")
		}
		return id
	}

	grantedList := newList("Granted", orgPoolAllocationTestMemberUser)
	otherList := newList("Ungranted", orgPoolAllocationTestMemberUser)

	readable := newCampaign("Readable granted", "BODY-GRANTED", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, "4 minutes", grantedList)
	hidden := newCampaign("Hidden audience", "BODY-HIDDEN", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, "3 minutes", otherList)
	noAudience := newCampaign("Fresh draft", "BODY-DRAFT", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, "2 minutes")
	shared := newCampaign("Organization shared", "BODY-SHARED", models.ResourceVisibilityOrganization, orgPoolAllocationTestManagerUser, "1 minute", otherList)
	otherOwner := newCampaign("Other owner", "BODY-OTHER", models.ResourceVisibilityPrivate, orgPoolAllocationTestOutsiderUser, "0 minutes", grantedList)

	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: orgPoolAllocationTestHomeOrgID, Role: models.OrganizationMemberRoleMember},
		UserID:    orgPoolAllocationTestMemberUser,
	}
	scope := core.CampaignReadScope{CustomerListIDs: []int{grantedList}}

	camps, total, err := a.core.QueryReadableWorkspaceCampaigns(member, scope, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	assertCampaignIDs(t, camps, []int{shared, noAudience, readable})
	for _, camp := range camps {
		if camp.Body != "" || camp.BodySource.Valid {
			t.Fatalf("campaign %d returned a body with no_body=true", camp.ID)
		}
	}

	// The total must count every readable row, not just the requested page.
	page, total, err := a.core.QueryReadableWorkspaceCampaigns(member, scope, "", nil, nil, "created_at", "desc", 0, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("page total = %d, want 3", total)
	}
	assertCampaignIDs(t, page, []int{shared, noAudience})
	page, total, err = a.core.QueryReadableWorkspaceCampaigns(member, scope, "", nil, nil, "created_at", "desc", 2, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("second page total = %d, want 3", total)
	}
	assertCampaignIDs(t, page, []int{readable})

	// Full list grants still cannot cross the workspace's owner boundary.
	all, total, err := a.core.QueryReadableWorkspaceCampaigns(member, core.CampaignReadScope{AllCustomerLists: true}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("all-grant total = %d, want 4", total)
	}
	assertCampaignIDs(t, all, []int{shared, noAudience, hidden, readable})
	for _, camp := range all {
		if camp.ID == otherOwner {
			t.Fatal("another owner's private campaign leaked into the member workspace")
		}
	}

	// Organization managers read every campaign in their organization.
	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: orgPoolAllocationTestHomeOrgID, Role: models.OrganizationMemberRoleManager},
		UserID:    orgPoolAllocationTestManagerUser,
	}
	managed, total, err := a.core.QueryReadableWorkspaceCampaigns(manager, core.CampaignReadScope{}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("manager total = %d, want 5", total)
	}
	if len(managed) != 5 {
		t.Fatalf("manager rows = %d, want 5", len(managed))
	}
}

// TestCampaignAnalyticsIDsRequireStrictAudienceGrants covers the report
// boundary that the list endpoint relaxes: a non-manager owner still needs the
// audience-list grant for an organization/global-published campaign, while
// managers and full-grant callers do not.
func TestCampaignAnalyticsIDsRequireStrictAudienceGrants(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	home := orgPoolAllocationTestHomeOrgID

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	newList := func(name string, owner int, org any) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO customer_lists(uuid,name,type,status,organization_id,owner_user_id,visibility)
			VALUES(gen_random_uuid(),$1,'private','active',$2,$3,'private') RETURNING id`,
			name, org, owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newCampaign := func(name, visibility string, owner int, org any, age string, lists ...int) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility,created_at)
			VALUES(gen_random_uuid(),$1,'Subject','sender@example.test','Body','email','draft',$2,$3,$4,now() - $5::interval) RETURNING id`,
			name, owner, org, visibility, age); err != nil {
			t.Fatal(err)
		}
		for _, listID := range lists {
			exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name) VALUES($1,$2,$3)`,
				id, listID, "audience")
		}
		return id
	}

	granted := newList("Granted", orgPoolAllocationTestMemberUser, home)
	ungranted := newList("Ungranted", orgPoolAllocationTestMemberUser, home)
	personalGrantedList := newList("Personal granted", orgPoolAllocationTestMemberUser, nil)
	personalUngrantedList := newList("Personal ungranted", orgPoolAllocationTestMemberUser, nil)

	shared := newCampaign("Shared ungranted", models.ResourceVisibilityOrganization, orgPoolAllocationTestMemberUser, home, "6 minutes", ungranted)
	private := newCampaign("Private granted", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "5 minutes", granted)
	draft := newCampaign("Draft", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "4 minutes")
	otherOwned := newCampaign("Other owner shared", models.ResourceVisibilityOrganization, orgPoolAllocationTestOutsiderUser, home, "3 minutes", granted)
	globalUngranted := newCampaign("Global ungranted", models.ResourceVisibilityGlobal, orgPoolAllocationTestMemberUser, home, "2 minutes", ungranted)
	personalGranted := newCampaign("Personal granted", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "90 seconds", personalGrantedList)
	personalUngranted := newCampaign("Personal ungranted", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "60 seconds", personalUngrantedList)
	personalDraft := newCampaign("Personal draft", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "30 seconds")

	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: home, Role: models.OrganizationMemberRoleMember},
		UserID:    orgPoolAllocationTestMemberUser,
	}
	ids, err := a.core.QueryCampaignAnalyticsIDs(member, core.CampaignReadScope{CustomerListIDs: []int{granted}})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{private, draft})

	// The full-grant bypass drops the legacy audience filter but keeps the
	// owner boundary: another owner's shared campaign stays out.
	ids, err = a.core.QueryCampaignAnalyticsIDs(member, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{shared, private, draft, globalUngranted})

	// Empty grants exercise the empty-array placeholder path.
	ids, err = a.core.QueryCampaignAnalyticsIDs(member, core.CampaignReadScope{})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{draft})

	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: home, Role: models.OrganizationMemberRoleManager},
		UserID:    orgPoolAllocationTestManagerUser,
	}
	ids, err = a.core.QueryCampaignAnalyticsIDs(manager, core.CampaignReadScope{})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{shared, private, draft, otherOwned, globalUngranted})

	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: orgPoolAllocationTestMemberUser}
	ids, err = a.core.QueryCampaignAnalyticsIDs(personal, core.CampaignReadScope{CustomerListIDs: []int{personalGrantedList}})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{personalGranted, personalDraft})
	ids, err = a.core.QueryCampaignAnalyticsIDs(personal, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{personalGranted, personalUngranted, personalDraft})

	platform := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: orgPoolAllocationTestAdminUser}
	ids, err = a.core.QueryCampaignAnalyticsIDs(platform, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{shared, private, draft, otherOwned, globalUngranted, personalGranted, personalUngranted, personalDraft})
}

func TestCampaignReadScopeMapping(t *testing.T) {
	platformAdmin := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: 9}
	if got := campaignReadScope(platformAdmin, permissionTestUser()); !got.AllCustomerLists || got.CustomerListIDs != nil {
		t.Fatalf("platform admin scope = %+v", got)
	}
	if got := campaignReadScope(models.WorkspaceAccess{UserID: 9}, permissionTestUser(auth.PermCampaignsGetAll)); !got.AllCustomerLists {
		t.Fatalf("campaigns:get_all scope = %+v", got)
	}
	if got := campaignReadScope(models.WorkspaceAccess{UserID: 9}, permissionTestUser(auth.PermCustomersGetAll)); !got.AllCustomerLists {
		t.Fatalf("customers:get_all scope = %+v", got)
	}

	granted := permissionTestUser()
	granted.GetCustomerListIDs = []int{7}
	granted.ManageCustomerListIDs = []int{3}
	if got := campaignReadScope(models.WorkspaceAccess{UserID: 9}, granted); got.AllCustomerLists ||
		!slices.Equal(got.CustomerListIDs, []int{3, 7}) {
		t.Fatalf("granted scope = %+v", got)
	}
	if got := campaignReadScope(models.WorkspaceAccess{UserID: 9}, permissionTestUser()); got.AllCustomerLists || len(got.CustomerListIDs) != 0 {
		t.Fatalf("no-grant scope = %+v", got)
	}
}

func TestReadableWorkspaceCampaignsWorkspaceShapesAndPageEdges(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	home := orgPoolAllocationTestHomeOrgID

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	newList := func(name string, owner int, org any, typ string) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO customer_lists(uuid,name,type,status,organization_id,owner_user_id,visibility)
			VALUES(gen_random_uuid(),$1,$2::customer_list_type,'active',$3,$4,'private') RETURNING id`,
			name, typ, org, owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newCampaign := func(name, body, visibility string, owner int, org any, age, tags string, lists ...int) int {
		t.Helper()
		var id int
		if err := a.db.Get(&id, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility,created_at,tags)
			VALUES(gen_random_uuid(),$1,'Subject','sender@example.test',$2,'email','draft',$3,$4,$5,now() - $6::interval,$7::varchar(100)[]) RETURNING id`,
			name, body, owner, org, visibility, age, tags); err != nil {
			t.Fatal(err)
		}
		for _, listID := range lists {
			exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name) VALUES($1,$2,$3)`,
				id, listID, "audience")
		}
		return id
	}

	orgList := newList("Org audience", orgPoolAllocationTestMemberUser, home, "private")
	otherList := newList("Other org audience", orgPoolAllocationTestMemberUser, home, "private")
	personalGrantedList := newList("Personal granted", orgPoolAllocationTestMemberUser, nil, "private")
	personalOtherList := newList("Personal other", orgPoolAllocationTestMemberUser, nil, "private")
	poolList := newList("Pool audience", orgPoolAllocationTestMemberUser, home, "pool")

	orgGranted := newCampaign("Org granted", "BODY-ORG", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "6 minutes", "{}", orgList)
	pending := newCampaign("Pending transfer", "BODY-PENDING", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "5 minutes", "{}", orgList)
	exec(`UPDATE campaigns SET transfer_pending_at=NOW() WHERE id=$1`, pending)
	personalGranted := newCampaign("Personal granted", "BODY-PERSONAL", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "4 minutes", "{}", personalGrantedList)
	personalHidden := newCampaign("Personal hidden", "BODY-PERSONAL-HIDDEN", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "3 minutes", "{}", personalOtherList)
	personalDraft := newCampaign("Personal draft", "BODY-PERSONAL-DRAFT", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, nil, "2 minutes", "{}")
	global := newCampaign("Global notice", "BODY-GLOBAL", models.ResourceVisibilityGlobal, orgPoolAllocationTestAdminUser, nil, "1 minute", "{}", otherList)
	poolOnly := newCampaign("Pool only", "BODY-POOL", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "30 seconds", "{}")
	exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_name,pool_id) VALUES($1,'Pool audience',$2)`, poolOnly, poolList)
	tagged := newCampaign("Tagged search target", "BODY-TAGGED", models.ResourceVisibilityPrivate, orgPoolAllocationTestMemberUser, home, "10 seconds", "{vip}", orgList)

	personal := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: orgPoolAllocationTestMemberUser}
	camps, total, err := a.core.QueryReadableWorkspaceCampaigns(personal,
		core.CampaignReadScope{CustomerListIDs: []int{personalGrantedList}}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("personal total = %d, want 3", total)
	}
	assertCampaignIDs(t, camps, []int{global, personalDraft, personalGranted})

	// NoBody=false returns the stored bodies.
	camps, _, err = a.core.QueryReadableWorkspaceCampaigns(personal,
		core.CampaignReadScope{CustomerListIDs: []int{personalGrantedList}}, "", nil, nil, "created_at", "desc", 0, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, camp := range camps {
		if camp.Body == "" {
			t.Fatalf("campaign %d has no body with no_body=false", camp.ID)
		}
	}

	// Negative offsets clamp to zero and limit=0 means every readable row.
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(personal,
		core.CampaignReadScope{CustomerListIDs: []int{personalGrantedList}}, "", nil, nil, "created_at", "desc", -10, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("negative-offset total = %d, want 3", total)
	}
	assertCampaignIDs(t, camps, []int{global, personalDraft})
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(personal,
		core.CampaignReadScope{CustomerListIDs: []int{personalGrantedList}}, "", nil, nil, "created_at", "desc", 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(camps) != 3 {
		t.Fatalf("limit=0 returned total=%d rows=%d, want 3/3", total, len(camps))
	}

	// The full-grant bypass still respects the personal workspace boundary.
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(personal, core.CampaignReadScope{AllCustomerLists: true}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("personal all-grant total = %d, want 4", total)
	}
	assertCampaignIDs(t, camps, []int{global, personalDraft, personalHidden, personalGranted})

	// No grants only expose audience-less drafts and published exceptions.
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(personal, core.CampaignReadScope{}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("no-grant total = %d, want 2", total)
	}
	assertCampaignIDs(t, camps, []int{global, personalDraft})

	member := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: home, Role: models.OrganizationMemberRoleMember},
		UserID:    orgPoolAllocationTestMemberUser,
	}
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member,
		core.CampaignReadScope{CustomerListIDs: []int{orgList}}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("member total = %d, want 3", total)
	}
	assertCampaignIDs(t, camps, []int{tagged, global, orgGranted})

	// Pending-transfer rows are invisible to members, while pool-only audiences
	// need the full-grant bypass.
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member, core.CampaignReadScope{}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("member no-grant total = %d, want 1", total)
	}
	assertCampaignIDs(t, camps, []int{global})
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member, core.CampaignReadScope{AllCustomerLists: true}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("member all-grant total = %d, want 4", total)
	}
	assertCampaignIDs(t, camps, []int{tagged, poolOnly, global, orgGranted})

	manager := models.WorkspaceAccess{
		Workspace: models.Workspace{OrganizationID: home, Role: models.OrganizationMemberRoleManager},
		UserID:    orgPoolAllocationTestManagerUser,
	}
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(manager, core.CampaignReadScope{}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("manager total = %d, want 5", total)
	}
	assertCampaignIDs(t, camps, []int{tagged, poolOnly, global, pending, orgGranted})

	platform := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}, UserID: orgPoolAllocationTestAdminUser}
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(platform, core.CampaignReadScope{AllCustomerLists: true}, "", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 8 || len(camps) != 8 {
		t.Fatalf("platform total = %d rows = %d, want 8/8", total, len(camps))
	}

	// Analytics ID resolution applies the owner/manager boundary in SQL.
	ids, err := a.core.QueryCampaignAnalyticsIDs(member, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{orgGranted, poolOnly, tagged})
	ids, err = a.core.QueryCampaignAnalyticsIDs(manager, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{orgGranted, pending, poolOnly, tagged})
	ids, err = a.core.QueryCampaignAnalyticsIDs(personal, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCampaignIDSet(t, ids, []int{personalGranted, personalHidden, personalDraft})
	ids, err = a.core.QueryCampaignAnalyticsIDs(platform, core.CampaignReadScope{AllCustomerLists: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 8 {
		t.Fatalf("platform analytics ids = %d, want 8", len(ids))
	}

	// Status, tag and search filters run inside the same permission predicate.
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member,
		core.CampaignReadScope{CustomerListIDs: []int{orgList}}, "", []string{"finished"}, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(camps) != 0 {
		t.Fatalf("status filter returned total=%d rows=%d, want 0/0", total, len(camps))
	}
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member,
		core.CampaignReadScope{CustomerListIDs: []int{orgList}}, "", nil, []string{"vip"}, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("tag filter total = %d, want 1", total)
	}
	assertCampaignIDs(t, camps, []int{tagged})
	camps, total, err = a.core.QueryReadableWorkspaceCampaigns(member,
		core.CampaignReadScope{CustomerListIDs: []int{orgList}}, "Tagged search", nil, nil, "created_at", "desc", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("search total = %d, want 1", total)
	}
	assertCampaignIDs(t, camps, []int{tagged})

	// QueryWorkspaceCampaigns keeps workspace-only semantics (no legacy grants).
	camps, total, err = a.core.QueryWorkspaceCampaigns(personal, "", nil, nil, "created_at", "desc", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("workspace-only personal total = %d, want 4", total)
	}
}

func assertCampaignIDSet(t *testing.T, got, want []int) {
	t.Helper()
	got = slices.Clone(got)
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("campaign ids = %v, want %v", got, want)
	}
}

func assertCampaignIDs(t *testing.T, camps models.Campaigns, want []int) {
	t.Helper()
	if len(camps) != len(want) {
		t.Fatalf("campaign count = %d, want %d", len(camps), len(want))
	}
	for i := range want {
		if camps[i].ID != want[i] {
			t.Fatalf("campaign[%d] = %d, want %d", i, camps[i].ID, want[i])
		}
	}
}
