package main

import "testing"

func TestParseMediaFolderFilter(t *testing.T) {
	if got, err := parseMediaFolderFilter(""); err != nil || got != nil {
		t.Fatalf("empty folder filter = %#v, error = %v; want nil", got, err)
	}
	if got, err := parseMediaFolderFilter("0"); err != nil || got == nil || *got != 0 {
		t.Fatalf("root folder filter = %#v, error = %v; want pointer to 0", got, err)
	}
	if got, err := parseMediaFolderFilter("12"); err != nil || got == nil || *got != 12 {
		t.Fatalf("folder filter = %#v, error = %v; want pointer to 12", got, err)
	}
	for _, raw := range []string{"-1", "folder"} {
		if _, err := parseMediaFolderFilter(raw); err == nil {
			t.Fatalf("folder filter %q was accepted", raw)
		}
	}
}
