package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveTempPaths covers the cleanup that releases an import's uploaded copy
// and extracted directory. Empty and already-missing paths must be tolerated:
// the caller passes them for every import whether or not a ZIP was involved.
func TestRemoveTempPaths(t *testing.T) {
	dir := t.TempDir()
	logger := log.New(io.Discard, "", 0)

	upload := filepath.Join(dir, "upload.csv")
	if err := os.WriteFile(upload, []byte("email\nuser@example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	extracted := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(extracted, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extracted, "customers.csv"), []byte("email\n"), 0600); err != nil {
		t.Fatal(err)
	}

	removeTempPaths(logger, upload, extracted, "", filepath.Join(dir, "missing"))

	for _, p := range []string{upload, extracted} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat error: %v", p, err)
		}
	}
}
