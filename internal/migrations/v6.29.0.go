package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

const NameFallbackSchema = `
ALTER TABLE templates ADD COLUMN IF NOT EXISTS name_fallback JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS name_fallback JSONB NOT NULL DEFAULT '{}'::jsonb;
`

func V6_29_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(NameFallbackSchema)
	return err
}
