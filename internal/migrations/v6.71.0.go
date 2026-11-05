package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_71_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(ReplyBounceSQL)
	return err
}

const ReplyBounceSQL = `
ALTER TABLE reply_mailboxes
    ADD COLUMN IF NOT EXISTS bounce_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS reply_bounce_events (
    id                     BIGSERIAL PRIMARY KEY,
    reply_mailbox_id       INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
    message_key            TEXT NOT NULL,
    recipient              TEXT NOT NULL DEFAULT '',
    sender                 TEXT NOT NULL DEFAULT '',
    subject                TEXT NOT NULL DEFAULT '',
    report_status          TEXT NOT NULL DEFAULT '',
    diagnostic             TEXT NOT NULL DEFAULT '',
    bounce_type            TEXT NOT NULL DEFAULT 'unknown'
                           CHECK (bounce_type IN ('hard','soft','unknown')),
    outcome                TEXT NOT NULL DEFAULT 'ignored'
                           CHECK (outcome IN ('recorded','blocklisted','ignored')),
    reason_code            TEXT NOT NULL DEFAULT '',
    campaign_id            INTEGER NULL REFERENCES campaigns(id) ON DELETE SET NULL ON UPDATE CASCADE,
    customer_id            INTEGER NULL REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE,
    pool_contact_id        BIGINT NULL,
    pool_id                INTEGER NULL,
    source_allocation_id   BIGINT NULL,
    source_organization_id BIGINT NULL,
    action_count           INTEGER NOT NULL DEFAULT 0,
    action_threshold       INTEGER NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at           TIMESTAMPTZ NULL,
    UNIQUE (reply_mailbox_id, message_key, recipient)
);

CREATE INDEX IF NOT EXISTS idx_reply_bounce_events_mailbox
    ON reply_bounce_events(reply_mailbox_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reply_bounce_events_customer
    ON reply_bounce_events(customer_id, created_at DESC)
    WHERE customer_id IS NOT NULL;
`
