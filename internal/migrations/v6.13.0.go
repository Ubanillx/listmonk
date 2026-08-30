package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_13_0 adds account-owned SMTP credentials and account-scoped daily usage.
// The existing settings.smtp JSON value intentionally remains the platform
// system SMTP and is not copied into this table.
func V6_13_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_smtp_servers (
			id               SERIAL PRIMARY KEY,
			uuid             UUID NOT NULL UNIQUE,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
			name             TEXT NOT NULL DEFAULT '',
			enabled          BOOLEAN NOT NULL DEFAULT TRUE,
			from_email       TEXT NOT NULL DEFAULT '',
			daily_limit      INT NOT NULL DEFAULT 0 CHECK (daily_limit >= 0),
			host             TEXT NOT NULL DEFAULT '',
			hello_hostname   TEXT NOT NULL DEFAULT '',
			port             INT NOT NULL DEFAULT 465 CHECK (port > 0 AND port <= 65535),
			auth_protocol    TEXT NOT NULL DEFAULT 'plain' CHECK (auth_protocol IN ('plain', 'login', 'cram', 'none')),
			username         TEXT NOT NULL DEFAULT '',
			password         TEXT NOT NULL DEFAULT '',
			email_headers    JSONB NOT NULL DEFAULT '[]',
			max_conns        INT NOT NULL DEFAULT 10 CHECK (max_conns > 0),
			max_msg_retries  INT NOT NULL DEFAULT 2 CHECK (max_msg_retries > 0),
			idle_timeout     TEXT NOT NULL DEFAULT '15s',
			wait_timeout     TEXT NOT NULL DEFAULT '5s',
			tls_type         TEXT NOT NULL DEFAULT 'TLS' CHECK (tls_type IN ('none', 'TLS', 'STARTTLS')),
			tls_skip_verify  BOOLEAN NOT NULL DEFAULT FALSE,
			created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_user_smtp_servers_user_enabled
			ON user_smtp_servers(user_id, enabled);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_smtp_servers_user_name
			ON user_smtp_servers(user_id, LOWER(name)) WHERE name <> '';

		CREATE TABLE IF NOT EXISTS user_smtp_daily_usage (
			smtp_uuid    UUID NOT NULL REFERENCES user_smtp_servers(uuid) ON DELETE CASCADE ON UPDATE CASCADE,
			usage_date   DATE NOT NULL,
			sent_count   INT NOT NULL DEFAULT 0,
			updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (smtp_uuid, usage_date)
		);
		CREATE INDEX IF NOT EXISTS idx_user_smtp_daily_usage_date
			ON user_smtp_daily_usage(usage_date);
	`)
	return err
}
