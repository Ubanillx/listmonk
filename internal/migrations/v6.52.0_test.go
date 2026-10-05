package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestOrganizationSMTPPoolMigrationPreservesSendersAndCampaigns(t *testing.T) {
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
	const schema = "smtp_pool_migration_test"
	if _, err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE; CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	_, err = db.Exec(`
		CREATE TABLE organizations(id BIGINT PRIMARY KEY);
		CREATE TABLE user_smtp_servers(id SERIAL PRIMARY KEY, organization_id BIGINT REFERENCES organizations(id),
			user_id INT, uuid UUID, name TEXT NOT NULL DEFAULT '', enabled BOOLEAN NOT NULL DEFAULT TRUE, password TEXT);
		CREATE TABLE campaigns(id SERIAL PRIMARY KEY, organization_id BIGINT REFERENCES organizations(id),
			smtp_source TEXT NOT NULL DEFAULT 'personal');
		CREATE UNIQUE INDEX idx_smtp_organization_name ON user_smtp_servers(organization_id,LOWER(name)) WHERE name<>'';
		INSERT INTO organizations VALUES(1),(2);
		INSERT INTO user_smtp_servers(organization_id,uuid,name,password) VALUES
			(1,'00000000-0000-0000-0000-000000000001','sender','secret'),
			(2,'00000000-0000-0000-0000-000000000002','sender','other-secret');
		INSERT INTO campaigns(organization_id,smtp_source) VALUES(1,'organization'),(2,'personal');
	`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_52_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err := db.Get(&valid, `SELECT EXISTS(
		SELECT 1 FROM user_smtp_servers s JOIN organization_smtp_pools p ON p.id=s.smtp_pool_id
		JOIN campaigns c ON c.smtp_pool_id=p.id
		WHERE s.organization_id=1 AND c.organization_id=1 AND s.password='secret'
		AND s.uuid='00000000-0000-0000-0000-000000000001')`); err != nil || !valid {
		t.Fatalf("legacy sender/campaign pool missing: %v %v", valid, err)
	}
	if _, err := db.Exec(`INSERT INTO organization_smtp_pools(organization_id,name) VALUES(1,'Second')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_smtp_servers(organization_id,smtp_pool_id,name) VALUES
		(1,(SELECT id FROM organization_smtp_pools WHERE name='Second'),'sender')`); err != nil {
		t.Fatalf("same sender name in a different pool rejected: %v", err)
	}
	for _, query := range []string{
		`INSERT INTO user_smtp_servers(organization_id,smtp_pool_id,name) VALUES(1,(SELECT id FROM organization_smtp_pools WHERE organization_id=2),'foreign')`,
		`INSERT INTO user_smtp_servers(organization_id,name) VALUES(1,'unpooled')`,
		`UPDATE campaigns SET smtp_pool_id=(SELECT id FROM organization_smtp_pools WHERE organization_id=2) WHERE organization_id=1`,
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("invalid pool relationship accepted: %s", query)
		}
	}
}
