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
-- campaign recipient. Aggregate tracking omits the subscriber UUID and still
-- records a campaign-level click. Existing campaigns are marked legacy by the
-- migration so their historical links remain valid without a new relation.
WITH link AS (
    SELECT id, url FROM links WHERE uuid = $1
),
campaign AS (
    SELECT id, organization_id, owner_user_id, tracking_links_mapped
    FROM campaigns WHERE uuid = $2::UUID
),
subscriber AS (
    SELECT id, organization_id, owner_user_id
    FROM subscribers
    WHERE id IN (
        SELECT id FROM subscribers WHERE uuid = NULLIF($3::TEXT, '')::UUID
        UNION
        SELECT subscriber_id FROM subscriber_uuid_aliases WHERE uuid = NULLIF($3::TEXT, '')::UUID
    )
),
snapshot_recipient AS (
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE EXISTS (
        SELECT 1 FROM campaign_recipients cr
        WHERE cr.campaign_id = c.id AND cr.subscriber_id = s.id
    )
),
legacy_recipient AS (
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
        AND EXISTS (
            SELECT 1 FROM campaign_lists cl
            JOIN subscriber_lists sl ON sl.list_id = cl.list_id
            WHERE cl.campaign_id = c.id AND sl.subscriber_id = s.id
        )
),
recipient AS (
    SELECT campaign_id, subscriber_id FROM snapshot_recipient
    UNION ALL
    SELECT campaign_id, subscriber_id FROM legacy_recipient
)
INSERT INTO link_clicks (campaign_id, subscriber_id, link_id)
    SELECT c.id,
        CASE WHEN $3::TEXT = '' THEN NULL ELSE r.subscriber_id END,
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
    AND ($3::TEXT = '' OR r.subscriber_id IS NOT NULL)
RETURNING (SELECT url FROM link);
