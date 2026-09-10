DROP TYPE IF EXISTS customer_list_type CASCADE; CREATE TYPE customer_list_type AS ENUM ('public', 'private', 'temporary', 'pool', 'pool_segment');
DROP TYPE IF EXISTS customer_list_optin CASCADE; CREATE TYPE customer_list_optin AS ENUM ('single', 'double');
DROP TYPE IF EXISTS customer_list_status CASCADE; CREATE TYPE customer_list_status AS ENUM ('active', 'archived');
DROP TYPE IF EXISTS customer_status CASCADE; CREATE TYPE customer_status AS ENUM ('enabled', 'disabled', 'blocklisted');
DROP TYPE IF EXISTS subscription_status CASCADE; CREATE TYPE subscription_status AS ENUM ('unconfirmed', 'confirmed', 'unsubscribed');
DROP TYPE IF EXISTS campaign_status CASCADE; CREATE TYPE campaign_status AS ENUM ('draft', 'running', 'scheduled', 'paused', 'deferred', 'cancelled', 'finished');
DROP TYPE IF EXISTS campaign_type CASCADE; CREATE TYPE campaign_type AS ENUM ('regular', 'optin');
DROP TYPE IF EXISTS campaign_recipient_status CASCADE; CREATE TYPE campaign_recipient_status AS ENUM ('pending', 'queued', 'deferred', 'sent', 'cancelled');
DROP TYPE IF EXISTS content_type CASCADE; CREATE TYPE content_type AS ENUM ('richtext', 'html', 'plain', 'markdown', 'visual');
DROP TYPE IF EXISTS bounce_type CASCADE; CREATE TYPE bounce_type AS ENUM ('soft', 'hard', 'complaint');
DROP TYPE IF EXISTS template_type CASCADE; CREATE TYPE template_type AS ENUM ('campaign', 'campaign_visual', 'tx');
DROP TYPE IF EXISTS user_type CASCADE; CREATE TYPE user_type AS ENUM ('user', 'api');
DROP TYPE IF EXISTS user_status CASCADE; CREATE TYPE user_status AS ENUM ('enabled', 'disabled');
DROP TYPE IF EXISTS role_type CASCADE; CREATE TYPE role_type AS ENUM ('user', 'customer_list');
DROP TYPE IF EXISTS twofa_type CASCADE; CREATE TYPE twofa_type AS ENUM ('none', 'totp');

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Organization tenancy tables are declared after users below because they
-- reference user IDs. Drop them explicitly on a destructive fresh install.
DROP TABLE IF EXISTS data_export_chunks CASCADE;
DROP TABLE IF EXISTS data_export_jobs CASCADE;
DROP TABLE IF EXISTS reply_ai_events CASCADE;
DROP TABLE IF EXISTS reply_forward_messages CASCADE;
DROP TABLE IF EXISTS reply_forward_rules CASCADE;
DROP TABLE IF EXISTS reply_mailboxes CASCADE;
DROP TABLE IF EXISTS organization_invites CASCADE;
DROP TABLE IF EXISTS organization_join_requests CASCADE;
DROP TABLE IF EXISTS organization_members CASCADE;
DROP TABLE IF EXISTS organizations CASCADE;

-- customers
DROP TABLE IF EXISTS customer_uuid_aliases CASCADE;
DROP TABLE IF EXISTS customers CASCADE;
CREATE TABLE customers (
    id              SERIAL PRIMARY KEY,
    uuid uuid       NOT NULL UNIQUE,
    email           TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    attribs         JSONB NOT NULL DEFAULT '{}',
    status          customer_status NOT NULL DEFAULT 'enabled',
    customer_code   TEXT NOT NULL DEFAULT '',

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_subs_email; CREATE UNIQUE INDEX idx_subs_email ON customers(LOWER(email));
DROP INDEX IF EXISTS idx_subs_status; CREATE INDEX idx_subs_status ON customers(status);
DROP INDEX IF EXISTS idx_subs_customer_code; CREATE INDEX idx_subs_customer_code ON customers(customer_code);
DROP INDEX IF EXISTS idx_subs_id_status; CREATE INDEX idx_subs_id_status ON customers(id, status);
DROP INDEX IF EXISTS idx_subs_created_at; CREATE INDEX idx_subs_created_at ON customers(created_at);
DROP INDEX IF EXISTS idx_subs_updated_at; CREATE INDEX idx_subs_updated_at ON customers(updated_at);

-- A customer can be merged into another record when scoped e-mail
-- duplicates are reconciled. Preserve previous UUIDs so delivered campaign
-- URLs continue resolving to the retained customer.
CREATE TABLE customer_uuid_aliases (
    uuid          UUID PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_customer_uuid_aliases_customer_id ON customer_uuid_aliases(customer_id);

-- customer_lists
DROP TABLE IF EXISTS customer_lists CASCADE;
CREATE TABLE customer_lists (
    id              SERIAL PRIMARY KEY,
    uuid            uuid NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    type            customer_list_type NOT NULL,
    optin           customer_list_optin NOT NULL DEFAULT 'single',
    status          customer_list_status NOT NULL DEFAULT 'active',
    tags            VARCHAR(100)[],
    description     TEXT NOT NULL DEFAULT '',
    mask_emails     BOOLEAN NOT NULL DEFAULT false,
    pool_parent_id  INTEGER NULL REFERENCES customer_lists(id) ON DELETE CASCADE,

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_lists_type; CREATE INDEX idx_lists_type ON customer_lists(type);
DROP INDEX IF EXISTS idx_lists_optin; CREATE INDEX idx_lists_optin ON customer_lists(optin);
DROP INDEX IF EXISTS idx_lists_status; CREATE INDEX idx_lists_status ON customer_lists(status);
DROP INDEX IF EXISTS idx_lists_name; CREATE INDEX idx_lists_name ON customer_lists(name);
DROP INDEX IF EXISTS idx_lists_created_at; CREATE INDEX idx_lists_created_at ON customer_lists(created_at);
DROP INDEX IF EXISTS idx_lists_updated_at; CREATE INDEX idx_lists_updated_at ON customer_lists(updated_at);
DROP INDEX IF EXISTS idx_customer_lists_pool_parent; CREATE INDEX idx_customer_lists_pool_parent ON customer_lists(pool_parent_id);


DROP TABLE IF EXISTS customer_list_memberships CASCADE;
CREATE TABLE customer_list_memberships (
    customer_id      INTEGER REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
    customer_list_id            INTEGER NULL REFERENCES customer_lists(id) ON DELETE CASCADE ON UPDATE CASCADE,
    meta               JSONB NOT NULL DEFAULT '{}',
    status             subscription_status NOT NULL DEFAULT 'unconfirmed',

    created_at         TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at         TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    PRIMARY KEY(customer_id, customer_list_id)
);
DROP INDEX IF EXISTS idx_sub_lists_sub_id; CREATE INDEX idx_sub_lists_sub_id ON customer_list_memberships(customer_id);
DROP INDEX IF EXISTS idx_sub_lists_customer_list_id; CREATE INDEX idx_sub_lists_customer_list_id ON customer_list_memberships(customer_list_id);
DROP INDEX IF EXISTS idx_sub_lists_status; CREATE INDEX idx_sub_lists_status ON customer_list_memberships(status);

-- templates
DROP TABLE IF EXISTS templates CASCADE;
CREATE TABLE templates (
    name_fallback JSONB NOT NULL DEFAULT '{}'::jsonb,
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    type            template_type NOT NULL DEFAULT 'campaign',
    subject         TEXT NOT NULL,
    body            TEXT NOT NULL,
    body_source     TEXT NULL,
    is_default      BOOLEAN NOT NULL DEFAULT false,

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE UNIQUE INDEX ON templates (is_default) WHERE is_default = true;

-- campaigns
DROP TABLE IF EXISTS campaigns CASCADE;
CREATE TABLE campaigns (
    name_fallback JSONB NOT NULL DEFAULT '{}'::jsonb,
    id               SERIAL PRIMARY KEY,
    uuid uuid        NOT NULL UNIQUE,
    name             TEXT NOT NULL,
    subject          TEXT NOT NULL,
    from_email       TEXT NOT NULL,
    body             TEXT NOT NULL,
    body_source      TEXT NULL,
    altbody          TEXT NULL,
    content_type     content_type NOT NULL DEFAULT 'richtext',
    send_at          TIMESTAMP WITH TIME ZONE,
    headers          JSONB NOT NULL DEFAULT '[]',
    attribs          JSONB NOT NULL DEFAULT '{}',
    status           campaign_status NOT NULL DEFAULT 'draft',
    -- Regular e-mail campaigns are capped at 300 messages per local day by
    -- default. SMTP server quotas may impose a lower effective limit.
    daily_send_limit INT NOT NULL DEFAULT 300,
    daily_resume_time TEXT NOT NULL DEFAULT '09:00',
    next_resume_at   TIMESTAMP WITH TIME ZONE,
    tags             VARCHAR(100)[],

    -- The subscription statuses of customers to which a campaign will be sent.
    -- For opt-in campaigns, this will be 'unsubscribed'.
    type campaign_type DEFAULT 'regular',

    -- The ID of the messenger backend used to send this campaign.
    messenger        TEXT NOT NULL,
    template_id      INTEGER REFERENCES templates(id) ON DELETE SET NULL,

    -- Progress and stats.
    to_send            INT NOT NULL DEFAULT 0,
    sent               INT NOT NULL DEFAULT 0,
    max_customer_id  INT NOT NULL DEFAULT 0,
    last_customer_id INT NOT NULL DEFAULT 0,

    -- Publishing.
    archive             BOOLEAN NOT NULL DEFAULT false,
    archive_slug        TEXT NULL UNIQUE,
    archive_template_id INTEGER REFERENCES templates(id) ON DELETE SET NULL,
    archive_meta        JSONB NOT NULL DEFAULT '{}',
    auto_track_links    BOOLEAN NOT NULL DEFAULT false,
    tracking_links_mapped BOOLEAN NOT NULL DEFAULT true,

    started_at       TIMESTAMP WITH TIME ZONE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_camps_status; CREATE INDEX idx_camps_status ON campaigns(status);
DROP INDEX IF EXISTS idx_camps_name; CREATE INDEX idx_camps_name ON campaigns(name);
DROP INDEX IF EXISTS idx_camps_next_resume_at; CREATE INDEX idx_camps_next_resume_at ON campaigns(next_resume_at);
DROP INDEX IF EXISTS idx_camps_created_at; CREATE INDEX idx_camps_created_at ON campaigns(created_at);
DROP INDEX IF EXISTS idx_camps_updated_at; CREATE INDEX idx_camps_updated_at ON campaigns(updated_at);


DROP TABLE IF EXISTS campaign_customer_lists CASCADE;
CREATE TABLE campaign_customer_lists (
    id           BIGSERIAL PRIMARY KEY,
    campaign_id  INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,

    -- CustomerLists may be deleted, so customer_list_id is nullable
    -- and a copy of the original customer_list name is maintained here.
    customer_list_id      INTEGER NULL REFERENCES customer_lists(id) ON DELETE SET NULL ON UPDATE CASCADE,
    customer_list_name    TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX ON campaign_customer_lists (campaign_id, customer_list_id);
DROP INDEX IF EXISTS idx_camp_lists_camp_id; CREATE INDEX idx_camp_lists_camp_id ON campaign_customer_lists(campaign_id);
DROP INDEX IF EXISTS idx_camp_lists_customer_list_id; CREATE INDEX idx_camp_lists_customer_list_id ON campaign_customer_lists(customer_list_id);

DROP TABLE IF EXISTS campaign_recipients CASCADE;
CREATE TABLE campaign_recipients (
    campaign_id    INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
    customer_id  INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
    status         campaign_recipient_status NOT NULL DEFAULT 'pending',
    email_snapshot TEXT,
    name_snapshot  TEXT,
    attribs_snapshot JSONB,
    sent_at        TIMESTAMP WITH TIME ZONE,
    created_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (campaign_id, customer_id)
);
DROP INDEX IF EXISTS idx_camp_recipients_status; CREATE INDEX idx_camp_recipients_status ON campaign_recipients(campaign_id, status, customer_id);
DROP INDEX IF EXISTS idx_camp_recipients_sub_id; CREATE INDEX idx_camp_recipients_sub_id ON campaign_recipients(customer_id);

DROP TABLE IF EXISTS smtp_daily_usage CASCADE;
CREATE TABLE smtp_daily_usage (
    smtp_uuid    uuid NOT NULL,
    usage_date   DATE NOT NULL,
    sent_count   INT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (smtp_uuid, usage_date)
);

DROP TABLE IF EXISTS campaign_daily_usage CASCADE;
CREATE TABLE campaign_daily_usage (
    campaign_id  INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
    usage_date   DATE NOT NULL,
    sent_count   INT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (campaign_id, usage_date)
);

DROP TABLE IF EXISTS campaign_views CASCADE;
CREATE TABLE campaign_views (
    id               BIGSERIAL PRIMARY KEY,
    campaign_id      INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,

    -- Customers may be deleted, but the view counts should remain.
    customer_id    INTEGER NULL REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_views_camp_id; CREATE INDEX idx_views_camp_id ON campaign_views(campaign_id);
DROP INDEX IF EXISTS idx_views_customer_id; CREATE INDEX idx_views_customer_id ON campaign_views(customer_id);
DROP INDEX IF EXISTS idx_views_date; CREATE INDEX idx_views_date ON campaign_views(created_at);

-- media
DROP TABLE IF EXISTS media CASCADE;
CREATE TABLE media (
    id               SERIAL PRIMARY KEY,
    uuid uuid        NOT NULL UNIQUE,
    provider         TEXT NOT NULL DEFAULT '',
    filename         TEXT NOT NULL,
    content_type     TEXT NOT NULL DEFAULT 'application/octet-stream',
    thumb            TEXT NOT NULL,
    meta             JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_media_filename; CREATE INDEX idx_media_filename ON media(provider, filename);

-- campaign_media
DROP TABLE IF EXISTS campaign_media CASCADE;
CREATE TABLE campaign_media (
    campaign_id  INTEGER REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,

    -- Media items may be deleted, so media_id is nullable
    -- and a copy of the original name is maintained here.
    media_id     INTEGER NULL REFERENCES media(id) ON DELETE SET NULL ON UPDATE CASCADE,

    filename     TEXT NOT NULL DEFAULT ''
);
DROP INDEX IF EXISTS idx_camp_media_id; CREATE UNIQUE INDEX idx_camp_media_id ON campaign_media (campaign_id, media_id);
DROP INDEX IF EXISTS idx_camp_media_camp_id; CREATE INDEX idx_camp_media_camp_id ON campaign_media(campaign_id);

-- template_media
CREATE TABLE template_media (
    template_id  INTEGER REFERENCES templates(id) ON DELETE CASCADE ON UPDATE CASCADE,

    -- Media items may be deleted, so media_id is nullable
    -- and a copy of the original name is maintained here.
    media_id     INTEGER NULL REFERENCES media(id) ON DELETE SET NULL ON UPDATE CASCADE,

    filename     TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX idx_template_media_id ON template_media (template_id, media_id);
CREATE INDEX idx_template_media_template_id ON template_media(template_id);


-- links
DROP TABLE IF EXISTS links CASCADE;
CREATE TABLE links (
    id               SERIAL PRIMARY KEY,
    uuid uuid        NOT NULL UNIQUE,
    url              TEXT NOT NULL UNIQUE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

DROP TABLE IF EXISTS campaign_links CASCADE;
CREATE TABLE campaign_links (
    campaign_id      INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
    link_id          INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE ON UPDATE CASCADE,
    PRIMARY KEY (campaign_id, link_id)
);
DROP INDEX IF EXISTS idx_campaign_links_link_id; CREATE INDEX idx_campaign_links_link_id ON campaign_links(link_id);

DROP TABLE IF EXISTS link_clicks CASCADE;
CREATE TABLE link_clicks (
    id               BIGSERIAL PRIMARY KEY,
    campaign_id      INTEGER NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
    link_id          INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE ON UPDATE CASCADE,

    -- Customers may be deleted, but the link counts should remain.
    customer_id    INTEGER NULL REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_clicks_camp_id; CREATE INDEX idx_clicks_camp_id ON link_clicks(campaign_id);
DROP INDEX IF EXISTS idx_clicks_link_id; CREATE INDEX idx_clicks_link_id ON link_clicks(link_id);
DROP INDEX IF EXISTS idx_clicks_sub_id; CREATE INDEX idx_clicks_sub_id ON link_clicks(customer_id);
DROP INDEX IF EXISTS idx_clicks_date; CREATE INDEX idx_clicks_date ON link_clicks(created_at);

-- settings
DROP TABLE IF EXISTS settings CASCADE;
CREATE TABLE settings (
    key             TEXT NOT NULL UNIQUE,
    value           JSONB NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_settings_key; CREATE INDEX idx_settings_key ON settings(key);
INSERT INTO settings (key, value) VALUES
	('customer.custom_fields', '[]'),
    ('app.site_name', '"Mailing customer_list"'),
    ('app.root_url', '"http://localhost:9000"'),
    ('app.favicon_url', '""'),
    ('app.from_email', '"listmonk <noreply@listmonk.yoursite.com>"'),
    ('app.logo_url', '""'),
    ('app.concurrency', '10'),
    ('app.message_rate', '10'),
    ('app.batch_size', '1000'),
    ('app.max_send_errors', '1000'),
    ('app.message_sliding_window', 'false'),
    ('app.message_sliding_window_duration', '"1h"'),
    ('app.message_sliding_window_rate', '10000'),
    ('app.cache_slow_queries', 'false'),
    ('app.cache_slow_queries_interval', '"0 3 * * *"'),
    ('app.enable_public_archive', 'true'),
    ('app.enable_public_subscription_page', 'true'),
    ('app.enable_public_archive_rss_content', 'true'),
    ('app.send_optin_confirmation', 'true'),
    ('app.check_updates', 'true'),
    ('app.notify_emails', '[]'),
    ('app.lang', '"zh-CN"'),
    ('privacy.individual_tracking', 'false'),
    ('privacy.disable_tracking', 'false'),
    ('privacy.unsubscribe_header', 'true'),
    ('privacy.allow_blocklist', 'true'),
    ('privacy.allow_export', 'true'),
    ('privacy.allow_wipe', 'true'),
    ('privacy.allow_preferences', 'true'),
    ('privacy.exportable', '["profile", "subscriptions", "campaign_views", "link_clicks"]'),
    ('privacy.domain_blocklist', '[]'),
    ('privacy.domain_allowlist', '[]'),
    ('privacy.record_optin_ip', 'false'),
    ('security.captcha', '{"altcha": {"enabled": false, "complexity": 300000}, "hcaptcha": {"enabled": false, "key": "", "secret": ""}}'),
    ('security.oidc', '{"enabled": false, "provider_url": "", "provider_name": "", "client_id": "", "client_secret": "", "auto_create_users": false, "default_user_role_id": null, "default_list_role_id": null}'),
    ('security.cors_origins', '[]'),
    ('upload.provider', '"filesystem"'),
    ('upload.max_file_size', '5000'),
    ('upload.extensions', '["jpg","jpeg","png","gif","svg","*"]'),
    ('upload.filesystem.upload_path', '"uploads"'),
    ('upload.filesystem.upload_uri', '"/uploads"'),
    ('upload.s3.url', '"https://ap-south-1.s3.amazonaws.com"'),
    ('upload.s3.public_url', '""'),
    ('upload.s3.aws_access_key_id', '""'),
    ('upload.s3.aws_secret_access_key', '""'),
    ('upload.s3.aws_default_region', '"ap-south-1"'),
    ('upload.s3.bucket', '""'),
    ('upload.s3.bucket_domain', '""'),
    ('upload.s3.bucket_path', '"/"'),
    ('upload.s3.bucket_type', '"public"'),
    ('upload.s3.expiry', '"167h"'),
    ('smtp',
        '[{"enabled":true, "is_primary":true, "from_email":"listmonk <noreply@listmonk.yoursite.com>", "daily_limit":0, "host":"smtp.yoursite.com","port":465,"auth_protocol":"plain","username":"username","password":"password","hello_hostname":"","max_conns":10,"idle_timeout":"15s","wait_timeout":"5s","max_msg_retries":2,"tls_type":"TLS","tls_skip_verify":false,"email_headers":[]},
          {"enabled":false, "is_primary":false, "from_email":"listmonk <noreply@listmonk.yoursite.com>", "daily_limit":0, "host":"smtp.gmail.com","port":465,"auth_protocol":"login","username":"username@gmail.com","password":"password","hello_hostname":"","max_conns":10,"idle_timeout":"15s","wait_timeout":"5s","max_msg_retries":2,"tls_type":"TLS","tls_skip_verify":false,"email_headers":[]}]'),
    ('messengers', '[]'),
    ('bounce.enabled', 'false'),
    ('bounce.webhooks_enabled', 'false'),
    ('bounce.actions', '{"soft": {"count": 2, "action": "none"}, "hard": {"count": 1, "action": "blocklist"}, "complaint" : {"count": 1, "action": "blocklist"}}'),
    ('bounce.ses_enabled', 'false'),
    ('bounce.sendgrid_enabled', 'false'),
    ('bounce.sendgrid_key', '""'),
    ('bounce.postmark', '{"enabled": false, "username": "", "password": ""}'),
    ('bounce.forwardemail', '{"enabled": false, "key": ""}'),
    ('bounce.mailboxes',
        '[{"enabled":false, "type": "pop", "host":"pop.yoursite.com","port":995,"auth_protocol":"userpass","username":"username","password":"password","return_path": "bounce@listmonk.yoursite.com","scan_interval":"15m","tls_enabled":true,"tls_skip_verify":false}]'),
    ('reply_ai', '{"enabled": false, "base_url": "", "api_key": "", "model": "", "timeout": "15s", "min_confidence": 0.98}'),
    ('appearance.admin.custom_css', '""'),
    ('appearance.admin.custom_js', '""'),
    ('appearance.public.custom_css', '""'),
    ('appearance.public.custom_js', '""'),
    ('maintenance.db', '{"vacuum": false, "vacuum_cron_interval": "0 2 * * *"}');

-- bounces
DROP TABLE IF EXISTS bounces CASCADE;
CREATE TABLE bounces (
    id               SERIAL PRIMARY KEY,
    customer_id    INTEGER NULL REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
    pool_contact_id BIGINT NULL,
    source_pool_id INTEGER NULL,
    source_segment_id BIGINT NULL,
    source_organization_id BIGINT NULL,
    campaign_id      INTEGER NULL REFERENCES campaigns(id) ON DELETE SET NULL ON UPDATE CASCADE,
    type             bounce_type NOT NULL DEFAULT 'hard',
    source           TEXT NOT NULL DEFAULT '',
    meta             JSONB NOT NULL DEFAULT '{}',
    reply_ai_event_id BIGINT NULL UNIQUE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
DROP INDEX IF EXISTS idx_bounces_sub_id; CREATE INDEX idx_bounces_sub_id ON bounces(customer_id);
DROP INDEX IF EXISTS idx_bounces_camp_id; CREATE INDEX idx_bounces_camp_id ON bounces(campaign_id);
DROP INDEX IF EXISTS idx_bounces_source; CREATE INDEX idx_bounces_source ON bounces(source);
DROP INDEX IF EXISTS idx_bounces_date; CREATE INDEX idx_bounces_date ON bounces(created_at);

-- roles
DROP TABLE IF EXISTS roles CASCADE;
CREATE TABLE roles (
    id               SERIAL PRIMARY KEY,
    type             role_type NOT NULL DEFAULT 'user',
    parent_id        INTEGER NULL REFERENCES roles(id) ON DELETE CASCADE ON UPDATE CASCADE,
    customer_list_id          INTEGER NULL REFERENCES customer_lists(id) ON DELETE CASCADE ON UPDATE CASCADE,
    permissions      TEXT[] NOT NULL DEFAULT '{}',
    name             TEXT NULL,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_roles ON roles (parent_id, customer_list_id);
CREATE UNIQUE INDEX idx_roles_name ON roles (type, name) WHERE name IS NOT NULL;

-- users
DROP TABLE IF EXISTS users CASCADE;
CREATE TABLE users (
    id               SERIAL PRIMARY KEY,
    username         TEXT NOT NULL UNIQUE,
    password_login   BOOLEAN NOT NULL DEFAULT false,
    password         TEXT NULL,
    email            TEXT NOT NULL UNIQUE,
    name             TEXT NOT NULL,
    attribs          JSONB NOT NULL DEFAULT '{}',
    avatar           TEXT NULL,
    type             user_type NOT NULL DEFAULT 'user',
    user_role_id     INTEGER NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    list_role_id     INTEGER NULL REFERENCES roles(id) ON DELETE CASCADE,
    status           user_status NOT NULL DEFAULT 'disabled',
    twofa_type       twofa_type NOT NULL DEFAULT 'none',
    twofa_key        TEXT NULL,
    loggedin_at      TIMESTAMP WITH TIME ZONE NULL,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE integration_tokens (
    id               SERIAL PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
    kind             TEXT NOT NULL DEFAULT 'service' CHECK (kind IN ('service', 'personal')),
    workspace_organization_id BIGINT NULL,
    name             TEXT NOT NULL,
    token_hash       TEXT NOT NULL UNIQUE,
    scopes           TEXT[] NOT NULL DEFAULT '{}',
    expires_at       TIMESTAMP WITH TIME ZONE NULL,
    last_used_at     TIMESTAMP WITH TIME ZONE NULL,
    revoked_at       TIMESTAMP WITH TIME ZONE NULL,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CHECK (kind = 'service' OR expires_at IS NOT NULL)
);
CREATE INDEX idx_integration_tokens_user_id ON integration_tokens(user_id);
CREATE INDEX idx_integration_tokens_active ON integration_tokens(user_id, revoked_at);
CREATE INDEX idx_integration_tokens_personal_workspace
    ON integration_tokens(user_id, workspace_organization_id, expires_at)
    WHERE kind = 'personal' AND revoked_at IS NULL;

-- personal SMTP servers
-- These credentials permanently belong to the account that created them.
-- They are deliberately separate from settings.smtp, which is the platform
-- system SMTP used for password resets and other system notifications.
DROP TABLE IF EXISTS user_smtp_daily_usage CASCADE;
DROP TABLE IF EXISTS user_smtp_servers CASCADE;
CREATE TABLE user_smtp_servers (
    id               SERIAL PRIMARY KEY,
    uuid             UUID NOT NULL UNIQUE,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
    name             TEXT NOT NULL DEFAULT '',
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    from_email       TEXT NOT NULL DEFAULT '',
    daily_limit      INT NOT NULL DEFAULT 0 CHECK (daily_limit >= 0),
    host             TEXT NOT NULL DEFAULT '',
    hello_hostname   TEXT NOT NULL DEFAULT '',
    port             INT NOT NULL DEFAULT 465 CHECK (port > 0 AND port <= 65535),
    auth_protocol    TEXT NOT NULL DEFAULT 'plain' CHECK (auth_protocol IN ('plain', 'login', 'cram', 'none')),
    username         TEXT NOT NULL DEFAULT '',
    password         TEXT NOT NULL DEFAULT '',
    email_headers    JSONB NOT NULL DEFAULT '[]',
    max_conns        INT NOT NULL DEFAULT 10 CHECK (max_conns > 0),
    max_msg_retries  INT NOT NULL DEFAULT 2 CHECK (max_msg_retries > 0),
    idle_timeout     TEXT NOT NULL DEFAULT '15s',
    wait_timeout     TEXT NOT NULL DEFAULT '5s',
    tls_type         TEXT NOT NULL DEFAULT 'TLS' CHECK (tls_type IN ('none', 'TLS', 'STARTTLS')),
    tls_skip_verify  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX idx_user_smtp_servers_user_enabled ON user_smtp_servers(user_id, enabled);
CREATE UNIQUE INDEX idx_user_smtp_servers_user_name ON user_smtp_servers(user_id, LOWER(name)) WHERE name <> '';

CREATE TABLE user_smtp_daily_usage (
    smtp_uuid    UUID NOT NULL REFERENCES user_smtp_servers(uuid) ON DELETE CASCADE ON UPDATE CASCADE,
    usage_date   DATE NOT NULL,
    sent_count   INT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (smtp_uuid, usage_date)
);
CREATE INDEX idx_user_smtp_daily_usage_date ON user_smtp_daily_usage(usage_date);

-- organizations and membership
CREATE TABLE organizations (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    archived_at        TIMESTAMP WITH TIME ZONE,
    created_at         TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at         TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_organizations_name_lower ON organizations (LOWER(name));
CREATE INDEX idx_organizations_status ON organizations(status);

CREATE TABLE organization_members (
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role               TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'manager')),
    joined_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    removed_at         TIMESTAMP WITH TIME ZONE,
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX idx_organization_members_user_active ON organization_members(user_id, organization_id) WHERE removed_at IS NULL;
CREATE INDEX idx_organization_members_org_active ON organization_members(organization_id, role) WHERE removed_at IS NULL;

CREATE TABLE organization_join_requests (
    id                   BIGSERIAL PRIMARY KEY,
    requested_name       TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn')),
    requested_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reviewed_by_user_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at          TIMESTAMP WITH TIME ZONE,
    review_note          TEXT NOT NULL DEFAULT '',
    organization_id      BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    created_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_organization_join_requests_pending_name ON organization_join_requests(LOWER(requested_name)) WHERE status = 'pending';
CREATE INDEX idx_organization_join_requests_status ON organization_join_requests(status, created_at DESC);

CREATE TABLE organization_invites (
    id                 BIGSERIAL PRIMARY KEY,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name               TEXT NOT NULL DEFAULT '',
    code_hash          TEXT NOT NULL UNIQUE,
    created_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expires_at         TIMESTAMP WITH TIME ZONE,
    revoked_at         TIMESTAMP WITH TIME ZONE,
    max_uses           INTEGER,
    use_count          INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CHECK (max_uses IS NULL OR max_uses > 0)
);
CREATE INDEX idx_organization_invites_org_active ON organization_invites(organization_id, created_at DESC) WHERE revoked_at IS NULL;

-- Dedicated 263 customer-reply mailboxes. They are receive-only credentials
-- and intentionally stay separate from account SMTP servers used to send
-- campaigns. A mailbox can be default in each of a user's workspaces.
CREATE TABLE reply_mailboxes (
    id               SERIAL PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
    organization_id  INTEGER NULL REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
    email            TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    username         TEXT NOT NULL DEFAULT '',
    imap_host        TEXT NOT NULL DEFAULT 'imap.263.net',
    imap_port        INTEGER NOT NULL DEFAULT 993 CHECK (imap_port > 0 AND imap_port <= 65535),
    imap_tls         BOOLEAN NOT NULL DEFAULT TRUE,
    folder           TEXT NOT NULL DEFAULT 'INBOX',
    password         TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','retained','disabled')),
    verified_at      TIMESTAMP WITH TIME ZONE NULL,
    is_default       BOOLEAN NOT NULL DEFAULT FALSE,
    ai_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    last_sync_at     TIMESTAMP WITH TIME ZONE NULL,
    last_sync_error  TEXT NOT NULL DEFAULT '',
    forward_count    INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_reply_mailboxes_user_email
    ON reply_mailboxes(user_id, COALESCE(organization_id, 0), LOWER(email));
CREATE INDEX idx_reply_mailboxes_user_status
    ON reply_mailboxes(user_id, status);
CREATE UNIQUE INDEX idx_reply_mailboxes_user_default
    ON reply_mailboxes(user_id, COALESCE(organization_id, 0))
    WHERE is_default = TRUE AND status IN ('pending','active','retained');

ALTER TABLE campaigns
    ADD COLUMN reply_mailbox_id INTEGER NULL REFERENCES reply_mailboxes(id) ON DELETE SET NULL;

CREATE TABLE reply_forward_rules (
    id                SERIAL PRIMARY KEY,
    reply_mailbox_id  INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
    organization_id   INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE ON UPDATE CASCADE,
    target_user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT ON UPDATE CASCADE,
    target_email      TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    disabled_at       TIMESTAMP WITH TIME ZONE NULL,
    disabled_by       INTEGER NULL REFERENCES users(id) ON DELETE SET NULL ON UPDATE CASCADE,
    last_error        TEXT NOT NULL DEFAULT '',
    last_forward_at   TIMESTAMP WITH TIME ZONE NULL,
    created_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (reply_mailbox_id, organization_id)
);
CREATE INDEX idx_reply_forward_rules_active ON reply_forward_rules(status, reply_mailbox_id);

CREATE TABLE reply_forward_messages (
    id                BIGSERIAL PRIMARY KEY,
    rule_id           INTEGER NOT NULL REFERENCES reply_forward_rules(id) ON DELETE CASCADE ON UPDATE CASCADE,
    message_key       TEXT NOT NULL,
    imap_uid          TEXT NOT NULL DEFAULT '',
    from_email        TEXT NOT NULL DEFAULT '',
    subject           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','forwarded','failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT NOT NULL DEFAULT '',
    received_at       TIMESTAMP WITH TIME ZONE NULL,
    forwarded_at      TIMESTAMP WITH TIME ZONE NULL,
    created_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (rule_id, message_key)
);
CREATE INDEX idx_reply_forward_messages_pending ON reply_forward_messages(status, created_at);

-- AI classifications are an idempotent, leased work queue. The normalized
-- message body is cleared once the item reaches a terminal state; the hash and
-- bounded decision fields remain for customer-level audit.
CREATE TABLE reply_ai_events (
    id                BIGSERIAL PRIMARY KEY,
    reply_mailbox_id  INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
    customer_id       INTEGER NULL REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE,
    pool_contact_id   BIGINT NULL,
    pool_id           INTEGER NULL,
    source_segment_id BIGINT NULL,
    source_organization_id BIGINT NULL,
    message_key       TEXT NOT NULL,
    from_email        TEXT NOT NULL DEFAULT '',
    subject           TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL DEFAULT '',
    body_hash         TEXT NOT NULL DEFAULT '',
    intent            TEXT NOT NULL DEFAULT 'other' CHECK (intent IN ('unsubscribe','complaint','other')),
    confidence        DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason_code       TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    action            TEXT NOT NULL DEFAULT 'pending' CHECK (action IN ('pending','ignored','blocklisted')),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','processed','ignored','failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT NOT NULL DEFAULT '',
    received_at       TIMESTAMP WITH TIME ZONE NULL,
    classified_at     TIMESTAMP WITH TIME ZONE NULL,
    actioned_at       TIMESTAMP WITH TIME ZONE NULL,
    next_attempt_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    lease_expires_at  TIMESTAMP WITH TIME ZONE NULL,
    lease_token       UUID NULL,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (reply_mailbox_id, message_key)
);
CREATE INDEX idx_reply_ai_events_claim ON reply_ai_events(status, next_attempt_at, created_at);
CREATE INDEX idx_reply_ai_events_customer ON reply_ai_events(customer_id, created_at DESC);
ALTER TABLE bounces
    ADD CONSTRAINT bounces_reply_ai_event_id_fkey
    FOREIGN KEY (reply_ai_event_id) REFERENCES reply_ai_events(id) ON DELETE SET NULL ON UPDATE CASCADE;

-- All user-owned resources receive an explicit tenancy and ownership scope.
-- organization_id is NULL for a personal workspace.
ALTER TABLE customer_lists
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global')),
    ADD COLUMN transfer_pending_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE customers
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global')),
    ADD COLUMN transfer_pending_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE templates
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global')),
    ADD COLUMN transfer_pending_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE campaigns
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global')),
    ADD COLUMN transfer_pending_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE media
    ADD COLUMN organization_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN original_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'global')),
    ADD COLUMN transfer_pending_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX idx_lists_workspace_owner ON customer_lists(organization_id, owner_user_id);
CREATE INDEX idx_customers_workspace_owner ON customers(organization_id, owner_user_id);
CREATE INDEX idx_templates_workspace_owner_visibility ON templates(organization_id, owner_user_id, visibility);
CREATE INDEX idx_campaigns_workspace_owner_visibility ON campaigns(organization_id, owner_user_id, visibility);
CREATE INDEX idx_media_workspace_owner ON media(organization_id, owner_user_id);

ALTER TABLE customers DROP CONSTRAINT customers_email_key;
DROP INDEX idx_subs_email;
CREATE UNIQUE INDEX idx_customers_scope_owner_email
    ON customers ((COALESCE(organization_id, 0)), owner_user_id, LOWER(email))
    WHERE owner_user_id IS NOT NULL;

-- Defaults are local to an owner in a personal or organization workspace.
DROP INDEX IF EXISTS templates_is_default_idx;
CREATE UNIQUE INDEX idx_templates_workspace_default
    ON templates ((COALESCE(organization_id, 0)), owner_user_id)
    WHERE is_default AND owner_user_id IS NOT NULL;

-- user sessions
DROP TABLE IF EXISTS sessions CASCADE;
CREATE TABLE sessions (
    id TEXT NOT NULL PRIMARY KEY,
    data JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT now() NOT NULL
);
DROP INDEX IF EXISTS idx_sessions; CREATE INDEX idx_sessions ON sessions (id, created_at);

-- materialized views

-- dashboard stats
DROP MATERIALIZED VIEW IF EXISTS mat_dashboard_counts;
CREATE MATERIALIZED VIEW mat_dashboard_counts AS
    WITH subs AS (
        SELECT COUNT(*) AS num, status FROM customers GROUP BY status
    )
    SELECT NOW() AS updated_at,
        JSON_BUILD_OBJECT(
            'customers', JSON_BUILD_OBJECT(
                'total', (SELECT SUM(num) FROM subs),
                'blocklisted', (SELECT num FROM subs WHERE status='blocklisted'),
                'orphans', (
                    SELECT COUNT(id) FROM customers
                    LEFT JOIN customer_list_memberships ON (customers.id = customer_list_memberships.customer_id)
                    WHERE customer_list_memberships.customer_id IS NULL
                )
            ),
            'customerLists', JSON_BUILD_OBJECT(
                'total', (SELECT COUNT(*) FROM customer_lists),
                'private', (SELECT COUNT(*) FROM customer_lists WHERE type='private'),
                'public', (SELECT COUNT(*) FROM customer_lists WHERE type='public'),
                'optin_single', (SELECT COUNT(*) FROM customer_lists WHERE optin='single'),
                'optin_double', (SELECT COUNT(*) FROM customer_lists WHERE optin='double')
            ),
            'campaigns', JSON_BUILD_OBJECT(
                'total', (SELECT COUNT(*) FROM campaigns),
                'by_status', (
                    SELECT JSON_OBJECT_AGG (status, num) FROM
                    (SELECT status, COUNT(*) AS num FROM campaigns GROUP BY status) r
                )
            ),
            'messages', (SELECT SUM(sent) AS messages FROM campaigns)
        ) AS data;
DROP INDEX IF EXISTS mat_dashboard_stats_idx; CREATE UNIQUE INDEX mat_dashboard_stats_idx ON mat_dashboard_counts (updated_at);


DROP MATERIALIZED VIEW IF EXISTS mat_dashboard_charts;
CREATE MATERIALIZED VIEW mat_dashboard_charts AS
    WITH clicks AS (
        SELECT JSON_AGG(ROW_TO_JSON(row))
        FROM (
            WITH viewDates AS (
              SELECT created_at::DATE AS to_date,
                     created_at::DATE - INTERVAL '30 DAY' AS from_date
                     FROM link_clicks ORDER BY id DESC LIMIT 1
            )
            SELECT COUNT(*) AS count, created_at::DATE as date FROM link_clicks
              WHERE created_at >= (SELECT from_date FROM viewDates)
                AND created_at < (SELECT to_date FROM viewDates) + INTERVAL '1 day'
              GROUP by date ORDER BY date
        ) row
    ),
    views AS (
        SELECT JSON_AGG(ROW_TO_JSON(row))
        FROM (
            WITH viewDates AS (
              SELECT created_at::DATE AS to_date,
                     created_at::DATE - INTERVAL '30 DAY' AS from_date
                     FROM campaign_views ORDER BY id DESC LIMIT 1
            )
            SELECT COUNT(*) AS count, created_at::DATE as date FROM campaign_views
              WHERE created_at >= (SELECT from_date FROM viewDates)
                AND created_at < (SELECT to_date FROM viewDates) + INTERVAL '1 day'
              GROUP by date ORDER BY date
        ) row
    )
    SELECT NOW() AS updated_at, JSON_BUILD_OBJECT('link_clicks', COALESCE((SELECT * FROM clicks), '[]'),
                                  'campaign_views', COALESCE((SELECT * FROM views), '[]')
                                ) AS data;
DROP INDEX IF EXISTS mat_dashboard_charts_idx; CREATE UNIQUE INDEX mat_dashboard_charts_idx ON mat_dashboard_charts (updated_at);

-- customer counts stats for customer_lists
DROP MATERIALIZED VIEW IF EXISTS mat_customer_list_customer_stats;
CREATE MATERIALIZED VIEW mat_customer_list_customer_stats AS
    SELECT NOW() AS updated_at, customer_lists.id AS customer_list_id, customer_list_memberships.status, COUNT(customer_list_memberships.status) AS customer_count FROM customer_lists
    LEFT JOIN customer_list_memberships ON (customer_list_memberships.customer_list_id = customer_lists.id)
    GROUP BY customer_lists.id, customer_list_memberships.status
    UNION ALL
    SELECT NOW() AS updated_at, 0 AS customer_list_id, NULL AS status, COUNT(id) AS customer_count FROM customers;
DROP INDEX IF EXISTS mat_customer_list_customer_stats_idx; CREATE UNIQUE INDEX mat_customer_list_customer_stats_idx ON mat_customer_list_customer_stats (customer_list_id, status);

-- First-class public customer pools and organization segments.
ALTER TABLE customer_lists ADD COLUMN IF NOT EXISTS pool_reply_mailbox_id INTEGER NULL REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
DROP INDEX IF EXISTS idx_customer_lists_pool_reply_mailbox; CREATE INDEX idx_customer_lists_pool_reply_mailbox ON customer_lists(pool_reply_mailbox_id);

DROP TABLE IF EXISTS campaign_pool_recipients CASCADE;
DROP TABLE IF EXISTS pool_merge_conflicts CASCADE;
DROP TABLE IF EXISTS pool_segment_exclusions CASCADE;
DROP TABLE IF EXISTS pool_organization_permissions CASCADE;
DROP TABLE IF EXISTS pool_segment_members CASCADE;
DROP TABLE IF EXISTS pool_segments CASCADE;
DROP TABLE IF EXISTS pool_members CASCADE;
DROP TABLE IF EXISTS pool_contacts CASCADE;

CREATE TABLE pool_contacts (
    id BIGSERIAL PRIMARY KEY,
    uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    customer_code TEXT NOT NULL DEFAULT '',
    company_name TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    attribs JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pool_contacts_code ON pool_contacts(customer_code);
CREATE INDEX idx_pool_contacts_email ON pool_contacts(LOWER(email));

CREATE TABLE pool_members (
    pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, contact_id)
);

CREATE TABLE pool_segments (
    id BIGSERIAL PRIMARY KEY,
    list_id INTEGER NOT NULL UNIQUE REFERENCES customer_lists(id) ON DELETE CASCADE,
    pool_id INTEGER NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_pool_segments_pool_org ON pool_segments(pool_id, organization_id);

CREATE TABLE pool_segment_members (
    segment_id BIGINT NOT NULL REFERENCES pool_segments(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','removed')),
    removed_reason TEXT NOT NULL DEFAULT '',
    removed_at TIMESTAMPTZ,
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (segment_id, contact_id)
);

CREATE TABLE pool_organization_permissions (
    pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    granted_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, organization_id)
);

CREATE TABLE pool_segment_exclusions (
    pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT 'manual',
    source TEXT NOT NULL DEFAULT 'segment',
    removed_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    removed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    restored_at TIMESTAMPTZ,
    PRIMARY KEY (pool_id, organization_id, contact_id)
);

CREATE TABLE pool_merge_conflicts (
    id BIGSERIAL PRIMARY KEY,
    pool_id INTEGER REFERENCES customer_lists(id) ON DELETE CASCADE,
    contact_id BIGINT REFERENCES pool_contacts(id) ON DELETE SET NULL,
    customer_code TEXT NOT NULL,
    existing_snapshot JSONB NOT NULL DEFAULT '{}',
    incoming_snapshot JSONB NOT NULL DEFAULT '{}',
    created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS pool_id INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL;
ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS pool_segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL;
ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS source_organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE campaign_customer_lists ADD COLUMN IF NOT EXISTS resolved_reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_campaign_customer_lists_pool ON campaign_customer_lists(pool_id, pool_segment_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_campaign_customer_lists_pool_unique ON campaign_customer_lists(campaign_id, pool_id, COALESCE(pool_segment_id, 0)) WHERE pool_id IS NOT NULL;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT REFERENCES pool_contacts(id) ON DELETE SET NULL;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_pool_id INTEGER REFERENCES customer_lists(id) ON DELETE SET NULL;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS source_organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL;
CREATE TABLE campaign_pool_recipients (
    campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    pool_contact_id BIGINT NOT NULL REFERENCES pool_contacts(id) ON DELETE CASCADE,
    pool_id INTEGER NOT NULL REFERENCES customer_lists(id) ON DELETE CASCADE,
    segment_id BIGINT REFERENCES pool_segments(id) ON DELETE SET NULL,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    reply_mailbox_id INTEGER REFERENCES reply_mailboxes(id) ON DELETE SET NULL,
    status campaign_recipient_status NOT NULL DEFAULT 'pending',
    email_snapshot TEXT NOT NULL,
    name_snapshot TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (campaign_id, pool_contact_id)
);

ALTER TABLE bounces ADD COLUMN IF NOT EXISTS pool_contact_id BIGINT;
ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_pool_id INTEGER;
ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_segment_id BIGINT;
ALTER TABLE bounces ADD COLUMN IF NOT EXISTS source_organization_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_bounces_pool_contact ON bounces(pool_contact_id);

CREATE TABLE IF NOT EXISTS data_export_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 organization_id BIGINT NOT NULL DEFAULT 0,
 request JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','complete','failed','expired')),
 filename TEXT NOT NULL,
 row_count BIGINT NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '',
 access_stamp TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 started_at TIMESTAMPTZ,
 completed_at TIMESTAMPTZ,
 expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days',
 download_count INTEGER NOT NULL DEFAULT 0,
 last_downloaded_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_data_export_jobs_owner ON data_export_jobs(user_id, organization_id, created_at DESC);
CREATE TABLE IF NOT EXISTS data_export_chunks (
 job_id UUID NOT NULL REFERENCES data_export_jobs(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL,
 content BYTEA NOT NULL,
 PRIMARY KEY(job_id, sequence)
);
