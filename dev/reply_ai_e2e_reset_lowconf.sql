-- Dev-only reset of the low-confidence E2E case (e2e-4) for a re-run.
-- The mock previously matched it as a high-confidence unsubscribe due to a
-- keyword-order bug in the mock itself; the application behaved correctly.
DO $$
DECLARE
  uid INT;
  mbx INT;
  cid INT;
BEGIN
  SELECT id INTO uid FROM users WHERE username='root';
  SELECT id INTO mbx FROM reply_mailboxes WHERE email='ai-e2e@example.test';
  DELETE FROM reply_ai_events WHERE message_key = 'e2e-4';

  SELECT id INTO cid FROM customers WHERE email='e2e-lowconf@example.test' AND owner_user_id=uid;
  UPDATE customers SET status='enabled' WHERE id=cid;
  UPDATE customer_list_memberships SET status='confirmed' WHERE customer_id=cid;

  INSERT INTO reply_ai_events (reply_mailbox_id, message_key, from_email, subject, body, body_hash, received_at)
  SELECT mbx, 'e2e-4', 'e2e-lowconf@example.test', 'Re: campaign', 'Maybe unsubscribe me later',
    encode(digest('Maybe unsubscribe me later','sha256'),'hex'), NOW() - INTERVAL '1 minute';
END $$;
