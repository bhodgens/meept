// kanban_wiring.go — the LIVE kanban stack behind [kanban].enabled
// (plans/kanban-board 07/01-daemon-wiring). Disabled (the default) the
// wiring constructs NOTHING: no store file, no goroutine, no subscriber,
// no RPC registration — the boot path stays byte-identical per
// docs/agents/opt-in-defaults.md and contract C9.
//
// Construction order (mirrors master.md): Store -> Manager ->
// HumanCommands (real LeaseAuthority over the queue/task machinery,
// C10) -> default board + projection-rule bootstrap -> Projector on the
// live bus -> Reconciler (real RowFetcher + ListFetcher) -> publishers
// armed on all five machines -> kanban.* RPC (C4, the frozen thirteen,
// plus the C7 pairing mint) -> periodic reconcile ticker (C9).
//
// Package direction: the RowFetcher/ListFetcher/LeaseAuthority live HERE
// because internal/kanban may never import internal/task, internal/queue,
// internal/plan, internal/employee (projector.go / reconcile.go seam
// docs); the daemon owns all five machines and closes over them.
//
// # from_state semantics (CRITICAL — reconcile.go WorkItemRef.State)
//
// Rules key on (kind, FROM_state) — the state a transition LEAVES
// (projection_rules.go). The projector consumes StateEvent.From, so the
// reconcile enumeration lane must report, for an item whose CURRENT state
// is S, the from_state of the LAST MAPPED transition that placed it: the
// predecessor of S in the machine's state graph. Terminal states report
// the edge that transitioned them IN (a completed job reports
// "processing", mirroring TestReconcile_KilledProjectorRebuiltFromSource;
// a completed task reports "testing"). Initial states report themselves
// so a never-transitioned queued item lands in "todo" on BOTH lanes
// (the projector creates the card on the first mapped leave, reconcile
// materialises it at enumeration — same rule, same column).
// kanbanFromState below encodes those graphs; each entry is documented
// with the machine edge it derives from.
//
// # Store location + handle
//
// Board DB: $MEEPT_HOME/kanban/board.db (config.MeeptPath is THE
// resolution point; one directory with the C7 token-store file so the
// subsystem has one home). kanban.NewStore migrates on open and pins
// MaxOpenConns(1). stopKanban follows the drain-then-close invariant:
// projector Stop (unsubscribe + drain + join), reconcile unbind, then
// Store.Close on the single SQLite handle.
//
// # Seeded default ruleset (one-shot bootstrap)
//
// When the default board has NO rules yet, kanbanSeedRules upserts
// (SetProjectionRule is an idempotent upsert keyed on
// board/kind/from_state — re-runs never clobber operator edits) the
// todo/doing/done map below, mirroring the projector tests'
// standardRules shape (pending->todo, claimed->doing, processing->done)
// with each machine's own state names:
//
//	job:  pending->todo  claimed->doing  processing->done
//	task: pending->todo  planning->todo  awaiting_approval->doing
//	      executing->doing  testing->done  cancelled->done  rejected->done
//	plan: draft->todo  pending_approval->todo  planning->todo
//	      approved->doing  synthesizing->doing  executing->done
//	      cancelled->done  failed->done
//	step: pending->todo  ready->todo  scheduled->doing  running->doing
//	      reviewing->doing  approved->done  rejected->done
//	      failed->done  skipped->done
//	goal: active->doing  paused->todo  blocked->todo
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/internal/kanbansvc"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/pkg/id"
)

const (
	// kanbanDefaultBoardName identifies the daemon-created default
	// board. Idempotence is by NAME on the boards list (the store mints
	// ids for rows created without one, so a fixed id could collide with
	// a hand-created board; a fixed name means re-runs find the same
	// board and never duplicate it).
	kanbanDefaultBoardName = "meept board"

	// Standard column names (positions 0..2). Rule seeds speak column
	// NAMES; kanbanSeedRules resolves them to the board's minted column
	// ids — the same convention the projector fixture uses.
	kanbanColTodo  = "todo"
	kanbanColDoing = "doing"
	kanbanColDone  = "done"

	// listScanLimit bounds one enumeration per kind/per state, matching
	// the machines' own list verbs (ListPlansByState caps at 1000).
	// Items past the bound are simply not enumerated — a documented
	// coverage ceiling, not silent corruption.
	listScanLimit = 1000
)

// kanbanStorePath is the board DB location: $MEEPT_HOME/kanban/board.db,
// beside the C7 token store file ($MEEPT_HOME/kanban/tokens.json) so the
// whole subsystem lives in one home directory.
func kanbanStorePath() string {
	return config.MeeptPath("kanban", "board.db")
}

// kanbanServices is the live wiring bundle: constructed once by
// wireKanbanServices when [kanban].enabled, owned by the Daemon, torn
// down by stopKanban. The machines inside the fetchers may be nil (a
// degraded boot): every fetcher/authority method is nil-safe and answers
// "source unresolvable", which the projector maps to its ProvRow{}
// fallback (title "kind:id") and reconcile to skip-wholesale — never a
// wipe and never a panic.
type kanbanServices struct {
	store      *kanban.Store
	manager    *kanban.Manager
	human      *kanban.HumanCommands
	reconciler *kanban.Reconciler
	projector  *kanban.Projector
	authorizer *kanban.Authorizer
	pairing    *kanbanPairing
	boardID    string
	logger     *slog.Logger
	cfg        config.KanbanConfig
}

// wireKanbanServices builds the live kanban stack. A disabled (or
// missing) config returns (nil, nil) WITHOUT touching the filesystem —
// the byte-identical posture. Failures are returned for the caller to
// Warn-and-continue: the board subsystem degrades, the daemon boots
// (same non-fatal posture as the sidecar spawn).
//
// planStore is the live plan SQLite store (New() already holds it as
// planStoreIF): PlanManager exposes no global list verb, so enumeration
// reads through the PlanStore seam.
func wireKanbanServices(c *Components, planStore plan.PlanStore, msgBus *bus.MessageBus, logger *slog.Logger) (*kanbanServices, error) {
	if c == nil || c.Config == nil {
		return nil, nil //nolint:nilnil // documented opt-in posture: no components = board subsystem absent, not an error
	}
	cfg := c.Config.Kanban
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // [kanban].enabled=false = byte-identical boot; the caller treats (nil, nil) as "not opted in"
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "kanban")
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("kanban wiring: %w", err)
	}

	dbPath := kanbanStorePath()
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("kanban wiring: create store dir: %w", err)
		}
	}
	store, err := kanban.NewStore(dbPath, logger)
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: open store: %w", err)
	}

	manager, err := kanban.NewManager(store)
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: manager: %w", err)
	}

	fetcher := &daemonRowFetcher{c: c, planStore: planStore}
	listFetcher := &daemonListFetcher{c: c, planStore: planStore}
	authority := &daemonLeaseAuthority{c: c, logger: logger}

	human, err := kanban.NewHumanCommands(store, kanban.HumanOptions{
		WIPMode:   cfg.WIPMode,
		Bus:       msgBus,
		Authority: authority,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: human lane: %w", err)
	}

	ctx := context.Background()
	boardID, err := ensureKanbanDefaultBoard(ctx, store, logger)
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: default board: %w", err)
	}
	if err := kanbanSeedRules(ctx, store, boardID, logger); err != nil {
		return nil, fmt.Errorf("kanban wiring: rule seed: %w", err)
	}

	rules, err := store.LoadProjectionRules(ctx, boardID)
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: load rules: %w", err)
	}
	projector, err := kanban.NewProjector(msgBus, store, rules, fetcher, kanban.WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("kanban wiring: projector: %w", err)
	}

	reconciler, err := kanban.NewReconcilerWithProvenance(store, listFetcher, fetcher)
	if err != nil {
		projector.Stop()
		return nil, fmt.Errorf("kanban wiring: reconciler: %w", err)
	}
	kanban.RegisterDefaultReconciler(store, reconciler)

	// Arm the five machines' opt-in StateEvent publishers (leaf 02/03 +
	// 02/04 gates). ONLY reached when enabled — the disabled boot never
	// calls these setters, so every machine stays byte-identical.
	armKanbanPublishers(c, msgBus, logger)

	pairing, err := newKanbanPairing(logger)
	if err != nil {
		// Pairing is a degrade, not a boot failure: the board itself
		// works over the owner socket; only browser tokens are offline.
		logger.Warn("kanban pairing degraded (codes cannot be minted)", "error", err)
		pairing = nil
	}

	return &kanbanServices{
		store:      store,
		manager:    manager,
		human:      human,
		reconciler: reconciler,
		projector:  projector,
		authorizer: kanban.NewAuthorizer(c.Config, c.AuthUsersStore),
		pairing:    pairing,
		boardID:    boardID,
		logger:     logger,
		cfg:        cfg,
	}, nil
}

// registerKanbanRPC registers the thirteen C4 methods plus the C7
// pairing mint on the live RPC server. A nil bundle (=
// [kanban].enabled false) registers NOTHING: every method answers
// "method not found" exactly like a pre-kanban daemon (byte-identical),
// and the CLI/sidecar surfaces render that as the disabled posture.
// Same call-site pattern as registerIntakeStatusRPC.
func registerKanbanRPC(svc *kanbanServices, server *rpc.Server, logger *slog.Logger) {
	if server == nil || svc == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	handler := rpc.NewKanbanHandler(rpc.KanbanDeps{
		Store:      svc.store,
		Manager:    svc.manager,
		Human:      svc.human,
		Reconciler: svc.reconciler,
		Authorizer: svc.authorizer,
	})
	handler.RegisterKanbanMethods(server)

	// C7 pairing mint + exchange (the 14th/15th methods are deliberately
	// OUTSIDE the frozen C4 thirteen — the board surface stays
	// byte-exact): mint prints a single-use code ONCE to the daemon
	// console (the designed delivery channel, same contract as
	// internal/comm/http/pairing.go); exchange consumes it for the
	// stored bearer token. They ride the owner Unix socket (0600,
	// owner-trusted by invariant) and reach the GUI through the existing
	// POST /api/v1/bus/call RPC bridge — mounting the literal HTTP path
	// requires a one-line hook in internal/comm/http (sibling-occupied;
	// the sidecar already serves its own /api/v1/pair/exchange lane).
	server.RegisterHandler("kanban.pair.mint", func(_ context.Context, params json.RawMessage) (any, error) {
		if svc.pairing == nil {
			return nil, errors.New("kanban.pair.mint: pairing store unavailable")
		}
		var req struct {
			Grants []string `json:"grants"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("kanban.pair.mint: decode params: %w", err)
			}
		}
		code, err := svc.pairing.Mint(req.Grants)
		if err != nil {
			return nil, err
		}
		return map[string]any{"code": code}, nil
	})
	server.RegisterHandler("kanban.pair.exchange", func(_ context.Context, params json.RawMessage) (any, error) {
		if svc.pairing == nil {
			return nil, errors.New("kanban.pair.exchange: pairing store unavailable")
		}
		var req struct {
			Code string `json:"code"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("kanban.pair.exchange: decode params: %w", err)
			}
		}
		token, grants, err := svc.pairing.Exchange(req.Code)
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": token, "grants": grants}, nil
	})
	logger.Info("kanban RPC registered", "methods", 15, "board", svc.boardID)
}

// registerKanbanTools arms the tool-provider seam and registers the
// kanban_* group. Runs from New() right after the wiring, BEFORE the
// C6 live-registry check in Components.Start (registration-at-
// construction is the github precedent, components.go:3857). Disabled
// wiring never reaches SetKanbanProvider, so the provider factory stays
// unbound, RegisterAll fails closed, and the registry is byte-identical.
func registerKanbanTools(cfg *config.Config, registry *tools.Registry, svc *kanbanServices, logger *slog.Logger) {
	if cfg == nil || registry == nil || svc == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	builtin.SetKanbanProvider(svc.store, svc.human, cfg.Kanban.Enabled)
	names := enabledToolProviderNames(cfg, []string{"kanban"}, logger)
	if len(names) == 0 {
		logger.Debug("kanban provider disabled by [tool_providers]; no kanban tools registered")
		return
	}
	if _, err := toolprov.RegisterAll(registry, names); err != nil {
		logger.Error("kanban tool registration failed", "error", err)
		return
	}
	logger.Info("kanban tools registered (seven board tools behind [kanban].enabled)")
}

// armKanbanPublishers turns on the opt-in kanban.event.*.state publishers
// on all five machines. Each machine is nil-guarded: a degraded boot
// (registry build failed) leaves that lane silent. Goal/step machines
// publish through a bus installed at arm time (they hold no bus from
// construction).
func armKanbanPublishers(c *Components, msgBus *bus.MessageBus, logger *slog.Logger) {
	if c == nil {
		return
	}
	if pq, ok := c.Queue.(*queue.PersistentQueue); ok && pq != nil {
		pq.SetKanbanEventPublishing(true)
	}
	if c.TaskRegistry != nil {
		c.TaskRegistry.SetKanbanEventPublishing(true)
	}
	if c.StepStore != nil {
		c.StepStore.SetKanbanEventBus(msgBus)
		c.StepStore.SetKanbanEventPublishing(true)
	}
	if c.PlanManager != nil {
		c.PlanManager.SetKanbanEventPublishing(true)
	}
	if c.EmployeeGoalStore != nil {
		c.EmployeeGoalStore.SetKanbanEventBus(msgBus)
		c.EmployeeGoalStore.SetKanbanEventPublishing(true)
	}
	logger.Info("kanban state publishers armed",
		"queue", c.Queue != nil,
		"task_registry", c.TaskRegistry != nil,
		"step_store", c.StepStore != nil,
		"plan_manager", c.PlanManager != nil,
		"goal_store", c.EmployeeGoalStore != nil)
}

// startKanbanReconcile arms the C9 periodic rebuild ticker on the daemon
// run context ([kanban].reconcile_interval, default "0s" = disabled = no
// goroutine at all; Run's ctx cancels on shutdown and RunPeriodicReconcile
// returns with it).
func (d *Daemon) startKanbanReconcile(ctx context.Context) {
	if d == nil || d.kanban == nil {
		return
	}
	svc := d.kanban
	interval, err := time.ParseDuration(svc.cfg.ReconcileInterval)
	if err != nil || interval <= 0 {
		if svc.cfg.ReconcileInterval != "" && err != nil {
			svc.logger.Warn("kanban reconcile_interval unparseable; periodic rebuild disabled",
				"value", svc.cfg.ReconcileInterval, "error", err)
		}
		return
	}
	go func() {
		if rerr := kanban.RunPeriodicReconcile(ctx, svc.store, interval); rerr != nil &&
			!errors.Is(rerr, context.Canceled) && !errors.Is(rerr, context.DeadlineExceeded) {
			svc.logger.Warn("kanban periodic reconcile loop exited", "error", rerr)
		}
	}()
	svc.logger.Info("kanban periodic reconcile armed", "interval", interval.String())
}

// stopKanban tears the live stack down on daemon shutdown: projector
// first (unsubscribe, drain the handoff queue into the store, join the
// worker), then the reconcile unbind (the ticker itself is driven by the
// Run context, which shutdown cancels before reaching here), and ONLY
// then Store.Close — the drain-then-close order the MaxOpenConns(1)
// invariant requires: no writer may still hold the single connection
// when the pool drains. A Close failure is logged, never fatal: the
// process is leaving. No-op when the subsystem was never enabled
// (byte-identical shutdown).
func (d *Daemon) stopKanban() {
	if d == nil || d.kanban == nil {
		return
	}
	svc := d.kanban
	stats := svc.projector.Stats()
	svc.projector.Stop()
	kanban.UnregisterDefaultReconciler(svc.store)
	d.kanban = nil
	if err := svc.store.Close(); err != nil {
		d.logger.Warn("kanban subsystem: store close failed", "error", err)
	}
	d.logger.Info("kanban subsystem stopped",
		"projected", stats.Processed, "dropped", stats.Dropped,
		"wip_overflow", stats.WIPOverflow, "failed", stats.Failed)
}

// ---------------------------------------------------------------------------
// default board + rule bootstrap
// ---------------------------------------------------------------------------

// ensureKanbanDefaultBoard creates the default board with the three
// standard columns (todo/doing/done in position order) on first boot;
// idempotent by board name. Returns the board id either way.
func ensureKanbanDefaultBoard(ctx context.Context, store *kanban.Store, logger *slog.Logger) (string, error) {
	boards, err := store.ListBoards(ctx)
	if err != nil {
		return "", fmt.Errorf("list boards: %w", err)
	}
	var boardID string
	for _, b := range boards {
		if b.Name == kanbanDefaultBoardName {
			boardID = b.ID
			break
		}
	}
	if boardID == "" {
		boardID = id.Generate("board-")
		if err := store.CreateBoard(ctx, kanban.Board{ID: boardID, Name: kanbanDefaultBoardName}); err != nil {
			return "", fmt.Errorf("create board: %w", err)
		}
		logger.Info("kanban default board created", "board", boardID)
	}
	cols, err := store.ListColumns(ctx, boardID)
	if err != nil {
		return "", fmt.Errorf("list columns: %w", err)
	}
	have := make(map[string]bool, len(cols))
	for _, col := range cols {
		have[col.Name] = true
	}
	for pos, name := range []string{kanbanColTodo, kanbanColDoing, kanbanColDone} {
		if have[name] {
			continue
		}
		if err := store.CreateColumn(ctx, kanban.Column{
			BoardID:  boardID,
			Name:     name,
			Position: pos,
		}); err != nil {
			return "", fmt.Errorf("create column %s: %w", name, err)
		}
	}
	return boardID, nil
}

// kanbanDefaultRuleSeeds is the seeded default ruleset documented in this
// file's header: (kind, from_state) -> column NAME. The from_state keys
// are exactly the values kanbanFromState reports for that machine, which
// is what makes the projector lane and the reconcile lane resolve the
// same column for the same item (board == source).
func kanbanDefaultRuleSeeds() []kanban.ProjectionRule {
	type seed struct {
		kind, from, col string
	}
	seeds := []seed{
		// job (internal/queue): pending -> claimed -> processing ->
		// completed | failed.
		{kanban.EventKindJob, "pending", kanbanColTodo},
		{kanban.EventKindJob, "claimed", kanbanColDoing},
		{kanban.EventKindJob, "processing", kanbanColDone},
		// task (internal/task): pending -> planning -> awaiting_approval
		// -> executing -> testing -> completed (+cancelled/rejected).
		{kanban.EventKindTask, "pending", kanbanColTodo},
		{kanban.EventKindTask, "planning", kanbanColTodo},
		{kanban.EventKindTask, "awaiting_approval", kanbanColDoing},
		{kanban.EventKindTask, "executing", kanbanColDoing},
		{kanban.EventKindTask, "testing", kanbanColDone},
		{kanban.EventKindTask, "cancelled", kanbanColDone},
		{kanban.EventKindTask, "rejected", kanbanColDone},
		// plan (internal/plan): draft -> pending_approval -> approved ->
		// synthesizing -> executing -> completed -> confirmed.
		{kanban.EventKindPlan, "draft", kanbanColTodo},
		{kanban.EventKindPlan, "pending_approval", kanbanColTodo},
		{kanban.EventKindPlan, "planning", kanbanColTodo},
		{kanban.EventKindPlan, "approved", kanbanColDoing},
		{kanban.EventKindPlan, "synthesizing", kanbanColDoing},
		{kanban.EventKindPlan, "executing", kanbanColDone},
		{kanban.EventKindPlan, "cancelled", kanbanColDone},
		{kanban.EventKindPlan, "failed", kanbanColDone},
		// step (internal/task step.go): pending -> ready -> scheduled ->
		// running -> reviewing -> approved -> completed (+skipped).
		{kanban.EventKindStep, "pending", kanbanColTodo},
		{kanban.EventKindStep, "ready", kanbanColTodo},
		{kanban.EventKindStep, "scheduled", kanbanColDoing},
		{kanban.EventKindStep, "running", kanbanColDoing},
		{kanban.EventKindStep, "reviewing", kanbanColDoing},
		{kanban.EventKindStep, "approved", kanbanColDone},
		{kanban.EventKindStep, "rejected", kanbanColDone},
		{kanban.EventKindStep, "failed", kanbanColDone},
		{kanban.EventKindStep, "skipped", kanbanColDone},
		// goal (internal/employee): active/paused/blocked (+retired,
		// which is never enumerated).
		{kanban.EventKindGoal, "active", kanbanColDoing},
		{kanban.EventKindGoal, "paused", kanbanColTodo},
		{kanban.EventKindGoal, "blocked", kanbanColTodo},
	}
	out := make([]kanban.ProjectionRule, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, kanban.ProjectionRule{Kind: s.kind, FromState: s.from, ColumnID: s.col})
	}
	return out
}

// kanbanSeedRules upserts the default ruleset onto the board ONLY when
// the board has no rules yet — an operator's edited ruleset is never
// clobbered. Column names are resolved to the board's column ids by
// name; a missing standard column is a bootstrap error (fail loud at
// boot, not silently at first event).
func kanbanSeedRules(ctx context.Context, store *kanban.Store, boardID string, logger *slog.Logger) error {
	existing, err := store.LoadProjectionRules(ctx, boardID)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	if existing.Len() > 0 {
		return nil // already configured: bootstrap is one-shot
	}
	cols, err := store.ListColumns(ctx, boardID)
	if err != nil {
		return fmt.Errorf("list columns: %w", err)
	}
	byName := make(map[string]string, len(cols))
	for _, col := range cols {
		byName[col.Name] = col.ID
	}
	for _, r := range kanbanDefaultRuleSeeds() {
		colID, ok := byName[r.ColumnID]
		if !ok {
			return fmt.Errorf("seed rule %s/%s names missing column %q", r.Kind, r.FromState, r.ColumnID)
		}
		r.BoardID = boardID
		r.ColumnID = colID
		if err := store.SetProjectionRule(ctx, r); err != nil {
			return fmt.Errorf("seed rule %s/%s: %w", r.Kind, r.FromState, err)
		}
	}
	logger.Info("kanban default projection rules seeded",
		"board", boardID, "rules", len(kanbanDefaultRuleSeeds()))
	return nil
}

// ---------------------------------------------------------------------------
// RowFetcher (provenance) + ListFetcher (enumeration, from_state)
// ---------------------------------------------------------------------------

// daemonRowFetcher implements kanban.RowFetcher over the live machine
// stores. Read-only and nil-safe: a machine absent from the boot returns
// an error ("source unresolvable"), which the projector maps to its
// ProvRow{} fallback — projection never dies on a missing row.
type daemonRowFetcher struct {
	c         *Components
	planStore plan.PlanStore
}

var _ kanban.RowFetcher = (*daemonRowFetcher)(nil)

func (f *daemonRowFetcher) TaskRow(id string) (kanban.ProvRow, error) {
	if f.c == nil || f.c.TaskRegistry == nil {
		return kanban.ProvRow{}, errors.New("task registry not wired")
	}
	t, err := f.c.TaskRegistry.Get(context.Background(), id)
	if err != nil {
		return kanban.ProvRow{}, fmt.Errorf("task row %s: %w", id, err)
	}
	if t == nil {
		return kanban.ProvRow{}, fmt.Errorf("task row %s: not found", id)
	}
	row := kanban.ProvRow{
		Title:  t.Name,
		GoalID: t.GoalID,
		// The task's assigned agent is its executor identity; the task
		// row has no separate employee id, so AssignedAgent rides
		// EmployeeID (documented mapping, same as the job lane).
		EmployeeID: t.AssignedAgent,
	}
	if len(t.LinkedSessions) > 0 {
		row.SessionID = t.LinkedSessions[0]
	}
	if planID := f.planIDForTask(t.ID); planID != "" {
		row.PlanID = planID
	}
	return row, nil
}

func (f *daemonRowFetcher) JobRow(id string) (kanban.ProvRow, error) {
	if f.c == nil || f.c.Queue == nil {
		return kanban.ProvRow{}, errors.New("queue not wired")
	}
	job, err := f.c.Queue.Get(context.Background(), id)
	if err != nil {
		return kanban.ProvRow{}, fmt.Errorf("job row %s: %w", id, err)
	}
	if job == nil {
		return kanban.ProvRow{}, fmt.Errorf("job row %s: not found", id)
	}
	row := kanban.ProvRow{
		Title: jobTitle(job),
		// The job's target agent is its executor identity; the job row
		// carries no employee id, so AgentID rides EmployeeID (the same
		// documented mapping as the task lane).
		EmployeeID: job.AgentID,
	}
	if job.TaskID != "" {
		row.TriggerRef = "task:" + job.TaskID
		if f.c.TaskRegistry != nil {
			if t, terr := f.c.TaskRegistry.Get(context.Background(), job.TaskID); terr == nil && t != nil {
				row.GoalID = t.GoalID
			}
		}
		if planID := f.planIDForTask(job.TaskID); planID != "" {
			row.PlanID = planID
		}
	}
	return row, nil
}

func (f *daemonRowFetcher) PlanRow(id string) (kanban.ProvRow, error) {
	if f.planStore == nil {
		return kanban.ProvRow{}, errors.New("plan store not wired")
	}
	p, err := f.planStore.GetPlan(context.Background(), id)
	if err != nil {
		return kanban.ProvRow{}, fmt.Errorf("plan row %s: %w", id, err)
	}
	if p == nil {
		return kanban.ProvRow{}, fmt.Errorf("plan row %s: not found", id)
	}
	return kanban.ProvRow{
		Title:     p.Title,
		GoalID:    p.GoalID,
		PlanID:    p.ID,
		SessionID: p.SourceSession,
	}, nil
}

func (f *daemonRowFetcher) StepRow(id string) (kanban.ProvRow, error) {
	if f.c == nil || f.c.StepStore == nil {
		return kanban.ProvRow{}, errors.New("step store not wired")
	}
	st, err := f.c.StepStore.GetByID(id)
	if err != nil {
		return kanban.ProvRow{}, fmt.Errorf("step row %s: %w", id, err)
	}
	if st == nil {
		return kanban.ProvRow{}, fmt.Errorf("step row %s: not found", id)
	}
	row := kanban.ProvRow{
		Title:      stepTitle(st),
		EmployeeID: st.AgentID,
		SessionID:  st.SessionID,
	}
	if st.TaskID != "" {
		row.TriggerRef = "task:" + st.TaskID
		if f.c.TaskRegistry != nil {
			if t, terr := f.c.TaskRegistry.Get(context.Background(), st.TaskID); terr == nil && t != nil {
				row.GoalID = t.GoalID
			}
		}
		if planID := f.planIDForTask(st.TaskID); planID != "" {
			row.PlanID = planID
		}
	}
	return row, nil
}

func (f *daemonRowFetcher) GoalRow(id string) (kanban.ProvRow, error) {
	if f.c == nil || f.c.EmployeeGoalStore == nil {
		return kanban.ProvRow{}, errors.New("goal store not wired")
	}
	g, err := f.c.EmployeeGoalStore.Get(context.Background(), id)
	if err != nil {
		return kanban.ProvRow{}, fmt.Errorf("goal row %s: %w", id, err)
	}
	if g == nil {
		return kanban.ProvRow{}, fmt.Errorf("goal row %s: not found", id)
	}
	return kanban.ProvRow{
		Title:      g.Title,
		GoalID:     g.ID,
		EmployeeID: g.EmployeeID,
		PlanID:     g.ActivePlanID,
		TriggerRef: g.TriggerRef,
	}, nil
}

// planIDForTask resolves the plan a task belongs to from the plan rows'
// own TaskID column (the authoritative plan->task direction). Scans the
// live plan states only — a bounded best-effort: absent -> empty
// provenance, never an error.
func (f *daemonRowFetcher) planIDForTask(taskID string) string {
	if f.planStore == nil || taskID == "" {
		return ""
	}
	for _, state := range []plan.PlanState{plan.StateExecuting, plan.StateSynthesizing, plan.StateCompleted} {
		plans, err := f.planStore.ListPlansByState(context.Background(), state, listScanLimit)
		if err != nil {
			return ""
		}
		for _, p := range plans {
			if p != nil && p.TaskID == taskID {
				return p.ID
			}
		}
	}
	return ""
}

// jobTitle renders a job card title from its envelope without importing
// the per-type payload structs (payload is machine-shaped JSON; title/
// description/message are the display keys, else job:<id> (<type>)).
func jobTitle(job *queue.Job) string {
	if len(job.Payload) > 0 {
		var probe struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Message     string `json:"message"`
		}
		if err := json.Unmarshal(job.Payload, &probe); err == nil {
			for _, s := range []string{probe.Title, probe.Description, probe.Message} {
				if s = strings.TrimSpace(s); s != "" {
					if len(s) > 120 {
						s = s[:120] + "..."
					}
					return s
				}
			}
		}
	}
	return fmt.Sprintf("job:%s (%s)", job.ID, job.Type)
}

// stepTitle renders a step card title: description wins, the phase tag
// keeps multi-phase plans readable.
func stepTitle(st *task.TaskStep) string {
	title := strings.TrimSpace(st.Description)
	if title == "" {
		title = fmt.Sprintf("step:%s", st.ID)
	}
	if st.Phase != "" {
		return "[" + st.Phase + "] " + title
	}
	return title
}

// daemonListFetcher implements kanban.ListFetcher: enumeration of LIVE
// work items per kind, each carrying the from_state of the last mapped
// transition per the WorkItemRef.State contract (see kanbanFromState).
// A machine absent from the boot returns an error per kind, which
// reconcile handles as skip-wholesale (never a wipe).
type daemonListFetcher struct {
	c         *Components
	planStore plan.PlanStore
}

var _ kanban.ListFetcher = (*daemonListFetcher)(nil)

func (f *daemonListFetcher) ListWorkItems(ctx context.Context, kind string) ([]kanban.WorkItemRef, error) {
	switch kind {
	case kanban.EventKindTask:
		return f.listTasks(ctx)
	case kanban.EventKindJob:
		return f.listJobs(ctx)
	case kanban.EventKindPlan:
		return f.listPlans(ctx)
	case kanban.EventKindStep:
		return f.listSteps(ctx)
	case kanban.EventKindGoal:
		return f.listGoals(ctx)
	default:
		return nil, fmt.Errorf("kanban list fetcher: unknown kind %q", kind)
	}
}

func (f *daemonListFetcher) listTasks(ctx context.Context) ([]kanban.WorkItemRef, error) {
	if f.c == nil || f.c.TaskRegistry == nil {
		return nil, errors.New("task registry not wired")
	}
	// List(nil state) enumerates every retained task: the task machine
	// has NO archived class (purge deletes the row), so terminal tasks
	// STAY live-enumerated — their done cards must not read as orphans.
	tasks, err := f.c.TaskRegistry.List(ctx, nil, listScanLimit)
	if err != nil {
		return nil, fmt.Errorf("enumerate tasks: %w", err)
	}
	out := make([]kanban.WorkItemRef, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		from, ok := kanbanFromState(kanban.EventKindTask, string(t.State))
		if !ok {
			continue // unknown state name: nothing maps it, skip
		}
		out = append(out, kanban.WorkItemRef{Kind: kanban.EventKindTask, ID: t.ID, State: from})
	}
	return out, nil
}

func (f *daemonListFetcher) listJobs(ctx context.Context) ([]kanban.WorkItemRef, error) {
	if f.c == nil || f.c.Queue == nil {
		return nil, errors.New("queue not wired")
	}
	// Every state EXCEPT dead letter: a dead job is the retired class
	// (its card parks until purge deletes the row; absent == deleted,
	// matching operator cleanup). completed/failed STAY enumerated: the
	// projector never deletes a done card, and dropping terminal rows
	// here would make reconcile delete exactly those cards (board !=
	// source; reconcile.go: only retired/archived rows must be absent).
	states := []queue.JobState{queue.StatePending, queue.StateClaimed, queue.StateProcessing, queue.StateCompleted, queue.StateFailed}
	out := make([]kanban.WorkItemRef, 0, 64)
	for _, st := range states {
		jobs, err := f.c.Queue.ListByState(ctx, st, listScanLimit)
		if err != nil {
			return nil, fmt.Errorf("enumerate jobs (%s): %w", st, err)
		}
		for _, j := range jobs {
			if j == nil {
				continue
			}
			from, ok := kanbanFromState(kanban.EventKindJob, string(j.State))
			if !ok {
				continue
			}
			out = append(out, kanban.WorkItemRef{Kind: kanban.EventKindJob, ID: j.ID, State: from})
		}
	}
	return out, nil
}

func (f *daemonListFetcher) listPlans(ctx context.Context) ([]kanban.WorkItemRef, error) {
	if f.planStore == nil {
		return nil, errors.New("plan store not wired")
	}
	// Every plan state EXCEPT confirmed: confirmation is the plan
	// machine's archive verb (the card already parked in the done column
	// executing placed it in; confirmation taking it out of enumeration
	// deletes it — the machine's own "closed" disposition, same posture
	// as a retired goal / dead job).
	states := []plan.PlanState{
		plan.StatePlanning, plan.StateDraft, plan.StatePendingApproval,
		plan.StateApproved, plan.StateSynthesizing, plan.StateExecuting,
		plan.StateCompleted, plan.StateCancelled, plan.StateFailed,
	}
	out := make([]kanban.WorkItemRef, 0, 64)
	for _, st := range states {
		plans, err := f.planStore.ListPlansByState(ctx, st, listScanLimit)
		if err != nil {
			return nil, fmt.Errorf("enumerate plans (%s): %w", st, err)
		}
		for _, p := range plans {
			if p == nil {
				continue
			}
			from, ok := kanbanFromState(kanban.EventKindPlan, string(p.State))
			if !ok {
				continue
			}
			out = append(out, kanban.WorkItemRef{Kind: kanban.EventKindPlan, ID: p.ID, State: from})
		}
	}
	return out, nil
}

func (f *daemonListFetcher) listSteps(ctx context.Context) ([]kanban.WorkItemRef, error) {
	if f.c == nil || f.c.StepStore == nil || f.c.TaskRegistry == nil {
		return nil, errors.New("step store not wired")
	}
	// The step store keys steps by task; enumerate through the task scan
	// (steps per task are bounded far below listScanLimit; DeleteByTaskID
	// on task purge removes the archived class, matching reconcile).
	tasks, err := f.c.TaskRegistry.List(ctx, nil, listScanLimit)
	if err != nil {
		return nil, fmt.Errorf("enumerate steps (task scan): %w", err)
	}
	out := make([]kanban.WorkItemRef, 0, 64)
	for _, t := range tasks {
		if t == nil {
			continue
		}
		steps, serr := f.c.StepStore.ListByTaskID(t.ID)
		if serr != nil {
			return nil, fmt.Errorf("enumerate steps for task %s: %w", t.ID, serr)
		}
		for _, s := range steps {
			if s == nil {
				continue
			}
			from, ok := kanbanFromState(kanban.EventKindStep, string(s.State))
			if !ok {
				continue
			}
			out = append(out, kanban.WorkItemRef{Kind: kanban.EventKindStep, ID: s.ID, State: from})
		}
	}
	return out, nil
}

func (f *daemonListFetcher) listGoals(ctx context.Context) ([]kanban.WorkItemRef, error) {
	if f.c == nil || f.c.EmployeeGoalStore == nil {
		return nil, errors.New("goal store not wired")
	}
	// ListActive excludes retired goals already (retired == the
	// archived class: absence deletes the card). active/paused/blocked
	// are all live.
	goals, err := f.c.EmployeeGoalStore.ListActive(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("enumerate goals: %w", err)
	}
	out := make([]kanban.WorkItemRef, 0, len(goals))
	for _, g := range goals {
		if g == nil {
			continue
		}
		from, ok := kanbanFromState(kanban.EventKindGoal, g.State.String())
		if !ok {
			continue
		}
		out = append(out, kanban.WorkItemRef{Kind: kanban.EventKindGoal, ID: g.ID, State: from})
	}
	return out, nil
}

// kanbanFromState maps a machine's CURRENT state name to the from_state
// of the last mapped transition that placed the item (reconcile.go
// WorkItemRef.State contract — the SAME key the projector consumed from
// StateEvent.From, so both lanes resolve one column per item). Each
// entry is the predecessor edge of the machine's state graph:
//
//   - job (internal/queue/queue.go transitions): Claim pending->claimed,
//     MarkProcessing claimed->processing, CompleteAttempt/FailAttempt
//     processing->terminal (queue.go:712-777 + store SQL).
//   - task (internal/task/task.go graph): pending->planning->
//     awaiting_approval->executing->testing->completed; CancelTask/
//     reject leave the live state they name below.
//   - plan (internal/plan/manager.go verbs): SubmitPlan draft->
//     pending_approval, ApprovePlan ->approved, ClaimPlanSynthesis
//     approved->synthesizing, expansion ->executing, completePlan
//     ->completed; RejectPlan/CancelPlan/failPlan are terminal leaves.
//   - step (internal/task/step.go SetState graph): pending->ready->
//     scheduled->running->reviewing->approved->completed.
//   - goal (internal/employee/goal.go): the fence allows
//     active<->paused/blocked; retired is terminal and unenumerated.
//
// Initial/queued classes report themselves so a never-transitioned item
// resolves the todo column on both lanes; states with NO mapped inbound
// edge (plan "confirmed", job "dead") return false: the item PARKS in
// whatever column its last mapped edge placed it in (reconcile.go parked
// posture) instead of lying about a transition. Unknown names return
// false for the same reason — schema drift parks, never misfiles.
func kanbanFromState(kind, state string) (string, bool) {
	switch kind {
	case kanban.EventKindJob:
		switch state {
		case "pending":
			return "pending", true // initial class -> todo
		case "claimed":
			return "pending", true // Claim: pending -> claimed
		case "processing":
			return "claimed", true // MarkProcessing: claimed -> processing
		case "completed":
			return "processing", true // CompleteAttempt: processing -> completed
		case "failed":
			return "processing", true // FailAttempt: processing -> failed (processing maps done)
		}
	case kanban.EventKindTask:
		switch state {
		case "pending":
			return "pending", true // initial class
		case "planning":
			return "pending", true // pending -> planning
		case "awaiting_approval":
			return "planning", true // planning -> awaiting_approval
		case "executing":
			return "awaiting_approval", true
		case "testing":
			return "executing", true
		case "completed":
			return "testing", true
		case "failed":
			return "testing", true // step-failure path arrives from executing/testing
		case "cancelled":
			return "executing", true // CancelTask from the live execution edge
		case "rejected":
			return "awaiting_approval", true
		}
	case kanban.EventKindPlan:
		switch state {
		case "draft", "planning":
			return state, true // initial classes -> todo
		case "pending_approval":
			return "draft", true // SubmitPlan: draft -> pending_approval
		case "approved":
			return "pending_approval", true // ApprovePlan
		case "synthesizing":
			return "approved", true // ClaimPlanSynthesis (lease claim)
		case "executing":
			return "synthesizing", true // expansion succeeded
		case "completed":
			return "executing", true // completePlan
		case "confirmed":
			return "", false // park: executing already placed the done column
		case "cancelled", "failed":
			return state, true // terminal leaves seeded -> done
		}
	case kanban.EventKindStep:
		switch state {
		case "pending", "ready":
			return state, true // initial classes -> todo
		case "scheduled":
			return "ready", true // PromoteReadySteps: ready -> scheduled
		case "running":
			return "scheduled", true
		case "reviewing":
			return "running", true
		case "approved":
			return "reviewing", true
		case "completed":
			return "approved", true
		case "rejected":
			return "reviewing", true // review rejection leaves reviewing
		case "failed":
			return "running", true // failure leaves running
		case "skipped":
			return "skipped", true // seeded -> done (no live inbound edge)
		}
	case kanban.EventKindGoal:
		switch state {
		case "active":
			return "active", true // seeded -> doing (creation lands active)
		case "paused", "blocked":
			return state, true // seeded -> todo
		case "retired":
			return "", false // never enumerated (archived class); belt-and-braces park
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// LeaseAuthority (C10) over the real queue/task machinery
// ---------------------------------------------------------------------------

// daemonLeaseAuthority implements kanban.LeaseAuthority against the live
// claim machinery. Lease rule A (C10): a human move onto a column mapped
// to an active state of a card whose work ref holds a live claim is
// refused; takeover is the only bypass and runs CancelEpisode BEFORE
// ReleaseToken (the order is owned by human.go's Takeover).
type daemonLeaseAuthority struct {
	c      *Components
	logger *slog.Logger
}

var _ kanban.LeaseAuthority = (*daemonLeaseAuthority)(nil)

// LookupLease reports live claims:
//   - job: claimed/processing = held by claimed_by since updated_at
//     (the leaf's pinned mapping over the queue.Store row fields).
//   - task: executing/testing = held by the assigned agent (the task
//     lane's claim analogue: its driving episode).
//   - step: running/reviewing = held by the step's agent.
//   - plan: synthesizing with an UNEXPIRED synthesis lease = held by the
//     synthesizer (SynthesisLeaseUntil is the deadline the machine
//     itself enforces — after expiry the plan is reclaimable, so no
//     lease is held; reclaimer precedent, manager.go).
//   - goal: active = held by the owning employee (the goal loop's live
//     round drives it).
//
// Unresolvable machines or rows report NOT held: a false refusal
// strands a card forever, a missed refusal is recoverable via the
// projector's next write — fail-open on read is the correct direction
// for the refusal gate (the mutation itself still audits in human.go).
func (a *daemonLeaseAuthority) LookupLease(ctx context.Context, ref kanban.WorkRef) (string, time.Time, bool, error) {
	switch ref.Kind {
	case kanban.EventKindJob:
		pq, ok := a.persistentQueue()
		if !ok {
			return "", time.Time{}, false, nil
		}
		job, err := pq.Get(ctx, ref.ID)
		if err != nil || job == nil {
			return "", time.Time{}, false, nil //nolint:nilerr // fail-open read (documented above): a machine that cannot answer reports NOT held
		}
		if job.State == queue.StateClaimed || job.State == queue.StateProcessing {
			holder := job.ClaimedBy
			if holder == "" {
				holder = "worker"
			}
			return holder, job.UpdatedAt, true, nil
		}
		return "", time.Time{}, false, nil

	case kanban.EventKindTask:
		if a.c == nil || a.c.TaskRegistry == nil {
			return "", time.Time{}, false, nil
		}
		t, err := a.c.TaskRegistry.Get(ctx, ref.ID)
		if err != nil || t == nil {
			return "", time.Time{}, false, nil //nolint:nilerr // fail-open read (documented above): unreadable task row reports NOT held
		}
		if t.State == task.StateExecuting || t.State == task.StateTesting {
			holder := t.AssignedAgent
			if holder == "" {
				holder = "agent"
			}
			return holder, t.UpdatedAt, true, nil
		}
		return "", time.Time{}, false, nil

	case kanban.EventKindStep:
		if a.c == nil || a.c.StepStore == nil {
			return "", time.Time{}, false, nil
		}
		st, err := a.c.StepStore.GetByID(ref.ID)
		if err != nil || st == nil {
			return "", time.Time{}, false, nil //nolint:nilerr // fail-open read (documented above): unreadable step row reports NOT held
		}
		if st.State == task.StepRunning || st.State == task.StepReviewing {
			holder := st.AgentID
			if holder == "" {
				holder = "agent"
			}
			return holder, st.UpdatedAt, true, nil
		}
		return "", time.Time{}, false, nil

	case kanban.EventKindPlan:
		if a.c == nil || a.c.PlanManager == nil {
			return "", time.Time{}, false, nil
		}
		p, err := a.c.PlanManager.GetPlan(ctx, ref.ID)
		if err != nil || p == nil {
			return "", time.Time{}, false, nil //nolint:nilerr // fail-open read (documented above): unreadable plan row reports NOT held
		}
		if p.State == plan.StateSynthesizing && p.SynthesisLeaseUntil != nil &&
			time.Now().Before(*p.SynthesisLeaseUntil) {
			return "synthesizer", p.UpdatedAt, true, nil
		}
		return "", time.Time{}, false, nil

	case kanban.EventKindGoal:
		if a.c == nil || a.c.EmployeeGoalStore == nil {
			return "", time.Time{}, false, nil
		}
		g, err := a.c.EmployeeGoalStore.Get(ctx, ref.ID)
		if err != nil || g == nil {
			return "", time.Time{}, false, nil //nolint:nilerr // fail-open read (documented above): unreadable goal row reports NOT held
		}
		if g.State == employee.GoalActive {
			holder := g.EmployeeID
			if holder == "" {
				holder = "employee"
			}
			return holder, g.CreatedAt, true, nil
		}
		return "", time.Time{}, false, nil
	}
	return "", time.Time{}, false, nil
}

// CancelEpisode stops the agent episode driving the work item, mirroring
// the daemon's existing cancel surfaces:
//   - task: Registry.CancelTask (interrupt trigger + StateCancelled) —
//     the exact path services/task_service.go Cancel uses (M-4 bughunt
//     reference, registry.go:303).
//   - job: interrupt the owning task (the job's episode IS its task's
//     agent run; jobs carry no separate interrupt id — the
//     interrupt-aware claiming callback proves the task-token
//     coupling). A standalone job has no episode; only the claim
//     release lands (ReleaseToken).
//   - step: interrupt the owning task (the step's worker runs under the
//     task's interrupt token).
//   - plan: PlanManager.CancelPlan (the machine's own cancel verb).
//   - goal: pause it (GoalPaused through the C-P3 transition fence with
//     the human-bypass flag — takeover IS a human gesture, the same
//     operator authority the fence grants unblocks).
func (a *daemonLeaseAuthority) CancelEpisode(ctx context.Context, ref kanban.WorkRef) error {
	switch ref.Kind {
	case kanban.EventKindJob:
		if a.c == nil || a.c.TaskRegistry == nil || a.c.Queue == nil {
			return nil
		}
		job, err := a.c.Queue.Get(ctx, ref.ID)
		if err != nil || job == nil || job.TaskID == "" {
			return nil //nolint:nilerr // takeover best-effort: an unreadable job has no episode to cancel
		}
		a.triggerInterrupt(job.TaskID, "kanban takeover (job)")
		return nil
	case kanban.EventKindTask:
		if a.c == nil || a.c.TaskRegistry == nil {
			return nil
		}
		return a.c.TaskRegistry.CancelTask(ctx, ref.ID, "kanban takeover")
	case kanban.EventKindStep:
		if a.c == nil || a.c.StepStore == nil {
			return nil
		}
		st, err := a.c.StepStore.GetByID(ref.ID)
		if err != nil || st == nil {
			return nil //nolint:nilerr // takeover best-effort: an unreadable step has no episode to cancel
		}
		if st.TaskID != "" && a.c.TaskRegistry != nil {
			a.triggerInterrupt(st.TaskID, "kanban takeover (step)")
		}
		return nil
	case kanban.EventKindPlan:
		if a.c == nil || a.c.PlanManager == nil {
			return nil
		}
		return a.c.PlanManager.CancelPlan(ctx, ref.ID, "kanban takeover")
	case kanban.EventKindGoal:
		if a.c == nil || a.c.EmployeeGoalStore == nil {
			return nil
		}
		g, err := a.c.EmployeeGoalStore.Get(ctx, ref.ID)
		if err != nil || g == nil {
			return nil //nolint:nilerr // takeover best-effort: an unreadable goal is nothing to pause
		}
		if g.State == employee.GoalRetired {
			return nil
		}
		g.State = employee.GoalPaused
		if err := a.c.EmployeeGoalStore.UpdateWithTransition(ctx, g, true); err != nil &&
			!errors.Is(err, employee.ErrGoalTransition) {
			return fmt.Errorf("pause goal %s: %w", g.ID, err)
		}
		return nil
	}
	return nil
}

// ReleaseToken releases the machine claim so no worker completes under
// the broken lease. Job: PersistentQueue.Requeue (failed|claimed|
// processing -> pending, claim_token cleared — the scheduler's own
// take-back verb, daemon.go:2703 Retry precedent's stricter sibling).
// Task: requeue every claimed/processing job of the task (the task lane
// holds claims only through its jobs). Step: requeue its job.
// Plan/goal: the CancelEpisode state change IS the release (synthesis
// lease and goal parking are the machine-side holds), so no-op.
func (a *daemonLeaseAuthority) ReleaseToken(ctx context.Context, ref kanban.WorkRef) error {
	pq, ok := a.persistentQueue()
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	switch ref.Kind {
	case kanban.EventKindJob:
		job, err := pq.Get(ctx, ref.ID)
		if err != nil || job == nil {
			return nil //nolint:nilerr // release best-effort: an unreadable job has no claim to release
		}
		if job.State == queue.StateClaimed || job.State == queue.StateProcessing || job.State == queue.StateFailed {
			return pq.Requeue(ctx, ref.ID, now)
		}
		return nil
	case kanban.EventKindTask:
		jobs, err := pq.ListByTaskID(ctx, ref.ID)
		if err != nil {
			return nil //nolint:nilerr // release best-effort: unenumerable jobs cannot be force-released
		}
		for _, j := range jobs {
			if j == nil {
				continue
			}
			if j.State == queue.StateClaimed || j.State == queue.StateProcessing {
				if rerr := pq.Requeue(ctx, j.ID, now); rerr != nil {
					a.leaseLogger().Warn("kanban takeover: job requeue failed",
						"job", j.ID, "error", rerr)
				}
			}
		}
		return nil
	case kanban.EventKindStep:
		if a.c == nil || a.c.StepStore == nil {
			return nil
		}
		st, err := a.c.StepStore.GetByID(ref.ID)
		if err != nil || st == nil || st.JobID == "" {
			return nil //nolint:nilerr // release best-effort: an unreadable step job has no claim to release
		}
		job, gerr := pq.Get(ctx, st.JobID)
		if gerr != nil || job == nil {
			return nil //nolint:nilerr // release best-effort: an unreadable queue job has no claim to release
		}
		if job.State == queue.StateClaimed || job.State == queue.StateProcessing {
			return pq.Requeue(ctx, job.ID, now)
		}
		return nil
	}
	return nil
}

func (a *daemonLeaseAuthority) triggerInterrupt(taskID, msg string) {
	if a.c == nil || a.c.TaskRegistry == nil {
		return
	}
	im := a.c.TaskRegistry.InterruptManager()
	if im == nil {
		return
	}
	if err := im.Trigger(taskID, task.ReasonUserCancelled, msg); err != nil {
		a.leaseLogger().Debug("kanban takeover: interrupt trigger failed",
			"task", taskID, "error", err)
	}
}

func (a *daemonLeaseAuthority) persistentQueue() (*queue.PersistentQueue, bool) {
	if a.c == nil {
		return nil, false
	}
	pq, ok := a.c.Queue.(*queue.PersistentQueue)
	return pq, ok && pq != nil
}

func (a *daemonLeaseAuthority) leaseLogger() *slog.Logger {
	if a.logger == nil {
		return slog.Default()
	}
	return a.logger
}

// ---------------------------------------------------------------------------
// C7 pairing: daemon-side mint
// ---------------------------------------------------------------------------

// kanbanPairing owns the daemon-side half of the C7 handshake: the
// $MEEPT_HOME/kanban/tokens.json TokenStore shared with the sidecar
// (7481f498 + 2da662e9 mounted the sidecar's POST exchange against the
// same file), code minting, and the single console print. Codes are
// IN-MEMORY in whichever process mints them (kanbansvc pairing.go — the
// tokens persist, the codes do not), so a daemon-minted code is
// exchangeable only against the daemon process; the sidecar redeems
// codes IT minted. The browser-facing exchange therefore runs on the
// daemon, where the mint happens: this pairing bundle is mounted on
// the daemon HTTP mux as POST /kanban/pair/exchange
// (internal/comm/http/kanban_pairing.go, loopback-only + rate-limited
// per the dev-key precedent), alongside the owner-socket RPC pair. A
// shared code store for sidecar-side redemption of daemon codes remains
// owned by a follow-up on 04-sidecar/03, NOT faked here.
type kanbanPairing struct {
	store  *kanbansvc.TokenStore
	logger *slog.Logger
}

func newKanbanPairing(logger *slog.Logger) (*kanbanPairing, error) {
	ts, err := kanbansvc.NewTokenStore(kanbansvc.DefaultTokenStorePath())
	if err != nil {
		return nil, fmt.Errorf("kanban pairing store: %w", err)
	}
	return &kanbanPairing{store: ts, logger: logger}, nil
}

// Mint creates a single-use pairing code carrying the requested grants
// (default: read+mutate), prints it ONCE to the daemon console (the
// designed delivery channel — the code never reaches slog at any level,
// same invariant as internal/comm/http/pairing.go) and returns the
// grouped form for the RPC reply, so an operator driving the mint over
// the owner socket sees exactly what the console printed.
func (p *kanbanPairing) Mint(grants []string) (string, error) {
	if p == nil || p.store == nil {
		return "", errors.New("kanban pairing store unavailable")
	}
	if len(grants) == 0 {
		grants = []string{kanban.GrantBoardRead, kanban.GrantBoardMutate}
	}
	code, err := p.store.GeneratePairCode(grants...)
	if err != nil {
		return "", fmt.Errorf("kanban pair mint: %w", err)
	}
	printed := formatKanbanPairingCode(code)
	fmt.Fprintln(os.Stdout, "kanban pairing code (single-use): "+printed)
	p.logger.Info("kanban pairing code minted", "grants", strings.Join(grants, ","))
	return printed, nil
}

// Exchange consumes a presented single-use code (daemon-minted) and
// returns the bearer token + grant set. A wrong, already-consumed, or
// missing code answers ONE uniform error — the code cannot be oracled.
// The code and token never reach the structured logger.
func (p *kanbanPairing) Exchange(code string) (string, []string, error) {
	if p == nil || p.store == nil {
		return "", nil, errors.New("kanban pairing store unavailable")
	}
	token, grants, err := p.store.ExchangePairCode(code)
	if err != nil {
		return "", nil, fmt.Errorf("kanban pair exchange: invalid or already-used code")
	}
	return token, grants, nil
}

// formatKanbanPairingCode groups a raw code 4-4 for typing (the same
// presentation contract as the dev-key pairing code). The grouping is
// display-only: ExchangePairCode normalizes away separators.
func formatKanbanPairingCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}
