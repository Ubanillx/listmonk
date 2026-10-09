package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

func seedDeletionList(t *testing.T, a *App, name, typ string, owner int) int {
	t.Helper()
	var id int
	if err := a.db.Get(&id, `INSERT INTO customer_lists(uuid,name,type,owner_user_id,visibility)
		VALUES(gen_random_uuid(),$1,$2,$3,'private') RETURNING id`, name, typ, owner); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedDeletionCustomer(t *testing.T, a *App, name string, owner int, lists ...int) int {
	t.Helper()
	var id int
	if err := a.db.Get(&id, `INSERT INTO customers(uuid,email,name,owner_user_id)
		VALUES(gen_random_uuid(),$1,$2,$3) RETURNING id`, name+"@example.com", name, owner); err != nil {
		t.Fatal(err)
	}
	for _, list := range lists {
		if _, err := a.db.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id) VALUES($1,$2)`, id, list); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func deletionContext(user auth.User, url string, id int) echo.Context {
	c := echo.New().NewContext(httptest.NewRequest(http.MethodDelete, url, nil), httptest.NewRecorder())
	c.Set(auth.UserHTTPCtxKey, user)
	if id > 0 {
		c.Set("id", id)
	}
	return c
}

func TestDeleteListsWithCustomers(t *testing.T) {
	for _, mode := range []string{"default", "single", "bulk", "query", "empty query"} {
		t.Run(mode, func(t *testing.T) {
			a := newOrgPoolAllocationTestApp(t)
			seedOrgPoolAllocationFixtures(t, a.db)
			admin := auth.User{Base: auth.Base{ID: 1}, UserRoleID: auth.SuperAdminRoleID}
			one := seedDeletionList(t, a, "Delete group one", "private", 1)
			two := seedDeletionList(t, a, "Delete group two", "public", 1)
			keep := seedDeletionList(t, a, "Keep", "private", 1)
			pool := seedDeletionList(t, a, "Delete group pool", "pool", 1)
			archived := seedDeletionList(t, a, "Delete group archived", "private", 1)
			if _, err := a.db.Exec(`UPDATE customer_lists SET status='archived' WHERE id=$1`, archived); err != nil {
				t.Fatal(err)
			}
			unique := seedDeletionCustomer(t, a, "unique", 1, one)
			both := seedDeletionCustomer(t, a, "both", 1, one, two)
			shared := seedDeletionCustomer(t, a, "shared", 1, one, keep)
			orphan := seedDeletionCustomer(t, a, "unrelated", 1)
			pending := seedDeletionCustomer(t, a, "pending", 1, one)
			crossOrg := seedDeletionCustomer(t, a, "cross-org", 1, one)
			if _, err := a.db.Exec(`UPDATE customers SET transfer_pending_at=NOW() WHERE id=$1`, pending); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`UPDATE customers SET organization_id=$2 WHERE id=$1`, crossOrg, orgPoolAllocationTestArchivedOrgID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`UPDATE customer_list_memberships SET status='unsubscribed' WHERE customer_id=$1 AND customer_list_id=$2`, shared, keep); err != nil {
				t.Fatal(err)
			}
			url := fmt.Sprintf("/api/customer-lists/%d?delete_customers=true", one)
			handler := a.DeleteList
			id := one
			wantDeleted := int64(1)
			if mode == "default" {
				url = fmt.Sprintf("/api/customer-lists/%d", one)
				wantDeleted = 0
			} else if mode == "bulk" || mode == "query" || mode == "empty query" {
				handler, id, wantDeleted = a.DeleteLists, 0, 2
				url = fmt.Sprintf("/api/customer-lists?id=%d&id=%d&delete_customers=true", one, two)
				if mode == "query" {
					url = "/api/customer-lists?query=Delete%20group&all=true&type_group=private&status=active&delete_customers=true"
				} else if mode == "empty query" {
					url = "/api/customer-lists?query=Missing&all=true&type_group=private&delete_customers=true"
					wantDeleted = 0
				}
			}
			c := deletionContext(admin, url, id)
			if err := handler(c); err != nil {
				t.Fatal(err)
			}
			metadata := c.Get(auditContextMetadata).(map[string]any)
			if metadata["deleted_customer_count"] != wantDeleted {
				t.Fatalf("audit count = %+v, want %d", metadata, wantDeleted)
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=ANY($1::INT[])`, pq.Array([]int{shared, orphan, pending, crossOrg})); got != 4 {
				t.Fatal("shared, unrelated, pending or cross-workspace customer was deleted")
			}
			wantUnique := 0
			if wantDeleted == 0 {
				wantUnique = 1
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, unique); got != wantUnique {
				t.Fatalf("unique customer count = %d, want %d", got, wantUnique)
			}
			wantBoth := 1
			if wantDeleted == 2 {
				wantBoth = 0
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, both); got != wantBoth {
				t.Fatalf("customer in two selected lists = %d, want %d", got, wantBoth)
			}
			if got := countRows(t, a.db, `SELECT COUNT(*) FROM customer_lists WHERE id=ANY($1::INT[])`, pq.Array([]int{keep, pool, archived})); got != 3 {
				t.Fatal("list outside the selection was deleted")
			}
		})
	}
}

func TestDeleteListCustomersPermissionsAndRollback(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	user := permissionTestUser(auth.PermWorkspacesPersonal, auth.PermListManageAll, auth.PermListDelete)
	user.ID = 3
	keepOnlyList := seedDeletionList(t, a, "List only", "private", user.ID)
	keepOnlyCustomer := seedDeletionCustomer(t, a, "keep-only", user.ID, keepOnlyList)
	if err := a.DeleteList(deletionContext(user, fmt.Sprintf("/api/customer-lists/%d", keepOnlyList), keepOnlyList)); err != nil {
		t.Fatalf("list-only deletion requires customer deletion permission: %v", err)
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, keepOnlyCustomer) != 1 {
		t.Fatal("list-only deletion removed a customer")
	}
	list := seedDeletionList(t, a, "Owned", "private", user.ID)
	owned := seedDeletionCustomer(t, a, "owned", user.ID, list)
	foreign := seedDeletionCustomer(t, a, "foreign", 4, list)
	url := fmt.Sprintf("/api/customer-lists/%d?delete_customers=true", list)
	requireOrgPoolAllocationRejection(t, a.DeleteList(deletionContext(user, url, list)), http.StatusForbidden)
	user.PermissionsMap[auth.PermCustomersDelete] = struct{}{}
	keyContext := deletionContext(user, url, list)
	keyContext.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{
		Kind: auth.IntegrationTokenKindPersonal, Scopes: pq.StringArray{apiKeyScopeListsWrite},
	})
	requireOrgPoolAllocationRejection(t, requireListCustomerDeletion(keyContext, "private", true), http.StatusForbidden)
	keyContext.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{
		Kind: auth.IntegrationTokenKindPersonal, Scopes: pq.StringArray{apiKeyScopeListsWrite, apiKeyScopeCustomersWrite},
	})
	if err := requireListCustomerDeletion(keyContext, "private", true); err != nil {
		t.Fatal(err)
	}
	// Locked list ownership rejects the whole operation without partial cleanup.
	access := models.WorkspaceAccess{Workspace: models.Workspace{Personal: true}, UserID: 4}
	if _, err := a.core.DeleteListsWithCustomersInWorkspace(access, []int{list}, true); err == nil {
		t.Fatal("cross-owner list deletion succeeded")
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=ANY($1::INT[])`, pq.Array([]int{owned, foreign})) != 2 {
		t.Fatal("failed transaction deleted customers")
	}
	if _, err := a.db.Exec(`UPDATE customer_lists SET transfer_pending_at=NOW() WHERE id=$1`, list); err != nil {
		t.Fatal(err)
	}
	access.UserID = user.ID
	if _, err := a.core.DeleteListsWithCustomersInWorkspace(access, []int{list}, true); err == nil {
		t.Fatal("pending transfer was deleted")
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, owned) != 1 {
		t.Fatal("pending-transfer rejection removed a customer")
	}
	if _, err := a.db.Exec(`UPDATE customer_lists SET transfer_pending_at=NULL WHERE id=$1`, list); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteList(deletionContext(user, url, list)); err != nil {
		t.Fatal(err)
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, owned) != 0 ||
		countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, foreign) != 1 {
		t.Fatal("customer owner boundary was not respected")
	}
}

func TestDeletePoolListWithCustomers(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	master := permissionTestUser(auth.PermWorkspacesPersonal, auth.PermPoolsMasterManage)
	master.ID = 3
	one := seedDeletionList(t, a, "Delete pool", "pool", 1)
	two := seedDeletionList(t, a, "Keep pool", "pool", 1)
	legacy := seedDeletionCustomer(t, a, "legacy-private", master.ID, one)
	unique, err := a.core.CreatePoolContact(one, models.PoolContact{CustomerCode: "UNIQUE", Email: "unique@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := a.core.CreatePoolContact(one, models.PoolContact{CustomerCode: "SHARED", Email: "shared@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO pool_members(pool_id,contact_id) VALUES($1,$2)`, two, shared.ID); err != nil {
		t.Fatal(err)
	}
	allocation, err := a.core.CreateOrgPoolAllocation(one, orgPoolAllocationTestHomeOrgID, "Deleted allocation", nil, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.AssignPoolContact(allocation.ID, unique.ID); err != nil {
		t.Fatal(err)
	}
	c := deletionContext(master, "/api/customer-lists/"+strconv.Itoa(one)+"?delete_customers=true", one)
	if err := a.DeleteList(c); err != nil {
		t.Fatal(err)
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM pool_contacts WHERE id=$1`, unique.ID) != 0 ||
		countRows(t, a.db, `SELECT COUNT(*) FROM pool_contacts WHERE id=$1`, shared.ID) != 1 {
		t.Fatal("shared pool contact protection failed")
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, legacy) != 1 {
		t.Fatal("pool master deletion removed a private customer")
	}
}

func TestDeleteListCustomersOrganizationBoundary(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	user := permissionTestUser(auth.PermListManageAll, auth.PermListDelete, auth.PermCustomersDelete)
	user.ID = 3
	list := seedDeletionList(t, a, "Organization list", "private", user.ID)
	owned := seedDeletionCustomer(t, a, "org-owned", user.ID, list)
	foreign := seedDeletionCustomer(t, a, "org-foreign", 2, list)
	if _, err := a.db.Exec(`UPDATE customer_lists SET organization_id=$2 WHERE id=$1`, list, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE customers SET organization_id=$2 WHERE id=ANY($1::INT[])`, pq.Array([]int{owned, foreign}), orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	access := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: orgPoolAllocationTestHomeOrgID}, UserID: user.ID}
	if _, err := a.db.Exec(`UPDATE organizations SET status='archived' WHERE id=$1`, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.DeleteListsWithCustomersInWorkspace(access, []int{list}, true); err == nil {
		t.Fatal("archived organization allowed deletion")
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=ANY($1::INT[])`, pq.Array([]int{owned, foreign})) != 2 {
		t.Fatal("archived organization partially deleted customers")
	}
	if _, err := a.db.Exec(`UPDATE organizations SET status='active' WHERE id=$1`, orgPoolAllocationTestHomeOrgID); err != nil {
		t.Fatal(err)
	}
	c := deletionContext(user, fmt.Sprintf("/api/customer-lists/%d?delete_customers=true", list), list)
	c.Request().Header.Set(workspaceHeader, strconv.Itoa(orgPoolAllocationTestHomeOrgID))
	if err := a.DeleteList(c); err != nil {
		t.Fatal(err)
	}
	if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, owned) != 0 ||
		countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, foreign) != 1 {
		t.Fatal("organization customer ownership was not respected")
	}
}

func TestListCustomerDeletionParameter(t *testing.T) {
	user := permissionTestUser()
	_, err := parseListCustomerDeletion(deletionContext(user, "/api/customer-lists?delete_customers=invalid", 0))
	requireOrgPoolAllocationRejection(t, err, http.StatusBadRequest)
	requireOrgPoolAllocationRejection(t, requireListCustomerDeletion(deletionContext(user, "/api/customer-lists", 0), "org_pool_allocation", true), http.StatusBadRequest)
}

func TestDeleteListCustomersConcurrentMembership(t *testing.T) {
	for _, race := range []string{"shared customer", "new customer"} {
		t.Run(race, func(t *testing.T) {
			a := newOrgPoolAllocationTestApp(t)
			seedOrgPoolAllocationFixtures(t, a.db)
			list := seedDeletionList(t, a, "Delete", "private", 1)
			keep := seedDeletionList(t, a, "Keep", "private", 1)
			customer := seedDeletionCustomer(t, a, "racing", 1, list)
			tx, err := a.db.Beginx()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			query, lockID, waitQuery := "SELECT id FROM customers WHERE id=$1 FOR UPDATE", customer, "%SELECT customer.id FROM customers customer%"
			if race == "new customer" {
				query, lockID, waitQuery = "SELECT id FROM customer_lists WHERE id=$1 FOR UPDATE", list, "%SELECT id FROM customer_lists WHERE%"
			}
			var locked int
			if err := tx.Get(&locked, query, lockID); err != nil {
				t.Fatal(err)
			}
			type deletionResult struct {
				count int64
				err   error
			}
			finished := make(chan deletionResult, 1)
			go func() {
				count, err := a.core.DeleteListsWithCustomersInWorkspace(models.WorkspaceAccess{
					Workspace: models.Workspace{Personal: true, PlatformAdmin: true}, UserID: 1,
				}, []int{list}, true)
				finished <- deletionResult{count, err}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				if err := a.db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
					WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, waitQuery); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("deletion did not reach the expected lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if race == "shared customer" {
				if _, err := tx.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id) VALUES($1,$2)`, customer, keep); err != nil {
					t.Fatal(err)
				}
			} else {
				var newID int
				if err := tx.Get(&newID, `INSERT INTO customers(uuid,email,name,owner_user_id)
					VALUES(gen_random_uuid(),'new-racing@example.com','New racing',1) RETURNING id`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(`INSERT INTO customer_list_memberships(customer_id,customer_list_id) VALUES($1,$2)`, newID, list); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-finished:
				if race == "shared customer" {
					if result.err != nil || result.count != 0 {
						t.Fatalf("concurrent shared customer was deleted: %+v", result)
					}
				} else {
					requireOrgPoolAllocationRejection(t, result.err, http.StatusConflict)
					if countRows(t, a.db, `SELECT COUNT(*) FROM customer_lists WHERE id=$1`, list) != 1 {
						t.Fatal("conflicting deletion removed the list")
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("deletion did not finish after releasing the lock")
			}
			if countRows(t, a.db, `SELECT COUNT(*) FROM customers WHERE id=$1`, customer) != 1 {
				t.Fatal("concurrent membership change did not preserve the customer")
			}
		})
	}
}
