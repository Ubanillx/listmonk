package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_12_0 preserves withdrawn organization-creation requests as user-visible
// history instead of deleting them. Existing requests retain their status.
func V6_12_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		ALTER TABLE organization_join_requests
			DROP CONSTRAINT IF EXISTS organization_join_requests_status_check;
		ALTER TABLE organization_join_requests
			ADD CONSTRAINT organization_join_requests_status_check
			CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn'));
	`)
	return err
}
