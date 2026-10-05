package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_49_0 keeps only the system notification SMTP server and moves transport
// options into one platform-wide setting. Account-owned SMTP rows are intact.
func V6_49_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		WITH selected AS (
			SELECT elem
			FROM settings s, JSONB_ARRAY_ELEMENTS(s.value) WITH ORDINALITY AS smtp(elem, ord)
			WHERE s.key = 'smtp' AND COALESCE((elem->>'enabled')::BOOLEAN, false)
			ORDER BY COALESCE((elem->>'is_primary')::BOOLEAN, false) DESC, ord
			LIMIT 1
		)
		INSERT INTO settings (key, value)
		SELECT 'smtp_delivery', JSONB_BUILD_OBJECT(
			'max_conns', COALESCE(NULLIF((elem->>'max_conns')::INT, 0), 10),
			'max_msg_retries', COALESCE(NULLIF((elem->>'max_msg_retries')::INT, 0), 2),
			'idle_timeout', COALESCE(NULLIF(elem->>'idle_timeout', ''), '15s'),
			'wait_timeout', COALESCE(NULLIF(elem->>'wait_timeout', ''), '5s'),
			'tls_type', COALESCE(NULLIF(elem->>'tls_type', ''), 'TLS'),
			'tls_skip_verify', COALESCE((elem->>'tls_skip_verify')::BOOLEAN, false),
			'email_headers', COALESCE(elem->'email_headers', '[]'::JSONB))
		FROM selected
		ON CONFLICT (key) DO NOTHING;

		WITH selected AS (
			SELECT elem
			FROM settings s, JSONB_ARRAY_ELEMENTS(s.value) WITH ORDINALITY AS smtp(elem, ord)
			WHERE s.key = 'smtp' AND COALESCE((elem->>'enabled')::BOOLEAN, false)
			ORDER BY COALESCE((elem->>'is_primary')::BOOLEAN, false) DESC, ord
			LIMIT 1
		)
		UPDATE settings SET value = JSONB_BUILD_ARRAY(
			JSONB_SET(selected.elem, '{is_primary}', 'true'::JSONB, true))
		FROM selected WHERE key = 'smtp';
	`)
	return err
}
