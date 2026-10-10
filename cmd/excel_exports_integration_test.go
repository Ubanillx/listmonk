package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

func TestExcelCustomerExportsKeepOwnershipAndSensitivePermissionPostgres(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.cfg = &Config{DBBatchSize: 1}
	a.cfg.Privacy.Exportable = map[string]bool{"profile": true, "subscriptions": true}
	var ownID, otherID int
	for _, fixture := range []struct {
		id    *int
		owner int
		code  string
		email string
	}{
		{&ownID, orgPoolAllocationTestMemberUser, "00001234567890123456", "alice@example.invalid"},
		{&otherID, orgPoolAllocationTestManagerUser, "OTHER", "hidden@example.invalid"},
	} {
		if err := a.db.Get(fixture.id, `INSERT INTO customers(uuid,name,customer_code,email,attribs,owner_user_id,organization_id)
			VALUES(gen_random_uuid(),'Alice',$1,$2,'{"company":"Example","phone":"private-phone"}',$3,$4) RETURNING id`,
			fixture.code, fixture.email, fixture.owner, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.db.Exec(`INSERT INTO settings(key,value) VALUES('customer.custom_fields',
		'[{"key":"company","label":"Company","type":"text","active":true}]') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	user := permissionTestUser(auth.PermCustomersGetAll, auth.PermCustomersExport)
	user.ID = orgPoolAllocationTestMemberUser
	request := func(path string, id int, user auth.User) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.Set(auth.UserHTTPCtxKey, user)
		c.Set("id", id)
		return c, rec
	}
	for _, sensitive := range []bool{false, true} {
		if sensitive {
			user.PermissionsMap[auth.PermCustomersSensitiveRead] = struct{}{}
		}
		for _, single := range []bool{false, true} {
			c, rec := request("/api/customers/export", ownID, user)
			handler := a.ExportCustomers
			if single {
				handler = a.ExportCustomerData
			}
			if err := handler(c); err != nil {
				t.Fatalf("sensitive=%t single=%t: %v", sensitive, single, err)
			}
			file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := file.GetRows(file.GetSheetName(0))
			file.Close()
			if err != nil || len(rows) != 2 || rows[1][0] != "00001234567890123456" {
				t.Fatalf("ownership or identifier changed: %v, %v", rows, err)
			}
			encoded := strings.Join(rows[1], " ")
			if strings.Contains(encoded, "hidden@example.invalid") || strings.Contains(encoded, "OTHER") {
				t.Fatalf("another owner's customer leaked: %v", rows)
			}
			if sensitive != strings.Contains(encoded, "alice@example.invalid") || sensitive != strings.Contains(encoded, "private-phone") {
				t.Fatalf("sensitive boundary changed: %v", rows)
			}
			if sensitive && !strings.Contains(strings.Join(rows[0], " "), "Company") {
				t.Fatalf("custom field remained inside JSON: %v", rows)
			}
		}
	}
	c, rec := request("/api/customers/"+strconv.Itoa(otherID)+"/export", otherID, user)
	if err := a.ExportCustomerData(c); err == nil || rec.Body.Len() > 0 {
		t.Fatalf("cross-owner privacy export: err=%v body=%d", err, rec.Body.Len())
	}
}

func TestExcelPoolExportsKeepSelectionAndMaskingPostgres(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.cfg = &Config{DBBatchSize: 1}
	poolID := seedOrgPoolAllocationPool(t, a.db, "Excel source pool")
	admin := auth.User{Base: auth.Base{ID: orgPoolAllocationTestAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	c, _ := newOrgPoolAllocationTestContext(t, echo.New(), admin, 0, poolID, orgPoolAllocationTestHomeOrgID, "Excel allocation")
	if err := a.CreateOrgPoolAllocation(c); err != nil {
		t.Fatal(err)
	}
	contact, err := a.core.CreatePoolContact(poolID, models.PoolContact{CustomerCode: "000001", Name: "Alice", Email: "alice@example.invalid", ReplyTo: "reply@example.invalid", AllocationDepartment: "Pool Allocation Home"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.CreatePoolContact(poolID, models.PoolContact{CustomerCode: "UNSELECTED", Name: "Bob", Email: "bob@example.invalid", AllocationDepartment: "Pool Allocation Home"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO org_pool_allocation_exclusions(pool_id,organization_id,contact_id,reason)
		VALUES($1,$2,$3,'Customer requested removal')`, poolID, orgPoolAllocationTestHomeOrgID, contact.ID); err != nil {
		t.Fatal(err)
	}
	user := permissionTestUser(auth.PermPoolsGet, auth.PermPoolsExport)
	user.ID = orgPoolAllocationTestMemberUser
	for _, aggregate := range []bool{false, true} {
		selectionPoolID := 0
		if aggregate {
			selectionPoolID = poolID
		}
		path := "/api/pools/contacts/export?contact=" + strconv.Itoa(selectionPoolID) + ":" + strconv.FormatInt(contact.ID, 10)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, path, nil), rec)
		c.Request().Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
		c.Set(auth.UserHTTPCtxKey, user)
		c.SetParamNames("id")
		c.SetParamValues(strconv.Itoa(poolID))
		handler := a.ExportPoolContacts
		if aggregate {
			handler = a.ExportAllPoolContacts
		}
		if err := handler(c); err != nil {
			t.Fatal(err)
		}
		file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := file.GetRows(file.GetSheetName(0))
		file.Close()
		if err != nil || len(rows) != 2 {
			t.Fatalf("selection changed: %v, %v", rows, err)
		}
		data := strings.Join(rows[1], " ")
		if strings.Contains(data, "alice@example.invalid") || strings.Contains(data, "UNSELECTED") || !strings.Contains(data, "000001") || !strings.Contains(data, "reply@example.invalid") {
			t.Fatalf("pool selection/masking/reply address changed: %v", rows)
		}
		if !strings.Contains(data, "Customer requested removal") || !strings.Contains(data, "Removed (this organization)") {
			t.Fatalf("removed customer appears active in the export: %v", rows)
		}
	}
}
