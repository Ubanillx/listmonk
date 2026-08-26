package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_8_0 repairs visibility values written before lists and subscribers were
// made strictly owner-private. Organization managers retain their read-only
// oversight through server-side predicates, not through publication flags.
func V6_8_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		UPDATE lists SET visibility = 'private', updated_at = NOW()
		WHERE visibility <> 'private';
		UPDATE subscribers SET visibility = 'private', updated_at = NOW()
		WHERE visibility <> 'private';
	`)
	return err
}
