package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestReplyIMAPPersistentBatchesAndFolder(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	allowLoopbackMailboxHosts(t)
	messages := make([]string, 201)
	for i := range messages {
		messages[i] = fmt.Sprintf("From: customer@example.com\r\nSubject: reply %d\r\nMessage-ID: <reply-%d@example.com>\r\n\r\nPlease unsubscribe\r\n", i, i)
	}
	port, commands := fakeReplyIMAP(t, messages)
	id := createReplyMailboxForTest(t, a, 0, port)
	source := replyAIMailboxSource{ID: id, Host: "127.0.0.1", Port: port, Username: "reply-user", Password: "stored-test-password", Folder: "Archive"}
	for round, want := range []int{200, 201, 201} {
		if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err != nil {
			t.Fatal(err)
		}
		var events, uid int
		if err := a.db.Get(&events, `SELECT COUNT(*) FROM reply_ai_events WHERE reply_mailbox_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1 AND consumer='ai'`, id); err != nil {
			t.Fatal(err)
		}
		if events != want || uid != want {
			t.Fatalf("round %d: events=%d uid=%d want=%d", round, events, uid, want)
		}
		seen := strings.Join(<-commands, ",")
		if !strings.Contains(seen, "EXAMINE Archive") {
			t.Fatal("configured folder was not selected read-only")
		}
		if round == 2 && strings.Contains(seen, "UID FETCH") {
			t.Fatal("already ingested messages were fetched again")
		}
	}
	// UIDVALIDITY or connection changes must invalidate the old UID position.
	if _, err := a.db.Exec(`UPDATE reply_mailbox_scan_cursors SET uid_validity=41 WHERE mailbox_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(<-commands, ","), "UID FETCH 1 ") {
		t.Fatal("UID validity reset did not backfill")
	}
}

func TestReplyIMAPIngestFailureDoesNotAdvance(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	allowLoopbackMailboxHosts(t)
	port, _ := fakeReplyIMAP(t, []string{"From: customer@example.com\r\nSubject: first\r\n\r\nstop", "From: customer@example.com\r\nSubject: fail\r\n\r\nstop"})
	id := createReplyMailboxForTest(t, a, 0, port)
	source := replyAIMailboxSource{ID: id, Host: "127.0.0.1", Port: port, Username: "reply-user", Password: "stored-test-password"}
	if _, err := a.db.Exec(`CREATE FUNCTION fail_reply_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='fail' THEN RAISE EXCEPTION 'test ingest failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_reply_insert BEFORE INSERT ON reply_ai_events FOR EACH ROW EXECUTE FUNCTION fail_reply_insert();`); err != nil {
		t.Fatal(err)
	}
	if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err == nil {
		t.Fatal("ingestion failure was hidden")
	}
	var uid int
	if err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if uid != 1 {
		t.Fatalf("cursor skipped failed message: %d", uid)
	}
	if _, err := a.db.Exec(`DROP TRIGGER fail_reply_insert ON reply_ai_events`); err != nil {
		t.Fatal(err)
	}
	if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if uid != 2 {
		t.Fatal("failed message was not retried")
	}
}

func TestReplyIMAPMalformedMessageDoesNotBlockLaterMessages(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	allowLoopbackMailboxHosts(t)
	port, _ := fakeReplyIMAP(t, []string{"invalid header without colon\r\n\r\nbody", "From: customer@example.com\r\nSubject: normal\r\n\r\nstop"})
	id := createReplyMailboxForTest(t, a, 0, port)
	source := replyAIMailboxSource{ID: id, Host: "127.0.0.1", Port: port, Username: "reply-user", Password: "stored-test-password"}
	// Even ignored records must be durable before the cursor moves on.
	if _, err := a.db.Exec(`CREATE FUNCTION fail_malformed_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.reason_code='malformed_message' THEN RAISE EXCEPTION 'test ignored insert failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_malformed_insert BEFORE INSERT ON reply_ai_events FOR EACH ROW EXECUTE FUNCTION fail_malformed_insert();`); err != nil {
		t.Fatal(err)
	}
	if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err == nil {
		t.Fatal("ignored event insertion failure was hidden")
	}
	var cursorCount int
	if err := a.db.Get(&cursorCount, `SELECT COUNT(*) FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1`, id); err != nil || cursorCount != 0 {
		t.Fatalf("failed ignored event advanced cursor: count=%d err=%v", cursorCount, err)
	}
	if _, err := a.db.Exec(`DROP TRIGGER fail_malformed_insert ON reply_ai_events`); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err != nil {
			t.Fatal(err)
		}
		var skipped, uid, events int
		if err := a.db.Get(&skipped, `SELECT COUNT(*) FROM reply_ai_events WHERE reply_mailbox_id=$1 AND reason_code='malformed_message' AND status='ignored' AND body=''`, id); err != nil {
			t.Fatal(err)
		}
		if err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err := a.db.Get(&events, `SELECT COUNT(*) FROM reply_ai_events WHERE reply_mailbox_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if skipped != 1 || uid != 2 || events != 2 {
			t.Fatalf("round %d: skipped=%d uid=%d events=%d", round, skipped, uid, events)
		}
	}
}

func TestReplyIMAPOversizedMessageRecorded(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	allowLoopbackMailboxHosts(t)
	port, _ := fakeReplyIMAP(t, []string{strings.Repeat("x", replyAIMaxMessageBytes+1), "From: customer@example.com\r\nSubject: normal\r\n\r\nstop"})
	id := createReplyMailboxForTest(t, a, 0, port)
	source := replyAIMailboxSource{ID: id, Host: "127.0.0.1", Port: port, Username: "reply-user", Password: "stored-test-password"}
	if err := a.scanOneReplyAIMailbox(source, replyAIDefaultScanBudget()); err == nil {
		t.Fatal("size limit was reported as a clean scan")
	}
	var skipped, uid int
	if err := a.db.Get(&skipped, `SELECT COUNT(*) FROM reply_ai_events WHERE reply_mailbox_id=$1 AND reason_code='message_too_large' AND status='ignored'`, id); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || uid != 2 {
		t.Fatalf("skipped=%d uid=%d", skipped, uid)
	}
}
