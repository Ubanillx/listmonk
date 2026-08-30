package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_15_0 adds dedicated customer-reply mailboxes and application-managed
// forwarding for retained mailboxes. The mailbox is intentionally separate
// from personal SMTP credentials: it is a receive-only Reply-To destination.
func V6_15_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS reply_mailboxes (
			id               SERIAL PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
			organization_id  INTEGER NULL REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
			email            TEXT NOT NULL,
			name             TEXT NOT NULL DEFAULT '',
			username         TEXT NOT NULL DEFAULT '',
			imap_host        TEXT NOT NULL DEFAULT 'imap.263.net',
			imap_port        INTEGER NOT NULL DEFAULT 993 CHECK (imap_port > 0 AND imap_port <= 65535),
			imap_tls         BOOLEAN NOT NULL DEFAULT TRUE,
			folder           TEXT NOT NULL DEFAULT 'INBOX',
			password         TEXT NOT NULL DEFAULT '',
			status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','retained','disabled')),
			verified_at      TIMESTAMP WITH TIME ZONE NULL,
			is_default       BOOLEAN NOT NULL DEFAULT FALSE,
			last_sync_at     TIMESTAMP WITH TIME ZONE NULL,
			last_sync_error  TEXT NOT NULL DEFAULT '',
			forward_count    INTEGER NOT NULL DEFAULT 0,
			created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_reply_mailboxes_user_email
			ON reply_mailboxes(user_id, COALESCE(organization_id, 0), LOWER(email));
		CREATE INDEX IF NOT EXISTS idx_reply_mailboxes_user_status
			ON reply_mailboxes(user_id, status);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_reply_mailboxes_user_default
			ON reply_mailboxes(user_id) WHERE is_default = TRUE AND status IN ('pending','active','retained');

		ALTER TABLE campaigns
			ADD COLUMN IF NOT EXISTS reply_mailbox_id INTEGER NULL
			REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
		ALTER TABLE reply_mailboxes
			ADD COLUMN IF NOT EXISTS organization_id INTEGER NULL
			REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE;
		DROP INDEX IF EXISTS idx_reply_mailboxes_user_email;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_reply_mailboxes_user_email
			ON reply_mailboxes(user_id, COALESCE(organization_id, 0), LOWER(email));

		CREATE TABLE IF NOT EXISTS reply_forward_rules (
			id                SERIAL PRIMARY KEY,
			reply_mailbox_id  INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
			organization_id   INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
			target_user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT ON UPDATE CASCADE,
			target_email      TEXT NOT NULL,
			status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
			disabled_at      TIMESTAMP WITH TIME ZONE NULL,
			disabled_by      INTEGER NULL REFERENCES users(id) ON DELETE SET NULL ON UPDATE CASCADE,
			last_error       TEXT NOT NULL DEFAULT '',
			last_forward_at  TIMESTAMP WITH TIME ZONE NULL,
			created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE (reply_mailbox_id, organization_id)
		);
		CREATE INDEX IF NOT EXISTS idx_reply_forward_rules_active
			ON reply_forward_rules(status, reply_mailbox_id);

		CREATE TABLE IF NOT EXISTS reply_forward_messages (
			id                BIGSERIAL PRIMARY KEY,
			rule_id           INTEGER NOT NULL REFERENCES reply_forward_rules(id) ON DELETE CASCADE ON UPDATE CASCADE,
			message_key       TEXT NOT NULL,
			imap_uid          TEXT NOT NULL DEFAULT '',
			from_email        TEXT NOT NULL DEFAULT '',
			subject           TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','forwarded','failed')),
			attempts          INTEGER NOT NULL DEFAULT 0,
			last_error        TEXT NOT NULL DEFAULT '',
			received_at       TIMESTAMP WITH TIME ZONE NULL,
			forwarded_at      TIMESTAMP WITH TIME ZONE NULL,
			created_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE (rule_id, message_key)
		);
		CREATE INDEX IF NOT EXISTS idx_reply_forward_messages_pending
			ON reply_forward_messages(status, created_at);
	`)
	return err
}
