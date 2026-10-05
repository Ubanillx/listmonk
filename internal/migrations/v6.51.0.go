package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_51_0 preserves existing folder audiences while adding explicit visibility.
func V6_51_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE media_folders ADD COLUMN IF NOT EXISTS visibility TEXT;
		UPDATE media_folders SET visibility = CASE WHEN organization_id IS NULL
			THEN 'private' ELSE 'organization' END WHERE visibility IS NULL;
		ALTER TABLE media_folders ALTER COLUMN visibility SET DEFAULT 'private';
		ALTER TABLE media_folders ALTER COLUMN visibility SET NOT NULL;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'media_folders_visibility_check'
				AND conrelid = 'media_folders'::regclass) THEN
				ALTER TABLE media_folders ADD CONSTRAINT media_folders_visibility_check
					CHECK (visibility IN ('private', 'organization', 'global')
						AND (visibility <> 'organization' OR organization_id IS NOT NULL));
			END IF;
		END $$;
	`)
	return err
}
