-- campaigns
-- name: create-campaign
-- This creates the campaign and inserts campaign_lists relationships.
WITH requested_lists AS (
    SELECT DISTINCT list_id FROM UNNEST($17::INT[]) AS requested(list_id)
),
scoped_lists AS (
    SELECT l.id, l.name
    FROM lists l
    JOIN requested_lists requested ON requested.list_id = l.id
    WHERE l.organization_id IS NOT DISTINCT FROM $25::BIGINT
        AND l.owner_user_id = $26
        AND l.transfer_pending_at IS NULL
),
valid_lists AS (
    -- Draft campaigns may intentionally start without a sending list. Reject
    -- only a non-empty request that contains a list outside the campaign
    -- owner's current workspace.
    SELECT COUNT(*) = (SELECT COUNT(*) FROM requested_lists) AS valid
    FROM scoped_lists
),
tpl AS (
    -- Select the template for the given template ID or use the default template.
    SELECT
        -- If the template is a visual template, then use it's HTML body as the campaign
        -- body and its block source as the campaign's block source,
        -- and don't set a template_id in the campaigns table, as it's essentially an
        -- HTML template body "import" during creation.
        id AS source_id,
        (CASE WHEN type = 'campaign_visual' THEN NULL ELSE id END) AS id,
        (CASE WHEN type = 'campaign_visual' THEN body ELSE '' END) AS body,
        (CASE WHEN type = 'campaign_visual' THEN body_source ELSE NULL END) AS body_source,
        (CASE WHEN type = 'campaign_visual' THEN 'visual' ELSE 'richtext' END) AS content_type
    FROM templates t
    WHERE
        CASE
            -- If a template ID is present, use it. If not, use the default template only if
            -- it's not a visual template.
            WHEN $16::INT IS NOT NULL THEN t.id = $16::INT
                AND t.transfer_pending_at IS NULL
                AND (t.organization_id IS NULL OR EXISTS (
                    SELECT 1 FROM organizations template_organization
                    WHERE template_organization.id = t.organization_id
                        AND template_organization.status = 'active'
                ))
                AND (
                    t.visibility = 'global'
                    OR (
                        t.organization_id IS NOT DISTINCT FROM $25::BIGINT
                        AND (t.owner_user_id = $26 OR t.visibility = 'organization')
                    )
                )
            ELSE $8 != 'visual' AND t.is_default = TRUE AND (
                (t.organization_id IS NOT DISTINCT FROM $25::BIGINT AND t.owner_user_id = $26)
                OR (t.visibility = 'global' AND t.transfer_pending_at IS NULL)
            )
        END
    ORDER BY CASE WHEN t.organization_id IS NOT DISTINCT FROM $25::BIGINT AND t.owner_user_id = $26 THEN 0 ELSE 1 END, t.id
    LIMIT 1
),
camp AS (
    INSERT INTO campaigns (uuid, type, name, subject, from_email, body, altbody,
        content_type, daily_send_limit, daily_resume_time, send_at, headers, attribs, tags, messenger, template_id, to_send,
        max_subscriber_id, archive, archive_slug, archive_template_id, archive_meta, body_source, auto_track_links,
        organization_id, owner_user_id, original_owner_user_id, visibility)
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
            $24,
            $25, $26, $27, $28
        WHERE (SELECT valid FROM valid_lists)
        RETURNING id
),
med AS (
    INSERT INTO campaign_media (campaign_id, media_id, filename)
        (SELECT (SELECT id FROM camp), m.id, m.filename FROM media m
         WHERE m.id = ANY($22::INT[])
            AND m.transfer_pending_at IS NULL
            AND (
                m.visibility = 'global'
                OR (
                    m.organization_id IS NOT DISTINCT FROM $25::BIGINT
                    AND (m.owner_user_id = $26 OR m.visibility = 'organization')
                )
            )
         UNION
         SELECT (SELECT id FROM camp), m.id, m.filename
         FROM template_media tm JOIN media m ON (m.id = tm.media_id)
         WHERE tm.template_id = (SELECT source_id FROM tpl)
            AND (SELECT id FROM tpl) IS NULL)
        ON CONFLICT (campaign_id, media_id) DO NOTHING
),
insLists AS (
    INSERT INTO campaign_lists (campaign_id, list_id, list_name)
        SELECT (SELECT id FROM camp), id, name FROM scoped_lists
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
        COALESCE((SELECT rm.email FROM reply_mailboxes rm WHERE rm.id = c.reply_mailbox_id), '') AS reply_mailbox_email,
        CASE
            WHEN EXISTS (
                SELECT 1
                FROM campaign_recipients crx
                JOIN subscribers sx ON sx.id = crx.subscriber_id
                WHERE crx.campaign_id = c.id
                    AND sx.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND sx.owner_user_id = c.owner_user_id
                    AND sx.transfer_pending_at IS NULL
            ) THEN (
                SELECT COUNT(*)
                FROM campaign_recipients cr
                JOIN subscribers sr ON sr.id = cr.subscriber_id
                WHERE cr.campaign_id = c.id
                    AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
                    AND sr.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND sr.owner_user_id = c.owner_user_id
                    AND sr.transfer_pending_at IS NULL
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
    COALESCE(owner_user.attribs, '{}'::jsonb) AS owner_user_attribs,
    COALESCE((SELECT rm.email FROM reply_mailboxes rm WHERE rm.id = campaigns.reply_mailbox_id), '') AS reply_mailbox_email,
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM campaign_recipients crx
            JOIN subscribers sx ON sx.id = crx.subscriber_id
            WHERE crx.campaign_id = campaigns.id
                AND sx.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sx.owner_user_id = campaigns.owner_user_id
                AND sx.transfer_pending_at IS NULL
        ) THEN (
            SELECT COUNT(*)
            FROM campaign_recipients cr
            JOIN subscribers sr ON sr.id = cr.subscriber_id
            WHERE cr.campaign_id = campaigns.id
                AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
                AND sr.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sr.owner_user_id = campaigns.owner_user_id
                AND sr.transfer_pending_at IS NULL
        )
        ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
    END AS unsent_count,
    COALESCE(templates.body, (
        SELECT fallback.body FROM templates fallback
            WHERE fallback.is_default = true
            AND fallback.transfer_pending_at IS NULL
            AND (fallback.organization_id IS NULL OR EXISTS (
                SELECT 1 FROM organizations fallback_organization
                WHERE fallback_organization.id = fallback.organization_id
                    AND fallback_organization.status = 'active'
            ))
            AND (
                (fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                    AND (fallback.owner_user_id = campaigns.owner_user_id
                        OR fallback.visibility = 'organization'))
                OR fallback.visibility = 'global'
            )
        ORDER BY CASE WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
            AND fallback.owner_user_id = campaigns.owner_user_id THEN 0 ELSE 1 END, fallback.id
        LIMIT 1
    ), '') AS template_body
    FROM campaigns
    LEFT JOIN users owner_user ON owner_user.id = campaigns.owner_user_id
    LEFT JOIN templates ON (
        CASE WHEN $4 = 'default' THEN templates.id = campaigns.template_id
        ELSE templates.id = campaigns.archive_template_id END
    )
        AND templates.transfer_pending_at IS NULL
        AND (templates.organization_id IS NULL OR EXISTS (
            SELECT 1 FROM organizations template_organization
            WHERE template_organization.id = templates.organization_id
                AND template_organization.status = 'active'
        ))
        AND (
            templates.visibility = 'global'
            OR (
                templates.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND (templates.owner_user_id = campaigns.owner_user_id
                    OR templates.visibility = 'organization')
            )
        )
    WHERE CASE
            WHEN $1 > 0 THEN campaigns.id = $1
            WHEN $3 != '' THEN campaigns.archive_slug = $3
            ELSE uuid = $2
          END
        AND ($4 <> 'archive' OR (
            campaigns.archive = true
            AND campaigns.type = 'regular'
            AND campaigns.status = ANY('{running, paused, deferred, finished}'::campaign_status[])
            AND campaigns.transfer_pending_at IS NULL
            AND (campaigns.organization_id IS NULL OR EXISTS (
                SELECT 1 FROM organizations organization
                WHERE organization.id = campaigns.organization_id AND organization.status = 'active'
            ))
        ));

-- name: get-public-campaign-recipient
WITH campaign AS (
    SELECT id, organization_id, owner_user_id
    FROM campaigns WHERE uuid = $1::UUID
),
subscriber AS (
    SELECT id, organization_id, owner_user_id
    FROM subscribers
    WHERE id IN (
        SELECT id FROM subscribers WHERE uuid = $2::UUID
        UNION
        SELECT subscriber_id FROM subscriber_uuid_aliases WHERE uuid = $2::UUID
    )
),
snapshot_recipient AS (
    -- A recipient snapshot is the authority for a sent message. Resource
    -- ownership can legitimately change after delivery (for example, when a
    -- departing member's resources are transferred), so do not bind this
    -- branch to the current owner or organization fields.
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE EXISTS (
        SELECT 1 FROM campaign_recipients cr
        WHERE cr.campaign_id = c.id AND cr.subscriber_id = s.id
    )
),
legacy_recipient AS (
    -- Old campaigns created before recipient snapshots existed can only use
    -- the historical campaign-list relationship, which must remain in the
    -- same current owner/workspace boundary.
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
        AND EXISTS (
            SELECT 1 FROM campaign_lists cl
            JOIN subscriber_lists sl ON sl.list_id = cl.list_id
            WHERE cl.campaign_id = c.id AND sl.subscriber_id = s.id
        )
)
SELECT campaign_id, subscriber_id FROM snapshot_recipient
UNION ALL
SELECT campaign_id, subscriber_id FROM legacy_recipient
LIMIT 1;

-- name: get-archived-campaigns
SELECT COUNT(*) OVER () AS total, campaigns.*,
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM campaign_recipients crx
            JOIN subscribers sx ON sx.id = crx.subscriber_id
            WHERE crx.campaign_id = campaigns.id
                AND sx.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sx.owner_user_id = campaigns.owner_user_id
                AND sx.transfer_pending_at IS NULL
        ) THEN (
            SELECT COUNT(*)
            FROM campaign_recipients cr
            JOIN subscribers sr ON sr.id = cr.subscriber_id
            WHERE cr.campaign_id = campaigns.id
                AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
                AND sr.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sr.owner_user_id = campaigns.owner_user_id
                AND sr.transfer_pending_at IS NULL
        )
        ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
    END AS unsent_count,
    COALESCE(templates.body, (
        SELECT fallback.body FROM templates fallback
            WHERE fallback.is_default = true
            AND fallback.transfer_pending_at IS NULL
            AND (fallback.organization_id IS NULL OR EXISTS (
                SELECT 1 FROM organizations fallback_organization
                WHERE fallback_organization.id = fallback.organization_id
                    AND fallback_organization.status = 'active'
            ))
            AND (
                (fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                    AND (fallback.owner_user_id = campaigns.owner_user_id
                        OR fallback.visibility = 'organization'))
                OR fallback.visibility = 'global'
            )
        ORDER BY CASE WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
            AND fallback.owner_user_id = campaigns.owner_user_id THEN 0 ELSE 1 END, fallback.id
        LIMIT 1
    ), '') AS template_body
    FROM campaigns
    LEFT JOIN organizations organization ON organization.id = campaigns.organization_id
    LEFT JOIN templates ON (
        CASE WHEN $3 = 'default' THEN templates.id = campaigns.template_id
        ELSE templates.id = campaigns.archive_template_id END
    )
        AND templates.transfer_pending_at IS NULL
        AND (templates.organization_id IS NULL OR EXISTS (
            SELECT 1 FROM organizations template_organization
            WHERE template_organization.id = templates.organization_id
                AND template_organization.status = 'active'
        ))
        AND (
            templates.visibility = 'global'
            OR (
                templates.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND (templates.owner_user_id = campaigns.owner_user_id
                    OR templates.visibility = 'organization')
            )
        )
    WHERE campaigns.archive=true AND campaigns.type='regular' AND campaigns.status=ANY('{running, paused, deferred, finished}')
		AND campaigns.transfer_pending_at IS NULL
		AND (campaigns.organization_id IS NULL OR organization.status = 'active')
		AND campaigns.visibility = 'global'
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
COALESCE(owner_user.attribs, '{}'::jsonb) AS owner_user_attribs,
CASE
    WHEN EXISTS (
        SELECT 1
        FROM campaign_recipients crx
        JOIN subscribers sx ON sx.id = crx.subscriber_id
        WHERE crx.campaign_id = campaigns.id
            AND sx.organization_id IS NOT DISTINCT FROM campaigns.organization_id
            AND sx.owner_user_id = campaigns.owner_user_id
            AND sx.transfer_pending_at IS NULL
    ) THEN (
        SELECT COUNT(*)
        FROM campaign_recipients cr
        JOIN subscribers sr ON sr.id = cr.subscriber_id
        WHERE cr.campaign_id = campaigns.id
            AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
            AND sr.organization_id IS NOT DISTINCT FROM campaigns.organization_id
            AND sr.owner_user_id = campaigns.owner_user_id
            AND sr.transfer_pending_at IS NULL
    )
    ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
END AS unsent_count,
COALESCE(templates.body, '') AS template_body,
COALESCE((
    SELECT ARRAY_AGG(DISTINCT x.media_id ORDER BY x.media_id)::INT[]
    FROM (
        SELECT cm.media_id FROM campaign_media cm
        WHERE cm.campaign_id = campaigns.id AND cm.media_id IS NOT NULL
        UNION
        SELECT tm.media_id FROM template_media tm
        WHERE tm.template_id = COALESCE(NULLIF($2, 0), campaigns.template_id)
            AND tm.media_id IS NOT NULL
    ) x
), '{}') AS media_id,
(
	SELECT COALESCE(ARRAY_TO_JSON(ARRAY_AGG(l)), '[]') FROM (
		SELECT COALESCE(campaign_lists.list_id, 0) AS id,
        campaign_lists.list_name AS name
        FROM campaign_lists WHERE campaign_lists.campaign_id = campaigns.id
	) l
) AS lists
FROM campaigns
LEFT JOIN users owner_user ON owner_user.id = campaigns.owner_user_id
LEFT JOIN templates ON (templates.id = (CASE WHEN $2=0 THEN campaigns.template_id ELSE $2 END))
WHERE campaigns.id = $1;

-- name: get-campaign-status
SELECT id, status, to_send, sent,
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM campaign_recipients crx
            JOIN subscribers sx ON sx.id = crx.subscriber_id
            WHERE crx.campaign_id = campaigns.id
                AND sx.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sx.owner_user_id = campaigns.owner_user_id
                AND sx.transfer_pending_at IS NULL
        ) THEN (
            SELECT COUNT(*)
            FROM campaign_recipients cr
            JOIN subscribers sr ON sr.id = cr.subscriber_id
            WHERE cr.campaign_id = campaigns.id
                AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
                AND sr.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND sr.owner_user_id = campaigns.owner_user_id
                AND sr.transfer_pending_at IS NULL
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
        COALESCE(owner_user.attribs, '{}'::jsonb) AS owner_user_attribs,
        COALESCE((SELECT rm.email FROM reply_mailboxes rm WHERE rm.id = campaigns.reply_mailbox_id), '') AS reply_mailbox_email,
        CASE
            WHEN EXISTS (
                SELECT 1
                FROM campaign_recipients crx
                JOIN subscribers sx ON sx.id = crx.subscriber_id
                WHERE crx.campaign_id = campaigns.id
                    AND sx.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                    AND sx.owner_user_id = campaigns.owner_user_id
                    AND sx.transfer_pending_at IS NULL
            ) THEN (
                SELECT COUNT(*)
                FROM campaign_recipients cr
                JOIN subscribers sr ON sr.id = cr.subscriber_id
                WHERE cr.campaign_id = campaigns.id
                    AND cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
                    AND sr.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                    AND sr.owner_user_id = campaigns.owner_user_id
                    AND sr.transfer_pending_at IS NULL
            )
            ELSE GREATEST(campaigns.to_send - campaigns.sent, 0)
        END AS unsent_count,
        COALESCE(templates.body, (
            SELECT fallback.body FROM templates fallback
            WHERE fallback.is_default = TRUE
                AND fallback.transfer_pending_at IS NULL
                AND (fallback.organization_id IS NULL OR EXISTS (
                    SELECT 1 FROM organizations fallback_organization
                    WHERE fallback_organization.id = fallback.organization_id
                        AND fallback_organization.status = 'active'
                ))
                AND (
                    (fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                        AND (fallback.owner_user_id = campaigns.owner_user_id
                            OR fallback.visibility = 'organization'))
                    OR fallback.visibility = 'global'
                )
            ORDER BY CASE WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND fallback.owner_user_id = campaigns.owner_user_id THEN 0
                WHEN fallback.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                    AND fallback.visibility = 'organization' THEN 1
                ELSE 2 END, fallback.id
            LIMIT 1
        ), '') AS template_body
    FROM campaigns
    LEFT JOIN users owner_user ON owner_user.id = campaigns.owner_user_id
    LEFT JOIN organizations organization ON organization.id = campaigns.organization_id
    -- Never hydrate a campaign with a template that is outside the campaign's
    -- active workspace.  The scheduler is global, so an ID-only join here
    -- could otherwise send another user's private template body when a
    -- campaign row is public or organization-shared.
    LEFT JOIN templates ON (
        templates.id = campaigns.template_id
        AND templates.transfer_pending_at IS NULL
        AND (templates.organization_id IS NULL OR EXISTS (
            SELECT 1 FROM organizations template_organization
            WHERE template_organization.id = templates.organization_id
                AND template_organization.status = 'active'
        ))
        AND (
            templates.visibility = 'global'
            OR (
                templates.organization_id IS NOT DISTINCT FROM campaigns.organization_id
                AND (
                    templates.owner_user_id = campaigns.owner_user_id
                    OR templates.visibility = 'organization'
                )
            )
        )
    )
    WHERE (
        campaigns.status='running'
        OR (campaigns.status='scheduled' AND $2::TIMESTAMPTZ >= campaigns.send_at)
        OR (campaigns.status='deferred' AND campaigns.next_resume_at IS NOT NULL AND $2::TIMESTAMPTZ >= campaigns.next_resume_at)
    )
    AND campaigns.owner_user_id IS NOT NULL
    AND campaigns.transfer_pending_at IS NULL
	AND (campaigns.organization_id IS NULL OR organization.status = 'active')
    AND NOT(campaigns.id = ANY($1::INT[]))
),
campMedia AS (
    SELECT c.id AS campaign_id, ARRAY_AGG(DISTINCT x.media_id ORDER BY x.media_id)::INT[] AS media_id
    FROM camps c
    JOIN LATERAL (
        SELECT cm.media_id
        FROM campaign_media cm
        JOIN media m ON m.id = cm.media_id
        WHERE cm.campaign_id = c.id AND cm.media_id IS NOT NULL
            AND m.transfer_pending_at IS NULL
            AND (
                m.visibility = 'global'
                OR (
                    m.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND m.owner_user_id = c.owner_user_id
                )
                OR (
                    m.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND m.visibility = 'organization'
                )
            )
        UNION
        SELECT tm.media_id FROM template_media tm
        JOIN templates t ON t.id = tm.template_id
        LEFT JOIN organizations template_organization ON template_organization.id = t.organization_id
        JOIN media m ON m.id = tm.media_id
        WHERE tm.template_id = c.template_id AND tm.media_id IS NOT NULL
            AND t.transfer_pending_at IS NULL
            AND (t.organization_id IS NULL OR template_organization.status = 'active')
            AND (
                t.visibility = 'global'
                OR (
                    t.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND (t.owner_user_id = c.owner_user_id OR t.visibility = 'organization')
                )
            )
            AND m.transfer_pending_at IS NULL
            AND (
                m.visibility = 'global'
                OR (
                    m.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND m.owner_user_id = c.owner_user_id
                )
                OR (
                    m.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND m.visibility = 'organization'
                )
                OR (
                    m.organization_id IS NOT DISTINCT FROM t.organization_id
                    AND m.owner_user_id IS NOT NULL
                    AND t.owner_user_id IS NOT NULL
                    AND m.owner_user_id = t.owner_user_id
                    AND m.visibility <> 'organization'
                )
            )
    ) x ON TRUE
    GROUP BY c.id
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

-- name: get-campaigns-report-summary
WITH sent AS (
    SELECT campaign_id, COUNT(*) AS sent
    FROM campaign_recipients
    WHERE campaign_id = ANY($1)
      AND sent_at IS NOT NULL
      AND sent_at >= $2
      AND sent_at <= $3
    GROUP BY campaign_id
),
views AS (
    SELECT
        campaign_id,
        COUNT(*) AS views_total,
        COUNT(DISTINCT subscriber_id) AS unique_viewers
    FROM campaign_views
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
    GROUP BY campaign_id
),
clicks AS (
    SELECT
        campaign_id,
        COUNT(*) AS clicks_total,
        COUNT(DISTINCT subscriber_id) AS unique_clickers
    FROM link_clicks
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
    GROUP BY campaign_id
),
bounces AS (
    SELECT campaign_id, COUNT(*) AS bounced
    FROM bounces
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
    GROUP BY campaign_id
)
SELECT
    COALESCE((SELECT SUM(sent) FROM sent), 0) AS sent,
    COALESCE((SELECT SUM(bounced) FROM bounces), 0) AS bounced,
    COALESCE((SELECT SUM(views_total) FROM views), 0) AS views_total,
    COALESCE((SELECT SUM(clicks_total) FROM clicks), 0) AS clicks_total,
    COALESCE((SELECT SUM(unique_viewers) FROM views), 0) AS unique_viewers,
    COALESCE((SELECT SUM(unique_clickers) FROM clicks), 0) AS unique_clickers;

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

-- name: get-campaigns-report-links
WITH sent AS (
    SELECT campaign_id, COUNT(*) AS sent
    FROM campaign_recipients
    WHERE campaign_id = ANY($1)
      AND sent_at IS NOT NULL
      AND sent_at >= $2
      AND sent_at <= $3
    GROUP BY campaign_id
)
SELECT
    lc.campaign_id,
    c.name AS campaign_name,
    c.subject AS campaign_subject,
    links.id AS link_id,
    links.url,
    COUNT(*) AS total_clicks,
    COUNT(DISTINCT lc.subscriber_id) AS unique_clickers,
    COALESCE(s.sent, 0) AS sent
FROM link_clicks lc
JOIN campaigns c ON c.id = lc.campaign_id
LEFT JOIN links ON lc.link_id = links.id
LEFT JOIN sent s ON s.campaign_id = lc.campaign_id
WHERE lc.campaign_id = ANY($1)
  AND lc.created_at >= $2
  AND lc.created_at <= $3
GROUP BY lc.campaign_id, c.name, c.subject, links.id, links.url, s.sent
ORDER BY total_clicks DESC, c.name ASC, links.url ASC
LIMIT 200;

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

-- name: query-campaigns-report-recipients
WITH view_stats AS (
    SELECT
        campaign_id,
        subscriber_id,
        COUNT(*) AS view_count,
        MIN(created_at) AS first_viewed_at,
        MAX(created_at) AS last_viewed_at
    FROM campaign_views
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
      AND subscriber_id IS NOT NULL
    GROUP BY campaign_id, subscriber_id
),
click_stats AS (
    SELECT
        campaign_id,
        subscriber_id,
        COUNT(*) AS click_count,
        MIN(created_at) AS first_clicked_at,
        MAX(created_at) AS last_clicked_at
    FROM link_clicks
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
      AND subscriber_id IS NOT NULL
    GROUP BY campaign_id, subscriber_id
),
last_click AS (
    SELECT DISTINCT ON (lc.campaign_id, lc.subscriber_id)
        lc.campaign_id,
        lc.subscriber_id,
        lc.link_id AS last_link_id,
        links.url AS last_link_url,
        lc.created_at AS last_clicked_at
    FROM link_clicks lc
    LEFT JOIN links ON links.id = lc.link_id
    WHERE lc.campaign_id = ANY($1)
      AND lc.created_at >= $2
      AND lc.created_at <= $3
      AND lc.subscriber_id IS NOT NULL
    ORDER BY lc.campaign_id, lc.subscriber_id, lc.created_at DESC, lc.id DESC
),
bounce_stats AS (
    SELECT
        campaign_id,
        subscriber_id,
        COUNT(*) AS bounce_count,
        MAX(created_at) AS last_bounced_at
    FROM bounces
    WHERE campaign_id = ANY($1)
      AND created_at >= $2
      AND created_at <= $3
    GROUP BY campaign_id, subscriber_id
),
filtered AS (
    SELECT
        c.id AS campaign_id,
        c.name AS campaign_name,
        c.subject AS campaign_subject,
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
    JOIN campaigns c ON c.id = cr.campaign_id
    JOIN subscribers s ON s.id = cr.subscriber_id
    LEFT JOIN view_stats vs ON vs.campaign_id = cr.campaign_id AND vs.subscriber_id = cr.subscriber_id
    LEFT JOIN click_stats cs ON cs.campaign_id = cr.campaign_id AND cs.subscriber_id = cr.subscriber_id
    LEFT JOIN last_click lc ON lc.campaign_id = cr.campaign_id AND lc.subscriber_id = cr.subscriber_id
    LEFT JOIN bounce_stats bs ON bs.campaign_id = cr.campaign_id AND bs.subscriber_id = cr.subscriber_id
    WHERE cr.campaign_id = ANY($1)
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
              WHERE lcf.campaign_id = cr.campaign_id
                AND lcf.subscriber_id = cr.subscriber_id
                AND lcf.created_at >= $2
                AND lcf.created_at <= $3
                AND lcf.link_id = $8
          )
      )
)
SELECT
    campaign_id,
    campaign_name,
    campaign_subject,
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
WITH valid_recipients AS (
    SELECT cr.*
    FROM campaign_recipients cr
    JOIN campaigns c ON c.id = cr.campaign_id
    JOIN subscribers s ON s.id = cr.subscriber_id
    WHERE c.id = $1
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id = c.owner_user_id
        AND s.transfer_pending_at IS NULL
)
SELECT campaigns.id AS campaign_id,
    campaigns.type AS campaign_type,
    campaigns.status,
    campaigns.messenger,
    campaigns.owner_user_id,
    CASE
        WHEN campaigns.type = 'regular'
             AND (campaigns.messenger = 'email' OR campaigns.messenger LIKE 'email-%')
             AND campaigns.daily_send_limit < 1
        THEN 300
        ELSE campaigns.daily_send_limit
    END AS daily_send_limit,
    campaigns.daily_resume_time,
    campaigns.next_resume_at,
    COALESCE((
        SELECT sent_count FROM campaign_daily_usage
        WHERE campaign_id = campaigns.id AND usage_date = $2::DATE
    ), 0) AS daily_sent_count,
    COALESCE((
        SELECT COUNT(*) FROM valid_recipients
        WHERE status = 'queued'
    ), 0) AS queued_count,
    COALESCE((
        SELECT COUNT(*) FROM valid_recipients
        WHERE status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])
    ), 0) AS unsent_count
FROM campaigns
WHERE campaigns.id = $1;

-- name: has-campaign-recipients
SELECT EXISTS (
    SELECT 1
    FROM campaign_recipients cr
    JOIN campaigns c ON c.id = cr.campaign_id
    JOIN subscribers s ON s.id = cr.subscriber_id
    WHERE cr.campaign_id = $1
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id = c.owner_user_id
        AND s.transfer_pending_at IS NULL
);

-- name: ensure-campaign-recipients
-- Build a recipient snapshot only from lists and subscribers that still
-- belong to the campaign owner in the same personal or organization space.
-- This is deliberately independent of API validation: resources can move
-- after a campaign is saved, and stale relationship rows must never cross a
-- tenant boundary into a send.
WITH campaign AS (
    SELECT c.id, c.type, c.organization_id, c.owner_user_id
    FROM campaigns c
    LEFT JOIN organizations organization ON organization.id = c.organization_id
    WHERE c.id = $1
        AND c.owner_user_id IS NOT NULL
        AND c.transfer_pending_at IS NULL
        AND (c.organization_id IS NULL OR organization.status = 'active')
),
campLists AS (
    SELECT l.id AS list_id, l.optin
    FROM campaign_lists cl
    JOIN campaign c ON c.id = cl.campaign_id
    JOIN lists l ON l.id = cl.list_id
    WHERE l.organization_id IS NOT DISTINCT FROM c.organization_id
        AND l.owner_user_id = c.owner_user_id
        AND l.transfer_pending_at IS NULL
),
subs AS (
    SELECT DISTINCT s.id AS subscriber_id
    FROM subscriber_lists sl
    JOIN campLists ON sl.list_id = campLists.list_id
    JOIN subscribers s ON s.id = sl.subscriber_id
    JOIN campaign c ON TRUE
    WHERE s.status != 'blocklisted'
    AND s.organization_id IS NOT DISTINCT FROM c.organization_id
    AND s.owner_user_id = c.owner_user_id
    AND s.transfer_pending_at IS NULL
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
INSERT INTO campaign_recipients (campaign_id, subscriber_id, status, email_snapshot, name_snapshot, attribs_snapshot)
SELECT $1, s.id, 'pending'::campaign_recipient_status, s.email, s.name, s.attribs
FROM subs
JOIN subscribers s ON s.id = subs.subscriber_id
ON CONFLICT (campaign_id, subscriber_id) DO NOTHING;

-- name: sync-campaign-progress
WITH counts AS (
    SELECT
        COUNT(*) AS total,
        COUNT(*) FILTER (WHERE cr.status = 'sent') AS sent
    FROM campaign_recipients cr
    JOIN campaigns c ON c.id = cr.campaign_id
    JOIN subscribers s ON s.id = cr.subscriber_id
    WHERE cr.campaign_id = $1
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id = c.owner_user_id
        AND s.transfer_pending_at IS NULL
)
UPDATE campaigns
SET to_send = COALESCE((SELECT total FROM counts), 0),
    sent = COALESCE((SELECT sent FROM counts), 0),
    started_at = CASE WHEN started_at IS NULL THEN NOW() ELSE started_at END,
    updated_at = NOW()
WHERE id = $1
RETURNING to_send, sent, started_at;

-- name: snapshot-campaign-recipients
-- Backfill snapshots for campaigns created before recipient snapshots existed.
UPDATE campaign_recipients cr
SET email_snapshot = s.email,
    name_snapshot = s.name,
    attribs_snapshot = s.attribs,
    updated_at = NOW()
FROM subscribers s
WHERE cr.campaign_id = $1
  AND cr.subscriber_id = s.id
  AND cr.email_snapshot IS NULL;

-- name: set-campaign-running
UPDATE campaigns
SET status = 'running',
    next_resume_at = NULL,
    started_at = CASE WHEN started_at IS NULL THEN NOW() ELSE started_at END,
    updated_at = NOW()
WHERE id = $1
    AND status = ANY('{scheduled,deferred}'::campaign_status[])
    AND owner_user_id IS NOT NULL
    AND transfer_pending_at IS NULL
    AND (organization_id IS NULL OR EXISTS (
        SELECT 1 FROM organizations organization
        WHERE organization.id = campaigns.organization_id
            AND organization.status = 'active'
    ));

-- name: set-campaign-deferred
UPDATE campaigns
SET status = 'deferred',
    next_resume_at = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: queue-campaign-subscribers
-- Recheck every snapshot recipient immediately before queuing. This protects
-- in-flight campaigns from a list/subscriber being moved or transferred after
-- the recipient snapshot was first created.
WITH picked AS (
    SELECT cr.subscriber_id
    FROM campaign_recipients cr
    JOIN campaigns c ON c.id = cr.campaign_id
    JOIN subscribers s ON s.id = cr.subscriber_id
    WHERE cr.campaign_id = $1
      AND cr.status = ANY($2::campaign_recipient_status[])
      AND c.status = 'running'
      AND c.owner_user_id IS NOT NULL
      AND c.transfer_pending_at IS NULL
      -- Use EXISTS instead of an outer join here. PostgreSQL rejects FOR
      -- UPDATE on a query whose nullable side participates in the join,
      -- even when the lock is explicitly restricted to campaign_recipients.
      AND (c.organization_id IS NULL OR EXISTS (
          SELECT 1 FROM organizations organization
          WHERE organization.id = c.organization_id
            AND organization.status = 'active'
      ))
      AND s.organization_id IS NOT DISTINCT FROM c.organization_id
      AND s.owner_user_id = c.owner_user_id
      AND s.transfer_pending_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM campaign_lists cl
          JOIN lists l ON l.id = cl.list_id
          JOIN subscriber_lists sl ON sl.list_id = l.id AND sl.subscriber_id = s.id
          WHERE cl.campaign_id = c.id
              AND l.organization_id IS NOT DISTINCT FROM c.organization_id
              AND l.owner_user_id = c.owner_user_id
              AND l.transfer_pending_at IS NULL
              AND (
                  (c.type = 'optin' AND sl.status = 'unconfirmed' AND l.optin = 'double')
                  OR (
                      c.type != 'optin' AND (
                          (l.optin = 'double' AND sl.status = 'confirmed')
                          OR (l.optin != 'double' AND sl.status != 'unsubscribed')
                      )
                  )
              )
      )
    ORDER BY subscriber_id
    -- Lock only the recipient rows that are about to be marked queued.
    FOR UPDATE OF cr SKIP LOCKED
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
SELECT s.id, s.uuid,
    COALESCE(cr.email_snapshot, s.email) AS email,
    COALESCE(cr.name_snapshot, s.name) AS name,
    COALESCE(cr.attribs_snapshot, s.attribs) AS attribs,
    s.status, s.created_at, s.updated_at,
    s.organization_id, s.owner_user_id, s.original_owner_user_id, s.visibility,
    s.transfer_pending_at,
    u.recipient_status, u.sent_at
FROM u
JOIN subscribers s ON s.id = u.subscriber_id
JOIN campaign_recipients cr ON cr.campaign_id = $1 AND cr.subscriber_id = s.id
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
SELECT s.* FROM subscribers s
JOIN campaigns c ON c.id = $1
JOIN subscriber_lists sl ON sl.subscriber_id = s.id AND sl.status != 'unsubscribed'
JOIN campaign_lists cl ON cl.list_id = sl.list_id AND cl.campaign_id = c.id
JOIN lists l ON l.id = cl.list_id
WHERE s.organization_id IS NOT DISTINCT FROM c.organization_id
    AND s.owner_user_id = c.owner_user_id
    AND s.transfer_pending_at IS NULL
    AND l.organization_id IS NOT DISTINCT FROM c.organization_id
    AND l.owner_user_id = c.owner_user_id
    AND l.transfer_pending_at IS NULL
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
        (SELECT $1 AS campaign_id, m.id, m.filename
        FROM media m
        JOIN campaigns c ON c.id = $1
        WHERE m.id = ANY($21::INT[])
            AND m.transfer_pending_at IS NULL
            AND (
                m.visibility = 'global'
                OR (
                    m.organization_id IS NOT DISTINCT FROM c.organization_id
                    AND (m.owner_user_id = c.owner_user_id OR m.visibility = 'organization')
                )
            ))
        ON CONFLICT (campaign_id, media_id) DO NOTHING
)
INSERT INTO campaign_lists (campaign_id, list_id, list_name)
    (SELECT $1 AS campaign_id, l.id, l.name
    FROM lists l
    JOIN campaigns c ON c.id = $1
    WHERE l.id = ANY($16::INT[])
        AND l.organization_id IS NOT DISTINCT FROM c.organization_id
        AND l.owner_user_id = c.owner_user_id
        AND l.transfer_pending_at IS NULL)
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
WHERE id = $1
    AND transfer_pending_at IS NULL
    AND (organization_id IS NULL OR EXISTS (
        SELECT 1 FROM organizations organization
        WHERE organization.id = campaigns.organization_id
            AND organization.status = 'active'
    ));

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
-- When individual tracking is enabled, only record a view for a recipient
-- relation belonging to this campaign. Without individual tracking, retain
-- the aggregate campaign-level event with a NULL subscriber ID. The fallback
-- covers historical campaigns created before recipient snapshots existed.
WITH campaign AS (
    SELECT id, organization_id, owner_user_id
    FROM campaigns WHERE uuid = $1::UUID
),
subscriber AS (
    SELECT id, organization_id, owner_user_id
    FROM subscribers
    WHERE id IN (
        SELECT id FROM subscribers WHERE uuid = NULLIF($2::TEXT, '')::UUID
        UNION
        SELECT subscriber_id FROM subscriber_uuid_aliases WHERE uuid = NULLIF($2::TEXT, '')::UUID
    )
),
snapshot_recipient AS (
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE EXISTS (
        SELECT 1 FROM campaign_recipients cr
        WHERE cr.campaign_id = c.id AND cr.subscriber_id = s.id
    )
),
legacy_recipient AS (
    SELECT c.id AS campaign_id, s.id AS subscriber_id
    FROM campaign c
    JOIN subscriber s ON TRUE
    WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
        AND s.organization_id IS NOT DISTINCT FROM c.organization_id
        AND s.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
        AND EXISTS (
            SELECT 1 FROM campaign_lists cl
            JOIN subscriber_lists sl ON sl.list_id = cl.list_id
            WHERE cl.campaign_id = c.id AND sl.subscriber_id = s.id
        )
),
recipient AS (
    SELECT campaign_id, subscriber_id FROM snapshot_recipient
    UNION ALL
    SELECT campaign_id, subscriber_id FROM legacy_recipient
),
view AS (
    SELECT c.id AS campaign_id,
        CASE WHEN $2::TEXT = '' THEN NULL ELSE r.subscriber_id END AS subscriber_id
    FROM campaign c
    LEFT JOIN recipient r ON r.campaign_id = c.id
    WHERE $2::TEXT = '' OR r.subscriber_id IS NOT NULL
)
INSERT INTO campaign_views (campaign_id, subscriber_id)
    SELECT campaign_id, subscriber_id FROM view;
