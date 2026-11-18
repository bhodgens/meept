package rpc

// kanban_test.go — leaf 05/01 contract pins. Two layers:
//
//  1. Unit pins: all 13 C4 methods delegate correctly, the C5 grant lane
//     (reads board.read, mutations board.mutate, takeover CONJUNCTIVE mutate
//     AND takeover — the rule kanbansvc 310701d5 pinned at REST level) is
//     enforced here too, and parameter errors carry ErrKanbanInvalidParams
//     so server.dispatch maps them to JSON-RPC -32602.
//  2. CROSS-LANE PINS: every payload below is copied verbatim from the
//     already-tested clients — cmd/meept/kanban.go RunE param maps,
//     internal/tui/kanban_view.go call sites, and
//     ui/flutter_ui/lib/features/kanban/kanban_service.dart bodies (plus
//     the gateway's query-string passthrough form, where buildParams
//     forwards map[string]string and ints arrive as decimal strings). The
//     reply-envelope tests pin every key the CLI renderers
//     (kanban_format.go getStringOr/numField), the TUI wire readers
//     (kanban_view_wire.go kanbanFieldStr), and the Dart decoders
//     (kanban_models.dart _pick) actually read.
//
// Failure transport is pinned deliberately: business refusals ride the
// Go-error path with the FROZEN C1 error text intact, because all three
// surfaces parse lease/WIP/grant refusals from the transport-error path
// (CLI kanbanError, TUI kanbanRefusalMessage, Dart mapStatusError behind
// the gateway 502).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/kanban"
)

// ---- fakes ------------------------------------------------------------------

type fakeBoardStore struct {
	boards    []kanban.Board
	columns   []kanban.Column
	cards     map[string]kanban.Card
	err       error
	lastOrder int // UpdateCard Order applied by the move-order-hint path
}

func (f *fakeBoardStore) ListBoards(context.Context) ([]kanban.Board, error) {
	return f.boards, f.err
}

func (f *fakeBoardStore) CreateBoard(_ context.Context, b kanban.Board) error {
	if f.err != nil {
		return f.err
	}
	f.boards = append(f.boards, b)
	return nil
}

func (f *fakeBoardStore) ListColumns(_ context.Context, boardID string) ([]kanban.Column, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []kanban.Column
	for _, c := range f.columns {
		if c.BoardID == boardID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeBoardStore) ListCards(_ context.Context, columnID string) ([]kanban.Card, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []kanban.Card
	for _, c := range f.cards {
		if c.ColumnID == columnID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeBoardStore) GetCard(_ context.Context, cardID string) (kanban.Card, error) {
	if f.err != nil {
		return kanban.Card{}, f.err
	}
	c, ok := f.cards[cardID]
	if !ok {
		return kanban.Card{}, kanban.ErrUnknownCard{ID: cardID}
	}
	return c, nil
}

func (f *fakeBoardStore) CreateCard(_ context.Context, c kanban.Card) error {
	if f.err != nil {
		return f.err
	}
	if f.cards == nil {
		f.cards = map[string]kanban.Card{}
	}
	f.cards[c.ID] = c
	return nil
}

func (f *fakeBoardStore) UpdateCard(_ context.Context, c kanban.Card) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.cards[c.ID]; !ok {
		return kanban.ErrUnknownCard{ID: c.ID}
	}
	f.cards[c.ID] = c
	f.lastOrder = c.Order
	return nil
}

type fakeManager struct {
	setWIPCalls int
	lastWIP     int
	lastBoard   string
	lastCol     string
	err         error
}

func (f *fakeManager) SetWIPLimit(_ context.Context, b kanban.Board, colID string, limit int) error {
	f.setWIPCalls++
	f.lastWIP = limit
	f.lastBoard = b.ID
	f.lastCol = colID
	if b.ID == "" || colID == "" {
		return errors.New("board id is required")
	}
	return f.err
}

type fakeHuman struct {
	moveCalls     int
	takeoverCalls int
	takeoverMove  int
	assignCalls   int
	commentCalls  int
	lastCardID    string
	lastActor     string
	lastFromCol   string
	lastToCol     string
	lastEmployee  string
	lastBody      string
	err           error
	events        []kanban.CardEvent
}

func (f *fakeHuman) MoveCard(_ context.Context, cardID, fromCol, toCol, actor string) error {
	f.moveCalls++
	f.lastCardID, f.lastFromCol, f.lastToCol, f.lastActor = cardID, fromCol, toCol, actor
	return f.err
}

func (f *fakeHuman) Takeover(_ context.Context, cardID, actor string) error {
	f.takeoverCalls++
	f.lastCardID, f.lastActor = cardID, actor
	return f.err
}

func (f *fakeHuman) TakeoverMove(_ context.Context, cardID, fromCol, toCol, actor string) error {
	f.takeoverMove++
	f.lastCardID, f.lastFromCol, f.lastToCol, f.lastActor = cardID, fromCol, toCol, actor
	return f.err
}

func (f *fakeHuman) Assign(_ context.Context, cardID, employeeID string) error {
	f.assignCalls++
	f.lastCardID, f.lastEmployee = cardID, employeeID
	return f.err
}

func (f *fakeHuman) Comment(_ context.Context, cardID, actor, text string) (kanban.CardEvent, error) {
	f.commentCalls++
	f.lastCardID, f.lastActor, f.lastBody = cardID, actor, text
	if f.err != nil {
		return kanban.CardEvent{}, f.err
	}
	return kanban.CardEvent{ID: "evt-1", CardID: cardID, Kind: "comment", Actor: actor, Body: text, CreatedAt: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)}, nil
}

func (f *fakeHuman) Timeline(_ context.Context, cardID string) ([]kanban.CardEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

type fakeReconciler struct {
	calls int
	stats kanban.ReconcileStats
	err   error
}

func (f *fakeReconciler) ReconcileNow(context.Context) error { f.calls++; return f.err }

func (f *fakeReconciler) ReconcileNowStats(context.Context) (kanban.ReconcileStats, error) {
	f.calls++
	return f.stats, f.err
}

type fakeGate struct {
	grants map[string]struct{} // grant set held by the test principal
	checks []string            // "principal|grant" trail
}

func (g *fakeGate) CheckGrant(principal, grant string) bool {
	g.checks = append(g.checks, principal+"|"+grant)
	_, ok := g.grants[grant]
	return ok
}

func allGrants() map[string]struct{} {
	return map[string]struct{}{
		kanban.GrantBoardRead: {}, kanban.GrantBoardMutate: {}, kanban.GrantBoardTakeover: {},
	}
}

// ---- harness ------------------------------------------------------------------

func newTestHandler(deps KanbanDeps) (*KanbanHandler, *fakeBoardStore, *fakeHuman, *fakeReconciler, *fakeManager) {
	st := &fakeBoardStore{
		boards: []kanban.Board{{ID: "board-1", Name: "team"}},
		// col-1/col-2 are the leaf fixtures; c1/c2 exist so the copied
		// client literals (CLI/TUI/Dart use "c1"/"c2"/"k1") resolve
		// against the board lookup without rewriting the payloads.
		columns: []kanban.Column{
			{ID: "col-1", BoardID: "board-1", Name: "todo", Position: 0, WIP: 2},
			{ID: "col-2", BoardID: "board-1", Name: "doing", Position: 1},
			{ID: "c1", BoardID: "board-1", Name: "one", Position: 2},
			{ID: "c2", BoardID: "board-1", Name: "two", Position: 3},
		},
		cards: map[string]kanban.Card{
			"card-1": {
				ID: "card-1", BoardID: "board-1", ColumnID: "col-1", Title: "thing",
				Ref: kanban.WorkRef{Kind: "goal", ID: "goal-9"}, Labels: []string{"now"},
				GoalID: "goal-9", Order: 3,
			},
			"k1": {ID: "k1", BoardID: "board-1", ColumnID: "c1", Title: "ship v2", Labels: []string{"urgent"}, Order: 2},
		},
	}
	hu := &fakeHuman{events: []kanban.CardEvent{{ID: "evt-1", CardID: "card-1", Kind: "comment", Actor: "a", Body: "b"}}}
	rec := &fakeReconciler{}
	mgr := &fakeManager{}
	deps.Store, deps.Human, deps.Reconciler, deps.Manager = st, hu, rec, mgr
	if deps.Authorizer == nil {
		deps.Authorizer = &fakeGate{grants: allGrants()}
	}
	return NewKanbanHandler(deps), st, hu, rec, mgr
}

// dispatchForTest routes through the method table so the test pins the
// C4 string -> handler wiring, not just the Go method.
func (h *KanbanHandler) dispatchForTest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	handlers := map[string]Handler{
		"kanban.board.list":     h.handleBoardList,
		"kanban.board.create":   h.handleBoardCreate,
		"kanban.column.list":    h.handleColumnList,
		"kanban.column.set_wip": h.handleColumnSetWIP,
		"kanban.card.list":      h.handleCardList,
		"kanban.card.get":       h.handleCardGet,
		"kanban.card.create":    h.handleCardCreate,
		"kanban.card.move":      h.handleCardMove,
		"kanban.card.takeover":  h.handleCardTakeover,
		"kanban.card.assign":    h.handleCardAssign,
		"kanban.card.comment":   h.handleCardComment,
		"kanban.card.timeline":  h.handleCardTimeline,
		"kanban.reconcile.now":  h.handleReconcileNow,
	}
	fn, ok := handlers[method]
	if !ok {
		return nil, errors.New("no handler for " + method)
	}
	return fn(ctx, params)
}

func rawJSON(t *testing.T, params any) json.RawMessage {
	t.Helper()
	if params == nil {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return b
}

// callRaw dispatches EXACT payload bytes (a raw JSON string) — the cross-lane
// pins feed it the literals copied out of the client sources.
func callRaw(t *testing.T, h *KanbanHandler, method, raw string) (map[string]any, error) {
	t.Helper()
	res, err := h.dispatchForTest(context.Background(), method, json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("%s: result type %T, want envelope map", method, res)
	}
	return m, nil
}

func call(t *testing.T, h *KanbanHandler, method string, params any) map[string]any {
	t.Helper()
	env, err := callRaw(t, h, method, string(rawJSON(t, params)))
	if err != nil {
		t.Fatalf("%s: unexpected Go error: %v", method, err)
	}
	return env
}

func callErr(t *testing.T, h *KanbanHandler, method string, raw string) error {
	t.Helper()
	_, err := callRaw(t, h, method, raw)
	if err == nil {
		t.Fatalf("%s: expected error, got success", method)
	}
	return err
}

func mustOK(t *testing.T, env map[string]any, method string) {
	t.Helper()
	if env["ok"] != true {
		t.Fatalf("%s: want ok=true, got %v (%v)", method, env["ok"], env["error"])
	}
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal error>"
	}
	return string(b)
}

// ---- registration ---------------------------------------------------------------

func TestKanbanRegistration_All13C4Methods(t *testing.T) {
	h, _, _, _, _ := newTestHandler(KanbanDeps{})
	srv := New(&Config{SocketPath: "/tmp/unused-kanban-test.sock"}, nil, nil)
	h.RegisterKanbanMethods(srv)
	for _, m := range []string{
		"kanban.board.list", "kanban.board.create", "kanban.column.list",
		"kanban.column.set_wip", "kanban.card.list", "kanban.card.get",
		"kanban.card.create", "kanban.card.move", "kanban.card.takeover",
		"kanban.card.assign", "kanban.card.comment", "kanban.card.timeline",
		"kanban.reconcile.now",
	} {
		// Missing board_id etc. is an ErrKanbanInvalidParams Go error
		// (server.dispatch maps it to -32602); either way, anything that is
		// NOT "method not found" proves registration.
		if _, err := srv.CallMethod(context.Background(), m, nil); err != nil && strings.Contains(err.Error(), "method not found") {
			t.Fatalf("method %s not registered: %v", m, err)
		}
	}
	var nilServer *Server
	(&KanbanHandler{}).RegisterKanbanMethods(nilServer) // nil-safe, no panic
}

// ---- CROSS-LANE PARAM PINS ----------------------------------------------------
//
// Every raw payload below is copied VERBATIM from the client source named in
// the subtest. A rename on either side must break this test.

func TestKanban_CrossLaneParamPins(t *testing.T) {
	// Exact literals from the clients. CLI = cmd/meept/kanban.go RunE maps;
	// Dart = kanban_service.dart bodies; TUI = kanban_view.go callRPC maps;
	// gateway-query = the map[string]string buildParams forwards (ints as
	// decimal strings).
	pins := []struct {
		name    string
		method  string
		payload string
		check   func(t *testing.T, env map[string]any, hu *fakeHuman, mgr *fakeManager, st *fakeBoardStore)
	}{
		// — column.set_wip —
		{"cli wip (no --board)", "kanban.column.set_wip",
			`{"column_id":"c1","wip":3}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, mgr *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "set_wip")
				if mgr.lastCol != "c1" || mgr.lastWIP != 3 || mgr.lastBoard != "board-1" {
					t.Errorf("delegation = board %q col %q wip %d, want board-1/c1/3 (board resolved from column)", mgr.lastBoard, mgr.lastCol, mgr.lastWIP)
				}
			}},
		{"cli wip with --board", "kanban.column.set_wip",
			`{"column_id":"col-1","wip":5,"board_id":"board-1"}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, mgr *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "set_wip")
				if mgr.lastBoard != "board-1" || mgr.lastCol != "col-1" || mgr.lastWIP != 5 {
					t.Errorf("delegation = %q/%q/%d", mgr.lastBoard, mgr.lastCol, mgr.lastWIP)
				}
			}},
		{"dart setColumnWip", "kanban.column.set_wip",
			`{"column_id":"c1","wip":2}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, mgr *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "set_wip")
				if mgr.lastCol != "c1" || mgr.lastWIP != 2 {
					t.Errorf("delegation = col %q wip %d", mgr.lastCol, mgr.lastWIP)
				}
			}},
		{"gateway query passthrough (wip as string)", "kanban.column.set_wip",
			`{"column_id":"c1","wip":"4"}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, mgr *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "set_wip")
				if mgr.lastWIP != 4 {
					t.Errorf("string wip must decode to 4, got %d", mgr.lastWIP)
				}
			}},
		{"legacy limit alias still accepted", "kanban.column.set_wip",
			`{"board_id":"board-1","column_id":"col-1","limit":6}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, mgr *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "set_wip")
				if mgr.lastWIP != 6 {
					t.Errorf("legacy limit = %d, want 6", mgr.lastWIP)
				}
			}},

		// — card.move —
		{"cli move", "kanban.card.move",
			`{"card_id":"k1","column_id":"c2"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.moveCalls != 1 || hu.lastCardID != "k1" || hu.lastToCol != "c2" {
					t.Errorf("MoveCard args: %d %q -> %q", hu.moveCalls, hu.lastCardID, hu.lastToCol)
				}
			}},
		{"cli move --order", "kanban.card.move",
			`{"card_id":"card-1","column_id":"col-1","order":7}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, st *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.moveCalls != 1 {
					t.Fatalf("same-column move must still run (ordering gesture), calls=%d", hu.moveCalls)
				}
				if st.lastOrder != 7 || st.cards["card-1"].Order != 7 {
					t.Errorf("explicit order hint = %d, want 7 applied via UpdateCard", st.lastOrder)
				}
			}},
		{"dart moveCard", "kanban.card.move",
			`{"card_id":"k1","column_id":"c2","order":5,"actor":"ana"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, st *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.lastToCol != "c2" || hu.lastActor != "ana" || st.lastOrder != 5 {
					t.Errorf("dart move: to=%q actor=%q order=%d", hu.lastToCol, hu.lastActor, st.lastOrder)
				}
			}},
		{"tui move", "kanban.card.move",
			`{"card_id":"card-one","column_id":"c2","actor":"tui"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.lastCardID != "card-one" || hu.lastToCol != "c2" || hu.lastActor != "tui" {
					t.Errorf("tui move: %q -> %q by %q", hu.lastCardID, hu.lastToCol, hu.lastActor)
				}
			}},
		{"legacy to_column alias accepted", "kanban.card.move",
			`{"card_id":"k1","from_column":"col-1","to_column":"col-2","actor":"maria"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.lastFromCol != "col-1" || hu.lastToCol != "col-2" {
					t.Errorf("legacy move from/to = %q/%q", hu.lastFromCol, hu.lastToCol)
				}
			}},
		{"column_id wins over to_column", "kanban.card.move",
			`{"card_id":"k1","column_id":"primary","to_column":"legacy"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "move")
				if hu.lastToCol != "primary" {
					t.Errorf("documented precedence broken: to=%q want primary", hu.lastToCol)
				}
			}},

		// — card.takeover —
		{"cli takeover bare", "kanban.card.takeover",
			`{"card_id":"k1"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "takeover bare")
				if hu.takeoverCalls != 1 || hu.takeoverMove != 0 {
					t.Errorf("bare card_id must route to Takeover: calls=%d moves=%d", hu.takeoverCalls, hu.takeoverMove)
				}
			}},
		{"cli takeover --column", "kanban.card.takeover",
			`{"card_id":"k1","column_id":"done"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "takeover")
				if hu.takeoverMove != 1 || hu.lastToCol != "done" {
					t.Errorf("takeoverMove=%d to=%q", hu.takeoverMove, hu.lastToCol)
				}
				if env["lease_blocked"] != true {
					t.Errorf("routed takeover must stamp lease_blocked for the CLI renderer: %s", jsonOf(env))
				}
			}},
		{"dart takeoverCard to_column_id", "kanban.card.takeover",
			`{"card_id":"k1","to_column_id":"c9","actor":"ana"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "takeover")
				if hu.takeoverMove != 1 || hu.lastToCol != "c9" || hu.lastActor != "ana" {
					t.Errorf("dart takeover: moves=%d to=%q actor=%q", hu.takeoverMove, hu.lastToCol, hu.lastActor)
				}
			}},
		{"tui takeover to_column_id", "kanban.card.takeover",
			`{"card_id":"card-one","actor":"tui","to_column_id":"c2"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "takeover")
				if hu.takeoverMove != 1 || hu.lastToCol != "c2" {
					t.Errorf("tui takeover: moves=%d to=%q", hu.takeoverMove, hu.lastToCol)
				}
			}},
		{"cli column_id beats dart to_column_id", "kanban.card.takeover",
			`{"card_id":"k1","column_id":"primary","to_column_id":"secondary"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "takeover")
				if hu.lastToCol != "primary" {
					t.Errorf("CLI-primary precedence broken: to=%q want primary", hu.lastToCol)
				}
			}},

		// — card.comment —
		{"cli comment", "kanban.card.comment",
			`{"card_id":"k1","body":"blocked on review"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "comment")
				if hu.commentCalls != 1 || hu.lastCardID != "k1" || hu.lastBody != "blocked on review" {
					t.Errorf("Comment args: %d %q %q", hu.commentCalls, hu.lastCardID, hu.lastBody)
				}
			}},
		{"dart commentCard", "kanban.card.comment",
			`{"card_id":"k1","body":"nice work"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "comment")
				if hu.lastBody != "nice work" {
					t.Errorf("dart comment body = %q", hu.lastBody)
				}
				if ev, ok := env["event"].(map[string]any); !ok || ev["body"] != "nice work" {
					t.Errorf("comment echo = %s", jsonOf(env))
				}
			}},
		{"legacy text alias accepted", "kanban.card.comment",
			`{"card_id":"card-1","text":"hello","actor":"maria"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "comment")
				if hu.lastBody != "hello" || hu.lastActor != "maria" {
					t.Errorf("legacy comment: body=%q actor=%q", hu.lastBody, hu.lastActor)
				}
			}},

		// — card.list —
		{"cli board cards", "kanban.card.list",
			`{"column_id":"col-1"}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "card.list")
				if !strings.Contains(jsonOf(env["cards"]), `"card-1"`) {
					t.Errorf("cards = %s", jsonOf(env["cards"]))
				}
			}},
		{"cli board cards --limit", "kanban.card.list",
			`{"column_id":"col-1","limit":1}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "card.list")
			}},
		{"gateway query passthrough (limit as string)", "kanban.card.list",
			`{"column_id":"col-1","limit":"0"}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "card.list")
				if list, _ := env["cards"].([]map[string]any); len(list) != 0 {
					t.Errorf("limit 0 must clamp to empty, got %s", jsonOf(env["cards"]))
				}
			}},
		{"dart board-only list (tui fetchCards)", "kanban.card.list",
			`{"board_id":"board-1"}`,
			func(t *testing.T, env map[string]any, _ *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "card.list board-only")
				if !strings.Contains(jsonOf(env["cards"]), `"card-1"`) {
					t.Errorf("board-only aggregate = %s", jsonOf(env["cards"]))
				}
			}},

		// — reads / creates / assign / reconcile —
		{"cli show --card", "kanban.card.get", `{"card_id":"card-1"}`, nil},
		{"cli timeline", "kanban.card.timeline", `{"card_id":"card-1"}`, nil},
		{"cli column list", "kanban.column.list", `{"board_id":"board-1"}`, nil},
		{"cli board create", "kanban.board.create", `{"name":"ops"}`, nil},
		{"cli board create --id", "kanban.board.create", `{"name":"ops","id":"b7"}`, nil},
		{"cli create card", "kanban.card.create",
			`{"board_id":"board-1","column_id":"col-1","title":"new thing","labels":["urgent"]}`, nil},
		{"dart createCard w/ order", "kanban.card.create",
			`{"board_id":"b","column_id":"c","title":"t","labels":["x"],"order":4}`, nil},
		{"cli assign", "kanban.card.assign", `{"card_id":"k1","employee_id":"coder"}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "assign")
				if hu.lastCardID != "k1" || hu.lastEmployee != "coder" {
					t.Errorf("Assign args: %q %q", hu.lastCardID, hu.lastEmployee)
				}
			}},
		{"dart assignCard", "kanban.card.assign", `{"card_id":"card-1","employee_id":""}`,
			func(t *testing.T, env map[string]any, hu *fakeHuman, _ *fakeManager, _ *fakeBoardStore) {
				mustOK(t, env, "assign")
				if hu.lastEmployee != "" {
					t.Errorf("empty employee_id must un-assign, got %q", hu.lastEmployee)
				}
			}},
		{"cli reconcile", "kanban.reconcile.now", `{}`, nil},
		{"dart reconcile (empty body -> null params)", "kanban.reconcile.now", `null`, nil},
	}
	for _, pin := range pins {
		t.Run(pin.name, func(t *testing.T) {
			h, st, hu, _, mgr := newTestHandler(KanbanDeps{})
			env, err := callRaw(t, h, pin.method, pin.payload)
			if err != nil {
				t.Fatalf("%s rejected its client's own payload: %v", pin.method, err)
			}
			if pin.check != nil {
				pin.check(t, env, hu, mgr, st)
			} else {
				mustOK(t, env, pin.method)
			}
		})
	}
}

// ---- happy paths: delegation + envelope echo ----------------------------------

func TestKanban_HappyPaths_Delegate(t *testing.T) {
	h, st, _, rec, mgr := newTestHandler(KanbanDeps{})

	env := call(t, h, "kanban.board.list", nil)
	mustOK(t, env, "board.list")
	if got := jsonOf(env["boards"]); !strings.Contains(got, `"board-1"`) {
		t.Errorf("boards payload = %s", got)
	}

	env = call(t, h, "kanban.board.create", map[string]any{"name": "second"})
	mustOK(t, env, "board.create")
	boardID := env["board"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(boardID, "board-") {
		t.Errorf("minted board id = %q, want board- prefix", boardID)
	}

	env = call(t, h, "kanban.column.list", map[string]any{"board_id": "board-1"})
	mustOK(t, env, "column.list")
	if !strings.Contains(jsonOf(env["columns"]), `"col-1"`) {
		t.Errorf("columns payload missing col-1: %s", jsonOf(env["columns"]))
	}

	call(t, h, "kanban.column.set_wip", map[string]any{"board_id": "board-1", "column_id": "col-1", "wip": 5})
	if mgr.setWIPCalls != 1 || mgr.lastWIP != 5 {
		t.Errorf("manager saw %d calls, last wip %d", mgr.setWIPCalls, mgr.lastWIP)
	}

	env = call(t, h, "kanban.card.get", map[string]any{"card_id": "card-1"})
	mustOK(t, env, "card.get")
	card := env["card"].(map[string]any)
	if card["goal_id"] != "goal-9" || card["order"] != 3 {
		t.Errorf("card provenance not echoed: %s", jsonOf(card))
	}
	if ref := card["ref"].(map[string]any); ref["kind"] != "goal" || ref["id"] != "goal-9" {
		t.Errorf("card ref DTO = %s", jsonOf(ref))
	}

	env = call(t, h, "kanban.card.create", map[string]any{"board_id": "board-1", "column_id": "col-1", "title": "new card"})
	mustOK(t, env, "card.create")
	newID := env["card"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(newID, "card-") {
		t.Errorf("minted card id = %q", newID)
	}
	if _, seen := st.cards[newID]; !seen {
		t.Errorf("store never saw the created card")
	}

	env = call(t, h, "kanban.card.timeline", map[string]any{"card_id": "card-1"})
	mustOK(t, env, "card.timeline")
	if !strings.Contains(jsonOf(env["events"]), `"evt-1"`) {
		t.Errorf("timeline echo = %s", jsonOf(env["events"]))
	}

	env = call(t, h, "kanban.reconcile.now", nil)
	mustOK(t, env, "reconcile.now")
	if rec.calls != 1 {
		t.Errorf("ReconcileNow not delegated")
	}
}

func TestKanbanActorResolution(t *testing.T) {
	h, _, hu, _, _ := newTestHandler(KanbanDeps{})
	call(t, h, "kanban.card.move", map[string]any{"card_id": "card-1", "column_id": "col-2"})
	if hu.lastActor != "owner" {
		t.Errorf("no-identity, no-actor move must resolve actor=owner (HumanCommands refuses empty), got %q", hu.lastActor)
	}
	ctx := ContextWithCallerIdentity(context.Background(), &auth.Identity{UserID: "u7", UserName: "Sam"})
	res, err := h.handleCardMove(ctx, json.RawMessage(`{"card_id":"card-1","column_id":"col-2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["ok"] != true || hu.lastActor != "Sam" {
		t.Errorf("validated identity name must become the audit actor, got %q", hu.lastActor)
	}
}

// ---- C5 grant lane -------------------------------------------------------------

func TestKanban_ReadsRequireBoardRead(t *testing.T) {
	gate := &fakeGate{grants: map[string]struct{}{kanban.GrantBoardMutate: {}, kanban.GrantBoardTakeover: {}}}
	h, _, _, _, _ := newTestHandler(KanbanDeps{Authorizer: gate})
	for _, m := range []struct {
		method string
		p      string
	}{
		{"kanban.board.list", `{}`},
		{"kanban.column.list", `{"board_id":"b"}`},
		{"kanban.card.list", `{"column_id":"c"}`},
		{"kanban.card.get", `{"card_id":"c"}`},
		{"kanban.card.timeline", `{"card_id":"c"}`},
	} {
		err := callErr(t, h, m.method, m.p)
		// The CLI (kanbanError grant scan), the gateway 403 lane, and the
		// Dart _grantFromMessage all parse "missing grant <g>" from the
		// error text — frozen contract.
		if !strings.Contains(err.Error(), "missing grant "+kanban.GrantBoardRead) {
			t.Errorf("%s without board.read = %v, want 'missing grant board.read'", m.method, err)
		}
	}
}

func TestKanban_MutationsRefusedWithoutBoardMutate(t *testing.T) {
	gate := &fakeGate{grants: map[string]struct{}{kanban.GrantBoardRead: {}, kanban.GrantBoardTakeover: {}}}
	h, _, hu, rec, _ := newTestHandler(KanbanDeps{Authorizer: gate})
	for _, m := range []struct {
		method string
		p      string
	}{
		{"kanban.board.create", `{"name":"x"}`},
		{"kanban.column.set_wip", `{"column_id":"c","wip":1}`},
		{"kanban.card.create", `{"board_id":"b","column_id":"c","title":"t"}`},
		{"kanban.card.move", `{"card_id":"c","column_id":"d"}`},
		{"kanban.card.assign", `{"card_id":"c","employee_id":"e"}`},
		{"kanban.card.comment", `{"card_id":"c","body":"t"}`},
		{"kanban.reconcile.now", `{}`},
	} {
		err := callErr(t, h, m.method, m.p)
		if !strings.Contains(err.Error(), "missing grant "+kanban.GrantBoardMutate) {
			t.Errorf("%s without board.mutate = %v, want 'missing grant board.mutate'", m.method, err)
		}
	}
	if hu.moveCalls+hu.takeoverCalls+hu.takeoverMove+hu.assignCalls+hu.commentCalls+rec.calls != 0 {
		t.Errorf("denied mutation must never reach the service layer")
	}
}

// TestKanban_TakeoverIsConjunctive pins the C5 rule from kanbansvc commit
// 310701d5 at the RPC lane: takeover needs board.mutate AND board.takeover;
// each single-grant token is refused, naming the first missing grant.
func TestKanban_TakeoverIsConjunctive(t *testing.T) {
	params := `{"card_id":"card-1","column_id":"col-2","actor":"maria"}`
	for _, tc := range []struct {
		name      string
		grants    []string
		wantFirst string
	}{
		{"mutate only", []string{kanban.GrantBoardMutate}, kanban.GrantBoardTakeover},
		{"takeover only", []string{kanban.GrantBoardTakeover}, kanban.GrantBoardMutate},
		{"read only", []string{kanban.GrantBoardRead}, kanban.GrantBoardMutate},
	} {
		g := &fakeGate{grants: map[string]struct{}{}}
		for _, grant := range tc.grants {
			g.grants[grant] = struct{}{}
		}
		h, _, hu, _, _ := newTestHandler(KanbanDeps{Authorizer: g})
		err := callErr(t, h, "kanban.card.takeover", params)
		if !strings.Contains(err.Error(), "missing grant "+tc.wantFirst) {
			t.Errorf("%s: takeover err = %v, want naming %s", tc.name, err, tc.wantFirst)
		}
		if hu.takeoverMove != 0 || hu.takeoverCalls != 0 {
			t.Errorf("%s: refused takeover must not reach the human lane", tc.name)
		}
	}
	// BOTH grants -> allowed.
	h, _, hu, _, _ := newTestHandler(KanbanDeps{})
	mustOK(t, call(t, h, "kanban.card.takeover", json.RawMessage(params)), "takeover both grants")
	if hu.takeoverMove != 1 {
		t.Errorf("conjunctive-allowed takeover must delegate once")
	}
}

func TestKanban_NilAuthorizerIsOwnerTrusted(t *testing.T) {
	deps := KanbanDeps{Authorizer: nil}
	h, _, _, _, _ := newTestHandler(deps)
	mustOK(t, call(t, h, "kanban.board.list", nil), "board.list nil gate")
	mustOK(t, call(t, h, "kanban.card.move", map[string]any{"card_id": "c", "column_id": "d"}), "move nil gate")
}

func TestKanban_PrincipalIsTheValidatedIdentity(t *testing.T) {
	gate := &fakeGate{grants: allGrants()}
	h, _, _, _, _ := newTestHandler(KanbanDeps{Authorizer: gate})
	ctx := ContextWithCallerIdentity(context.Background(), &auth.Identity{UserID: "u7"})
	if _, err := h.handleBoardList(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(gate.checks) != 1 || gate.checks[0] != "u7|"+kanban.GrantBoardRead {
		t.Errorf("grant checks = %v, want [u7|board.read]", gate.checks)
	}
	// Socket path (no identity): principal "" -> owner-trusted by Authorizer semantics.
	if _, err := h.handleBoardList(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if gate.checks[1] != "|"+kanban.GrantBoardRead {
		t.Errorf("socket principal = %q, want empty", gate.checks[1])
	}
}

// ---- typed service errors: text rides the error path intact ---------------------

func TestKanban_TypedErrorTextPins(t *testing.T) {
	since := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		service string
		err     error
		method  string
		params  string
		wantSub string // frozen text the CLI/TUI/Dart regexes match
	}{
		{"lease held", "human", kanban.ErrLeaseHeld{Holder: "bot-9", Since: since},
			"kanban.card.move", `{"card_id":"card-1","column_id":"col-2"}`,
			"lease held by bot-9 since 2026-11-15T08:00:00Z"},
		{"wip full", "human", kanban.ErrWIPFull{ColumnID: "col-2", Limit: 2},
			"kanban.card.move", `{"card_id":"card-1","column_id":"col-2"}`,
			"WIP limit reached for column col-2: 2"},
		{"unknown card", "store", nil, // store fake answers ErrUnknownCard naturally
			"kanban.card.get", `{"card_id":"nope"}`, "unknown card: nope"},
		{"unknown column (set_wip)", "manager", kanban.ErrUnknownColumn{ID: "col-x"},
			"kanban.column.set_wip", `{"board_id":"board-1","column_id":"col-x","wip":1}`,
			"unknown column: col-x"},
		{"stale card", "human", kanban.ErrStaleCard,
			"kanban.card.move", `{"card_id":"card-1","column_id":"col-2"}`,
			"card not in source column or not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st, hu, _, mgr := newTestHandler(KanbanDeps{})
			switch tc.service {
			case "human":
				hu.err = tc.err
			case "manager":
				mgr.err = tc.err
			case "store":
				st.err = tc.err
			}
			// The transport surfaces handler errors verbatim
			// ("[code] message"), so errors.Is must survive too (typed
			// identity for any future in-daemon caller).
			err := callErr(t, h, tc.method, tc.params)
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err %q missing frozen text %q", err.Error(), tc.wantSub)
			}
			if tc.err != nil && !errors.Is(tc.err, kanban.ErrStaleCard) && !errors.Is(err, tc.err) {
				t.Errorf("typed error must survive the handler unchanged, got %T %v", err, err)
			}
		})
	}
}

func TestKanban_WrappedTypedErrorsStillIdentifiable(t *testing.T) {
	// The service layer %w-wraps contract errors (e.g. "kanban: move: %w");
	// the handler passes them through untouched, so errors.Is and the
	// substring both still resolve on the client side.
	h, _, hu, _, _ := newTestHandler(KanbanDeps{})
	wrapped := &wrapErr{err: kanban.ErrLeaseHeld{Holder: "b", Since: time.Now()}}
	hu.err = wrapped
	err := callErr(t, h, "kanban.card.move", `{"card_id":"card-1","column_id":"col-2"}`)
	if !errors.Is(err, wrapped.err) {
		t.Errorf("wrapped ErrLeaseHeld lost its identity: %v", err)
	}
	if !strings.Contains(err.Error(), "lease held by b") {
		t.Errorf("wrapped ErrLeaseHeld lost its text: %v", err)
	}
}

type wrapErr struct{ err error }

func (w *wrapErr) Error() string { return "kanban: move: " + w.err.Error() }
func (w *wrapErr) Unwrap() error { return w.err }

// ---- param validation & decode tolerance ---------------------------------------

func TestKanban_ParamValidation(t *testing.T) {
	h, _, _, _, _ := newTestHandler(KanbanDeps{})
	for _, tc := range []struct {
		method, why string
		p           string
	}{
		{"kanban.board.create", "name", `{}`},
		{"kanban.column.list", "board_id", `{}`},
		{"kanban.card.list", "column_id or board_id", `{}`},
		{"kanban.card.get", "card_id", `{}`},
		{"kanban.card.timeline", "card_id", `{}`},
		{"kanban.card.create", "title", `{"board_id":"b","column_id":"c"}`},
		{"kanban.card.move", "target column", `{"card_id":"c"}`},
		{"kanban.card.takeover", "card_id", `{}`},
		{"kanban.card.assign", "card_id", `{}`},
		{"kanban.card.comment", "body", `{"card_id":"c"}`},
		{"kanban.column.set_wip", "column_id", `{"wip":2}`},
		{"kanban.column.set_wip", "wip (0 must be legal, absence not)", `{"column_id":"c"}`},
	} {
		err := callErr(t, h, tc.method, tc.p)
		if !errors.Is(err, ErrKanbanInvalidParams) {
			t.Errorf("%s missing %s: err %v must carry ErrKanbanInvalidParams (-32602 mapping)", tc.method, tc.why, err)
		}
		if !errcls.IsParameterError(err) {
			t.Errorf("%s missing %s: server.dispatch must map to InvalidParams", tc.method, tc.why)
		}
	}
	err := callErr(t, h, "kanban.card.move", `null`)
	if !errors.Is(err, ErrKanbanInvalidParams) {
		t.Errorf("null params move = %v, want invalid params", err)
	}
	// Malformed JSON is an InvalidParams error, never a panic and never
	// misreported as a service failure.
	res, err := h.handleCardMove(context.Background(), json.RawMessage(`{"oops`))
	if res != nil {
		t.Errorf("junk params must not produce a result: %v", res)
	}
	if !errors.Is(err, ErrKanbanInvalidParams) {
		t.Errorf("junk params = %v, want invalid params", err)
	}
}

func TestKanban_DecodeIsTolerantOfUnknownKeys(t *testing.T) {
	// The gateway forwards keys verbatim; a key added by one lane must not
	// 400 the others (forwarded-proxy convention: tolerant decode +
	// explicit required-field checks).
	h, _, hu, _, _ := newTestHandler(KanbanDeps{})
	env, err := callRaw(t, h, "kanban.card.move", `{"card_id":"k1","column_id":"c2","future_lane_field":{"nested":true}}`)
	if err != nil {
		t.Fatalf("unknown key must be ignored, got %v", err)
	}
	mustOK(t, env, "move")
	if hu.moveCalls != 1 {
		t.Errorf("delegation did not happen")
	}
}

func TestKanban_KanbanFlexInt(t *testing.T) {
	var p struct {
		A *kanbanFlexInt `json:"a"`
		B *kanbanFlexInt `json:"b"`
		C *kanbanFlexInt `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":7,"b":"9","c":0}`), &p); err != nil {
		t.Fatalf("number and decimal-string forms must both decode: %v", err)
	}
	if *p.A != 7 || *p.B != 9 || *p.C != 0 {
		t.Errorf("decoded %v %v %v", *p.A, *p.B, *p.C)
	}
	var bad struct {
		A *kanbanFlexInt `json:"a"`
	}
	if err := json.Unmarshal([]byte(`{"a":"nope"}`), &bad); err == nil {
		t.Errorf("non-numeric string must fail decode")
	}
}

// ---- reply-envelope keys (readers: CLI kanban_format.go, TUI kanban_view_wire.go,
// Dart kanban_models.dart) — every listed key must be present on the wire ---------

func roundTrip(env map[string]any) map[string]any {
	b, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
}

func requireKeys(t *testing.T, reader string, row map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := row[k]; !ok {
			t.Errorf("%s reader expects key %q, handler reply = %s", reader, k, jsonOf(row))
		}
	}
}

func TestKanban_EnvelopeKeys_BoardsColumnsCards(t *testing.T) {
	h, _, _, _, _ := newTestHandler(KanbanDeps{})

	env := roundTrip(call(t, h, "kanban.board.list", nil))
	rows, _ := env["boards"].([]any) // CLI listMaps(result["boards"]); Dart _listOf(data,"boards")
	if len(rows) != 1 {
		t.Fatalf("boards = %s", jsonOf(env["boards"]))
	}
	// renderBoards getStringOr: id, name. KanbanBoard.fromJson: id, name.
	requireKeys(t, "board", rows[0].(map[string]any), "id", "name")

	env = roundTrip(call(t, h, "kanban.column.list", map[string]any{"board_id": "board-1"}))
	rows = env["columns"].([]any) // CLI renderColumns; Dart _listOf(data,"columns")
	// renderColumns: id, name, position (numberString), wip (wipString/numField).
	// KanbanColumn.fromJson: id, board_id, name, position, wip.
	requireKeys(t, "column", rows[0].(map[string]any), "id", "board_id", "name", "position", "wip")

	env = roundTrip(call(t, h, "kanban.card.list", map[string]any{"column_id": "col-1"}))
	rows = env["cards"].([]any) // CLI renderCards + renderBoardView; Dart _listOf(data,"cards")
	// renderCards: id, title, order, employee_id, labels. renderCardResult:
	// + board_id, column_id, ref{kind,id}, goal_id, plan_id, session_id,
	// trigger_ref. KanbanCard.fromJson: all of those.
	for _, raw := range rows {
		card := raw.(map[string]any)
		requireKeys(t, "card", card,
			"id", "board_id", "column_id", "title", "ref", "labels",
			"goal_id", "employee_id", "plan_id", "session_id", "trigger_ref", "order")
		ref := card["ref"].(map[string]any)
		requireKeys(t, "card.ref", ref, "kind", "id")
	}
}

func TestKanban_EnvelopeKeys_CardGetCommentTimeline(t *testing.T) {
	h, _, _, _, _ := newTestHandler(KanbanDeps{})

	env := roundTrip(call(t, h, "kanban.card.get", map[string]any{"card_id": "card-1"}))
	// CLI renderCardResult reads result["card"]; Dart _record(data, ["card",…]).
	card, ok := env["card"].(map[string]any)
	if !ok {
		t.Fatalf("card envelope = %s", jsonOf(env))
	}
	requireKeys(t, "card.get", card,
		"id", "board_id", "column_id", "title", "ref", "labels",
		"goal_id", "employee_id", "plan_id", "session_id", "trigger_ref", "order")

	env = roundTrip(call(t, h, "kanban.card.comment", map[string]any{"card_id": "card-1", "body": "hi"}))
	// Dart _record(data, ["event","Event","card_event","CardEvent"]).
	ev, ok := env["event"].(map[string]any)
	if !ok {
		t.Fatalf("comment echo = %s", jsonOf(env))
	}
	// CLI renderTimeline + TUI + KanbanCardEvent.fromJson readers.
	requireKeys(t, "card event", ev, "id", "card_id", "kind", "actor", "body", "created_at")
	if ts, _ := ev["created_at"].(string); ts == "" {
		t.Errorf("created_at must be an RFC3339 string both time.Parse and DateTime.tryParse accept, got %v", ev["created_at"])
	}

	env = roundTrip(call(t, h, "kanban.card.timeline", map[string]any{"card_id": "card-1"}))
	rows := env["events"].([]any) // CLI renderTimeline; Dart _listOf(data,"events"); TUI kanbanDecodeList
	requireKeys(t, "timeline event", rows[0].(map[string]any), "id", "card_id", "kind", "actor", "body", "created_at")
}

func TestKanban_EnvelopeKeys_CreateReconcileMutation(t *testing.T) {
	h, _, _, rec, _ := newTestHandler(KanbanDeps{})

	env := roundTrip(call(t, h, "kanban.board.create", map[string]any{"name": "b"}))
	// renderCreated reads top-level id OR board.id; Dart _record(data,["board",…]).
	board := env["board"].(map[string]any)
	requireKeys(t, "created board", board, "id", "name")

	env = roundTrip(call(t, h, "kanban.card.create", map[string]any{"board_id": "board-1", "column_id": "col-1", "title": "t"}))
	requireKeys(t, "created card", env["card"].(map[string]any), "id")

	// reconcile counters: the CLI renderReconcile reads created/moved/fixed/
	// deleted top-level (or under "stats") and the Dart reconcileNow passes
	// the WHOLE body to KanbanReconcileStats.fromJson — so the counters ride
	// flat at the top level.
	rec.stats = kanban.ReconcileStats{Created: 4, Moved: 2, Fixed: 1, Deleted: 0}
	env = roundTrip(call(t, h, "kanban.reconcile.now", map[string]any{}))
	requireKeys(t, "reconcile", env, "created", "moved", "fixed", "deleted", "total")
	if env["created"].(float64) != 4 || env["moved"].(float64) != 2 || env["fixed"].(float64) != 1 || env["deleted"].(float64) != 0 {
		t.Errorf("counters = %s", jsonOf(env))
	}

	// move ok-line: the CLI renderMutation reads ok, optional note and
	// lease_blocked; a plain move must NOT carry lease_blocked (only the
	// takeover bypass stamps it).
	env = roundTrip(call(t, h, "kanban.card.move", map[string]any{"card_id": "card-1", "column_id": "col-2"}))
	mustOK(t, env, "move")
	if _, has := env["lease_blocked"]; has {
		t.Errorf("plain move must not stamp lease_blocked: %s", jsonOf(env))
	}
}

func TestKanban_UnknownColumnsResolveThroughBoardLookup(t *testing.T) {
	// CLI sends column_id without board_id; an unknown column must surface
	// the frozen "unknown column: <id>" text (Dart _cardIdFromMessage-style
	// parsing and the CLI passthrough both key off it), NOT the Manager's
	// "board id is required" plumbing error.
	h, _, _, _, mgr := newTestHandler(KanbanDeps{})
	mgr.err = nil
	err := callErr(t, h, "kanban.column.set_wip", `{"column_id":"ghost","wip":1}`)
	if !errors.Is(err, kanban.ErrUnknownColumn{ID: "ghost"}) && !strings.Contains(err.Error(), "unknown column: ghost") {
		t.Errorf("set_wip unknown column = %v, want unknown column text", err)
	}
}

// ---- unwired deps ---------------------------------------------------------------

func TestKanban_UnwiredDepsDegradeNeverPanic(t *testing.T) {
	h := NewKanbanHandler(KanbanDeps{})
	for _, tc := range []struct {
		method string
		p      string
	}{
		{"kanban.board.list", `{}`},
		{"kanban.board.create", `{"name":"x"}`},
		{"kanban.column.list", `{"board_id":"b"}`},
		{"kanban.column.set_wip", `{"column_id":"c","wip":1}`},
		{"kanban.card.list", `{"column_id":"c"}`},
		{"kanban.card.get", `{"card_id":"c"}`},
		{"kanban.card.create", `{"board_id":"b","column_id":"c","title":"t"}`},
		{"kanban.card.move", `{"card_id":"c","column_id":"d"}`},
		{"kanban.card.takeover", `{"card_id":"c"}`},
		{"kanban.card.assign", `{"card_id":"c","employee_id":"e"}`},
		{"kanban.card.comment", `{"card_id":"c","body":"t"}`},
		{"kanban.card.timeline", `{"card_id":"c"}`},
		{"kanban.reconcile.now", `{}`},
	} {
		_, err := callRaw(t, h, tc.method, tc.p)
		if err == nil || !strings.Contains(err.Error(), "not wired") {
			t.Errorf("%s unwired = %v, want a not-wired error", tc.method, err)
		}
	}
	// nil handler receiver must not panic either (intake_status posture).
	var nilH *KanbanHandler
	if _, err := nilH.handleReconcileNow(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "not wired") {
		t.Errorf("nil handler reconcile = %v", err)
	}
}

func TestKanban_ReconcileNowMapsToReconciler(t *testing.T) {
	h, _, _, rec, _ := newTestHandler(KanbanDeps{})
	mustOK(t, call(t, h, "kanban.reconcile.now", nil), "reconcile.now")
	if rec.calls != 1 {
		t.Fatalf("ReconcileNowStats calls = %d, want 1", rec.calls)
	}
	rec.err = fmt.Errorf("reconcile: store unavailable")
	err := callErr(t, h, "kanban.reconcile.now", `{}`)
	if err == nil || !strings.Contains(err.Error(), "store unavailable") {
		t.Errorf("reconcile failure = %v, want the service text to pass through", err)
	}
}
