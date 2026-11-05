package mailbox

import (
	"strings"
	"testing"
)

func TestParseDSNHardRecipientAndOriginalHeaders(t *testing.T) {
	raw := strings.Join([]string{
		"From: Mail Delivery System <MAILER-DAEMON@example.com>",
		"To: replies@example.com",
		"Subject: Undelivered Mail Returned to Sender",
		"Message-ID: <dsn-1@example.com>",
		"Date: Mon, 02 Jan 2006 15:04:05 -0700",
		"MIME-Version: 1.0",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="BOUND"`,
		"",
		"--BOUND",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"The following recipient failed.",
		"--BOUND",
		"Content-Type: message/delivery-status",
		"",
		"Reporting-MTA: dns; mx.example.com",
		"",
		"Final-Recipient: rfc822; Missing@Example.com",
		"Original-Recipient: rfc822; original@example.com",
		"Action: failed",
		"Status: 5.1.1",
		"Diagnostic-Code: smtp; 550 5.1.1 User unknown",
		"--BOUND",
		"Content-Type: message/rfc822",
		"",
		"From: sender@example.com",
		"To: missing@example.com",
		"Message-ID: <lm.11111111-1111-1111-1111-111111111111.c42@reply.listmonk>",
		"X-Listmonk-Customer: 22222222-2222-2222-2222-222222222222",
		"X-Listmonk-Campaign: 11111111-1111-1111-1111-111111111111",
		"Subject: Campaign",
		"",
		"body",
		"--BOUND--",
		"",
	}, "\r\n")

	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if !dsn.IsBounce {
		t.Fatal("expected a bounce")
	}
	if dsn.MessageID != "<dsn-1@example.com>" {
		t.Fatalf("message id = %q", dsn.MessageID)
	}
	if len(dsn.Recipients) != 1 {
		t.Fatalf("recipients = %#v", dsn.Recipients)
	}
	got := dsn.Recipients[0]
	if got.Address != "missing@example.com" || got.Source != "final-recipient" ||
		got.Status != "5.1.1" || got.BounceClass != "hard" {
		t.Fatalf("recipient = %#v", got)
	}
	if dsn.OriginalMessageID != "<lm.11111111-1111-1111-1111-111111111111.c42@reply.listmonk>" {
		t.Fatalf("original message id = %q", dsn.OriginalMessageID)
	}
	if dsn.OriginalHeaders["X-Listmonk-Customer"] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("customer header = %q", dsn.OriginalHeaders["X-Listmonk-Customer"])
	}
	if dsn.OriginalHeaders["X-Listmonk-Campaign"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("campaign header = %q", dsn.OriginalHeaders["X-Listmonk-Campaign"])
	}
}

func TestParseDSNPerRecipientStatus(t *testing.T) {
	raw := strings.Join([]string{
		"From: MAILER-DAEMON@example.com",
		"Subject: Delivery Status Notification",
		"Message-ID: <dsn-2@example.com>",
		"Content-Type: message/delivery-status",
		"",
		"Reporting-MTA: dns; mx.example.com",
		"",
		"Final-Recipient: rfc822; hard@example.com",
		"Action: failed",
		"Status: 5.2.1",
		"",
		"Final-Recipient: rfc822; soft@example.com",
		"Action: delayed",
		"Status: 4.2.2",
		"",
	}, "\r\n")

	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !dsn.IsBounce || len(dsn.Recipients) != 2 {
		t.Fatalf("dsn = %#v", dsn)
	}
	if dsn.Recipients[0].Address != "hard@example.com" || dsn.Recipients[0].BounceClass != "hard" {
		t.Fatalf("first = %#v", dsn.Recipients[0])
	}
	if dsn.Recipients[1].Address != "soft@example.com" || dsn.Recipients[1].BounceClass != "soft" {
		t.Fatalf("second = %#v", dsn.Recipients[1])
	}
}

func TestParseDSNRejectsDirectStatusInsideMixed(t *testing.T) {
	raw := strings.Join([]string{
		"From: human@example.com",
		"Subject: Fwd: Undelivered",
		`Content-Type: multipart/mixed; boundary="MIXED"`,
		"",
		"--MIXED",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"forwarded",
		"--MIXED",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; missing@example.com",
		"Status: 5.1.1",
		"--MIXED--",
		"",
	}, "\r\n")
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dsn.IsBounce {
		t.Fatalf("a delivery-status part inside multipart/mixed must not be an outer DSN: %#v", dsn)
	}
}

func TestParseDSNRejectsForwardedReport(t *testing.T) {
	raw := strings.Join([]string{
		"From: human@example.com",
		"Subject: Fwd: Undelivered Mail",
		`Content-Type: multipart/mixed; boundary="OUT"`,
		"",
		"--OUT",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"See the attached bounce.",
		"--OUT",
		"Content-Type: message/rfc822",
		"",
		"From: MAILER-DAEMON@example.com",
		"Subject: Undelivered Mail",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="IN"`,
		"",
		"--IN",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; missing@example.com",
		"Status: 5.1.1",
		"--IN--",
		"--OUT--",
		"",
	}, "\r\n")

	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dsn.IsBounce {
		t.Fatalf("forwarded report must not be treated as a bounce: %#v", dsn)
	}
}

func TestParseDSNRejectsPlainTextFailure(t *testing.T) {
	raw := "From: human@example.com\r\nSubject: problem\r\n\r\nuser unknown, please check\r\n"
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dsn.IsBounce || len(dsn.Recipients) != 0 {
		t.Fatalf("plain text must not be a DSN: %#v", dsn)
	}
}

func TestParseDSNRejectsFeedbackReport(t *testing.T) {
	raw := strings.Join([]string{
		"From: reports@example.com",
		`Content-Type: multipart/report; report-type=feedback-report; boundary="B"`,
		"",
		"--B",
		"Content-Type: message/feedback-report",
		"",
		"Feedback-Type: abuse",
		"--B--",
		"",
	}, "\r\n")
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dsn.IsBounce {
		t.Fatal("ARF feedback report must not be a bounce")
	}
}

func TestParseDSNUnknownStatusAndMissingRecipient(t *testing.T) {
	raw := strings.Join([]string{
		"From: MAILER-DAEMON@example.com",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; unknown@example.com",
		"Action: failed",
		"",
	}, "\r\n")
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !dsn.IsBounce || len(dsn.Recipients) != 1 || dsn.Recipients[0].BounceClass != "unknown" {
		t.Fatalf("dsn = %#v", dsn)
	}

	empty := strings.Join([]string{
		"From: MAILER-DAEMON@example.com",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="B"`,
		"",
		"--B",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"no report fields here",
		"--B--",
		"",
	}, "\r\n")
	dsn, err = ParseDSN([]byte(empty))
	if err != nil {
		t.Fatal(err)
	}
	if !dsn.IsBounce || len(dsn.Recipients) != 0 {
		t.Fatalf("expected an empty report, got %#v", dsn)
	}
}

func TestParseDSNTextRFC822Headers(t *testing.T) {
	raw := strings.Join([]string{
		"From: MAILER-DAEMON@example.com",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="B"`,
		"",
		"--B",
		"Content-Type: text/rfc822-headers",
		"",
		"Message-ID: <original@example.com>",
		"X-Listmonk-Customer: customer-uuid",
		"X-Listmonk-Campaign: campaign-uuid",
		"--B",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; missing@example.com",
		"Status: 5.1.1",
		"--B--",
		"",
	}, "\r\n")
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dsn.OriginalMessageID != "<original@example.com>" ||
		dsn.OriginalHeaders["X-Listmonk-Customer"] != "customer-uuid" ||
		dsn.OriginalHeaders["X-Listmonk-Campaign"] != "campaign-uuid" {
		t.Fatalf("original headers = %#v", dsn.OriginalHeaders)
	}
}

func TestParseDSNDuplicateAddressPrefersHard(t *testing.T) {
	raw := strings.Join([]string{
		"From: MAILER-DAEMON@example.com",
		"Content-Type: message/delivery-status",
		"",
		"Final-Recipient: rfc822; same@example.com",
		"Status: 4.2.2",
		"",
		"Final-Recipient: rfc822; same@example.com",
		"Status: 5.1.1",
		"",
	}, "\r\n")
	dsn, err := ParseDSN([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsn.Recipients) != 1 || dsn.Recipients[0].BounceClass != "hard" {
		t.Fatalf("recipients = %#v", dsn.Recipients)
	}
}
