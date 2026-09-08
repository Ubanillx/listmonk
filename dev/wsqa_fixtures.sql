-- Isolated WS-capability QA fixtures. All names prefixed wsqa_.
-- Cleanup is possible with: DELETE FROM users WHERE username LIKE 'wsqa\_%'; DELETE FROM roles WHERE name = 'wsqa-perm-role';

-- 1) A full user role that INCLUDES workspaces:personal.
INSERT INTO roles (type, name, permissions)
SELECT 'user', 'wsqa-perm-role', ARRAY[
    'customers:get','customers:get_all','customers:manage','customers:import','tx:send',
    'campaigns:get','campaigns:get_all','campaigns:get_analytics','campaigns:manage','campaigns:manage_all',
    'bounces:get','bounces:manage','webhooks:post_bounce',
    'media:get','media:manage','templates:get','templates:manage',
    'customer_lists:get_all','customer_lists:manage_all','workspaces:personal']
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE type = 'user' AND name = 'wsqa-perm-role');

-- 2) Three users sharing the password Test@1234 (bcrypt via CRYPT).
--    wsqa_noperm: role 4 ("user", full perms, NO workspaces:personal), member of org 1.
--    wsqa_perm:   role with workspaces:personal, NO organizations.
--    wsqa_multi:  role 4 (NO personal), member of org 1 and org 2.
INSERT INTO users (username, password_login, password, email, name, type, user_role_id, status)
SELECT 'wsqa_noperm', TRUE, CRYPT('Test@1234', GEN_SALT('bf')), 'wsqa_noperm@example.test', 'WSQA NoPerm', 'user',
    (SELECT id FROM roles WHERE type = 'user' AND name = 'user' AND id = 4), 'enabled'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = 'wsqa_noperm');

INSERT INTO users (username, password_login, password, email, name, type, user_role_id, status)
SELECT 'wsqa_perm', TRUE, CRYPT('Test@1234', GEN_SALT('bf')), 'wsqa_perm@example.test', 'WSQA Perm', 'user',
    (SELECT id FROM roles WHERE type = 'user' AND name = 'wsqa-perm-role'), 'enabled'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = 'wsqa_perm');

INSERT INTO users (username, password_login, password, email, name, type, user_role_id, status)
SELECT 'wsqa_multi', TRUE, CRYPT('Test@1234', GEN_SALT('bf')), 'wsqa_multi@example.test', 'WSQA Multi', 'user',
    (SELECT id FROM roles WHERE type = 'user' AND name = 'user' AND id = 4), 'enabled'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = 'wsqa_multi');

-- 3) Memberships.
INSERT INTO organization_members (organization_id, user_id, role)
SELECT 1, u.id, 'member' FROM users u WHERE u.username = 'wsqa_noperm'
ON CONFLICT (organization_id, user_id) DO NOTHING;

INSERT INTO organization_members (organization_id, user_id, role)
SELECT 1, u.id, 'member' FROM users u WHERE u.username = 'wsqa_multi'
ON CONFLICT (organization_id, user_id) DO NOTHING;

INSERT INTO organization_members (organization_id, user_id, role)
SELECT 2, u.id, 'member' FROM users u WHERE u.username = 'wsqa_multi'
ON CONFLICT (organization_id, user_id) DO NOTHING;

-- 4) Retained personal data for wsqa_noperm (a personal list + a personal template),
--    which must stay visible to the migration read exemption after the capability is gone.
INSERT INTO customer_lists (uuid, name, type, optin, status, tags, owner_user_id, organization_id, visibility, created_at, updated_at)
SELECT gen_random_uuid(), 'wsqa-personal-list', 'private', 'single', 'active', '{}', u.id, NULL, 'private', NOW(), NOW()
FROM users u WHERE u.username = 'wsqa_noperm'
AND NOT EXISTS (SELECT 1 FROM customer_lists l WHERE l.name = 'wsqa-personal-list');
