-- customers
-- name: get-customer
-- Get a single customer by id or UUID or email.
SELECT * FROM customers WHERE
    CASE
        WHEN $1 > 0 THEN id = $1
        WHEN $2 != '' THEN id IN (
            SELECT id FROM customers WHERE uuid = $2::UUID
            UNION
            SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $2::UUID
        )
        WHEN $3 != '' THEN email = $3
    END;

-- name: has-customer-list-memberships
-- Used for checking access permission by customer_list.
SELECT s.id AS customer_id,
    CASE
        WHEN EXISTS (SELECT 1 FROM customer_list_memberships sl WHERE sl.customer_id = s.id AND sl.customer_list_id = ANY($2))
        THEN TRUE
        ELSE FALSE
    END AS has
FROM customers s WHERE s.id = ANY($1);

-- name: get-customers-by-emails
-- Get customers by emails.
SELECT * FROM customers WHERE email=ANY($1);

-- name: get-customer-list-memberships
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
SELECT * FROM customer_lists
    LEFT JOIN customer_list_memberships ON (customer_lists.id = customer_list_memberships.customer_list_id)
    WHERE customer_id = (SELECT id FROM sub)
    -- Optional customer_list IDs or UUIDs to filter.
    AND (CASE WHEN CARDINALITY($3::INT[]) > 0 THEN id = ANY($3::INT[])
          WHEN CARDINALITY($4::UUID[]) > 0 THEN uuid = ANY($4::UUID[])
          ELSE TRUE
    END)
    AND (CASE WHEN $5 != '' THEN customer_list_memberships.status = $5::subscription_status ELSE TRUE END)
    AND (CASE WHEN $6 != '' THEN customer_lists.optin = $6::customer_list_optin ELSE TRUE END)
    ORDER BY id;

-- name: get-customer-list-memberships-lazy
-- Get customer_lists associations of customers given a customer_list of customer IDs.
-- This query is used to lazy load given a customer_list of customer IDs.
-- The query returns results in the same order as the given customer IDs, and for non-existent customer IDs,
-- the query still returns a row with 0 values. Thus, for lazy loading, the application simply iterate on the results in
-- the same order as the customer_list of campaigns it would've queried and attach the results.
WITH subs AS (
    SELECT customer_id, JSON_AGG(
        ROW_TO_JSON(
            (SELECT l FROM (
                SELECT
                    customer_list_memberships.status AS subscription_status,
                    customer_list_memberships.created_at AS subscription_created_at,
                    customer_list_memberships.updated_at AS subscription_updated_at,
                    customer_list_memberships.meta AS subscription_meta,
                    customer_lists.*
            ) l)
        )
    ) AS customer_lists FROM customer_lists
    LEFT JOIN customer_list_memberships ON (customer_list_memberships.customer_list_id = customer_lists.id)
    WHERE customer_list_memberships.customer_id = ANY($1)
    GROUP BY customer_id
)
SELECT id as customer_id,
    COALESCE(s.customer_lists, '[]') AS customer_lists
    FROM (SELECT id FROM UNNEST($1) AS id) x
    LEFT JOIN subs AS s ON (s.customer_id = id)
    ORDER BY ARRAY_POSITION($1, id);

-- name: get-subscriptions
-- Retrieves all customer_lists a customer is attached to.
-- if $3 is set to true, all customer_lists are fetched including the customer's subscriptions.
-- subscription_status, and subscription_created_at are null in that case.
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
SELECT customer_lists.*,
    customer_list_memberships.status as subscription_status,
    customer_list_memberships.created_at as subscription_created_at,
    customer_list_memberships.meta as subscription_meta
    FROM customer_lists LEFT JOIN customer_list_memberships
    ON (customer_list_memberships.customer_list_id = customer_lists.id AND customer_list_memberships.customer_id = (SELECT id FROM sub))
    WHERE CASE WHEN $3 = TRUE THEN TRUE ELSE customer_list_memberships.status IS NOT NULL END
    ORDER BY customer_list_memberships.status;

-- name: insert-customer
WITH sub AS (
    INSERT INTO customers (uuid, email, name, status, attribs)
    VALUES($1, $2, $3, $4, $5)
    RETURNING id, status
),
customer_list_ids AS (
    SELECT id FROM customer_lists WHERE
        (CASE WHEN CARDINALITY($6::INT[]) > 0 THEN id=ANY($6)
              ELSE uuid=ANY($7::UUID[]) END)
),
subs AS (
    INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    VALUES(
        (SELECT id FROM sub),
        UNNEST(ARRAY(SELECT id FROM customer_list_ids)),
        (CASE WHEN $4='blocklisted' THEN 'unsubscribed'::subscription_status ELSE $8::subscription_status END)
    )
    ON CONFLICT (customer_id, customer_list_id) DO UPDATE
        SET updated_at=NOW(),
            status=(
                CASE WHEN $4='blocklisted' OR (SELECT status FROM sub)='blocklisted'
                THEN 'unsubscribed'::subscription_status
                ELSE $8::subscription_status END
            )
)
SELECT id from sub;

-- name: upsert-customer
-- Upserts a customer while preserving existing attributes.
-- If $7 = true, update name. If $8 = true, update subscription status.
-- $9 = customer_code (always overwritten when non-empty).
WITH sub AS (
    INSERT INTO customers as s (uuid, email, name, attribs, status, customer_code)
    VALUES($1, $2, $3, $4, 'enabled', $9)
    ON CONFLICT ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
        WHERE owner_user_id IS NOT NULL
    DO UPDATE SET
        name=(CASE WHEN $7 THEN $3 ELSE s.name END),
        customer_code=(CASE WHEN $9 != '' THEN $9 ELSE s.customer_code END),
        updated_at=NOW()
    RETURNING uuid, id, status
),
subs AS (
    INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    SELECT sub.id, customerListID, CASE WHEN sub.status = 'blocklisted' THEN 'unsubscribed' ELSE $6::subscription_status END
    FROM sub, UNNEST($5::INT[]) AS customerListID
    ON CONFLICT (customer_id, customer_list_id) DO UPDATE
    SET updated_at = NOW(),
        status = CASE WHEN $8 THEN EXCLUDED.status ELSE customer_list_memberships.status END
)
SELECT uuid, id from sub;

-- name: upsert-blocklist-customer
-- Upserts a customer where the update will only set the status to blocklisted
-- unlike upsert-customers where names can be updated. In addition, all
-- existing subscriptions are marked as 'unsubscribed'.
-- This is used in the bulk importer.
WITH sub AS (
    INSERT INTO customers (uuid, email, name, attribs, status)
    VALUES($1, $2, $3, $4, 'blocklisted')
    ON CONFLICT ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
        WHERE owner_user_id IS NOT NULL
    DO UPDATE SET status='blocklisted', updated_at=NOW()
    RETURNING id
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE customer_id = (SELECT id FROM sub);

-- name: upsert-workspace-customer
-- Scoped importer variant. Legacy bootstrap records still use upsert-customer
-- because they are created before the first administrator exists.
-- $12 = customer_code (always overwritten when non-empty).
WITH active_membership AS (
    SELECT om.organization_id
    FROM organization_members om
    JOIN organizations o ON o.id = om.organization_id
    WHERE $9::BIGINT IS NOT NULL AND om.organization_id = $9::BIGINT
        AND om.user_id = $10 AND om.removed_at IS NULL AND o.status = 'active'
    FOR SHARE OF o, om
), active_workspace AS (
    SELECT 1 WHERE $9::BIGINT IS NULL
    UNION ALL
    SELECT 1 FROM active_membership
), sub AS (
    INSERT INTO customers AS s (
        uuid, email, name, attribs, status,
        organization_id, owner_user_id, original_owner_user_id, visibility,
        customer_code
    ) SELECT $1, $2, $3, $4, 'enabled', $9, $10, $11, 'private', $12
    FROM active_workspace
    ON CONFLICT ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
        WHERE owner_user_id IS NOT NULL
    DO UPDATE SET
        name=(CASE WHEN $7 THEN $3 ELSE s.name END),
        customer_code=(CASE WHEN $12 != '' THEN $12 ELSE s.customer_code END),
        updated_at=NOW()
    RETURNING uuid, id, status
),
subs AS (
    INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    SELECT sub.id, l.id,
        CASE WHEN sub.status = 'blocklisted' THEN 'unsubscribed' ELSE $6::subscription_status END
    FROM sub
    JOIN UNNEST($5::INT[]) AS requested_list(id) ON TRUE
    JOIN customer_lists l ON l.id = requested_list.id
        AND l.organization_id IS NOT DISTINCT FROM $9::BIGINT
        AND l.owner_user_id = $10
        AND l.transfer_pending_at IS NULL
    ON CONFLICT (customer_id, customer_list_id) DO UPDATE
    SET updated_at = NOW(),
        status = CASE WHEN $8 THEN EXCLUDED.status ELSE customer_list_memberships.status END
)
SELECT uuid, id FROM sub;

-- name: upsert-workspace-blocklist-customer
WITH active_membership AS (
    SELECT om.organization_id
    FROM organization_members om
    JOIN organizations o ON o.id = om.organization_id
    WHERE $5::BIGINT IS NOT NULL AND om.organization_id = $5::BIGINT
        AND om.user_id = $6 AND om.removed_at IS NULL AND o.status = 'active'
    FOR SHARE OF o, om
), active_workspace AS (
    SELECT 1 WHERE $5::BIGINT IS NULL
    UNION ALL
    SELECT 1 FROM active_membership
), sub AS (
    INSERT INTO customers AS s (
        uuid, email, name, attribs, status,
        organization_id, owner_user_id, original_owner_user_id, visibility
    ) SELECT $1, $2, $3, $4, 'blocklisted', $5, $6, $7, 'private'
    FROM active_workspace
    ON CONFLICT ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
        WHERE owner_user_id IS NOT NULL
    DO UPDATE SET status='blocklisted', updated_at=NOW()
    RETURNING id
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE customer_id = (SELECT id FROM sub);

-- name: update-customer
UPDATE customers SET
    email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
    name=(CASE WHEN $3 != '' THEN $3 ELSE name END),
    status=(CASE WHEN $4 != '' THEN $4::customer_status ELSE status END),
    attribs=(CASE WHEN $5 != '' THEN $5::JSONB ELSE attribs END),
    customer_code=(CASE WHEN $6 != '' THEN $6 ELSE customer_code END),
    updated_at=NOW()
WHERE id = $1;

-- name: update-customer-with-customer-lists
-- Updates a customer's data, and given a customer_list of customer_customer_list_ids, inserts subscriptions
-- for them while deleting existing subscriptions not in the customer_list.
-- $11 = customer_code (overwritten when non-empty).
WITH s AS (
    UPDATE customers SET
        email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
        name=(CASE WHEN $3 != '' THEN $3 ELSE name END),
        status=(CASE WHEN $4 != '' THEN $4::customer_status ELSE status END),
        attribs=(CASE WHEN $5 != '' THEN $5::JSONB ELSE attribs END),
        customer_code=(CASE WHEN $11 != '' THEN $11 ELSE customer_code END),
        updated_at=NOW()
    WHERE id = $1 RETURNING id
),
customer_list_ids AS (
    SELECT id FROM customer_lists WHERE
        (CASE WHEN CARDINALITY($6::INT[]) > 0 THEN id=ANY($6)
              ELSE uuid=ANY($7::UUID[]) END)
),
d AS (
    DELETE FROM customer_list_memberships WHERE $9 = TRUE AND customer_id = $1
        AND customer_list_id != ALL(SELECT id FROM customer_list_ids)
        AND (CARDINALITY($10::INT[]) = 0 OR customer_list_id = ANY($10::INT[]))
)
INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    VALUES(
        (SELECT id FROM s),
        UNNEST(ARRAY(SELECT id FROM customer_list_ids)),
        (CASE WHEN $4='blocklisted' THEN 'unsubscribed'::subscription_status ELSE $8::subscription_status END)
    )
    ON CONFLICT (customer_id, customer_list_id) DO UPDATE
    SET status = (
        CASE
            WHEN $4='blocklisted' THEN 'unsubscribed'::subscription_status
            -- When customer is edited from the admin form, retain the status. Otherwise, a blocklisted
            -- customer when being re-enabled, their subscription statuses change.
            WHEN customer_list_memberships.status = 'confirmed' THEN 'confirmed'
            WHEN customer_list_memberships.status = 'unsubscribed' THEN 'unsubscribed'::subscription_status
            ELSE $8::subscription_status
        END
    );

-- name: delete-customers
-- Delete one or more customers by ID or UUID.
DELETE FROM customers WHERE CASE
    WHEN ARRAY_LENGTH($1::INT[], 1) > 0 THEN id = ANY($1)
    ELSE id IN (
        SELECT id FROM customers WHERE uuid = ANY($2::UUID[])
        UNION
        SELECT customer_id FROM customer_uuid_aliases WHERE uuid = ANY($2::UUID[])
    )
END;

-- name: delete-blocklisted-customers
DELETE FROM customers WHERE status = 'blocklisted';

-- name: delete-orphan-customers
DELETE FROM customers a WHERE NOT EXISTS
    (SELECT 1 FROM customer_list_memberships b WHERE b.customer_id = a.id);

-- name: blocklist-customers
WITH b AS (
    UPDATE customers SET status='blocklisted', updated_at=NOW()
    WHERE id = ANY($1::INT[])
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE customer_id = ANY($1::INT[]);

-- name: add-customers-to-customer-lists
INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    (SELECT a, b, (CASE WHEN $3 != '' THEN $3::subscription_status ELSE 'unconfirmed' END) FROM UNNEST($1::INT[]) a, UNNEST($2::INT[]) b)
    ON CONFLICT (customer_id, customer_list_id) DO UPDATE SET status=(CASE WHEN $3 != '' THEN $3::subscription_status ELSE customer_list_memberships.status END);

-- name: delete-subscriptions
DELETE FROM customer_list_memberships
    WHERE (customer_id, customer_list_id) = ANY(SELECT a, b FROM UNNEST($1::INT[]) a, UNNEST($2::INT[]) b);

-- name: confirm-subscription-optin
WITH subID AS (
    SELECT id FROM customers
    WHERE id IN (
        SELECT id FROM customers WHERE uuid = $1::UUID
        UNION
        SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $1::UUID
    )
),
customer_list_ids AS (
    SELECT id FROM customer_lists WHERE uuid = ANY($2::UUID[])
)
UPDATE customer_list_memberships SET status='confirmed', meta=meta || $3, updated_at=NOW()
    WHERE customer_id = (SELECT id FROM subID) AND customer_list_id = ANY(SELECT id FROM customer_list_ids);

-- name: unsubscribe-customers-from-customer-lists
WITH customer_list_ids AS (
    SELECT ARRAY(
        SELECT id FROM customer_lists WHERE
        (CASE WHEN CARDINALITY($2::INT[]) > 0 THEN id=ANY($2) ELSE uuid=ANY($3::UUID[]) END)
    ) id
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE (customer_id, customer_list_id) = ANY(SELECT a, b FROM UNNEST($1::INT[]) a, UNNEST((SELECT id FROM customer_list_ids)) b);

-- name: unsubscribe-by-campaign
-- Unsubscribes a customer given a campaign UUID (from all the customer_lists in the campaign) and the customer UUID.
-- If $3 is TRUE, then all subscriptions of the customer is blocklisted
-- and all existing subscriptions, irrespective of customer_lists, unsubscribed.
-- The campaign and customer UUIDs come from a bearer unsubscribe link. They
-- must be linked by an actual campaign recipient before either the profile or
-- its subscriptions can be changed. The legacy fallback is restricted to the
-- same owner/workspace and only applies when no recipient snapshot exists.
WITH campaign AS (
    SELECT id, organization_id, owner_user_id
    FROM campaigns WHERE uuid = $1::UUID
),
customer AS (
    SELECT id, organization_id, owner_user_id
    FROM customers
    WHERE id IN (
        SELECT id FROM customers WHERE uuid = $2::UUID
        UNION
        SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $2::UUID
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
),
recipient AS (
    SELECT campaign_id, customer_id FROM snapshot_recipient
    UNION ALL
    SELECT campaign_id, customer_id FROM legacy_recipient
),
customer_lists AS (
    SELECT cl.customer_list_id FROM campaign_customer_lists cl
    JOIN recipient r ON r.campaign_id = cl.campaign_id
),
sub AS (
    UPDATE customers s
    SET status = (CASE WHEN $3 IS TRUE THEN 'blocklisted' ELSE s.status END)
    FROM recipient r
    WHERE s.id = r.customer_id
    RETURNING s.id
)
UPDATE customer_list_memberships SET status = 'unsubscribed', updated_at=NOW() WHERE
    customer_id = (SELECT id FROM sub) AND status != 'unsubscribed' AND
    -- If $3 is false, unsubscribe from the campaign's customer_lists, otherwise all customer_lists.
    CASE WHEN $3 IS FALSE THEN customer_list_id = ANY(SELECT customer_list_id FROM customer_lists) ELSE customer_list_id != 0 END;

-- name: delete-unconfirmed-subscriptions
WITH optins AS (
    SELECT id FROM customer_lists WHERE optin = 'double'
)
DELETE FROM customer_list_memberships
    WHERE status = 'unconfirmed' AND customer_list_id IN (SELECT id FROM optins) AND created_at < $1;


-- Partial and RAW queries used to construct arbitrary customer
-- queries for segmentation follow.

-- name: query-customers
-- raw: true
-- Unprepared statement for issuring arbitrary WHERE conditions for
-- searching customers. While the results are sliced using offset+limit,
-- there's a COUNT() OVER() that still returns the total result count
-- for pagination in the frontend, albeit being a field that'll repeat
-- with every resultant row.
SELECT customers.* FROM customers
    LEFT JOIN customer_list_memberships
    ON (
        -- Optional customer_list filtering.
        (CASE WHEN CARDINALITY($1::INT[]) > 0 THEN true ELSE false END)
        AND customer_list_memberships.customer_id = customers.id
        AND ($2 = '' OR customer_list_memberships.status = $2::subscription_status)
    )
    WHERE (CARDINALITY($1) = 0 OR customer_list_memberships.customer_list_id = ANY($1::INT[]))
    AND (CASE WHEN $3 != '' THEN name ~* $3 OR email ~* $3 ELSE TRUE END)
    AND %query%
    ORDER BY %order% OFFSET $4 LIMIT (CASE WHEN $5 < 1 THEN NULL ELSE $5 END);

-- name: query-customers-count
-- Replica of query-customers for obtaining the results count.
SELECT COUNT(*) AS total FROM customers
    LEFT JOIN customer_list_memberships
    ON (
        -- Optional customer_list filtering.
        (CASE WHEN CARDINALITY($1::INT[]) > 0 THEN true ELSE false END)
        AND customer_list_memberships.customer_id = customers.id
        AND ($2 = '' OR customer_list_memberships.status = $2::subscription_status)
    )
    WHERE (CARDINALITY($1) = 0 OR customer_list_memberships.customer_list_id = ANY($1::INT[]))
    AND (CASE WHEN $3 != '' THEN name ~* $3 OR email ~* $3 ELSE TRUE END)
    AND %query%;

-- name: query-customers-count-all
-- Cached query for getting the "all" customer count without arbitrary conditions.
SELECT COALESCE(SUM(customer_count), 0) AS total FROM mat_customer_list_customer_stats
    WHERE customer_list_id = ANY(CASE WHEN CARDINALITY($1::INT[]) > 0 THEN $1 ELSE '{0}' END)
    AND ($2 = '' OR status = $2::subscription_status);

-- name: query-customers-for-export
-- raw: true
-- Unprepared statement for issuring arbitrary WHERE conditions for
-- searching customers to do bulk CSV export.
SELECT customers.id,
       customers.uuid,
       customers.email,
       customers.name,
       customers.status,
       customers.attribs,
       customers.customer_code,
       customers.created_at,
       customers.updated_at
       FROM customers
    LEFT JOIN customer_list_memberships
    ON (
        -- Optional customer_list filtering.
        (CASE WHEN CARDINALITY($1::INT[]) > 0 THEN true ELSE false END)
        AND customer_list_memberships.customer_id = customers.id
        AND ($4 = '' OR customer_list_memberships.status = $4::subscription_status)
    )
    WHERE customer_list_memberships.customer_list_id = ALL($1::INT[]) AND id > $2
    AND (CASE WHEN CARDINALITY($3::INT[]) > 0 THEN id=ANY($3) ELSE true END)
    AND (CASE WHEN $5 != '' THEN name ~* $5 OR email ~* $5 ELSE TRUE END)
    AND %query%
    ORDER BY customers.id ASC LIMIT (CASE WHEN $6 < 1 THEN NULL ELSE $6 END);

-- name: query-customers-template
-- raw: true
-- This raw query is reused in multiple queries (blocklist, add to customer_list, delete)
-- etc., so it's kept has a raw template to be injected into other raw queries,
-- and for the same reason, it is not terminated with a semicolon.
--
-- All queries that embed this query should expect
-- $1=true/false (dry-run or not) and $2=[]INT (option customer_list IDs).
-- That is, their positional arguments should start from $4.
SELECT customers.id FROM customers
LEFT JOIN customer_list_memberships
ON (
    -- Optional customer_list filtering.
    (CASE WHEN CARDINALITY($2::INT[]) > 0 THEN true ELSE false END)
    AND customer_list_memberships.customer_id = customers.id
    AND ($3 = '' OR customer_list_memberships.status = $3::subscription_status)
)
WHERE customer_list_memberships.customer_list_id = ALL($2::INT[])
    AND (CASE WHEN $4 != '' THEN name ~* $4 OR email ~* $4 ELSE TRUE END)
    AND %query%
LIMIT (CASE WHEN $1 THEN 1 END)

-- name: delete-customers-by-query
-- raw: true
WITH subs AS (%query%)
DELETE FROM customers WHERE id=ANY(SELECT id FROM subs);

-- name: blocklist-customers-by-query
-- raw: true
WITH subs AS (%query%),
b AS (
    UPDATE customers SET status='blocklisted', updated_at=NOW()
    WHERE id = ANY(SELECT id FROM subs)
)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE customer_id = ANY(SELECT id FROM subs);

-- name: add-customers-to-customer-lists-by-query
-- raw: true
WITH subs AS (%query%)
INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
    (SELECT a, b, (CASE WHEN $6 != '' THEN $6::subscription_status ELSE 'unconfirmed' END) FROM UNNEST(ARRAY(SELECT id FROM subs)) a, UNNEST($5::INT[]) b)
    ON CONFLICT (customer_id, customer_list_id) DO NOTHING;

-- name: delete-subscriptions-by-query
-- raw: true
WITH subs AS (%query%)
DELETE FROM customer_list_memberships
    WHERE (customer_id, customer_list_id) = ANY(SELECT a, b FROM UNNEST(ARRAY(SELECT id FROM subs)) a, UNNEST($5::INT[]) b);

-- name: unsubscribe-customers-from-customer-lists-by-query
-- raw: true
WITH subs AS (%query%)
UPDATE customer_list_memberships SET status='unsubscribed', updated_at=NOW()
    WHERE (customer_id, customer_list_id) = ANY(SELECT a, b FROM UNNEST(ARRAY(SELECT id FROM subs)) a, UNNEST($5::INT[]) b);


-- privacy
-- name: export-customer-data
WITH prof AS (
    SELECT id, uuid, email, name, attribs, status, created_at, updated_at FROM customers WHERE
    CASE
        WHEN $1 > 0 THEN id = $1
        ELSE id IN (
            SELECT id FROM customers WHERE uuid = $2::UUID
            UNION
            SELECT customer_id FROM customer_uuid_aliases WHERE uuid = $2::UUID
        )
    END
),
subs AS (
    SELECT customer_list_memberships.status AS subscription_status,
            (CASE WHEN customer_lists.type = 'private' THEN 'Private customer_list' ELSE customer_lists.name END) as name,
            customer_lists.type, customer_list_memberships.created_at
    FROM customer_lists
    LEFT JOIN customer_list_memberships ON (customer_list_memberships.customer_list_id = customer_lists.id)
    WHERE customer_list_memberships.customer_id = (SELECT id FROM prof)
),
views AS (
    SELECT subject as campaign, COUNT(customer_id) as views FROM campaign_views
        LEFT JOIN campaigns ON (campaigns.id = campaign_views.campaign_id)
        WHERE customer_id = (SELECT id FROM prof)
        GROUP BY campaigns.id ORDER BY campaigns.id
),
clicks AS (
    SELECT url, COUNT(customer_id) as clicks FROM link_clicks
        LEFT JOIN links ON (links.id = link_clicks.link_id)
        WHERE customer_id = (SELECT id FROM prof)
        GROUP BY links.id ORDER BY links.id
)
SELECT (SELECT email FROM prof) as email,
        COALESCE((SELECT JSON_AGG(t) FROM prof t), '{}') AS profile,
        COALESCE((SELECT JSON_AGG(t) FROM subs t), '[]') AS subscriptions,
        COALESCE((SELECT JSON_AGG(t) FROM views t), '[]') AS campaign_views,
        COALESCE((SELECT JSON_AGG(t) FROM clicks t), '[]') AS link_clicks;

-- name: get-customer-activity
-- Gets the customer's campaign views and link clicks with detailed information
-- for display in the Activity tab
WITH views AS (
    SELECT
        c.id,
        c.uuid,
        c.name,
        c.subject,
        COUNT(*) as view_count,
        MAX(cv.created_at) as last_viewed_at
    FROM campaign_views cv
    LEFT JOIN campaigns c ON c.id = cv.campaign_id
    WHERE cv.customer_id = $1
    GROUP BY c.id, c.uuid, c.name, c.subject
    ORDER BY last_viewed_at DESC
),
clicks AS (
    SELECT
        l.id as link_id,
        l.url,
        c.id as campaign_id,
        c.uuid as campaign_uuid,
        c.name as campaign_name,
        c.subject as campaign_subject,
        COUNT(*) as click_count,
        MAX(lc.created_at) as last_clicked_at
    FROM link_clicks lc
    LEFT JOIN links l ON l.id = lc.link_id
    LEFT JOIN campaigns c ON c.id = lc.campaign_id
    WHERE lc.customer_id = $1
    GROUP BY l.id, l.url, c.id, c.uuid, c.name, c.subject
    ORDER BY last_clicked_at DESC
)
SELECT
    COALESCE((SELECT JSON_AGG(v) FROM views v), '[]') as campaign_views,
    COALESCE((SELECT JSON_AGG(c) FROM clicks c), '[]') as link_clicks;
