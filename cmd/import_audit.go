package main

// This file records the terminal audit events of the asynchronous customer
// importer.
//
// The route-mapped events in cmd/audit.go (customer.import_started,
// customer.import_stopped) describe the HTTP request that started or cancelled
// an import. They cannot describe how the import ended, because the session
// keeps running after the request returns. Exactly one of the events below is
// recorded for every admitted session, when that session reaches its terminal
// state.
//
// Two properties are enforced here rather than left to the call sites:
//
//   - The importer is a process-wide singleton that publishes exactly one
//     status. The status a session ends with is gone as soon as the next session
//     is admitted, so a session's identity is captured at admission
//     (importAuditSession) and the admission of the next session claims and
//     records a session that ended without being observed before it publishes
//     its own status (importAuditTracker).
//   - Audit writes are best effort. Events are built and recorded outside every
//     lock and the result is only logged (App.recordAuditEvent), so a failing
//     audit insert can never change the outcome of an import.

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gofrs/uuid/v5"
	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/internal/subimporter"
)

const (
	// Terminal actions of the asynchronous customer importer. The route-mapped
	// customer.import_started and customer.import_stopped actions are unchanged.
	auditActionImportCompleted = "customer.import_completed"
	auditActionImportFailed    = "customer.import_failed"

	auditObjectTypeCustomerImport = "customer_import"

	// Safe, normalized reason codes for Event.ReasonCode. Each one names a
	// terminal state of the importer itself, so an operator can filter terminal
	// events without parsing free text.
	importReasonCompleted     = "completed"
	importReasonStoppedByUser = "stopped_by_user"
	importReasonFailed        = "failed"
)

// importAuditOutcome maps an import status to the single audit event that closes
// the session. An empty action means the session has not reached a terminal
// state yet, so there is no terminal event to record.
//
// A stopped import is a success: the caller asked for it and the importer ended
// the session without a failure. Only an importer failure is reported as failed.
func importAuditOutcome(status subimporter.Status) (action, result, reason string) {
	switch status.Status {
	case subimporter.StatusFinished:
		return auditActionImportCompleted, "success", importReasonCompleted
	case subimporter.StatusStopped:
		return auditActionImportCompleted, "success", importReasonStoppedByUser
	case subimporter.StatusFailed:
		return auditActionImportFailed, "failed", importReasonFailed
	default:
		// importing, stopping, none (cleared) and unknown statuses: the session
		// is still running, or the status is not a terminal state of this
		// importer. Recording here would invent an outcome and a count.
		return "", "", ""
	}
}

// importAuditImporter is the part of the process-wide customer importer a
// terminal observation needs. *subimporter.Importer implements it; the interface
// keeps the observation testable without a database.
type importAuditImporter interface {
	// Done is the completion signal of the session that is admitted right now.
	// It is per-session: a waiter can never observe a later session ending.
	Done() <-chan struct{}
	// GetStats is the status snapshot of the session that is admitted right now.
	GetStats() subimporter.Status
}

// importAuditSession is the identity of one admitted import session, captured
// while the importer still describes it. Everything the terminal event needs is
// kept here, so the event does not depend on the live status still belonging to
// this session.
type importAuditSession struct {
	// ID is the object id of the terminal event. It is generated at admission, so
	// two consecutive imports of the same file by the same user stay
	// distinguishable.
	ID string

	// Filename, OwnerUserID and OrganizationID mirror what the importer publishes
	// as Status.Name, Status.OwnerUserID and Status.OrganizationID for this
	// session. They are what tells the session apart from the next one.
	Filename       string
	OwnerUserID    int
	OrganizationID int

	// done is this session's own completion signal, not the importer's current
	// one.
	done <-chan struct{}
}

// newImportAuditSession captures the identity of an admitted session. The
// identity comes from the options the importer published, not from the live
// status, so it cannot be confused with a later session's status.
func newImportAuditSession(opt subimporter.SessionOpt, done <-chan struct{}) importAuditSession {
	organizationID := 0
	if opt.OrganizationID != nil {
		organizationID = *opt.OrganizationID
	}

	return importAuditSession{
		ID:             newImportAuditSessionID(),
		Filename:       opt.Filename,
		OwnerUserID:    opt.OwnerUserID,
		OrganizationID: organizationID,
		done:           done,
	}
}

// matches reports whether a status snapshot still describes this session. A
// status that does not match belongs to a later session (or to a cleared
// importer) and must never be reported under this session's identity.
func (s importAuditSession) matches(status subimporter.Status) bool {
	return status.Name == s.Filename &&
		status.OwnerUserID == s.OwnerUserID &&
		status.OrganizationID == s.OrganizationID
}

// newImportAuditSessionID identifies one admitted session for the lifetime of
// its terminal event. A UUID keeps two imports of the same file by the same user
// apart; if the system entropy source is unavailable, the admission time still
// separates two consecutive sessions.
func newImportAuditSessionID() string {
	if id, err := uuid.NewV4(); err == nil {
		return id.String()
	}
	return fmt.Sprintf("import-%d", time.Now().UTC().UnixNano())
}

// importTerminalAuditEvent builds the single terminal event of a session. It is
// pure, so the event contract can be unit tested.
//
// The caller must have established that status still describes session (see
// importAuditTracker.claimLocked). Without that check a status belonging to a
// later session could be reported with this session's identity.
func importTerminalAuditEvent(session importAuditSession, status subimporter.Status) auditlog.Event {
	action, result, reason := importAuditOutcome(status)

	// A personal workspace is 0 and stays a non-nil value: the column means "not
	// part of an organization", not "unknown".
	organizationID := int64(session.OrganizationID)

	event := auditlog.Event{
		OrganizationID: &organizationID,
		ActorType:      "system",
		Action:         action,
		ObjectType:     auditObjectTypeCustomerImport,
		ObjectID:       session.ID,
		Result:         result,
		ReasonCode:     reason,
		Metadata:       importAuditMetadata(session, status),
	}

	// The user who started the import is the actor of its outcome. A session that
	// cannot be attributed to a user is reported as a system action instead of
	// guessing an identity.
	if session.OwnerUserID > 0 {
		actorUserID := session.OwnerUserID
		event.ActorType = "user"
		event.ActorUserID = &actorUserID
	}

	return event
}

// importAuditMetadata is the whole payload of a terminal event: the terminal
// status, the two counts, and the uploaded file's name. It goes through
// auditDetailMap, which drops empty values and bounds text length.
//
// It deliberately carries nothing else: no customer row, no e-mail address, no
// credential, and no importer log output. The status snapshot the importer
// exposes (subimporter.Status) has no field for any of those, and the key set
// here is fixed.
func importAuditMetadata(session importAuditSession, status subimporter.Status) map[string]any {
	return auditDetailMap(map[string]any{
		"status":   status.Status,
		"total":    status.Total,
		"imported": status.Imported,
		"filename": importAuditFilename(session.Filename),
	})
}

// importAuditFilename reports the uploaded file's name without control
// characters. The name is client supplied, and a line break in it would
// otherwise let a single audit row render as several lines in a CSV export.
func importAuditFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
}

// importAuditTracker holds the identity of the import session whose terminal
// event has not been recorded yet.
//
// The importer is a process-wide singleton, so its tracker is process-wide too:
// the watcher of a session and the request that admits the next session have to
// agree on which session the importer's current status describes. A session is
// claimed exactly once (claimLocked clears it), so it produces exactly one
// terminal event.
type importAuditTracker struct {
	mu      sync.Mutex
	pending *importAuditSession
}

// importAuditSessions tracks the process-wide importer. Tests replace it with
// their own instance (see resetImportAuditSessions).
var importAuditSessions = &importAuditTracker{}

// armLocked publishes session as the one whose terminal event is still to be
// recorded. The caller must hold the tracker lock.
func (t *importAuditTracker) armLocked(session importAuditSession) {
	t.pending = &session
}

// claimLocked takes ownership of a pending session that has reached its terminal
// state, so exactly one caller records its event. only restricts the claim to
// one session id; "" claims whichever session is pending, which is what the
// admission of the next session uses. The caller must hold the tracker lock.
//
// It reports false when there is nothing to record: no pending session, a
// different session than the requested one, a status that no longer describes
// the pending session (a later session was published, or the importer cleared
// its state), or a session that is still running.
func (t *importAuditTracker) claimLocked(im importAuditImporter, only string) (importAuditSession, subimporter.Status, bool) {
	session := t.pending
	if session == nil || (only != "" && session.ID != only) {
		return importAuditSession{}, subimporter.Status{}, false
	}

	status := im.GetStats()
	if !session.matches(status) {
		return importAuditSession{}, subimporter.Status{}, false
	}
	if action, _, _ := importAuditOutcome(status); action == "" {
		return importAuditSession{}, subimporter.Status{}, false
	}

	t.pending = nil
	return *session, status, true
}

// watchImportAuditSession records the single terminal audit event of one import
// session. It is started once per admitted session and exits as soon as that
// session ends: the completion channel belongs to the session, so a later
// session can neither close it nor delay this watcher.
func watchImportAuditSession(t *importAuditTracker, im importAuditImporter, session importAuditSession, record func(auditlog.Event)) {
	<-session.done

	t.mu.Lock()
	observed, status, ok := t.claimLocked(im, session.ID)
	t.mu.Unlock()
	if !ok {
		// Another observer recorded this session's event already, or the status
		// no longer describes it. Recording here would duplicate an event or
		// attribute a later session's outcome to this one.
		return
	}

	if record != nil {
		record(importTerminalAuditEvent(observed, status))
	}
}

// recordImportTerminalAudit writes the claimed terminal event of one session.
// It is called outside the tracker lock: the audit insert is best effort and
// must never gate admitting the next import or the end of this one.
func (a *App) recordImportTerminalAudit(session importAuditSession, status subimporter.Status) {
	a.recordAuditEvent(importTerminalAuditEvent(session, status))
}
