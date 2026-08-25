package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_6_0 adds media-library attachments to reusable templates.
func V6_6_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS template_media (
		    template_id  INTEGER REFERENCES templates(id) ON DELETE CASCADE ON UPDATE CASCADE,
		    media_id     INTEGER NULL REFERENCES media(id) ON DELETE SET NULL ON UPDATE CASCADE,
		    filename     TEXT NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_template_media_id ON template_media (template_id, media_id);
		CREATE INDEX IF NOT EXISTS idx_template_media_template_id ON template_media(template_id);
	`)
	return err
}
