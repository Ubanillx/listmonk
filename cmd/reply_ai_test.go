package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-message"
)

// parseReply parses a raw RFC-5322 message into an entity for unit tests.
func parseReply(t *testing.T, raw string) *message.Entity {
	t.Helper()
	entity, err := message.Read(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("message.Read: %v", err)
	}
	return entity
}

func TestIsAutomatedInboundReply(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			"human plain reply is actionable",
			"From: Customer <c@example.com>\r\nTo: list@company.example\r\nSubject: Re: newsletter\r\nMessage-ID: <m1@example.com>\r\n\r\nPlease unsubscribe me.",
			false,
		},
		{
			"auto-submitted reply is skipped",
			"From: Mailer <auto@example.com>\r\nAuto-Submitted: auto-replied\r\nSubject: out of office\r\n\r\naway",
			true,
		},
		{
			"explicit auto-submitted no is kept",
			"From: c@example.com\r\nAuto-Submitted: no\r\nSubject: Re: hi\r\n\r\nplease remove me",
			false,
		},
		{
			"list bulk precedence is skipped",
			"From: list@example.com\r\nPrecedence: bulk\r\nSubject: digest\r\n\r\ncontent",
			true,
		},
		{
			"listmonk forwarded copy is skipped",
			"From: a@example.com\r\nX-Listmonk-Forwarded-Reply: true\r\nSubject: Re: hi\r\n\r\nbody",
			true,
		},
		{
			"mailer daemon is skipped",
			"From: Mailer-Daemon@example.com\r\nSubject: Undelivered Mail\r\n\r\nbounce",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAutomatedInboundReply(parseReply(t, c.raw)); got != c.want {
				t.Fatalf("isAutomatedInboundReply = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNormalizeReplyAddress(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Customer <Cust@Example.com>", "cust@example.com"},
		{"cust@example.com", "cust@example.com"},
		{"not-an-address", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeReplyAddress(c.in); got != c.want {
			t.Fatalf("normalizeReplyAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizedReplyText(t *testing.T) {
	raw := "From: c@example.com\r\nSubject: Re: hi\r\n\r\n" +
		"Please stop emailing me.\r\n\r\n" +
		"> On Mon, quoted history\r\n" +
		"> more quoted\r\n" +
		"-----Original Message-----\r\nfull original body\r\n"
	got := normalizedReplyText(parseReply(t, raw))
	if !strings.Contains(got, "Please stop emailing me.") {
		t.Fatalf("expected latest reply text, got %q", got)
	}
	if strings.Contains(got, "quoted history") || strings.Contains(got, "original body") {
		t.Fatalf("quoted history and original message must be stripped, got %q", got)
	}

	// HTML bodies are stripped of markup and unescaped.
	htmlRaw := "From: c@example.com\r\nContent-Type: text/html\r\n\r\n" +
		"<html><body><p>Unsubscribe me &amp; now</p><div>keep</div></body></html>"
	htmlGot := normalizedReplyText(parseReply(t, htmlRaw))
	if !strings.Contains(htmlGot, "Unsubscribe me & now") || strings.Contains(htmlGot, "<p>") {
		t.Fatalf("html normalization failed, got %q", htmlGot)
	}
}

func TestReplyAIErrorText(t *testing.T) {
	long := strings.Repeat("x", 2000)
	got := replyAIErrorText(fmt.Errorf("%s", long))
	if len([]rune(got)) > 500 {
		t.Fatalf("replyAIErrorText did not truncate, len=%d", len([]rune(got)))
	}
	if got := replyAIErrorText(nil); got != "" {
		t.Fatalf("replyAIErrorText(nil) = %q, want empty", got)
	}
}
