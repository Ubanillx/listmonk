package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_37_0 adds logical media folders. Existing media remains in the implicit
// root folder; folder operations only change database metadata and never move
// provider objects.
func V6_37_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS media_folders (
			id                 SERIAL PRIMARY KEY,
			name               TEXT NOT NULL CHECK (name <> ''),
			parent_id          INTEGER NULL REFERENCES media_folders(id) ON DELETE SET NULL,
			organization_id    BIGINT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			owner_user_id      INTEGER NULL REFERENCES users(id) ON DELETE SET NULL,
			created_by_user_id INTEGER NULL REFERENCES users(id) ON DELETE SET NULL,
			created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			CHECK (organization_id IS NOT NULL OR owner_user_id IS NOT NULL)
		);
		ALTER TABLE media ADD COLUMN IF NOT EXISTS folder_id INTEGER;
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conname = 'media_folder_id_fkey'
				  AND conrelid = 'media'::regclass
			) THEN
				ALTER TABLE media
					ADD CONSTRAINT media_folder_id_fkey
					FOREIGN KEY (folder_id) REFERENCES media_folders(id) ON DELETE SET NULL;
			END IF;
		END $$;
		CREATE INDEX IF NOT EXISTS idx_media_folder_id ON media(folder_id);
		CREATE INDEX IF NOT EXISTS idx_media_folders_parent_id ON media_folders(parent_id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_media_folders_org_parent_name
			ON media_folders (organization_id, COALESCE(parent_id, 0), LOWER(name))
			WHERE organization_id IS NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_media_folders_personal_parent_name
			ON media_folders (owner_user_id, COALESCE(parent_id, 0), LOWER(name))
			WHERE organization_id IS NULL;
	`)
	return err
}
