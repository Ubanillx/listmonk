package migrations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

func TestBusinessPermissionMigrationPreservesGrantsWithoutPoolEscalation(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DSN is not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("business_permission_test_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err := db.Exec(`CREATE TABLE roles(id INT,type TEXT DEFAULT 'user',parent_id INT,permissions TEXT[]);
		CREATE TABLE users(id INT,user_role_id INT,list_role_id INT);
		CREATE TABLE organization_members(user_id INT,role TEXT,removed_at TIMESTAMPTZ);
		INSERT INTO roles VALUES(1,'user',NULL,'{}'),(2,'user',NULL,'{customer_lists:manage_all,templates:manage}'),
		(3,'user',NULL,'{customers:get}'),(4,'customer_list',NULL,'{}'),(5,'customer_list',4,'{customer_list:manage}');
		INSERT INTO users VALUES(10,3,4);
		INSERT INTO organization_members VALUES(10,'manager',NULL);`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := V6_56_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int{2, 3} {
		var permissions pq.StringArray
		if err := db.Get(&permissions, `SELECT permissions FROM roles WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		set := map[string]bool{}
		for _, p := range permissions {
			if set[p] {
				t.Fatalf("duplicate grant: %s", p)
			}
			set[p] = true
		}
		for _, p := range []string{"customer_lists:delete", "mailboxes:use", "mailboxes:manage"} {
			if !set[p] {
				t.Fatalf("role %d lost %s", id, p)
			}
		}
		if set["pools:master_manage"] || set["pools:delivery_manage"] {
			t.Fatalf("role %d received platform pool grants", id)
		}
		if id == 2 && !set["assets:share"] {
			t.Fatal("existing template maintainer lost sharing")
		}
		if id == 3 && (!set["pools:manage"] || set["assets:share"]) {
			t.Fatal("manager allocation grants widened unrelated sharing")
		}
	}
}
