package core

import (
	"encoding/json"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestWorkspacePoolDashboardCountsMatchPoolRows(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)
	org := env.seedOrganization("Current")
	otherOrg := env.seedOrganization("Other")
	firstPool := env.seedPool("First")
	secondPool := env.seedPool("Second")
	first := env.seedContact("A", "A", "a@example.com", "active")
	second := env.seedContact("B", "B", "b@example.com", "active")
	third := env.seedContact("C", "C", "c@example.com", "active")
	for _, pair := range [][2]int64{{int64(firstPool), first}, {int64(firstPool), second},
		{int64(secondPool), first}, {int64(secondPool), third}} {
		env.joinPool(int(pair[0]), pair[1])
	}
	firstAllocation := env.seedAllocation(firstPool, org)
	secondAllocation := env.seedAllocation(secondPool, org)
	otherAllocation := env.seedAllocation(firstPool, otherOrg)
	env.allocate(firstAllocation, first, "active")
	env.allocate(firstAllocation, second, "removed")
	env.allocate(secondAllocation, first, "active")
	env.allocate(otherAllocation, first, "active")
	env.exclude(secondPool, org, secondAllocation, first, false)

	cases := []struct {
		name   string
		access models.WorkspaceAccess
		want   dashboardPoolCounts
	}{
		{"platform", models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}}, dashboardPoolCounts{4, 2, 2}},
		{"organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(org)}}, dashboardPoolCounts{3, 1, 2}},
		{"other organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(otherOrg)}}, dashboardPoolCounts{1, 1, 0}},
		{"personal", models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}}, dashboardPoolCounts{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := env.core.getWorkspacePoolDashboardCounts(tc.access)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("pool counts = %+v, want %+v", got, tc.want)
			}
		})
	}
	listCases := []struct {
		name   string
		access models.WorkspaceAccess
		want   dashboardPoolListCounts
	}{
		{"platform", models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}}, dashboardPoolListCounts{2, 2, 0, 3, 2}},
		{"organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(org)}}, dashboardPoolListCounts{2, 2, 0, 2, 1}},
		{"other organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(otherOrg)}}, dashboardPoolListCounts{1, 1, 0, 1, 1}},
		{"personal", models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}}, dashboardPoolListCounts{}},
	}
	for _, tc := range listCases {
		t.Run("pool lists "+tc.name, func(t *testing.T) {
			got, err := env.core.getWorkspacePoolListDashboardCounts(tc.access)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("pool list counts = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWorkspacePoolListDashboardCountsDescribeBindings(t *testing.T) {
	env := newPoolRecipientsTestEnv(t)
	org := env.seedOrganization("Current")
	otherOrg := env.seedOrganization("Other")
	archivedOrg := env.seedOrganization("Archived")
	env.exec(`UPDATE organizations SET status='archived' WHERE id=$1`, archivedOrg)
	firstPool := env.seedPool("Shared")
	secondPool := env.seedPool("Current only")
	thirdPool := env.seedPool("Authorized without binding")
	fourthPool := env.seedPool("Inactive allocation")
	fifthPool := env.seedPool("Archived organization only")
	archivedPool := env.seedPool("Inactive pool")
	env.exec(`UPDATE customer_lists SET status='archived' WHERE id=$1`, archivedPool)
	env.seedAllocation(firstPool, org)
	env.seedAllocation(firstPool, otherOrg)
	env.seedAllocation(secondPool, org)
	inactiveAllocation := env.seedAllocation(fourthPool, org)
	env.exec(`UPDATE customer_lists SET status='archived'
		WHERE id=(SELECT list_id FROM org_pool_allocations WHERE id=$1)`, inactiveAllocation)
	env.seedAllocation(fifthPool, archivedOrg)
	env.seedAllocation(archivedPool, org)
	env.exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id)
		VALUES($1,$2),($1,$3)`, thirdPool, org, otherOrg)
	// An unbound allocation has no source pool and does not bind any pool.
	listID := env.id(`INSERT INTO customer_lists(name,type) VALUES('Unbound','org_pool_allocation') RETURNING id`)
	env.exec(`INSERT INTO org_pool_allocations(list_id,organization_id) VALUES($1,$2)`, listID, org)

	cases := []struct {
		name   string
		access models.WorkspaceAccess
		want   dashboardPoolListCounts
	}{
		{"platform", models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}}, dashboardPoolListCounts{5, 2, 3, 3, 2}},
		{"organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(org)}}, dashboardPoolListCounts{4, 2, 2, 2, 1}},
		{"other organization", models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(otherOrg)}}, dashboardPoolListCounts{2, 1, 1, 1, 1}},
		{"personal", models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}}, dashboardPoolListCounts{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := env.core.getWorkspacePoolListDashboardCounts(tc.access)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("pool list counts = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWorkspaceDashboardCountsSeparateCustomerGroups(t *testing.T) {
	env := newPoolRecipientsTestEnvWithDDL(t, poolRecipientsTestDDL+`
ALTER TABLE customer_lists ADD COLUMN optin TEXT NOT NULL DEFAULT 'single',
    ADD COLUMN transfer_pending_at TIMESTAMPTZ;
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'enabled',
    organization_id BIGINT,
    owner_user_id INTEGER,
    transfer_pending_at TIMESTAMPTZ
);
CREATE TABLE customer_list_memberships (
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    customer_list_id BIGINT NOT NULL REFERENCES customer_lists(id)
);
ALTER TABLE campaigns ADD COLUMN status TEXT NOT NULL DEFAULT 'draft',
    ADD COLUMN sent INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN owner_user_id INTEGER,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private',
    ADD COLUMN transfer_pending_at TIMESTAMPTZ;
`)
	org := env.seedOrganization("Current")
	other := env.seedOrganization("Other")
	env.exec(`INSERT INTO customer_lists(name,organization_id,owner_user_id) VALUES
		('Visible', $1, 1), ('Hidden', $2, 1)`, org, other)
	env.exec(`INSERT INTO customers(organization_id,owner_user_id) VALUES ($1,1),($2,1)`, org, other)
	env.exec(`INSERT INTO campaigns(organization_id,owner_user_id,sent) VALUES ($1,1,2),($2,1,5)`, org, other)
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: int(org)}, UserID: 1}
	out, err := env.core.GetWorkspaceDashboardCounts(access)
	if err != nil {
		t.Fatal(err)
	}
	var counts struct {
		Customers struct {
			Total int `json:"total"`
		} `json:"customers"`
		PrivateCustomers struct {
			Total int `json:"total"`
		} `json:"private_customers"`
		PoolCustomers dashboardPoolCounts `json:"pool_customers"`
		Campaigns     struct {
			Total int `json:"total"`
		} `json:"campaigns"`
		Messages int `json:"messages"`
	}
	if err := json.Unmarshal(out, &counts); err != nil {
		t.Fatal(err)
	}
	if counts.Customers.Total != 1 || counts.PrivateCustomers.Total != 1 ||
		counts.PoolCustomers.Total != 0 || counts.Campaigns.Total != 1 || counts.Messages != 2 {
		t.Fatalf("workspace dashboard counts leaked or mixed groups: %+v", counts)
	}
}
