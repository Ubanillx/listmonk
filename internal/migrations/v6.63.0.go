package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_63_0 adds durable selection flags for targeted campaign retries.
func V6_63_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS retry_mode BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS retry_selected BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE campaign_pool_recipients ADD COLUMN IF NOT EXISTS retry_selected BOOLEAN NOT NULL DEFAULT false;
		CREATE INDEX IF NOT EXISTS campaign_recipients_retry_idx ON campaign_recipients(campaign_id,customer_id) WHERE retry_selected;
		CREATE INDEX IF NOT EXISTS campaign_pool_recipients_retry_idx ON campaign_pool_recipients(campaign_id,pool_contact_id) WHERE retry_selected;
		CREATE INDEX IF NOT EXISTS campaign_send_errors_recipient_idx ON campaign_send_errors(campaign_id,recipient_type,recipient_id,created_at DESC,id DESC);`)
	return err
}
