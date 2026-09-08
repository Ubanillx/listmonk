package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_25_0 adds an opt-in, OpenAI-compatible inbound-reply classification
// queue. The queue is retained independently of reply forwarding so retries
// and complaint provenance remain idempotent and auditable.
func V6_25_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		ALTER TABLE reply_mailboxes
			ADD COLUMN IF NOT EXISTS ai_enabled BOOLEAN NOT NULL DEFAULT FALSE;

		CREATE TABLE IF NOT EXISTS reply_ai_events (
			id                BIGSERIAL PRIMARY KEY,
			reply_mailbox_id  INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
			customer_id       INTEGER NULL,
			message_key       TEXT NOT NULL,
			from_email        TEXT NOT NULL DEFAULT '',
			subject           TEXT NOT NULL DEFAULT '',
			body              TEXT NOT NULL DEFAULT '',
			body_hash         TEXT NOT NULL DEFAULT '',
			intent            TEXT NOT NULL DEFAULT 'other' CHECK (intent IN ('unsubscribe','complaint','other')),
			confidence        DOUBLE PRECISION NOT NULL DEFAULT 0,
			reason_code       TEXT NOT NULL DEFAULT '',
			model             TEXT NOT NULL DEFAULT '',
			action            TEXT NOT NULL DEFAULT 'pending' CHECK (action IN ('pending','ignored','blocklisted')),
			status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','processed','ignored','failed')),
			attempts          INTEGER NOT NULL DEFAULT 0,
			last_error        TEXT NOT NULL DEFAULT '',
			received_at       TIMESTAMP WITH TIME ZONE NULL,
			classified_at     TIMESTAMP WITH TIME ZONE NULL,
			actioned_at       TIMESTAMP WITH TIME ZONE NULL,
			next_attempt_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			lease_expires_at  TIMESTAMP WITH TIME ZONE NULL,
			lease_token       UUID NULL,
			created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			UNIQUE (reply_mailbox_id, message_key)
		);
		CREATE INDEX IF NOT EXISTS idx_reply_ai_events_claim
			ON reply_ai_events(status, next_attempt_at, created_at);
		CREATE INDEX IF NOT EXISTS idx_reply_ai_events_customer
			ON reply_ai_events(customer_id, created_at DESC);
		-- Kept additive for databases that already ran the initial queue
		-- definition before the lease token was introduced.
		ALTER TABLE reply_ai_events
			ADD COLUMN IF NOT EXISTS lease_token UUID NULL;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'reply_ai_events_customer_id_fkey') THEN
				ALTER TABLE reply_ai_events ADD CONSTRAINT reply_ai_events_customer_id_fkey
					FOREIGN KEY (customer_id) REFERENCES customers(id)
					ON DELETE SET NULL ON UPDATE CASCADE;
			END IF;
		END $$;

		ALTER TABLE bounces ADD COLUMN IF NOT EXISTS reply_ai_event_id BIGINT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_bounces_reply_ai_event
			ON bounces(reply_ai_event_id);
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'bounces_reply_ai_event_id_fkey') THEN
				ALTER TABLE bounces ADD CONSTRAINT bounces_reply_ai_event_id_fkey
					FOREIGN KEY (reply_ai_event_id) REFERENCES reply_ai_events(id)
					ON DELETE SET NULL ON UPDATE CASCADE;
			END IF;
		END $$;

		INSERT INTO settings (key, value)
		VALUES ('reply_ai', '{"enabled": false, "base_url": "", "api_key": "", "model": "", "timeout": "15s", "min_confidence": 0.98}'::JSONB)
		ON CONFLICT (key) DO NOTHING;
	`)
	return err
}
