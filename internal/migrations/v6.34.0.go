package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_34_0 allows product/service complaints to be retained as a distinct
// non-actionable AI classification. Spam/abuse complaints remain the only
// complaint intent that can blocklist a customer.
func V6_34_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE reply_ai_events DROP CONSTRAINT IF EXISTS reply_ai_events_intent_check;
		ALTER TABLE reply_ai_events
			ADD CONSTRAINT reply_ai_events_intent_check
			CHECK (intent IN ('unsubscribe','complaint','product_complaint','other'));
	`)
	return err
}
