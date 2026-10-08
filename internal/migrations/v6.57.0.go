package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
	"log"
)

// V6_57_0 preserves private delivery routes and durable IMAP scan positions.
// Historical private routes cannot be reconstructed from an edited campaign;
// leave them NULL and resolve only unambiguous legacy deliveries.
func V6_57_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS reply_to_snapshot TEXT;
		ALTER TABLE reply_ai_events ADD COLUMN IF NOT EXISTS reply_references TEXT NOT NULL DEFAULT '';
		CREATE TABLE IF NOT EXISTS reply_mailbox_scan_cursors (
			mailbox_id INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE,
			consumer TEXT NOT NULL CHECK(consumer IN ('ai','forward')),
			connection_hash TEXT NOT NULL,
			uid_validity BIGINT NOT NULL,
			last_uid BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY(mailbox_id,consumer)
		);
	`)
	return err
}
