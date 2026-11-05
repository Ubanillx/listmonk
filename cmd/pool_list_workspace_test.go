package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
)

func TestPoolListsStayWithinAllocatedWorkspace(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.cfg = &Config{DBBatchSize: 1000}
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 100, PageParam: "page", PerPageParam: "per_page"})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(q string, args ...any) int {
		t.Helper()
		var out int
		if err := a.db.Get(&out, q, args...); err != nil {
			t.Fatal(err)
		}
		return out
	}
	home := seedOrgPoolAllocationPool(t, a.db, "Home source")
	foreign := seedOrgPoolAllocationPool(t, a.db, "Foreign source")
	grantOnly := seedOrgPoolAllocationPool(t, a.db, "Grant without allocation")
	detached := seedOrgPoolAllocationPool(t, a.db, "Detached source")
	allocate := func(pool, org int, state string) (int, int) {
		list := id(`INSERT INTO customer_lists(uuid,name,type,organization_id,visibility)
			VALUES(gen_random_uuid(),$1,'org_pool_allocation',$2,'organization') RETURNING id`, "Allocation "+strconv.Itoa(org)+" "+strconv.Itoa(pool), org)
		allocation := id(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,state) VALUES($1,$2,$3,$4) RETURNING id`, list, pool, org, state)
		return list, allocation
	}
	homeList, homeAllocation := allocate(home, 101, "active")
	otherList, _ := allocate(foreign, 102, "active")
	sharedOtherList, otherAllocation := allocate(home, 102, "active")
	detachedList, _ := allocate(detached, 101, "detached")
	exec(`INSERT INTO pool_organization_permissions(pool_id,organization_id) VALUES($1,101)`, grantOnly)
	for _, allocation := range []int{homeAllocation, otherAllocation} {
		contact := id(`INSERT INTO pool_contacts(email,name) VALUES(gen_random_uuid()::text||'@example.invalid','Contact') RETURNING id`)
		exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, home, contact)
		exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, allocation, contact)
		org := 101
		if allocation == otherAllocation {
			org = 102
		}
		exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,source,reason)
			VALUES($1,$2,$3,'unsubscribe','test')`, home, org, contact)
		if allocation == homeAllocation {
			exec(`INSERT INTO org_pool_allocation_members(allocation_id,contact_id) VALUES($1,$2)`, otherAllocation, contact)
			exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,source,reason)
				VALUES($1,102,$2,'manual','Other department reason')`, home, contact)
		}
	}
	user := auth.User{Base: auth.Base{ID: 2}, PermissionsMap: map[string]struct{}{auth.PermPoolsGet: {}, auth.PermWorkspacesPersonal: {}}}
	for _, privilege := range []string{"member", auth.PermPoolsMasterManage, auth.PermPoolsDeliveryManage, auth.PermPoolsAllocationPlatform, auth.PermCampaignsPublicPoolSend, "platform-admin"} {
		t.Run(privilege, func(t *testing.T) {
			actor := user
			actor.PermissionsMap = map[string]struct{}{auth.PermPoolsGet: {}, auth.PermPoolsExport: {}, auth.PermWorkspacesPersonal: {}}
			actor.PermissionsMap[privilege] = struct{}{}
			if privilege == "platform-admin" {
				actor.UserRoleID = auth.SuperAdminRoleID
			}
			for _, query := range []string{"type_group=pool&per_page=all", "minimal=true&per_page=all"} {
				req := httptest.NewRequest(http.MethodGet, "/api/customer-lists?"+query, nil)
				req.Header.Set(workspaceHeader, "101")
				rec := httptest.NewRecorder()
				ctx := echo.New().NewContext(req, rec)
				ctx.Set(auth.UserHTTPCtxKey, actor)
				if err := a.GetLists(ctx); err != nil {
					t.Fatal(err)
				}
				var out struct {
					Data struct {
						Results []models.CustomerList `json:"results"`
						Total   int                   `json:"total"`
					} `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				got := make([]int, 0)
				for _, row := range out.Data.Results {
					got = append(got, row.ID)
					if row.ID == home && row.CustomerCount != 1 {
						t.Errorf("source count leaked another department: %d", row.CustomerCount)
					}
					if row.ID == home && query == "type_group=pool&per_page=all" && row.CustomerCounts["unsubscribed"] != 1 {
						t.Errorf("unsubscribe count leaked another department: %v", row.CustomerCounts)
					}
				}
				slices.Sort(got)
				want := []int{home, homeList}
				slices.Sort(want)
				if !slices.Equal(got, want) || out.Data.Total != 2 {
					t.Errorf("%s lists=%v total=%d want=%v; foreign=%d shared-other=%d detached=%d", query, got, out.Data.Total, want, otherList, sharedOtherList, detachedList)
				}
			}
			for _, list := range []int{foreign, grantOnly, otherList, sharedOtherList, detached, detachedList} {
				for _, handler := range []echo.HandlerFunc{a.GetList, a.GetPoolContacts, a.ExportPoolContacts, a.GetOrgPoolAllocations} {
					req := httptest.NewRequest(http.MethodGet, "/api/customer-lists/"+strconv.Itoa(list), nil)
					req.Header.Set(workspaceHeader, "101")
					ctx := echo.New().NewContext(req, httptest.NewRecorder())
					ctx.Set(auth.UserHTTPCtxKey, actor)
					ctx.Set("id", list)
					ctx.SetParamNames("id")
					ctx.SetParamValues(strconv.Itoa(list))
					if err := handler(ctx); err == nil {
						t.Errorf("direct request exposed unallocated list %d", list)
					}
				}
			}
			for _, endpoint := range []struct {
				name, path string
				handler    echo.HandlerFunc
			}{
				{"list contacts", "/api/customer-lists/" + strconv.Itoa(home) + "/pool-contacts", a.GetPoolContacts},
				{"all contacts", "/api/pools/contacts", a.GetAllPoolContacts},
				{"list metadata", "/api/customer-lists/" + strconv.Itoa(home), a.GetList},
			} {
				req := httptest.NewRequest(http.MethodGet, endpoint.path, nil)
				req.Header.Set(workspaceHeader, "101")
				rec := httptest.NewRecorder()
				ctx := echo.New().NewContext(req, rec)
				ctx.Set(auth.UserHTTPCtxKey, actor)
				ctx.Set("id", home)
				ctx.SetParamNames("id")
				ctx.SetParamValues(strconv.Itoa(home))
				if err := endpoint.handler(ctx); err != nil {
					t.Fatalf("%s: %v", endpoint.name, err)
				}
				var out struct {
					Data struct {
						Total         int                  `json:"total"`
						CustomerCount int                  `json:"customer_count"`
						Results       []models.PoolContact `json:"results"`
					} `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.Data.Total+out.Data.CustomerCount != 1 {
					t.Errorf("%s returned another department's customers: %+v", endpoint.name, out.Data)
				}
				for _, row := range out.Data.Results {
					for _, exclusion := range row.Exclusions {
						if exclusion.OrganizationID != 101 {
							t.Errorf("%s leaked another department's exclusion: %+v", endpoint.name, exclusion)
						}
					}
				}
			}
		})
	}

	invoke := func(handler echo.HandlerFunc, list int, actor auth.User) (*httptest.ResponseRecorder, error) {
		req := httptest.NewRequest(http.MethodGet, "/api/customer-lists/"+strconv.Itoa(list)+"?type_group=pool&per_page=all", nil)
		req.Header.Set(workspaceHeader, "101")
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(req, rec)
		ctx.Set(auth.UserHTTPCtxKey, actor)
		ctx.Set("id", list)
		ctx.SetParamNames("id")
		ctx.SetParamValues(strconv.Itoa(list))
		return rec, handler(ctx)
	}
	t.Run("disabled-still-bound", func(t *testing.T) {
		exec(`UPDATE org_pool_allocations SET state='disabled' WHERE id=$1`, homeAllocation)
		defer exec(`UPDATE org_pool_allocations SET state='active' WHERE id=$1`, homeAllocation)
		for _, list := range []int{home, homeList} {
			for _, handler := range []echo.HandlerFunc{a.GetList, a.GetPoolContacts} {
				if _, err := invoke(handler, list, user); err != nil {
					t.Errorf("disabled bound allocation should remain readable: %v", err)
				}
			}
		}
	})
	t.Run("personal-platform-exclusions", func(t *testing.T) {
		rows, _, err := a.core.QueryPoolContacts(home, 0, true, "", "", "", "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		foundShared := false
		for _, row := range rows.([]models.PoolContact) {
			if len(row.Exclusions) == 2 {
				foundShared = true
			}
		}
		if !foundShared {
			t.Error("personal platform source lost its cross-organization exclusion overview")
		}
		rows, _, err = a.core.QueryPoolContacts(homeList, 0, true, "", "", "", "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows.([]models.PoolContact) {
			if len(row.Exclusions) != 1 || row.Exclusions[0].OrganizationID != 101 {
				t.Errorf("allocation exclusion scope changed: %+v", row.Exclusions)
			}
		}
	})
	for _, privilege := range []string{auth.PermPoolsManage, auth.PermPoolsDeliveryManage, auth.PermPoolsAllocationPlatform, auth.PermCampaignsManage} {
		t.Run("metadata-only/"+privilege, func(t *testing.T) {
			actor := user
			actor.PermissionsMap = map[string]struct{}{privilege: {}, auth.PermWorkspacesPersonal: {}}
			for _, list := range []int{home, homeList} {
				rec, err := invoke(a.GetList, list, actor)
				if err != nil {
					t.Errorf("visible metadata rejected: %v", err)
					continue
				}
				var out struct {
					Data models.CustomerList `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.Data.CustomerCount != 0 || len(out.Data.CustomerCounts) != 0 {
					t.Errorf("metadata-only role saw contact counts: %+v", out.Data)
				}
			}
		})
	}
	t.Run("personal-platform-metadata", func(t *testing.T) {
		actor := user
		actor.PermissionsMap = map[string]struct{}{auth.PermPoolsAllocationPlatform: {}, auth.PermWorkspacesPersonal: {}}
		for _, list := range []int{home, homeList, otherList} {
			req := httptest.NewRequest(http.MethodGet, "/api/customer-lists/"+strconv.Itoa(list), nil)
			rec := httptest.NewRecorder()
			ctx := echo.New().NewContext(req, rec)
			ctx.Set(auth.UserHTTPCtxKey, actor)
			ctx.Set("id", list)
			if err := a.GetList(ctx); err != nil {
				t.Errorf("platform directory metadata rejected: %v", err)
				continue
			}
			var out struct {
				Data models.CustomerList `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Data.CustomerCount != 0 || len(out.Data.CustomerCounts) != 0 {
				t.Errorf("platform metadata-only role saw contact counts: %+v", out.Data)
			}
			for _, update := range []string{"status='archived'", "transfer_pending_at=NOW()"} {
				exec(`UPDATE customer_lists SET `+update+` WHERE id=$1`, list)
				rejected := echo.New().NewContext(req, httptest.NewRecorder())
				rejected.Set(auth.UserHTTPCtxKey, actor)
				rejected.Set("id", list)
				if err := a.GetList(rejected); err == nil {
					t.Errorf("personal platform metadata escaped directory: list %d %s", list, update)
				}
				exec(`UPDATE customer_lists SET status='active',transfer_pending_at=NULL WHERE id=$1`, list)
			}
		}
	})
	for _, pendingList := range []int{home, homeList} {
		t.Run("pending-transfer/"+strconv.Itoa(pendingList), func(t *testing.T) {
			exec(`UPDATE customer_lists SET transfer_pending_at=NOW() WHERE id=$1`, pendingList)
			defer exec(`UPDATE customer_lists SET transfer_pending_at=NULL WHERE id=$1`, pendingList)
			actor := user
			actor.UserRoleID = auth.SuperAdminRoleID
			for _, list := range []int{home, homeList} {
				for _, handler := range []echo.HandlerFunc{a.GetList, a.GetPoolContacts, a.ExportPoolContacts, a.GetOrgPoolAllocations} {
					if _, err := invoke(handler, list, actor); err == nil {
						t.Errorf("pending-transfer list %d exposed through list %d", pendingList, list)
					}
				}
			}
			for _, handler := range []echo.HandlerFunc{a.GetLists, a.GetAllPoolContacts, a.GetAllPoolContactFilters} {
				rec, err := invoke(handler, home, actor)
				if err != nil {
					t.Fatal(err)
				}
				var out struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if len(out.Data) > 0 && out.Data[0] == '[' {
					if string(out.Data) != "[]" {
						t.Errorf("pending-transfer contacts exposed in filter options: %s", out.Data)
					}
				} else {
					var page models.PageResults
					if err := json.Unmarshal(out.Data, &page); err != nil {
						t.Fatal(err)
					}
					if page.Total != 0 {
						t.Errorf("pending-transfer data exposed: %s", out.Data)
					}
				}
			}
		})
	}
}
