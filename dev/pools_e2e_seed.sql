-- Public-pool E2E fixture for the Docker development database.
-- Run with: docker exec -i dev-db-1 psql -U <user> -d <db> < dev/pools_e2e_seed.sql
-- All names are prefixed wsqa-pool_ and are safe to remove after the run.
BEGIN;

INSERT INTO users (username, password_login, password, email, name, type, user_role_id, status)
SELECT 'wsqa_pool_manager', TRUE, crypt('Test@1234', gen_salt('bf')), 'wsqa_pool_manager@example.test', 'Pool QA Manager', 'user', 4, 'enabled'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username='wsqa_pool_manager');

INSERT INTO organization_members (organization_id, user_id, role)
SELECT 1, id, 'manager' FROM users WHERE username='wsqa_pool_manager'
ON CONFLICT (organization_id,user_id) DO UPDATE SET role='manager', removed_at=NULL;

-- A separate organization deliberately has no public-pool delivery grant.
-- It makes the browser regression self-contained: the member must not see a
-- public-pool management entry point and the first-level list endpoint must
-- reject direct detail requests for this workspace.
INSERT INTO organizations (name,description,status,created_by_user_id)
SELECT 'wsqa-pool-unassigned-org','Pool E2E organization without delivery access','active',1
WHERE NOT EXISTS (SELECT 1 FROM organizations WHERE name='wsqa-pool-unassigned-org');

-- This organization intentionally starts without a pool allocation. It drives
-- the highest-admin create-and-bind workflow without conflicting with the
-- primary test organization's already-bound allocation.
INSERT INTO organizations (name,description,status,created_by_user_id)
SELECT 'wsqa-pool-guided-org','Pool E2E organization for pool-allocation creation','active',1
WHERE NOT EXISTS (SELECT 1 FROM organizations WHERE name='wsqa-pool-guided-org');

INSERT INTO users (username, password_login, password, email, name, type, user_role_id, status)
SELECT 'wsqa_pool_unassigned', TRUE, crypt('Test@1234', gen_salt('bf')), 'wsqa_pool_unassigned@example.test',
  'Pool QA Unassigned', 'user', 4, 'enabled'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username='wsqa_pool_unassigned');

INSERT INTO organization_members (organization_id, user_id, role)
SELECT o.id, u.id, 'member'
FROM organizations o CROSS JOIN users u
WHERE o.name='wsqa-pool-unassigned-org' AND u.username='wsqa_pool_unassigned'
ON CONFLICT (organization_id,user_id) DO UPDATE SET role='member', removed_at=NULL;

INSERT INTO customer_lists (uuid,name,type,optin,status,tags,description,mask_emails,organization_id,owner_user_id,visibility)
SELECT gen_random_uuid(),'wsqa-pool-primary','pool','single','active','{}','pool E2E fixture',true,NULL,1,'global'
WHERE NOT EXISTS (SELECT 1 FROM customer_lists WHERE name='wsqa-pool-primary');

INSERT INTO customer_lists (uuid,name,type,optin,status,tags,description,mask_emails,organization_id,owner_user_id,visibility,pool_parent_id)
SELECT gen_random_uuid(),'wsqa-org-pool-allocation','org_pool_allocation','single','active','{}','allocation E2E fixture',true,1,u.id,'organization',p.id
FROM users u, customer_lists p
WHERE u.id=1 AND p.name='wsqa-pool-primary'
  AND NOT EXISTS (SELECT 1 FROM customer_lists WHERE name='wsqa-org-pool-allocation');

INSERT INTO org_pool_allocations (list_id,pool_id,organization_id,created_by_user_id)
SELECT s.id,p.id,1,1
FROM customer_lists s, customer_lists p
WHERE s.name='wsqa-org-pool-allocation' AND p.name='wsqa-pool-primary'
  AND NOT EXISTS (SELECT 1 FROM org_pool_allocations ps WHERE ps.list_id=s.id);

INSERT INTO pool_organization_permissions (pool_id,organization_id,granted_by_user_id)
SELECT id,1,1 FROM customer_lists WHERE name='wsqa-pool-primary'
ON CONFLICT (pool_id,organization_id) DO NOTHING;

INSERT INTO pool_contacts (uuid,customer_code,company_name,email,name,attribs,status)
SELECT gen_random_uuid(), v.customer_code, v.company_name, v.email, v.name, v.attribs, v.status
FROM (VALUES
  ('DUP-001','QA Alpha Co','alpha-pool@example.test','Alpha Contact','{}'::jsonb,'active'),
  ('DUP-001','QA Beta Co','beta-pool@example.test','Beta Contact','{}'::jsonb,'active'),
  ('UNIQUE-001','QA Unique Co','unique-pool@example.test','Unique Contact','{}'::jsonb,'active')
) AS v(customer_code,company_name,email,name,attribs,status)
WHERE NOT EXISTS (SELECT 1 FROM pool_contacts WHERE customer_code IN ('DUP-001','UNIQUE-001') AND email=v.email);

INSERT INTO pool_members (pool_id,contact_id)
SELECT p.id,c.id FROM customer_lists p JOIN pool_contacts c ON c.email IN ('alpha-pool@example.test','beta-pool@example.test','unique-pool@example.test')
WHERE p.name='wsqa-pool-primary' ON CONFLICT DO NOTHING;

INSERT INTO org_pool_allocation_members (allocation_id,contact_id,status)
SELECT ps.id,c.id,'active' FROM org_pool_allocations ps JOIN customer_lists p ON p.id=ps.pool_id
JOIN pool_contacts c ON c.email IN ('alpha-pool@example.test','beta-pool@example.test','unique-pool@example.test')
WHERE p.name='wsqa-pool-primary' ON CONFLICT DO NOTHING;

INSERT INTO reply_mailboxes (user_id,organization_id,email,name,username,password,status,verified_at,is_default,ai_enabled)
SELECT 1,1,'pool-replies@example.test','Pool QA Replies','pool-qa','fixture-secret','active',NOW(),true,false
WHERE NOT EXISTS (SELECT 1 FROM reply_mailboxes WHERE user_id=1 AND organization_id=1 AND email='pool-replies@example.test');

UPDATE org_pool_allocations ps SET reply_mailbox_id=rm.id
FROM customer_lists s, reply_mailboxes rm
WHERE ps.list_id=s.id AND s.name='wsqa-org-pool-allocation' AND rm.email='pool-replies@example.test';

COMMIT;
