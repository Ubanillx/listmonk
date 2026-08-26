package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_11_0 preserves UUIDs from subscribers merged during organization resource
// migration. Existing current UUIDs remain on subscribers; aliases are added
// only for rows that are merged after this migration has run.
func V6_11_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	return ensureSubscriberUUIDAliases(db)
}

func ensureSubscriberUUIDAliases(db *sqlx.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS subscriber_uuid_aliases (
			uuid UUID PRIMARY KEY,
			subscriber_id INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE ON UPDATE CASCADE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_subscriber_uuid_aliases_subscriber_id
			ON subscriber_uuid_aliases(subscriber_id);
	`)
	return err
}
