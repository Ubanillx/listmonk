package subimporter

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestCSVImportIgnoresRemovedAttributesColumn(t *testing.T) {
	s := &Session{
		im:       &Importer{stop: make(chan bool, 1), status: Status{Status: StatusImporting}},
		log:      log.New(io.Discard, "", 0),
		subQueue: make(chan SubReq, 2),
		opt:      SessionOpt{Mode: ModeSubscribe},
	}
	path := filepath.Join(t.TempDir(), "customers.csv")
	csv := "email,name,customer_code,attributes\nuser@example.com,\"Last, First\",C001,\"{\"\"age\"\":42}\"\n"
	if err := os.WriteFile(path, []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadCSV(path); err != nil {
		t.Fatal(err)
	}
	row, ok := <-s.subQueue
	if !ok || row.Email != "user@example.com" || row.Name != "Last, First" || row.CustomerCode != "C001" {
		t.Fatalf("unexpected imported row: %+v", row)
	}
	if len(row.Attribs) != 0 {
		t.Fatalf("removed attributes column was imported: %+v", row.Attribs)
	}
}

func TestStopPublishesStoppingBeforeSignal(t *testing.T) {
	im := &Importer{
		stop:   make(chan bool, 1),
		status: Status{Status: StatusImporting},
	}

	im.Stop()

	if got := im.getStatus(); got != StatusStopping {
		t.Fatalf("status after Stop() = %q, want %q", got, StatusStopping)
	}
	select {
	case <-im.stop:
	default:
		t.Fatal("Stop() did not signal the active loader")
	}
}

func TestStopClearsCompletedImport(t *testing.T) {
	im := &Importer{
		stop:   make(chan bool, 1),
		status: Status{Status: StatusFinished, Name: "completed.csv"},
	}

	im.Stop()

	if got := im.getStatus(); got != StatusNone {
		t.Fatalf("status after clearing completed import = %q, want %q", got, StatusNone)
	}
}
