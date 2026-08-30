package core

import (
	"bytes"
	"testing"
)

func TestRewriteProtectedMediaReferences(t *testing.T) {
	body := []byte(`{"html":"<img src=\"https://host/api/media/file/12/logo.png?x=1\"><img src='/api/media/file/12/logo.png'>"}`)
	got := rewriteProtectedMediaReferences(body, map[int]int{12: 99})
	want := []byte(`{"html":"<img src=\"https://host/api/media/file/99/logo.png?x=1\"><img src='/api/media/file/99/logo.png'>"}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("rewritten body = %q, want %q", got, want)
	}
}

func TestRewriteProtectedMediaReferencesLeavesLegacyURLs(t *testing.T) {
	body := []byte(`<img src="/uploads/logo.png"><img src="/api/media/file/logo.png">`)
	got := rewriteProtectedMediaReferences(body, map[int]int{12: 99})
	if !bytes.Equal(got, body) {
		t.Fatalf("legacy media URLs must remain unchanged: %q", got)
	}
}

func TestRewriteMediaReferencesRewritesKnownLegacyRoutes(t *testing.T) {
	body := []byte(`<p><img src="/uploads/logo%20one.png?size=large"><img src="http://localhost:9173/api/media/file/logo%20one.png#top"><a href="/uploads/logo%20one.png">same file</a></p>`)
	got := rewriteMediaReferences(body, map[int]int{12: 99}, map[int]string{12: "logo one.png"})
	want := []byte(`<p><img src="/api/media/file/99/logo%20one.png?size=large"><img src="/api/media/file/99/logo%20one.png#top"><a href="/api/media/file/99/logo%20one.png">same file</a></p>`)
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy media references = %q, want %q", got, want)
	}
}

func TestRewriteMediaReferencesLeavesExternalAndAmbiguousRoutes(t *testing.T) {
	body := []byte(`<img src="https://cdn.example/uploads/logo.png"><img src="/uploads/shared.png">`)
	got := rewriteMediaReferences(body, map[int]int{12: 99, 13: 100, 14: 101}, map[int]string{12: "logo.png", 13: "shared.png", 14: "shared.png"})
	if !bytes.Equal(got, body) {
		t.Fatalf("external or ambiguous media references must remain unchanged: %q", got)
	}
}
