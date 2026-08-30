package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_17_0 adds the global subscriber custom-field definition store and the
// per-campaign recipient snapshots used while rendering messages.
func V6_17_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		INSERT INTO settings (key, value) VALUES ('subscriber.custom_fields', '[]')
		ON CONFLICT (key) DO NOTHING;

		ALTER TABLE campaign_recipients
			ADD COLUMN IF NOT EXISTS email_snapshot TEXT,
			ADD COLUMN IF NOT EXISTS name_snapshot TEXT,
			ADD COLUMN IF NOT EXISTS attribs_snapshot JSONB;
	`)
	return err
}
