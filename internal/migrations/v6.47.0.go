package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_47_0 stores approximate city-level location on campaign open events.
// Historical opens cannot be geolocated because their IP addresses were never retained.
func V6_47_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS country_code TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS country TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS region TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '';
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION NULL;
		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION NULL;
	`)
	return err
}
