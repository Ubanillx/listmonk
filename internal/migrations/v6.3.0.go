package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_3_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		ALTER TYPE campaign_status ADD VALUE IF NOT EXISTS 'deferred';

		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'campaign_recipient_status') THEN
				CREATE TYPE campaign_recipient_status AS ENUM ('pending', 'queued', 'deferred', 'sent', 'cancelled');
			END IF;
		END $$;

		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS daily_send_limit INT NOT NULL DEFAULT 0;
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS daily_resume_time TEXT NOT NULL DEFAULT '09:00';
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS next_resume_at TIMESTAMP WITH TIME ZONE NULL;
		CREATE INDEX IF NOT EXISTS idx_camps_next_resume_at ON campaigns(next_resume_at);

		CREATE TABLE IF NOT EXISTS campaign_recipients (
			campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
			subscriber_id INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE ON UPDATE CASCADE,
			status campaign_recipient_status NOT NULL DEFAULT 'pending',
			sent_at TIMESTAMP WITH TIME ZONE NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (campaign_id, subscriber_id)
		);
		CREATE INDEX IF NOT EXISTS idx_camp_recipients_status ON campaign_recipients(campaign_id, status, subscriber_id);
		CREATE INDEX IF NOT EXISTS idx_camp_recipients_sub_id ON campaign_recipients(subscriber_id);

		CREATE TABLE IF NOT EXISTS smtp_daily_usage (
			smtp_uuid uuid NOT NULL,
			usage_date DATE NOT NULL,
			sent_count INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (smtp_uuid, usage_date)
		);

		CREATE TABLE IF NOT EXISTS campaign_daily_usage (
			campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
			usage_date DATE NOT NULL,
			sent_count INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (campaign_id, usage_date)
		);

		UPDATE settings
		SET value = (
			SELECT JSONB_AGG(
				JSONB_SET(elem, '{daily_limit}', COALESCE(elem->'daily_limit', '0'::JSONB), true)
				ORDER BY ord
			)
			FROM JSONB_ARRAY_ELEMENTS(settings.value) WITH ORDINALITY AS arr(elem, ord)
		)
		WHERE key = 'smtp';
	`)
	if err != nil {
		return err
	}

	return nil
}
