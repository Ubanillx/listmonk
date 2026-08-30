package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_16_0 scopes the default reply mailbox uniqueness to the personal or
// organization workspace. A user may belong to several organizations and use
// one default customer-reply mailbox in each workspace.
func V6_16_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		DROP INDEX IF EXISTS idx_reply_mailboxes_user_default;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_reply_mailboxes_user_default
			ON reply_mailboxes(user_id, COALESCE(organization_id, 0))
			WHERE is_default = TRUE AND status IN ('pending','active','retained');
	`)
	return err
}
