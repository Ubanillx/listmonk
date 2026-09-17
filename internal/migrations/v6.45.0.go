package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_45_0 removes the abandoned pool_contacts.company_name field and backfills
// the configurable public-pool permissions (pools:get / pools:manage /
// pools:export) onto the Super Admin role.
//
//   - company_name was imported, stored and displayed but never used for
//     identity, matching or delivery; the contact's name is the display field.
//   - The three pools permissions are validated against permissions.json at
//     role creation, so existing role rows are not rejected without them; only
//     role id 1 receives the new grants automatically so an up-to-date Super
//     Admin keeps working without manual configuration. Every other role keeps
//     its current grants and is opt-in through the role UI.
//
// Every statement is guarded, so the migration is idempotent and a no-op on
// databases created from the current schema.sql.
func V6_45_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		DO $$ BEGIN
			IF to_regclass('pool_contacts') IS NOT NULL
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('pool_contacts') AND a.attname='company_name' AND NOT a.attisdropped) THEN
				ALTER TABLE pool_contacts DROP COLUMN company_name;
			END IF;
		END $$;

		DO $$ BEGIN
			IF to_regclass('roles') IS NOT NULL
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('roles') AND a.attname='permissions' AND NOT a.attisdropped) THEN
				UPDATE roles
				SET permissions = permissions || ARRAY(
					SELECT p FROM unnest(ARRAY['pools:get','pools:manage','pools:export']::TEXT[]) AS p
					WHERE NOT (permissions @> ARRAY[p])
				)
				WHERE id=1 AND permissions IS NOT NULL;
			END IF;
		END $$;
	`)
	return err
}
