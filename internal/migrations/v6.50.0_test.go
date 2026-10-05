package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestOrganizationSMTPMigrationPreservesAccountOwnership(t *testing.T) {
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
	_, err = db.Exec(`
		CREATE TEMP TABLE users(id INT PRIMARY KEY);
		CREATE TEMP TABLE organizations(id BIGINT PRIMARY KEY);
		CREATE TEMP TABLE user_smtp_servers(id SERIAL PRIMARY KEY, uuid UUID UNIQUE, user_id INT NOT NULL REFERENCES users(id), name TEXT DEFAULT '', enabled BOOLEAN DEFAULT TRUE, password TEXT);
		CREATE TEMP TABLE campaigns(id INT PRIMARY KEY);
		INSERT INTO users VALUES(1);
		INSERT INTO organizations VALUES(1),(2);
		INSERT INTO user_smtp_servers(uuid,user_id,name,password) VALUES('00000000-0000-0000-0000-000000000001',1,'original','original-password');
		INSERT INTO campaigns VALUES(1);
	`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_50_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var preserved bool
	if err := db.Get(&preserved, `SELECT user_id=1 AND organization_id IS NULL AND password='original-password' FROM user_smtp_servers WHERE id=1`); err != nil || !preserved {
		t.Fatalf("existing owner preserved=%v err=%v", preserved, err)
	}
	var source string
	if err := db.Get(&source, `SELECT smtp_source FROM campaigns WHERE id=1`); err != nil || source != "personal" {
		t.Fatalf("source=%s err=%v", source, err)
	}
	if _, err := db.Exec(`INSERT INTO user_smtp_servers(organization_id,name) VALUES(1,'original'),(2,'original')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO user_smtp_servers(user_id,organization_id) VALUES(1,1)`,
		`INSERT INTO user_smtp_servers(name) VALUES('unowned')`,
		`INSERT INTO user_smtp_servers(organization_id,name) VALUES(1,'original')`,
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("invalid ownership accepted: %s", query)
		}
	}
}
