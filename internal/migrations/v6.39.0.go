package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_39_0 removes the obsolete asynchronous export-center artifacts. Direct
// customer and audit exports do not create persistent export jobs or files.
// Existing user roles that could read customers retain the direct customer
// export capability; the action can then be revoked independently in the role.
func V6_39_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		DROP TABLE IF EXISTS data_export_chunks CASCADE;
		DROP TABLE IF EXISTS data_export_jobs CASCADE;

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[]) || ARRAY['customers:export']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND COALESCE(permissions, ARRAY[]::text[]) && ARRAY['customers:get', 'customers:get_all', 'exports:create'];

		UPDATE roles
		SET permissions = ARRAY(
			SELECT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[])) AS permission
			WHERE permission NOT IN ('exports:create', 'exports:download')
		)
		WHERE COALESCE(permissions, ARRAY[]::text[]) && ARRAY['exports:create', 'exports:download'];
	`)
	return err
}
