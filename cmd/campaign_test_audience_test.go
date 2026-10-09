package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

func TestCampaignTestAudienceUsesPoolDeliveryAccess(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.manager = manager.New(manager.Config{}, nil, a.i18n, a.log)
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
	allocatedPool := seedOrgPoolAllocationPool(t, a.db, "Allocated public pool")
	grantedPool := seedOrgPoolAllocationPool(t, a.db, "Granted public pool")
	otherPool := seedOrgPoolAllocationPool(t, a.db, "Other organization's public pool")
	list := func(kind string, org int) int {
		return id(`INSERT INTO customer_lists(uuid,name,type,organization_id,owner_user_id)
			VALUES(gen_random_uuid(),'Audience',$1,$2,$3) RETURNING id`, kind, org, orgPoolAllocationTestManagerUser)
	}
	allocationList := list(models.CustomerListTypeOrgPoolAllocation, orgPoolAllocationTestHomeOrgID)
	exec(`INSERT INTO org_pool_allocations(list_id,pool_id,organization_id) VALUES($1,$2,$3)`,
		allocationList, allocatedPool, orgPoolAllocationTestHomeOrgID)
	if err := a.core.GrantPoolOrganization(grantedPool, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestAdminUser); err != nil {
		t.Fatal(err)
	}
	privateList := list(models.CustomerListTypePrivate, orgPoolAllocationTestHomeOrgID)
	otherPrivateList := list(models.CustomerListTypePrivate, orgPoolAllocationTestOtherOrgID)
	campaign := func(scope string) int {
		return id(`INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,pool_scope,organization_id,owner_user_id)
			VALUES(gen_random_uuid(),'Test audience','Subject','sender@example.invalid','Body','email',$1,$2,$3) RETURNING id`,
			scope, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestManagerUser)
	}
	organizationCampaign := campaign(models.CampaignPoolScopeOrganization)
	allOrgCampaign := campaign(models.CampaignPoolScopeAllOrganizations)
	member := permissionTestUser(auth.PermCampaignsManage, auth.PermCampaignsTest, auth.PermMailboxesUse)
	member.ID = orgPoolAllocationTestManagerUser
	admin := auth.User{Base: auth.Base{ID: member.ID}, UserRoleID: auth.SuperAdminRoleID}
	for _, tc := range []struct {
		name         string
		user         auth.User
		campaign     int
		lists        []int
		requestScope string
		wantCode     int
		wantMessage  string
	}{
		{name: "admin selects global pool", user: admin, campaign: organizationCampaign, lists: []int{allocatedPool}},
		{name: "member uses allocation without master permission", user: member, campaign: organizationCampaign, lists: []int{allocatedPool}},
		{name: "member uses delivery grant without master permission", user: member, campaign: organizationCampaign, lists: []int{grantedPool}},
		{name: "ungranted pool rejected", user: member, campaign: organizationCampaign, lists: []int{otherPool}, wantCode: http.StatusForbidden},
		{name: "request cannot widen saved scope", user: member, campaign: organizationCampaign, lists: []int{otherPool},
			requestScope: models.CampaignPoolScopeAllOrganizations, wantCode: http.StatusForbidden},
		{name: "admin cannot attach cross workspace private list", user: admin, campaign: organizationCampaign, lists: []int{otherPrivateList},
			wantCode: http.StatusForbidden, wantMessage: "a selected customer_list is outside the active workspace"},
		{name: "mixed pool and local private audience", user: admin, campaign: organizationCampaign, lists: []int{allocatedPool, privateList}},
		{name: "allocation list rejected", user: admin, campaign: organizationCampaign, lists: []int{allocationList}, wantCode: http.StatusBadRequest,
			wantMessage: "campaign audiences must use a first-level public pool, not a pool allocation"},
		{name: "all organizations uses saved scope", user: admin, campaign: allOrgCampaign, lists: []int{otherPool}},
		{name: "all organizations rejects private audience", user: admin, campaign: allOrgCampaign, lists: []int{privateList}, wantCode: http.StatusBadRequest,
			wantMessage: "an all-organization public pool campaign accepts public pool audiences only"},
		{name: "request cannot downgrade saved scope", user: admin, campaign: allOrgCampaign, lists: []int{allocatedPool, privateList},
			requestScope: models.CampaignPoolScopeOrganization, wantCode: http.StatusBadRequest,
			wantMessage: "an all-organization public pool campaign accepts public pool audiences only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"name": "Test audience", "subject": "Subject", "from_email": "Sender <sender@example.invalid>",
				"content_type": "plain", "body": "Body", "customer_list_ids": tc.lists, "pool_scope": tc.requestScope,
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/campaigns/1/test", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
			ctx := echo.New().NewContext(req, httptest.NewRecorder())
			ctx.Set(auth.UserHTTPCtxKey, tc.user)
			ctx.Set("id", tc.campaign)
			err = a.TestCampaign(ctx)
			// Accepted audiences reach the next validation. Omit test recipients
			// deliberately so this permission test cannot send any messages.
			code, message := tc.wantCode, tc.wantMessage
			if code == 0 {
				code, message = http.StatusBadRequest, a.i18n.T("campaigns.noSubsToTest")
			}
			httpErr, ok := err.(*echo.HTTPError)
			if !ok || httpErr.Code != code || (message != "" && httpErr.Message != message) {
				t.Fatalf("TestCampaign() = %v, want %d %q", err, code, message)
			}
		})
	}
}
