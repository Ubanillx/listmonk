package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_64_0 lowers the default personal campaign rate without changing saved rates.
func V6_64_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`ALTER TABLE campaigns ALTER COLUMN smtp_rate_limit SET DEFAULT 5`)
	return err
}
