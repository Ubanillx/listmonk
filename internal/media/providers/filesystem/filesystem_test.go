package filesystem

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPutCreatesMissingUploadDirectory(t *testing.T) {
	uploadPath := filepath.Join(t.TempDir(), "nested", "uploads")
	store, err := New(Opts{UploadPath: uploadPath})
	if err != nil {
		t.Fatalf("create filesystem store: %v", err)
	}

	const filename = "document.txt"
	const contents = "test media contents"
	name, err := store.Put(filename, "text/plain", bytes.NewReader([]byte(contents)))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if name != filename {
		t.Fatalf("returned filename = %q, want %q", name, filename)
	}

	got, err := os.ReadFile(filepath.Join(uploadPath, filename))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(got) != contents {
		t.Fatalf("uploaded contents = %q, want %q", got, contents)
	}
}
