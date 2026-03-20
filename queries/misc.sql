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

-- name: get-db-info
SELECT JSON_BUILD_OBJECT('version', (SELECT VERSION()),
                        'size_mb', (SELECT ROUND(pg_database_size((SELECT CURRENT_DATABASE()))/(1024^2)))) AS info;
