package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_23_0 completes the subscriber/list terminology migration while
// preserving all existing rows and relationships.
func V6_23_0(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	_ = fs
	_ = ko
	_ = lo
	_, err := db.Exec(`
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'subscriber_status') AND NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'customer_status') THEN ALTER TYPE subscriber_status RENAME TO customer_status; END IF;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'list_type') AND NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'customer_list_type') THEN ALTER TYPE list_type RENAME TO customer_list_type; END IF;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'list_optin') AND NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'customer_list_optin') THEN ALTER TYPE list_optin RENAME TO customer_list_optin; END IF;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'list_status') AND NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'customer_list_status') THEN ALTER TYPE list_status RENAME TO customer_list_status; END IF;
  IF EXISTS (SELECT 1 FROM pg_enum WHERE enumtypid = 'role_type'::regtype AND enumlabel = 'list') THEN ALTER TYPE role_type RENAME VALUE 'list' TO 'customer_list'; END IF;
END $$;

DO $$
BEGIN
  IF to_regclass('subscribers') IS NOT NULL AND to_regclass('customers') IS NULL THEN ALTER TABLE subscribers RENAME TO customers; END IF;
  IF to_regclass('subscriber_uuid_aliases') IS NOT NULL AND to_regclass('customer_uuid_aliases') IS NULL THEN ALTER TABLE subscriber_uuid_aliases RENAME TO customer_uuid_aliases; END IF;
  IF to_regclass('lists') IS NOT NULL AND to_regclass('customer_lists') IS NULL THEN ALTER TABLE lists RENAME TO customer_lists; END IF;
  IF to_regclass('subscriber_lists') IS NOT NULL AND to_regclass('customer_list_memberships') IS NULL THEN ALTER TABLE subscriber_lists RENAME TO customer_list_memberships; END IF;
  IF to_regclass('campaign_lists') IS NOT NULL AND to_regclass('campaign_customer_lists') IS NULL THEN ALTER TABLE campaign_lists RENAME TO campaign_customer_lists; END IF;
END $$;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['bounces','campaign_recipients','campaign_views','link_clicks','customer_list_memberships','customer_uuid_aliases'] LOOP
    IF to_regclass(t) IS NOT NULL AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=t AND column_name='subscriber_id') THEN EXECUTE format('ALTER TABLE %I RENAME COLUMN subscriber_id TO customer_id', t); END IF;
  END LOOP;
  IF to_regclass('customer_list_memberships') IS NOT NULL AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='customer_list_memberships' AND column_name='list_id') THEN ALTER TABLE customer_list_memberships RENAME COLUMN list_id TO customer_list_id; END IF;
  IF to_regclass('campaign_customer_lists') IS NOT NULL THEN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='campaign_customer_lists' AND column_name='list_id') THEN ALTER TABLE campaign_customer_lists RENAME COLUMN list_id TO customer_list_id; END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='campaign_customer_lists' AND column_name='list_name') THEN ALTER TABLE campaign_customer_lists RENAME COLUMN list_name TO customer_list_name; END IF;
  END IF;
  IF to_regclass('roles') IS NOT NULL AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='roles' AND column_name='list_id') THEN ALTER TABLE roles RENAME COLUMN list_id TO customer_list_id; END IF;
  IF to_regclass('campaigns') IS NOT NULL THEN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='campaigns' AND column_name='max_subscriber_id') THEN ALTER TABLE campaigns RENAME COLUMN max_subscriber_id TO max_customer_id; END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='campaigns' AND column_name='last_subscriber_id') THEN ALTER TABLE campaigns RENAME COLUMN last_subscriber_id TO last_customer_id; END IF;
  END IF;
END $$;

DO $$ BEGIN
  IF to_regclass('mat_list_subscriber_stats') IS NOT NULL AND to_regclass('mat_customer_list_customer_stats') IS NULL THEN ALTER MATERIALIZED VIEW mat_list_subscriber_stats RENAME TO mat_customer_list_customer_stats;
  ELSIF to_regclass('mat_list_customer_stats') IS NOT NULL AND to_regclass('mat_customer_list_customer_stats') IS NULL THEN ALTER MATERIALIZED VIEW mat_list_customer_stats RENAME TO mat_customer_list_customer_stats; END IF;
END $$;

CREATE MATERIALIZED VIEW IF NOT EXISTS mat_customer_list_customer_stats AS
  SELECT NOW() AS updated_at, customer_lists.id AS customer_list_id,
         customer_list_memberships.status, COUNT(customer_list_memberships.status) AS customer_count
  FROM customer_lists
  LEFT JOIN customer_list_memberships ON customer_list_memberships.customer_list_id = customer_lists.id
  GROUP BY customer_lists.id, customer_list_memberships.status
  UNION ALL
  SELECT NOW(), 0, NULL, COUNT(id) FROM customers;

-- Renaming a materialized view does not rename its output columns. These
-- columns are in pg_attribute, not information_schema.columns.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'mat_customer_list_customer_stats'::regclass AND attname = 'list_id' AND NOT attisdropped) THEN
    ALTER MATERIALIZED VIEW mat_customer_list_customer_stats RENAME COLUMN list_id TO customer_list_id;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'mat_customer_list_customer_stats'::regclass AND attname = 'subscriber_count' AND NOT attisdropped) THEN
    ALTER MATERIALIZED VIEW mat_customer_list_customer_stats RENAME COLUMN subscriber_count TO customer_count;
  END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS mat_customer_list_customer_stats_idx
  ON mat_customer_list_customer_stats (customer_list_id, status);

DO $$ BEGIN
  IF to_regclass('reply_ai_events') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='reply_ai_events_customer_id_fkey') THEN
    ALTER TABLE reply_ai_events ADD CONSTRAINT reply_ai_events_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE;
  END IF;
END $$;

UPDATE roles SET type = 'customer_list'::role_type WHERE type::text = 'list';
UPDATE roles SET permissions = ARRAY(SELECT CASE WHEN p='lists:get_all' THEN 'customer_lists:get_all' WHEN p='lists:manage_all' THEN 'customer_lists:manage_all' WHEN p='list:get' THEN 'customer_list:get' WHEN p='list:manage' THEN 'customer_list:manage' WHEN p LIKE 'subscribers:%' THEN replace(p,'subscribers:','customers:') ELSE p END FROM unnest(permissions) p);
UPDATE integration_tokens SET scopes = ARRAY(SELECT CASE WHEN p='lists:read' THEN 'customer_lists:read' WHEN p='lists:write' THEN 'customer_lists:write' WHEN p='subscribers:read' THEN 'customers:read' WHEN p='subscribers:write' THEN 'customers:write' WHEN p='subscribers:import' THEN 'customers:import' ELSE p END FROM unnest(scopes) p);
UPDATE settings SET key='customer.custom_fields' WHERE key='subscriber.custom_fields';
`)
	return err
}
