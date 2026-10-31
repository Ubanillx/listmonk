package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

// These comparisons run against an installed throwaway PostgreSQL database.
// Date equality is excluded, single-opt-in subscriptions are retained, and
// platform-wide customer counts include several owners and organizations.
func TestMaintenancePreviewMatchesDeletes(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	before := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO customer_lists(id,uuid,name,type,optin,owner_user_id,organization_id) VALUES
		(701,gen_random_uuid(),'Double','private','double',3,101),
		(702,gen_random_uuid(),'Single','private','single',3,101)`)
	for i, status := range []string{"enabled", "blocklisted", "enabled", "enabled"} {
		exec(`INSERT INTO customers(id,uuid,email,name,status,owner_user_id,organization_id)
			VALUES($1,gen_random_uuid(),$2,'Preview',$3,$4,$5)`, 701+i, string(rune('a'+i))+"@example.test", status, 3+i%2, 101+i%2)
	}
	exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id,status,created_at) VALUES
		(701,701,'unconfirmed',$1::timestamptz-INTERVAL '1 second'),
		(702,701,'unconfirmed',$1), (703,702,'unconfirmed',$1::timestamptz-INTERVAL '1 day'),
		(703,701,'confirmed',$1::timestamptz-INTERVAL '1 day')`, before)
	preview, err := a.core.PreviewMaintenance("subscriptions", "unconfirmed", before)
	if err != nil || preview.Count != 1 {
		t.Fatalf("subscription preview=%+v err=%v", preview, err)
	}
	n, err := a.core.DeleteUnconfirmedSubscriptions(before)
	if err != nil || n != preview.Count {
		t.Fatalf("subscription delete=%d err=%v", n, err)
	}
	preview, err = a.core.PreviewMaintenance("customers", "blocklisted", time.Time{})
	if err != nil || preview.Count != 1 {
		t.Fatalf("blocklist preview=%+v err=%v", preview, err)
	}
	n, err = a.core.DeleteBlocklistedCustomers()
	if err != nil || n != preview.Count {
		t.Fatalf("blocklist delete=%d err=%v", n, err)
	}
	preview, err = a.core.PreviewMaintenance("customers", "orphan", time.Time{})
	if err != nil || preview.Count != 2 {
		t.Fatalf("orphan preview=%+v err=%v", preview, err)
	}
	n, err = a.core.DeleteOrphanCustomers()
	if err != nil || n != preview.Count {
		t.Fatalf("orphan delete=%d err=%v", n, err)
	}
	exec(`INSERT INTO campaigns(id,uuid,name,subject,from_email,body,messenger) VALUES(701,gen_random_uuid(),'Preview','Subject','sender@example.test','Body','email')`)
	exec(`INSERT INTO links(id,uuid,url) VALUES(701,gen_random_uuid(),'https://example.test')`)
	exec(`INSERT INTO campaign_views(campaign_id,created_at) VALUES(701,$1::timestamptz-INTERVAL '1 second'),(701,$1),(701,$1::timestamptz+INTERVAL '1 second')`, before)
	exec(`INSERT INTO link_clicks(link_id,created_at) VALUES(701,$1::timestamptz-INTERVAL '1 day'),(701,$1)`, before)
	preview, err = a.core.PreviewMaintenance("analytics", "all", before)
	if err != nil || preview.Count != 2 || preview.Views != 1 || preview.Clicks != 1 {
		t.Fatalf("analytics preview=%+v err=%v", preview, err)
	}
	if p, err := a.core.PreviewMaintenance("analytics", "clicks", before); err != nil || p.Views != 0 || p.Count != 1 {
		t.Fatalf("clicks preview=%+v %v", p, err)
	}
	if err := a.core.DeleteCampaignViews(before); err != nil {
		t.Fatal(err)
	}
	if err := a.core.DeleteCampaignLinkClicks(before); err != nil {
		t.Fatal(err)
	}
	preview, err = a.core.PreviewMaintenance("analytics", "all", before)
	if err != nil || preview.Count != 0 {
		t.Fatalf("post-delete preview=%+v err=%v", preview, err)
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM campaign_views`) != 2 || countRows(t, a.db, `SELECT COUNT(*) FROM link_clicks`) != 1 {
		t.Fatal("date boundary removed")
	}
}

func TestMaintenancePreviewValidationAndPermission(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	for _, tc := range []struct{ dataset, typ, query string }{
		{"analytics", "all", ""}, {"analytics", "bogus", "?before_date=2026-10-01T00:00:00Z"},
		{"customers", "all", ""}, {"subscriptions", "unconfirmed", "?before_date=invalid"},
	} {
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/"+tc.query, nil), httptest.NewRecorder())
		c.SetParamNames("dataset", "type")
		c.SetParamValues(tc.dataset, tc.typ)
		requireOrgPoolAllocationRejection(t, a.PreviewMaintenance(c), http.StatusBadRequest)
	}
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, permissionTestUser(auth.PermCustomersManage))
	requireOrgPoolAllocationRejection(t, (&auth.Auth{}).Perm(a.PreviewMaintenance, "settings:maintain")(c), http.StatusForbidden)
}
