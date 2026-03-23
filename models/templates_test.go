package models

import (
	"testing"

	null "gopkg.in/volatiletech/null.v6"
)

func TestTemplateCloneCopiesSourceFields(t *testing.T) {
	src := Template{
		Name:       "Source",
		Type:       TemplateTypeCampaignVisual,
		Subject:    "ignored",
		Body:       "<h1>Hello</h1>",
		BodySource: null.StringFrom(`{"root":true}`),
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
