package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// CampaignRecipientResolutionSchema adds the single definition of how a public
// bearer (campaign UUID + customer UUID) is bound to an actual recipient of that
// campaign.
//
// Before this, four security-sensitive queries each repeated the same CTE: the
// archive render lookup (get-public-campaign-recipient), view registration
// (register-campaign-view), link-click recording (register-link-click) and the
// bearer unsubscribe (unsubscribe-by-campaign). They had already diverged on how
// an empty customer reference is handled, so the same bearer could be judged
// differently depending on which event path received it.
//
// CREATE OR REPLACE keeps the migration idempotent. The body must stay identical
// to the definition in schema.sql so a fresh install and an upgraded install
// behave the same.
const CampaignRecipientResolutionSchema = `
CREATE OR REPLACE FUNCTION resolve_campaign_recipient(campaign_uuid UUID, customer_ref TEXT)
RETURNS TABLE (campaign_id INT, customer_id INT)
AS $$
    WITH campaign AS (
        SELECT id, organization_id, owner_user_id
        FROM campaigns WHERE uuid = campaign_uuid
    ),
    customer AS (
        SELECT id, organization_id, owner_user_id
        FROM customers
        WHERE id IN (
            SELECT id FROM customers WHERE uuid = NULLIF(customer_ref, '')::UUID
            UNION
            SELECT customer_id FROM customer_uuid_aliases WHERE uuid = NULLIF(customer_ref, '')::UUID
        )
    ),
    snapshot_recipient AS (
        SELECT c.id AS campaign_id, s.id AS customer_id
        FROM campaign c
        JOIN customer s ON TRUE
        WHERE EXISTS (
            SELECT 1 FROM campaign_recipients cr
            WHERE cr.campaign_id = c.id AND cr.customer_id = s.id
        )
    ),
    legacy_recipient AS (
        SELECT c.id AS campaign_id, s.id AS customer_id
        FROM campaign c
        JOIN customer s ON TRUE
        WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
            AND s.organization_id IS NOT DISTINCT FROM c.organization_id
            AND s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
            AND EXISTS (
                SELECT 1 FROM campaign_customer_lists cl
                JOIN customer_list_memberships sl ON sl.customer_list_id = cl.customer_list_id
                WHERE cl.campaign_id = c.id AND sl.customer_id = s.id
            )
    )
    SELECT campaign_id, customer_id FROM snapshot_recipient
    UNION ALL
    SELECT campaign_id, customer_id FROM legacy_recipient;
$$ LANGUAGE SQL STABLE;
`

// V6_30_0 installs the shared campaign recipient resolution function.
func V6_30_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(CampaignRecipientResolutionSchema)
	return err
}
