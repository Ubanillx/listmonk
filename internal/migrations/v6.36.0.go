package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_36_0 backfills the new action-specific permissions for existing user
// roles. The broad permissions remain compatible grants; these entries let
// administrators revoke an individual action from a role without changing
// the existing behavior of roles that were already in use.
func V6_36_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(permissions || ARRAY['customers:delete', 'customers:blocklist', 'customers:membership_manage']) AS permission
		)
		WHERE type = 'user' AND id <> 1 AND 'customers:manage' = ANY(permissions);

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(permissions || ARRAY['campaigns:send']) AS permission
		)
		WHERE type = 'user' AND id <> 1
			AND permissions && ARRAY['campaigns:manage', 'campaigns:manage_all'];

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(permissions || ARRAY['campaigns:recipients']) AS permission
		)
		WHERE type = 'user' AND id <> 1 AND 'campaigns:get_analytics' = ANY(permissions);

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(permissions || ARRAY['bounces:delete', 'bounces:blocklist']) AS permission
		)
		WHERE type = 'user' AND id <> 1 AND 'bounces:manage' = ANY(permissions);

		UPDATE roles
		SET permissions = ARRAY(
			SELECT DISTINCT permission
			FROM unnest(permissions || ARRAY['users:tokens', 'organizations:platform_manage']) AS permission
		)
		WHERE type = 'user' AND id <> 1 AND 'users:manage' = ANY(permissions);
	`)
	return err
}
