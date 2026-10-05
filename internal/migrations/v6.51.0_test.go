package migrations

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestMediaFolderVisibilityMigration(t *testing.T) {
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
	if _, err := db.Exec(`CREATE TEMP TABLE media_folders(id INT PRIMARY KEY, organization_id BIGINT);
		INSERT INTO media_folders VALUES(1,NULL),(2,7);`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := V6_51_0(db, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var visible string
	for id, want := range map[int]string{1: "private", 2: "organization"} {
		if err := db.Get(&visible, `SELECT visibility FROM media_folders WHERE id=$1`, id); err != nil || visible != want {
			t.Fatalf("id=%d visibility=%s want=%s err=%v", id, visible, want, err)
		}
	}
	if _, err := db.Exec(`UPDATE media_folders SET visibility='global' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := V6_51_0(db, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&visible, `SELECT visibility FROM media_folders WHERE id=1`); err != nil || visible != "global" {
		t.Fatalf("migration reset explicit permission: %s %v", visible, err)
	}
	for _, sql := range []string{`UPDATE media_folders SET visibility='organization' WHERE id=1`, `UPDATE media_folders SET visibility='invalid' WHERE id=2`} {
		if _, err := db.Exec(sql); err == nil {
			t.Fatalf("invalid audience accepted: %s", sql)
		}
	}
}
