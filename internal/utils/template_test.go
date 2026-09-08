package utils

import (
	"bytes"
	"testing"
)

func TestGetTplSubject(t *testing.T) {
	body := []byte(`<title data-i18n>  Custom subject  </title><p>body</p>`)
	subject, got := GetTplSubject("fallback", body)
	if subject != "Custom subject" || !bytes.Equal(got, []byte(`<p>body</p>`)) {
		t.Fatalf("unexpected subject/body: %q %q", subject, got)
	}
	if subject, got = GetTplSubject("fallback", []byte("<p>body</p>")); subject != "fallback" || string(got) != "<p>body</p>" {
		t.Fatalf("fallback changed: %q %q", subject, got)
	}
}
