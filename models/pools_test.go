package models

import "testing"

func TestMaskPoolEmail(t *testing.T) {
	for in, want := range map[string]string{
		"liuxin@gmail.com": "liuxxx@gmail.com",
		"ab@example.com":   "xx@example.com",
		"":                 "",
		"invalid":          "invalid",
	} {
		if got := MaskPoolEmail(in); got != want {
			t.Errorf("MaskPoolEmail(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestSafePoolContactDoesNotExposePersonName(t *testing.T) {
	p := PoolContact{ID: 9, CustomerCode: "C-9", CompanyName: "Acme", Email: "contact@example.com", Name: "Jane Doe", Status: "active"}
	got := p.Safe()
	if got.Name != "" || got.Email != "conxxxx@example.com" || got.CustomerCode != "C-9" || got.CompanyName != "Acme" {
		t.Fatalf("unexpected safe pool contact: %+v", got)
	}
}
