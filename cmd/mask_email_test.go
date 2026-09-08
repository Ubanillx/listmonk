package main

import "testing"

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"typical", "liuxin@gmail.com", "liuxxx@gmail.com"},
		{"long local part", "john.doe@example.com", "johxxxxx@example.com"},
		{"exactly three chars", "liu@gmail.com", "xxx@gmail.com"},
		{"two chars", "ab@example.com", "xx@example.com"},
		{"one char", "a@example.com", "x@example.com"},
		{"empty local part is preserved as-is", "@example.com", "@example.com"},
		{"no domain stays unchanged", "notanemail", "notanemail"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskEmail(c.in); got != c.want {
				t.Fatalf("maskEmail(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
