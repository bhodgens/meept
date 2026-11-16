package daemon

// Boot tests for kanban-board leaf 07/01-daemon-wiring: the [kanban]
// .enabled gate in both directions, driven through the REAL wiring
// functions. Case (g) of the e2e suite (leaf 06/02) covers the full
// daemon boot; these pin the wiring unit itself:
//
//   - disabled (the default): wireKanbanServices touches NOTHING — no
//     store file, no goroutine, no subscriber; registerKanbanRPC(nil)
//     registers nothing (kanban.* answers "method not found", exactly
//     like a pre-kanban daemon).
//   - enabled: store file created under $MEEPT_HOME/kanban, default
//     board + todo/doing/done columns + the full seeded ruleset exist,
//     the rule bootstrap is idempotent, and a StateEvent published on
//     the live bus lands as a canonical card through the REAL projector
//     (board == source), with reconcile seeing the same placement.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/internal/rpc"
)

// newKanbanWiringHarness builds the minimal Components the wiring reads
// (Config only — the five machines are absent, the documented degraded
// posture: fetchers answer "source unresolvable", the projector falls
// back to "kind:id" titles, and reconcile skips kinds it cannot
// enumerate; the wiring itself must never panic).
func newKanbanWiringHarness(t *testing.T, enabled bool) (*Components, *bus.MessageBus) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	cfg := config.DefaultConfig()
	cfg.Kanban.Enabled = enabled
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Components{Config: cfg, Logger: logger}
	b := bus.New(nil, logger)
	t.Cleanup(b.Close)
	return c, b
}

func TestKanbanWiring_DisabledConstructsNothing(t *testing.T) {
	c, b := newKanbanWiringHarness(t, false)

	svc, err := wireKanbanServices(c, nil, b, nil)
	if err != nil {
		t.Fatalf("disabled wiring must return nil error, got %v", err)
	}
	if svc != nil {
		t.Fatal("disabled wiring must return a nil bundle")
	}
	// Byte-identical: not a single byte of the kanban dir exists.
	if _, err := os.Stat(filepath.Dir(kanbanStorePath())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled wiring must not create the store dir (stat err = %v)", err)
	}

	// RPC: a nil bundle registers NOTHING — the frozen C4 names must
	// answer "method not found", the pre-kanban daemon's exact shape.
	server := rpc.New(&rpc.Config{SocketPath: filepath.Join(t.TempDir(), "x.sock")}, b, nil)
	registerKanbanRPC(nil, server, nil)
	_, callErr := server.CallMethod(context.Background(), "kanban.board.list", nil)
	if callErr == nil {
		t.Fatal("disabled boot must not answer kanban.board.list")
	}
	if got := callErr.Error(); got != "method not found: kanban.board.list" {
		t.Fatalf("disabled call error = %q, want the byte-identical not-found text", got)
	}
	if _, callErr := server.CallMethod(context.Background(), "kanban.pair.mint", nil); callErr == nil {
		t.Fatal("disabled boot must not answer kanban.pair.mint")
	}
}

func TestKanbanWiring_DefaultConfigIsDisabled(t *testing.T) {
	// The opt-in-defaults rule pinned at the schema level: a config
	// without the [kanban] block must construct nothing.
	if config.DefaultConfig().Kanban.Enabled {
		t.Fatal("[kanban].enabled default must be false")
	}
	if config.DefaultConfig().Kanban.ReconcileInterval != "0s" {
		t.Fatal("[kanban].reconcile_interval default must be \"0s\"")
	}
}

func TestKanbanWiring_EnabledWiresStoreRulesAndProjection(t *testing.T) {
	c, b := newKanbanWiringHarness(t, true)

	svc, err := wireKanbanServices(c, nil, b, nil)
	if err != nil {
		t.Fatalf("enabled wiring: %v", err)
	}
	if svc == nil {
		t.Fatal("enabled wiring must return the bundle")
	}
	kanban.RegisterDefaultReconciler(svc.store, svc.reconciler)
	t.Cleanup(func() {
		svc.projector.Stop()
		kanban.UnregisterDefaultReconciler(svc.store)
	})

	// Store file created at the documented location.
	dbPath := kanbanStorePath()
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("store file missing at %s: %v", dbPath, err)
	}

	ctx := context.Background()

	// Default board with the three columns.
	if svc.boardID == "" {
		t.Fatal("default board id empty")
	}
	cols, err := svc.store.ListColumns(ctx, svc.boardID)
	if err != nil {
		t.Fatalf("list columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("columns = %d, want todo/doing/done", len(cols))
	}

	// Full seeded ruleset loaded in the projector's snapshot.
	rules, err := svc.store.LoadProjectionRules(ctx, svc.boardID)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	if want := len(kanbanDefaultRuleSeeds()); rules.Len() != want {
		t.Fatalf("rules = %d, want %d", rules.Len(), want)
	}
	if len(rules.Matches(kanban.EventKindTask, "pending")) == 0 {
		t.Fatal("seeded ruleset must map (task, pending)")
	}

	// A StateEvent on the live bus lands as a card through the REAL
	// projector: the wiring's own bus subscription is the live path.
	bus.PublishT(b, kanban.TopicTaskState, "task-registry", kanban.StateEvent{
		Kind: kanban.EventKindTask, ID: "t-live-1",
		From: "pending", To: "claimed", TS: time.Now().UTC(),
	})
	waitForKanbanCard(t, svc.store, kanban.EventKindTask, "t-live-1")

	card, err := svc.store.GetCardByRef(ctx, kanban.EventKindTask, "t-live-1")
	if err != nil {
		t.Fatalf("card after projection: %v", err)
	}
	todoID := colIDByName(t, cols, kanbanColTodo)
	if card.ColumnID != todoID {
		t.Fatalf("card landed in %s, want the todo column %s", card.ColumnID, todoID)
	}
	// The degraded machines answer "task registry not wired", so the
	// projector's documented fallback title applies.
	if card.Title != "task:t-live-1" {
		t.Fatalf("card title = %q, want the fetcher-fallback title", card.Title)
	}

	// RPC: the thirteen C4 methods (+ pairing) are live on the server.
	server := rpc.New(&rpc.Config{SocketPath: filepath.Join(t.TempDir(), "x.sock")}, b, nil)
	registerKanbanRPC(svc, server, nil)
	boards, callErr := server.CallMethod(ctx, "kanban.board.list", nil)
	if callErr != nil {
		t.Fatalf("enabled boot must answer kanban.board.list: %v", callErr)
	}
	if boards == nil {
		t.Fatal("kanban.board.list answered nil")
	}

	// Reconcile over the real fetchers with nil machines: every kind
	// errors -> skip-wholesale -> zero writes, never a wipe, never a
	// panic (reconcile.go posture pinned through the daemon's fetchers).
	stats, err := svc.reconciler.ReconcileNowStats(ctx)
	if err != nil {
		t.Fatalf("reconcile with unwired machines: %v", err)
	}
	if stats.Total() != 0 {
		t.Fatalf("unwired-machine reconcile changed %d rows, want 0 (skip-wholesale)", stats.Total())
	}
}

func TestKanbanWiring_RuleBootstrapIdempotent(t *testing.T) {
	c, b := newKanbanWiringHarness(t, true)
	svc, err := wireKanbanServices(c, nil, b, nil)
	if err != nil {
		t.Fatalf("first wiring: %v", err)
	}
	defer func() {
		svc.projector.Stop()
		kanban.UnregisterDefaultReconciler(svc.store)
	}()
	before := svc.boardID

	svc2, err := wireKanbanServices(c, nil, b, nil)
	if err != nil {
		t.Fatalf("second wiring: %v", err)
	}
	defer func() {
		svc2.projector.Stop()
		kanban.UnregisterDefaultReconciler(svc2.store)
	}()
	if svc2.boardID != before {
		t.Fatalf("second boot created a second board (%s != %s) — name idempotence broken", svc2.boardID, before)
	}
	rules, err := svc2.store.LoadProjectionRules(context.Background(), svc2.boardID)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	if rules.Len() != len(kanbanDefaultRuleSeeds()) {
		t.Fatalf("rules = %d after re-boot, want the seed count (upsert, no stacking)", rules.Len())
	}
}

func TestKanbanFromState_ReportsLastMappedFromState(t *testing.T) {
	// The CRITICAL semantic (reconcile.go WorkItemRef.State): every
	// mapping reports the from_state of the last MAPPED transition, so
	// the seeded ruleset + the projector event stream + the enumeration
	// lane resolve one column per item. Spot-check the contract edges
	// including the terminal ones pinned by TestReconcile_KilledProjector.
	cases := []struct {
		kind, state, want string
		ok                bool
	}{
		{kanban.EventKindJob, "completed", "processing", true},
		{kanban.EventKindJob, "processing", "claimed", true},
		{kanban.EventKindJob, "claimed", "pending", true},
		{kanban.EventKindJob, "pending", "pending", true},
		{kanban.EventKindJob, "dead", "", false},
		{kanban.EventKindTask, "completed", "testing", true},
		{kanban.EventKindTask, "pending", "pending", true},
		{kanban.EventKindPlan, "confirmed", "", false}, // parked posture
		{kanban.EventKindPlan, "executing", "synthesizing", true},
		{kanban.EventKindStep, "completed", "approved", true},
		{kanban.EventKindGoal, "active", "active", true},
		{kanban.EventKindGoal, "retired", "", false},
		{"mystery", "whatever", "", false},
	}
	for _, tc := range cases {
		got, ok := kanbanFromState(tc.kind, tc.state)
		if ok != tc.ok || got != tc.want {
			t.Errorf("kanbanFromState(%s,%s) = (%q,%v), want (%q,%v)", tc.kind, tc.state, got, ok, tc.want, tc.ok)
		}
	}

	// Closure property: EVERY enumerated from_state the map reports
	// exists in the seeded ruleset, so board == source cannot diverge
	// through a missing rule.
	seeded := map[string]bool{}
	for _, r := range kanbanDefaultRuleSeeds() {
		seeded[r.Kind+"\x00"+r.FromState] = true
	}
	for kind, states := range map[string][]string{
		kanban.EventKindJob:  {"pending", "claimed", "processing", "completed", "failed"},
		kanban.EventKindTask: {"pending", "planning", "awaiting_approval", "executing", "testing", "completed", "failed", "cancelled", "rejected"},
		kanban.EventKindPlan: {"planning", "draft", "pending_approval", "approved", "synthesizing", "executing", "completed", "cancelled", "failed"},
		kanban.EventKindGoal: {"active", "paused", "blocked"},
	} {
		for _, st := range states {
			from, ok := kanbanFromState(kind, st)
			if !ok {
				t.Errorf("live state %s/%s must enumerate", kind, st)
				continue
			}
			if !seeded[kind+"\x00"+from] {
				t.Errorf("from_state %s/%s is not covered by the seeded ruleset (board would diverge from source)", kind, from)
			}
		}
	}
}

func TestKanbanWiring_ReconcileTickerGatedByInterval(t *testing.T) {
	c, b := newKanbanWiringHarness(t, true)
	svc, err := wireKanbanServices(c, nil, b, nil)
	if err != nil {
		t.Fatalf("wiring: %v", err)
	}
	defer func() {
		svc.projector.Stop()
		kanban.UnregisterDefaultReconciler(svc.store)
	}()

	d := &Daemon{
		fullConfig: c.Config,
		kanban:     svc,
		logger:     c.Logger,
	}

	// C9 default "0s": no goroutine, no ticker, nothing to cancel.
	ctx, cancel := context.WithCancel(context.Background())
	d.startKanbanReconcile(ctx)
	cancel() // must be a clean no-op join

	// A real interval arms the loop; cancellation ends it.
	svc.cfg.ReconcileInterval = "50ms"
	ctx2, cancel2 := context.WithCancel(context.Background())
	d.startKanbanReconcile(ctx2)
	// One tick lands against the unwired fetchers (skip-wholesale,
	// zero rows) without panicking; cancel joins the loop.
	time.Sleep(120 * time.Millisecond)
	cancel2()
}

func waitForKanbanCard(t *testing.T, store *kanban.Store, kind, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.GetCardByRef(context.Background(), kind, id); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no card projected for %s/%s within 5s", kind, id)
}

func colIDByName(t *testing.T, cols []kanban.Column, name string) string {
	t.Helper()
	for _, c := range cols {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("column %q missing", name)
	return ""
}

// guard: the wiring's store path is $MEEPT_HOME-relative (documented
// choice); pin it so a refactor that silently moves the DB fails loud.
func TestKanbanWiring_StorePathUnderMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	want := filepath.Join(home, "kanban", "board.db")
	if got := kanbanStorePath(); got != want {
		t.Fatalf("kanbanStorePath = %s, want %s", got, want)
	}
	// tokens.json lives beside it (one directory for the subsystem).
	if filepath.Dir(kanbanStorePath()) != filepath.Dir(filepath.Join(home, "kanban", "tokens.json")) {
		t.Fatal("board.db must live in the same directory as the C7 token store")
	}
}
