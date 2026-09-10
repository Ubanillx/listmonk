package subimporter

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zipTestSession returns a session in the running state, which is what
// ExtractZIP requires. The importer is not backed by a database because
// extraction does not touch one.
func zipTestSession() *Session {
	return &Session{
		im:       &Importer{stop: make(chan bool, 1), status: Status{Status: StatusImporting}, done: make(chan struct{})},
		log:      log.New(io.Discard, "", 0),
		subQueue: make(chan SubReq, 2),
		opt:      SessionOpt{Mode: ModeSubscribe},
	}
}

// writeTestZip writes a ZIP containing the given entries and returns its path.
func writeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "import.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	w := zip.NewWriter(f)
	for name, content := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

// tempImportEntries lists extraction and upload temp paths currently present in
// the OS temp directory, so a test can assert that none were leaked.
func tempImportEntries(t *testing.T) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, e := range entries {
		// Both MkdirTemp and CreateTemp use the "listmonk" prefix.
		if strings.HasPrefix(e.Name(), "listmonk") {
			found[e.Name()] = true
		}
	}

	return found
}

func assertNoNewTempEntries(t *testing.T, before map[string]bool) {
	t.Helper()

	for name := range tempImportEntries(t) {
		if !before[name] {
			t.Errorf("temporary path %q was left behind", name)
		}
	}
}

func TestExtractZIPExtractsCSV(t *testing.T) {
	s := zipTestSession()
	path := writeTestZip(t, map[string]string{
		"customers.csv": "email,name\nuser@example.com,User\n",
		"notes.txt":     "ignored",
	})

	dir, files, err := s.ExtractZIP(path, 1)
	if err != nil {
		t.Fatalf("expected extraction to succeed: %v", err)
	}
	// The caller owns the directory on success.
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if len(files) != 1 || files[0] != "customers.csv" {
		t.Fatalf("unexpected extracted files: %v", files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "customers.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "email,name\nuser@example.com,User\n"; string(b) != want {
		t.Fatalf("extracted content = %q, want %q", b, want)
	}
}

// TestExtractZIPRemovesDirectoryOnFailure covers the original leak: the
// extraction directory was created before any validation, and the error paths
// returned without telling the caller its path, so it could never be removed.
func TestExtractZIPRemovesDirectoryOnFailure(t *testing.T) {
	before := tempImportEntries(t)

	s := zipTestSession()
	if _, _, err := s.ExtractZIP(writeTestZip(t, map[string]string{"notes.txt": "no csv here"}), 1); err == nil {
		t.Fatal("expected an archive without CSV entries to be rejected")
	}

	assertNoNewTempEntries(t, before)

	if got := s.im.getStatus(); got != StatusFailed {
		t.Fatalf("status after a failed extraction = %q, want %q", got, StatusFailed)
	}
}

// TestExtractZIPRejectsDeclaredOversizedEntry checks that a ZIP bomb is refused
// from its declared size, before the content reaches disk.
func TestExtractZIPRejectsDeclaredOversizedEntry(t *testing.T) {
	defer func(prev int64) { maxImportEntrySize = prev }(maxImportEntrySize)
	maxImportEntrySize = 1024

	before := tempImportEntries(t)

	s := zipTestSession()
	path := writeTestZip(t, map[string]string{
		"customers.csv": "email\n" + strings.Repeat("user@example.com\n", 200),
	})
	if _, _, err := s.ExtractZIP(path, 1); err == nil {
		t.Fatal("expected an entry over the per-entry limit to be rejected")
	}

	assertNoNewTempEntries(t, before)
}

// TestExtractZIPRejectsArchiveOverBudget checks the total expansion budget, so
// that many entries each under the per-entry limit still cannot fill the disk.
func TestExtractZIPRejectsArchiveOverBudget(t *testing.T) {
	defer func(prev int64) { maxImportUnzippedSize = prev }(maxImportUnzippedSize)
	maxImportUnzippedSize = 2048

	before := tempImportEntries(t)

	s := zipTestSession()
	path := writeTestZip(t, map[string]string{
		"customers.csv": "email\n" + strings.Repeat("user@example.com\n", 200),
	})
	if _, _, err := s.ExtractZIP(path, 1); err == nil {
		t.Fatal("expected an archive over the expansion budget to be rejected")
	}

	assertNoNewTempEntries(t, before)
}

// TestExtractZIPBoundsEntryWithLyingHeader covers an entry whose header
// understates its real content. The write is bounded, so extraction fails
// instead of writing the full payload, and nothing is left behind.
func TestExtractZIPBoundsEntryWithLyingHeader(t *testing.T) {
	defer func(prev int64) { maxImportEntrySize = prev }(maxImportEntrySize)
	maxImportEntrySize = 4096

	before := tempImportEntries(t)

	declared := uint32(16)
	path := writeTestZip(t, map[string]string{
		"customers.csv": "email\n" + strings.Repeat("user@example.com\n", 400),
	})
	patchCentralDirectoryDeclaredSize(t, path, declared)

	s := zipTestSession()
	if _, _, err := s.ExtractZIP(path, 1); err == nil {
		t.Fatal("expected an entry with an inconsistent size header to be rejected")
	}

	assertNoNewTempEntries(t, before)
}

// patchCentralDirectoryDeclaredSize rewrites the uncompressed-size field of the
// first central directory entry, producing an archive whose header claims a
// smaller content than the compressed data actually holds.
func patchCentralDirectoryDeclaredSize(t *testing.T, path string, size uint32) {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Central file header signature, then: version made by (2), version needed
	// (2), flags (2), method (2), time (2), date (2), CRC (4), compressed size
	// (4), uncompressed size (4).
	i := bytes.Index(b, []byte{'P', 'K', 0x01, 0x02})
	if i < 0 {
		t.Fatal("central directory header not found")
	}
	binary.LittleEndian.PutUint32(b[i+24:i+28], size)

	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

// TestDoneSignalsTerminalStatus covers the completion signal that import
// handlers wait on to release their temporary files. A loader that fails does
// not close its queue, which leaves Start blocked forever, so the status
// transition has to be the signal.
func TestDoneSignalsTerminalStatus(t *testing.T) {
	im := New(Options{}, nil, nil)

	// An idle importer has nothing in flight.
	select {
	case <-im.Done():
	default:
		t.Fatal("expected an idle importer to report completion")
	}

	// A session in flight reports nothing until it reaches a terminal state.
	im.Lock()
	im.status = Status{Status: StatusImporting}
	im.done = make(chan struct{})
	im.Unlock()

	select {
	case <-im.Done():
		t.Fatal("expected a running import to report no completion")
	default:
	}

	// Cancellation is not terminal: the session is still draining.
	im.setStatus(StatusStopping)
	select {
	case <-im.Done():
		t.Fatal("expected a stopping import to report no completion")
	default:
	}

	im.setStatus(StatusFinished)
	select {
	case <-im.Done():
	case <-time.After(time.Second):
		t.Fatal("expected a finished import to report completion")
	}
}

// TestDoneIsNilSafe covers a zero-value Importer, which has no session.
func TestDoneIsNilSafe(t *testing.T) {
	im := &Importer{}

	select {
	case <-im.Done():
	default:
		t.Fatal("expected a zero-value importer to report completion")
	}
	im.setStatus(StatusFailed)
}

// TestNewSessionPublishesFreshDoneSignal covers a waiter from a finished session
// not waking up for the next one.
func TestNewSessionPublishesFreshDoneSignal(t *testing.T) {
	im := New(Options{}, nil, nil)

	sess, err := im.NewSession(SessionOpt{Mode: ModeSubscribe, CustomerListIDs: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if sess == nil {
		t.Fatal("expected a session")
	}

	select {
	case <-im.Done():
		t.Fatal("expected a new session to report no completion")
	default:
	}

	im.setStatus(StatusFinished)
	select {
	case <-im.Done():
	case <-time.After(time.Second):
		t.Fatal("expected the session to report completion")
	}
}
