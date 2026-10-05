package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_54_0 adds an explicit public-pool contact blocklist state without
// changing existing active or archived contacts.
func V6_54_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE pool_contacts DROP CONSTRAINT IF EXISTS pool_contacts_status_check;
		ALTER TABLE pool_contacts ADD CONSTRAINT pool_contacts_status_check
		CHECK (status IN ('active','archived','blocklisted'));`); err != nil {
		return err
	}
	return tx.Commit()
}
