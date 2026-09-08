package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_21_0 adds a per-subscriber customer code and a per-list e-mail masking
// flag. The customer code is required on the admin/import paths (enforced at
// the application layer) but remains optional for public subscriptions, so the
// column stays nullable with a plain (non-unique) index. mask_emails opts a
// list into showing masked e-mail addresses to viewers without sensitive-data
// access.
func V6_21_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
		ALTER TABLE subscribers ADD COLUMN IF NOT EXISTS customer_code TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_subs_customer_code ON subscribers(customer_code);
		ALTER TABLE lists ADD COLUMN IF NOT EXISTS mask_emails BOOLEAN NOT NULL DEFAULT false;
	`)
	return err
}
