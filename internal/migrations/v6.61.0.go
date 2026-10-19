package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_61_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`ALTER TABLE org_pool_allocations
		ADD COLUMN IF NOT EXISTS state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','disabled','detached')),
		ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1,
		ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();`)
	return err
}
