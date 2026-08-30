package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_18_0 changes the default application language to Simplified Chinese.
// Existing installations that still use the original English default are
// migrated; users who explicitly selected another language are left intact.
func V6_18_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		UPDATE settings SET value = '"zh-CN"'
		WHERE key = 'app.lang' AND value = '"en"';
	`)
	return err
}
