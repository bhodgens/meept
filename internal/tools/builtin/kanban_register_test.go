package builtin

// kanban_register_test.go — gate tests for kanban plan leaf 06/01
// (mcp-tool-provider). Drives the production path exactly like
// github_register_test.go: SetKanbanProvider -> toolprov.Names /
// toolprov.RegisterAll -> registry membership, plus per-tool Execute
// delegation against fakes.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// expectedKanbanToolNames is the frozen leaf-06/01 contract set.
// kanban_claim is EXCLUDED by master.md decision 1 (claims stay on the
// internal queue/task machinery; the board refuses human moves on claimed
// cards and offers kanban_takeover instead) — do not add it here.
var expectedKanbanToolNames = []string{
	"kanban_list",
	"kanban_show",
	"kanban_create",
	"kanban_move",
	"kanban_takeover",
	"kanban_assign",
	"kanban_comment",
}

// ---- fakes ---------------------------------------------------------------

type fakeBoard struct {
	boards     []kanban.Board
	columns    map[string][]kanban.Column
	cards      map[string][]kanban.Card
	cardByID   map[string]kanban.Card
	created    []kanban.Card
	updated    []kanban.Card
	listCards  []string // column ids passed to ListCards
	columnList []string // board ids passed to ListColumns
	err        error
}

func newFakeBoard() *fakeBoard {
	return &fakeBoard{
		columns:  map[string][]kanban.Column{},
		cards:    map[string][]kanban.Card{},
		cardByID: map[string]kanban.Card{},
	}
}

func (f *fakeBoard) ListBoards(context.Context) ([]kanban.Board, error) {
	return f.boards, f.err
}

func (f *fakeBoard) ListColumns(_ context.Context, boardID string) ([]kanban.Column, error) {
	f.columnList = append(f.columnList, boardID)
	return f.columns[boardID], f.err
}

func (f *fakeBoard) ListCards(_ context.Context, columnID string) ([]kanban.Card, error) {
	f.listCards = append(f.listCards, columnID)
	return f.cards[columnID], f.err
}

func (f *fakeBoard) GetCard(_ context.Context, cardID string) (kanban.Card, error) {
	if f.err != nil {
		return kanban.Card{}, f.err
	}
	c, ok := f.cardByID[cardID]
	if !ok {
		return kanban.Card{}, fmt.Errorf("unknown card: %s", cardID)
	}
	return c, nil
}

func (f *fakeBoard) CreateCard(_ context.Context, c kanban.Card) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, c)
	return nil
}

func (f *fakeBoard) UpdateCard(_ context.Context, c kanban.Card) error {
	if f.err != nil {
		return f.err
	}
	f.updated = append(f.updated, c)
	return nil
}

type fakeHuman struct {
	moves     []moveCall
	takeovers []string
	tkMoves   []moveCall
	assigns   []assignCall
	comments  []commentCall
	timelines []string
	err       error
}

type moveCall struct{ cardID, from, to, actor string }
type assignCall struct {
	cardID, employeeID string
}
type commentCall struct {
	cardID, actor, body string
}

func (f *fakeHuman) MoveCard(_ context.Context, cardID, fromCol, toCol, actor string) error {
	f.moves = append(f.moves, moveCall{cardID, fromCol, toCol, actor})
	return f.err
}

func (f *fakeHuman) Takeover(_ context.Context, cardID, actor string) error {
	f.takeovers = append(f.takeovers, cardID+":"+actor)
	return f.err
}

func (f *fakeHuman) TakeoverMove(_ context.Context, cardID, fromCol, toCol, actor string) error {
	f.tkMoves = append(f.tkMoves, moveCall{cardID, fromCol, toCol, actor})
	return f.err
}

func (f *fakeHuman) Assign(_ context.Context, cardID, employeeID string) error {
	f.assigns = append(f.assigns, assignCall{cardID: cardID, employeeID: employeeID})
	return f.err
}

func (f *fakeHuman) Comment(_ context.Context, cardID, actor, text string) (kanban.CardEvent, error) {
	f.comments = append(f.comments, commentCall{cardID, actor, text})
	if f.err != nil {
		return kanban.CardEvent{}, f.err
	}
	return kanban.CardEvent{
		ID: "ev-1", CardID: cardID, Kind: "comment", Actor: actor, Body: text,
		CreatedAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (f *fakeHuman) Timeline(_ context.Context, cardID string) ([]kanban.CardEvent, error) {
	f.timelines = append(f.timelines, cardID)
	if f.err != nil {
		return nil, f.err
	}
	return []kanban.CardEvent{{
		ID: "ev-1", CardID: cardID, Kind: "move", Actor: "agent", Body: "moved",
		CreatedAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
	}}, nil
}

// compile-time pin the fakes satisfy the seams.
var (
	_ KanbanBoard     = (*fakeBoard)(nil)
	_ KanbanHumanLane = (*fakeHuman)(nil)
)

// ---- helpers ---------------------------------------------------------------

func bindKanbanProviderForTest(t *testing.T, board KanbanBoard, human KanbanHumanLane, enabled bool) {
	t.Helper()
	SetKanbanProvider(board, human, enabled)
	t.Cleanup(ResetKanbanProviderForTest)
}

func toolNamesOf(ts []tools.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, tl := range ts {
		out = append(out, tl.Name())
	}
	slices.Sort(out)
	return out
}

// ---- registration / visibility --------------------------------------------

// TestKanbanProviderRegistration walks the production seam: bound+enabled
// lands every contract tool in a registry; unbound fails closed to zero;
// bound+disabled ([kanban].enabled=false) discovers nothing.
func TestKanbanProviderRegistration(t *testing.T) {
	if !slices.Contains(toolprov.Names(), "kanban") {
		t.Fatalf("kanban provider missing from toolprov.Names(): got %v", toolprov.Names())
	}

	// ---- Enabled case: bound provider registers all seven tools. ----
	bindKanbanProviderForTest(t, newFakeBoard(), &fakeHuman{}, true)
	reg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	n, err := toolprov.RegisterAll(reg, []string{"kanban"})
	if err != nil {
		t.Fatalf("RegisterAll(kanban): %v", err)
	}
	if n != len(expectedKanbanToolNames) {
		t.Errorf("RegisterAll(kanban) registered %d tools, want %d", n, len(expectedKanbanToolNames))
	}
	for _, name := range expectedKanbanToolNames {
		if got := reg.Get(name); got == nil {
			t.Errorf("enabled: registry missing tool %q; registered=%v", name, reg.Names())
		}
	}

	// ---- [kanban].enabled=false: Discover returns nothing. ----
	disabledReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	bindKanbanProviderForTest(t, newFakeBoard(), &fakeHuman{}, false)
	f, ok := toolprov.Factory("kanban")
	if !ok {
		t.Fatal("kanban factory not registered")
	}
	p := f()
	if p == nil {
		t.Fatal("bound provider must not be nil")
	}
	if got := p.Discover(); len(got) != 0 {
		t.Errorf("enabled=false: Discover returned %d tools, want 0", len(got))
	}
	// RegisterAll over an empty Discover registers nothing (a nil provider
	// would error; the disabled provider is intentionally non-nil so the
	// seam walk stays quiet).
	n, err = toolprov.RegisterAll(disabledReg, []string{"kanban"})
	if err != nil || n != 0 {
		t.Errorf("enabled=false RegisterAll: n=%d err=%v, want 0 nil", n, err)
	}

	// ---- Unbound case: factory returns nil, RegisterAll fails closed. ----
	ResetKanbanProviderForTest()
	if p := f(); p != nil {
		t.Error("unbound kanban factory must return nil Provider")
	}
	unboundReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	if _, err := toolprov.RegisterAll(unboundReg, []string{"kanban"}); err == nil {
		t.Error("RegisterAll on unbound provider: want error, got nil")
	}
	if len(unboundReg.Names()) != 0 {
		t.Errorf("unbound: registry must stay empty, got %v", unboundReg.Names())
	}

	// ---- Typed-nil bindings leave the provider unbound (fail closed). ----
	bindKanbanProviderForTest(t, (*fakeBoard)(nil), &fakeHuman{}, true)
	if p := f(); p != nil {
		t.Error("boxed-nil board binding must fail closed (unbound)")
	}
	bindKanbanProviderForTest(t, newFakeBoard(), (*fakeHuman)(nil), true)
	if p := f(); p != nil {
		t.Error("boxed-nil human binding must fail closed (unbound)")
	}
}

// TestKanbanProviderMirrorsFactoryList pins the two-lists-identical
// invariant (the github lane's red flag): the kanbanToolFactories closure
// and kanbanProvider.Discover construct the same names, in the same order,
// with the same constructor arity.
func TestKanbanProviderMirrorsFactoryList(t *testing.T) {
	if kanbanToolFactories == nil {
		t.Fatal("kanbanToolFactories seam not populated by init()")
	}
	factoryTools := kanbanToolFactories(newFakeBoard(), &fakeHuman{})
	provider := &kanbanProvider{board: newFakeBoard(), human: &fakeHuman{}, enabled: true}
	discoverTools := provider.Discover()

	if len(factoryTools) != len(discoverTools) {
		t.Fatalf("mirror drift: factories construct %d tools, Discover %d",
			len(factoryTools), len(discoverTools))
	}
	for i := range factoryTools {
		if factoryTools[i].Name() != discoverTools[i].Name() {
			t.Errorf("mirror drift at %d: factories=%s Discover=%s",
				i, factoryTools[i].Name(), discoverTools[i].Name())
		}
		if a, b := reflect.TypeOf(factoryTools[i]), reflect.TypeOf(discoverTools[i]); a != b {
			t.Errorf("mirror drift at %s: concrete types %v vs %v", factoryTools[i].Name(), a, b)
		}
	}

	// Names must equal the frozen contract list exactly (order-insensitive).
	got := toolNamesOf(discoverTools)
	want := append([]string(nil), expectedKanbanToolNames...)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Discover names %v, want contract set %v", got, want)
	}

	// kanban_claim must NEVER appear (master.md decision 1).
	for _, tl := range append(append([]tools.Tool(nil), factoryTools...), discoverTools...) {
		if strings.Contains(tl.Name(), "claim") {
			t.Errorf("kanban_claim is excluded by master decision 1, found %q", tl.Name())
		}
	}
}

// TestKanbanProviderMetadata checks provider identity, risk ceiling, and
// the nil broker posture.
func TestKanbanProviderMetadata(t *testing.T) {
	p := &kanbanProvider{enabled: true}
	if p.Name() != "kanban" {
		t.Errorf("Name=%q, want kanban", p.Name())
	}
	if p.PermissionClass() != toolprov.RiskMutating {
		t.Errorf("PermissionClass=%q, want mutating", p.PermissionClass())
	}
	if p.Broker() != nil {
		t.Error("Broker must be nil (no secret material)")
	}
}

// ---- nil-deps error shape ---------------------------------------------------

func TestKanbanToolsNilDepsErrorShape(t *testing.T) {
	cases := []struct {
		name string
		tool tools.Tool
		args map[string]any
		want string
	}{
		{"kanban_list", NewKanbanListTool(nil, nil), map[string]any{}, "board store not wired"},
		{"kanban_show", NewKanbanShowTool(nil, nil), map[string]any{"card_id": "c1"}, "not wired"},
		{"kanban_create", NewKanbanCreateTool(nil, nil), map[string]any{"board_id": "b", "column_id": "c", "title": "t"}, "board store not wired"},
		{"kanban_move", NewKanbanMoveTool(nil, nil), map[string]any{"card_id": "c1", "column_id": "c2"}, "human lane not wired"},
		{"kanban_takeover", NewKanbanTakeoverTool(nil, nil), map[string]any{"card_id": "c1"}, "human lane not wired"},
		{"kanban_assign", NewKanbanAssignTool(nil, nil), map[string]any{"card_id": "c1", "employee_id": "e"}, "human lane not wired"},
		{"kanban_comment", NewKanbanCommentTool(nil, nil), map[string]any{"card_id": "c1", "body": "hi"}, "human lane not wired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.tool.Execute(context.Background(), tc.args)
			if err == nil {
				t.Fatal("nil deps: want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// ---- per-tool Execute delegation -------------------------------------------

func TestKanbanListDelegation(t *testing.T) {
	t.Run("no args lists boards", func(t *testing.T) {
		b := newFakeBoard()
		b.boards = []kanban.Board{{ID: "b1", Name: "main"}}
		res, err := NewKanbanListTool(b, nil).Execute(context.Background(), map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		out := res.(map[string]any)
		if out["ok"] != true {
			t.Errorf("envelope %v", out)
		}
		boards := out["boards"].([]map[string]any)[0]
		if boards["id"] != "b1" || boards["name"] != "main" {
			t.Errorf("board DTO %v", boards)
		}
	})
	t.Run("column_id lists cards", func(t *testing.T) {
		b := newFakeBoard()
		b.cards["c1"] = []kanban.Card{{ID: "k1", ColumnID: "c1", Title: "t"}}
		res, err := NewKanbanListTool(b, nil).Execute(context.Background(), map[string]any{"column_id": "c1"})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(b.listCards, []string{"c1"}) {
			t.Errorf("ListCards got %v", b.listCards)
		}
		if got := res.(map[string]any)["cards"].([]map[string]any)[0]["id"]; got != "k1" {
			t.Errorf("card DTO id=%v", got)
		}
	})
	t.Run("board_id aggregates columns then cards", func(t *testing.T) {
		b := newFakeBoard()
		b.columns["b1"] = []kanban.Column{{ID: "col1", BoardID: "b1"}, {ID: "col2", BoardID: "b1"}}
		b.cards["col1"] = []kanban.Card{{ID: "k1", ColumnID: "col1"}}
		res, err := NewKanbanListTool(b, nil).Execute(context.Background(), map[string]any{"board_id": "b1"})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(b.columnList, []string{"b1"}) {
			t.Errorf("ListColumns got %v", b.columnList)
		}
		if !slices.Equal(b.listCards, []string{"col1", "col2"}) {
			t.Errorf("board aggregation must walk columns in order, got %v", b.listCards)
		}
		out := res.(map[string]any)
		if len(out["columns"].([]map[string]any)) != 2 {
			t.Error("columns missing")
		}
	})
}

func TestKanbanShowDelegation(t *testing.T) {
	b := newFakeBoard()
	b.cardByID["k1"] = kanban.Card{ID: "k1", BoardID: "b1", ColumnID: "c1", Title: "t", GoalID: "g1"}
	h := &fakeHuman{}
	res, err := NewKanbanShowTool(b, h).Execute(context.Background(), map[string]any{"card_id": "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.timelines, []string{"k1"}) {
		t.Errorf("timeline call %v", h.timelines)
	}
	out := res.(map[string]any)
	card := out["card"].(map[string]any)
	if card["goal_id"] != "g1" {
		t.Errorf("provenance DTO %v", card)
	}
	events := out["events"].([]map[string]any)[0]
	if events["kind"] != "move" || events["created_at"] != "2026-11-15T12:00:00Z" {
		t.Errorf("event DTO %v", events)
	}
	// Missing required arg.
	if _, err := NewKanbanShowTool(b, h).Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("card_id required: want error")
	}
}

func TestKanbanCreateDelegation(t *testing.T) {
	b := newFakeBoard()
	_, err := NewKanbanCreateTool(b, nil).Execute(context.Background(), map[string]any{
		"board_id": "b1", "column_id": "c1", "title": "new card",
		"labels": []any{"urgent", "ops"}, "employee_id": "e7", "order": float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.created) != 1 {
		t.Fatalf("created %d", len(b.created))
	}
	c := b.created[0]
	if c.BoardID != "b1" || c.ColumnID != "c1" || c.Title != "new card" {
		t.Errorf("create DTO %+v", c)
	}
	if !slices.Equal(c.Labels, []string{"urgent", "ops"}) {
		t.Errorf("labels %v", c.Labels)
	}
	if c.EmployeeID != "e7" || c.Order != 3 {
		t.Errorf("employee/order %q/%d", c.EmployeeID, c.Order)
	}
	if !strings.HasPrefix(c.ID, "card-") {
		t.Errorf("id mint %q", c.ID)
	}
	// Missing title -> param error.
	if _, err := NewKanbanCreateTool(b, nil).Execute(context.Background(), map[string]any{
		"board_id": "b1", "column_id": "c1",
	}); err == nil {
		t.Error("title required: want error")
	}
}

func TestKanbanMoveDelegation(t *testing.T) {
	b := newFakeBoard()
	b.cardByID["k1"] = kanban.Card{ID: "k1", ColumnID: "from", Order: 0}
	h := &fakeHuman{}
	_, err := NewKanbanMoveTool(b, h).Execute(context.Background(), map[string]any{
		"card_id": "k1", "column_id": "target", "actor": "human-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.moves) != 1 {
		t.Fatalf("moves %+v", h.moves)
	}
	if h.moves[0] != (moveCall{cardID: "k1", from: "", to: "target", actor: "human-1"}) {
		t.Errorf("move call %+v, want target column + actor passthrough", h.moves[0])
	}

	// Default actor when omitted.
	h2 := &fakeHuman{}
	if _, err := NewKanbanMoveTool(b, h2).Execute(context.Background(), map[string]any{
		"card_id": "k1", "column_id": "target",
	}); err != nil {
		t.Fatal(err)
	}
	if h2.moves[0].actor != "agent" {
		t.Errorf("default actor %+v", h2.moves[0])
	}

	// order applies after the move via store UpdateCard.
	h3 := &fakeHuman{}
	if _, err := NewKanbanMoveTool(b, h3).Execute(context.Background(), map[string]any{
		"card_id": "k1", "column_id": "target", "order": 5,
	}); err != nil {
		t.Fatal(err)
	}
	if len(b.updated) != 1 || b.updated[len(b.updated)-1].Order != 5 {
		t.Errorf("order write %+v", b.updated)
	}

	// Lease refusal surfaces (error text passthrough, not swallowed).
	hErr := &fakeHuman{err: errors.New("lease held by ep-9 since 2026-11-15T00:00:00Z")}
	_, err = NewKanbanMoveTool(b, hErr).Execute(context.Background(), map[string]any{
		"card_id": "k1", "column_id": "target",
	})
	if err == nil || !strings.Contains(err.Error(), "lease held by") {
		t.Errorf("lease refusal must surface, got %v", err)
	}
}

func TestKanbanTakeoverDelegation(t *testing.T) {
	// Standalone takeover (no target): Takeover only.
	h := &fakeHuman{}
	res, err := NewKanbanTakeoverTool(nil, h).Execute(context.Background(), map[string]any{"card_id": "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.takeovers, []string{"k1:agent"}) {
		t.Errorf("takeover %+v", h.takeovers)
	}
	if len(h.tkMoves) != 0 {
		t.Errorf("standalone takeover must not move: %+v", h.tkMoves)
	}
	if _, ok := res.(map[string]any)["lease_blocked"]; ok {
		t.Error("standalone takeover reply must not carry lease_blocked")
	}

	// With target: TakeoverMove + lease_blocked:true.
	h2 := &fakeHuman{}
	res, err = NewKanbanTakeoverTool(nil, h2).Execute(context.Background(), map[string]any{
		"card_id": "k1", "to_column_id": "target", "actor": "ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h2.tkMoves) != 1 || h2.tkMoves[0] != (moveCall{cardID: "k1", from: "", to: "target", actor: "ops"}) {
		t.Errorf("takeover+move %+v", h2.tkMoves)
	}
	if len(h2.takeovers) != 0 {
		t.Errorf("composed gesture must route via TakeoverMove only: %+v", h2.takeovers)
	}
	if res.(map[string]any)["lease_blocked"] != true {
		t.Errorf("lease_blocked flag %v", res)
	}
}

func TestKanbanAssignDelegation(t *testing.T) {
	h := &fakeHuman{}
	if _, err := NewKanbanAssignTool(nil, h).Execute(context.Background(), map[string]any{
		"card_id": "k1", "employee_id": "emp-7",
	}); err != nil {
		t.Fatal(err)
	}
	if len(h.assigns) != 1 || h.assigns[0] != (assignCall{cardID: "k1", employeeID: "emp-7"}) {
		t.Errorf("assign delegation %+v", h.assigns)
	}
	// Empty employee_id un-assigns and must reach the lane.
	if _, err := NewKanbanAssignTool(nil, h).Execute(context.Background(), map[string]any{
		"card_id": "k1", "employee_id": "",
	}); err != nil {
		t.Fatal(err)
	}
	if len(h.assigns) != 2 || h.assigns[1].employeeID != "" {
		t.Errorf("un-assign %+v", h.assigns)
	}
	// Missing employee_id key entirely -> param error.
	if _, err := NewKanbanAssignTool(nil, h).Execute(context.Background(), map[string]any{"card_id": "k1"}); err == nil {
		t.Error("employee_id required: want error")
	}
}

func TestKanbanCommentDelegation(t *testing.T) {
	h := &fakeHuman{}
	res, err := NewKanbanCommentTool(nil, h).Execute(context.Background(), map[string]any{
		"card_id": "k1", "body": "progress note", "actor": "emp-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.comments) != 1 || h.comments[0] != (commentCall{cardID: "k1", actor: "emp-7", body: "progress note"}) {
		t.Errorf("comment delegation %+v", h.comments)
	}
	ev := res.(map[string]any)["event"].(map[string]any)
	if ev["body"] != "progress note" || ev["kind"] != "comment" {
		t.Errorf("event echo %v", ev)
	}
	// Missing body -> param error.
	if _, err := NewKanbanCommentTool(nil, h).Execute(context.Background(), map[string]any{"card_id": "k1"}); err == nil {
		t.Error("body required: want error")
	}
}

// TestKanbanSchemaNames pins each tool's Name() against the contract list
// and asserts snake_case + kanban_ prefix on every one.
func TestKanbanSchemaNames(t *testing.T) {
	set := []tools.Tool{
		NewKanbanListTool(nil, nil),
		NewKanbanShowTool(nil, nil),
		NewKanbanCreateTool(nil, nil),
		NewKanbanMoveTool(nil, nil),
		NewKanbanTakeoverTool(nil, nil),
		NewKanbanAssignTool(nil, nil),
		NewKanbanCommentTool(nil, nil),
	}
	if len(set) != len(expectedKanbanToolNames) {
		t.Fatalf("schema-check set has %d tools, want %d", len(set), len(expectedKanbanToolNames))
	}
	names := toolNamesOf(set)
	want := append([]string(nil), expectedKanbanToolNames...)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("tool names %v, want %v", names, want)
	}
	for _, tl := range set {
		if !strings.HasPrefix(tl.Name(), "kanban_") {
			t.Errorf("name %q must carry the kanban_ prefix", tl.Name())
		}
		if strings.ToLower(tl.Name()) != tl.Name() {
			t.Errorf("name %q must be snake_case", tl.Name())
		}
	}
}
