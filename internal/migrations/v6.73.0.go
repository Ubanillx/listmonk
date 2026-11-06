package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_73_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(ReplyMailboxScanSQL)
	return err
}

// Backfill once, preserving an explicitly paused mailbox on repeated upgrades.
const ReplyMailboxScanSQL = `
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
        WHERE table_schema=current_schema() AND table_name='reply_mailboxes' AND column_name='scan_enabled') THEN
        ALTER TABLE reply_mailboxes ADD COLUMN scan_enabled BOOLEAN NOT NULL DEFAULT FALSE;
        UPDATE reply_mailboxes SET scan_enabled=(ai_enabled OR bounce_enabled);
    END IF;
END $$;
`
