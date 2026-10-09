package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_58_0 adds durable, unguessable recipient links without publishing the media library.
func V6_58_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS email_media_links (
		media_id INTEGER PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
		token UUID NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		media_uuid UUID NOT NULL,
		organization_id INTEGER,
		owner_user_id INTEGER,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	return err
}
