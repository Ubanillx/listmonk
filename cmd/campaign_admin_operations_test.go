package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gopkg.in/volatiletech/null.v6"
)

func TestPlatformAdminCampaignAudienceUsesSavedOwnerWorkspace(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	a.importer = subimporter.New(subimporter.Options{}, a.db.DB, a.i18n)
	a.manager = manager.New(manager.Config{}, nil, a.i18n, a.log)
	seedOrgPoolAllocationFixtures(t, a.db)
	owner := orgPoolAllocationTestManagerUser
	list := seedDeletionList(t, a, "Owner audience", "private", owner)
	otherOwner := seedDeletionList(t, a, "Other audience", "private", orgPoolAllocationTestMemberUser)
	otherOrganization := seedDeletionList(t, a, "Other organization", "private", owner)
	if _, err := a.db.Exec(`UPDATE customer_lists SET organization_id=$1 WHERE id=ANY($2::INT[])`, orgPoolAllocationTestHomeOrgID, pq.Array([]int{list, otherOwner})); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE customer_lists SET organization_id=$1 WHERE id=$2`, orgPoolAllocationTestOtherOrgID, otherOrganization); err != nil {
		t.Fatal(err)
	}
	admin := auth.User{Base: auth.Base{ID: orgPoolAllocationTestAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPut, "/api/campaigns/1", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, admin)
	camp := models.Campaign{ResourceScope: models.ResourceScope{
		OwnerUserID:    null.Int{Int: owner, Valid: true},
		OrganizationID: null.Int{Int: orgPoolAllocationTestHomeOrgID, Valid: true},
	}}
	access := campaignAdminOperationAccess(models.WorkspaceAccess{
		Workspace: models.Workspace{Personal: true, PlatformAdmin: true}, UserID: admin.ID,
	}, camp)
	if access.OrganizationID != orgPoolAllocationTestHomeOrgID || access.Personal {
		t.Fatalf("admin operation access = %+v", access)
	}
	if err := a.requireCampaignAudienceListsForMutation(c, access, camp, []int{list}); err != nil {
		t.Fatalf("saved owner audience: %v", err)
	}
	for _, id := range []int{otherOwner, otherOrganization} {
		if err := a.requireCampaignAudienceListsForMutation(c, access, camp, []int{id}); err == nil {
			t.Fatalf("accepted foreign audience %d", id)
		}
	}
	if err := requireCampaignSendOwnership(c, camp); err != nil {
		t.Fatalf("interactive administrator send: %v", err)
	}
	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,owner_user_id,organization_id,visibility,content_type)
		VALUES(gen_random_uuid(),'Owned campaign','Old subject','sender@example.test','Body','email','draft',$1,$2,'private','plain') RETURNING id`, owner, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"name":"Owned campaign","subject":"Admin updated","from_email":"sender@example.test","content_type":"plain","messenger":"email","customer_list_ids":[%d]}`, list)
	update := echo.New().NewContext(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/campaigns/%d", campaignID), strings.NewReader(body)), httptest.NewRecorder())
	update.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	update.Set(auth.UserHTTPCtxKey, admin)
	update.Set("id", campaignID)
	if err := a.UpdateCampaign(update); err != nil {
		t.Fatalf("administrator updated owner campaign: %v", err)
	}
	var subject string
	if err := a.db.Get(&subject, `SELECT subject FROM campaigns WHERE id=$1`, campaignID); err != nil || subject != "Admin updated" {
		t.Fatalf("updated subject = %q, err = %v", subject, err)
	}
	foreignBody := fmt.Sprintf(`{"name":"Owned campaign","subject":"Wrong audience","from_email":"sender@example.test","content_type":"plain","messenger":"email","customer_list_ids":[%d]}`, otherOwner)
	foreign := echo.New().NewContext(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/campaigns/%d", campaignID), strings.NewReader(foreignBody)), httptest.NewRecorder())
	foreign.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	foreign.Set(auth.UserHTTPCtxKey, admin)
	foreign.Set("id", campaignID)
	if err := a.UpdateCampaign(foreign); err == nil {
		t.Fatal("administrator reassigned the campaign to another owner's audience")
	}
	if err := a.db.Get(&subject, `SELECT subject FROM campaigns WHERE id=$1`, campaignID); err != nil || subject != "Admin updated" {
		t.Fatalf("failed update changed subject = %q, err = %v", subject, err)
	}
	if _, err := a.db.Exec(`UPDATE campaigns SET status='running' WHERE id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	status := echo.New().NewContext(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/campaigns/%d/status", campaignID), strings.NewReader(`{"status":"paused"}`)), httptest.NewRecorder())
	status.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	status.Set(auth.UserHTTPCtxKey, admin)
	status.Set("id", campaignID)
	if err := a.UpdateCampaignStatus(status); err != nil {
		t.Fatalf("administrator paused owner campaign: %v", err)
	}
	var savedStatus string
	if err := a.db.Get(&savedStatus, `SELECT status FROM campaigns WHERE id=$1`, campaignID); err != nil || savedStatus != "paused" {
		t.Fatalf("campaign status = %q, err = %v", savedStatus, err)
	}
	c.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal})
	if err := requireCampaignSendOwnership(c, camp); err == nil {
		t.Fatal("personal API key sent another owner's campaign")
	}
}
