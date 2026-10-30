package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
)

func TestPlatformAdminInspectsForeignPrivateList(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 100, PageParam: "page", PerPageParam: "per_page"})
	listID := seedDeletionList(t, a, "Foreign private audience", "private", 3)
	if _, err := a.db.Exec(`WITH moved AS (UPDATE customer_lists SET organization_id=101 WHERE id=$1),
		customer AS (INSERT INTO customers(uuid,email,name,customer_code,owner_user_id,organization_id)
		VALUES(gen_random_uuid(),'inspection@example.test','Inspection customer','INSPECT',3,101) RETURNING id)
		INSERT INTO customer_list_memberships(customer_id,customer_list_id,status) SELECT id,$1,'confirmed' FROM customer`, listID); err != nil {
		t.Fatal(err)
	}
	admin := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}
	for _, endpoint := range []struct {
		name, url, expected string
		handler             echo.HandlerFunc
	}{
		{"metadata", fmt.Sprintf("/api/customer-lists/%d", listID), "Foreign private audience", a.GetList},
		{"members", fmt.Sprintf("/api/customers?customer_list_id=%d", listID), "inspection@example.test", a.QueryCustomers},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, endpoint.url, nil), rec)
			c.Set(auth.UserHTTPCtxKey, admin)
			c.Set("id", listID)
			if err := endpoint.handler(c); err != nil {
				t.Fatalf("administrator's list inspection failed: %v", err)
			}
			if !strings.Contains(rec.Body.String(), endpoint.expected) {
				t.Fatalf("administrator's inspection omitted the target list/customer: %s", rec.Body.String())
			}
			for _, actor := range []struct {
				user auth.User
				key  bool
			}{
				{auth.User{Base: auth.Base{ID: 4}, UserRoleID: 2}, false},
				{admin, true},
			} {
				c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, endpoint.url, nil), httptest.NewRecorder())
				c.Set(auth.UserHTTPCtxKey, actor.user)
				c.Set("id", listID)
				if actor.key {
					c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal})
				}
				if err := endpoint.handler(c); err == nil {
					t.Fatalf("inspection permission leaked to ordinary user or personal key (key=%v)", actor.key)
				}
			}
		})
	}
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/customers", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, admin)
	access, err := a.workspaceAccess(c)
	if err != nil {
		t.Fatal(err)
	}
	access.InspectPrivateLists = true
	if err := a.requireWorkspaceCustomerListIDsForRequest(c, access, []int{listID}, true); err == nil {
		t.Fatal("read-only inspection allowed cross-workspace mutations")
	}
}
