package subimporter

import "testing"

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
