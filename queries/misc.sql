-- name: get-dashboard-charts
SELECT data FROM mat_dashboard_charts;

-- name: get-dashboard-counts
SELECT data FROM mat_dashboard_counts;

-- name: get-settings
SELECT JSON_OBJECT_AGG(key, value) AS settings FROM (SELECT * FROM settings ORDER BY key) t;

-- name: update-settings
UPDATE settings AS s SET value = c.value
    -- For each key in the incoming JSON map, update the row with the key and its value.
    FROM(SELECT * FROM JSONB_EACH($1)) AS c(key, value) WHERE s.key = c.key;

-- name: update-settings-by-key
UPDATE settings SET value = $2, updated_at = NOW() WHERE key = $1;

-- name: get-smtp-daily-usage
SELECT COALESCE((
    SELECT sent_count FROM smtp_daily_usage WHERE smtp_uuid = $1 AND usage_date = $2::DATE
), 0) AS sent_count;

-- name: increment-smtp-daily-usage
INSERT INTO smtp_daily_usage (smtp_uuid, usage_date, sent_count, updated_at)
VALUES ($1, $2::DATE, 1, NOW())
ON CONFLICT (smtp_uuid, usage_date) DO UPDATE
SET sent_count = smtp_daily_usage.sent_count + 1,
    updated_at = NOW();

-- name: get-user-smtp-servers
SELECT s.*,
       FALSE AS is_primary,
       COALESCE(u.sent_count, 0) AS sent_today
FROM user_smtp_servers s
LEFT JOIN user_smtp_daily_usage u
  ON u.smtp_uuid = s.uuid AND u.usage_date = $2::DATE
WHERE s.user_id = $1
ORDER BY s.id;

-- name: get-enabled-user-smtp-servers
-- Campaign and transactional delivery must only use currently enabled
-- account-owned SMTP rows. The all-rows query above is intentionally kept for
-- the profile editor, which needs to show disabled rows as well.
SELECT s.*,
       FALSE AS is_primary,
       COALESCE(u.sent_count, 0) AS sent_today
FROM user_smtp_servers s
LEFT JOIN user_smtp_daily_usage u
  ON u.smtp_uuid = s.uuid AND u.usage_date = $2::DATE
WHERE s.user_id = $1 AND s.enabled = TRUE
ORDER BY s.id;

-- name: get-user-smtp-server
SELECT s.*,
       FALSE AS is_primary,
       COALESCE(u.sent_count, 0) AS sent_today
FROM user_smtp_servers s
LEFT JOIN user_smtp_daily_usage u
  ON u.smtp_uuid = s.uuid AND u.usage_date = $3::DATE
WHERE s.id = $1 AND s.user_id = $2;

-- name: create-user-smtp-server
INSERT INTO user_smtp_servers (
    uuid, user_id, name, enabled, from_email, daily_limit, host,
    hello_hostname, port, auth_protocol, username, password, email_headers,
    max_conns, max_msg_retries, idle_timeout, wait_timeout, tls_type,
    tls_skip_verify
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19
)
RETURNING id;

-- name: update-user-smtp-server
UPDATE user_smtp_servers SET
    name=$3,
    enabled=$4,
    from_email=$5,
    daily_limit=$6,
    host=$7,
    hello_hostname=$8,
    port=$9,
    auth_protocol=$10,
    username=$11,
    password=$12,
    email_headers=$13,
    max_conns=$14,
    max_msg_retries=$15,
    idle_timeout=$16,
    wait_timeout=$17,
    tls_type=$18,
    tls_skip_verify=$19,
    updated_at=NOW()
WHERE id=$1 AND user_id=$2;

-- name: delete-user-smtp-server
DELETE FROM user_smtp_servers WHERE id=$1 AND user_id=$2;

-- name: has-user-running-campaigns
-- The SMTP editor needs to warn when a configuration change affects any
-- campaign that can currently send.  Scheduled and deferred campaigns are
-- not running in a worker yet, but removing the last enabled SMTP converts
-- them to drafts and changing a pool changes their next delivery path.
SELECT EXISTS (
    SELECT 1 FROM campaigns
    WHERE owner_user_id = $1
      AND status = ANY('{running,scheduled,deferred}'::campaign_status[])
);

-- name: get-user-smtp-daily-usage
SELECT COALESCE((
    SELECT sent_count FROM user_smtp_daily_usage
    WHERE smtp_uuid = $1 AND usage_date = $2::DATE
), 0) AS sent_count;

-- name: increment-user-smtp-daily-usage
INSERT INTO user_smtp_daily_usage (smtp_uuid, usage_date, sent_count, updated_at)
VALUES ($1, $2::DATE, 1, NOW())
ON CONFLICT (smtp_uuid, usage_date) DO UPDATE
SET sent_count = user_smtp_daily_usage.sent_count + 1,
    updated_at = NOW();

-- name: get-user-smtp-remaining
-- Return the account's aggregate SMTP capacity for the local day. A value of
-- -1 means at least one enabled server is unlimited (daily_limit=0), while a
-- non-negative value is the sum of remaining slots across finite servers.
SELECT CASE
    -- No enabled rows is handled by the running-pipe SMTP availability
    -- guard. Do not turn that configuration error into a quota deferral.
    WHEN COUNT(*) = 0 THEN -1
    WHEN COUNT(*) FILTER (WHERE s.daily_limit = 0) > 0 THEN -1
    ELSE COALESCE(SUM(GREATEST(s.daily_limit - COALESCE(u.sent_count, 0), 0)), 0)
END AS remaining
FROM user_smtp_servers s
LEFT JOIN user_smtp_daily_usage u
  ON u.smtp_uuid = s.uuid AND u.usage_date = $2::DATE
WHERE s.user_id = $1 AND s.enabled = TRUE;

-- name: get-db-info
SELECT JSON_BUILD_OBJECT('version', (SELECT VERSION()),
                        'size_mb', (SELECT ROUND(pg_database_size((SELECT CURRENT_DATABASE()))/(1024^2)))) AS info;
