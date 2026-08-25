package models

import (
	"testing"

	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

func TestTemplateCloneCopiesSourceFields(t *testing.T) {
	src := Template{
		Name:       "Source",
		Type:       TemplateTypeCampaignVisual,
		Subject:    "ignored",
		Body:       "<h1>Hello</h1>",
		BodySource: null.StringFrom(`{"root":true}`),
		MediaIDs:   pq.Int64Array{7, 11},
	}

	out := src.Clone("Cloned", "should not apply")

	if out.Name != "Cloned" {
		t.Fatalf("expected cloned name, got %q", out.Name)
	}
	if out.Type != src.Type {
		t.Fatalf("expected type %q, got %q", src.Type, out.Type)
	}
	if out.Body != src.Body {
		t.Fatalf("expected body to be copied")
	}
	if out.BodySource != src.BodySource {
		t.Fatalf("expected body_source to be copied")
	}
	if out.Subject != src.Subject {
		t.Fatalf("expected non-tx subject to stay unchanged, got %q", out.Subject)
	}
	if len(out.MediaIDs) != 2 || out.MediaIDs[0] != 7 || out.MediaIDs[1] != 11 {
		t.Fatalf("expected media IDs to be copied, got %v", out.MediaIDs)
	}

	out.MediaIDs[0] = 99
	if src.MediaIDs[0] != 7 {
		t.Fatal("expected cloned media IDs not to share the source backing array")
	}
}

func TestTemplateCloneOverridesTxSubject(t *testing.T) {
	src := Template{
		Name:    "Source TX",
		Type:    TemplateTypeTx,
		Subject: "Old subject",
		Body:    "<p>Hello</p>",
	}

	out := src.Clone("Cloned TX", "New subject")

	if out.Name != "Cloned TX" {
		t.Fatalf("expected cloned name, got %q", out.Name)
	}
	if out.Subject != "New subject" {
		t.Fatalf("expected tx subject override, got %q", out.Subject)
	}
}
