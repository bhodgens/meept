// Package builtin — kanban_tools.go implements the model-facing kanban_*
// tool set (plan tree plans/kanban-board/06-wiring-delivery/01-mcp-tool-provider.md):
// kanban_list / kanban_show / kanban_create / kanban_move / kanban_takeover /
// kanban_assign / kanban_comment.
//
// kanban_claim is DELIBERATELY EXCLUDED per plans/kanban-board/master.md
// decision 1: agents hold claims via their work items (the queue/task
// machinery owns claim acquisition/release); the board only REFUSES human
// moves on claimed cards and offers kanban_takeover as the bypass. Do not
// add a claim tool.
//
// Layering (master.md "no logic duplication" review check): the tools call
// the committed internal/kanban SERVICE layer directly (Store for
// reads/creates, HumanCommands for the lease/WIP-governed mutations), NOT
// the kanban.* RPC surface — same posture as internal/rpc/kanban.go, which
// documents the identical rule for the RPC lane. Lease rule A, WIP policy,
// audit-before-effect ordering, and takeover semantics all live in
// internal/kanban; these tools only decode args, delegate, and serialise.
//
// Dependency shape: the tools sit on the narrow KanbanBoard / KanbanHumanLane
// interfaces below (compile-time pinned to *kanban.Store /
// *kanban.HumanCommands) so the daemon wiring leaf (07/01) binds the live
// instances through SetKanbanProvider (kanban_register.go) and unit tests
// fake them. Nil deps are legal at construction: every Execute answers a
// clear "not wired" error and never panics.
//
// Reply shape mirrors the C4 wire DTOs in internal/rpc/kanban.go (snake_case
// keys: id/name/board_id/column_id/title/ref/labels/goal_id/employee_id/
// plan_id/session_id/trigger_ref/order, events carry kind/actor/body/
// created_at as RFC3339 UTC) so surfaces and agents see one shape.
package builtin

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// KanbanBoard is the read/create seam over the board store, satisfied by
// *kanban.Store. It is exactly the method set the kanban_* tools consume;
// widening it (e.g. adding DeleteCard) without a tool that needs it is
// discouraged so fakes stay small.
type KanbanBoard interface {
	ListBoards(ctx context.Context) ([]kanban.Board, error)
	ListColumns(ctx context.Context, boardID string) ([]kanban.Column, error)
	ListCards(ctx context.Context, columnID string) ([]kanban.Card, error)
	GetCard(ctx context.Context, cardID string) (kanban.Card, error)
	CreateCard(ctx context.Context, c kanban.Card) error
	UpdateCard(ctx context.Context, c kanban.Card) error
}

// KanbanHumanLane is the human-mutation seam, satisfied by
// *kanban.HumanCommands. MoveCard/Takeover/TakeoverMove/Assign/Comment
// carry lease rule A + WIP policy; Timeline is the read HumanCommands
// owns (C3/C4 posture).
type KanbanHumanLane interface {
	MoveCard(ctx context.Context, cardID, fromCol, toCol, actor string) error
	Takeover(ctx context.Context, cardID, actor string) error
	TakeoverMove(ctx context.Context, cardID, fromCol, toCol, actor string) error
	Assign(ctx context.Context, cardID, employeeID string) error
	Comment(ctx context.Context, cardID, actor, text string) (kanban.CardEvent, error)
	Timeline(ctx context.Context, cardID string) ([]kanban.CardEvent, error)
}

// Compile-time proof the live service types satisfy the seams (leaf 07/01
// wires these exact concrete instances — same pins as internal/rpc/kanban.go).
var (
	_ KanbanBoard     = (*kanban.Store)(nil)
	_ KanbanHumanLane = (*kanban.HumanCommands)(nil)
)

// kanbanDefaultActor is the audit actor stamped on a mutation when the
// calling turn supplies none. HumanCommands refuses an empty actor, so the
// fallback chain must never yield "". The RPC lane resolves richer
// identities from the caller context; the tool lane's caller is the agent
// itself, so "agent" is the honest sentinel.
const kanbanDefaultActor = "agent"

// ---- shared errors + arg helpers -------------------------------------------

// errKanbanBoardNotWired / errKanbanHumanNotWired mirror the RPC lane's
// "store not wired" posture: an unbound seam answers a descriptive error,
// never a panic.
var (
	errKanbanBoardNotWired = errors.New("kanban tool: board store not wired (07/01 daemon wiring pending)")
	errKanbanHumanNotWired = errors.New("kanban tool: human lane not wired (07/01 daemon wiring pending)")
)

// kanbanOptString returns args[key] when it is a non-empty string.
func kanbanOptString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// kanbanOptInt accepts an int, a float64 (the JSON number decode shape), or
// a decimal string, returning ok=false when absent or unparseable.
func kanbanOptInt(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// kanbanOptStringSlice decodes an optional array-of-strings arg.
func kanbanOptStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---- wire DTOs (mirror internal/rpc/kanban.go cardDTO et al.) ---------------

func kanbanBoardDTO(b kanban.Board) map[string]any {
	return map[string]any{"id": b.ID, "name": b.Name}
}

func kanbanColumnDTO(c kanban.Column) map[string]any {
	return map[string]any{
		"id": c.ID, "board_id": c.BoardID, "name": c.Name,
		"position": c.Position, "wip": c.WIP,
	}
}

func kanbanCardDTO(c kanban.Card) map[string]any {
	labels := c.Labels
	if labels == nil {
		labels = []string{}
	}
	return map[string]any{
		"id": c.ID, "board_id": c.BoardID, "column_id": c.ColumnID,
		"title":   c.Title,
		"ref":     map[string]any{"kind": c.Ref.Kind, "id": c.Ref.ID},
		"labels":  labels,
		"goal_id": c.GoalID, "employee_id": c.EmployeeID,
		"plan_id": c.PlanID, "session_id": c.SessionID,
		"trigger_ref": c.TriggerRef, "order": c.Order,
	}
}

func kanbanCardEventDTO(ev kanban.CardEvent) map[string]any {
	return map[string]any{
		"id": ev.ID, "card_id": ev.CardID, "kind": ev.Kind,
		"actor": ev.Actor, "body": ev.Body,
		"created_at": ev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func kanbanCardDTOs(cs []kanban.Card) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, kanbanCardDTO(c))
	}
	return out
}

func kanbanOK() map[string]any { return map[string]any{"ok": true} }

// ---- kanban_list -------------------------------------------------------------

// KanbanListTool reads the board: all boards (no args), one board's
// columns + aggregated cards (board_id), or one column's cards (column_id).
type KanbanListTool struct {
	tools.ToolDefaults
	board KanbanBoard
}

// NewKanbanListTool constructs kanban_list.
func NewKanbanListTool(board KanbanBoard, _ KanbanHumanLane) *KanbanListTool {
	return &KanbanListTool{board: board}
}

func (t *KanbanListTool) Name() string     { return "kanban_list" }
func (t *KanbanListTool) Category() string { return "kanban" }
func (t *KanbanListTool) Description() string {
	return "List the kanban board: all boards with no args; a board's columns and cards with board_id; one column's cards with column_id."
}
func (t *KanbanListTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"board_id":  {Type: "string", Description: "Optional board id: lists that board's columns and all its cards."},
			"column_id": {Type: "string", Description: "Optional column id: lists only that column's cards (wins over board_id)."},
		},
	}
}

func (t *KanbanListTool) IsReadOnly(map[string]any) bool        { return true }
func (t *KanbanListTool) IsConcurrencySafe(map[string]any) bool { return true }

func (t *KanbanListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.board == nil {
		return nil, errKanbanBoardNotWired
	}
	columnID := kanbanOptString(args, "column_id")
	boardID := kanbanOptString(args, "board_id")
	out := kanbanOK()
	switch {
	case columnID != "":
		cards, err := t.board.ListCards(ctx, columnID)
		if err != nil {
			return nil, fmt.Errorf("kanban_list: %w", err)
		}
		out["cards"] = kanbanCardDTOs(cards)
	case boardID != "":
		columns, err := t.board.ListColumns(ctx, boardID)
		if err != nil {
			return nil, fmt.Errorf("kanban_list columns: %w", err)
		}
		cols := make([]map[string]any, 0, len(columns))
		for _, c := range columns {
			cols = append(cols, kanbanColumnDTO(c))
		}
		out["columns"] = cols
		var cards []kanban.Card
		for _, col := range columns {
			colCards, err := t.board.ListCards(ctx, col.ID)
			if err != nil {
				return nil, fmt.Errorf("kanban_list cards: %w", err)
			}
			cards = append(cards, colCards...)
		}
		out["cards"] = kanbanCardDTOs(cards)
	default:
		boards, err := t.board.ListBoards(ctx)
		if err != nil {
			return nil, fmt.Errorf("kanban_list boards: %w", err)
		}
		dtos := make([]map[string]any, 0, len(boards))
		for _, b := range boards {
			dtos = append(dtos, kanbanBoardDTO(b))
		}
		out["boards"] = dtos
	}
	return out, nil
}

// ---- kanban_show -------------------------------------------------------------

// KanbanShowTool returns one card plus its timeline (C3 append-only
// card_events, oldest-first — the same read kanban.card.timeline serves).
type KanbanShowTool struct {
	tools.ToolDefaults
	board KanbanBoard
	human KanbanHumanLane
}

// NewKanbanShowTool constructs kanban_show.
func NewKanbanShowTool(board KanbanBoard, human KanbanHumanLane) *KanbanShowTool {
	return &KanbanShowTool{board: board, human: human}
}

func (t *KanbanShowTool) Name() string     { return "kanban_show" }
func (t *KanbanShowTool) Category() string { return "kanban" }
func (t *KanbanShowTool) Description() string {
	return "Show one kanban card with its full provenance (goal/employee/plan/session refs) and timeline of events."
}
func (t *KanbanShowTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"card_id": {Type: "string", Description: "Card id to inspect."},
		},
		Required: []string{"card_id"},
	}
}

func (t *KanbanShowTool) IsReadOnly(map[string]any) bool { return true }

func (t *KanbanShowTool) IsConcurrencySafe(map[string]any) bool { return true }

func (t *KanbanShowTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.board == nil {
		return nil, errKanbanBoardNotWired
	}
	if t.human == nil {
		return nil, errKanbanHumanNotWired
	}
	cardID, err := argString(args, "card_id")
	if err != nil {
		return nil, err
	}
	card, err := t.board.GetCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("kanban_show: %w", err)
	}
	events, err := t.human.Timeline(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("kanban_show timeline: %w", err)
	}
	out := kanbanOK()
	out["card"] = kanbanCardDTO(card)
	dtos := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		dtos = append(dtos, kanbanCardEventDTO(ev))
	}
	out["events"] = dtos
	return out, nil
}

// ---- kanban_create -------------------------------------------------------------

// KanbanCreateTool creates a manual card. Provenance fields ride through
// untouched (C1); the minted id is echoed so agents can follow the card.
type KanbanCreateTool struct {
	tools.ToolDefaults
	board KanbanBoard
}

// NewKanbanCreateTool constructs kanban_create.
func NewKanbanCreateTool(board KanbanBoard, _ KanbanHumanLane) *KanbanCreateTool {
	return &KanbanCreateTool{board: board}
}

func (t *KanbanCreateTool) Name() string     { return "kanban_create" }
func (t *KanbanCreateTool) Category() string { return "kanban" }
func (t *KanbanCreateTool) Description() string {
	return "Create a kanban card in a board column (board_id + column_id + title required; optional labels, work ref, provenance ids)."
}
func (t *KanbanCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"board_id":    {Type: "string", Description: "Target board id."},
			"column_id":   {Type: "string", Description: "Target column id."},
			"title":       {Type: "string", Description: "Card title."},
			"labels":      {Type: "array", Description: "Optional label strings.", Items: &llm.ParameterProperty{Type: "string"}},
			"ref_kind":    {Type: "string", Description: "Optional work-ref kind (step|goal|job|ticket|plan)."},
			"ref_id":      {Type: "string", Description: "Optional work-ref id of the source work item."},
			"employee_id": {Type: "string", Description: "Optional owning employee id."},
			"goal_id":     {Type: "string", Description: "Optional linked goal id."},
			"plan_id":     {Type: "string", Description: "Optional linked plan id."},
			"trigger_ref": {Type: "string", Description: "Optional provenance trigger ref."},
			"order":       {Type: "integer", Description: "Optional manual ordering position within the column."},
		},
		Required: []string{"board_id", "column_id", "title"},
	}
}

func (t *KanbanCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.board == nil {
		return nil, errKanbanBoardNotWired
	}
	boardID, err := argString(args, "board_id")
	if err != nil {
		return nil, err
	}
	columnID, err := argString(args, "column_id")
	if err != nil {
		return nil, err
	}
	title, err := argString(args, "title")
	if err != nil {
		return nil, err
	}
	order, _ := kanbanOptInt(args, "order")
	c := kanban.Card{
		ID:         id.Generate("card-"),
		BoardID:    boardID,
		ColumnID:   columnID,
		Title:      title,
		Ref:        kanban.WorkRef{Kind: kanbanOptString(args, "ref_kind"), ID: kanbanOptString(args, "ref_id")},
		Labels:     kanbanOptStringSlice(args, "labels"),
		GoalID:     kanbanOptString(args, "goal_id"),
		EmployeeID: kanbanOptString(args, "employee_id"),
		PlanID:     kanbanOptString(args, "plan_id"),
		SessionID:  kanbanOptString(args, "session_id"),
		TriggerRef: kanbanOptString(args, "trigger_ref"),
		Order:      order,
	}
	if err := t.board.CreateCard(ctx, c); err != nil {
		return nil, fmt.Errorf("kanban_create: %w", err)
	}
	out := kanbanOK()
	out["card"] = kanbanCardDTO(c)
	return out, nil
}

// ---- kanban_move -------------------------------------------------------------

// KanbanMoveTool moves a card to a target column. This is a HUMAN-path
// move: MoveCard enforces lease rule A (a card claimed by a live agent
// episode refuses with "lease held by <holder> ..."; use kanban_takeover
// to break the claim first) and the WIP policy from [kanban].wip_mode.
type KanbanMoveTool struct {
	tools.ToolDefaults
	board KanbanBoard
	human KanbanHumanLane
}

// NewKanbanMoveTool constructs kanban_move.
func NewKanbanMoveTool(board KanbanBoard, human KanbanHumanLane) *KanbanMoveTool {
	return &KanbanMoveTool{board: board, human: human}
}

func (t *KanbanMoveTool) Name() string     { return "kanban_move" }
func (t *KanbanMoveTool) Category() string { return "kanban" }
func (t *KanbanMoveTool) Description() string {
	return "Move a kanban card to a target column (human path: refuses on a live agent lease — use kanban_takeover to break it first). Optional order applies after the move."
}
func (t *KanbanMoveTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"card_id":        {Type: "string", Description: "Card to move."},
			"column_id":      {Type: "string", Description: "Target column id."},
			"from_column_id": {Type: "string", Description: "Optional expected source column (stale-move guard)."},
			"order":          {Type: "integer", Description: "Optional ordering position applied in the target column after the move."},
			"actor":          {Type: "string", Description: "Audit actor (default: agent)."},
		},
		Required: []string{"card_id", "column_id"},
	}
}

func (t *KanbanMoveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.human == nil {
		return nil, errKanbanHumanNotWired
	}
	cardID, err := argString(args, "card_id")
	if err != nil {
		return nil, err
	}
	target, err := argString(args, "column_id")
	if err != nil {
		return nil, err
	}
	from := kanbanOptString(args, "from_column_id")
	actor := kanbanOptString(args, "actor")
	if actor == "" {
		actor = kanbanDefaultActor
	}
	if err := t.human.MoveCard(ctx, cardID, from, target, actor); err != nil {
		return nil, fmt.Errorf("kanban_move: %w", err)
	}
	if order, ok := kanbanOptInt(args, "order"); ok {
		if err := t.setCardOrder(ctx, cardID, order); err != nil {
			return nil, err
		}
	}
	return kanbanOK(), nil
}

// setCardOrder applies an explicit ordering hint post-move via the store
// read-modify-write — the same gesture kanban.card.move performs (the
// human lane has no order verb). GetCard failure propagates: a card
// deleted mid-move is a stale write the caller must see, not silence.
func (t *KanbanMoveTool) setCardOrder(ctx context.Context, cardID string, order int) error {
	if t.board == nil {
		return errKanbanBoardNotWired
	}
	card, err := t.board.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("kanban_move order: %w", err)
	}
	if card.Order == order {
		return nil
	}
	card.Order = order
	if err := t.board.UpdateCard(ctx, card); err != nil {
		return fmt.Errorf("kanban_move order: %w", err)
	}
	return nil
}

// ---- kanban_takeover ---------------------------------------------------------

// KanbanTakeoverTool breaks the agent lease on a card (audit-before-effect,
// cancel episode, release token — HumanCommands owns the order, master.md
// C10). With to_column_id it composes the full takeover-then-move gesture;
// without it, takeover stays legal standalone (move separately).
type KanbanTakeoverTool struct {
	tools.ToolDefaults
	human KanbanHumanLane
}

// NewKanbanTakeoverTool constructs kanban_takeover.
func NewKanbanTakeoverTool(_ KanbanBoard, human KanbanHumanLane) *KanbanTakeoverTool {
	return &KanbanTakeoverTool{human: human}
}

func (t *KanbanTakeoverTool) Name() string     { return "kanban_takeover" }
func (t *KanbanTakeoverTool) Category() string { return "kanban" }
func (t *KanbanTakeoverTool) Description() string {
	return "Take over a kanban card from its agent lease: audits, cancels the episode, releases the claim. Optional to_column_id also moves the card (the move skips the just-broken lease re-check)."
}
func (t *KanbanTakeoverTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"card_id":      {Type: "string", Description: "Card to take over."},
			"to_column_id": {Type: "string", Description: "Optional target column: takeover THEN move."},
			"actor":        {Type: "string", Description: "Audit actor (default: agent)."},
		},
		Required: []string{"card_id"},
	}
}

func (t *KanbanTakeoverTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.human == nil {
		return nil, errKanbanHumanNotWired
	}
	cardID, err := argString(args, "card_id")
	if err != nil {
		return nil, err
	}
	actor := kanbanOptString(args, "actor")
	if actor == "" {
		actor = kanbanDefaultActor
	}
	out := kanbanOK()
	if target := kanbanOptString(args, "to_column_id"); target != "" {
		if err := t.human.TakeoverMove(ctx, cardID, "", target, actor); err != nil {
			return nil, fmt.Errorf("kanban_takeover: %w", err)
		}
		// The lease-bypass flag the CLI renderMutation surfaces, same key
		// as the RPC takeover reply.
		out["lease_blocked"] = true
	} else {
		if err := t.human.Takeover(ctx, cardID, actor); err != nil {
			return nil, fmt.Errorf("kanban_takeover: %w", err)
		}
	}
	return out, nil
}

// ---- kanban_assign -------------------------------------------------------------

// KanbanAssignTool records the owning employee on a card. employee_id ""
// un-assigns (HumanCommands.Assign contract), so it is accepted, not
// required.
type KanbanAssignTool struct {
	tools.ToolDefaults
	human KanbanHumanLane
}

// NewKanbanAssignTool constructs kanban_assign.
func NewKanbanAssignTool(_ KanbanBoard, human KanbanHumanLane) *KanbanAssignTool {
	return &KanbanAssignTool{human: human}
}

func (t *KanbanAssignTool) Name() string     { return "kanban_assign" }
func (t *KanbanAssignTool) Category() string { return "kanban" }
func (t *KanbanAssignTool) Description() string {
	return "Assign a kanban card to an employee (employee_id \"\" un-assigns). An assignment is not a column transition."
}
func (t *KanbanAssignTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"card_id":     {Type: "string", Description: "Card to assign."},
			"employee_id": {Type: "string", Description: "Employee id; empty string un-assigns."},
		},
		Required: []string{"card_id", "employee_id"},
	}
}

func (t *KanbanAssignTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.human == nil {
		return nil, errKanbanHumanNotWired
	}
	cardID, err := argString(args, "card_id")
	if err != nil {
		return nil, err
	}
	employeeID, ok := args["employee_id"].(string)
	if !ok {
		return nil, errors.New("employee_id is required")
	}
	if err := t.human.Assign(ctx, cardID, employeeID); err != nil {
		return nil, fmt.Errorf("kanban_assign: %w", err)
	}
	return kanbanOK(), nil
}

// ---- kanban_comment ------------------------------------------------------------

// KanbanCommentTool appends a kind="comment" row to the card timeline and
// echoes the stored event (same round-trip posture as kanban.card.comment).
type KanbanCommentTool struct {
	tools.ToolDefaults
	human KanbanHumanLane
}

// NewKanbanCommentTool constructs kanban_comment.
func NewKanbanCommentTool(_ KanbanBoard, human KanbanHumanLane) *KanbanCommentTool {
	return &KanbanCommentTool{human: human}
}

func (t *KanbanCommentTool) Name() string     { return "kanban_comment" }
func (t *KanbanCommentTool) Category() string { return "kanban" }
func (t *KanbanCommentTool) Description() string {
	return "Add a comment to a kanban card's timeline (appends one event row, echoes it back)."
}
func (t *KanbanCommentTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"card_id": {Type: "string", Description: "Card to comment on."},
			"body":    {Type: "string", Description: "Comment text."},
			"actor":   {Type: "string", Description: "Audit actor (default: agent)."},
		},
		Required: []string{"card_id", "body"},
	}
}

func (t *KanbanCommentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.human == nil {
		return nil, errKanbanHumanNotWired
	}
	cardID, err := argString(args, "card_id")
	if err != nil {
		return nil, err
	}
	body, err := argString(args, "body")
	if err != nil {
		return nil, err
	}
	actor := kanbanOptString(args, "actor")
	if actor == "" {
		actor = kanbanDefaultActor
	}
	ev, err := t.human.Comment(ctx, cardID, actor, body)
	if err != nil {
		return nil, fmt.Errorf("kanban_comment: %w", err)
	}
	out := kanbanOK()
	out["event"] = kanbanCardEventDTO(ev)
	return out, nil
}
