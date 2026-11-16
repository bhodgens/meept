package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
)

// stateTopicSub is one of C2's five typed state topics plus the
// subscriber id the projector registers on it.
type stateTopicSub struct {
	topic bus.Topic[StateEvent]
	name  string
}

// projectorStateTopics is the frozen C2 surface — every work-item
// machine that publishes transitions.
func projectorStateTopics() []stateTopicSub {
	return []stateTopicSub{
		{TopicTaskState, "kanban-projector-task"},
		{TopicJobState, "kanban-projector-job"},
		{TopicPlanState, "kanban-projector-plan"},
		{TopicStepState, "kanban-projector-step"},
		{TopicGoalState, "kanban-projector-goal"},
	}
}

// ProvRow is the provenance the projector reads from a source work-item
// row at projection time (C1: the StateEvent carries identity only — the
// card's GoalID/EmployeeID/PlanID/SessionID/TriggerRef fields come from
// the machine's own store, keyed by StateEvent.ID).
type ProvRow struct {
	Title      string
	GoalID     string
	EmployeeID string
	PlanID     string
	SessionID  string
	TriggerRef string
}

// RowFetcher is the read-only seam between the projector and the five
// work-item machines. internal/kanban may NEVER import internal/task,
// internal/queue, internal/plan, internal/employee or internal/tui, so
// the daemon wiring (07/01) supplies an implementation that closes over
// those stores. Every method is read-only: the projector writes nothing
// back to the source machines.
type RowFetcher interface {
	TaskRow(id string) (ProvRow, error)
	JobRow(id string) (ProvRow, error)
	PlanRow(id string) (ProvRow, error)
	StepRow(id string) (ProvRow, error)
	GoalRow(id string) (ProvRow, error)
}

// noopFetcher is the nil-safe default: a zero ProvRow makes the projector
// fall back to a generated card title and empty provenance, which is
// byte-identical to "source row not found" — projection never dies on a
// missing row.
type noopFetcher struct{}

func (noopFetcher) TaskRow(string) (ProvRow, error) { return ProvRow{}, nil }
func (noopFetcher) JobRow(string) (ProvRow, error)  { return ProvRow{}, nil }
func (noopFetcher) PlanRow(string) (ProvRow, error) { return ProvRow{}, nil }
func (noopFetcher) StepRow(string) (ProvRow, error) { return ProvRow{}, nil }
func (noopFetcher) GoalRow(string) (ProvRow, error) { return ProvRow{}, nil }

// defaultQueueSize bounds the handoff buffer between the (fast-returning)
// bus subscriber callbacks and the serialized projection worker. A full
// queue drops + counts, never blocks — same posture as the bus itself.
const defaultQueueSize = 256

// stopSettleWindow caps Stop()'s wait for subscriber reader goroutines
// to finish handing off in-flight events before the worker's final
// drain.
const stopSettleWindow = 100 * time.Millisecond

// ProjectorStats is the projector's monotonic counters, exported for the
// status surface (05-surfaces) and tests. WIPOverflow is the WIP posture
// counter: projection writes that landed on a column already at its WIP
// limit (see handleProjected for why projection never refuses).
type ProjectorStats struct {
	Processed   int64 // coalesced projection writes handled
	Dropped     int64 // events dropped at a full queue
	WIPOverflow int64 // projection writes past a column's WIP limit
	Failed      int64 // projection writes that errored
}

// Projector subscribes to the five committed kanban.event.*.state topics
// (C2) and maintains one canonical board card per work item (master.md:
// board == rebuildable projection). Design:
//
//   - Subscriber callbacks only hand the StateEvent to a bounded queue and
//     return; a serialized worker goroutine does all store I/O. The
//     publisher can never block on a projection write, and SQLite writes
//     stay linearised through one goroutine (the store is
//     single-connection by design).
//   - Burst coalescence: the worker drains the queue greedily and merges
//     per work item (Kind, ID), last state wins — a five-transition burst
//     becomes one projection write against the final mapping.
//   - Lease rule A reading (master.md decision 1): the refusal applies to
//     HUMAN moves ("a human card move onto a column mapped to an active
//     state of a work item holding a live claim is REFUSED"). The
//     projector is the machine lane: it mirrors transitions the machines
//     already committed — including legitimate claim acquisitions — so it
//     performs NO lease check. Lease enforcement lives in the human
//     command path (03-board-projection/03).
//   - WIP posture: a projection move that would exceed the target
//     column's WIP limit is NOT dropped and NOT blocked: the write lands,
//     a Warn is logged, and WIPOverflow increments. Rationale: the board
//     must equal the source (contract C2/master decision 2); silently
//     parking an over-WIP card would make board != source. wip_mode
//     (C9) governs the human move path, enforced by 03/03.
//   - Unmapped states: no (Kind, FromState) rule match creates no card
//     ("no card until the first mapped transition") and never moves or
//     deletes an existing card — the card parks in its last mapped
//     column until a later rule matches. Terminal-cleanup of stale parked
//     cards is reconcile's job (03/02).
type Projector struct {
	bus     *bus.MessageBus
	store   *Store
	rules   ProjectionRules
	fetcher RowFetcher
	logger  *slog.Logger

	queue  chan StateEvent
	subs   []*bus.Subscriber
	stopCh chan struct{}
	done   chan struct{}

	// mu guards ONLY the counters below — no I/O happens under it
	// (mutexio invariant). The worker goroutine is the sole writer of
	// cards, so no store-ordering lock is needed.
	mu        sync.Mutex
	processed int64
	dropped   int64
	overflow  int64
	failed    int64
	stopOnce  sync.Once
	unsubOnce sync.Once
}

// ProjectorOption tunes NewProjector; each setter is nil/value-guarded.
type ProjectorOption func(*Projector)

// WithQueueSize sets the bounded handoff buffer. size <= 0 keeps the
// default.
func WithQueueSize(size int) ProjectorOption {
	return func(p *Projector) {
		if p == nil || size <= 0 {
			return
		}
		p.queue = make(chan StateEvent, size)
	}
}

// WithLogger sets the projector's logger; nil keeps slog.Default().
func WithLogger(logger *slog.Logger) ProjectorOption {
	return func(p *Projector) {
		if p == nil || logger == nil {
			return
		}
		p.logger = logger
	}
}

// NewProjector wires the projector to the bus and store and STARTS it
// (the leaf contract is "construct and it projects"). Guards: a nil bus
// or nil store is a constructor error; a nil fetcher falls back to the
// nil-safe no-op fetcher; an empty rules snapshot is legal and means "no
// transition ever maps" (everything parks).
func NewProjector(b *bus.MessageBus, store *Store, rules ProjectionRules, fetcher RowFetcher, opts ...ProjectorOption) (*Projector, error) {
	if b == nil {
		return nil, fmt.Errorf("projector: bus is required")
	}
	if store == nil {
		return nil, fmt.Errorf("projector: store is required")
	}
	p := &Projector{
		bus:     b,
		store:   store,
		rules:   rules,
		fetcher: fetcher,
		logger:  slog.Default(),
		queue:   make(chan StateEvent, defaultQueueSize),
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}
	if p.fetcher == nil {
		p.fetcher = noopFetcher{}
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}

	// C2's five typed topics. SubscribeT hands each message to fn on
	// its own reader goroutine, so enqueue (select-default) keeps the
	// callback O(1) and never blocks the publisher chain.
	for _, ts := range projectorStateTopics() {
		sub := bus.SubscribeT(b, ts.name, ts.topic, p.enqueue)
		if sub == nil {
			p.Stop()
			return nil, fmt.Errorf("projector: subscribe %s: bus returned a nil subscriber", ts.topic.Name)
		}
		p.subs = append(p.subs, sub)
	}

	go p.run()
	return p, nil
}

// enqueue is the subscriber callback: non-blocking handoff, drop+count on
// a full queue, never any I/O.
func (p *Projector) enqueue(ev StateEvent) {
	select {
	case <-p.stopCh:
		// Post-stop events are dropped without counting: they arrive
		// during the unsubscribe window and stopping already promises a
		// drained queue.
		return
	default:
	}
	select {
	case p.queue <- ev:
	default:
		p.mu.Lock()
		p.dropped++
		p.mu.Unlock()
		p.logger.Warn("kanban projector: queue full, dropping state event",
			"kind", ev.Kind, "id", ev.ID, "from", ev.From, "to", ev.To)
	}
}

// workKey identifies a canonical card's work item (one card per work
// item, master.md).
type workKey struct{ kind, id string }

// run is the serialized projection worker: one event at a time, each
// followed by a greedy non-blocking drain that coalesces per work item
// (last state wins) before any store write happens.
func (p *Projector) run() {
	defer close(p.done)
	for {
		select {
		case ev := <-p.queue:
			batch := p.newBatch(ev)
			// Greedy non-blocking drain: everything already handed off
			// joins this batch, merged per work item (last state wins),
			// so a burst costs one projection write per item instead of
			// one per transition. Events that arrive after the drain are
			// handled by the next pass — order is preserved either way
			// because this is the only writer.
			draining := true
			for draining {
				select {
				case next := <-p.queue:
					p.merge(batch, next)
				default:
					draining = false
				}
			}
			p.handleBatch(batch)
		case <-p.stopCh:
			// Drain what the callbacks already handed off, coalescing
			// the same way, so Stop never loses a projection the bus
			// already delivered.
			var batch map[workKey]StateEvent
			for {
				select {
				case ev := <-p.queue:
					if batch == nil {
						batch = p.newBatch(ev)
					} else {
						p.merge(batch, ev)
					}
				default:
					if batch != nil {
						p.handleBatch(batch)
					}
					return
				}
			}
		}
	}
}

func (p *Projector) newBatch(ev StateEvent) map[workKey]StateEvent {
	b := make(map[workKey]StateEvent, 8)
	p.merge(b, ev)
	return b
}

// merge keeps the newest event per work item. "Newest" is TS when both
// carry one (equal TS falls back to arrival order — the later arrival is
// the later transition); a zero TS always loses to a real one.
func (p *Projector) merge(batch map[workKey]StateEvent, ev StateEvent) {
	key := workKey{ev.Kind, ev.ID}
	prev, exists := batch[key]
	if !exists {
		batch[key] = ev
		return
	}
	if eventNewer(ev, prev) {
		batch[key] = ev
	}
}

func eventNewer(ev, prev StateEvent) bool {
	if ev.TS.IsZero() && prev.TS.IsZero() {
		// Neither carries a time: arrival order is the only signal — the
		// later arrival is the later transition.
		return true
	}
	if ev.TS.IsZero() {
		return false
	}
	if prev.TS.IsZero() {
		return true
	}
	return !ev.TS.Before(prev.TS)
}

// handleBatch projects a coalesced batch deterministically (map iteration
// order would otherwise make multi-item bursts nondeterministic under
// tests).
func (p *Projector) handleBatch(batch map[workKey]StateEvent) {
	ctx := context.Background()
	for _, key := range batchOrder(batch) {
		p.handleEvent(ctx, batch[key])
	}
}

// batchOrder sorts the batch by TS then key for a deterministic write
// sequence.
func batchOrder(batch map[workKey]StateEvent) []workKey {
	keys := make([]workKey, 0, len(batch))
	for k := range batch {
		keys = append(keys, k)
	}
	// insertion sort: batches are tiny (bounded by the burst size)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && batchOrderLess(batch[keys[j]], batch[keys[j-1]], keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func batchOrderLess(a, b StateEvent, ka, kb workKey) bool {
	if !a.TS.Equal(b.TS) && !a.TS.IsZero() && !b.TS.IsZero() {
		return a.TS.Before(b.TS)
	}
	if ka.kind != kb.kind {
		return ka.kind < kb.kind
	}
	return ka.id < kb.id
}

// handleEvent performs one projection write (or declines: no rule match
// parks, per the type doc). All I/O here runs OUTSIDE p.mu.
func (p *Projector) handleEvent(ctx context.Context, ev StateEvent) {
	if ev.Kind == "" || ev.ID == "" {
		p.countFailed()
		p.logger.Warn("kanban projector: state event missing identity", "kind", ev.Kind, "id", ev.ID)
		return
	}

	matches := p.rules.Matches(ev.Kind, ev.From)
	if len(matches) == 0 {
		// Unmapped transition: no card until the first mapped state;
		// an existing card parks where it stands.
		return
	}
	// Rules are a deterministic snapshot (LoadProjectionRules orders by
	// kind/from/id); on the rare multi-board ruleset the first match wins
	// — one canonical card can only live in one column, and reconcile
	// (03/02) owns per-board materialisation if that ever matters.
	rule := matches[0]

	card, err := p.store.GetCardByRef(ctx, ev.Kind, ev.ID)
	var unknown ErrUnknownCard
	if errors.As(err, &unknown) {
		if err := p.createCard(ctx, ev, rule); err != nil {
			p.countFailed()
			p.logger.Warn("kanban projector: create card failed",
				"kind", ev.Kind, "id", ev.ID, "column", rule.ColumnID, "error", err)
			return
		}
	} else if err != nil {
		p.countFailed()
		p.logger.Warn("kanban projector: card lookup failed",
			"kind", ev.Kind, "id", ev.ID, "error", err)
		return
	} else if card.ColumnID != rule.ColumnID {
		if err := p.moveCard(ctx, card, rule); err != nil {
			p.countFailed()
			p.logger.Warn("kanban projector: move card failed",
				"card", card.ID, "kind", ev.Kind, "id", ev.ID,
				"from", card.ColumnID, "to", rule.ColumnID, "error", err)
			return
		}
	} else {
		// Already in the mapped column: refresh provenance-only drift
		// (the source row may have gained goal/plan links since create).
		if err := p.refreshProvenance(ctx, card, ev); err != nil {
			p.countFailed()
			p.logger.Warn("kanban projector: provenance refresh failed",
				"card", card.ID, "error", err)
			return
		}
	}

	p.mu.Lock()
	p.processed++
	p.mu.Unlock()
}

// createCard materialises the canonical card with provenance read from
// the source row AT projection time (C1 — nothing provenance-like rides
// the event).
func (p *Projector) createCard(ctx context.Context, ev StateEvent, rule ProjectionRule) error {
	prov := p.fetchProv(ev.Kind, ev.ID)
	title := prov.Title
	if title == "" {
		title = fmt.Sprintf("%s:%s", ev.Kind, ev.ID)
	}
	count, err := p.store.countCards(ctx, rule.ColumnID)
	if err != nil {
		return fmt.Errorf("failed to size target column: %w", err)
	}
	p.checkWIP(ctx, rule, count)

	card := Card{
		ID:         id.Generate("card-"),
		BoardID:    rule.BoardID,
		ColumnID:   rule.ColumnID,
		Title:      title,
		Ref:        WorkRef{Kind: ev.Kind, ID: ev.ID},
		GoalID:     prov.GoalID,
		EmployeeID: prov.EmployeeID,
		PlanID:     prov.PlanID,
		SessionID:  prov.SessionID,
		TriggerRef: prov.TriggerRef,
		Order:      count,
	}
	if err := p.store.CreateCard(ctx, card); err != nil {
		return fmt.Errorf("failed to create card for %s/%s: %w", ev.Kind, ev.ID, err)
	}
	return nil
}

// moveCard re-targets the canonical card. Lease rule A is NOT applied
// here (machine lane — see the type doc). ErrStaleCard means a human
// moved the card concurrently between our read and the CAS write: the
// projection lane re-reads and forces the column via UpdateCard, because
// after a committed machine transition the mapped column IS the truth
// (board == source), and the human's intent against a stale view loses.
func (p *Projector) moveCard(ctx context.Context, card Card, rule ProjectionRule) error {
	count, err := p.store.countCards(ctx, rule.ColumnID)
	if err != nil {
		return fmt.Errorf("failed to size target column: %w", err)
	}
	p.checkWIP(ctx, rule, count)

	if err := p.store.MoveCard(ctx, card.ID, card.ColumnID, rule.ColumnID); err != nil {
		if errors.Is(err, ErrStaleCard) {
			fresh, getErr := p.store.GetCard(ctx, card.ID)
			if getErr != nil {
				return fmt.Errorf("failed to re-read card after stale move: %w", getErr)
			}
			if fresh.ColumnID == rule.ColumnID {
				return nil
			}
			fresh.ColumnID = rule.ColumnID
			if err := p.store.UpdateCard(ctx, fresh); err != nil {
				return fmt.Errorf("failed to force column after stale move: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to move card: %w", err)
	}
	return nil
}

// refreshProvenance re-reads the source row and writes only the
// provenance fields when they drifted; a fetch error is a no-op (the
// card stands with its last-known provenance).
func (p *Projector) refreshProvenance(ctx context.Context, card Card, ev StateEvent) error {
	prov := p.fetchProv(ev.Kind, ev.ID)
	if prov.GoalID == card.GoalID && prov.EmployeeID == card.EmployeeID &&
		prov.PlanID == card.PlanID && prov.SessionID == card.SessionID &&
		prov.TriggerRef == card.TriggerRef {
		return nil
	}
	card.GoalID = prov.GoalID
	card.EmployeeID = prov.EmployeeID
	card.PlanID = prov.PlanID
	card.SessionID = prov.SessionID
	card.TriggerRef = prov.TriggerRef
	if err := p.store.UpdateCard(ctx, card); err != nil {
		return fmt.Errorf("failed to write provenance: %w", err)
	}
	return nil
}

func (p *Projector) fetchProv(kind, itemID string) ProvRow {
	var (
		prov ProvRow
		err  error
	)
	switch kind {
	case EventKindTask:
		prov, err = p.fetcher.TaskRow(itemID)
	case EventKindJob:
		prov, err = p.fetcher.JobRow(itemID)
	case EventKindPlan:
		prov, err = p.fetcher.PlanRow(itemID)
	case EventKindStep:
		prov, err = p.fetcher.StepRow(itemID)
	case EventKindGoal:
		prov, err = p.fetcher.GoalRow(itemID)
	default:
		return ProvRow{}
	}
	if err != nil {
		p.logger.Warn("kanban projector: provenance fetch failed, projecting without it",
			"kind", kind, "id", itemID, "error", err)
		return ProvRow{}
	}
	return prov
}

// checkWIP logs + counts an over-limit landing. It never refuses: the
// projection must equal the source (see the type doc for the posture).
func (p *Projector) checkWIP(ctx context.Context, rule ProjectionRule, currentCount int) {
	var wip int
	columns, err := p.store.ListColumns(ctx, rule.BoardID)
	if err != nil {
		p.logger.Warn("kanban projector: column WIP lookup failed",
			"board", rule.BoardID, "column", rule.ColumnID, "error", err)
		return
	}
	for _, c := range columns {
		if c.ID == rule.ColumnID {
			wip = c.WIP
			break
		}
	}
	if wip <= 0 || currentCount < wip {
		return
	}
	p.mu.Lock()
	p.overflow++
	p.mu.Unlock()
	p.logger.Warn("kanban projector: projection exceeds column WIP limit",
		"board", rule.BoardID, "column", rule.ColumnID,
		"wip", wip, "count", currentCount)
}

func (p *Projector) countFailed() {
	p.mu.Lock()
	p.failed++
	p.mu.Unlock()
}

// Stats returns a snapshot of the projector counters. Nil-safe.
func (p *Projector) Stats() ProjectorStats {
	if p == nil {
		return ProjectorStats{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return ProjectorStats{
		Processed:   p.processed,
		Dropped:     p.dropped,
		WIPOverflow: p.overflow,
		Failed:      p.failed,
	}
}

// Stop unsubscribes from all five topics, waits briefly for the
// SubscribeT reader goroutines to finish handing off events they were
// mid-delivery on, then signals the worker and blocks until the handoff
// queue is drained. Idempotent; safe on a nil Projector (nil guard
// convention for lifecycle methods).
//
// The settle window is best-effort by design: the bus's own drop-on-
// close semantics can still lose an event that never reached p.queue
// under adversarial timing, and the board's authoritative recovery
// there is reconcile (03-board-projection/02) — the projection is
// rebuildable (master.md decision 2), so Stop optimises for draining
// what was already handed off, not for a zero-loss join.
func (p *Projector) Stop() {
	if p == nil {
		return
	}
	// Unsubscribe FIRST so no NEW callback can start after the drain
	// snapshot the worker takes. Unsubscribe closes the subscriber
	// channel; its reader goroutine still drains the buffered events.
	p.unsubOnce.Do(func() {
		for _, sub := range p.subs {
			bus.Unsubscribe(p.bus, sub)
		}
	})
	// Settle: wait until the handoff queue has read empty twice in a
	// row (capped), so late reader handoffs land before the worker
	// takes its final drain.
	deadline := time.Now().Add(stopSettleWindow)
	quiet := 0
	for quiet < 2 && time.Now().Before(deadline) {
		if len(p.queue) == 0 {
			quiet++
		} else {
			quiet = 0
		}
		time.Sleep(time.Millisecond)
	}
	p.stopOnce.Do(func() { close(p.stopCh) })
	<-p.done
}

// Close is the io.Closer alias of Stop.
func (p *Projector) Close() error {
	p.Stop()
	return nil
}

// GetCardByRef returns the canonical card projected for the work item
// (kind, id), or ErrUnknownCard. Missing accessor on Store (the projector
// needs identity lookup by ref, not by card id); lives here so
// 03/02-rebuild can reuse it. ref_kind/ref_id are unique per card by
// construction: the projector is the only writer of ref fields, and it
// keys on (Kind, ID).
func (s *Store) GetCardByRef(ctx context.Context, kind, refID string) (Card, error) {
	var c Card
	var labelsJSON string
	var refKind, refVal sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val
		FROM kanban_cards WHERE ref_kind = ? AND ref_id = ? ORDER BY id ASC LIMIT 1`, kind, refID).Scan(
		&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &refKind, &refVal, &labelsJSON,
		&c.GoalID, &c.EmployeeID, &c.PlanID, &c.SessionID, &c.TriggerRef, &c.Order)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Card{}, ErrUnknownCard{ID: kind + "/" + refID}
		}
		return Card{}, fmt.Errorf("failed to get card by ref: %w", err)
	}
	c.Ref = WorkRef{Kind: refKind.String, ID: refVal.String}
	if err := unmarshalLabels(labelsJSON, &c.Labels); err != nil {
		return Card{}, fmt.Errorf("failed to decode card %s labels: %w", c.ID, err)
	}
	return c, nil
}

// countCards sizes a column for append-order + WIP posture.
func (s *Store) countCards(ctx context.Context, columnID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kanban_cards WHERE column_id = ?", columnID).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count cards: %w", err)
	}
	return n, nil
}
