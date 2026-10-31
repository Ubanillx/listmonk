package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestPrivateCustomerStatusFiltersMatchBulkAndExport(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if _, err := f.a.db.Exec(`WITH blocked AS (UPDATE customers SET status='blocklisted' WHERE id=$1)
		UPDATE customer_list_memberships SET status='unsubscribed' WHERE customer_id=$2`, f.customers[0].ID, f.customers[1].ID); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{UserID: 3, Workspace: models.Workspace{OrganizationID: 101, Role: models.OrganizationMemberRoleMember}}
	for _, tc := range []struct {
		status string
		count  int
	}{{"all", 2}, {"enabled", 0}, {"blocklisted", 1}, {"unsubscribed", 1}, {"disabled", 0}} {
		rows, total, err := f.a.core.QueryWorkspaceCustomers(access, "", []int{f.listID}, "", "id", "asc", 0, 1, tc.status)
		if err != nil || total != tc.count || len(rows) != min(tc.count, 1) {
			t.Fatalf("%s page=%+v total=%d err=%v", tc.status, rows, total, err)
		}
		if tc.status == "unsubscribed" && !rows[0].Unsubscribed {
			t.Fatal("unsubscribe badge data disagrees with its filter")
		}
		if (tc.status == "blocklisted" && rows[0].ID != f.customers[0].ID) || (tc.status == "unsubscribed" && rows[0].ID != f.customers[1].ID) {
			t.Fatalf("%s returned the wrong customer: %+v", tc.status, rows)
		}
		ids, err := f.a.core.GetWorkspaceCustomerIDs(access, "", []int{f.listID}, "", tc.status)
		if err != nil || len(ids) != tc.count {
			t.Fatalf("%s bulk IDs=%v err=%v", tc.status, ids, err)
		}
		export, err := f.a.core.ExportWorkspaceCustomers(access, "", []int{f.listID}, []int{f.customers[0].ID, f.customers[1].ID}, "", 10, tc.status)
		if err != nil {
			t.Fatal(err)
		}
		out, err := export()
		if err != nil || len(out) != tc.count {
			t.Fatalf("%s export count=%d err=%v", tc.status, len(out), err)
		}
	}
	if _, _, err := f.a.core.QueryWorkspaceCustomers(access, "", nil, "", "", "", 0, 20, "not-a-status"); err == nil {
		t.Fatal("invalid contact status was accepted")
	}
	// The public handler must forward the new parameter, with page totals intact.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/customers?customer_list_id=%d&contact_status=blocklisted", f.listID), nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID})
	if err := f.a.QueryCustomers(c); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Data.Total != 1 {
		t.Fatalf("API filter total=%d err=%v", response.Data.Total, err)
	}
	// A recipient can remain subscribed to another list after opting out here.
	other := seedDeletionList(t, f.a, "Still subscribed", "private", 3)
	if _, err := f.a.db.Exec(`WITH scoped AS (UPDATE customer_lists SET organization_id=101 WHERE id=$1)
		INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) VALUES($2,$1,'confirmed')`, other, f.customers[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, total, err := f.a.core.QueryWorkspaceCustomers(access, "", []int{other}, "", "", "", 0, 20, "unsubscribed"); err != nil || total != 0 {
		t.Fatalf("unrelated list unsubscribe affected current-list filter: %d %v", total, err)
	}
	rows, _, err := f.a.core.QueryWorkspaceCustomers(access, "", []int{other}, "", "", "", 0, 20, "all")
	if err != nil || len(rows) != 1 || rows[0].Unsubscribed {
		t.Fatalf("current-list badge included another list's unsubscribe: %+v %v", rows, err)
	}
	body, _ := json.Marshal(map[string]any{"all": true, "customer_list_ids": []int{f.listID}, "contact_status": "blocklisted"})
	req = httptest.NewRequest(http.MethodPost, "/api/customers/bulk/delete", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(workspaceHeader, "101")
	c = echo.New().NewContext(req, httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID})
	if err := f.a.DeleteCustomersByFilter(c); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := f.a.db.Get(&preserved, `SELECT NOT EXISTS(SELECT 1 FROM customers WHERE id=$1)
		AND EXISTS(SELECT 1 FROM customers WHERE id=$2)`, f.customers[0].ID, f.customers[1].ID); err != nil || !preserved {
		t.Fatalf("filtered bulk delete changed untouched recipients: %v %v", preserved, err)
	}
}

func TestPoolCustomerStatusFiltersRespectUnsubscribeOrganization(t *testing.T) {
	f := newUnsubscribeFixture(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.a.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(query string, args ...any) int64 {
		t.Helper()
		var result int64
		if err := f.a.db.Get(&result, query, args...); err != nil {
			t.Fatal(err)
		}
		return result
	}
	pool := seedOrgPoolAllocationPool(t, f.a.db, "Contact state pool")
	allocations := map[int]int64{}
	lists := map[int]int64{}
	for _, org := range []int{101, 102} {
		lists[org] = id(`INSERT INTO customer_lists(uuid,name,type,owner_user_id,organization_id)
			VALUES(gen_random_uuid(),$1,'org_pool_allocation',3,$2) RETURNING id`, fmt.Sprint(org), org)
		allocations[org] = id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,$3) RETURNING id`, lists[org], pool, org)
	}
	for i, status := range []string{"active", "blocklisted", "active", "archived", "active"} {
		contact := id(`INSERT INTO pool_contacts(email,name,customer_code,status) VALUES($1,$2,$2,$3) RETURNING id`, fmt.Sprintf("status%d@example.test", i), fmt.Sprintf("STATE-%d", i), status)
		exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, contact)
		for _, org := range []int{101, 102} {
			exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, allocations[org], contact)
		}
		if i == 2 || i == 4 {
			source := "unsubscribe"
			if i == 4 {
				source = "manual"
			}
			exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,source)
				VALUES($1,101,$2,$3,$4,$4)`, pool, contact, allocations[101], source)
		}
	}
	for _, tc := range []struct {
		status string
		count  int
	}{{"all", 5}, {"active", 1}, {"blocklisted", 1}, {"unsubscribed", 1}, {"removed", 2}, {"archived", 1}} {
		rows, total, err := f.a.core.QueryAllPoolContacts(0, true, tc.status, "", int64(pool), nil, "id", "asc", 0, 1)
		if err != nil || total != tc.count || len(rows.([]models.PoolContact)) != 1 {
			t.Fatalf("aggregate %s total=%d rows=%+v err=%v", tc.status, total, rows, err)
		}
		_, total, err = f.a.core.QueryPoolContacts(pool, 0, true, tc.status, "", "id", "asc", 0, 20)
		if err != nil || total != tc.count {
			t.Fatalf("single pool %s total=%d err=%v", tc.status, total, err)
		}
	}
	for _, org := range []int{101, 102} {
		rows, total, err := f.a.core.QueryAllPoolContacts(org, false, "unsubscribed", "", int64(pool), nil, "id", "asc", 0, 20)
		want := 1
		if org == 102 {
			want = 0
		}
		if err != nil || total != want {
			t.Fatalf("organization %d unsubscribe scope: total=%d err=%v", org, total, err)
		}
		if want == 1 {
			row := rows.([]models.SafePoolContact)[0]
			if !row.Unsubscribed || row.Email == "status2@example.test" {
				t.Fatal("safe status DTO lost unsubscribe or exposed raw email")
			}
		}
		_, total, err = f.a.core.QueryPoolContacts(int(lists[org]), org, true, "unsubscribed", "", "id", "asc", 0, 20)
		if err != nil || total != want {
			t.Fatalf("allocation %d unsubscribe scope: total=%d err=%v", org, total, err)
		}
	}
	checkCounts := func(org int, admin, master bool, wantPool, want101, want102 int) {
		t.Helper()
		rows := []models.CustomerList{
			{Base: models.Base{ID: pool}, Type: models.CustomerListTypePool},
			{Base: models.Base{ID: int(lists[101])}, Type: models.CustomerListTypeOrgPoolAllocation},
			{Base: models.Base{ID: int(lists[102])}, Type: models.CustomerListTypeOrgPoolAllocation},
			{Base: models.Base{ID: f.listID}, Type: models.CustomerListTypePublic, CustomerCounts: models.StringIntMap{"confirmed": 2}},
		}
		if err := f.a.core.EnrichPoolListUnsubscribeCounts(rows, org, admin, master); err != nil {
			t.Fatal(err)
		}
		for i, want := range []int{wantPool, want101, want102} {
			if got := rows[i].CustomerCounts["unsubscribed"]; got != want {
				t.Fatalf("org=%d admin=%v master=%v list=%d count=%d want=%d", org, admin, master, rows[i].ID, got, want)
			}
		}
		if rows[3].CustomerCounts["confirmed"] != 2 {
			t.Fatal("pool counters changed private-list statistics")
		}
	}
	checkCounts(0, true, false, 1, 1, 0)
	checkCounts(101, false, false, 1, 1, 0)
	checkCounts(102, false, false, 0, 0, 0)
	checkCounts(0, false, true, 1, 0, 0)

	// The list index exposes the same count, without widening metadata-only grants.
	exec(`UPDATE customer_lists SET visibility='global', owner_user_id=1 WHERE id=$1`, pool)
	for _, canRead := range []bool{true, false} {
		user := permissionTestUser(auth.PermPoolsMasterManage, auth.PermWorkspacesPersonal)
		if canRead {
			user = permissionTestUser(auth.PermPoolsMasterManage, auth.PermWorkspacesPersonal, auth.PermPoolsGet)
		}
		user.ID = orgPoolAllocationTestMemberUser
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/customer-lists?type_group=pool", nil), rec)
		ctx.Set(auth.UserHTTPCtxKey, user)
		if err := f.a.GetLists(ctx); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Data struct {
				Results []models.CustomerList `json:"results"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range response.Data.Results {
			if row.ID != pool {
				continue
			}
			found = true
			if canRead && row.CustomerCounts["unsubscribed"] != 1 {
				t.Fatalf("list API omitted unsubscribe count: %+v", row)
			}
			if !canRead && (len(row.CustomerCounts) != 0 || row.CustomerCount != 0) {
				t.Fatalf("metadata-only grant exposed customer counts: %+v", row)
			}
		}
		if !found {
			t.Fatalf("authorized pool missing from list API: %s", rec.Body.String())
		}
	}
	// Opting out in two organizations still counts one source-pool customer.
	exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,allocation_id,reason,source)
		SELECT pool_id,102,contact_id,$2,'unsubscribe','unsubscribe' FROM org_pool_allocation_exclusions
		WHERE pool_id=$1 AND source='unsubscribe'`, pool, allocations[102])
	checkCounts(0, true, false, 1, 1, 1)
	checkCounts(102, false, false, 1, 0, 1)
	exec(`UPDATE org_pool_allocation_exclusions SET restored_at=NOW() WHERE pool_id=$1 AND source='unsubscribe'`, pool)
	checkCounts(0, true, false, 0, 0, 0)
}
