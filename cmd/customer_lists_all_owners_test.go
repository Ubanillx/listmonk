package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
)

func TestPlatformPrivateListIndexDoesNotWidenWorkspaceSelectors(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 50, PageParam: "page", PerPageParam: "per_page", AllowAll: true})
	seedOrgPoolAllocationFixtures(t, a.db)
	admin := auth.User{Base: auth.Base{ID: orgPoolAllocationTestAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	personal := seedDeletionList(t, a, "Personal list", "private", orgPoolAllocationTestMemberUser)
	organization := seedDeletionList(t, a, "Organization list", "private", orgPoolAllocationTestManagerUser)
	pool := seedDeletionList(t, a, "Pool list", "pool", orgPoolAllocationTestAdminUser)
	if _, err := a.db.Exec(`UPDATE customer_lists SET organization_id=$1 WHERE id=$2`, orgPoolAllocationTestHomeOrgID, organization); err != nil {
		t.Fatal(err)
	}

	request := func(user auth.User, url string) (int, []models.CustomerList) {
		t.Helper()
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, url, nil), rec)
		c.Set(auth.UserHTTPCtxKey, user)
		if err := a.GetLists(c); err != nil {
			if httpErr, ok := err.(*echo.HTTPError); ok {
				return httpErr.Code, nil
			}
			t.Fatal(err)
		}
		var response struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data) == 0 || response.Data[0] == '[' {
			return rec.Code, nil
		}
		var page struct {
			Results []models.CustomerList `json:"results"`
		}
		if err := json.Unmarshal(response.Data, &page); err != nil {
			t.Fatal(err)
		}
		return rec.Code, page.Results
	}

	url := "/api/customer-lists?minimal=true&type_group=private&status=active"
	if code, lists := request(admin, url); code != http.StatusOK || len(lists) != 0 {
		t.Fatalf("default personal selector: status=%d lists=%v", code, lists)
	}
	if code, lists := request(admin, url+"&scope=all_owners"); code != http.StatusOK || len(lists) != 2 || lists[0].ID != organization || lists[1].ID != personal {
		t.Fatalf("platform private index: status=%d lists=%v (pool %d must be excluded)", code, lists, pool)
	}
	if code, lists := request(admin, "/api/customer-lists?type_group=private&scope=all_owners&query=Organization&per_page=1"); code != http.StatusOK || len(lists) != 1 || lists[0].ID != organization {
		t.Fatalf("filtered full index: status=%d lists=%v", code, lists)
	}
	if code, _ := request(admin, "/api/customer-lists?minimal=true&type_group=pool&scope=all_owners"); code != http.StatusForbidden {
		t.Fatalf("pool index with private scope: status=%d", code)
	}
	member := auth.User{Base: auth.Base{ID: orgPoolAllocationTestMemberUser}, UserRoleID: 2}
	if code, _ := request(member, url+"&scope=all_owners"); code != http.StatusForbidden {
		t.Fatalf("non-admin platform scope: status=%d", code)
	}
}
