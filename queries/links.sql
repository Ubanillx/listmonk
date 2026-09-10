-- links
-- name: create-link
-- URLs remain globally deduplicated, while campaign_links records which
-- campaign actually emitted each tracking URL. The association is deliberately
-- separate from links because the same destination can appear in many campaigns.
WITH link AS (
    INSERT INTO links (uuid, url) VALUES($1, $2)
    ON CONFLICT (url) DO UPDATE SET url=EXCLUDED.url
    RETURNING id, uuid
), relation AS (
    INSERT INTO campaign_links (campaign_id, link_id)
    SELECT c.id, l.id
    FROM campaigns c
    CROSS JOIN link l
    WHERE c.uuid = $3::UUID
    ON CONFLICT (campaign_id, link_id) DO NOTHING
	RETURNING campaign_id
)
SELECT l.uuid
FROM link l
LEFT JOIN relation r ON TRUE
LIMIT 1;

-- name: get-link-url
SELECT url FROM links WHERE uuid = $1;

-- name: register-link-click
-- A link UUID is global, but an individual click must belong to an actual
-- campaign recipient. Aggregate tracking omits the customer UUID and still
-- records a campaign-level click. Existing campaigns are marked legacy by the
-- migration so their historical links remain valid without a new relation.
-- The binding rule, including the historical fallback, lives in
-- resolve_campaign_recipient (see schema.sql).
WITH link AS (
    SELECT id, url FROM links WHERE uuid = $1
),
campaign AS (
    SELECT id, organization_id, owner_user_id, tracking_links_mapped
    FROM campaigns WHERE uuid = $2::UUID
),
recipient AS (
    SELECT * FROM resolve_campaign_recipient($2::UUID, $3::TEXT)
)
INSERT INTO link_clicks (campaign_id, customer_id, link_id)
    SELECT c.id,
        CASE WHEN $3::TEXT = '' THEN NULL ELSE r.customer_id END,
        l.id
    FROM campaign c
    CROSS JOIN link l
    LEFT JOIN recipient r ON r.campaign_id = c.id
    WHERE (
        NOT c.tracking_links_mapped
        OR EXISTS (
            SELECT 1 FROM campaign_links cl
            WHERE cl.campaign_id = c.id AND cl.link_id = l.id
        )
    )
    AND ($3::TEXT = '' OR r.customer_id IS NOT NULL)
RETURNING (SELECT url FROM link);
