package mailbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

const dsnFixture = "From: Mailer <mailer@example.com>\r\nSubject: =?UTF-8?B?6YCA5L+h?=\r\nDate: Tue, 8 Sep 2026 10:00:00 +0800\r\nContent-Type: multipart/report; boundary=outer; report-type=delivery-status\r\n\r\n--outer\r\nContent-Type: text/plain\r\n\r\nDelivery failed.\r\n--outer\r\nContent-Type: message/delivery-status\r\n\r\nReporting-MTA: dns; mx.example.com\r\n\r\nOriginal-Recipient: rfc822; original@example.com\r\nFinal-Recipient: rfc822; failed@example.com\r\nStatus: 5.1.1\r\nDiagnostic-Code: smtp; 550\r\n user unknown\r\n\r\nFinal-Recipient: rfc822; full@example.com\r\nStatus: 4.2.2\r\nDiagnostic-Code: smtp; 450 mailbox full\r\n--outer\r\nContent-Type: message/rfc822\r\n\r\nFrom: Original Sender <original@example.com>\r\nTo: innocent@example.com\r\nSubject: Original subject\r\n\r\nHello\r\n--outer--\r\n"

func TestPreviewDSN(t *testing.T) {
	m, err := parsePreview([]byte(dsnFixture))
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "Mailer <mailer@example.com>" || m.Subject != "退信" || m.Date == "" || m.DateSource != "date" {
		t.Fatalf("wrong outer metadata: %+v", m)
	}
	if len(m.Recipients) != 2 || m.Recipients[0].Address != "failed@example.com" || m.Recipients[0].Reason != "smtp; 550 user unknown" || m.Recipients[1].Status != "4.2.2" {
		t.Fatalf("wrong recipients: %+v", m.Recipients)
	}
	if !m.IsBounce || m.BounceType != "hard" {
		t.Fatalf("wrong classification: %+v", m)
	}
}

func TestPreviewFallbacks(t *testing.T) {
	for _, tc := range []struct{ name, raw, address, source string }{
		{"plain mail", "From: user@example.com\r\nTo: recipient@example.com\r\nSubject: Hello\r\n\r\nHello user@example.com", "", ""},
		{"x failed", "X-Failed-Recipients: failed@example.com\r\n\r\nFailure", "failed@example.com", "x-failed-recipients"},
		{"encoded body", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("Original-Recipient: rfc822; old@example.com\nFinal-Recipient: rfc822; new@example.com")), "new@example.com", "final-recipient"},
		{"inferred", "Subject: Failed\r\n\r\nDelivery failed for user@example.com", "user@example.com", "body_inferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := parsePreview([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if tc.address == "" {
				if m.IsBounce {
					t.Fatal("ordinary mail classified as bounce")
				}
				return
			}
			if len(m.Recipients) != 1 || m.Recipients[0].Address != tc.address || m.Recipients[0].Source != tc.source {
				t.Fatalf("%+v", m)
			}
		})
	}
}

func TestPreviewReceivedDateAndMalformed(t *testing.T) {
	m, err := parsePreview([]byte("Received: by mx.example.com; Tue, 8 Sep 2026 11:00:00 +0800\r\n" + dsnFixture))
	if err != nil || m.DateSource != "received" || !strings.Contains(m.Date, "11:00:00") {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err = parsePreview([]byte("not a mail header\r\n\r\nbody")); err == nil {
		t.Fatal("malformed header accepted")
	}
}
