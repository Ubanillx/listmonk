package bounce

import (
	"sort"
	"strings"
	"time"

	"github.com/knadh/listmonk/models"
)

// This file aggregates the bounces the writer has persisted into one summary
// per workspace per flush window.
//
// The per-message truth stays in the bounces table; audit_events is a business
// operation log, so a bulk bounce run must not turn into one audit row per
// bounce. Everything here is optional: when Opt.RecordBounceBatchCB is nil the
// manager keeps no aggregation state at all and the writer behaves exactly as
// it did before aggregation existed.
const (
	// aggregateWorkspaceCap flushes the pending summaries once any single
	// workspace has this many pending bounces, so a flood is reported promptly
	// and no entry grows past it.
	aggregateWorkspaceCap = 500

	// aggregatePendingCap flushes every workspace once this many bounces are
	// pending in total. Together with aggregateWorkspaceCap and
	// aggregateWorkspaceCountCap it bounds the aggregator's memory
	// independently of the queue size: a single flooding workspace can never
	// grow the state without bound.
	aggregatePendingCap = 2000

	// aggregateWorkspaceCountCap bounds the number of workspaces tracked at
	// once. It is orders of magnitude above a realistic workspace count;
	// reaching it means the traffic is not attributable to a small, stable set
	// of workspaces, so the pending summaries are flushed and the state is
	// reset rather than kept.
	aggregateWorkspaceCountCap = 256

	// fallbackOrganizationID is the personal workspace. A bounce whose
	// organization the writer cannot resolve is summarized here instead of
	// being dropped, so the audit trail still accounts for every processed
	// bounce.
	fallbackOrganizationID = 0

	// aggregateClassificationCap bounds how many distinct type/source labels a
	// summary reports. Real traffic uses a handful (hard/soft/complaint and a
	// few provider names); the cap keeps a producer that sends arbitrary type
	// or source strings from inflating one audit row. Counts stay exact, only
	// the label list is truncated.
	aggregateClassificationCap = 16
)

// aggregateFlushInterval is how long a summary may wait before it is recorded.
// It is a variable so tests can shrink it.
var aggregateFlushInterval = 5 * time.Second

// BounceBatch is one workspace's aggregated bounce summary. It is what the
// optional Opt.RecordBounceBatchCB receives, and it deliberately carries only
// counts and classifications: never a recipient address, message body, header
// or provider payload.
type BounceBatch struct {
	OrganizationID int64     `json:"organization_id"`
	Count          int       `json:"count"`
	Types          []string  `json:"types,omitempty"`
	Sources        []string  `json:"sources,omitempty"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
}

// pendingBounceBatch accumulates one workspace's bounces between flushes.
type pendingBounceBatch struct {
	count   int
	types   map[string]struct{}
	sources map[string]struct{}
	first   time.Time
	last    time.Time
}

// observe folds one bounce into the summary.
func (p *pendingBounceBatch) observe(b models.Bounce, at time.Time) {
	if p.types == nil {
		p.types = make(map[string]struct{}, 3)
		p.sources = make(map[string]struct{}, 3)
	}

	if bounceType := strings.TrimSpace(b.Type); bounceType != "" && len(p.types) < aggregateClassificationCap {
		p.types[bounceType] = struct{}{}
	}
	if source := strings.TrimSpace(b.Source); source != "" && len(p.sources) < aggregateClassificationCap {
		p.sources[source] = struct{}{}
	}

	if p.count == 0 || at.Before(p.first) {
		p.first = at
	}
	if at.After(p.last) {
		p.last = at
	}
	p.count++
}

// batch renders the accumulated summary. Classifications are sorted so the
// audit metadata is stable regardless of the order the bounces arrived in.
func (p *pendingBounceBatch) batch(organizationID int64) BounceBatch {
	return BounceBatch{
		OrganizationID: organizationID,
		Count:          p.count,
		Types:          sortedSet(p.types),
		Sources:        sortedSet(p.sources),
		FirstSeen:      p.first,
		LastSeen:       p.last,
	}
}

// bounceAggregator accumulates per-workspace summaries between flushes.
//
// It is not safe for concurrent use and does not need to be: the manager's
// single writer goroutine is the only producer, and the same goroutine performs
// the flushes, so no extra goroutines or locks are needed for it.
type bounceAggregator struct {
	pending map[int64]*pendingBounceBatch
	total   int
}

// newBounceAggregator returns an empty aggregator.
func newBounceAggregator() *bounceAggregator {
	return &bounceAggregator{pending: make(map[int64]*pendingBounceBatch)}
}

// add folds one persisted bounce into the pending summaries. It returns the
// summaries that are due immediately because a cap was reached, or nil when the
// window is still open.
func (a *bounceAggregator) add(b models.Bounce) []BounceBatch {
	if a == nil {
		return nil
	}

	at := b.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}

	if a.pending == nil {
		a.pending = make(map[int64]*pendingBounceBatch)
	}

	organizationID := bounceOrganizationID(b)
	entry, ok := a.pending[organizationID]
	if !ok {
		// Bound the number of tracked workspaces. The flushed summaries are
		// recorded first; the bounce being added starts the next window.
		var flushed []BounceBatch
		if len(a.pending) >= aggregateWorkspaceCountCap {
			flushed = a.drain()
		}
		entry = &pendingBounceBatch{}
		a.pending[organizationID] = entry
		entry.observe(b, at)
		a.total++
		return flushed
	}

	entry.observe(b, at)
	a.total++

	if entry.count >= aggregateWorkspaceCap || a.total >= aggregatePendingCap {
		return a.drain()
	}
	return nil
}

// drain returns every pending summary, ordered by organization id, and resets
// the state.
func (a *bounceAggregator) drain() []BounceBatch {
	if a == nil || len(a.pending) == 0 {
		return nil
	}

	organizationIDs := make([]int64, 0, len(a.pending))
	for organizationID := range a.pending {
		organizationIDs = append(organizationIDs, organizationID)
	}
	sort.Slice(organizationIDs, func(i, j int) bool { return organizationIDs[i] < organizationIDs[j] })

	out := make([]BounceBatch, 0, len(organizationIDs))
	for _, organizationID := range organizationIDs {
		out = append(out, a.pending[organizationID].batch(organizationID))
	}

	a.pending = make(map[int64]*pendingBounceBatch)
	a.total = 0

	return out
}

// bounceOrganizationID reports the workspace a persisted bounce belongs to.
//
// It reads the same organization the write path already resolves and records on
// the bounce (bounces.source_organization_id) instead of applying a second
// resolution rule of its own. Producers that cannot supply one (the webhook
// path carries no workspace today) are summarized under the personal
// workspace, never dropped.
func bounceOrganizationID(b models.Bounce) int64 {
	if b.SourceOrganizationID.Valid && b.SourceOrganizationID.Int > 0 {
		return int64(b.SourceOrganizationID.Int)
	}
	return fallbackOrganizationID
}

// sortedSet returns a set's members in ascending order. An empty set yields a
// nil slice so callers can omit the field entirely.
func sortedSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}

	out := make([]string, 0, len(set))
	for member := range set {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}

// flushBounces records aggregated summaries. A summary that cannot be recorded
// is logged rather than dropped silently; persistence of the bounce itself has
// already succeeded at this point, so a failure here never changes the business
// result.
func (m *Manager) flushBounces(batches []BounceBatch) {
	if len(batches) == 0 || m.opt.RecordBounceBatchCB == nil {
		return
	}

	for _, batch := range batches {
		if batch.Count == 0 {
			continue
		}
		if err := m.opt.RecordBounceBatchCB(batch); err != nil && m.log != nil {
			m.log.Printf("error recording %d aggregated bounces for organization %d: %v",
				batch.Count, batch.OrganizationID, err)
		}
	}
}
