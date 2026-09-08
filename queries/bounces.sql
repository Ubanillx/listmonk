-- name: record-bounce
-- Insert a bounce and count the bounces for the customer and either unsubscribe them,
WITH camp AS (
    SELECT id, organization_id, owner_user_id FROM campaigns WHERE $3 != '' AND uuid = $3::UUID
),
sub AS (
    SELECT s.id, s.status
    FROM customers s
    WHERE (
        CASE WHEN $1 != '' THEN s.id IN (
            SELECT id FROM customers WHERE uuid = $1::UUID
            UNION
            SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $1::UUID
        ) ELSE LOWER(s.email) = LOWER($2) END
    )
        AND (
            CASE
                -- A campaign-bound bounce must resolve to a customer in
                -- that campaign owner's workspace. This avoids an identical
                -- e-mail in another organization being modified.
                WHEN $3 != '' THEN s.organization_id IS NOT DISTINCT FROM (SELECT organization_id FROM camp)
                    AND s.owner_user_id = (SELECT owner_user_id FROM camp)
                -- UUIDs are globally unique. E-mail-only bounces are safe
                -- only when the address exists in exactly one workspace.
                WHEN $1 != '' THEN TRUE
                ELSE 1 = (SELECT COUNT(*) FROM customers sx WHERE LOWER(sx.email) = LOWER($2))
            END
        )
    LIMIT 1
),
num AS (
    -- Add a +1 to include the current insertion that is happening.
    SELECT COUNT(*) + 1 AS num FROM bounces WHERE customer_id = (SELECT id FROM sub) AND type = $4
),
-- block1 and block2 will run when $8 = 'blocklist' and the number of bounces exceed $8.
block1 AS (
    UPDATE customers SET status='blocklisted'
    WHERE $9 = 'blocklist' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
),
block2 AS (
    UPDATE customer_list_memberships SET status='unsubscribed'
    WHERE $9 = 'unsubscribe' AND (SELECT num FROM num) >= $8 AND customer_id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
),
bounce AS (
    -- Record the bounce if the customer is not already blocklisted;
    INSERT INTO bounces (customer_id, campaign_id, type, source, meta, created_at)
    SELECT (SELECT id FROM sub), (SELECT id FROM camp), $4, $5, $6, $7
    WHERE (SELECT id FROM sub) IS NOT NULL
        AND NOT EXISTS (SELECT 1 WHERE (SELECT status FROM sub) = 'blocklisted' OR (SELECT num FROM num) > $8)
)
-- This delete  will only run when $9 = 'delete' and the number of bounces exceed $8.
DELETE FROM customers
    WHERE $9 = 'delete' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub);

-- name: query-bounces
SELECT COUNT(*) OVER () AS total,
    bounces.id,
    bounces.type,
    bounces.source,
    bounces.meta,
    bounces.created_at,
    bounces.customer_id,
    bounces.pool_contact_id,
    bounces.source_pool_id,
    bounces.source_segment_id,
    bounces.source_organization_id,
    customers.uuid AS customer_uuid,
    COALESCE(customers.email, pool_contacts.email, '') AS email,
    COALESCE(customers.status, pool_contacts.status, '') as customer_status,
    (
        CASE WHEN bounces.campaign_id IS NOT NULL
        THEN JSON_BUILD_OBJECT('id', bounces.campaign_id, 'name', campaigns.name)
        ELSE NULL END
    ) AS campaign
FROM bounces
LEFT JOIN customers ON (customers.id = bounces.customer_id)
LEFT JOIN pool_contacts ON (pool_contacts.id = bounces.pool_contact_id)
LEFT JOIN campaigns ON (campaigns.id = bounces.campaign_id)
WHERE ($1 = 0 OR bounces.id = $1)
    AND ($2 = 0 OR bounces.campaign_id = $2)
    AND ($3 = 0 OR bounces.customer_id = $3)
    AND ($4 = '' OR bounces.source = $4)
ORDER BY %order% OFFSET $5 LIMIT (CASE WHEN $6 < 1 THEN NULL ELSE $6 END);

-- name: delete-bounces
DELETE FROM bounces WHERE $2 = TRUE OR id = ANY($1);

-- name: delete-bounces-by-customer
WITH sub AS (
    SELECT id FROM customers WHERE CASE
        WHEN $1 > 0 THEN id = $1
        ELSE id IN (
            SELECT id FROM customers WHERE uuid = $2::UUID
            UNION
            SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $2::UUID
        )
    END
)
DELETE FROM bounces WHERE customer_id = (SELECT id FROM sub);

-- name: blocklist-bounced-customers
WITH subs AS (
    SELECT customer_id FROM bounces
),
b AS (
    UPDATE customers SET status='blocklisted', updated_at=NOW()
    WHERE id = ANY(SELECT customer_id FROM subs)
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE customer_id = ANY(SELECT customer_id FROM subs);
