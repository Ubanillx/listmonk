package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_65_0 bounds campaign-recipient retries using durable failure history.
func V6_65_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	// PostgreSQL requires the new enum value to commit before it can be used.
	if _, err := db.Exec(`ALTER TYPE campaign_recipient_status ADD VALUE IF NOT EXISTS 'failed'`); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE settings SET value=value || '{"max_recipient_retries":3}'::JSONB,updated_at=NOW()
		WHERE key='smtp_delivery' AND NOT (value ? 'max_recipient_retries');
		CREATE OR REPLACE FUNCTION campaign_recipient_retry_exhausted(campaign_id INTEGER,recipient_type TEXT,recipient_id BIGINT)
		RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
			SELECT EXISTS(SELECT 1 FROM campaigns c WHERE c.id=$1 AND c.type='regular'
				AND (c.messenger='email' OR c.messenger LIKE 'email-%') AND EXISTS(SELECT 1 FROM campaign_send_errors e
				WHERE e.campaign_id=$1 AND e.recipient_type=$2 AND e.recipient_id=$3
				OFFSET GREATEST(0,COALESCE((SELECT (value->>'max_recipient_retries')::INT FROM settings WHERE key='smtp_delivery'),3)) LIMIT 1));
		$$;
		UPDATE campaign_recipients SET status='failed',updated_at=NOW()
			WHERE status IN ('pending','queued','deferred') AND campaign_recipient_retry_exhausted(campaign_id,'private',customer_id);
		UPDATE campaign_pool_recipients SET status='failed',updated_at=NOW()
			WHERE status IN ('pending','queued','deferred') AND campaign_recipient_retry_exhausted(campaign_id,'pool',pool_contact_id);`)
	return err
}
