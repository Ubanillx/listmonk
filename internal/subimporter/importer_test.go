package subimporter

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResolveMappingsSupportsUnicodeHeaders(t *testing.T) {
	s := &Session{opt: SessionOpt{FieldMap: map[string]string{
		"email":         "邮箱",
		"name":          "姓名",
		"customer_code": "客户编号",
	}}}

	got, hasHeader, err := s.resolveMappings([]string{"客户编号", "注册名称", "姓名", "邮箱"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHeader {
		t.Fatal("localized header mapping was not recognized as a header")
	}
	want := map[string]int{"customer_code": 0, "name": 2, "email": 3}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("resolved mappings = %v, want %v", got, want)
	}
}

func TestParseColumnRefRejectsUnicodeHeaderAsColumnRef(t *testing.T) {
	if _, ok := parseColumnRef("邮箱"); ok {
		t.Fatal("localized header was accepted as an Excel column reference")
	}
}

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

// startBarrier releases its participants as close to simultaneously as the
// runtime allows: they spin on the same counter, so the concurrent callers
// reach the admission check together instead of crossing it one at a time. A
// plain channel wake-up spreads the callers over microseconds, which is far
// wider than the window the admission has to close, so the barrier would not
// reliably exercise it.
type startBarrier struct {
	participants int32
	arrived      int32
}

// spinBudget is how many spins a participant waits before yielding a processor
// to participants the runtime has not scheduled yet. Without it, a machine with
// fewer processors than participants would let the spinners starve each other.
const spinBudget = 1 << 16

func newStartBarrier(participants int) *startBarrier {
	return &startBarrier{participants: int32(participants)}
}

func (b *startBarrier) wait() {
	atomic.AddInt32(&b.arrived, 1)

	spins := 0
	for atomic.LoadInt32(&b.arrived) < b.participants {
		if spins++; spins == spinBudget {
			spins = 0
			runtime.Gosched()
		}
	}
}

// raceAdmission releases n callers into NewSession at the same time and returns
// the sessions and errors they observed. Every option is built before the
// barrier so that the callers enter the admission check without any work of
// their own in between.
func raceAdmission(im *Importer, n int, opt func(i int) SessionOpt) ([]*Session, []error) {
	var (
		barrier = newStartBarrier(n)
		done    sync.WaitGroup

		opts     = make([]SessionOpt, n)
		sessions = make([]*Session, n)
		errs     = make([]error, n)
	)

	for i := range opts {
		opts[i] = opt(i)
	}

	done.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer done.Done()

			barrier.wait()
			sessions[i], errs[i] = im.NewSession(opts[i])
		}(i)
	}
	done.Wait()

	return sessions, errs
}

// admissionResult returns the callers that were admitted and fails the test for
// any caller that neither got a session nor was refused with ErrIsImporting.
func admissionResult(t *testing.T, sessions []*Session, errs []error) []int {
	t.Helper()

	var winners []int
	for i := range sessions {
		switch {
		case errs[i] == nil && sessions[i] != nil:
			winners = append(winners, i)
		case errs[i] == nil:
			t.Fatalf("caller %d got neither a session nor an error", i)
		case sessions[i] != nil:
			t.Fatalf("caller %d got a session along with the error %v", i, errs[i])
		case !errors.Is(errs[i], ErrIsImporting):
			t.Fatalf("caller %d was refused with %v, want %v", i, errs[i], ErrIsImporting)
		}
	}

	return winners
}

// TestNewSessionAdmitsExactlyOneConcurrentCaller covers the admission race in
// the singleton importer. Checking that no import is running and publishing the
// new running status have to be one critical section: otherwise several callers
// pass the check and each publishes its own session, so the global status, owner
// and log buffer are overwritten while two loaders write to the same importer.
func TestNewSessionAdmitsExactlyOneConcurrentCaller(t *testing.T) {
	const (
		callers = 32
		rounds  = 25
	)

	im := New(Options{}, nil, nil)
	for round := 0; round < rounds; round++ {
		name := func(i int) string { return fmt.Sprintf("round-%d-caller-%d.csv", round, i) }

		sessions, errs := raceAdmission(im, callers, func(i int) SessionOpt {
			return SessionOpt{Mode: ModeSubscribe, Filename: name(i), OwnerUserID: i + 1}
		})

		winners := admissionResult(t, sessions, errs)
		if len(winners) != 1 {
			t.Fatalf("round %d admitted %d of %d concurrent callers, want exactly 1",
				round, len(winners), callers)
		}

		// The one admitted session is the only one allowed to own the
		// singleton's observable state.
		winner := winners[0]
		if got := im.GetStats(); got.Status != StatusImporting ||
			got.Name != name(winner) || got.OwnerUserID != winner+1 {
			t.Fatalf("round %d: stats %+v do not describe the admitted session %q (owner %d)",
				round, got, name(winner), winner+1)
		}
		if logs := string(im.GetLogs()); !strings.Contains(logs, name(winner)) {
			t.Fatalf("round %d: the admitted session's log line is missing: %q", round, logs)
		}

		// Finish the round's session so the next round contends over admission
		// again from an idle importer.
		im.setStatus(StatusFinished)
	}
}

// TestNewSessionAdmittedAfterTerminalStatus covers the other half of the
// admission rule: a session that has reached a terminal state must not block the
// next one, while one that is still importing or stopping must.
func TestNewSessionAdmittedAfterTerminalStatus(t *testing.T) {
	org := 7

	for _, terminal := range []string{StatusFinished, StatusFailed, StatusStopped} {
		t.Run(terminal, func(t *testing.T) {
			im := New(Options{}, nil, nil)

			first, err := im.NewSession(SessionOpt{Mode: ModeSubscribe, Filename: "first.csv", OwnerUserID: 11})
			if err != nil || first == nil {
				t.Fatalf("expected the first session to be admitted: %v", err)
			}

			// Cancellation in progress is still busy: the loader may be draining
			// the rows it already accepted.
			im.setStatus(StatusStopping)
			if _, err := im.NewSession(SessionOpt{Mode: ModeSubscribe, Filename: "while-stopping.csv"}); !errors.Is(err, ErrIsImporting) {
				t.Fatalf("NewSession while stopping = %v, want %v", err, ErrIsImporting)
			}

			im.setStatus(terminal)
			select {
			case <-im.Done():
			default:
				t.Fatalf("expected the %q session to publish its completion signal", terminal)
			}

			// A new session, from a different workspace than the finished one.
			second, err := im.NewSession(SessionOpt{
				Mode:           ModeSubscribe,
				Filename:       "second.csv",
				OwnerUserID:    22,
				OrganizationID: &org,
			})
			if err != nil || second == nil {
				t.Fatalf("expected a new session after %q to be admitted: %v", terminal, err)
			}

			if got := im.GetStats(); got.Status != StatusImporting || got.Name != "second.csv" ||
				got.OwnerUserID != 22 || got.OrganizationID != org {
				t.Fatalf("stats after re-admission = %+v, want the new session's", got)
			}

			// The re-admitted session owns a fresh completion signal, so a waiter
			// of the finished session does not mistake it for its own.
			select {
			case <-im.Done():
				t.Fatal("expected the re-admitted session to report no completion")
			default:
			}
		})
	}
}

// TestNewSessionCrossWorkspaceSingleHolder covers the isolation aspect of the
// admission race: two workspaces racing for the singleton importer must not both
// hold a session. Every loser has to observe the winner's owner and workspace,
// and no loser may reach the shared log buffer.
func TestNewSessionCrossWorkspaceSingleHolder(t *testing.T) {
	const (
		callers = 32
		rounds  = 25
	)

	var (
		orgA = 7
		orgB = 9

		// workspace returns the identity of the caller at index i; the round's
		// callers alternate between two workspaces. The file name is unique per
		// caller so that the published session identifies exactly one of them.
		workspace = func(round, i int) (name string, owner, org int) {
			if i%2 == 0 {
				return fmt.Sprintf("ws-a-%d-caller-%d.csv", round, i), 101, orgA
			}
			return fmt.Sprintf("ws-b-%d-caller-%d.csv", round, i), 202, orgB
		}
	)

	im := New(Options{}, nil, nil)
	for round := 0; round < rounds; round++ {
		sessions, errs := raceAdmission(im, callers, func(i int) SessionOpt {
			name, owner, org := workspace(round, i)
			return SessionOpt{
				Mode:           ModeSubscribe,
				Filename:       name,
				OwnerUserID:    owner,
				OrganizationID: &org,
			}
		})

		winners := admissionResult(t, sessions, errs)
		if len(winners) != 1 {
			t.Fatalf("round %d admitted %d of %d concurrent callers, want exactly 1",
				round, len(winners), callers)
		}

		winner := winners[0]
		winnerName, winnerOwner, winnerOrg := workspace(round, winner)

		// This is the read the handler's requireImportAccess performs; the loser
		// must see the winner's import, never its own workspace.
		stats := im.GetStats()
		if stats.Status != StatusImporting || stats.Name != winnerName ||
			stats.OwnerUserID != winnerOwner || stats.OrganizationID != winnerOrg {
			t.Fatalf("round %d: stats %+v, want the winner's %q (owner %d, org %d)",
				round, stats, winnerName, winnerOwner, winnerOrg)
		}

		for _, i := range admissionResultLosers(callers, winners) {
			loserName, _, loserOrg := workspace(round, i)

			// The session the loser supplied is not the one the singleton
			// published: a refused caller can never be the holder.
			if stats.Name == loserName {
				t.Fatalf("round %d: refused caller %d observes its own import %q as running",
					round, i, loserName)
			}

			// A caller from a different workspace than the winner's must never
			// observe its own workspace as the owner of the running import.
			if loserOrg != winnerOrg && stats.OrganizationID == loserOrg {
				t.Fatalf("round %d: caller %d from workspace %d observes its own workspace as the running import's owner",
					round, i, loserOrg)
			}
		}

		// Only the winner's log line may reach the session-wide log buffer.
		logs := string(im.GetLogs())
		if !strings.Contains(logs, winnerName) {
			t.Fatalf("round %d: the winner's log line is missing: %q", round, logs)
		}
		for i := 0; i < callers; i++ {
			if i == winner {
				continue
			}
			name, _, _ := workspace(round, i)
			if strings.Contains(logs, name) {
				t.Fatalf("round %d: refused caller %d wrote %q to the shared log: %q", round, i, name, logs)
			}
		}

		im.setStatus(StatusFinished)
	}
}

// admissionResultLosers returns every caller index that is not a winner.
func admissionResultLosers(callers int, winners []int) []int {
	admitted := make(map[int]bool, len(winners))
	for _, w := range winners {
		admitted[w] = true
	}

	losers := make([]int, 0, callers-len(winners))
	for i := 0; i < callers; i++ {
		if !admitted[i] {
			losers = append(losers, i)
		}
	}

	return losers
}

// TestNewSessionDrainsStaleStopSignal covers the session setup that has to
// survive the atomic admission: a signal left buffered by a stopped loader must
// not be carried into the next session.
func TestNewSessionDrainsStaleStopSignal(t *testing.T) {
	im := New(Options{}, nil, nil)

	// A stopped loader can leave its non-blocking signal buffered.
	im.stop <- true

	sess, err := im.NewSession(SessionOpt{Mode: ModeSubscribe, Filename: "after-stop.csv"})
	if err != nil || sess == nil {
		t.Fatalf("expected a session after a stop: %v", err)
	}

	select {
	case <-im.stop:
		t.Fatal("a stale stop signal from the previous session reached the new one")
	default:
	}
	select {
	case <-im.Done():
		t.Fatal("expected the new session to report no completion")
	default:
	}
	if got := im.GetStats(); got.Status != StatusImporting || got.Name != "after-stop.csv" {
		t.Fatalf("unexpected stats for the new session: %+v", got)
	}
	if logs := string(im.GetLogs()); !strings.Contains(logs, "processing 'after-stop.csv'") {
		t.Fatalf("the new session's own log buffer was not published: %q", logs)
	}
}
