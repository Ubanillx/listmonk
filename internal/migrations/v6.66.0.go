package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_66_0 records confirmed opt-outs. Current subscription state cannot reveal
// which historical campaign caused an opt-out, so no historical counts are guessed.
func V6_66_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS campaign_unsubscriptions (
		campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		recipient_type TEXT NOT NULL CHECK(recipient_type IN ('private','pool')),
		recipient_id BIGINT NOT NULL CHECK(recipient_id>0),
		campaign_owner_user_id INTEGER,campaign_organization_id INTEGER,
		recipient_organization_id INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(campaign_id,recipient_type,recipient_id));
		CREATE INDEX IF NOT EXISTS campaign_unsubscriptions_time_idx ON campaign_unsubscriptions(campaign_id,created_at DESC);`)
	return err
}
