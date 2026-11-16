package kanban

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	_ "modernc.org/sqlite"
)

// fakeFetcher is the test-side RowFetcher: provenance lives in a map the
// test mutates, never in the event (C1 — the projector must read the
// SOURCE row at projection time).
type fakeFetcher struct {
	mu      sync.RWMutex
	rows    map[string]ProvRow
	errKeys map[string]error
	calls   atomic.Int64
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{rows: map[string]ProvRow{}, errKeys: map[string]error{}}
}

func (f *fakeFetcher) set(kind, id string, prov ProvRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[kind+"/"+id] = prov
}

func (f *fakeFetcher) setErr(kind, id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errKeys[kind+"/"+id] = err
}

func (f *fakeFetcher) fetch(kind, id string) (ProvRow, error) {
	f.calls.Add(1)
	key := kind + "/" + id
	f.mu.RLock()
	defer f.mu.RUnlock()
	if err, ok := f.errKeys[key]; ok && err != nil {
		return ProvRow{}, err
	}
	return f.rows[key], nil
}

func (f *fakeFetcher) TaskRow(id string) (ProvRow, error) { return f.fetch(EventKindTask, id) }
func (f *fakeFetcher) JobRow(id string) (ProvRow, error)  { return f.fetch(EventKindJob, id) }
func (f *fakeFetcher) PlanRow(id string) (ProvRow, error) { return f.fetch(EventKindPlan, id) }
func (f *fakeFetcher) StepRow(id string) (ProvRow, error) { return f.fetch(EventKindStep, id) }
func (f *fakeFetcher) GoalRow(id string) (ProvRow, error) { return f.fetch(EventKindGoal, id) }

// gatedFetcher blocks its FIRST TaskRow fetch until the gate opens. The
// atGate signal lets a test prove the worker is parked mid-fetch before
// flooding events behind it.
type gatedFetcher struct {
	inner     *fakeFetcher
	gate      chan struct{}
	atGate    atomic.Bool
	gatedOnce sync.Once
}

func (g *gatedFetcher) TaskRow(id string) (ProvRow, error) {
	g.gatedOnce.Do(func() {
		g.atGate.Store(true)
		<-g.gate
	})
	return g.inner.TaskRow(id)
}

func (g *gatedFetcher) JobRow(string) (ProvRow, error)  { return ProvRow{}, nil }
func (g *gatedFetcher) PlanRow(string) (ProvRow, error) { return ProvRow{}, nil }
func (g *gatedFetcher) StepRow(string) (ProvRow, error) { return ProvRow{}, nil }
func (g *gatedFetcher) GoalRow(string) (ProvRow, error) { return ProvRow{}, nil }

var (
	_ RowFetcher = (*fakeFetcher)(nil)
	_ RowFetcher = (*gatedFetcher)(nil)
)

// projectorFixture is the canonical rig: real store (sqlite under the
// test's temp dir, house newTestStore pattern), real bus, rules written
// through the store's own upsert then reloaded — the same path 07/01
// wiring will take.
type projectorFixture struct {
	bus     *bus.MessageBus
	store   *Store
	fetcher *fakeFetcher
	proj    *Projector
	boardID string
	cols    map[string]string // column NAME -> id
}

// newProjectorFixture materialises one board with one column per distinct
// rule.ColumnID (rules speak column NAMES here; the fixture rewrites
// them to ids so tests read naturally), seeds kanban_projections, loads
// the rules snapshot, and starts a projector on a fresh bus.
func newProjectorFixture(t *testing.T, rules []ProjectionRule) *projectorFixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "projector-test.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.db.Close() })

	f := &projectorFixture{
		bus:     bus.New(nil, nil),
		store:   store,
		fetcher: newFakeFetcher(),
		boardID: "b-proj",
		cols:    map[string]string{},
	}
	t.Cleanup(func() { f.bus.Close() })

	ctx := context.Background()
	if err := store.CreateBoard(ctx, Board{ID: f.boardID, Name: "Projection Board"}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	// Columns first (FK), then rules.
	pos := 0
	for _, r := range rules {
		if _, exists := f.cols[r.ColumnID]; exists {
			continue
		}
		colID := "col-" + r.ColumnID
		if err := store.CreateColumn(ctx, Column{ID: colID, BoardID: f.boardID, Name: r.ColumnID, Position: pos}); err != nil {
			t.Fatalf("CreateColumn %s: %v", colID, err)
		}
		f.cols[r.ColumnID] = colID
		pos++
	}
	loadedIn := make([]ProjectionRule, 0, len(rules))
	for _, r := range rules {
		r.BoardID = f.boardID
		r.ColumnID = f.cols[r.ColumnID]
		loadedIn = append(loadedIn, r)
		if err := store.SetProjectionRule(ctx, r); err != nil {
			t.Fatalf("SetProjectionRule: %v", err)
		}
	}
	snapshot, err := store.LoadProjectionRules(ctx, f.boardID)
	if err != nil {
		t.Fatalf("LoadProjectionRules: %v", err)
	}

	proj, err := NewProjector(f.bus, f.store, snapshot, f.fetcher)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	f.proj = proj
	t.Cleanup(func() { proj.Stop() })
	return f
}

func (f *projectorFixture) publish(t *testing.T, topic bus.Topic[StateEvent], ev StateEvent) {
	t.Helper()
	if n := bus.PublishT(f.bus, topic, "test-publisher", ev); n == 0 {
		t.Fatalf("publish to %s reached no subscriber", topic.Name)
	}
}

func (f *projectorFixture) waitProcessed(t *testing.T, want int64) {
	t.Helper()
	waitFor(t, func() bool { return f.proj.Stats().Processed >= want }, "projector to process")
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// standardRules is the canonical ruleset: the mapping keys on FROM
// state (C3: kind + from_state -> column_id), so "pending->todo" means
// "when the item LEAVES pending, park its card in todo".
func standardRules(kind string) []ProjectionRule {
	return []ProjectionRule{
		{Kind: kind, FromState: "pending", ColumnID: "todo"},
		{Kind: kind, FromState: "claimed", ColumnID: "doing"},
		{Kind: kind, FromState: "processing", ColumnID: "done"},
	}
}

func mustGetCardByRef(t *testing.T, store *Store, kind, refID string) Card {
	t.Helper()
	card, err := store.GetCardByRef(context.Background(), kind, refID)
	if err != nil {
		t.Fatalf("GetCardByRef(%s/%s): %v", kind, refID, err)
	}
	return card
}

// TestProjector_MappedEdgeCreatesCardWithProvenance covers the C1 path:
// the first mapped transition creates the canonical card and fills
// provenance from the fake fetcher's SOURCE row, never from the event.
func TestProjector_MappedEdgeCreatesCardWithProvenance(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))
	ctx := context.Background()

	f.fetcher.set(EventKindTask, "task-1", ProvRow{
		Title: "Paper cuts", GoalID: "goal-9", EmployeeID: "emp-3",
		PlanID: "plan-2", SessionID: "sess-7", TriggerRef: "intake:repo#12",
	})
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "pending", To: "claimed"})
	f.waitProcessed(t, 1)

	card := mustGetCardByRef(t, f.store, EventKindTask, "task-1")
	if want := f.cols["todo"]; card.ColumnID != want {
		t.Errorf("ColumnID = %q, want %q", card.ColumnID, want)
	}
	if card.Title != "Paper cuts" {
		t.Errorf("Title = %q, want source-row title", card.Title)
	}
	if card.GoalID != "goal-9" || card.EmployeeID != "emp-3" || card.PlanID != "plan-2" ||
		card.SessionID != "sess-7" || card.TriggerRef != "intake:repo#12" {
		t.Errorf("provenance not filled from fetcher: %+v", card)
	}
	if card.Ref.Kind != EventKindTask || card.Ref.ID != "task-1" {
		t.Errorf("Ref = %+v, want task/task-1", card.Ref)
	}

	// Second mapped transition MOVES the same canonical card — one card
	// per work item, no duplicates.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "claimed", To: "processing"})
	f.waitProcessed(t, 2)
	eventuallyMoved := false
	waitFor(t, func() bool {
		moved, err := f.store.GetCardByRef(ctx, EventKindTask, "task-1")
		eventuallyMoved = err == nil && moved.ColumnID == f.cols["doing"]
		return eventuallyMoved
	}, "card to reach doing")
	cards, err := f.store.ListCards(ctx, f.cols["todo"])
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("card left behind in todo: %+v", cards)
	}
}

// TestProjector_UnmappedStateParks proves the no-rule posture: no card
// before the first mapped transition, and an existing card parks
// untouched when a later transition matches no rule.
func TestProjector_UnmappedStateParks(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))
	ctx := context.Background()

	// Unmapped FIRST: nothing exists yet -> no card at all.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "backlog", To: "pending"})
	time.Sleep(100 * time.Millisecond)
	var unknown ErrUnknownCard
	if _, err := f.store.GetCardByRef(ctx, EventKindTask, "task-1"); !errors.As(err, &unknown) {
		t.Fatalf("unmapped first event created a card or errored oddly: %v", err)
	}
	if got := f.proj.Stats().Processed; got != 0 {
		t.Errorf("Processed = %d, want 0 for unmapped events", got)
	}

	// Mapped once: card lands in todo.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "pending", To: "claimed"})
	f.waitProcessed(t, 1)

	// Unmapped AGAIN while a card exists: the item leaves on_hold, a
	// state with no rule; the card parks in todo, untouched.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "on_hold", To: "claimed"})
	time.Sleep(100 * time.Millisecond)
	card := mustGetCardByRef(t, f.store, EventKindTask, "task-1")
	if card.ColumnID != f.cols["todo"] {
		t.Errorf("card moved on unmapped state: column = %q, want parked %q", card.ColumnID, f.cols["todo"])
	}
	if got := f.proj.Stats().Processed; got != 1 {
		t.Errorf("Processed = %d, want 1 (unmapped must not count)", got)
	}
}

// TestProjector_coalescence: a burst of five same-ID transitions that
// arrives while the worker is parked must collapse to ONE projection
// write at the final mapping (last state wins), and the publisher must
// never block — measured by the publish loop's own latency.
func TestProjector_coalescence(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))
	ctx := context.Background()
	f.fetcher.set(EventKindTask, "task-burst", ProvRow{Title: "Burst"})

	// Replace the fixture projector with a gated one so the worker is
	// provably parked on one write while the burst queues behind it.
	snapshot, err := f.store.LoadProjectionRules(ctx, f.boardID)
	if err != nil {
		t.Fatalf("LoadProjectionRules: %v", err)
	}
	f.proj.Stop()
	fCallsBefore := f.fetcher.calls.Load()
	busB := bus.New(nil, nil)
	t.Cleanup(func() { busB.Close() })
	gate := make(chan struct{})
	slow := &gatedFetcher{inner: f.fetcher, gate: gate}
	proj, err := NewProjector(busB, f.store, snapshot, slow)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	t.Cleanup(func() { proj.Stop() })
	f.proj = proj

	// Warm event: the worker takes it and parks inside the gated fetch.
	bus.PublishT(busB, TopicTaskState, "burst", StateEvent{
		Kind: EventKindTask, ID: "task-burst", From: "pending", To: "claimed",
		TS: time.Now().UTC(),
	})
	waitFor(t, func() bool { return slow.atGate.Load() }, "worker to park on the gate")

	// Burst: five more transitions, the last one maps to done.
	burst := []StateEvent{
		{Kind: EventKindTask, ID: "task-burst", From: "claimed", To: "processing"},
		{Kind: EventKindTask, ID: "task-burst", From: "processing", To: "blocked"},
		{Kind: EventKindTask, ID: "task-burst", From: "blocked", To: "claimed"},
		{Kind: EventKindTask, ID: "task-burst", From: "pending", To: "claimed"},
		{Kind: EventKindTask, ID: "task-burst", From: "processing", To: "done"},
	}
	start := time.Now()
	for i := range burst {
		burst[i].TS = time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		bus.PublishT(busB, TopicTaskState, "burst", burst[i])
	}
	publishLatency := time.Since(start)

	// Deterministic coalescence: prove all five queued BEFORE releasing
	// the worker, so the post-block batch is exactly the merged burst.
	waitFor(t, func() bool { return len(proj.queue) >= len(burst) }, "burst to queue behind the parked worker")

	// The publisher never blocked: five typed publishes are channel
	// sends. A projector that did I/O in the callback would take
	// milliseconds-to-seconds per event on sqlite.
	if publishLatency > 250*time.Millisecond {
		t.Errorf("publisher blocked: 5 publishes took %v", publishLatency)
	}

	// Release the worker. It finishes the warm write, then drains and
	// MERGES the five queued events into one batch (last state wins).
	close(gate)
	waitFor(t, func() bool {
		card, err := f.store.GetCardByRef(ctx, EventKindTask, "task-burst")
		return err == nil && card.ColumnID == f.cols["done"]
	}, "burst card to reach the final mapped column (done)")

	// Coalescence proof: the merged batch caused ONE post-block write,
	// not five — the fetcher is called once more for the item, total
	// calls <= warm + 1 merged batch.
	calls := f.fetcher.calls.Load() - fCallsBefore
	if calls > 2 {
		t.Errorf("fetcher called %d times after burst (want <= 2: warm write + one coalesced batch)", calls)
	}
	if st := proj.Stats(); st.Processed > 2 {
		t.Errorf("Processed = %d, want <= 2 (Processed = projection writes; the burst merged): %+v", st.Processed, st)
	}

	// ONE canonical card total across all fixture columns.
	total := 0
	for _, colID := range f.cols {
		cards, err := f.store.ListCards(ctx, colID)
		if err != nil {
			t.Fatalf("ListCards: %v", err)
		}
		total += len(cards)
	}
	if total != 1 {
		t.Errorf("card count = %d, want exactly 1 canonical card", total)
	}
}

// TestProjector_WIPOverflowPosture proves the pinned posture: a
// projection move into a column already at its WIP limit still lands
// (board == source outranks the limit), logs a Warn, and increments the
// exported overflow counter.
func TestProjector_WIPOverflowPosture(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))
	ctx := context.Background()

	// doing carries WIP 1.
	if err := f.store.UpdateColumnWIP(ctx, f.boardID, f.cols["doing"], 1); err != nil {
		t.Fatalf("UpdateColumnWIP: %v", err)
	}

	f.fetcher.set(EventKindTask, "task-a", ProvRow{Title: "A"})
	f.fetcher.set(EventKindTask, "task-b", ProvRow{Title: "B"})

	// A fills the limit: pending->todo, then claimed->doing.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-a", From: "pending", To: "claimed"})
	f.waitProcessed(t, 1)
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-a", From: "claimed", To: "processing"})
	f.waitProcessed(t, 2)

	// B arrives at doing while it is already at WIP 1: the write must
	// NOT be dropped or parked — projection == source.
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-b", From: "pending", To: "claimed"})
	f.waitProcessed(t, 3)
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-b", From: "claimed", To: "processing"})
	f.waitProcessed(t, 4)

	eventuallyOverflow := false
	waitFor(t, func() bool {
		eventuallyOverflow = f.proj.Stats().WIPOverflow >= 1
		return eventuallyOverflow
	}, "overflow counter to increment")
	cards, err := f.store.ListCards(ctx, f.cols["doing"])
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("doing has %d cards, want 2: over-WIP projection writes land anyway (board == source)", len(cards))
	}
	for _, c := range cards {
		if c.Ref.ID != "task-a" && c.Ref.ID != "task-b" {
			t.Errorf("unexpected card in doing: %+v", c)
		}
	}
}

// TestProjector_LeaseRuleReading states the contract reading: lease rule
// A (master.md decision 1) refuses HUMAN moves onto a leased work item.
// The projector is the machine lane — it mirrors transitions the source
// machines already committed, including claim acquisitions — so it
// performs NO lease check and must never refuse a claim-driven move.
// (Lease enforcement lives in the human command path, 03/03.)
func TestProjector_LeaseRuleReading(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))

	// A claim transition INTO the active column while the source work
	// item holds the live claim (reason=claim). The projection lands.
	f.fetcher.set(EventKindTask, "task-leased", ProvRow{Title: "Leased work"})
	f.publish(t, TopicTaskState, StateEvent{
		Kind: EventKindTask, ID: "task-leased", From: "pending", To: "claimed",
		Reason: EventReasonClaim,
	})
	f.waitProcessed(t, 1)
	f.publish(t, TopicTaskState, StateEvent{
		Kind: EventKindTask, ID: "task-leased", From: "claimed", To: "processing",
		Reason: EventReasonClaim,
	})
	f.waitProcessed(t, 2)

	card := mustGetCardByRef(t, f.store, EventKindTask, "task-leased")
	if want := f.cols["doing"]; card.ColumnID != want {
		t.Errorf("ColumnID = %q, want %q — machine lane must ignore lease rule A", card.ColumnID, want)
	}
	if st := f.proj.Stats(); st.Failed != 0 {
		t.Errorf("Failed = %d, want 0", st.Failed)
	}
}

// TestProjector_StopDrainsInFlight: every event handed off before Stop()
// is projected before Stop returns; Stop is idempotent.
func TestProjector_StopDrainsInFlight(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))

	f.fetcher.set(EventKindTask, "task-drain", ProvRow{Title: "Drain"})
	for i := 0; i < 10; i++ {
		ev := StateEvent{Kind: EventKindTask, ID: "task-drain", From: "pending", To: "claimed"}
		if i%2 == 1 {
			ev.From = "claimed"
			ev.To = "processing"
		}
		ev.TS = time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		bus.PublishT(f.bus, TopicTaskState, "drain-test", ev)
	}
	f.proj.Stop()

	// The last event mapped claimed->doing. Whatever the batching did,
	// the card exists and is in a MAPPED column — nothing lost.
	card := mustGetCardByRef(t, f.store, EventKindTask, "task-drain")
	if card.ColumnID != f.cols["todo"] && card.ColumnID != f.cols["doing"] {
		t.Errorf("card in unexpected column %q after drained stop", card.ColumnID)
	}
	f.proj.Stop() // idempotent
}

// TestProjector_ConstructorGuards: nil bus / nil store error; a nil
// fetcher is nil-safe (defaults to the no-op fetcher); empty rules are
// legal; lifecycle methods survive a nil Projector.
func TestProjector_ConstructorGuards(t *testing.T) {
	store := newTestStore(t)
	b := bus.New(nil, nil)
	t.Cleanup(func() { b.Close() })
	rules := NewProjectionRules([]ProjectionRule{{BoardID: "b", Kind: "task", FromState: "pending", ColumnID: "c"}})

	if _, err := NewProjector(nil, store, rules, newFakeFetcher()); err == nil {
		t.Error("NewProjector(nil bus) = nil error, want error")
	}
	if _, err := NewProjector(b, nil, rules, newFakeFetcher()); err == nil {
		t.Error("NewProjector(nil store) = nil error, want error")
	}
	p, err := NewProjector(b, store, rules, nil)
	if err != nil {
		t.Fatalf("NewProjector(nil fetcher): %v", err)
	}
	p.Stop()
	if _, err := NewProjector(b, store, ProjectionRules{}, newFakeFetcher()); err != nil {
		t.Fatalf("NewProjector(empty rules): %v", err)
	}
	// Nil-projector lifecycle guards.
	var nilProj *Projector
	nilProj.Stop()
	if err := nilProj.Close(); err != nil {
		t.Errorf("nil Close() = %v, want nil", err)
	}
	if st := nilProj.Stats(); st != (ProjectorStats{}) {
		t.Errorf("nil Stats() = %+v, want zero", st)
	}
	// Option guards.
	var nilP *Projector
	WithQueueSize(8)(nilP)
	WithQueueSize(0)(nil)
	WithLogger(nil)(nil)
}

// TestProjector_FetcherErrorProjectsWithoutProvenance: a broken source
// row read degrades the card (fallback title, empty provenance) — it
// does not fail the projection.
func TestProjector_FetcherErrorProjectsWithoutProvenance(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))

	f.fetcher.setErr(EventKindTask, "task-fetch-err", errors.New("source store on fire"))
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-fetch-err", From: "pending", To: "claimed"})
	f.waitProcessed(t, 1)

	card := mustGetCardByRef(t, f.store, EventKindTask, "task-fetch-err")
	if card.GoalID != "" || card.Title != "task:task-fetch-err" {
		t.Errorf("card = %+v, want empty provenance + fallback title", card)
	}
	if st := f.proj.Stats(); st.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (fetch failure is a degraded write, not a failure)", st.Failed)
	}
}

// TestProjector_ProvenanceDriftRefresh: once the card sits in its mapped
// column, a repeated mapped event refreshes provenance from the source
// row (a goal/plan link that happened after create).
func TestProjector_ProvenanceDriftRefresh(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))

	f.fetcher.set(EventKindTask, "task-drift", ProvRow{Title: "Drift"})
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-drift", From: "pending", To: "claimed"})
	f.waitProcessed(t, 1)

	f.fetcher.set(EventKindTask, "task-drift", ProvRow{Title: "Drift", PlanID: "plan-late", GoalID: "goal-late"})
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-drift", From: "pending", To: "claimed"})
	f.waitProcessed(t, 2)

	waitFor(t, func() bool {
		card, err := f.store.GetCardByRef(context.Background(), EventKindTask, "task-drift")
		return err == nil && card.PlanID == "plan-late" && card.GoalID == "goal-late"
	}, "provenance to refresh")
}

// TestProjector_EventsAcrossAllFiveTopics: one mapped transition per
// machine topic projects per-kind cards — the C2 five-topic surface is
// really subscribed.
func TestProjector_EventsAcrossAllFiveTopics(t *testing.T) {
	kinds := []string{EventKindTask, EventKindJob, EventKindPlan, EventKindStep, EventKindGoal}
	var rules []ProjectionRule
	for _, k := range kinds {
		rules = append(rules, standardRules(k)...)
	}
	f := newProjectorFixture(t, rules)

	topics := map[string]bus.Topic[StateEvent]{
		EventKindTask: TopicTaskState,
		EventKindJob:  TopicJobState,
		EventKindPlan: TopicPlanState,
		EventKindStep: TopicStepState,
		EventKindGoal: TopicGoalState,
	}
	for i, k := range kinds {
		refID := k + "-x"
		f.fetcher.set(k, refID, ProvRow{Title: "T-" + k})
		f.publish(t, topics[k], StateEvent{Kind: k, ID: refID, From: "pending", To: "claimed"})
		f.waitProcessed(t, int64(i+1))
		card := mustGetCardByRef(t, f.store, k, refID)
		if want := f.cols["todo"]; card.ColumnID != want {
			t.Errorf("kind %s: column %q, want %q", k, card.ColumnID, want)
		}
	}
}

// TestProjector_QueuedDropsAreCounted proves the overflow posture: with
// the worker parked and a size-2 handoff queue, a 20-event flood drops
// (counted, logged) instead of blocking the publisher or growing
// unboundedly.
func TestProjector_QueuedDropsAreCounted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "drops.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.db.Close() })
	b := bus.New(&bus.Config{BufferSize: 1024}, nil)
	t.Cleanup(func() { b.Close() })

	ctx := context.Background()
	if err := store.CreateBoard(ctx, Board{ID: "b-1", Name: "Drops"}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	if err := store.CreateColumn(ctx, Column{ID: "col-1", BoardID: "b-1", Name: "todo", Position: 0}); err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	if err := store.SetProjectionRule(ctx, ProjectionRule{BoardID: "b-1", Kind: "task", FromState: "pending", ColumnID: "col-1"}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	rules, err := store.LoadProjectionRules(ctx, "b-1")
	if err != nil {
		t.Fatalf("LoadProjectionRules: %v", err)
	}

	gate := make(chan struct{})
	slow := &gatedFetcher{inner: newFakeFetcher(), gate: gate}
	proj, err := NewProjector(b, store, rules, slow, WithQueueSize(2), WithLogger(nil))
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	t.Cleanup(func() { proj.Stop() })

	for i := 0; i < 20; i++ {
		bus.PublishT(b, TopicTaskState, "flood", StateEvent{
			Kind: EventKindTask, ID: "task-flood", From: "pending", To: "claimed",
			TS: time.Now().UTC(),
		})
	}
	// The worker is parked on the first gated fetch; the 2-slot queue
	// fills; the rest must drop+count, never block.
	waitFor(t, func() bool { return proj.Stats().Dropped > 0 }, "drops to be counted")
	close(gate)
	proj.Stop() // drains; also proves Stop survives a drop storm

	if st := proj.Stats(); st.Processed == 0 {
		t.Errorf("Processed = 0 after release, want >= 1 (dropped events don't stall the worker): %+v", st)
	}
}

// TestProjectionRules_RoundTripAndUpsert covers the rules loader seam:
// re-setting a key replaces the target column instead of stacking a
// second mapping (two rules for one key would ping-pong the card).
func TestProjectionRules_RoundTripAndUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)

	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colA}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colB}); err != nil {
		t.Fatalf("SetProjectionRule upsert: %v", err)
	}
	rules, err := s.LoadProjectionRules(ctx, boardID)
	if err != nil {
		t.Fatalf("LoadProjectionRules: %v", err)
	}
	if rules.Len() != 1 {
		t.Fatalf("rules = %d rows, want 1 (upsert replaces, never stacks): %+v", rules.Len(), rules.Rows())
	}
	matches := rules.Matches("task", "pending")
	if len(matches) != 1 || matches[0].ColumnID != colB {
		t.Errorf("Matches(task,pending) = %+v, want the colB rule", matches)
	}
	if len(rules.Matches("task", "claimed")) != 0 {
		t.Error("Matches found an unmapped state")
	}
	// Empty boardID loads every board's rules (reconcile's path).
	all, err := s.LoadProjectionRules(ctx, "")
	if err != nil {
		t.Fatalf("LoadProjectionRules(all): %v", err)
	}
	if all.Len() != 1 {
		t.Errorf("global rules = %d, want 1", all.Len())
	}
	// Validation guards.
	for _, bad := range []ProjectionRule{
		{Kind: "task", FromState: "x", ColumnID: colA},
		{BoardID: boardID, FromState: "x", ColumnID: colA},
		{BoardID: boardID, Kind: "task", ColumnID: colA},
		{BoardID: boardID, Kind: "task", FromState: "x"},
	} {
		if err := s.SetProjectionRule(ctx, bad); err == nil {
			t.Errorf("invalid rule accepted: %+v", bad)
		}
	}
	// Rows() returns a defensive copy.
	rows := rules.Rows()
	rows[0].ColumnID = "mutated"
	if rules.Rows()[0].ColumnID == "mutated" {
		t.Error("Rows() leaked the internal slice")
	}
}
