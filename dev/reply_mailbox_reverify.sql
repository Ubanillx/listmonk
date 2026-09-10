-- Dev-only verification for update-reply-mailbox: changing a connection
-- parameter must reset status/verified_at, while saving with unchanged
-- parameters must preserve them.
-- Run with:
--   docker exec -i dev-db-1 psql -U <user> -d <db> -v ON_ERROR_STOP=1 < dev/reply_mailbox_reverify.sql
BEGIN;

INSERT INTO reply_mailboxes (user_id, email, username, imap_host, imap_port, imap_tls, folder, password, status, verified_at, is_default, ai_enabled)
SELECT id, 'dsh-reverify@example.com', 'dsh', 'pop.example.com', 995, TRUE, 'INBOX', 'secret', 'active', NOW(), FALSE, FALSE
FROM users ORDER BY id LIMIT 1
RETURNING id AS mailbox_id, user_id AS owner_id \gset

PREPARE upd_reply_mailbox (int, int, text, text, text, text, int, bool, text, text, bool, bool, int) AS
UPDATE reply_mailboxes
SET email = $3, name = $4, username = $5, imap_host = $6, imap_port = $7,
    imap_tls = $8, folder = $9,
	password = CASE WHEN $10 = '' THEN password ELSE $10 END,
	is_default = $11,
	ai_enabled = $12,
	status = CASE WHEN (imap_host, imap_port, imap_tls, folder, username) IS DISTINCT FROM ($6, $7, $8, $9, $5)
	                   OR ($10 <> '' AND password IS DISTINCT FROM $10)
	              THEN 'pending' ELSE status END,
	verified_at = CASE WHEN (imap_host, imap_port, imap_tls, folder, username) IS DISTINCT FROM ($6, $7, $8, $9, $5)
	                        OR ($10 <> '' AND password IS DISTINCT FROM $10)
	                   THEN NULL ELSE verified_at END,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND organization_id IS NOT DISTINCT FROM $13
RETURNING id;

\echo '=== 1. identical parameters: verification must be preserved (expect active | t) ==='
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'dsh-reverify@example.com', '', 'dsh', 'pop.example.com', 995, TRUE, 'INBOX', '', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified FROM reply_mailboxes WHERE id = :mailbox_id;

\echo '=== 2. changed host: must reset (expect pending | f) ==='
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'dsh-reverify@example.com', '', 'dsh', 'pop.other.example.com', 995, TRUE, 'INBOX', '', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified, imap_host FROM reply_mailboxes WHERE id = :mailbox_id;

\echo '=== 3. reset again, then change only the port: must reset (expect pending | f) ==='
UPDATE reply_mailboxes SET status = 'active', verified_at = NOW() WHERE id = :mailbox_id;
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'dsh-reverify@example.com', '', 'dsh', 'pop.other.example.com', 110, FALSE, 'INBOX', '', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified, imap_port FROM reply_mailboxes WHERE id = :mailbox_id;

\echo '=== 4. reset again, then change only the folder: must reset (expect pending | f) ==='
UPDATE reply_mailboxes SET status = 'active', verified_at = NOW() WHERE id = :mailbox_id;
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'dsh-reverify@example.com', '', 'dsh', 'pop.other.example.com', 110, FALSE, 'Archive', '', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified, folder FROM reply_mailboxes WHERE id = :mailbox_id;

\echo '=== 5. change only the password (masked field filled in): must reset (expect pending | f) ==='
UPDATE reply_mailboxes SET status = 'active', verified_at = NOW() WHERE id = :mailbox_id;
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'dsh-reverify@example.com', '', 'dsh', 'pop.other.example.com', 110, FALSE, 'Archive', 'new-secret', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified FROM reply_mailboxes WHERE id = :mailbox_id;

\echo '=== 6. non-connection fields only (name/email): must be preserved (expect active | t) ==='
UPDATE reply_mailboxes SET status = 'active', verified_at = NOW() WHERE id = :mailbox_id;
EXECUTE upd_reply_mailbox (:mailbox_id, :owner_id, 'renamed@example.com', 'Renamed', 'dsh', 'pop.other.example.com', 110, FALSE, 'Archive', '', FALSE, FALSE, NULL);
SELECT status, verified_at IS NOT NULL AS verified, email, name FROM reply_mailboxes WHERE id = :mailbox_id;

ROLLBACK;
