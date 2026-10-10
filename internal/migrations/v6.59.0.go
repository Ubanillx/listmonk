package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_59_0 persists cumulative campaign delivery failures. Historical failures
// were only logged and cannot be reconstructed reliably from recipient state.
func V6_59_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS
		send_errors INT NOT NULL DEFAULT 0 CHECK (send_errors >= 0)`)
	return err
}
