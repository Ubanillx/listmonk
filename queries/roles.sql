-- name: get-user-roles
WITH mainroles AS (
    SELECT ur.* FROM roles ur WHERE type = 'user' AND ur.parent_id IS NULL AND
    CASE WHEN $1::INT != 0 THEN ur.id = $1 ELSE TRUE END
),
customerListPerms AS (
    SELECT ur.parent_id, JSONB_AGG(JSONB_BUILD_OBJECT('id', ur.customer_list_id, 'name', customer_lists.name, 'permissions', ur.permissions)) AS customerListPerms
    FROM roles ur
    LEFT JOIN customer_lists ON(customer_lists.id = ur.customer_list_id)
    WHERE ur.parent_id IS NOT NULL GROUP BY ur.parent_id
)
SELECT p.*, COALESCE(l.customerListPerms, '[]'::JSONB) AS "list_permissions" FROM mainroles p
    LEFT JOIN customerListPerms l ON p.id = l.parent_id ORDER BY p.created_at;

-- name: get-customer_list-roles
WITH mainroles AS (
    SELECT ur.* FROM roles ur WHERE type = 'customer_list' AND ur.parent_id IS NULL
),
customerListPerms AS (
    SELECT ur.parent_id, JSONB_AGG(JSONB_BUILD_OBJECT('id', ur.customer_list_id, 'name', customer_lists.name, 'permissions', ur.permissions)) AS customerListPerms
    FROM roles ur
    LEFT JOIN customer_lists ON(customer_lists.id = ur.customer_list_id)
    WHERE ur.parent_id IS NOT NULL GROUP BY ur.parent_id
)
SELECT p.*, COALESCE(l.customerListPerms, '[]'::JSONB) AS "list_permissions" FROM mainroles p
    LEFT JOIN customerListPerms l ON p.id = l.parent_id ORDER BY p.created_at;


-- name: create-role
INSERT INTO roles (name, type, permissions, created_at, updated_at) VALUES($1, $2, $3, NOW(), NOW()) RETURNING *;

-- name: upsert-customer_list-permissions
WITH d AS (
    -- Delete customer_lists that aren't included.
    DELETE FROM roles WHERE parent_id = $1 AND customer_list_id != ALL($2::INT[])
),
p AS (
    -- Get (customer_list_id, perms[]), (customer_list_id, perms[])
    SELECT UNNEST($2) AS customer_list_id, JSONB_ARRAY_ELEMENTS(TO_JSONB($3::TEXT[][])) AS perms
)
INSERT INTO roles (parent_id, customer_list_id, permissions, type)
    SELECT $1, customer_list_id, ARRAY_REMOVE(ARRAY(SELECT JSONB_ARRAY_ELEMENTS_TEXT(perms)), ''), 'customer_list' FROM p
    ON CONFLICT (parent_id, customer_list_id) DO UPDATE SET permissions = EXCLUDED.permissions;

-- name: delete-customer_list-permission
DELETE FROM roles WHERE parent_id=$1 AND customer_list_id=$2;

-- name: update-role
UPDATE roles SET name=$2, permissions=$3 WHERE id=$1 and parent_id IS NULL RETURNING *;

-- name: delete-role
DELETE FROM roles WHERE id=$1;
