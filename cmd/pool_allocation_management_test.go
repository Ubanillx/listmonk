package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/migrations"
	"github.com/labstack/echo/v4"
)

func TestAllocationRequestDecoder(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"valid", `{"action":"disable","expected_version":1}`, true},
		{"unknown field", `{"action":"disable","pool_id":1}`, false},
		{"second JSON value", `{"action":"disable"} {}`, false},
		{"malformed", `{"action":`, false},
		{"oversized trailing body", `{"action":"disable"}` + strings.Repeat(" ", 64*1024) + `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tc.body)), httptest.NewRecorder())
			var in core.AllocationChange
			err := decodeAllocationBody(ctx, &in)
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed {
				requireOrgPoolAllocationRejection(t, err, http.StatusBadRequest)
			}
		})
	}
}

func TestAllocationWorkbenchBoundaries(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	db := a.db
	seedOrgPoolAllocationFixtures(t, db)
	pool := seedOrgPoolAllocationPool(t, db, "Workbench source")
	allocation, err := a.core.CreateOrgPoolAllocation(pool, orgPoolAllocationTestHomeOrgID, "Home allocation", nil, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.core.CreateOrgPoolAllocation(pool, orgPoolAllocationTestOtherOrgID, "Other allocation", nil, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	admin := core.AllocationAccess{UserID: 1, Manage: true, Across: true, Read: true, Audit: true, Highest: true}
	delegate := core.AllocationAccess{UserID: 4, Manage: true, Across: true, Read: true, Audit: true}
	manager := core.AllocationAccess{UserID: 2, OrganizationID: orgPoolAllocationTestHomeOrgID, Manage: true, Read: true, Audit: true}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := db.Get(&n, q, args...); err != nil {
			t.Fatal(err)
		}
		return n
	}
	contact := id(`INSERT INTO pool_contacts(customer_code,name,email,allocation_department,reply_to) VALUES('C001','Test company','private@example.com','Pool Allocation Home','route@example.com') RETURNING id`)
	exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, contact)
	for _, target := range []int64{allocation.ID, other.ID} {
		if err := a.core.ChangeAllocationMembers(admin, target, "assign", "", []int64{contact}); err != nil {
			t.Fatal(err)
		}
	}
	change := func(action string) error {
		d, err := a.core.GetAllocationDetail(admin, allocation.ID)
		if err != nil {
			return err
		}
		return a.core.ChangeAllocation(admin, d.ID, core.AllocationChange{Action: action, ExpectedVersion: d.Version, Reason: "QA reason", ConfirmOrganization: d.OrganizationName})
	}
	t.Run("creation never grants delivery even for highest administrator", func(t *testing.T) {
		if n := countRows(t, db, `SELECT COUNT(*) FROM pool_organization_permissions WHERE pool_id=$1`, pool); n != 0 {
			t.Fatalf("implicit grants: %d", n)
		}
	})
	t.Run("own organization boundary", func(t *testing.T) {
		_, err := a.core.GetAllocationDetail(manager, other.ID)
		requireOrgPoolAllocationRejection(t, err, 403)
	})
	t.Run("cross organization relationship only", func(t *testing.T) {
		if _, err := a.core.GetAllocationDetail(delegate, other.ID); err != nil {
			t.Fatal(err)
		}
		noRead := delegate
		noRead.Read = false
		_, _, err := a.core.AllocationContacts(noRead, other.ID, "active", "", 0, 20)
		requireOrgPoolAllocationRejection(t, err, 403)
	})
	t.Run("masked DTO and candidate scope", func(t *testing.T) {
		rows, n, err := a.core.AllocationContacts(delegate, allocation.ID, "active", "C001", 0, 20)
		if err != nil || n != 1 || len(rows) != 1 || rows[0].Email == "private@example.com" {
			t.Fatalf("unsafe rows %+v total %d err %v", rows, n, err)
		}
		_, n, err = a.core.AllocationContacts(delegate, other.ID, "candidates", "", 0, 20)
		if err != nil || n != 0 {
			t.Fatalf("candidate scope %d %v", n, err)
		}
	})
	t.Run("batch rollback and source validation", func(t *testing.T) {
		err := a.core.ChangeAllocationMembers(manager, allocation.ID, "remove", "batch", []int64{contact, 999999})
		requireOrgPoolAllocationRejection(t, err, 400)
		if n := countRows(t, db, `SELECT COUNT(*) FROM org_pool_allocation_members WHERE allocation_id=$1 AND status='active'`, allocation.ID); n != 1 {
			t.Fatal("partial batch committed")
		}
	})
	t.Run("organization scoped remove and restore", func(t *testing.T) {
		if err := a.core.ChangeAllocationMembers(manager, allocation.ID, "remove", "Transferred to private", []int64{contact}); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, db, `SELECT COUNT(*) FROM org_pool_allocation_members WHERE allocation_id=$1 AND status='active'`, other.ID); n != 1 {
			t.Fatal("other organization changed")
		}
		if n := countRows(t, db, `SELECT COUNT(*) FROM pool_contacts WHERE id=$1 AND email='private@example.com' AND name='Test company'`, contact); n != 1 {
			t.Fatal("master data changed")
		}
		if err := a.core.ChangeAllocationMembers(admin, allocation.ID, "assign", "", []int64{contact}); err == nil {
			t.Fatal("assign silently restored removal")
		}
		exec(`UPDATE pool_contacts SET status='blocklisted' WHERE id=$1`, contact)
		if err := a.core.ChangeAllocationMembers(manager, allocation.ID, "restore", "", []int64{contact}); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, db, `SELECT COUNT(*) FROM pool_contacts WHERE id=$1 AND status='blocklisted'`, contact); n != 1 {
			t.Fatal("restore removed blacklist")
		}
		exec(`UPDATE pool_contacts SET status='active' WHERE id=$1`, contact)
	})
	t.Run("optimistic update and immutable ownership", func(t *testing.T) {
		d, _ := a.core.GetAllocationDetail(admin, allocation.ID)
		if err := a.core.ChangeAllocation(admin, d.ID, core.AllocationChange{Action: "update", Name: "Updated allocation", Description: "Notes", ExpectedVersion: d.Version}); err != nil {
			t.Fatal(err)
		}
		err := a.core.ChangeAllocation(admin, d.ID, core.AllocationChange{Action: "update", Name: "stale", ExpectedVersion: d.Version})
		requireOrgPoolAllocationRejection(t, err, 409)
	})
	t.Run("atomic audit failure rolls back update", func(t *testing.T) {
		d, _ := a.core.GetAllocationDetail(admin, allocation.ID)
		exec(`ALTER TABLE audit_events RENAME TO audit_events_qa`)
		err := a.core.ChangeAllocation(admin, d.ID, core.AllocationChange{Action: "update", Name: "must rollback", ExpectedVersion: d.Version})
		exec(`ALTER TABLE audit_events_qa RENAME TO audit_events`)
		if err == nil {
			t.Fatal("audit failure ignored")
		}
		after, _ := a.core.GetAllocationDetail(admin, d.ID)
		if after.Name != d.Name || after.Version != d.Version {
			t.Fatal("mutation survived failed audit")
		}
	})
	campaign := id(`INSERT INTO campaigns(uuid,name,subject,from_email,body,messenger,status,organization_id) VALUES(gen_random_uuid(),'QA','QA','qa@example.com','test','email','running',$1) RETURNING id`, orgPoolAllocationTestHomeOrgID)
	exec(`INSERT INTO campaign_customer_lists(campaign_id,customer_list_name,pool_id,source_organization_id) VALUES($1,'source',$2,$3)`, campaign, pool, orgPoolAllocationTestHomeOrgID)
	exec(`INSERT INTO campaign_pool_recipients(campaign_id,pool_contact_id,pool_id,allocation_id,organization_id,email_snapshot,status) VALUES($1,$2,$3,$4,$5,'private@example.com','queued')`, campaign, contact, pool, allocation.ID, orgPoolAllocationTestHomeOrgID)
	store := &store{db: db}
	t.Run("sender rechecks grants and lifecycle after queue", func(t *testing.T) {
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("ungranted delivery allowed")
		}
		if err := a.core.GrantPoolOrganization(pool, orgPoolAllocationTestHomeOrgID, 1); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := change("disable"); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("disabled queued recipient delivered")
		}
		if err := a.core.ChangeAllocationMembers(admin, allocation.ID, "remove", "disabled", []int64{contact}); err == nil {
			t.Fatal("disabled membership mutated")
		}
		if n := countRows(t, db, `SELECT COUNT(*) FROM pool_organization_permissions WHERE pool_id=$1`, pool); n != 1 {
			t.Fatal("disabling changed grant")
		}
		if err := change("enable"); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := a.core.RevokePoolOrganization(pool, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("revoked queued recipient delivered")
		}
	})
	t.Run("queued recipients respect transfers removal blocklist and independent all organization policy", func(t *testing.T) {
		if err := a.core.GrantPoolOrganization(pool, orgPoolAllocationTestHomeOrgID, 1); err != nil {
			t.Fatal(err)
		}
		for _, listID := range []int{pool, allocation.ListID} {
			exec(`UPDATE customer_lists SET transfer_pending_at=NOW() WHERE id=$1`, listID)
			if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
				t.Fatal("transfer-pending queued recipient delivered")
			}
			exec(`UPDATE customer_lists SET transfer_pending_at=NULL WHERE id=$1`, listID)
		}
		if err := a.core.ChangeAllocationMembers(admin, allocation.ID, "remove", "sender QA", []int64{contact}); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("removed queued recipient delivered")
		}
		if err := a.core.ChangeAllocationMembers(admin, allocation.ID, "restore", "", []int64{contact}); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE pool_contacts SET status='blocklisted' WHERE id=$1`, contact)
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("blocklisted queued recipient delivered")
		}
		exec(`UPDATE pool_contacts SET status='active' WHERE id=$1`, contact)
		exec(`DELETE FROM pool_members WHERE pool_id=$1 AND contact_id=$2`, pool, contact)
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("recipient no longer in source pool delivered")
		}
		exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, pool, contact)
		exec(`UPDATE campaigns SET pool_scope='all_organizations' WHERE id=$1`, campaign)
		if err := a.core.RevokePoolOrganization(pool, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err != nil {
			t.Fatalf("independent platform delivery policy was lost: %v", err)
		}
		if err := change("disable"); err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateCampaignReplyRoute(int(campaign), contact, "route@example.com"); err == nil {
			t.Fatal("all organization delivery bypassed disabled allocation")
		}
		if err := change("enable"); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE campaigns SET pool_scope='organization' WHERE id=$1`, campaign)
	})
	t.Run("unfinished campaigns block detach", func(t *testing.T) {
		if err := change("detach"); err == nil {
			t.Fatal("active campaign detached")
		}
		n, err := a.core.AllocationImpact(admin, allocation.ID)
		if err != nil || n != 1 {
			t.Fatalf("impact %d %v", n, err)
		}
	})
	t.Run("detach and rebind preserve history and ids", func(t *testing.T) {
		exec(`UPDATE campaigns SET status='finished' WHERE id=$1`, campaign)
		if err := change("detach"); err != nil {
			t.Fatal(err)
		}
		if err := change("rebind"); err != nil {
			t.Fatal(err)
		}
		d, err := a.core.GetAllocationDetail(admin, allocation.ID)
		if err != nil || d.State != "disabled" || d.ListID != allocation.ListID {
			t.Fatalf("rebind %+v %v", d, err)
		}
		if n := countRows(t, db, `SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$1 AND allocation_id=$2`, campaign, allocation.ID); n != 1 {
			t.Fatal("history removed")
		}
	})
	t.Run("restricted audit scope", func(t *testing.T) {
		rows, _, err := a.core.AllocationEvents(delegate, allocation.ID, 0, 100)
		if err != nil || len(rows) < 5 {
			t.Fatalf("audit %+v %v", rows, err)
		}
		for _, row := range rows {
			if fmt.Sprint(row.Metadata["allocation_id"]) != strconv.FormatInt(allocation.ID, 10) {
				t.Fatalf("foreign event %+v", row)
			}
		}
		denied := delegate
		denied.Audit = false
		_, _, err = a.core.AllocationEvents(denied, allocation.ID, 0, 20)
		requireOrgPoolAllocationRejection(t, err, 403)
	})
	t.Run("migration is idempotent and grants no permissions", func(t *testing.T) {
		for range 2 {
			if err := migrations.V6_61_0(db, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		d, _ := a.core.GetAllocationDetail(admin, allocation.ID)
		if d.State != "disabled" {
			t.Fatal("upgrade reset lifecycle")
		}
	})
	t.Run("membership rechecked after locking", func(t *testing.T) {
		tx, err := db.Beginx()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			result <- a.core.ChangeAllocationMembers(manager, allocation.ID, "remove", "race", []int64{contact})
		}()
		time.Sleep(80 * time.Millisecond)
		if _, err := tx.Exec(`UPDATE organization_members SET removed_at=NOW() WHERE organization_id=$1 AND user_id=2`, orgPoolAllocationTestHomeOrgID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("stale member wrote")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("mutation deadlocked")
		}
	})
}

func TestAllocationHTTPActionBoundaries(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	pool := seedOrgPoolAllocationPool(t, a.db, "HTTP source")
	d, err := a.core.CreateOrgPoolAllocation(pool, orgPoolAllocationTestOtherOrgID, "HTTP allocation", nil, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		permissions []string
		body        map[string]any
		want        int
	}{
		{"delivery cannot manage allocations", []string{auth.PermPoolsDeliveryManage}, map[string]any{"action": "disable", "expected_version": 1}, 403},
		{"master cannot manage allocations", []string{auth.PermPoolsMasterManage}, map[string]any{"action": "disable", "expected_version": 1}, 403},
		{"foreign organization denied", []string{auth.PermPoolsManage}, map[string]any{"action": "disable", "expected_version": 1}, 403},
		{"unknown ownership fields rejected", []string{auth.PermPoolsAllocationPlatform}, map[string]any{"action": "update", "name": "bad", "organization_id": 101, "expected_version": 1}, 400},
		{"cross organization delegate allowed", []string{auth.PermPoolsAllocationPlatform}, map[string]any{"action": "disable", "expected_version": 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := map[string]struct{}{}
			for _, v := range tc.permissions {
				p[v] = struct{}{}
			}
			user := auth.User{Base: auth.Base{ID: 2}, PermissionsMap: p}
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPut, "/api/pools/allocations/"+strconv.FormatInt(d.ID, 10)+"/manage", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set(workspaceHeader, "101")
			ctx := echo.New().NewContext(req, httptest.NewRecorder())
			ctx.SetParamNames("id")
			ctx.SetParamValues(strconv.FormatInt(d.ID, 10))
			ctx.Set(auth.UserHTTPCtxKey, user)
			err := a.ChangeAllocationManagement(ctx)
			if tc.want != 0 {
				requireOrgPoolAllocationRejection(t, err, tc.want)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
