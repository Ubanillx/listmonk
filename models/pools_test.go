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

func TestSafePoolContactKeepsNameAndMasksEmail(t *testing.T) {
	p := PoolContact{ID: 9, CustomerCode: "C-9", Email: "contact@example.com", Name: "Jane Doe", AllocationDepartment: "Sales", Status: "active"}
	got := p.Safe()
	if got.Name != "Jane Doe" || got.Email != "conxxxx@example.com" || got.CustomerCode != "C-9" || got.AllocationDepartment != "Sales" {
		t.Fatalf("unexpected safe pool contact: %+v", got)
	}
}
