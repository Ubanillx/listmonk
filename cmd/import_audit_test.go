package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/subimporter"
)

// The tests in this file cover the terminal audit event of an asynchronous
// customer import. They need no database: App.audit is the auditRecorder
// interface, so a recording sink stands in for the audit writer, and the
// importer only touches PostgreSQL when it commits rows, which the stopped
// session below never does.

var (
	// importAuditTestEmail matches the shape of a customer e-mail address.
	importAuditTestEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// importAuditTestCredentialKey matches metadata keys that would suggest a
	// credential, session or token was copied into an audit event.
	importAuditTestCredentialKey = regexp.MustCompile(`(?i)pass|secret|token|credential|authorization|api[_-]?key|private[_-]?key|session`)
	// importAuditTestMetadataKeys is the complete set of keys a terminal event
	// may carry.
	importAuditTestMetadataKeys = map[string]bool{
		"status": true, "total": true, "imported": true, "filename": true,
	}
)

// terminalAuditSink records the events the importer watchers write. It is safe
// for concurrent use, because the production watcher records from its own
// goroutine.
type terminalAuditSink struct {
	mu       sync.Mutex
	events   []auditlog.Event
	attempts int
	err      error
}

func (s *terminalAuditSink) Record(_ context.Context, event auditlog.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attempts++
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func (s *terminalAuditSink) recorded() []auditlog.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]auditlog.Event(nil), s.events...)
}

func (s *terminalAuditSink) attempted() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.attempts
}

// stubbedImportAuditImporter publishes one status for a test session, standing
// in for the process-wide importer.
type stubbedImportAuditImporter struct {
	done  chan struct{}
	stats subimporter.Status
}

func (s *stubbedImportAuditImporter) Done() <-chan struct{} { return s.done }

func (s *stubbedImportAuditImporter) GetStats() subimporter.Status { return s.stats }

// pendingID is the id of the session the tracker still owes a terminal event.
func (t *importAuditTracker) pendingID() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.pending == nil {
		return ""
	}
	return t.pending.ID
}

// resetImportAuditSessions replaces the process-wide tracker for one test. The
// importer is a process-wide singleton, so tests that drive
// App.admitImportSession share that tracker with every other import in the
// process; each one starts from an empty state and restores the previous
// tracker afterwards.
func resetImportAuditSessions(t *testing.T) *importAuditTracker {
	t.Helper()

	previous := importAuditSessions
	tracker := &importAuditTracker{}
	importAuditSessions = tracker
	t.Cleanup(func() { importAuditSessions = previous })

	return tracker
}

// intPointer is the *int the session options expect for an organization.
func intPointer(value int) *int { return &value }

// assertImportTerminalEventIsSafe fails when a terminal event carries anything
// that could not come from the fixed, privacy-safe payload: the terminal status,
// the two counts and the uploaded file's name. It inspects the whole serialized
// event, so a field added later cannot smuggle a value past it unnoticed.
func assertImportTerminalEventIsSafe(t *testing.T, event auditlog.Event) {
	t.Helper()

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshaling the terminal event: %v", err)
	}
	if match := importAuditTestEmail.Find(encoded); match != nil {
		t.Fatalf("terminal event %s contains an e-mail-like value %q", event.Action, match)
	}
	if bytes.ContainsAny(encoded, "\r\n") {
		t.Fatalf("terminal event %s contains a line break, which would let it render as log text: %s", event.Action, encoded)
	}

	metadata, ok := event.Metadata.(map[string]any)
	if !ok {
		t.Fatalf("terminal event metadata = %#v, want a map", event.Metadata)
	}
	for key, value := range metadata {
		if !importAuditTestMetadataKeys[key] {
			t.Fatalf("terminal event metadata carries unexpected key %q: %#v", key, metadata)
		}
		if importAuditTestCredentialKey.MatchString(key) {
			t.Fatalf("terminal event metadata carries a credential-looking key %q", key)
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.ContainsFunc(text, unicode.IsControl) {
			t.Fatalf("terminal event metadata value for %q contains a control character: %q", key, text)
		}
	}
}

// assertImportAuditMetadataEquals checks the exact payload of a terminal event,
// including the keys that must be present.
func assertImportAuditMetadataEquals(t *testing.T, event auditlog.Event, want map[string]any) {
	t.Helper()

	metadata, ok := event.Metadata.(map[string]any)
	if !ok {
		t.Fatalf("terminal event metadata = %#v, want a map", event.Metadata)
	}
	if len(metadata) != len(want) {
		t.Fatalf("terminal event metadata = %#v, want exactly %#v", metadata, want)
	}
	for key, value := range want {
		if metadata[key] != value {
			t.Fatalf("terminal event metadata[%q] = %#v, want %#v (metadata %#v)", key, metadata[key], value, metadata)
		}
	}
}

// TestImportAuditOutcomeMapsTerminalStatusesOnly pins the status to event
// mapping, including the running statuses that must not produce an event at all.
func TestImportAuditOutcomeMapsTerminalStatusesOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     subimporter.Status
		wantAction string
		wantResult string
		wantReason string
	}{
		{
			name:       "a finished import completes",
			status:     subimporter.Status{Status: subimporter.StatusFinished, Total: 250, Imported: 248},
			wantAction: "customer.import_completed", wantResult: "success", wantReason: "completed",
		},
		{
			name:       "a stopped import completes with the user's reason",
			status:     subimporter.Status{Status: subimporter.StatusStopped, Total: 250, Imported: 12},
			wantAction: "customer.import_completed", wantResult: "success", wantReason: "stopped_by_user",
		},
		{
			name:       "a failed import fails",
			status:     subimporter.Status{Status: subimporter.StatusFailed, Total: 3, Imported: 0},
			wantAction: "customer.import_failed", wantResult: "failed", wantReason: "failed",
		},
		{
			name:   "an importing session has no terminal event yet",
			status: subimporter.Status{Status: subimporter.StatusImporting, Total: 250, Imported: 12},
		},
		{
			name:   "a stopping session has no terminal event yet",
			status: subimporter.Status{Status: subimporter.StatusStopping},
		},
		{
			name:   "a cleared importer has no terminal event",
			status: subimporter.Status{Status: subimporter.StatusNone},
		},
		{
			name:   "an unknown status has no terminal event",
			status: subimporter.Status{Status: "something-else"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action, result, reason := importAuditOutcome(tc.status)
			if action != tc.wantAction || result != tc.wantResult || reason != tc.wantReason {
				t.Fatalf("importAuditOutcome(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.status.Status, action, result, reason, tc.wantAction, tc.wantResult, tc.wantReason)
			}
			if action == "" && (result != "" || reason != "") {
				t.Fatalf("a status without a terminal event reported result %q and reason %q", result, reason)
			}
		})
	}
}

// TestImportTerminalAuditEventContract pins the actor, workspace and object of a
// terminal event, plus the fallbacks for an unattributable import.
func TestImportTerminalAuditEventContract(t *testing.T) {
	for _, tc := range []struct {
		name            string
		session         importAuditSession
		status          subimporter.Status
		wantActorType   string
		wantActorUserID int
		wantOrgID       int64
	}{
		{
			name:            "a user import in an organization workspace",
			session:         importAuditSession{ID: "session-1", Filename: "customers.csv", OwnerUserID: 42, OrganizationID: 7},
			status:          subimporter.Status{Status: subimporter.StatusFinished, Total: 250, Imported: 248},
			wantActorType:   "user",
			wantActorUserID: 42,
			wantOrgID:       7,
		},
		{
			name:            "a personal workspace is reported as zero, not as unknown",
			session:         importAuditSession{ID: "session-2", Filename: "customers.csv", OwnerUserID: 42},
			status:          subimporter.Status{Status: subimporter.StatusStopped},
			wantActorType:   "user",
			wantActorUserID: 42,
			wantOrgID:       0,
		},
		{
			name:          "an import without an owner is a system action",
			session:       importAuditSession{ID: "session-3", Filename: "customers.csv"},
			status:        subimporter.Status{Status: subimporter.StatusFailed},
			wantActorType: "system",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := importTerminalAuditEvent(tc.session, tc.status)

			if event.ObjectType != auditObjectTypeCustomerImport {
				t.Fatalf("object type = %q, want %q", event.ObjectType, auditObjectTypeCustomerImport)
			}
			if event.ObjectID != tc.session.ID {
				t.Fatalf("object id = %q, want the session id %q", event.ObjectID, tc.session.ID)
			}
			if event.ActorType != tc.wantActorType {
				t.Fatalf("actor type = %q, want %q", event.ActorType, tc.wantActorType)
			}
			if tc.wantActorUserID == 0 {
				if event.ActorUserID != nil {
					t.Fatalf("actor user id = %d, want none", *event.ActorUserID)
				}
			} else if event.ActorUserID == nil || *event.ActorUserID != tc.wantActorUserID {
				t.Fatalf("actor user id = %v, want %d", event.ActorUserID, tc.wantActorUserID)
			}
			if event.OrganizationID == nil {
				t.Fatal("organization id is nil, want a non-nil value")
			}
			if *event.OrganizationID != tc.wantOrgID {
				t.Fatalf("organization id = %d, want %d", *event.OrganizationID, tc.wantOrgID)
			}

			_, wantResult, wantReason := importAuditOutcome(tc.status)
			if event.Result != wantResult || event.ReasonCode != wantReason {
				t.Fatalf("result/reason = (%q, %q), want (%q, %q)", event.Result, event.ReasonCode, wantResult, wantReason)
			}

			assertImportTerminalEventIsSafe(t, event)
		})
	}
}

// TestImportAuditMetadataCarriesOnlySafeCounts checks the payload keys and the
// file-name sanitization: the name is client supplied, so a line break in it
// must not reach the event.
func TestImportAuditMetadataCarriesOnlySafeCounts(t *testing.T) {
	session := importAuditSession{ID: "session-1", Filename: "customers\r\npassword=hunter2.csv", OwnerUserID: 42, OrganizationID: 7}
	status := subimporter.Status{Status: subimporter.StatusFinished, Total: 250, Imported: 248}

	event := importTerminalAuditEvent(session, status)
	assertImportAuditMetadataEquals(t, event, map[string]any{
		"status":   "finished",
		"total":    250,
		"imported": 248,
		"filename": "customers  password=hunter2.csv",
	})
	assertImportTerminalEventIsSafe(t, event)

	// An import without a file name simply omits the key.
	anonymous := importTerminalAuditEvent(importAuditSession{ID: "session-2", OwnerUserID: 42}, status)
	assertImportAuditMetadataEquals(t, anonymous, map[string]any{
		"status":   "finished",
		"total":    250,
		"imported": 248,
	})
}

// TestWatchImportAuditSessionRecordsOneTerminalEvent drives the watcher of a
// finished session: it must record exactly one event, carrying the actor, the
// workspace, the counts and nothing but the safe payload.
func TestWatchImportAuditSessionRecordsOneTerminalEvent(t *testing.T) {
	tracker := &importAuditTracker{}
	im := &stubbedImportAuditImporter{done: make(chan struct{})}
	session := newImportAuditSession(subimporter.SessionOpt{
		Filename:       "customers-2026-02.csv",
		OwnerUserID:    42,
		OrganizationID: intPointer(7),
	}, im.done)
	if session.ID == "" {
		t.Fatal("the admitted session has no id")
	}
	tracker.armLocked(session)

	// The session ends: the importer publishes its terminal status and closes
	// this session's completion signal.
	im.stats = subimporter.Status{
		Name:           "customers-2026-02.csv",
		Status:         subimporter.StatusFinished,
		Total:          250,
		Imported:       248,
		OwnerUserID:    42,
		OrganizationID: 7,
	}
	close(im.done)

	var recorded []auditlog.Event
	record := func(event auditlog.Event) { recorded = append(recorded, event) }

	watchImportAuditSession(tracker, im, session, record)
	// A second observation of the same session - its watcher waking up twice, or
	// the admission of the next import claiming a session the watcher already
	// recorded - must not produce a second event.
	watchImportAuditSession(tracker, im, session, record)

	if len(recorded) != 1 {
		t.Fatalf("recorded %d terminal event(s), want exactly 1: %+v", len(recorded), recorded)
	}
	event := recorded[0]
	if event.Action != auditActionImportCompleted || event.Result != "success" || event.ReasonCode != importReasonCompleted {
		t.Fatalf("terminal event = (%q, %q, %q), want (%q, %q, %q)",
			event.Action, event.Result, event.ReasonCode, auditActionImportCompleted, "success", importReasonCompleted)
	}
	if event.ActorType != "user" || event.ActorUserID == nil || *event.ActorUserID != 42 {
		t.Fatalf("actor = (%q, %v), want user 42", event.ActorType, event.ActorUserID)
	}
	if event.OrganizationID == nil || *event.OrganizationID != 7 {
		t.Fatalf("organization = %v, want 7", event.OrganizationID)
	}
	if event.ObjectType != auditObjectTypeCustomerImport || event.ObjectID != session.ID {
		t.Fatalf("object = (%q, %q), want (%q, %q)", event.ObjectType, event.ObjectID, auditObjectTypeCustomerImport, session.ID)
	}
	assertImportAuditMetadataEquals(t, event, map[string]any{
		"status":   "finished",
		"total":    250,
		"imported": 248,
		"filename": "customers-2026-02.csv",
	})
	assertImportTerminalEventIsSafe(t, event)
}

// TestImportAuditClaimRejectsRunningAndSupersededSessions covers the two
// observations that must not be reported: a session that has not ended, and a
// session whose status has already been replaced by the next import's.
func TestImportAuditClaimRejectsRunningAndSupersededSessions(t *testing.T) {
	tracker := &importAuditTracker{}
	im := &stubbedImportAuditImporter{done: make(chan struct{})}
	first := newImportAuditSession(subimporter.SessionOpt{
		Filename:       "customers.csv",
		OwnerUserID:    42,
		OrganizationID: intPointer(7),
	}, im.done)
	tracker.armLocked(first)

	// Still running: there is no terminal event yet, and the session stays
	// pending so that the terminal state observed later is still recorded.
	im.stats = subimporter.Status{Name: "customers.csv", Status: subimporter.StatusImporting, OwnerUserID: 42, OrganizationID: 7}
	if _, _, ok := tracker.claimLocked(im, first.ID); ok {
		t.Fatal("a running session was claimed")
	}
	if tracker.pendingID() != first.ID {
		t.Fatalf("pending session = %q, want %q", tracker.pendingID(), first.ID)
	}

	// The session ends and the next import is admitted. The admission claims the
	// finished session while the importer still reports it, which is the only
	// moment its terminal status can be read.
	im.stats = subimporter.Status{
		Name: "customers.csv", Status: subimporter.StatusFinished,
		Total: 5, Imported: 5, OwnerUserID: 42, OrganizationID: 7,
	}
	close(im.done)

	var recorded []auditlog.Event
	record := func(event auditlog.Event) { recorded = append(recorded, event) }

	secondDone := make(chan struct{})
	tracker.mu.Lock()
	claimed, claimedStatus, ok := tracker.claimLocked(im, "")
	if !ok {
		tracker.mu.Unlock()
		t.Fatal("the finished session was not claimed at admission")
	}
	// The next session is published and armed before the lock is released, so the
	// previous session's watcher cannot mistake the new status for its own. Both
	// imports deliberately look identical: same owner, workspace and file.
	im.done = secondDone
	second := newImportAuditSession(subimporter.SessionOpt{
		Filename:       "customers.csv",
		OwnerUserID:    42,
		OrganizationID: intPointer(7),
	}, secondDone)
	im.stats = subimporter.Status{
		Name: "customers.csv", Status: subimporter.StatusFinished,
		Total: 900, Imported: 900, OwnerUserID: 42, OrganizationID: 7,
	}
	tracker.armLocked(second)
	tracker.mu.Unlock()

	record(importTerminalAuditEvent(claimed, claimedStatus))

	// The first session's watcher wakes up only now, after the status changed.
	watchImportAuditSession(tracker, im, first, record)
	if len(recorded) != 1 {
		t.Fatalf("recorded %d event(s) after the superseded observation, want 1: %+v", len(recorded), recorded)
	}
	if recorded[0].ObjectID != first.ID {
		t.Fatalf("recorded event belongs to %q, want the first session %q", recorded[0].ObjectID, first.ID)
	}
	if total := recorded[0].Metadata.(map[string]any)["total"]; total != 5 {
		t.Fatalf("first session's total = %v, want its own 5 and not the later session's 900", total)
	}

	// The second session's own watcher records its own event.
	close(secondDone)
	watchImportAuditSession(tracker, im, second, record)
	if len(recorded) != 2 {
		t.Fatalf("recorded %d event(s), want one per session: %+v", len(recorded), recorded)
	}
	if recorded[1].ObjectID != second.ID {
		t.Fatalf("second session's event belongs to %q, want %q", recorded[1].ObjectID, second.ID)
	}
	if total := recorded[1].Metadata.(map[string]any)["total"]; total != 900 {
		t.Fatalf("second session's total = %v, want 900", total)
	}
}

// newImportAuditTestApp returns an App wired to a real importer and the given
// audit sink, without a database. The importer only reaches PostgreSQL when it
// commits rows, and the session below is stopped before it commits.
func newImportAuditTestApp(t *testing.T, sink auditRecorder) (*App, *subimporter.Importer) {
	t.Helper()

	langB, err := os.ReadFile(filepath.Join("..", "i18n", "en.json"))
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	translator, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}

	im := subimporter.New(subimporter.Options{
		// The importer notifies through this callback when a session ends.
		PostCB: func(string, any) error { return nil },
	}, nil, translator)

	return &App{importer: im, audit: sink, log: log.New(io.Discard, "", 0)}, im
}

// runStoppedImport admits one two-row import session and stops it the way a
// user's stop request does, so the session ends in its "stopped" terminal state
// with a real total and without committing anything.
func runStoppedImport(t *testing.T, app *App, im *subimporter.Importer, filename string) {
	t.Helper()

	csvPath := filepath.Join(t.TempDir(), "customers.csv")
	if err := os.WriteFile(csvPath, []byte("email,name\nada@example.com,Ada\ngrace@example.com,Grace\n"), 0600); err != nil {
		t.Fatal(err)
	}

	sess, err := app.admitImportSession(subimporter.SessionOpt{
		Filename:       filename,
		Mode:           subimporter.ModeBlocklist,
		OwnerUserID:    42,
		OrganizationID: intPointer(7),
	})
	if err != nil {
		t.Fatalf("admitting the import session: %v", err)
	}

	// Load the rows so the session reports a real total, then stop it before the
	// consumer commits: that is the "stopped" terminal state a user's stop
	// request produces.
	if err := sess.LoadCSV(csvPath); err != nil {
		t.Fatalf("loading the CSV: %v", err)
	}
	im.Stop()
	go sess.Start()
}

// waitForImportAuditAttempts waits until the watcher has called the audit sink.
func waitForImportAuditAttempts(t *testing.T, sink *terminalAuditSink, want int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sink.attempted() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d audit attempt(s), got %d", want, sink.attempted())
}

// TestImportSessionTerminalAuditForStoppedImport drives a real importer session
// through admission, loading and stopping, and checks the single terminal event
// its watcher records.
func TestImportSessionTerminalAuditForStoppedImport(t *testing.T) {
	tracker := resetImportAuditSessions(t)
	sink := &terminalAuditSink{}
	app, im := newImportAuditTestApp(t, sink)

	runStoppedImport(t, app, im, "customers-2026-02.csv")
	waitForImportAuditAttempts(t, sink, 1)

	if status := im.GetStats().Status; status != subimporter.StatusStopped {
		t.Fatalf("importer status = %q, want %q", status, subimporter.StatusStopped)
	}

	recorded := sink.recorded()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d terminal event(s), want exactly 1: %+v", len(recorded), recorded)
	}
	event := recorded[0]
	if event.Action != auditActionImportCompleted || event.Result != "success" || event.ReasonCode != importReasonStoppedByUser {
		t.Fatalf("terminal event = (%q, %q, %q), want (%q, %q, %q)",
			event.Action, event.Result, event.ReasonCode, auditActionImportCompleted, "success", importReasonStoppedByUser)
	}
	if event.ObjectType != auditObjectTypeCustomerImport || event.ObjectID == "" {
		t.Fatalf("object = (%q, %q), want a %q with an id", event.ObjectType, event.ObjectID, auditObjectTypeCustomerImport)
	}
	if event.ActorType != "user" || event.ActorUserID == nil || *event.ActorUserID != 42 {
		t.Fatalf("actor = (%q, %v), want user 42", event.ActorType, event.ActorUserID)
	}
	if event.OrganizationID == nil || *event.OrganizationID != 7 {
		t.Fatalf("organization = %v, want 7", event.OrganizationID)
	}
	assertImportAuditMetadataEquals(t, event, map[string]any{
		"status":   "stopped",
		"total":    2,
		"imported": 0,
		"filename": "customers-2026-02.csv",
	})
	assertImportTerminalEventIsSafe(t, event)

	// The session was claimed exactly once, so no second event can follow.
	if pending := tracker.pendingID(); pending != "" {
		t.Fatalf("session %q is still pending after its terminal event was recorded", pending)
	}
}

// TestImportTerminalAuditFailureKeepsImportOutcome guards the best-effort rule:
// an audit insert that fails is logged by App.recordAuditEvent and must not
// change what the import did.
func TestImportTerminalAuditFailureKeepsImportOutcome(t *testing.T) {
	resetImportAuditSessions(t)
	sink := &terminalAuditSink{err: errors.New("audit insert failed")}
	app, im := newImportAuditTestApp(t, sink)

	runStoppedImport(t, app, im, "customers-2026-02.csv")
	waitForImportAuditAttempts(t, sink, 1)

	if status := im.GetStats().Status; status != subimporter.StatusStopped {
		t.Fatalf("importer status = %q after a failed audit insert, want %q", status, subimporter.StatusStopped)
	}
	if recorded := sink.recorded(); len(recorded) != 0 {
		t.Fatalf("a failing sink recorded %d event(s): %+v", len(recorded), recorded)
	}
	if got := sink.attempted(); got != 1 {
		t.Fatalf("audit insert attempts = %d, want exactly 1", got)
	}
}

// TestImportAuditMetadataKeysAreTheDocumentedSafePayload keeps the expected key
// set in the safety helper honest: it is the complete list of keys a terminal
// event may carry.
func TestImportAuditMetadataKeysAreTheDocumentedSafePayload(t *testing.T) {
	keys := make([]string, 0, len(importAuditTestMetadataKeys))
	for key := range importAuditTestMetadataKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	if got := strings.Join(keys, ","); got != "filename,imported,status,total" {
		t.Fatalf("terminal event metadata keys = %q, want the documented safe payload", got)
	}
}
