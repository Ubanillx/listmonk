-- templates
-- name: get-templates
-- Only if the second param ($2 - noBody) is true, body and body_source is returned.
SELECT templates.id, templates.name, templates.type, templates.subject,
    (CASE WHEN $2 = false THEN templates.body ELSE '' END) as body,
    (CASE WHEN $2 = false THEN templates.body_source ELSE NULL END) as body_source,
    templates.is_default, templates.created_at, templates.updated_at,
    templates.organization_id, templates.owner_user_id, templates.original_owner_user_id,
    templates.visibility, templates.transfer_pending_at,
    COALESCE(owner.username, '') AS owner_username, COALESCE(owner.name, '') AS owner_name,
    COALESCE((
        SELECT ARRAY_AGG(template_media.media_id ORDER BY template_media.media_id)::INT[]
        FROM template_media
        WHERE template_media.template_id = templates.id AND template_media.media_id IS NOT NULL
    ), '{}') AS media_id,
    COALESCE((
        SELECT JSON_AGG(JSON_BUILD_OBJECT('id', template_media.media_id, 'filename', template_media.filename)
            ORDER BY template_media.media_id)
        FROM template_media
        WHERE template_media.template_id = templates.id
    ), '[]') AS media
    FROM templates LEFT JOIN users owner ON owner.id = COALESCE(templates.owner_user_id, templates.original_owner_user_id)
    WHERE ($1 = 0 OR templates.id = $1) AND ($3 = '' OR templates.type = $3::template_type)
    ORDER BY templates.created_at;

-- name: create-template
WITH tpl AS (
    INSERT INTO templates (
        name, type, subject, body, body_source,
        organization_id, owner_user_id, original_owner_user_id, visibility
    ) VALUES($1, $2, $3, $4, $5, $7, $8, $9, $10) RETURNING id
),
med AS (
    INSERT INTO template_media (template_id, media_id, filename)
        (SELECT (SELECT id FROM tpl), id, filename FROM media WHERE id = ANY($6::INT[]))
)
SELECT id FROM tpl;

-- name: update-template
WITH tpl AS (
    UPDATE templates SET
        name=(CASE WHEN $2 != '' THEN $2 ELSE name END),
        subject=(CASE WHEN $3 != '' THEN $3 ELSE subject END),
        body=(CASE WHEN $4 != '' THEN $4 ELSE body END),
        body_source=(CASE WHEN $5 != '' THEN $5 ELSE body_source END),
        updated_at=NOW()
    WHERE id = $1
    RETURNING id
),
del AS (
    DELETE FROM template_media
    WHERE template_id = $1 AND (media_id IS NULL OR NOT(media_id = ANY($6::INT[])))
),
med AS (
    INSERT INTO template_media (template_id, media_id, filename)
        (SELECT $1, id, filename FROM media WHERE id = ANY($6::INT[]))
        ON CONFLICT (template_id, media_id) DO NOTHING
)
SELECT id FROM tpl;

-- name: set-default-template
WITH u AS (
    UPDATE templates SET is_default=true WHERE id=$1 AND type='campaign' RETURNING id
)
UPDATE templates SET is_default=false WHERE id != $1;

-- name: delete-template
-- Delete a template as long as there's more than one. On deletion, set all campaigns
-- with that template to the default template instead.
WITH tpl AS (
    DELETE FROM templates WHERE id = $1 AND (SELECT COUNT(id) FROM templates) > 1 AND is_default = false RETURNING id
),
def AS (
    SELECT id FROM templates WHERE is_default = true AND (type='campaign' OR type='campaign_visual') LIMIT 1
),
up AS (
    UPDATE campaigns SET template_id = (SELECT id FROM def) WHERE (SELECT id FROM tpl) > 0 AND template_id = $1
)
SELECT id FROM tpl;
