package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_10_0 records the relationship between a tracking URL and the campaign
// that emitted it. URLs are globally deduplicated, so this prevents a known
// link UUID from being attributed to an unrelated campaign.
func V6_10_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo

	_, err := db.Exec(`
		-- Existing campaigns may contain links sent before campaign_links
		-- existed. Keep those URLs functional, while every subsequently
		-- created campaign defaults to strict per-campaign link mapping.
		ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS tracking_links_mapped BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE campaigns ALTER COLUMN tracking_links_mapped SET DEFAULT TRUE;

		CREATE TABLE IF NOT EXISTS campaign_links (
			campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
			link_id INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE ON UPDATE CASCADE,
			PRIMARY KEY (campaign_id, link_id)
		);
		CREATE INDEX IF NOT EXISTS idx_campaign_links_link_id ON campaign_links(link_id);
	`)
	return err
}
