package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// CampaignTrackingRecipientSchema is also defined in schema.sql for fresh installs.
const CampaignTrackingRecipientSchema = `
CREATE OR REPLACE FUNCTION resolve_campaign_tracking_recipient(campaign_uuid UUID, recipient_ref TEXT)
RETURNS TABLE (campaign_id INT, customer_id INT, pool_contact_id BIGINT)
AS $$
    WITH ordinary AS (
        SELECT r.campaign_id, r.customer_id
        FROM resolve_campaign_recipient(campaign_uuid, recipient_ref) r
    )
    SELECT r.campaign_id, r.customer_id, NULL::BIGINT FROM ordinary r
    UNION ALL
    SELECT cpr.campaign_id, NULL::INT, cpr.pool_contact_id
    FROM campaigns c
    JOIN campaign_pool_recipients cpr ON cpr.campaign_id = c.id
    JOIN pool_contacts pc ON pc.id = cpr.pool_contact_id
    WHERE c.uuid = campaign_uuid
        AND pc.uuid = NULLIF(recipient_ref, '')::UUID
        AND NOT EXISTS (SELECT 1 FROM ordinary);
$$ LANGUAGE SQL STABLE;
`

// V6_46_0 records pool delivery times and binds tracking events to the pool
// recipient snapshot. Historical sent_at values are approximate: updated_at
// is the only timestamp retained for earlier pool deliveries.
func V6_46_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(`
		ALTER TABLE campaign_pool_recipients ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL;
		UPDATE campaign_pool_recipients SET sent_at = updated_at
		WHERE status = 'sent' AND sent_at IS NULL;

		ALTER TABLE campaign_views ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT NULL REFERENCES pool_contacts(id) ON DELETE SET NULL;
		ALTER TABLE link_clicks ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT NULL REFERENCES pool_contacts(id) ON DELETE SET NULL;
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'campaign_views_one_recipient') THEN
				ALTER TABLE campaign_views ADD CONSTRAINT campaign_views_one_recipient CHECK (customer_id IS NULL OR pool_contact_id IS NULL);
			END IF;
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'link_clicks_one_recipient') THEN
				ALTER TABLE link_clicks ADD CONSTRAINT link_clicks_one_recipient CHECK (customer_id IS NULL OR pool_contact_id IS NULL);
			END IF;
		END $$;
		CREATE INDEX IF NOT EXISTS idx_views_pool_contact_id ON campaign_views(pool_contact_id);
		CREATE INDEX IF NOT EXISTS idx_clicks_pool_contact_id ON link_clicks(pool_contact_id);
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(CampaignTrackingRecipientSchema)
	return err
}
