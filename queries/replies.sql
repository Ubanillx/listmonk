-- name: get-reply-mailboxes
-- In a personal workspace ($2 NULL) only the caller's own mailboxes are
-- listed. In an organization workspace every mailbox belonging to the
-- organization is listed so any member can select a shared customer reply
-- mailbox in a campaign; the `manageable` flag tells callers which rows they
-- actually own (and may edit/disable/test). Passwords are never selected.
SELECT id, user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls,
       folder, status, verified_at, is_default, ai_enabled, last_sync_at, last_sync_error,
       forward_count, created_at, updated_at,
       (user_id = $1) AS manageable
FROM reply_mailboxes
-- The cast pins $2's type: a bare `$2 IS NULL` predicate gives PostgreSQL
-- nothing to infer from and PREPARE fails with "could not determine data type
-- of parameter $2".
WHERE ($2::INT IS NULL AND user_id = $1 AND organization_id IS NULL)
   OR organization_id = $2::INT
ORDER BY is_default DESC, id;

-- name: get-reply-mailbox
SELECT id, user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls,
       folder, status, verified_at, is_default, ai_enabled, last_sync_at, last_sync_error,
       forward_count, created_at, updated_at
FROM reply_mailboxes
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3;

-- name: create-reply-mailbox
INSERT INTO reply_mailboxes
    (user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls, folder,
     password, status, verified_at, is_default, ai_enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', NULL, $11, $12)
RETURNING id;

-- name: update-reply-mailbox
UPDATE reply_mailboxes
SET email = $3, name = $4, username = $5, imap_host = $6, imap_port = $7,
    imap_tls = $8, folder = $9,
	password = CASE WHEN $10 = '' THEN password ELSE $10 END,
	is_default = $11,
	ai_enabled = $12,
	-- Any change to the connection parameters invalidates the previous
	-- verification. The stored 'active' state used to survive a host/port edit,
	-- so an endpoint that had never been tested (for example an internal
	-- address) kept being polled as if it had passed.
	status = CASE WHEN (imap_host, imap_port, imap_tls, folder, username) IS DISTINCT FROM ($6, $7, $8, $9, $5)
	                   OR ($10 <> '' AND password IS DISTINCT FROM $10)
	              THEN 'pending' ELSE status END,
	verified_at = CASE WHEN (imap_host, imap_port, imap_tls, folder, username) IS DISTINCT FROM ($6, $7, $8, $9, $5)
	                        OR ($10 <> '' AND password IS DISTINCT FROM $10)
	                   THEN NULL ELSE verified_at END,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $13
RETURNING id;

-- name: disable-reply-mailbox
UPDATE reply_mailboxes
SET status = 'disabled', is_default = FALSE, ai_enabled = FALSE, updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3
RETURNING id;

-- name: enable-reply-mailbox
-- A disabled mailbox keeps its last successful verification. Restore that
-- state without requiring the user to re-enter a password that is never
-- returned to the browser. Unverified mailboxes remain pending until tested.
UPDATE reply_mailboxes
SET status = CASE WHEN verified_at IS NOT NULL THEN 'active' ELSE 'pending' END,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3
  AND status = 'disabled'
RETURNING id;

-- name: get-reply-ai-mailboxes
SELECT m.id, m.user_id, m.organization_id, m.email, m.username, m.password,
       m.imap_host, m.imap_port, m.imap_tls, m.folder
FROM reply_mailboxes m
JOIN users u ON u.id = m.user_id AND u.status = 'enabled'
WHERE m.status = 'active' AND m.ai_enabled = TRUE
  AND (m.organization_id IS NULL OR EXISTS (
      SELECT 1
      FROM organizations o
      JOIN organization_members om ON om.organization_id = o.id
      WHERE o.id = m.organization_id
        AND o.status = 'active'
        AND om.user_id = m.user_id
        AND om.removed_at IS NULL
  ))
ORDER BY m.id;

-- name: get-reply-ai-mailbox
SELECT m.id, m.user_id, m.organization_id, m.email, m.username, m.password,
       m.imap_host, m.imap_port, m.imap_tls, m.folder
FROM reply_mailboxes m
JOIN users u ON u.id = m.user_id AND u.status = 'enabled'
WHERE m.id = $1 AND m.status = 'active' AND m.ai_enabled = TRUE
  AND (m.organization_id IS NULL OR EXISTS (
      SELECT 1
      FROM organizations o
      JOIN organization_members om ON om.organization_id = o.id
      WHERE o.id = m.organization_id
        AND o.status = 'active'
        AND om.user_id = m.user_id
        AND om.removed_at IS NULL
  ));

-- name: insert-reply-ai-event
INSERT INTO reply_ai_events
    (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (reply_mailbox_id, message_key) DO NOTHING
RETURNING id;

-- name: claim-reply-ai-event
WITH candidates AS (
    SELECT id
    FROM reply_ai_events
    WHERE (
        (status IN ('pending', 'failed') AND next_attempt_at <= NOW() AND attempts < 3)
        OR (status = 'processing' AND lease_expires_at < NOW() AND attempts < 3)
    )
    ORDER BY next_attempt_at, id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE reply_ai_events e
SET status = 'processing',
    attempts = e.attempts + 1,
    lease_expires_at = NOW() + INTERVAL '2 minutes',
    lease_token = gen_random_uuid(),
    updated_at = NOW()
FROM candidates c
WHERE e.id = c.id
RETURNING e.*;

-- name: finish-reply-ai-event
UPDATE reply_ai_events
SET customer_id = $2,
    intent = $3,
    confidence = $4,
    reason_code = $5,
    model = $6,
    action = $7,
    status = $8,
    body = '',
    last_error = '',
    classified_at = NOW(),
    actioned_at = CASE WHEN $7 = 'blocklisted' THEN NOW() ELSE actioned_at END,
    lease_expires_at = NULL,
    lease_token = NULL,
    updated_at = NOW()
WHERE id = $1 AND status = 'processing' AND lease_token = $9::UUID;

-- name: fail-reply-ai-event
UPDATE reply_ai_events
SET status = 'failed',
    last_error = $2,
    -- Once the retry budget is exhausted the row is terminal: drop the
    -- retained body so failed attempts do not keep the raw reply forever.
    body = CASE WHEN attempts >= 3 THEN '' ELSE body END,
    lease_expires_at = NULL,
    lease_token = NULL,
    next_attempt_at = NOW() + INTERVAL '5 minutes',
    updated_at = NOW()
WHERE id = $1 AND status = 'processing' AND lease_token = $3::UUID;

-- name: update-reply-ai-mailbox-sync
UPDATE reply_mailboxes
SET last_sync_at = NOW(), last_sync_error = $2, updated_at = NOW()
WHERE id = $1;
