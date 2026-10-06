package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/subimporter"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

func TestBusinessPermissionsDoNotInheritMaintenance(t *testing.T) {
	broad := permissionTestUser(auth.PermCustomersManage, auth.PermListManageAll, auth.PermPoolsManage,
		auth.PermTemplatesManage, auth.PermMediaManage, auth.PermCampaignsManageAll)
	for _, permission := range []string{auth.PermListDelete, auth.PermPoolsMasterManage, auth.PermPoolsDeliveryManage,
		auth.PermAssetsShare, auth.PermMailboxesUse, auth.PermMailboxesManage, auth.PermCustomersSensitiveRead} {
		t.Run(permission, func(t *testing.T) {
			if requireLegacyPermission(broad, permission) == nil {
				t.Fatalf("maintenance unexpectedly grants %s", permission)
			}
			if err := requireLegacyPermission(permissionTestUser(permission), permission); err != nil {
				t.Fatal(err)
			}
		})
	}
	if requireCustomerListAction(broad, models.CustomerListTypePrivate, true) == nil ||
		requireCustomerListAction(broad, models.CustomerListTypePool, false) == nil {
		t.Fatal("list deletion or pool master mutation accepted unrelated permissions")
	}
	if requireAssetSharing(broad, "private", "organization") == nil || requireAssetSharing(broad, "global", "private") == nil {
		t.Fatal("publication changes must require sharing")
	}
	if err := requireAssetSharing(broad, "organization", "organization"); err != nil {
		t.Fatalf("unchanged publication should remain editable: %v", err)
	}
}

func TestSensitiveCustomerPermissionAlsoAppliesToOwnersAndManagers(t *testing.T) {
	a := &App{}
	for _, role := range []string{"member", "manager"} {
		access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 7, Role: role}, UserID: 3}
		customer := models.Customer{ResourceScope: permissionTestScope(7, 3, "private", false), Email: "alice@example.com",
			UUID: "private-uuid", Attribs: models.JSON{"phone": "123"}, CustomerLists: []byte(`[{"id":1}]`)}
		a.redactWorkspaceCustomerSensitiveFields(access, map[int]bool{1: true}, &customer, permissionTestUser(auth.PermCustomersManage))
		if customer.Email != "ali**@example.com" || customer.UUID != "" || len(customer.Attribs) != 0 {
			t.Fatalf("%s read bypassed sensitive permission: %+v", role, customer)
		}
		if len(customer.CustomerLists) == 0 {
			t.Fatal("non-sensitive memberships must remain available for edits")
		}
	}
	for _, profile := range []string{`[{"email":"alice@example.com","uuid":"secret","attribs":{"phone":123},"name":"Alice"}]`,
		`{"email":"alice@example.com","uuid":"secret","attribs":{},"name":"Alice"}`} {
		out, err := redactCustomerExportProfile(json.RawMessage(profile))
		if err != nil || strings.Contains(string(out), "email") || strings.Contains(string(out), "uuid") || !strings.Contains(string(out), "Alice") {
			t.Fatalf("redacted profile=%s, err=%v", out, err)
		}
	}
}

func TestMailboxUseAndConfigurationAreIndependent(t *testing.T) {
	e := echo.New()
	a := &App{}
	useOnly := permissionTestUser(auth.PermMailboxesUse)
	c := e.NewContext(httptest.NewRequest("GET", "/api/personal-smtp", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, useOnly)
	for _, handler := range []echo.HandlerFunc{a.GetPersonalSMTP, a.CreateReplyMailbox, a.UpdateReplyMailbox, a.DeleteReplyMailbox, a.TestReplyMailbox} {
		requireOrgPoolAllocationRejection(t, handler(c), http.StatusForbidden)
	}
	configureOnly := permissionTestUser(auth.PermMailboxesManage)
	if requireCampaignMailboxSelection(configureOnly, models.Campaign{}, models.Campaign{SMTPSource: "personal"}) == nil {
		t.Fatal("configuration permission must not permit campaign mailbox selection")
	}
	if err := requireCampaignMailboxSelection(configureOnly, models.Campaign{}, models.Campaign{}); err != nil {
		t.Fatalf("draft without mailbox selection rejected: %v", err)
	}
	mailbox := models.Campaign{SMTPSource: "organization", ReplyMailboxID: null.NewInt(5, true)}
	if err := requireCampaignMailboxSelection(configureOnly, mailbox, mailbox); err != nil {
		t.Fatalf("unchanged mailbox rejected: %v", err)
	}
}

func TestBusinessPermissionsEnforceDatabaseBoundaries(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	e := echo.New()
	master := permissionTestUser(auth.PermPoolsMasterManage, auth.PermWorkspacesPersonal)
	master.ID = orgPoolAllocationTestMemberUser
	poolID := seedOrgPoolAllocationPool(t, a.db, "Delegated pool")
	// Allocation management cannot silently create platform delivery grants.
	allocationUser := permissionTestUser(auth.PermPoolsManage)
	allocationUser.ID = orgPoolAllocationTestMemberUser
	allocationContext, _ := newOrgPoolAllocationTestContext(t, e, allocationUser, orgPoolAllocationTestHomeOrgID,
		poolID, orgPoolAllocationTestHomeOrgID, "Member allocation")
	requireOrgPoolAllocationRejection(t, a.CreateOrgPoolAllocation(allocationContext), http.StatusForbidden)
	if err := a.core.GrantPoolOrganization(poolID, orgPoolAllocationTestHomeOrgID, orgPoolAllocationTestAdminUser); err != nil {
		t.Fatal(err)
	}
	allocationContext, _ = newOrgPoolAllocationTestContext(t, e, allocationUser, orgPoolAllocationTestHomeOrgID,
		poolID, orgPoolAllocationTestHomeOrgID, "Member allocation")
	if err := a.CreateOrgPoolAllocation(allocationContext); err != nil {
		t.Fatalf("authorized member cannot manage its allocation: %v", err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: master.ID, PoolMaster: true}
	if _, err := a.core.RequireManageResource(access, resourceLists, poolID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.GetWorkspaceList(access, poolID); err != nil {
		t.Fatalf("delegated pool read failed: %v", err)
	}
	var privateID int
	if err := a.db.Get(&privateID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'Private owner list','private',$1,'private') RETURNING id`, orgPoolAllocationTestAdminUser); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.RequireManageResource(access, resourceLists, privateID); err == nil {
		t.Fatal("pool master mutated another owner's private list")
	}
	if err := a.core.DeleteListsInWorkspace(access, []int{privateID}); err == nil {
		t.Fatal("pool master transaction bypassed private ownership")
	}
	contact, err := a.core.CreatePoolContact(poolID, models.PoolContact{CustomerCode: "P-1", Name: "Alice", Email: "alice@example.com",
		AllocationDepartment: "Pool Allocation Home"})
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := a.core.QueryPoolContacts(poolID, 0, false, "", "", "id", "asc", 0, 10, true)
	if err != nil || total != 1 {
		t.Fatalf("delegated scope query: total=%d, err=%v", total, err)
	}
	safe, ok := rows.([]models.SafePoolContact)
	if !ok || safe[0].Email == contact.Email {
		t.Fatalf("delegated scope leaked plaintext: %+v", rows)
	}
	manager := permissionTestUser(auth.PermCustomersGetAll, auth.PermCampaignsGetAll)
	manager.ID = orgPoolAllocationTestManagerUser
	c := e.NewContext(httptest.NewRequest("GET", "/api/customers/export", nil), httptest.NewRecorder())
	c.Request().Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
	c.Set(auth.UserHTTPCtxKey, manager)
	requireOrgPoolAllocationRejection(t, a.ExportCustomers(c), http.StatusForbidden)
	requireOrgPoolAllocationRejection(t, a.ExportCustomerData(c), http.StatusForbidden)
	orgAccess, err := a.workspaceAccess(c)
	if err != nil {
		t.Fatal(err)
	}
	requireOrgPoolAllocationRejection(t, a.requireCampaignAnalytics(c, orgAccess, 0), http.StatusForbidden)
	// The deletion handler and the locked mutation both accept only the pool.
	deleteContext := e.NewContext(httptest.NewRequest("DELETE", "/api/customer-lists/"+strconv.Itoa(poolID), nil), httptest.NewRecorder())
	deleteContext.Set("id", poolID)
	deleteContext.Set(auth.UserHTTPCtxKey, master)
	if err := a.DeleteList(deleteContext); err != nil {
		t.Fatalf("delegated pool deletion failed: %v", err)
	}
}

func TestBusinessPermissionsListAndRedactedCustomerEdits(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.importer = subimporter.New(subimporter.Options{}, a.db.DB, a.i18n)
	e := echo.New()
	user := permissionTestUser(auth.PermPoolsMasterManage, auth.PermCustomersManage, auth.PermListManageAll, auth.PermWorkspacesPersonal)
	user.ID = orgPoolAllocationTestMemberUser
	context := func(method, body string, id int) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, "/api/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.UserHTTPCtxKey, user)
		c.Set("id", id)
		return c, rec
	}
	create, rec := context("POST", `{"name":"Delegated master","type":"pool","optin":"single"}`, 0)
	if err := a.CreateList(create); err != nil {
		t.Fatalf("pool master cannot create a list: %v", err)
	}
	var result struct{ Data models.CustomerList }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.OrganizationID.Valid || result.Data.Visibility != "global" {
		t.Fatalf("pool must retain platform scope: %+v", result.Data.ResourceScope)
	}
	update, _ := context("PUT", `{"name":"Updated pool","type":"pool","optin":"single","status":"active"}`, result.Data.ID)
	if err := a.UpdateList(update); err != nil {
		t.Fatalf("pool master cannot edit its list: %v", err)
	}
	var privateID, customerID int
	if err := a.db.Get(&privateID, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'Private','private',$1,'private') RETURNING id`, user.ID); err != nil {
		t.Fatal(err)
	}
	deleteList, _ := context("DELETE", "", privateID)
	requireOrgPoolAllocationRejection(t, a.DeleteList(deleteList), http.StatusForbidden)
	user.PermissionsMap[auth.PermListDelete] = struct{}{}
	deleteList, _ = context("DELETE", "", privateID)
	if err := a.DeleteList(deleteList); err != nil {
		t.Fatalf("explicit deletion grant rejected: %v", err)
	}
	if err := a.db.Get(&customerID, `INSERT INTO customers(uuid,email,name,customer_code,attribs,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'alice@example.com','Alice','C-1','{"phone":"123","region":"CN"}',$1,'private') RETURNING id`, user.ID); err != nil {
		t.Fatal(err)
	}
	edit, rec := context("PUT", `{"name":"Alice updated","customer_code":"C-1","customer_list_ids":[]}`, customerID)
	if err := a.UpdateCustomer(edit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "alice@example.com") || strings.Contains(rec.Body.String(), "123") {
		t.Fatalf("editing leaked hidden fields: %s", rec.Body.String())
	}
	var stored models.Customer
	if err := a.db.Get(&stored, `SELECT email,name,attribs FROM customers WHERE id=$1`, customerID); err != nil {
		t.Fatal(err)
	}
	if stored.Email != "alice@example.com" || stored.Attribs["phone"] != "123" || stored.Attribs["region"] != "CN" || stored.Name != "Alice updated" {
		t.Fatalf("redacted edit lost stored fields: %+v", stored)
	}
}

func TestBusinessPermissionsSharedTemplateDoesNotBypassMediaGrant(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	var templateID, mediaID int
	if err := a.db.Get(&templateID, `INSERT INTO templates(name,subject,body,owner_user_id,visibility)
		VALUES('Shared','','',1,'global') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&mediaID, `INSERT INTO media(uuid,filename,thumb,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'private.png','',1,'private') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO template_media(template_id,media_id) VALUES($1,$2)`, templateID, mediaID); err != nil {
		t.Fatal(err)
	}
	user := permissionTestUser(auth.PermTemplatesGet)
	user.ID = orgPoolAllocationTestMemberUser
	access := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: user.ID}
	c := echo.New().NewContext(httptest.NewRequest("GET", "/", nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, user)
	req := campReq{Campaign: models.Campaign{TemplateID: null.NewInt(templateID, true)}, MediaIDs: []int{mediaID}}
	requireOrgPoolAllocationRejection(t, a.requireUsableCampaignResources(c, access, req), http.StatusForbidden)
	camp := models.Campaign{TemplateID: req.TemplateID}
	requireOrgPoolAllocationRejection(t, a.preloadTestCampaignMedia(c, access, &camp, templateID), http.StatusForbidden)
	user.PermissionsMap[auth.PermMediaGet] = struct{}{}
	c.Set(auth.UserHTTPCtxKey, user)
	if err := a.requireUsableCampaignResources(c, access, req); err != nil {
		t.Fatalf("authorized template association should remain usable: %v", err)
	}
}

func TestSensitiveCustomerPermissionCoversBounceDetails(t *testing.T) {
	a := &App{}
	access := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 3}
	for _, poolID := range []int64{0, 9} {
		bounce := models.Bounce{Email: "alice@example.com", CustomerUUID: "secret", PoolContactID: poolID,
			OwnerUserID: null.NewInt(3, true), Meta: json.RawMessage(`{"email":"alice@example.com"}`)}
		a.redactWorkspaceBounceSensitiveFields(access, &bounce, permissionTestUser(auth.PermBouncesGet))
		if bounce.Email != "" || bounce.CustomerUUID != "" || len(bounce.Meta) != 0 {
			t.Fatalf("bounce disclosed recipient identity: %+v", bounce)
		}
	}
}
