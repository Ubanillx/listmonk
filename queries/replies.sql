-- name: get-reply-mailboxes
SELECT id, user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls,
       folder, status, verified_at, is_default, last_sync_at, last_sync_error,
       forward_count, created_at, updated_at
FROM reply_mailboxes
WHERE user_id = $1 AND organization_id IS NOT DISTINCT FROM $2
ORDER BY is_default DESC, id;

-- name: get-reply-mailbox
SELECT id, user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls,
       folder, status, verified_at, is_default, last_sync_at, last_sync_error,
       forward_count, created_at, updated_at
FROM reply_mailboxes
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3;

-- name: create-reply-mailbox
INSERT INTO reply_mailboxes
    (user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls, folder,
     password, status, verified_at, is_default)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', NULL, $11)
RETURNING id;

-- name: update-reply-mailbox
UPDATE reply_mailboxes
SET email = $3, name = $4, username = $5, imap_host = $6, imap_port = $7,
    imap_tls = $8, folder = $9,
	password = CASE WHEN $10 = '' THEN password ELSE $10 END,
	is_default = $11,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $12
RETURNING id;

-- name: disable-reply-mailbox
UPDATE reply_mailboxes
SET status = 'disabled', is_default = FALSE, updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $3
RETURNING id;
