// Package builtin — kanban_register.go populates the toolprov provider seam
// for the kanban_* tool set (plan tree plans/kanban-board/06-wiring-delivery/
// 01-mcp-tool-provider.md), mirroring the github lane (github_register.go)
// and the goal lane (goal_register.go):
//
//  1. kanbanToolFactories - a builtin-local construction seam (the
//     githubsvc.ToolFactories precedent; kanban owns no factories package,
//     so the mirror list lives here). init() populates it.
//  2. toolprov.Register("kanban", ...) - the compiled-in provider seam. The
//     provider's Discover() constructs the SAME seven kanban_* tools with
//     the same constructor arguments as the factories closure, so both
//     paths serve identical tool sets. Two identical lists pinned by
//     TestKanbanProviderMirrorsFactoryList - a new kanban_* tool MUST be
//     added to BOTH lists (the same red-flag rule as the github_* tools).
//
// The construction inputs (live *kanban.Store / *kanban.HumanCommands and
// the [kanban].enabled visibility flag) are runtime values only the daemon
// can supply (leaf 07/01 daemon wiring), so this registers a provider
// FACTORY whose inputs are captured via SetKanbanProvider rather than at
// init time. An unbound factory returns a nil Provider and
// toolprov.RegisterAll fails closed - never a panic, zero kanban tools.
//
// Visibility gate: Discover() returns NOTHING when the bound [kanban].enabled
// flag is false (KanbanConfig.Enabled, default false / opt-in). The
// [tool_providers] "kanban" enabled=false skip is the daemon-side filter
// exactly like "github" (internal/daemon/toolprovider_config.go); this
// package cannot read config, so the daemon passes [kanban].enabled into
// SetKanbanProvider and 07/01 additionally honours the tool_providers map.
//
// kanban_claim is DELIBERATELY EXCLUDED from this set per master.md
// decision 1: agents hold claims via their work items (claim
// acquisition/release stays on the internal queue/task machinery); the
// board only REFUSES human moves on claimed cards and offers
// kanban_takeover as the bypass. Do not add a claim tool.
package builtin

import (
	"sync"

	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

func init() {
	// The construction seam: seven tools, same constructors/arguments as
	// kanbanProvider.Discover below. Mirrored lists - keep identical.
	kanbanToolFactories = func(board KanbanBoard, human KanbanHumanLane) []tools.Tool {
		return []tools.Tool{
			NewKanbanListTool(board, human),
			NewKanbanShowTool(board, human),
			NewKanbanCreateTool(board, human),
			NewKanbanMoveTool(board, human),
			NewKanbanTakeoverTool(board, human),
			NewKanbanAssignTool(board, human),
			NewKanbanCommentTool(board, human),
		}
	}

	// Register the provider factory; a duplicate "kanban" registration can
	// only happen if this init runs twice (impossible in one binary), so an
	// error here is a programming failure surfaced at startup - never
	// silently swallowed.
	if err := toolprov.Register("kanban", kanbanProviderFactory); err != nil {
		panic("builtin: kanban provider registration: " + err.Error())
	}
}

// kanbanToolFactories is the builtin-local construction seam mirrored by
// the provider's Discover(). nil only before init() has run (never in a
// linked binary).
var kanbanToolFactories func(board KanbanBoard, human KanbanHumanLane) []tools.Tool

// kanbanProviderSnapshot is the frozen binding SetKanbanProvider stores.
// The provider constructed from it never re-reads shared mutable state.
type kanbanProviderSnapshot struct {
	board   KanbanBoard
	human   KanbanHumanLane
	enabled bool
}

var (
	// kanbanProviderMu guards the snapshot pointer. No I/O is ever done
	// under it: the critical sections are one map-free assignment each.
	kanbanProviderMu sync.Mutex
	// kanbanProviderSnapshot is nil = unbound (the fail-closed default).
	kanbanProviderSnapshotVal *kanbanProviderSnapshot
)

// kanbanProviderFactory is the thunk handed to toolprov.Register. It
// returns a provider constructed from the frozen snapshot the daemon
// captured via SetKanbanProvider. An unbound seam (no SetKanbanProvider
// yet - the default) returns nil and toolprov.RegisterAll fails closed,
// exactly like the goal / github lanes.
func kanbanProviderFactory() toolprov.Provider {
	kanbanProviderMu.Lock()
	snap := kanbanProviderSnapshotVal
	kanbanProviderMu.Unlock()
	if snap == nil {
		return nil
	}
	return &kanbanProvider{board: snap.board, human: snap.human, enabled: snap.enabled}
}

// SetKanbanProvider binds the kanban provider's construction inputs: the
// board store seam (live *kanban.Store), the human lane seam (live
// *kanban.HumanCommands), and the [kanban].enabled visibility flag. Typed-
// nil guards reject a true nil or a nil pointer boxed into the interface
// (the classic boxed-nil wiring bug): a nil binding leaves the provider
// unbound so it fails closed instead of arming tools against a dead store.
func SetKanbanProvider(board KanbanBoard, human KanbanHumanLane, enabled bool) {
	kanbanProviderMu.Lock()
	defer kanbanProviderMu.Unlock()
	if reflectNil(board) || reflectNil(human) {
		kanbanProviderSnapshotVal = nil
		return
	}
	kanbanProviderSnapshotVal = &kanbanProviderSnapshot{board: board, human: human, enabled: enabled}
}

// ResetKanbanProviderForTest restores the unbound (default) factory state
// in tests that bind the provider before checking the fail-closed default.
// Production never calls this.
func ResetKanbanProviderForTest() {
	kanbanProviderMu.Lock()
	defer kanbanProviderMu.Unlock()
	kanbanProviderSnapshotVal = nil
}

// kanbanProvider is the toolprov.Provider for the compiled-in kanban tool
// group.
type kanbanProvider struct {
	board   KanbanBoard
	human   KanbanHumanLane
	enabled bool
}

// Name implements toolprov.Provider - the [tool_providers] config key.
func (p *kanbanProvider) Name() string { return "kanban" }

// Discover implements toolprov.Provider. It mirrors the kanbanToolFactories
// closure exactly: same tools, same order, same constructor arguments -
// pinned by TestKanbanProviderMirrorsFactoryList. The [kanban].enabled
// visibility gate short-circuits to NOTHING when disabled, so agents never
// see kanban tools on a board the operator has not opted into.
//
// kanban_claim is excluded by master.md decision 1 (claims live on the
// queue/task machinery; the board's human lane only refuses moves on
// claimed cards and offers takeover) - the seven below are the frozen
// contract set of leaf 06/01.
func (p *kanbanProvider) Discover() []tools.Tool {
	if !p.enabled {
		return nil
	}
	return []tools.Tool{
		NewKanbanListTool(p.board, p.human),
		NewKanbanShowTool(p.board, p.human),
		NewKanbanCreateTool(p.board, p.human),
		NewKanbanMoveTool(p.board, p.human),
		NewKanbanTakeoverTool(p.board, p.human),
		NewKanbanAssignTool(p.board, p.human),
		NewKanbanCommentTool(p.board, p.human),
	}
}

// PermissionClass implements toolprov.Provider. The kanban group spans
// read-only board lookups AND mutating writes (create, move, takeover,
// assign, comment), so its ceiling is mutating - it never spawns
// processes, so it is not privileged.
func (p *kanbanProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider. The kanban tools hold no secret
// material (the board store is in-process), so the broker binding is nil.
func (p *kanbanProvider) Broker() *secrets.Broker { return nil }
