-- customer_lists
-- name: get-customer-lists
SELECT * FROM customer_lists WHERE (CASE WHEN $1 = '' THEN 1=1 ELSE type=$1::customer_list_type END)
    AND (CASE WHEN $2 = '' THEN 1=1 ELSE status=$2::customer_list_status END)
    AND CASE
        -- Optional customer_list IDs based on user permission.
        WHEN $4 = TRUE THEN TRUE ELSE id = ANY($5::INT[])
    END
    ORDER BY CASE WHEN $3 = 'id' THEN id END, CASE WHEN $3 = 'name' THEN name END;

-- name: query-customer-lists
WITH ls AS (
    SELECT COUNT(*) OVER () AS total, customer_lists.* FROM customer_lists WHERE
    CASE
        WHEN $1 > 0 THEN id = $1
        WHEN $2 != '' THEN uuid = $2::UUID
        WHEN $3 != '' THEN (TO_TSVECTOR(name) @@ TO_TSQUERY ($3) OR name ILIKE $3)
        ELSE TRUE
    END
    AND ($4 = '' OR type = $4::customer_list_type)
    AND ($5 = '' OR optin = $5::customer_list_optin)
    AND ($6 = '' OR status = $6::customer_list_status)
    AND (CARDINALITY($7::VARCHAR(100)[]) = 0 OR $7 <@ tags)
    AND CASE
        -- Optional customer_list IDs based on user permission.
        WHEN $8 = TRUE THEN TRUE ELSE id = ANY($9::INT[])
    END
    OFFSET $10 LIMIT (CASE WHEN $11 < 1 THEN NULL ELSE $11 END)
),
statuses AS (
    SELECT
        customer_list_id,
        COALESCE(JSONB_OBJECT_AGG(status, customer_count) FILTER (WHERE status IS NOT NULL), '{}') AS customer_statuses,
        SUM(customer_count) AS customer_count
    FROM mat_customer_list_customer_stats
    GROUP BY customer_list_id
)
SELECT ls.*, COALESCE(ss.customer_statuses, '{}') AS customer_statuses, COALESCE(ss.customer_count, 0) AS customer_count
    FROM ls LEFT JOIN statuses ss ON (ls.id = ss.customer_list_id) ORDER BY %order%;

-- name: get-customer-lists-by-optin
-- Can have a customer_list of IDs or a customer_list of UUIDs.
SELECT * FROM customer_lists WHERE (CASE WHEN $1 != '' THEN optin=$1::customer_list_optin ELSE TRUE END) AND
    (CASE WHEN $2::INT[] IS NOT NULL THEN id = ANY($2::INT[])
          WHEN $3::UUID[] IS NOT NULL THEN uuid = ANY($3::UUID[])
    END) ORDER BY name;

-- name: get-customer-list-types
-- Retrieves the private|public type of customer_lists by ID or uuid. Used for filtering.
SELECT id, uuid, type FROM customer_lists WHERE
    (CASE WHEN $1::INT[] IS NOT NULL THEN id = ANY($1::INT[])
          WHEN $2::UUID[] IS NOT NULL THEN uuid = ANY($2::UUID[])
    END);

-- name: create-customer-list
INSERT INTO customer_lists (
    uuid, name, type, optin, status, tags, description, mask_emails,
    organization_id, owner_user_id, original_owner_user_id, visibility
) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id;

-- name: update-customer-list
WITH l AS (
    UPDATE customer_lists SET
        name=(CASE WHEN $2 != '' THEN $2 ELSE name END),
        type=(CASE WHEN $3 != '' THEN $3::customer_list_type ELSE type END),
        optin=(CASE WHEN $4 != '' THEN $4::customer_list_optin ELSE optin END),
        status=(CASE WHEN $5 != '' THEN $5::customer_list_status ELSE status END),
        tags=$6::VARCHAR(100)[],
        description=(CASE WHEN $7 != '' THEN $7 ELSE description END),
        mask_emails=$8,
        updated_at=NOW()
    WHERE id = $1
    RETURNING id, name
),
c AS (
    UPDATE campaign_customer_lists SET customer_list_name = l.name FROM l WHERE campaign_customer_lists.customer_list_id = l.id RETURNING 1
)
SELECT COUNT(*) FROM l, c;

-- name: update-customer-lists-date
UPDATE customer_lists SET updated_at=NOW() WHERE id = ANY($1);

-- name: delete-customer-lists
DELETE FROM customer_lists
WHERE CASE
    WHEN CARDINALITY($1::INT[]) > 0 THEN id = ANY($1)
    ELSE ($2 = '' OR to_tsvector(name) @@ to_tsquery($2))
END
AND CASE
    -- Optional customer_list IDs based on user permission.
    WHEN $3 = TRUE THEN TRUE ELSE id = ANY($4::INT[])
END;
