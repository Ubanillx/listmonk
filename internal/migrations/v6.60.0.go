package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
	"log"
)

func V6_60_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS campaign_send_errors (
		id BIGSERIAL PRIMARY KEY,
		campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		campaign_owner_user_id INTEGER,
		campaign_organization_id INTEGER,
		recipient_type TEXT NOT NULL CHECK(recipient_type IN ('private','pool')),
		recipient_id BIGINT NOT NULL CHECK(recipient_id > 0),
		recipient_organization_id INTEGER NOT NULL DEFAULT 0,
		customer_code TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
		stage TEXT NOT NULL CHECK(stage IN ('render','send')),
		category TEXT NOT NULL, smtp_code INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS campaign_send_errors_campaign_idx ON campaign_send_errors(campaign_id,created_at,id);`)
	return err
}
