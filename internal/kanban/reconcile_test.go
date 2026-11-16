package kanban

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// recListFetcher is the test ListFetcher: per-kind item lists behind a
// mutex (the race detector runs periodic ticks against manual passes),
// with per-kind error injection for the enumeration-failure guard.
type recListFetcher struct {
	mu    sync.RWMutex
	items map[string][]WorkItemRef
	errs  map[string]error
	calls int
}

func newRecListFetcher() *recListFetcher {
	return &recListFetcher{
		items: map[string][]WorkItemRef{},
		errs:  map[string]error{},
	}
}

func (f *recListFetcher) set(kind string, items ...WorkItemRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[kind] = items
}

func (f *recListFetcher) setError(kind string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[kind] = err
}

func (f *recListFetcher) callCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.calls
}

func (f *recListFetcher) ListWorkItems(_ context.Context, kind string) ([]WorkItemRef, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	f.mu.RLock()
	defer f.mu.RUnlock()
	if err, ok := f.errs[kind]; ok && err != nil {
		return nil, err
	}
	return append([]WorkItemRef(nil), f.items[kind]...), nil
}

var _ ListFetcher = (*recListFetcher)(nil)

// recFixture is the reconcile rig: real store under $TMPDIR (house
// newTestStore pattern), one board with two columns (seedBoard), two
// task rules (pending -> col-a, claimed -> col-b) written through the
// store's own upsert, and a reconciler over a controllable ListFetcher.
// The rules key on from_state exactly like the projector's, so an
// enumerated WorkItemRef.State names the from_state of the transition
// that last placed the item (see WorkItemRef doc).
type recFixture struct {
	store   *Store
	list    *recListFetcher
	rec     *Reconciler
	boardID string
	colA    string
	colB    string
}

func newRecFixture(t *testing.T) *recFixture {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	for _, r := range []ProjectionRule{
		{BoardID: boardID, Kind: EventKindTask, FromState: "pending", ColumnID: colA},
		{BoardID: boardID, Kind: EventKindTask, FromState: "claimed", ColumnID: colB},
	} {
		if err := s.SetProjectionRule(ctx, r); err != nil {
			t.Fatalf("SetProjectionRule: %v", err)
		}
	}
	list := newRecListFetcher()
	rec, err := NewReconciler(s, list)
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	return &recFixture{store: s, list: list, rec: rec, boardID: boardID, colA: colA, colB: colB}
}

func (f *recFixture) reconcile(t *testing.T) ReconcileStats {
	t.Helper()
	stats, err := f.rec.ReconcileNowStats(context.Background())
	if err != nil {
		t.Fatalf("ReconcileNowStats: %v", err)
	}
	return stats
}

func recCountRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM kanban_cards").Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func recMustCard(t *testing.T, s *Store, kind, refID string) Card {
	t.Helper()
	c, err := s.GetCardByRef(context.Background(), kind, refID)
	if err != nil {
		t.Fatalf("GetCardByRef(%s/%s): %v", kind, refID, err)
	}
	return c
}

// TestReconcile_BuildsFromEmptyProjection: the full-scan rebuild from a
// board with zero cards materialises one canonical card per mapped
// live item, at the rule column, with source-row title/provenance
// (C1 identity-only: everything else comes from the enumerated Row).
func TestReconcile_BuildsFromEmptyProjection(t *testing.T) {
	f := newRecFixture(t)
	f.list.set(EventKindTask,
		WorkItemRef{Kind: EventKindTask, ID: "task-1", State: "claimed", Row: ProvRow{
			Title: "Fix wire", GoalID: "goal-1", EmployeeID: "emp-1",
			PlanID: "plan-1", SessionID: "sess-1", TriggerRef: "trig-1",
		}},
		WorkItemRef{Kind: EventKindTask, ID: "task-2", State: "pending", Row: ProvRow{Title: "Write docs"}},
	)

	stats := f.reconcile(t)
	if stats.Created != 2 || stats.Total() != 2 {
		t.Fatalf("stats = %+v, want Created=2", stats)
	}
	c1 := recMustCard(t, f.store, EventKindTask, "task-1")
	if c1.ColumnID != f.colB || c1.BoardID != f.boardID {
		t.Errorf("task-1 landed at %s/%s, want %s/%s", c1.BoardID, c1.ColumnID, f.boardID, f.colB)
	}
	if c1.Title != "Fix wire" || c1.GoalID != "goal-1" || c1.EmployeeID != "emp-1" ||
		c1.PlanID != "plan-1" || c1.SessionID != "sess-1" || c1.TriggerRef != "trig-1" {
		t.Errorf("task-1 provenance not filled from enumerated row: %+v", c1)
	}
	c2 := recMustCard(t, f.store, EventKindTask, "task-2")
	if c2.ColumnID != f.colA {
		t.Errorf("task-2 column = %s, want %s", c2.ColumnID, f.colA)
	}

	// Idempotence pin (leaf contract): a second pass changes zero rows.
	if again := f.reconcile(t); again.Total() != 0 {
		t.Errorf("second pass stats = %+v, want all zero", again)
	}
}

// TestReconcile_DriftRepair covers the three drift classes the plan
// names — wrong column, missing card, orphan card — in one pass, then
// pins that the repaired board is a fixed point.
func TestReconcile_DriftRepair(t *testing.T) {
	ctx := context.Background()
	f := newRecFixture(t)

	// Wrong column: card sits in colB, source says it left pending.
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-drift", BoardID: f.boardID, ColumnID: f.colB,
		Title: "task:wrong", Ref: WorkRef{Kind: EventKindTask, ID: "wrong"},
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// Orphan: a projected card whose work item no longer exists.
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-orphan", BoardID: f.boardID, ColumnID: f.colA,
		Title: "task:gone", Ref: WorkRef{Kind: EventKindTask, ID: "gone"},
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// A manual (NULL-ref) card the rebuild must never touch.
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-manual", BoardID: f.boardID, ColumnID: f.colA, Title: "human note",
	}); err != nil {
		t.Fatalf("CreateCard manual: %v", err)
	}

	// Missing card: "missing" is mapped in the enumeration, has no row.
	f.list.set(EventKindTask,
		WorkItemRef{Kind: EventKindTask, ID: "wrong", State: "pending", Row: ProvRow{Title: "task:wrong"}},
		WorkItemRef{Kind: EventKindTask, ID: "missing", State: "claimed", Row: ProvRow{Title: "Fresh"}},
	)

	stats := f.reconcile(t)
	if stats.Moved != 1 || stats.Created != 1 || stats.Deleted != 1 || stats.Total() != 3 {
		t.Fatalf("stats = %+v, want Moved=1 Created=1 Deleted=1", stats)
	}
	if got := recMustCard(t, f.store, EventKindTask, "wrong").ColumnID; got != f.colA {
		t.Errorf("drift card column = %s, want %s", got, f.colA)
	}
	if got := recMustCard(t, f.store, EventKindTask, "missing").ColumnID; got != f.colB {
		t.Errorf("created card column = %s, want %s", got, f.colB)
	}
	var unknown ErrUnknownCard
	if _, err := f.store.GetCardByRef(ctx, EventKindTask, "gone"); !errors.As(err, &unknown) {
		t.Errorf("orphan card survived reconcile: %v", err)
	}
	if _, err := f.store.GetCard(ctx, "card-manual"); err != nil {
		t.Errorf("manual card was touched by the rebuild: %v", err)
	}
	if again := f.reconcile(t); again.Total() != 0 {
		t.Errorf("repaired board is not a fixed point: %+v", again)
	}
}

// TestReconcile_ParkedCardPosture: a live item whose enumerated state
// matches no rule parks — the card keeps its last mapped column and
// the pass reports zero changes. Deleting or moving parked cards would
// contradict the projector type-doc's split of duties.
func TestReconcile_ParkedCardPosture(t *testing.T) {
	f := newRecFixture(t)
	// Live item whose last transition's from_state ("backlog") maps to
	// no rule: reconcile must NOT invent a card for it, and a parked
	// card for it must never be moved or deleted (next test).
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "park-1", State: "backlog", Row: ProvRow{Title: "Parked"}})

	if stats := f.reconcile(t); stats.Total() != 0 {
		t.Fatalf("unmapped item caused writes: %+v", stats)
	}
	var unknown ErrUnknownCard
	if _, err := f.store.GetCardByRef(context.Background(), EventKindTask, "park-1"); !errors.As(err, &unknown) {
		t.Errorf("card created for an unmapped item: %v", err)
	}
}

// TestReconcile_ParkedKeepsExistingCard proves the move/delete side of
// the parked posture: a card that already exists for an unmapped-state
// item stands untouched.
func TestReconcile_ParkedKeepsExistingCard(t *testing.T) {
	ctx := context.Background()
	f := newRecFixture(t)
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-park", BoardID: f.boardID, ColumnID: f.colA,
		Title: "task:park", Ref: WorkRef{Kind: EventKindTask, ID: "park"},
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "park", State: "backlog"})

	if stats := f.reconcile(t); stats.Total() != 0 {
		t.Fatalf("parked item caused writes: %+v", stats)
	}
	before := recMustCard(t, f.store, EventKindTask, "park")
	if before.ColumnID != f.colA {
		t.Errorf("parked card moved: %s", before.ColumnID)
	}
}

// TestReconcile_EnumerationErrorNeverDeletes: a source failure for one
// kind must not masquerade as orphanage — the kind's cards stand.
func TestReconcile_EnumerationErrorNeverDeletes(t *testing.T) {
	ctx := context.Background()
	f := newRecFixture(t)
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-src", BoardID: f.boardID, ColumnID: f.colA,
		Title: "task:src", Ref: WorkRef{Kind: EventKindTask, ID: "src"},
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	f.list.setError(EventKindTask, errors.New("machine store down"))

	if stats := f.reconcile(t); stats.Total() != 0 {
		t.Fatalf("errored enumeration changed rows: %+v", stats)
	}
	if _, err := f.store.GetCard(ctx, "card-src"); err != nil {
		t.Errorf("card deleted on a failed enumeration: %v", err)
	}
	// Once the source answers again (item really gone), deletion runs.
	f.list.setError(EventKindTask, nil)
	if stats := f.reconcile(t); stats.Deleted != 1 {
		t.Fatalf("stats = %+v, want Deleted=1 after recovery", stats)
	}
}

// TestReconcile_ProvenanceDriftRepaired covers the Fixed counter: an
// in-place card in the right column whose source row changed gets its
// title/provenance rewritten, no move.
func TestReconcile_ProvenanceDriftRepaired(t *testing.T) {
	ctx := context.Background()
	f := newRecFixture(t)
	if err := f.store.CreateCard(ctx, Card{
		ID: "card-drift2", BoardID: f.boardID, ColumnID: f.colA,
		Title: "stale title", Ref: WorkRef{Kind: EventKindTask, ID: "d2"}, GoalID: "old-goal",
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "d2", State: "pending",
		Row: ProvRow{Title: "new title", GoalID: "new-goal"}})

	stats := f.reconcile(t)
	if stats.Fixed != 1 || stats.Moved != 0 || stats.Total() != 1 {
		t.Fatalf("stats = %+v, want Fixed=1", stats)
	}
	c := recMustCard(t, f.store, EventKindTask, "d2")
	if c.Title != "new title" || c.GoalID != "new-goal" {
		t.Errorf("drift not repaired: %+v", c)
	}
	if again := f.reconcile(t); again.Total() != 0 {
		t.Errorf("post-repair pass not idempotent: %+v", again)
	}
}

// TestReconcile_DuplicateCardsCollapse: the canonical invariant is one
// card per (kind, id); a race that left two rows loses the
// higher-id one, and the survivor lands at the rule target.
func TestReconcile_DuplicateCardsCollapse(t *testing.T) {
	ctx := context.Background()
	f := newRecFixture(t)
	for _, id := range []string{"card-aa", "card-bb"} {
		if err := f.store.CreateCard(ctx, Card{
			ID: id, BoardID: f.boardID, ColumnID: f.colA,
			Title: "task:dup", Ref: WorkRef{Kind: EventKindTask, ID: "dup"},
		}); err != nil {
			t.Fatalf("CreateCard %s: %v", id, err)
		}
	}
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "dup", State: "claimed", Row: ProvRow{Title: "task:dup"}})

	stats := f.reconcile(t)
	if stats.Deleted != 1 || stats.Moved != 1 || stats.Total() != 2 {
		t.Fatalf("stats = %+v, want Deleted=1 Moved=1", stats)
	}
	c := recMustCard(t, f.store, EventKindTask, "dup")
	if c.ID != "card-aa" {
		t.Errorf("survivor = %s, want the first-by-id card-aa", c.ID)
	}
}

// TestReconcile_KillProjectorRestart pins the integration contract
// (master.md: "rebuild equality — kill projector, restart, board ==
// source"). The deterministic lost-event: the projector is STOPPED
// before the final transition publishes, so the live lane never saw
// it (the dropped-event window its Stop doc concedes). Reconcile's
// full scan must land the board exactly where a surviving projector
// would have.
func TestReconcile_KillProjectorRestart(t *testing.T) {
	f := newProjectorFixture(t, standardRules(EventKindTask))
	ctx := context.Background()

	// Live lane: task-1 walks pending->claimed->processing (todo ->
	// doing), task-2 takes one step (todo).
	f.fetcher.set(EventKindTask, "task-1", ProvRow{Title: "Long run"})
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "pending", To: "claimed", TS: time.Now().UTC()})
	f.waitProcessed(t, 1)
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-1", From: "claimed", To: "processing", TS: time.Now().UTC()})
	f.waitProcessed(t, 2)
	f.publish(t, TopicTaskState, StateEvent{Kind: EventKindTask, ID: "task-2", From: "pending", To: "claimed", TS: time.Now().UTC()})
	f.waitProcessed(t, 3)

	// Kill the projector, then the final transition fires — its event
	// is lost (no subscriber; publish count deliberately unchecked).
	f.proj.Stop()
	if n := bus.PublishT(f.bus, TopicTaskState, "dropped",
		StateEvent{Kind: EventKindTask, ID: "task-1", From: "processing", To: "confirmed", TS: time.Now().UTC()}); n != 0 {
		t.Fatalf("expected the stopped projector to miss the event, %d subscribers took it", n)
	}

	// A reconcile pass must reproduce the projector's outcome. The
	// enumerated State is the from_state of the last transition, the
	// same key the projector consumed: task-1 left processing (maps to
	// done), task-2 left pending (maps to todo).
	list := newRecListFetcher()
	list.set(EventKindTask,
		WorkItemRef{Kind: EventKindTask, ID: "task-1", State: "processing", Row: ProvRow{Title: "Long run"}},
		WorkItemRef{Kind: EventKindTask, ID: "task-2", State: "pending", Row: ProvRow{Title: fmt.Sprintf("task:%s", "task-2")}},
	)
	rec, err := NewReconcilerWithProvenance(f.store, list, f.fetcher)
	if err != nil {
		t.Fatalf("NewReconcilerWithProvenance: %v", err)
	}
	stats, err := rec.ReconcileNowStats(ctx)
	if err != nil {
		t.Fatalf("ReconcileNowStats: %v", err)
	}
	if stats.Total() != 1 || stats.Moved != 1 {
		t.Errorf("stats = %+v, want exactly the one dropped-event move", stats)
	}

	// board == source, field-for-field against the projector's own
	// lookup path.
	wantCols := map[string]string{
		"task-1": f.cols["done"],
		"task-2": f.cols["todo"],
	}
	for ref, want := range wantCols {
		if got := recMustCard(t, f.store, EventKindTask, ref).ColumnID; got != want {
			t.Errorf("%s column = %s, want %s", ref, got, want)
		}
	}
	if n := recCountRows(t, f.store); n != 2 {
		t.Errorf("card count = %d, want 2", n)
	}
	// And the rebuilt board is a fixed point.
	if again, err := rec.ReconcileNowStats(ctx); err != nil || again.Total() != 0 {
		t.Errorf("second pass = %+v (err %v), want zero changes", again, err)
	}
}

// TestReconcile_ConstructorAndNilGuards pins the leaf's guards.
func TestReconcile_ConstructorAndNilGuards(t *testing.T) {
	if _, err := NewReconciler(nil, nil); err == nil {
		t.Error("NewReconciler(nil store) = nil error, want error")
	}
	// nil fetcher is legal: the pass is a no-op, never a wipe.
	s := newTestStore(t)
	rec, err := NewReconciler(s, nil)
	if err != nil {
		t.Fatalf("NewReconciler(nil fetcher): %v", err)
	}
	if err := rec.ReconcileNow(context.Background()); err != nil {
		t.Errorf("nil-fetcher ReconcileNow = %v, want nil (no-op)", err)
	}
	// Nil-receiver guards (Set* convention + lifecycle posture).
	var nilRec *Reconciler
	if err := nilRec.ReconcileNow(context.Background()); err == nil {
		t.Error("nil Reconciler.ReconcileNow = nil error, want error")
	}
	nilRec.SetProvenanceFetcher(nil)
	nilRec.SetProvenanceFetcher(noopFetcher{})
	nilRec.SetSourceFetcher(nil)
	nilRec.SetLogger(nil)
	nilRec.SetOnDrift(nil)
	if st := (ReconcileStats{}).Total(); st != 0 {
		t.Error("zero stats Total != 0")
	}
}

// TestReconcile_OnDriftHookFiresOnlyOnDrift pins the callback posture:
// it is the 05/01 notification seam, so a clean pass must stay silent.
func TestReconcile_OnDriftHookFiresOnlyOnDrift(t *testing.T) {
	f := newRecFixture(t)
	var fires int
	f.rec.SetOnDrift(func(context.Context) { fires++ })

	f.reconcile(t) // clean board, empty source: zero rows, zero fires
	if fires != 0 {
		t.Fatalf("clean pass fired onDrift %d times, want 0", fires)
	}
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "hook-1", State: "pending"})
	f.reconcile(t)
	if fires != 1 {
		t.Fatalf("drift pass fired onDrift %d times, want 1", fires)
	}
	f.reconcile(t) // idempotent second pass: no fire
	if fires != 1 {
		t.Fatalf("second clean pass fired onDrift (%d total), want still 1", fires)
	}
}

// TestPeriodicReconcile_FiresAndStopsOnCancel drives the 07/01 helper:
// small interval ticks call ReconcileNow, ctx cancellation exits
// cleanly with context.Canceled, and no pass runs after the exit.
func TestPeriodicReconcile_FiresAndStopsOnCancel(t *testing.T) {
	f := newRecFixture(t)
	f.list.set(EventKindTask, WorkItemRef{Kind: EventKindTask, ID: "tick", State: "pending"})
	RegisterDefaultReconciler(f.store, f.rec)
	t.Cleanup(func() { UnregisterDefaultReconciler(f.store) })

	before := f.list.callCount()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunPeriodicReconcile(ctx, f.store, 10*time.Millisecond) }()

	waitFor(t, func() bool { return f.list.callCount() >= before+2 }, "periodic ticks to reach the fetcher")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("RunPeriodicReconcile returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunPeriodicReconcile did not exit on ctx cancel")
	}
	quiet := f.list.callCount()
	time.Sleep(50 * time.Millisecond)
	if moved := f.list.callCount() - quiet; moved != 0 {
		t.Errorf("%d passes ran after ctx cancel", moved)
	}
}

// TestPeriodicReconcile_ZeroIntervalDisabled pins C9: interval 0 (the
// reconcile_interval default) means the helper never ticks and returns
// immediately — the daemon's enabled=false path is inert.
func TestPeriodicReconcile_ZeroIntervalDisabled(t *testing.T) {
	f := newRecFixture(t)
	RegisterDefaultReconciler(f.store, f.rec)
	t.Cleanup(func() { UnregisterDefaultReconciler(f.store) })

	for _, interval := range []time.Duration{0, -time.Second} {
		start := time.Now()
		if err := RunPeriodicReconcile(context.Background(), f.store, interval); err != nil {
			t.Errorf("RunPeriodicReconcile(interval %v) = %v, want nil", interval, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("disabled call took %v, want immediate return", elapsed)
		}
	}
	if calls := f.list.callCount(); calls != 0 {
		t.Errorf("disabled periodic ran %d passes, want 0", calls)
	}
	if err := RunPeriodicReconcile(context.Background(), nil, time.Minute); err == nil {
		t.Error("RunPeriodicReconcile(nil store) = nil error, want error")
	}
}

// TestReconcile_StoreWithoutWiredFetcherIsInert doubles as the
// byte-identical posture check for the unwired path: an empty rule set
// and empty source means no board writes and no errors.
func TestReconcile_StoreWithoutWiredFetcherIsInert(t *testing.T) {
	s := newTestStore(t)
	list := newRecListFetcher()
	rec, err := NewReconciler(s, list)
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	if err := rec.ReconcileNow(context.Background()); err != nil {
		t.Errorf("inert pass errored: %v", err)
	}
	if n := recCountRows(t, s); n != 0 {
		t.Errorf("inert pass wrote %d rows, want 0", n)
	}
}

// TestReconcile_FetcherRowFallback: with no RowFetcher configured the
// enumerated WorkItemRef.Row is the provenance source, and the title
// falls back to "kind:id" exactly like projector.createCard.
func TestReconcile_FetcherRowFallback(t *testing.T) {
	f := newRecFixture(t)
	f.list.set(EventKindJob, WorkItemRef{Kind: EventKindJob, ID: "job-1", State: "queued"})
	// No job rules exist: parked posture, nothing is created.
	if stats := f.reconcile(t); stats.Total() != 0 {
		t.Fatalf("unruled job kind caused %d writes, want 0", stats.Total())
	}

	// Add a rule for the job kind and let the fallback build the card.
	if err := f.store.SetProjectionRule(context.Background(), ProjectionRule{
		BoardID: f.boardID, Kind: EventKindJob, FromState: "queued", ColumnID: f.colA,
	}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	if stats := f.reconcile(t); stats.Created != 1 {
		t.Fatalf("stats = %+v, want Created=1", stats)
	}
	c := recMustCard(t, f.store, EventKindJob, "job-1")
	if c.Title != "job:job-1" {
		t.Errorf("Title = %q, want the kind:id fallback", c.Title)
	}
}
