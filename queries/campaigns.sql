-- campaigns
-- name: create-campaign
-- This creates the campaign and inserts campaign_lists relationships.
WITH tpl AS (
    -- Select the template for the given template ID or use the default template.
    SELECT
        -- If the template is a visual template, then use it's HTML body as the campaign
        -- body and its block source as the campaign's block source,
        -- and don't set a template_id in the campaigns table, as it's essentially an
        -- HTML template body "import" during creation.
        (CASE WHEN type = 'campaign_visual' THEN NULL ELSE id END) AS id,
        (CASE WHEN type = 'campaign_visual' THEN body ELSE '' END) AS body,
        (CASE WHEN type = 'campaign_visual' THEN body_source ELSE NULL END) AS body_source,
        (CASE WHEN type = 'campaign_visual' THEN 'visual' ELSE 'richtext' END) AS content_type
    FROM templates
    WHERE
        CASE
            -- If a template ID is present, use it. If not, use the default template only if
            -- it's not a visual template.
            WHEN $16::INT IS NOT NULL THEN id = $16::INT
            ELSE $8 != 'visual' AND is_default = TRUE
        END
    LIMIT 1
),
camp AS (
    INSERT INTO campaigns (uuid, type, name, subject, from_email, body, altbody,
        content_type, daily_send_limit, daily_resume_time, send_at, headers, attribs, tags, messenger, template_id, to_send,
        max_subscriber_id, archive, archive_slug, archive_template_id, archive_meta, body_source, auto_track_links)
        SELECT $1, $2, $3, $4, $5,
            -- body
            COALESCE(NULLIF($6, ''), (SELECT body FROM tpl), ''),
            $7,
            $8::content_type,
            $9,
            $10,
            $11, $12, $13, $14, $15,
            (SELECT id FROM tpl),
            0,
            0,
            $18, $19,
            -- archive_template_id
            $20,
            $21,
            -- body_source
            COALESCE($23, (SELECT body_source FROM tpl)),
            $24
        RETURNING id
),
med AS (
    INSERT INTO campaign_media (campaign_id, media_id, filename)
        (SELECT (SELECT id FROM camp), id, filename FROM media WHERE id=ANY($22::INT[]))
),
insLists AS (
    INSERT INTO campaign_lists (campaign_id, list_id, list_name)
        SELECT (SELECT id FROM camp), id, name FROM lists WHERE id=ANY($17::INT[])
)
SELECT id FROM camp;

-- name: query-campaigns
-- Here, 'lists' is returned as an aggregated JSON array from campaign_lists because
-- the list reference may have been deleted.
-- While the results are sliced using offset+limit,
-- there's a COUNT() OVER() that still returns the total result count
-- for pagination in the frontend, albeit being a field that'll repeat
-- with every resultant row.
SELECT  c.*,
        CASE
            WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = c.id) THEN (
                SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = c.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
            )
            ELSE GREATEST(c.to_send - c.sent, 0)
        END AS unsent_count,
        COUNT(*) OVER () AS total,
        (
            SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
                SELECT COALESCE(campaign_lists.list_id, 0) AS id,
                campaign_lists.list_name AS name
                FROM campaign_lists WHERE campaign_lists.campaign_id = c.id
        ) l
    ) AS lists
FROM campaigns c
WHERE ($1 = 0 OR id = $1)
    AND (CARDINALITY($2::campaign_status[]) = 0 OR status = ANY($2))
    AND (CARDINALITY($3::VARCHAR(100)[]) = 0 OR $3 <@ tags)
    AND ($4 = '' OR TO_TSVECTOR(CONCAT(name, ' ', subject)) @@ TO_TSQUERY($4) OR CONCAT(c.name, ' ', c.subject) ILIKE $4)
    -- Get all campaigns or filter by list IDs.
    AND (
        $5 OR EXISTS (
            SELECT 1 FROM campaign_lists WHERE campaign_id = c.id AND list_id = ANY($6::INT[])
        )
    )
ORDER BY %order% OFFSET $7 LIMIT (CASE WHEN $8 < 1 THEN NULL ELSE $8 END);

-- name: get-campaign
SELECT campaigns.*,
    CASE
        WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
            SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = campaigns.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
        )
        ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
    END AS unsent_count,
    COALESCE(templates.body, (SELECT body FROM templates WHERE is_default = true LIMIT 1), '') AS template_body
    FROM campaigns
    LEFT JOIN templates ON (
        CASE WHEN $4 = 'default' THEN templates.id = campaigns.template_id
        ELSE templates.id = campaigns.archive_template_id END
    )
    WHERE CASE
            WHEN $1 > 0 THEN campaigns.id = $1
            WHEN $3 != '' THEN campaigns.archive_slug = $3
            ELSE uuid = $2
          END;

-- name: get-archived-campaigns
SELECT COUNT(*) OVER () AS total, campaigns.*,
    CASE
        WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
            SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = campaigns.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
        )
        ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
    END AS unsent_count,
    COALESCE(templates.body, (SELECT body FROM templates WHERE is_default = true LIMIT 1), '') AS template_body
    FROM campaigns
    LEFT JOIN templates ON (
        CASE WHEN $3 = 'default' THEN templates.id = campaigns.template_id
        ELSE templates.id = campaigns.archive_template_id END
    )
    WHERE campaigns.archive=true AND campaigns.type='regular' AND campaigns.status=ANY('{running, paused, deferred, finished}')
    ORDER by campaigns.created_at DESC OFFSET $1 LIMIT $2;

-- name: get-campaign-stats
-- This query is used to lazy load campaign stats (views, counts, list of lists) given a list of campaign IDs.
-- The query returns results in the same order as the given campaign IDs, and for non-existent campaign IDs,
-- the query still returns a row with 0 values. Thus, for lazy loading, the application simply iterate on the results in
-- the same order as the list of campaigns it would've queried and attach the results.
WITH lists AS (
    SELECT campaign_id, JSON_AGG(JSON_BUILD_OBJECT('id', list_id, 'name', list_name)) AS lists FROM campaign_lists
    WHERE campaign_id = ANY($1) GROUP BY campaign_id
),
media AS (
    SELECT campaign_id, JSON_AGG(JSON_BUILD_OBJECT('id', media_id, 'filename', filename)) AS media FROM campaign_media
    WHERE campaign_id = ANY($1) GROUP BY campaign_id
),
views AS (
    SELECT campaign_id, COUNT(campaign_id) as num FROM campaign_views
    WHERE campaign_id = ANY($1)
    GROUP BY campaign_id
),
clicks AS (
    SELECT campaign_id, COUNT(campaign_id) as num FROM link_clicks
    WHERE campaign_id = ANY($1)
    GROUP BY campaign_id
),
bounces AS (
    SELECT campaign_id, COUNT(campaign_id) as num FROM bounces
    WHERE campaign_id = ANY($1)
    GROUP BY campaign_id
)
SELECT id as campaign_id,
    COALESCE(v.num, 0) AS views,
    COALESCE(c.num, 0) AS clicks,
    COALESCE(b.num, 0) AS bounces,
    COALESCE(l.lists, '[]') AS lists,
    COALESCE(m.media, '[]') AS media
FROM (SELECT id FROM UNNEST($1) AS id) x
LEFT JOIN lists AS l ON (l.campaign_id = id)
LEFT JOIN media AS m ON (m.campaign_id = id)
LEFT JOIN views AS v ON (v.campaign_id = id)
LEFT JOIN clicks AS c ON (c.campaign_id = id)
LEFT JOIN bounces AS b ON (b.campaign_id = id)
ORDER BY ARRAY_POSITION($1, id);

-- name: get-campaign-for-preview
SELECT campaigns.*,
CASE
    WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
        SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = campaigns.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
    )
    ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
END AS unsent_count,
COALESCE(templates.body, '') AS template_body,
(
	SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
		SELECT COALESCE(campaign_lists.list_id, 0) AS id,
        campaign_lists.list_name AS name
        FROM campaign_lists WHERE campaign_lists.campaign_id = campaigns.id
	) l
) AS lists
FROM campaigns
LEFT JOIN templates ON (templates.id = (CASE WHEN $2=0 THEN campaigns.template_id ELSE $2 END))
WHERE campaigns.id = $1;

-- name: get-campaign-status
SELECT id, status, to_send, sent,
    CASE
        WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
            SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = campaigns.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
        )
        ELSE GREATEST(to_send - sent, 0)
    END AS unsent_count,
    next_resume_at,
    started_at,
    updated_at
FROM campaigns WHERE status=$1;

-- name: campaign-has-lists
-- Returns TRUE if the campaign $1 has any of the lists given in $2.
SELECT EXISTS (
    SELECT TRUE FROM campaign_lists WHERE campaign_id = $1 AND list_id = ANY($2::INT[])
);

-- name: next-campaigns
-- Retreives campaigns that are running (or scheduled and the time's up) and need
-- to be processed. It updates the to_send count and max_subscriber_id of the campaign,
-- that is, the total number of subscribers to be processed across all lists of a campaign.
-- Thus, it has a sideaffect.
-- In addition, it finds the max_subscriber_id, the upper limit across all lists of
-- a campaign. This is used to fetch and slice subscribers for the campaign in next-campaign-subscribers.
WITH camps AS (
    SELECT campaigns.*,
        CASE
            WHEN EXISTS (SELECT 1 FROM campaign_recipients crx WHERE crx.campaign_id = campaigns.id) THEN (
                SELECT COUNT(*) FROM campaign_recipients cr WHERE cr.campaign_id = campaigns.id AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
            )
            ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
        END AS unsent_count,
        COALESCE(templates.body, (SELECT body FROM templates WHERE is_default = true LIMIT 1), '') AS template_body
    FROM campaigns
    LEFT JOIN templates ON (templates.id = campaigns.template_id)
    WHERE (
        status='running'
        OR (status='scheduled' AND NOW() >= campaigns.send_at)
        OR (status='deferred' AND campaigns.next_resume_at IS NOT NULL AND NOW() >= campaigns.next_resume_at)
    )
    AND NOT(campaigns.id = ANY($1::INT[]))
),
campMedia AS (
    SELECT campaign_id, ARRAY_AGG(campaign_media.media_id)::INT[] AS media_id FROM campaign_media
    WHERE campaign_id = ANY(SELECT id FROM camps) AND media_id IS NOT NULL
    GROUP BY campaign_id
)
SELECT camps.*, campMedia.media_id FROM camps LEFT JOIN campMedia ON (campMedia.campaign_id = camps.id);

-- name: get-campaign-analytics-unique-counts
WITH intval AS (
    -- For intervals < a week, aggregate counts hourly, otherwise daily.
    SELECT CASE WHEN (EXTRACT (EPOCH FROM ($3::TIMESTAMP - $2::TIMESTAMP)) / 86400) >= 7 THEN 'day' ELSE 'hour' END
),
uniqIDs AS (
    SELECT DISTINCT ON(subscriber_id) subscriber_id, campaign_id, DATE_TRUNC((SELECT * FROM intval), created_at) AS "timestamp"
    FROM %s
    WHERE campaign_id=ANY($1) AND created_at >= $2 AND created_at <= $3
    ORDER BY subscriber_id, "timestamp"
)
SELECT COUNT(*) AS "count", campaign_id, "timestamp"
    FROM uniqIDs GROUP BY campaign_id, "timestamp" ORDER BY "timestamp" ASC;

-- name: get-campaign-analytics-counts
-- raw: true
WITH intval AS (
    -- For intervals < a week, aggregate counts hourly, otherwise daily.
    SELECT CASE WHEN (EXTRACT (EPOCH FROM ($3::TIMESTAMP - $2::TIMESTAMP)) / 86400) >= 7 THEN 'day' ELSE 'hour' END
)
SELECT campaign_id, COUNT(*) AS "count", DATE_TRUNC((SELECT * FROM intval), created_at) AS "timestamp"
    FROM %s
    WHERE campaign_id=ANY($1) AND created_at >= $2 AND created_at <= $3
    GROUP BY campaign_id, "timestamp" ORDER BY "timestamp" ASC;

-- name: get-campaign-bounce-counts
WITH intval AS (
    -- For intervals < a week, aggregate counts hourly, otherwise daily.
    SELECT CASE WHEN (EXTRACT (EPOCH FROM ($3::TIMESTAMP - $2::TIMESTAMP)) / 86400) >= 7 THEN 'day' ELSE 'hour' END
)
SELECT campaign_id, COUNT(*) AS "count", DATE_TRUNC((SELECT * FROM intval), created_at) AS "timestamp"
    FROM bounces
    WHERE campaign_id=ANY($1) AND created_at >= $2 AND created_at <= $3
    GROUP BY campaign_id, "timestamp" ORDER BY "timestamp" ASC;

-- name: get-campaign-link-counts
-- raw: true
-- %s = * or DISTINCT subscriber_id (prepared based on based on individual tracking=on/off). Prepared on boot.
SELECT links.id AS link_id, COUNT(%s) AS "count", url
    FROM link_clicks
    LEFT JOIN links ON (link_clicks.link_id = links.id)
    WHERE campaign_id=ANY($1) AND link_clicks.created_at >= $2 AND link_clicks.created_at <= $3
    GROUP BY links.id, links.url ORDER BY "count" DESC, links.url ASC LIMIT 50;

-- name: get-campaign-report-summary
WITH sent AS (
    SELECT COUNT(*) AS sent
    FROM campaign_recipients
    WHERE campaign_id = $1
      AND sent_at IS NOT NULL
      AND sent_at >= $2
      AND sent_at <= $3
),
views AS (
    SELECT
        COUNT(*) AS views_total,
        COUNT(DISTINCT subscriber_id) AS unique_viewers
    FROM campaign_views
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
),
clicks AS (
    SELECT
        COUNT(*) AS clicks_total,
        COUNT(DISTINCT subscriber_id) AS unique_clickers
    FROM link_clicks
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
),
bounces AS (
    SELECT COUNT(*) AS bounced
    FROM bounces
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
)
SELECT
    $1 AS campaign_id,
    COALESCE((SELECT sent FROM sent), 0) AS sent,
    COALESCE((SELECT bounced FROM bounces), 0) AS bounced,
    COALESCE((SELECT views_total FROM views), 0) AS views_total,
    COALESCE((SELECT clicks_total FROM clicks), 0) AS clicks_total,
    COALESCE((SELECT unique_viewers FROM views), 0) AS unique_viewers,
    COALESCE((SELECT unique_clickers FROM clicks), 0) AS unique_clickers;

-- name: get-campaign-report-links
SELECT
    links.id AS link_id,
    links.url,
    COUNT(*) AS total_clicks,
    COUNT(DISTINCT link_clicks.subscriber_id) AS unique_clickers
FROM link_clicks
LEFT JOIN links ON (link_clicks.link_id = links.id)
WHERE link_clicks.campaign_id = $1
  AND link_clicks.created_at >= $2
  AND link_clicks.created_at <= $3
GROUP BY links.id, links.url
ORDER BY total_clicks DESC, links.url ASC
LIMIT 50;

-- name: query-campaign-report-recipients
WITH view_stats AS (
    SELECT
        subscriber_id,
        COUNT(*) AS view_count,
        MIN(created_at) AS first_viewed_at,
        MAX(created_at) AS last_viewed_at
    FROM campaign_views
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
      AND subscriber_id IS NOT NULL
    GROUP BY subscriber_id
),
click_stats AS (
    SELECT
        subscriber_id,
        COUNT(*) AS click_count,
        MIN(created_at) AS first_clicked_at,
        MAX(created_at) AS last_clicked_at
    FROM link_clicks
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
      AND subscriber_id IS NOT NULL
    GROUP BY subscriber_id
),
last_click AS (
    SELECT DISTINCT ON (lc.subscriber_id)
        lc.subscriber_id,
        lc.link_id AS last_link_id,
        links.url AS last_link_url,
        lc.created_at AS last_clicked_at
    FROM link_clicks lc
    LEFT JOIN links ON links.id = lc.link_id
    WHERE lc.campaign_id = $1
      AND lc.created_at >= $2
      AND lc.created_at <= $3
      AND lc.subscriber_id IS NOT NULL
    ORDER BY lc.subscriber_id, lc.created_at DESC, lc.id DESC
),
bounce_stats AS (
    SELECT
        subscriber_id,
        COUNT(*) AS bounce_count,
        MAX(created_at) AS last_bounced_at
    FROM bounces
    WHERE campaign_id = $1
      AND created_at >= $2
      AND created_at <= $3
    GROUP BY subscriber_id
),
filtered AS (
    SELECT
        s.id AS subscriber_id,
        s.uuid,
        s.email,
        s.name,
        cr.status AS recipient_status,
        cr.sent_at,
        COALESCE(bs.bounce_count, 0) AS bounce_count,
        COALESCE(vs.view_count, 0) AS view_count,
        COALESCE(cs.click_count, 0) AS click_count,
        vs.first_viewed_at,
        vs.last_viewed_at,
        cs.first_clicked_at,
        cs.last_clicked_at,
        lc.last_link_id,
        lc.last_link_url,
        GREATEST(
            COALESCE(vs.last_viewed_at, '-infinity'::TIMESTAMP WITH TIME ZONE),
            COALESCE(cs.last_clicked_at, '-infinity'::TIMESTAMP WITH TIME ZONE),
            COALESCE(bs.last_bounced_at, '-infinity'::TIMESTAMP WITH TIME ZONE),
            COALESCE(cr.sent_at, '-infinity'::TIMESTAMP WITH TIME ZONE)
        ) AS last_engaged_at,
        COUNT(*) OVER() AS total
    FROM campaign_recipients cr
    JOIN subscribers s ON s.id = cr.subscriber_id
    LEFT JOIN view_stats vs ON vs.subscriber_id = cr.subscriber_id
    LEFT JOIN click_stats cs ON cs.subscriber_id = cr.subscriber_id
    LEFT JOIN last_click lc ON lc.subscriber_id = cr.subscriber_id
    LEFT JOIN bounce_stats bs ON bs.subscriber_id = cr.subscriber_id
    WHERE cr.campaign_id = $1
      AND ($4 = '' OR s.email ILIKE $4 OR s.name ILIKE $4)
      AND (
          $5 = 'all'
          OR ($5 = 'yes' AND COALESCE(vs.view_count, 0) > 0)
          OR ($5 = 'no' AND COALESCE(vs.view_count, 0) = 0)
      )
      AND (
          $6 = 'all'
          OR ($6 = 'yes' AND COALESCE(cs.click_count, 0) > 0)
          OR ($6 = 'no' AND COALESCE(cs.click_count, 0) = 0)
      )
      AND (
          $7 = 'all'
          OR ($7 = 'yes' AND COALESCE(bs.bounce_count, 0) > 0)
          OR ($7 = 'no' AND COALESCE(bs.bounce_count, 0) = 0)
      )
      AND (
          $8 = 0 OR EXISTS (
              SELECT 1
              FROM link_clicks lcf
              WHERE lcf.campaign_id = $1
                AND lcf.subscriber_id = cr.subscriber_id
                AND lcf.created_at >= $2
                AND lcf.created_at <= $3
                AND lcf.link_id = $8
          )
      )
)
SELECT
    subscriber_id,
    uuid,
    email,
    name,
    recipient_status,
    sent_at,
    bounce_count,
    view_count,
    click_count,
    first_viewed_at,
    last_viewed_at,
    first_clicked_at,
    last_clicked_at,
    last_link_id,
    last_link_url,
    NULLIF(last_engaged_at, '-infinity'::TIMESTAMP WITH TIME ZONE) AS last_engaged_at,
    total
FROM filtered
ORDER BY %order%, email ASC
OFFSET $9 LIMIT (CASE WHEN $10 < 1 THEN NULL ELSE $10 END);

-- name: get-campaign-send-state
SELECT campaigns.id AS campaign_id,
    campaigns.type AS campaign_type,
    campaigns.status,
    campaigns.messenger,
    campaigns.daily_send_limit,
    campaigns.daily_resume_time,
    campaigns.next_resume_at,
    COALESCE((
        SELECT sent_count FROM campaign_daily_usage
        WHERE campaign_id = campaigns.id AND usage_date = $2::DATE
    ), 0) AS daily_sent_count,
    COALESCE((
        SELECT COUNT(*) FROM campaign_recipients
        WHERE campaign_id = campaigns.id AND status = 'queued'
    ), 0) AS queued_count,
    COALESCE((
        SELECT COUNT(*) FROM campaign_recipients
        WHERE campaign_id = campaigns.id AND status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
    ), 0) AS unsent_count
FROM campaigns
WHERE campaigns.id = $1;

-- name: has-campaign-recipients
SELECT EXISTS (SELECT 1 FROM campaign_recipients WHERE campaign_id = $1);

-- name: ensure-campaign-recipients
WITH campLists AS (
    SELECT lists.id AS list_id, optin FROM lists
    LEFT JOIN campaign_lists ON campaign_lists.list_id = lists.id
    WHERE campaign_lists.campaign_id = $1
),
subs AS (
    SELECT DISTINCT s.id AS subscriber_id
    FROM subscriber_lists sl
    JOIN campLists ON sl.list_id = campLists.list_id
    JOIN subscribers s ON s.id = sl.subscriber_id
    JOIN campaigns c ON c.id = $1
    WHERE s.status != 'blocklisted'
    AND (
        (c.type = 'optin' AND sl.status = 'unconfirmed' AND campLists.optin = 'double')
        OR (
            c.type != 'optin' AND (
                (campLists.optin = 'double' AND sl.status = 'confirmed')
                OR (campLists.optin != 'double' AND sl.status != 'unsubscribed')
            )
        )
    )
)
INSERT INTO campaign_recipients (campaign_id, subscriber_id, status)
SELECT $1, subscriber_id, 'pending'::campaign_recipient_status FROM subs
ON CONFLICT (campaign_id, subscriber_id) DO NOTHING;

-- name: sync-campaign-progress
WITH counts AS (
    SELECT
        COUNT(*) AS total,
        COUNT(*) FILTER (WHERE status = 'sent') AS sent
    FROM campaign_recipients
    WHERE campaign_id = $1
)
UPDATE campaigns
SET to_send = COALESCE((SELECT total FROM counts), 0),
    sent = COALESCE((SELECT sent FROM counts), 0),
    started_at = CASE WHEN started_at IS NULL THEN NOW() ELSE started_at END,
    updated_at = NOW()
WHERE id = $1
RETURNING to_send, sent, started_at;

-- name: set-campaign-running
UPDATE campaigns
SET status = 'running',
    next_resume_at = NULL,
    started_at = CASE WHEN started_at IS NULL THEN NOW() ELSE started_at END,
    updated_at = NOW()
WHERE id = $1;

-- name: set-campaign-deferred
UPDATE campaigns
SET status = 'deferred',
    next_resume_at = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: queue-campaign-subscribers
WITH picked AS (
    SELECT subscriber_id
    FROM campaign_recipients
    WHERE campaign_id = $1
      AND status = ANY($2::campaign_recipient_status[])
    ORDER BY subscriber_id
    FOR UPDATE SKIP LOCKED
    LIMIT $3
),
u AS (
    UPDATE campaign_recipients cr
    SET status = 'queued',
        updated_at = NOW()
    FROM picked
    WHERE cr.campaign_id = $1
      AND cr.subscriber_id = picked.subscriber_id
    RETURNING cr.subscriber_id, cr.status AS recipient_status, cr.sent_at
)
SELECT s.*, u.recipient_status, u.sent_at
FROM u
JOIN subscribers s ON s.id = u.subscriber_id
ORDER BY s.id;

-- name: mark-campaign-recipient-sent
UPDATE campaign_recipients
SET status = 'sent',
    sent_at = NOW(),
    updated_at = NOW()
WHERE campaign_id = $1 AND subscriber_id = $2;

-- name: mark-campaign-recipient-status
UPDATE campaign_recipients
SET status = $3::campaign_recipient_status,
    updated_at = NOW()
WHERE campaign_id = $1 AND subscriber_id = $2;

-- name: reset-campaign-queued-recipients
UPDATE campaign_recipients
SET status = $2::campaign_recipient_status,
    updated_at = NOW()
WHERE campaign_id = $1 AND status = 'queued';

-- name: update-campaign-recipient-statuses
UPDATE campaign_recipients
SET status = $2::campaign_recipient_status,
    updated_at = NOW()
WHERE campaign_id = $1 AND status = ANY($3::campaign_recipient_status[]);

-- name: increment-campaign-daily-usage
INSERT INTO campaign_daily_usage (campaign_id, usage_date, sent_count, updated_at)
VALUES ($1, $2::DATE, 1, NOW())
ON CONFLICT (campaign_id, usage_date) DO UPDATE
SET sent_count = campaign_daily_usage.sent_count + 1,
    updated_at = NOW();

-- name: get-campaign-list-ids
SELECT COALESCE(list_id, 0) AS id FROM campaign_lists WHERE campaign_id = $1 ORDER BY id;

-- name: delete-campaign-views
DELETE FROM campaign_views WHERE created_at < $1;

-- name: delete-campaign-link-clicks
DELETE FROM link_clicks WHERE created_at < $1;

-- name: get-one-campaign-subscriber
SELECT * FROM subscribers
LEFT JOIN subscriber_lists ON (subscribers.id = subscriber_lists.subscriber_id AND subscriber_lists.status != 'unsubscribed')
WHERE subscriber_lists.list_id=ANY(
    SELECT list_id FROM campaign_lists where campaign_id=$1 AND list_id IS NOT NULL
)
ORDER BY RANDOM() LIMIT 1;

-- name: update-campaign
WITH camp AS (
    UPDATE campaigns SET
        name=$2,
        subject=$3,
        from_email=$4,
        body=$5,
        altbody=(CASE WHEN $6 = '' THEN NULL ELSE $6 END),
        content_type=$7::content_type,
        daily_send_limit=$8,
        daily_resume_time=$9,
        send_at=$10::TIMESTAMP WITH TIME ZONE,
        status=(
            CASE
                WHEN status = 'scheduled' AND $10 IS NULL THEN 'draft'
                ELSE status
            END
        ),
        headers=$11,
        attribs=$12,
        tags=$13::VARCHAR(100)[],
        messenger=$14,
        -- template_id shouldn't be saved for visual campaigns.
        template_id=(CASE WHEN $7::content_type = 'visual' THEN NULL ELSE $15::INT END),
        archive=$17,
        archive_slug=$18,
        archive_template_id=(CASE WHEN $7::content_type = 'visual' THEN NULL ELSE $19::INT END),
        archive_meta=$20,
        body_source=$22,
        auto_track_links=$23,
        updated_at=NOW()
    WHERE id = $1 RETURNING id
),
clists AS (
    -- Reset list relationships
    DELETE FROM campaign_lists WHERE campaign_id = $1 AND NOT(list_id = ANY($16))
),
med AS (
    DELETE FROM campaign_media WHERE campaign_id = $1
    AND ( media_id IS NULL or NOT(media_id = ANY($21))) RETURNING media_id
),
medi AS (
    INSERT INTO campaign_media (campaign_id, media_id, filename)
        (SELECT $1 AS campaign_id, id, filename FROM media WHERE id=ANY($21::INT[]))
        ON CONFLICT (campaign_id, media_id) DO NOTHING
)
INSERT INTO campaign_lists (campaign_id, list_id, list_name)
    (SELECT $1 as campaign_id, id, name FROM lists WHERE id=ANY($16::INT[]))
    ON CONFLICT (campaign_id, list_id) DO UPDATE SET list_name = EXCLUDED.list_name;

-- name: update-campaign-counts
UPDATE campaigns SET
    to_send=(CASE WHEN $2 != 0 THEN $2 ELSE to_send END),
    sent=sent+$3,
    last_subscriber_id=(CASE WHEN $4 > 0 THEN $4 ELSE last_subscriber_id END),
    updated_at=NOW()
WHERE id=$1;

-- name: update-campaign-status
UPDATE campaigns SET
    status=(
        CASE
            WHEN send_at IS NOT NULL AND $2 = 'running' THEN 'scheduled'
            ELSE $2::campaign_status
        END
    ),
    updated_at=NOW()
WHERE id = $1;

-- name: update-campaign-archive
UPDATE campaigns SET
    archive=$2,
    archive_slug=(CASE WHEN $3::TEXT = '' THEN NULL ELSE $3 END),
    archive_template_id=(CASE WHEN $4 > 0 THEN $4 ELSE archive_template_id END),
    archive_meta=(CASE WHEN $5::TEXT != '' THEN $5::JSONB ELSE archive_meta END),
    updated_at=NOW()
    WHERE id=$1;

-- name: delete-campaign
DELETE FROM campaigns WHERE id=$1;

-- name: delete-campaigns
DELETE FROM campaigns c
WHERE (
    CASE
        WHEN CARDINALITY($1::INT[]) > 0 THEN id = ANY($1)
        ELSE $2 = '' OR TO_TSVECTOR(CONCAT(name, ' ', subject)) @@ TO_TSQUERY($2) OR CONCAT(c.name, ' ', c.subject) ILIKE $2
    END
)
-- Get all campaigns or filter by permitted list IDs.
AND (
    $3 OR EXISTS (
        SELECT 1 FROM campaign_lists WHERE campaign_id = c.id AND list_id = ANY($4::INT[])
    )
);

-- name: register-campaign-view
WITH view AS (
    SELECT campaigns.id as campaign_id, subscribers.id AS subscriber_id FROM campaigns
    LEFT JOIN subscribers ON (CASE WHEN $2::TEXT != '' THEN subscribers.uuid = $2::UUID ELSE FALSE END)
    WHERE campaigns.uuid = $1
)
INSERT INTO campaign_views (campaign_id, subscriber_id)
    VALUES((SELECT campaign_id FROM view), (SELECT subscriber_id FROM view));
