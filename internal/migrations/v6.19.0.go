package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_19_0 adds per-account values for administrator-defined custom fields.
func V6_19_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		ALTER TABLE users ADD COLUMN IF NOT EXISTS attribs JSONB NOT NULL DEFAULT '{}';
	`)
	return err
}
