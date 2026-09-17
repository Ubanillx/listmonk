package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/knadh/listmonk/models"
)

// Temporary tables shadow application tables on a single connection. The test
// never changes existing data and exercises real PostgreSQL UUID/enum coercion.
func TestBounceQueriesMixedCustomerTypes(t *testing.T) {
	dsn := os.Getenv("BOUNCE_TEST_DSN")
	if dsn == "" {
		t.Skip("BOUNCE_TEST_DSN not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec(`
CREATE TEMP TABLE bounce_test_session (id int);
CREATE TYPE pg_temp.test_customer_status AS ENUM ('enabled','blocklisted');
CREATE TEMP TABLE customers (id int, uuid uuid, email text, status pg_temp.test_customer_status,
 organization_id bigint,owner_user_id int,transfer_pending_at timestamptz);
CREATE TEMP TABLE pool_contacts (id bigint,uuid uuid,email text,status text);
CREATE TEMP TABLE campaigns (id int,name text,organization_id bigint,owner_user_id int);
CREATE TEMP TABLE organizations (id bigint,status text);
CREATE TEMP TABLE bounces (id int,type text,source text,meta jsonb NOT NULL DEFAULT '{}',created_at timestamptz DEFAULT NOW(),
 customer_id int,pool_contact_id bigint,source_pool_id int,source_allocation_id bigint,source_organization_id bigint,campaign_id int);
INSERT INTO organizations VALUES(10,'active'),(20,'active');
INSERT INTO customers VALUES(1,'10000000-0000-0000-0000-000000000001','customer@example.invalid','blocklisted',10,1,NULL);
INSERT INTO pool_contacts VALUES(2,'20000000-0000-0000-0000-000000000002','pool@example.invalid','active');
INSERT INTO bounces(id,type,source,customer_id) VALUES(1,'hard','test',1);
INSERT INTO bounces(id,type,source,pool_contact_id,source_organization_id) VALUES(2,'soft','test',2,10),(3,'soft','test',2,20);
`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "queries", "bounces.sql"))
	if err != nil {
		t.Fatal(err)
	}
	queries, err := goyesql.ParseBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{db: db, q: &models.Queries{QueryBounces: queries["query-bounces"].Query}}
	admin := models.WorkspaceAccess{Workspace: models.Workspace{PlatformAdmin: true}}
	manager := models.WorkspaceAccess{Workspace: models.Workspace{OrganizationID: 10, Role: models.OrganizationMemberRoleManager}, UserID: 1}
	for _, tc := range []struct {
		name   string
		access models.WorkspaceAccess
		count  int
	}{{"admin", admin, 3}, {"manager", manager, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := c.QueryWorkspaceBounces(tc.access, 0, 0, 0, "", "id", SortAsc, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			if total != tc.count || len(rows) != tc.count {
				t.Fatalf("unexpected row count %d/%d", len(rows), total)
			}
			assertMixedBounces(t, rows)
			row, err := c.GetWorkspaceBounce(tc.access, 2)
			if err != nil || row.PoolContactID != 2 {
				t.Fatalf("pool detail: %+v, %v", row, err)
			}
		})
	}
	var rows []models.Bounce
	err = db.Select(&rows, strings.ReplaceAll(c.q.QueryBounces, "%order%", "id ASC"), 0, 0, 0, "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Total != 3 {
		t.Fatalf("legacy query count %d", len(rows))
	}
	assertMixedBounces(t, rows)
}

func assertMixedBounces(t *testing.T, rows []models.Bounce) {
	t.Helper()
	for _, row := range rows {
		if row.ID == 1 && (row.CustomerUUID != "10000000-0000-0000-0000-000000000001" || row.CustomerStatus != "blocklisted" || row.PoolContactID != 0) {
			t.Fatalf("customer fields: %+v", row)
		}
		if row.ID == 2 && (row.CustomerUUID != "20000000-0000-0000-0000-000000000002" || row.CustomerStatus != "active" || row.CustomerID != 0) {
			t.Fatalf("pool fields: %+v", row)
		}
	}
}
