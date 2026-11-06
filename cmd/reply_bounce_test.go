package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-message"
	"github.com/knadh/listmonk/internal/bounce/mailbox"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
)

func seedReplyBounceMailbox(t *testing.T, a *App) int {
	t.Helper()
	if _, err := a.db.Exec(`INSERT INTO roles (id, name, permissions) VALUES (1, 'Super Admin', '{}')
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO users (id, username, email, name, user_role_id, status)
		VALUES (501, 'bounce-user', 'bounce@example.com', 'Bounce User', 1, 'enabled')`); err != nil {
		t.Fatal(err)
	}
	var mailboxID int
	if err := a.db.Get(&mailboxID, `INSERT INTO reply_mailboxes
		(user_id, email, username, imap_host, imap_port, imap_tls, folder, password, status, verified_at, ai_enabled, bounce_enabled, scan_enabled)
		VALUES (501, 'replies@example.com', 'replies@example.com', 'imap.example.com', 993, TRUE, 'INBOX', 'secret', 'active', NOW(), FALSE, TRUE, TRUE)
		RETURNING id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE settings SET value='true'::JSONB WHERE key='bounce.enabled'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE settings SET value='{"soft":{"count":2,"action":"none"},"hard":{"count":1,"action":"blocklist"},"complaint":{"count":1,"action":"blocklist"}}'::JSONB WHERE key='bounce.actions'`); err != nil {
		t.Fatal(err)
	}
	return mailboxID
}

func seedReplyBounceDelivery(t *testing.T, a *App, mailboxID int, email string) (int, int) {
	t.Helper()
	var customerID int
	if err := a.db.Get(&customerID, `INSERT INTO customers (uuid, email, name, owner_user_id)
		VALUES (gen_random_uuid(), $1, 'Customer', 501) RETURNING id`, email); err != nil {
		t.Fatal(err)
	}
	var campaignID int
	if err := a.db.Get(&campaignID, `INSERT INTO campaigns
		(uuid, name, subject, from_email, body, messenger, owner_user_id, reply_mailbox_id)
		VALUES (gen_random_uuid(), 'Bounce campaign', 'Subject', 'sender@example.com', 'body', 'email', 501, $1)
		RETURNING id`, mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO campaign_recipients (campaign_id, customer_id, status, email_snapshot)
		VALUES ($1, $2, 'sent', $3)`, campaignID, customerID, email); err != nil {
		t.Fatal(err)
	}
	return campaignID, customerID
}

func replyBounceInput(mailboxID, campaignID, customerID int, email, key, bounceType string, userID int) core.ReplyBounceInput {
	return core.ReplyBounceInput{
		MailboxID:  mailboxID,
		MessageKey: key,
		Recipient:  email,
		Type:       bounceType,
		Status:     "5.1.1",
		Diagnostic: "smtp; 550 user unknown",
		MessageID:  "<dsn-" + key + "@example.com>",
		Sender:     "MAILER-DAEMON@example.com",
		Subject:    "Undelivered Mail",
		Delivery: core.ReplyAIDelivery{
			CampaignID: campaignID,
			CustomerID: customerID,
			UserID:     userID,
		},
	}
}

func TestReplyBounceHardBlocklistsPrivateCustomer(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "missing@example.com")
	input := replyBounceInput(mailboxID, campaignID, customerID, "missing@example.com", "hard-1", "hard", 501)
	input.Subject = strings.Repeat("退信", 400)
	input.Diagnostic = strings.Repeat("无法投递 ", 400) + "missing@example.com"

	result, err := a.core.RecordReplyBounce(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "blocklisted" || result.ReasonCode != "blocklisted" || result.ActionCount != 1 {
		t.Fatalf("result = %#v", result)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "blocklisted" {
		t.Fatalf("customer status=%q err=%v", status, err)
	}
	var bounces int
	if err := a.db.Get(&bounces, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1 AND type='hard'`, customerID); err != nil || bounces != 1 {
		t.Fatalf("bounces=%d err=%v", bounces, err)
	}
	var outcome, reason string
	if err := a.db.Get(&outcome, `SELECT outcome FROM reply_bounce_events WHERE reply_mailbox_id=$1`, mailboxID); err != nil || outcome != "blocklisted" {
		t.Fatalf("ledger outcome=%q err=%v", outcome, err)
	}
	if err := a.db.Get(&reason, `SELECT reason_code FROM reply_bounce_events WHERE reply_mailbox_id=$1`, mailboxID); err != nil || reason != "blocklisted" {
		t.Fatalf("ledger reason=%q err=%v", reason, err)
	}

	duplicate, err := a.core.RecordReplyBounce(input)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate {
		t.Fatalf("duplicate was not reported: %#v", duplicate)
	}
	if err := a.db.Get(&bounces, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1 AND type='hard'`, customerID); err != nil || bounces != 1 {
		t.Fatalf("duplicate inserted another bounce: %d err=%v", bounces, err)
	}
}

func TestReplyBounceSoftDoesNotBlocklist(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "soft@example.com")
	input := replyBounceInput(mailboxID, campaignID, customerID, "soft@example.com", "soft-1", "soft", 501)
	input.Status = "4.2.2"

	result, err := a.core.RecordReplyBounce(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "recorded" || result.ReasonCode != "soft_recorded" {
		t.Fatalf("result = %#v", result)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "enabled" {
		t.Fatalf("soft bounce changed customer status: %q err=%v", status, err)
	}
	var bounces int
	if err := a.db.Get(&bounces, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1 AND type='soft'`, customerID); err != nil || bounces != 1 {
		t.Fatalf("soft bounces=%d err=%v", bounces, err)
	}
}

func TestReplyBounceDeleteActionOnlyRecords(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	if _, err := a.db.Exec(`UPDATE settings SET value='{"soft":{"count":2,"action":"none"},"hard":{"count":1,"action":"delete"},"complaint":{"count":1,"action":"blocklist"}}'::JSONB WHERE key='bounce.actions'`); err != nil {
		t.Fatal(err)
	}
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "delete@example.com")
	input := replyBounceInput(mailboxID, campaignID, customerID, "delete@example.com", "delete-1", "hard", 501)

	result, err := a.core.RecordReplyBounce(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "recorded" || result.ReasonCode != "action_not_supported" {
		t.Fatalf("result = %#v", result)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "enabled" {
		t.Fatalf("delete action changed customer status: %q err=%v", status, err)
	}
}

func TestReplyBounceRequiresMailboxOwnerWorkspace(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "foreign@example.com")
	input := replyBounceInput(mailboxID, campaignID, customerID, "foreign@example.com", "foreign-1", "hard", 999)

	result, err := a.core.RecordReplyBounce(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "ignored" || result.ReasonCode != "workspace_not_writable" {
		t.Fatalf("result = %#v", result)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "enabled" {
		t.Fatalf("workspace guard changed customer status: %q err=%v", status, err)
	}
	var bounces int
	if err := a.db.Get(&bounces, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1`, customerID); err != nil || bounces != 0 {
		t.Fatalf("workspace guard inserted a bounce: %d err=%v", bounces, err)
	}
}

func TestIngestReplyBounceDSNMatchesHeadersAndBlocklists(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "branch@example.com")
	var campaignUUID, customerUUID string
	if err := a.db.Get(&campaignUUID, `SELECT uuid::TEXT FROM campaigns WHERE id=$1`, campaignID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&customerUUID, `SELECT uuid::TEXT FROM customers WHERE id=$1`, customerID); err != nil {
		t.Fatal(err)
	}
	originalID := models.CampaignReplyMessageID(campaignUUID, customerID, 0)
	raw := fmt.Sprintf(`From: Mail Delivery System <MAILER-DAEMON@example.com>
To: replies@example.com
Subject: Undelivered Mail Returned to Sender
Message-ID: <dsn-branch@example.com>
MIME-Version: 1.0
Content-Type: multipart/report; report-type=delivery-status; boundary="BOUND"

--BOUND
Content-Type: text/plain; charset=utf-8

Recipient failed.
--BOUND
Content-Type: message/delivery-status

Reporting-MTA: dns; mx.example.com

Final-Recipient: rfc822; branch@example.com
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550 5.1.1 User unknown
--BOUND
Content-Type: message/rfc822

From: sender@example.com
To: branch@example.com
Message-ID: %s
X-Listmonk-Customer: %s
X-Listmonk-Campaign: %s
Subject: Campaign

body
--BOUND--
`, originalID, customerUUID, campaignUUID)

	entity, err := message.Read(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := mailbox.ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !dsn.IsBounce || len(dsn.Recipients) != 1 {
		t.Fatalf("unexpected parsed DSN: %#v", dsn)
	}
	source := replyAIMailboxSource{
		ID:           mailboxID,
		UserID:       501,
		Email:        "replies@example.com",
		BounceActive: true,
	}
	if err := a.ingestReplyBounceDSN(source, dsn, entity, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "blocklisted" {
		t.Fatalf("customer status=%q err=%v", status, err)
	}
	var outcome string
	if err := a.db.Get(&outcome, `SELECT outcome FROM reply_bounce_events WHERE reply_mailbox_id=$1 AND recipient=$2`, mailboxID, "branch@example.com"); err != nil || outcome != "blocklisted" {
		t.Fatalf("ledger outcome=%q err=%v", outcome, err)
	}
}

func TestReplyBounceThresholdWaitsForSecondHardBounce(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	if _, err := a.db.Exec(`UPDATE settings SET value='{"soft":{"count":2,"action":"none"},"hard":{"count":2,"action":"blocklist"},"complaint":{"count":1,"action":"blocklist"}}'::JSONB WHERE key='bounce.actions'`); err != nil {
		t.Fatal(err)
	}
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "threshold@example.com")

	first, err := a.core.RecordReplyBounce(replyBounceInput(mailboxID, campaignID, customerID, "threshold@example.com", "threshold-1", "hard", 501))
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != "recorded" || first.ActionCount != 1 {
		t.Fatalf("first result = %#v", first)
	}
	var status string
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "enabled" {
		t.Fatalf("first bounce changed status: %q err=%v", status, err)
	}

	second, err := a.core.RecordReplyBounce(replyBounceInput(mailboxID, campaignID, customerID, "threshold@example.com", "threshold-2", "hard", 501))
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != "blocklisted" || second.ActionCount != 2 {
		t.Fatalf("second result = %#v", second)
	}
	if err := a.db.Get(&status, `SELECT status FROM customers WHERE id=$1`, customerID); err != nil || status != "blocklisted" {
		t.Fatalf("second bounce status: %q err=%v", status, err)
	}
}

func TestReplyBounceAlreadyBlocklistedOnlyRecords(t *testing.T) {
	a := newReplyMailboxDeleteTestApp(t)
	mailboxID := seedReplyBounceMailbox(t, a)
	campaignID, customerID := seedReplyBounceDelivery(t, a, mailboxID, "already@example.com")
	if _, err := a.db.Exec(`UPDATE customers SET status='blocklisted' WHERE id=$1`, customerID); err != nil {
		t.Fatal(err)
	}
	result, err := a.core.RecordReplyBounce(replyBounceInput(mailboxID, campaignID, customerID, "already@example.com", "already-1", "hard", 501))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "ignored" || result.ReasonCode != "already_blocklisted" {
		t.Fatalf("result = %#v", result)
	}
	var bounces int
	if err := a.db.Get(&bounces, `SELECT COUNT(*) FROM bounces WHERE customer_id=$1`, customerID); err != nil || bounces != 0 {
		t.Fatalf("already blocklisted inserted a bounce: %d err=%v", bounces, err)
	}
}
