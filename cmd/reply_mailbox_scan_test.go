package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestReplyMailboxScanSwitchPreservesConsumersAndConnection(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	if _, err := a.db.Exec(`UPDATE users SET status='enabled' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id := createReplyMailboxForTest(t, a, 0, 993)
	if _, err := a.db.Exec(`UPDATE settings SET value=jsonb_set(value,'{enabled}','true') WHERE key='reply_ai';
		UPDATE settings SET value='true'::JSONB WHERE key='bounce.enabled'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE reply_mailboxes SET status='active',verified_at=NOW(),bounce_enabled=TRUE WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		body := fmt.Sprintf(`{"email":"reply@example.com","scan_enabled":%t,"ai_enabled":true,"bounce_enabled":true,
			"username":"reply-user","imap_host":"127.0.0.1","imap_port":993,"imap_tls":false,"folder":"INBOX"}`, enabled)
		c, rec := replyMailboxConfigContext(1, 0, id, body)
		if err := a.UpdateReplyMailbox(c); err != nil {
			t.Fatal(err)
		}
		var out struct {
			Data models.ReplyMailbox `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Data.ScanEnabled != enabled || !out.Data.AIEnabled || !out.Data.BounceEnabled || !out.Data.HasPassword {
			t.Fatalf("scan switch lost child settings or password: %+v", out.Data)
		}
		var sources []replyAIMailboxSource
		if err := a.queries.GetReplyAIMailboxes.Select(&sources); err != nil {
			t.Fatal(err)
		}
		if !enabled && len(sources) != 0 {
			t.Fatal("paused mailbox is still selected by the background scanner")
		}
		if enabled && (len(sources) != 1 || sources[0].ID != id || !sources[0].AIActive || !sources[0].BounceActive) {
			t.Fatalf("resuming detection did not restore both consumers: %+v", sources)
		}
	}
}

func TestReplyMailboxScanPauseBlocksInFlightBounce(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	id := seedReplyBounceMailbox(t, a)
	campaign, customer := seedReplyBounceDelivery(t, a, id, "paused@example.com")
	if _, err := a.db.Exec(`UPDATE reply_mailboxes SET scan_enabled=FALSE WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	result, err := a.core.RecordReplyBounce(replyBounceInput(id, campaign, customer, "paused@example.com", "paused", "hard", 501))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "ignored" {
		t.Fatalf("paused mailbox processed an in-flight bounce: %+v", result)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customer); err != nil || status != "enabled" {
		t.Fatalf("pause did not prevent blocklisting: status=%s err=%v", status, err)
	}
}

func TestReplyMailboxScanConnectionBeforeChoosingConsumers(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	missingHost, _ := replyMailboxConfigContext(1, 0, 0, `{"email":"missing-host@example.com","scan_enabled":true,"password":"test-only-password"}`)
	if err := a.CreateReplyMailbox(missingHost); err == nil {
		t.Fatal("explicit detection silently guessed the receiving server")
	}
	c, rec := replyMailboxConfigContext(1, 0, 0, `{"email":"connection@example.com","scan_enabled":true,
		"ai_enabled":false,"bounce_enabled":false,"imap_host":"imap.example.com","password":"test-only-password"}`)
	if err := a.CreateReplyMailbox(c); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data models.ReplyMailbox `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Data.ScanEnabled || out.Data.Status != "pending" || !out.Data.HasPassword || out.Data.AIEnabled || out.Data.BounceEnabled {
		t.Fatalf("receiving setup should be independent of consumers: %+v", out.Data)
	}
}
