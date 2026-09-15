package models

import (
	"net/textproto"
	"testing"
)

func TestSetListUnsubscribeHeaders(t *testing.T) {
	h := textproto.MIMEHeader{}
	SetListUnsubscribeHeaders(h, "https://mail.example/subscription/campaign/customer")

	if got, want := h.Get(EmailHeaderListUnsubscribe), "<https://mail.example/subscription/campaign/customer>"; got != want {
		t.Fatalf("List-Unsubscribe = %q, want %q", got, want)
	}
	if got, want := h.Get(EmailHeaderListUnsubscribePost), EmailHeaderListUnsubscribePostValue; got != want {
		t.Fatalf("List-Unsubscribe-Post = %q, want %q", got, want)
	}
	if got := h.Get("CustomerList-Unsubscribe"); got != "" {
		t.Fatalf("legacy CustomerList-Unsubscribe header was set to %q", got)
	}
}
