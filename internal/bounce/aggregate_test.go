package bounce

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

// aggregateTestBounce builds a bounce the way the writer receives it: the
// fields the producers set plus the organization the write path resolved. An
// organization of 0 leaves the field unset, which is what an unattributable
// bounce looks like.
func aggregateTestBounce(organizationID int64, source, bounceType string, at time.Time) models.Bounce {
	b := models.Bounce{
		Type:      bounceType,
		Source:    source,
		CreatedAt: at,
	}
	if organizationID > 0 {
		b.SourceOrganizationID = null.Int{Int: int(organizationID), Valid: true}
	}
	return b
}

// newAggregateTestManager builds a manager through the real constructor so the
// tests cover the optional-hook wiring as well as the flush loop.
func newAggregateTestManager(t *testing.T, batchCB func(BounceBatch) error) *Manager {
	t.Helper()

	m, err := New(Opt{
		RecordBounceCB:      func(models.Bounce) error { return nil },
		RecordBounceBatchCB: batchCB,
	}, &Queries{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("unexpected initialization error: %v", err)
	}
	return m
}

// shrinkAggregateFlushInterval makes the flush window test-friendly and restores
// it afterwards.
func shrinkAggregateFlushInterval(t *testing.T, interval time.Duration) {
	t.Helper()

	prev := aggregateFlushInterval
	aggregateFlushInterval = interval
	t.Cleanup(func() { aggregateFlushInterval = prev })
}

// TestBounceAggregateSplitsByWorkspace covers the core requirement: bounces are
// summarized per workspace, and a bounce whose workspace the write path could
// not resolve is summarized under the personal workspace instead of dropped.
func TestBounceAggregateSplitsByWorkspace(t *testing.T) {
	now := time.Now()
	agg := newBounceAggregator()

	for _, b := range []models.Bounce{
		aggregateTestBounce(7, "ses", models.BounceTypeHard, now),
		aggregateTestBounce(42, "postmark", models.BounceTypeSoft, now.Add(time.Second)),
		aggregateTestBounce(7, "sendgrid", models.BounceTypeHard, now.Add(2*time.Second)),
		aggregateTestBounce(0, "ses", models.BounceTypeComplaint, now.Add(3*time.Second)),
	} {
		if flushed := agg.add(b); len(flushed) != 0 {
			t.Fatalf("the window flushed before it closed: %+v", flushed)
		}
	}

	batches := agg.drain()
	if len(batches) != 3 {
		t.Fatalf("expected three workspace summaries, got %d: %+v", len(batches), batches)
	}

	// drain reports workspaces in ascending id order, so the flush itself is
	// deterministic.
	if got := batches[0]; got.OrganizationID != fallbackOrganizationID || got.Count != 1 {
		t.Fatalf("expected the unattributable bounce under workspace %d, got %+v", fallbackOrganizationID, got)
	}
	if got := batches[1]; got.OrganizationID != 7 || got.Count != 2 {
		t.Fatalf("expected two bounces for workspace 7, got %+v", got)
	}
	if got := batches[2]; got.OrganizationID != 42 || got.Count != 1 {
		t.Fatalf("expected one bounce for workspace 42, got %+v", got)
	}

	if got := batches[1]; !got.FirstSeen.Equal(now) || !got.LastSeen.Equal(now.Add(2*time.Second)) {
		t.Fatalf("expected the workspace window to span its bounces, got %v..%v", got.FirstSeen, got.LastSeen)
	}

	if pending := agg.drain(); len(pending) != 0 {
		t.Fatalf("expected the drain to reset the state, got %+v", pending)
	}
}

// TestBounceAggregateAccumulatesOneEventPerWorkspace proves that repeated
// bounces for one workspace become a single summary rather than one event per
// bounce, and that the summary carries no recipient-identifying data.
func TestBounceAggregateAccumulatesOneEventPerWorkspace(t *testing.T) {
	now := time.Now()
	agg := newBounceAggregator()

	const total = 25
	for i := 0; i < total; i++ {
		b := aggregateTestBounce(11, "ses", models.BounceTypeSoft, now.Add(time.Duration(i)*time.Second))
		b.Email = "recipient@example.com"
		b.Meta = json.RawMessage(`{"email":"recipient@example.com","diagnostic":"550 mailbox unavailable"}`)
		if flushed := agg.add(b); len(flushed) != 0 {
			t.Fatalf("the window flushed early at bounce %d: %+v", i, flushed)
		}
	}

	batches := agg.drain()
	if len(batches) != 1 {
		t.Fatalf("expected one summary for one workspace, got %d: %+v", len(batches), batches)
	}

	batch := batches[0]
	if batch.Count != total {
		t.Fatalf("expected all %d bounces in one summary, got %d", total, batch.Count)
	}
	if batch.OrganizationID != 11 {
		t.Fatalf("expected workspace 11, got %d", batch.OrganizationID)
	}
	if !batch.FirstSeen.Equal(now) || !batch.LastSeen.Equal(now.Add((total-1)*time.Second)) {
		t.Fatalf("unexpected window %v..%v", batch.FirstSeen, batch.LastSeen)
	}

	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	for _, leak := range []string{"recipient@example.com", "mailbox unavailable"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("summary leaked %q: %s", leak, encoded)
		}
	}
}

// TestBounceAggregateSortsClassifications locks the bookkeeping: distinct types
// and sources are reported once each, in a stable order, no matter what order
// the bounces arrived in.
func TestBounceAggregateSortsClassifications(t *testing.T) {
	now := time.Now()
	orders := [][]struct{ source, bounceType string }{
		{
			{"ses", models.BounceTypeSoft},
			{"postmark", models.BounceTypeHard},
			{"ses", models.BounceTypeComplaint},
			{"postmark", models.BounceTypeSoft},
		},
		{
			{"postmark", models.BounceTypeSoft},
			{"ses", models.BounceTypeComplaint},
			{"postmark", models.BounceTypeHard},
			{"ses", models.BounceTypeSoft},
		},
	}

	for i, order := range orders {
		agg := newBounceAggregator()
		for j, entry := range order {
			agg.add(aggregateTestBounce(3, entry.source, entry.bounceType, now.Add(time.Duration(j)*time.Second)))
		}

		batches := agg.drain()
		if len(batches) != 1 {
			t.Fatalf("order %d: expected one summary, got %d", i, len(batches))
		}

		if want := []string{"complaint", "hard", "soft"}; !slices.Equal(batches[0].Types, want) {
			t.Fatalf("order %d: expected types %v, got %v", i, want, batches[0].Types)
		}
		if want := []string{"postmark", "ses"}; !slices.Equal(batches[0].Sources, want) {
			t.Fatalf("order %d: expected sources %v, got %v", i, want, batches[0].Sources)
		}
	}
}

// TestBounceAggregateCapsBoundPendingState proves the aggregation state cannot
// grow without bound: the per-workspace cap, the pending cap and the tracked
// workspace cap all flush, and no bounce is lost in the process.
func TestBounceAggregateCapsBoundPendingState(t *testing.T) {
	t.Run("per-workspace cap", func(t *testing.T) {
		agg := newBounceAggregator()

		var flushed []BounceBatch
		for i := 0; i < aggregateWorkspaceCap; i++ {
			flushed = append(flushed, agg.add(aggregateTestBounce(4, "ses", models.BounceTypeHard, time.Time{}))...)
		}

		if len(flushed) != 1 {
			t.Fatalf("expected exactly one cap flush, got %d: %+v", len(flushed), flushed)
		}
		if flushed[0].Count != aggregateWorkspaceCap || flushed[0].OrganizationID != 4 {
			t.Fatalf("expected one summary of %d bounces for workspace 4, got %+v", aggregateWorkspaceCap, flushed[0])
		}
		if pending, total := len(agg.pending), agg.total; pending != 0 || total != 0 {
			t.Fatalf("expected the cap flush to reset the state, got %d workspaces / %d bounces", pending, total)
		}

		// The next bounce opens a new window instead of extending the flush.
		if extra := agg.add(aggregateTestBounce(4, "ses", models.BounceTypeHard, time.Time{})); len(extra) != 0 {
			t.Fatalf("expected a fresh window after the cap flush, got %+v", extra)
		}
	})

	t.Run("pending cap", func(t *testing.T) {
		agg := newBounceAggregator()

		flushedCount := 0
		flushes := 0
		const bounces = aggregatePendingCap * 2
		for i := 0; i < bounces; i++ {
			// Ten workspaces, each far below the per-workspace cap, so only the
			// total cap can fire.
			batches := agg.add(aggregateTestBounce(int64(i%10+1), "ses", models.BounceTypeHard, time.Time{}))
			for _, batch := range batches {
				flushes++
				flushedCount += batch.Count
			}
			if agg.total > aggregatePendingCap {
				t.Fatalf("pending total %d exceeded the cap %d", agg.total, aggregatePendingCap)
			}
			if len(agg.pending) > aggregateWorkspaceCountCap {
				t.Fatalf("tracked workspaces %d exceeded the cap %d", len(agg.pending), aggregateWorkspaceCountCap)
			}
		}

		if flushes == 0 {
			t.Fatal("expected the pending cap to flush")
		}
		if outstanding := flushedCount + agg.total; outstanding != bounces {
			t.Fatalf("expected %d accounted bounces, got %d flushed + %d pending", bounces, flushedCount, agg.total)
		}
	})

	t.Run("tracked workspace cap", func(t *testing.T) {
		agg := newBounceAggregator()

		flushedCount := 0
		for i := 0; i <= aggregateWorkspaceCountCap; i++ {
			batches := agg.add(aggregateTestBounce(int64(i+1), "ses", models.BounceTypeHard, time.Time{}))
			for _, batch := range batches {
				flushedCount += batch.Count
			}
			if len(agg.pending) > aggregateWorkspaceCountCap {
				t.Fatalf("tracked workspaces %d exceeded the cap %d", len(agg.pending), aggregateWorkspaceCountCap)
			}
		}

		if flushedCount == 0 {
			t.Fatal("expected the workspace cap to flush the pending summaries")
		}
	})

	t.Run("classification label cap", func(t *testing.T) {
		agg := newBounceAggregator()

		const bounces = aggregateClassificationCap * 3
		for i := 0; i < bounces; i++ {
			agg.add(aggregateTestBounce(6, fmt.Sprintf("source-%02d", i), models.BounceTypeHard, time.Time{}))
		}

		batches := agg.drain()
		if len(batches) != 1 {
			t.Fatalf("expected one summary, got %d", len(batches))
		}
		if batches[0].Count != bounces {
			t.Fatalf("expected the count to stay exact (%d), got %d", bounces, batches[0].Count)
		}
		if len(batches[0].Sources) > aggregateClassificationCap {
			t.Fatalf("expected at most %d sources, got %d", aggregateClassificationCap, len(batches[0].Sources))
		}
		if len(batches[0].Sources) != aggregateClassificationCap {
			t.Fatalf("expected the first %d sources to be reported, got %v", aggregateClassificationCap, batches[0].Sources)
		}
	})
}

// TestManagerFlushesAggregateOnWindow covers the time-based flush through the
// real write loop: the window fires once, with the full counts.
func TestManagerFlushesAggregateOnWindow(t *testing.T) {
	shrinkAggregateFlushInterval(t, 20*time.Millisecond)

	batches := make(chan BounceBatch, 8)
	m := newAggregateTestManager(t, func(batch BounceBatch) error {
		batches <- batch
		return nil
	})

	done := make(chan struct{})
	go func() {
		m.Run()
		close(done)
	}()

	for i := 0; i < 3; i++ {
		if err := m.Record(aggregateTestBounce(5, "ses", models.BounceTypeHard, time.Time{})); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
	}

	var flushed BounceBatch
	select {
	case flushed = <-batches:
	case <-time.After(10 * time.Second):
		t.Fatal("the flush window never fired")
	}
	if flushed.OrganizationID != 5 || flushed.Count != 3 {
		t.Fatalf("expected one summary of three bounces for workspace 5, got %+v", flushed)
	}

	// Exactly once: an empty window must not emit another summary.
	select {
	case extra := <-batches:
		t.Fatalf("the window flushed twice: %+v", extra)
	case <-time.After(4 * aggregateFlushInterval):
	}

	close(m.queue)
	<-done
}

// TestManagerFlushesAggregateOnSizeCap covers the cap flush through the write
// loop with a window long enough that only the cap can be responsible.
func TestManagerFlushesAggregateOnSizeCap(t *testing.T) {
	shrinkAggregateFlushInterval(t, time.Hour)

	batches := make(chan BounceBatch, 8)
	m := newAggregateTestManager(t, func(batch BounceBatch) error {
		batches <- batch
		return nil
	})

	done := make(chan struct{})
	go func() {
		m.Run()
		close(done)
	}()

	for i := 0; i < aggregateWorkspaceCap; i++ {
		if err := m.Record(aggregateTestBounce(9, "postmark", models.BounceTypeSoft, time.Time{})); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
	}

	var flushed BounceBatch
	select {
	case flushed = <-batches:
	case <-time.After(10 * time.Second):
		t.Fatal("the size cap never fired")
	}
	if flushed.OrganizationID != 9 || flushed.Count != aggregateWorkspaceCap {
		t.Fatalf("expected one summary of %d bounces for workspace 9, got %+v", aggregateWorkspaceCap, flushed)
	}

	select {
	case extra := <-batches:
		t.Fatalf("the cap flushed twice: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}

	close(m.queue)
	<-done

	select {
	case extra := <-batches:
		t.Fatalf("the stop flush duplicated the cap flush: %+v", extra)
	default:
	}
}

// TestManagerFlushesAggregateOnStop covers the stop path: with a window that
// cannot fire, closing the queue must still record the remainder.
func TestManagerFlushesAggregateOnStop(t *testing.T) {
	shrinkAggregateFlushInterval(t, time.Hour)

	batches := make(chan BounceBatch, 8)
	m := newAggregateTestManager(t, func(batch BounceBatch) error {
		batches <- batch
		return nil
	})

	done := make(chan struct{})
	go func() {
		m.Run()
		close(done)
	}()

	for i := 0; i < 2; i++ {
		if err := m.Record(aggregateTestBounce(8, "sendgrid", models.BounceTypeComplaint, time.Time{})); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
	}

	close(m.queue)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop when the queue closed")
	}

	select {
	case flushed := <-batches:
		if flushed.OrganizationID != 8 || flushed.Count != 2 {
			t.Fatalf("expected the remainder of two bounces for workspace 8, got %+v", flushed)
		}
	default:
		t.Fatal("the stop flush did not record the pending summary")
	}
}

// TestManagerWithoutAggregateCallback covers the optional hook: with no batch
// callback the manager keeps no aggregation state and the writer behaves
// exactly as it did before aggregation existed.
func TestManagerWithoutAggregateCallback(t *testing.T) {
	persisted := make(chan models.Bounce, 4)
	m, err := New(Opt{
		RecordBounceCB: func(b models.Bounce) error {
			persisted <- b
			return nil
		},
	}, &Queries{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("unexpected initialization error: %v", err)
	}

	if m.aggregator != nil {
		t.Fatal("expected no aggregation state without a batch callback")
	}
	if m.opt.RecordBounceBatchCB != nil {
		t.Fatal("expected no batch callback")
	}

	done := make(chan struct{})
	go func() {
		m.Run()
		close(done)
	}()

	const bounces = 3
	for i := 0; i < bounces; i++ {
		if err := m.Record(aggregateTestBounce(5, "ses", models.BounceTypeHard, time.Time{})); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
	}
	for i := 0; i < bounces; i++ {
		select {
		case <-persisted:
		case <-time.After(10 * time.Second):
			t.Fatalf("the writer persisted only %d of %d bounces", i, bounces)
		}
	}
	if extra := len(persisted); extra != 0 {
		t.Fatalf("expected one persistence call per bounce, got %d extra", extra)
	}

	close(m.queue)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop when the queue closed")
	}
}

// TestManagerLogsUnrecordableAggregate covers the "drop nothing silently" rule:
// a summary that cannot be recorded is logged.
func TestManagerLogsUnrecordableAggregate(t *testing.T) {
	var buf bytes.Buffer
	m := &Manager{
		opt: Opt{
			RecordBounceBatchCB: func(BounceBatch) error { return errors.New("audit store down") },
		},
		log: log.New(&buf, "", 0),
	}

	m.flushBounces([]BounceBatch{{OrganizationID: 3, Count: 2}})

	if logged := buf.String(); !strings.Contains(logged, "audit store down") {
		t.Fatalf("expected the failure to be logged, got %q", logged)
	}
}
