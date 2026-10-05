package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_55_0 adds contact reply addresses, campaign routing order and immutable
// per-recipient reply snapshots. Existing delivery history retains its route.
func V6_55_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		ALTER TABLE pool_contacts ADD COLUMN IF NOT EXISTS reply_to TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS pool_reply_priority TEXT NOT NULL DEFAULT 'contact_first'
			CHECK (pool_reply_priority IN ('contact_first','organization_first'));
		ALTER TABLE campaign_pool_recipients ADD COLUMN IF NOT EXISTS reply_to_snapshot TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_pool_recipients ADD COLUMN IF NOT EXISTS reply_to_source TEXT NOT NULL DEFAULT '';
		UPDATE campaign_pool_recipients cpr SET reply_to_snapshot=rm.email,reply_to_source='organization'
		FROM reply_mailboxes rm WHERE rm.id=cpr.reply_mailbox_id AND cpr.reply_to_source='';
	`); err != nil {
		return err
	}
	return tx.Commit()
}
