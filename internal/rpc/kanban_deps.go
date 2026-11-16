package rpc

// kanban_deps.go — the injected seam for the C4 kanban.* RPC unit
// (leaf 05/01, live wiring 07/01). Narrow interfaces, not concrete types:
// the daemon's live *kanban.Store/Manager/HumanCommands/Reconciler/Authorizer
// instances satisfy them structurally, and tests fake them without SQLite.

import (
	"context"

	"github.com/caimlas/meept/internal/kanban"
)

// KanbanBoardStore is the read/create seam for board/column/card rows.
// *kanban.Store satisfies it.
type KanbanBoardStore interface {
	ListBoards(ctx context.Context) ([]kanban.Board, error)
	CreateBoard(ctx context.Context, b kanban.Board) error
	ListColumns(ctx context.Context, boardID string) ([]kanban.Column, error)
	ListCards(ctx context.Context, columnID string) ([]kanban.Card, error)
	GetCard(ctx context.Context, cardID string) (kanban.Card, error)
	CreateCard(ctx context.Context, c kanban.Card) error
	// UpdateCard is the CAS card write; the RPC layer uses it ONLY for the
	// explicit move-order hint (CLI --order / Dart order) that
	// HumanCommands.MoveCard has no verb for.
	UpdateCard(ctx context.Context, c kanban.Card) error
}

// KanbanBoardManager is the column-mutation seam. *kanban.Manager satisfies
// it. Column CREATION is deliberately exposed through the store seam
// (CreateCard/CreateBoard symmetry with the C4 surface); set_wip is the
// Manager verb C4 names.
type KanbanBoardManager interface {
	SetWIPLimit(ctx context.Context, b kanban.Board, colID string, limit int) error
}

// KanbanHumanLane is the human-command seam. *kanban.HumanCommands satisfies
// it. Lease/WIP enforcement and timeline audit live there — this layer
// delegates and maps, never reimplements.
type KanbanHumanLane interface {
	MoveCard(ctx context.Context, cardID, fromCol, toCol, actor string) error
	Takeover(ctx context.Context, cardID, actor string) error
	TakeoverMove(ctx context.Context, cardID, fromCol, toCol, actor string) error
	Assign(ctx context.Context, cardID, employeeID string) error
	Comment(ctx context.Context, cardID, actor, text string) (kanban.CardEvent, error)
	Timeline(ctx context.Context, cardID string) ([]kanban.CardEvent, error)
}

// KanbanReconcileLane is the rebuild seam. *kanban.Reconciler satisfies it.
// ReconcileNowStats is the counters variant the RPC uses so the reply can
// carry the per-pass ReconcileStats both readers render; ReconcileNow stays
// on the seam for the frozen leaf contract (07/01's ticker path uses it).
type KanbanReconcileLane interface {
	ReconcileNow(ctx context.Context) error
	ReconcileNowStats(ctx context.Context) (kanban.ReconcileStats, error)
}

// KanbanGrantGate is the C5 grant check. *kanban.Authorizer satisfies it.
type KanbanGrantGate interface {
	CheckGrant(principal string, grant string) bool
}

// KanbanDeps wires the registerable unit. 07/01-daemon-wiring constructs the
// live store/manager/human/reconciler/authorizer and passes them here; tests
// pass fakes. Every field may be nil: a missing dep answers with an
// internal-code envelope, never a panic and never "method not found"
// (intake_status.go posture).
type KanbanDeps struct {
	Store      KanbanBoardStore
	Manager    KanbanBoardManager
	Human      KanbanHumanLane
	Reconciler KanbanReconcileLane
	Authorizer KanbanGrantGate
}
