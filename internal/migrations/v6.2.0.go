package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_2_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_, err := db.Exec(`
		WITH app_from AS (
			SELECT COALESCE(value #>> '{}', '') AS from_email
			FROM settings
			WHERE key = 'app.from_email'
		),
		smtp_blocks AS (
			SELECT value
			FROM settings
			WHERE key = 'smtp'
		),
		flags AS (
			SELECT
				COALESCE(
					BOOL_OR(
						COALESCE((elem->>'enabled')::BOOLEAN, false)
						AND COALESCE((elem->>'is_primary')::BOOLEAN, false)
					),
					false
				) AS has_primary,
				COALESCE(
					MIN(ord) FILTER (WHERE COALESCE((elem->>'enabled')::BOOLEAN, false)),
					1
				) AS first_enabled_ord
			FROM smtp_blocks, JSONB_ARRAY_ELEMENTS(value) WITH ORDINALITY AS arr(elem, ord)
		)
		UPDATE settings
		SET value = (
			SELECT JSONB_AGG(
				JSONB_SET(
					JSONB_SET(
						elem,
						'{from_email}',
						TO_JSONB(COALESCE(NULLIF(elem->>'from_email', ''), app_from.from_email)),
						true
					),
					'{is_primary}',
					CASE
						WHEN flags.has_primary THEN TO_JSONB(COALESCE((elem->>'is_primary')::BOOLEAN, false))
						ELSE TO_JSONB(ord = flags.first_enabled_ord)
					END,
					true
				)
				ORDER BY ord
			)
			FROM JSONB_ARRAY_ELEMENTS(settings.value) WITH ORDINALITY AS arr(elem, ord), app_from, flags
		)
		WHERE key = 'smtp';
	`)
	if err != nil {
		return err
	}

	return nil
}
