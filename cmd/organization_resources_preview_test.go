package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

func TestPendingResourcesPreviewScopePaginationAndTransfer(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 23; i++ {
		exec(`INSERT INTO customers(uuid,email,name,organization_id,owner_user_id,original_owner_user_id,transfer_pending_at)
			VALUES(gen_random_uuid(),$1,$2,101,NULL,3,NOW())`, fmt.Sprintf("pending%d@example.test", i), fmt.Sprintf("Pending %d", i))
	}
	exec(`INSERT INTO customers(uuid,email,name,organization_id,owner_user_id,transfer_pending_at) VALUES
		(gen_random_uuid(),'pending0@example.test','Existing target',101,2,NULL),
		(gen_random_uuid(),'retained@example.test','Has owner',101,3,NOW()),
		(gen_random_uuid(),'other@example.test','Other organization',102,NULL,NOW()),
		(gen_random_uuid(),'personal@example.test','Personal',NULL,3,NULL),
		(gen_random_uuid(),'unmarked@example.test','Not pending',101,NULL,NULL)`)
	exec(`INSERT INTO customer_lists(uuid,name,type,organization_id,owner_user_id,original_owner_user_id,transfer_pending_at)
		VALUES(gen_random_uuid(),'Pending list','private',101,NULL,3,NOW())`)
	exec(`INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,organization_id,owner_user_id,original_owner_user_id,transfer_pending_at)
		VALUES(gen_random_uuid(),'Pending campaign','Subject','sender@example.test','Body','email',101,NULL,3,NOW())`)
	exec(`INSERT INTO templates(name,subject,body,organization_id,owner_user_id,original_owner_user_id,transfer_pending_at)
		VALUES('Pending template','Subject','Body',101,NULL,3,NOW())`)
	exec(`INSERT INTO media(uuid,filename,thumb,organization_id,owner_user_id,original_owner_user_id,transfer_pending_at)
		VALUES(gen_random_uuid(),'pending.png','',101,NULL,3,NOW())`)
	p, err := a.core.PreviewPendingOrganizationResources(101, 1)
	if err != nil || p.Total != 27 || len(p.Results) != 20 || len(p.Counts) != 5 {
		t.Fatalf("preview=%+v err=%v", p, err)
	}
	for _, row := range p.Results {
		if row.OriginalOwnerName != "member" || row.OriginalOwnerUserID != 3 {
			t.Fatalf("owner=%+v", row)
		}
	}
	next, err := a.core.PreviewPendingOrganizationResources(101, 2)
	if err != nil || len(next.Results) != 7 || next.Results[6].Name != "Pending template" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	clamped, err := a.core.PreviewPendingOrganizationResources(101, 99)
	if err != nil || clamped.Page != 2 {
		t.Fatalf("clamped=%+v err=%v", clamped, err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "@example.test") {
		t.Fatal("preview exposed customer email")
	}
	if err := a.core.TransferPendingOrganizationResources(101, 2); err != nil {
		t.Fatal(err)
	}
	p, err = a.core.PreviewPendingOrganizationResources(101, 1)
	if err != nil || p.Total != 0 || len(p.Results) != 0 {
		t.Fatalf("post-transfer=%+v err=%v", p, err)
	}
	if n := countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE organization_id=101 AND owner_user_id=2`); n != 23 {
		t.Fatalf("merged target customers=%d", n)
	}
	if n := countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE email IN ('retained@example.test','other@example.test','personal@example.test','unmarked@example.test')`); n != 4 {
		t.Fatal("unrelated resources changed")
	}
}

func TestPendingResourcesPreviewAuthorization(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	admin := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}
	manager := permissionTestUser()
	manager.ID = 2
	member := permissionTestUser()
	member.ID = 3
	operator := permissionTestUser(auth.PermOrganizationsPlatformManage)
	operator.ID = 4
	for _, tc := range []struct {
		name   string
		user   auth.User
		org    int
		query  string
		status int
	}{
		{"manager", manager, 101, "", 0}, {"admin", admin, 102, "", 0}, {"platform operator", operator, 102, "", 0},
		{"member", member, 101, "", http.StatusForbidden}, {"nonmember", manager, 102, "", http.StatusForbidden},
		{"archived manager", manager, 103, "", http.StatusConflict}, {"archived admin", admin, 103, "", http.StatusConflict},
		{"personal", admin, 0, "", http.StatusBadRequest}, {"bad page", manager, 101, "?page=-1", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/organizations/resources/pending"+tc.query, nil)
			req.Header.Set(workspaceHeader, strconv.Itoa(tc.org))
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			c.SetPath("/api/organizations/resources/pending")
			c.Set(auth.UserHTTPCtxKey, tc.user)
			err := a.PreviewPendingOrganizationResources(c)
			if tc.status != 0 {
				requireOrgPoolAllocationRejection(t, err, tc.status)
			} else if err != nil || rec.Code != http.StatusOK {
				t.Fatalf("%d %v", rec.Code, err)
			}
		})
	}
}
