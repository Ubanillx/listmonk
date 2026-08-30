package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_14_0 makes the regular e-mail campaign daily limit of 300 explicit.
// Older databases used zero as the column default, which meant that campaigns
// created through direct SQL (or before the UI default was introduced) could
// accidentally bypass the campaign-level cap. Zero is retained only as a
// legacy input value and is normalized to 300 for regular e-mail campaigns.
func V6_14_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		ALTER TABLE campaigns
			ALTER COLUMN daily_send_limit SET DEFAULT 300;

		UPDATE campaigns
		SET daily_send_limit = 300
		WHERE type = 'regular'
		  AND (messenger = 'email' OR messenger LIKE 'email-%')
		  AND daily_send_limit < 1;
	`)
	return err
}
