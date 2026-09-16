package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_33_0 adds the department imported from the public-pool source template.
// The column is metadata on the first-level pool contact; it does not create
// or bind an organization secondary list automatically.
func V6_33_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE pool_contacts ADD COLUMN IF NOT EXISTS allocation_department TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_pool_contacts_allocation_department ON pool_contacts(allocation_department);
	`)
	return err
}
