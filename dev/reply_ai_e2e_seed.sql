DO $$
DECLARE
  uid INT;
  mbx INT;
BEGIN
  SELECT id INTO uid FROM users WHERE username='root';
  DELETE FROM reply_ai_events WHERE message_key LIKE 'e2e-%';
  DELETE FROM reply_mailboxes WHERE email='ai-e2e@example.test';
  DELETE FROM customers WHERE email IN ('e2e-unsub@example.test','e2e-spam@example.test','e2e-other@example.test','e2e-lowconf@example.test') AND owner_user_id=uid AND organization_id IS NULL;
  DELETE FROM customer_lists WHERE name='e2e-reply-ai-list' AND owner_user_id=uid AND organization_id IS NULL;

  INSERT INTO reply_mailboxes (user_id, organization_id, email, name, username, imap_host, imap_port, imap_tls, folder, password, status, verified_at, is_default, ai_enabled)
  VALUES (uid, NULL, 'ai-e2e@example.test', 'E2E', 'ai-e2e@example.test', 'imap.example.test', 993, TRUE, 'INBOX', 'pw', 'active', NOW(), FALSE, TRUE)
  RETURNING id INTO mbx;

  INSERT INTO customer_lists (uuid, name, type, optin, status, tags, owner_user_id, organization_id, visibility)
  VALUES (gen_random_uuid(), 'e2e-reply-ai-list', 'private', 'single', 'active', '{}', uid, NULL, 'private');

  INSERT INTO customers (uuid, email, name, status, owner_user_id, organization_id, visibility, customer_code)
  SELECT gen_random_uuid(), 'e2e-unsub@example.test', 'E2E Unsub', 'enabled', uid, NULL, 'private', 'e2e-unsub'
  WHERE NOT EXISTS (SELECT 1 FROM customers WHERE email='e2e-unsub@example.test');
  INSERT INTO customers (uuid, email, name, status, owner_user_id, organization_id, visibility, customer_code)
  SELECT gen_random_uuid(), 'e2e-spam@example.test', 'E2E Spam', 'enabled', uid, NULL, 'private', 'e2e-spam'
  WHERE NOT EXISTS (SELECT 1 FROM customers WHERE email='e2e-spam@example.test');
  INSERT INTO customers (uuid, email, name, status, owner_user_id, organization_id, visibility, customer_code)
  SELECT gen_random_uuid(), 'e2e-other@example.test', 'E2E Other', 'enabled', uid, NULL, 'private', 'e2e-other'
  WHERE NOT EXISTS (SELECT 1 FROM customers WHERE email='e2e-other@example.test');
  INSERT INTO customers (uuid, email, name, status, owner_user_id, organization_id, visibility, customer_code)
  SELECT gen_random_uuid(), 'e2e-lowconf@example.test', 'E2E Low', 'enabled', uid, NULL, 'private', 'e2e-lowconf'
  WHERE NOT EXISTS (SELECT 1 FROM customers WHERE email='e2e-lowconf@example.test');

  INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
  SELECT c.id, l.id, 'confirmed' FROM customers c, customer_lists l
  WHERE c.email='e2e-unsub@example.test' AND l.name='e2e-reply-ai-list' AND l.owner_user_id=uid
  ON CONFLICT DO NOTHING;
  INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
  SELECT c.id, l.id, 'confirmed' FROM customers c, customer_lists l
  WHERE c.email='e2e-spam@example.test' AND l.name='e2e-reply-ai-list' AND l.owner_user_id=uid
  ON CONFLICT DO NOTHING;
  INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
  SELECT c.id, l.id, 'confirmed' FROM customers c, customer_lists l
  WHERE c.email='e2e-other@example.test' AND l.name='e2e-reply-ai-list' AND l.owner_user_id=uid
  ON CONFLICT DO NOTHING;
  INSERT INTO customer_list_memberships (customer_id, customer_list_id, status)
  SELECT c.id, l.id, 'confirmed' FROM customers c, customer_lists l
  WHERE c.email='e2e-lowconf@example.test' AND l.name='e2e-reply-ai-list' AND l.owner_user_id=uid
  ON CONFLICT DO NOTHING;

  INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
  SELECT mbx, 'e2e-1', 'e2e-unsub@example.test', 'Re: campaign', 'Please unsubscribe me immediately', encode(digest('Please unsubscribe me immediately','sha256'),'hex'), NOW() - INTERVAL '1 minute';
  INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
  SELECT mbx, 'e2e-2', 'e2e-spam@example.test', 'Re: campaign', 'I will report spam to my provider', encode(digest('I will report spam to my provider','sha256'),'hex'), NOW() - INTERVAL '1 minute';
  INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
  SELECT mbx, 'e2e-3', 'e2e-other@example.test', 'Re: campaign', 'I have no idea what you mean', encode(digest('I have no idea what you mean','sha256'),'hex'), NOW() - INTERVAL '1 minute';
  INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
  SELECT mbx, 'e2e-4', 'e2e-lowconf@example.test', 'Re: campaign', 'Maybe unsubscribe me later', encode(digest('Maybe unsubscribe me later','sha256'),'hex'), NOW() - INTERVAL '1 minute';
END $$;
