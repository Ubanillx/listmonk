package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_43_0 moves the public-pool reply mailbox from the per-allocation
// org_pool_allocations.reply_mailbox_id to the organization's single unified
// reply mailbox (organizations.reply_mailbox_id). Every public-pool audience of
// an organization now resolves its reply route through the organization mailbox
// alone; a pool allocation no longer carries one.
//
// Backfill order: an organization whose allocations all carried the same
// mailbox adopts it first, then organizations that never bound one adopt their
// single active+verified mailbox. An organization with two different candidate
// mailboxes is left unconfigured on purpose: the migration refuses to guess and
// preview/send stays blocked until an administrator picks one.
//
// Every statement is guarded, so the migration is idempotent and a no-op on
// databases created from the current schema.sql, which already ships the
// organization column and no allocation column. The historical migrations keep
// the allocation column on purpose: they run before this one.
func V6_43_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		DO $$ BEGIN
			IF to_regclass('organizations') IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('organizations') AND a.attname='reply_mailbox_id' AND NOT a.attisdropped) THEN
				ALTER TABLE organizations ADD COLUMN reply_mailbox_id INTEGER NULL REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
			END IF;
		END $$;

		-- The state-dependent backfills only make sense while the allocation
		-- column still exists: they move data off it. On a database created from
		-- schema.sql the column never existed, so both steps are skipped and the
		-- migration stays a true no-op.
		DO $$ BEGIN
			IF to_regclass('organizations') IS NOT NULL
				AND to_regclass('org_pool_allocations') IS NOT NULL
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('organizations') AND a.attname='reply_mailbox_id' AND NOT a.attisdropped)
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('org_pool_allocations') AND a.attname='reply_mailbox_id' AND NOT a.attisdropped) THEN

				-- Backfill step 1: organizations whose pool allocations carried
				-- exactly one distinct non-null mailbox adopt it.
				UPDATE organizations o
				SET reply_mailbox_id = adopted.mailbox_id
				FROM (
					SELECT s.organization_id, MIN(s.reply_mailbox_id) AS mailbox_id
					FROM org_pool_allocations s
					WHERE s.reply_mailbox_id IS NOT NULL
					GROUP BY s.organization_id
					HAVING COUNT(DISTINCT s.reply_mailbox_id) = 1
				) adopted
				WHERE o.id = adopted.organization_id AND o.reply_mailbox_id IS NULL;

				-- Backfill step 2: organizations still without one that have
				-- exactly one active+verified mailbox adopt it. Two candidates are
				-- left NULL on purpose.
				UPDATE organizations o
				SET reply_mailbox_id = adopter.mailbox_id
				FROM (
					SELECT rm.organization_id, MIN(rm.id) AS mailbox_id
					FROM reply_mailboxes rm
					WHERE rm.organization_id IS NOT NULL
						AND rm.status = 'active'
						AND rm.verified_at IS NOT NULL
					GROUP BY rm.organization_id
					HAVING COUNT(*) = 1
				) adopter
				WHERE o.id = adopter.organization_id AND o.reply_mailbox_id IS NULL;
			END IF;
		END $$;

		DO $$ BEGIN
			IF to_regclass('org_pool_allocations') IS NOT NULL
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass('org_pool_allocations') AND a.attname='reply_mailbox_id' AND NOT a.attisdropped) THEN
				ALTER TABLE org_pool_allocations DROP COLUMN reply_mailbox_id;
			END IF;
		END $$;
	`)
	return err
}
