package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_38_0 separates business actions that were previously bundled into
// customer/campaign/export roles. The platform administrator role is left
// untouched; organization managers continue to use the organization boundary
// rather than these global role switches.
func V6_38_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[]) || ARRAY['campaigns:test']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND COALESCE(permissions, ARRAY[]::text[]) && ARRAY['campaigns:manage', 'campaigns:manage_all'];

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[]) || ARRAY['campaigns:schedule']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND COALESCE(permissions, ARRAY[]::text[]) && ARRAY['campaigns:manage', 'campaigns:manage_all']
			AND 'campaigns:send' = ANY(COALESCE(permissions, ARRAY[]::text[]));

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[]) || ARRAY['campaigns:control']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND COALESCE(permissions, ARRAY[]::text[]) && ARRAY['campaigns:manage', 'campaigns:manage_all'];

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(COALESCE(permissions, ARRAY[]::text[]) || ARRAY['customers:export']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND 'exports:create' = ANY(COALESCE(permissions, ARRAY[]::text[]));
	`)
	return err
}
