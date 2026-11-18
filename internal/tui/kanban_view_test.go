package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeKanbanRPC records kanban.* calls and answers from a canned result
// map — the component-level fake pattern this package uses for RPC-backed
// models (stubAgentsRPC is the nil-answer variant; the kanban view needs
// recorded params + per-method errors, so calls are captured).
type fakeKanbanRPC struct {
	mu      sync.Mutex
	calls   []fakeKanbanCall
	results map[string]any    // method -> raw JSON result
	errs    map[string]string // method -> error message
}

type fakeKanbanCall struct {
	method string
	params any
}

func newFakeKanbanRPC() *fakeKanbanRPC {
	return &fakeKanbanRPC{results: map[string]any{}, errs: map[string]string{}}
}

func (f *fakeKanbanRPC) setResult(method string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	f.results[method] = string(raw)
}

func (f *fakeKanbanRPC) setError(method, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[method] = msg
}

func (f *fakeKanbanRPC) recordedCalls() []fakeKanbanCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeKanbanCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeKanbanRPC) lastCall(method string) (fakeKanbanCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range slices.Backward(f.calls) {
		if v.method == method {
			return v, true
		}
	}
	return fakeKanbanCall{}, false
}

func (f *fakeKanbanRPC) Call(method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeKanbanCall{method: method, params: params})
	if msg, ok := f.errs[method]; ok {
		return nil, fmt.Errorf("%s", msg)
	}
	raw, ok := f.results[method]
	if !ok {
		return json.RawMessage(`{}`), nil
	}
	s, _ := raw.(string)
	return json.RawMessage(s), nil
}

func (f *fakeKanbanRPC) IsConnected() bool { return true }

// seed a two-column, three-card board in snake_case (the sidecar spelling)
// plus one pascal-case variant test below.
func seedBoard(f *fakeKanbanRPC) {
	f.setResult("kanban.board.list", map[string]any{
		"boards": []any{map[string]any{"id": "b1", "name": "Team board"}},
	})
	f.setResult("kanban.column.list", map[string]any{
		"columns": []any{
			map[string]any{"id": "c1", "board_id": "b1", "name": "Backlog", "position": 0, "wip": 0},
			map[string]any{"id": "c2", "board_id": "b1", "name": "Doing", "position": 1, "wip": 2},
		},
	})
	f.setResult("kanban.card.list", map[string]any{
		"cards": []any{
			map[string]any{"id": "card-one", "board_id": "b1", "column_id": "c1",
				"title": "alpha task", "ref": map[string]any{"kind": "goal", "id": "goal-1234567890"},
				"labels": []any{"bug"}, "employee_id": "eve", "order": 0},
			map[string]any{"id": "card-two", "board_id": "b1", "column_id": "c2",
				"title": "beta task", "ref": map[string]any{"kind": "job", "id": "job-7"},
				"order": 1},
		},
	})
}

// buildPanel constructs a sized panel and drains the Init fetch chain
// synchronously through Update (the component-level test style used by
// agents_panel_render_test.go: drive the model, assert on View()).
func buildPanel(t *testing.T, f *fakeKanbanRPC) *KanbanPanel {
	t.Helper()
	p := NewKanbanPanel(f)
	p.SetSize(120, 30)
	drain(t, p, p.Init())
	return p
}

func drain(t *testing.T, p *KanbanPanel, cmd tea.Cmd) {
	t.Helper()
	for i := 0; i < 6 && cmd != nil; i++ {
		msg := cmd()
		cmd = p.Update(msg)
	}
}

func kp(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r} }
func kEnter() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func kEsc() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// Invariant (AGENTS.md / docs/workflows/tui.md): a sized table must render
// rows in the body, not just a header over an empty viewport.
func TestKanbanBoard_TableSizingInvariant(t *testing.T) {
	f := newFakeKanbanRPC()
	seedBoard(f)
	p := buildPanel(t, f)

	out := p.View()
	if !strings.Contains(out, "alpha task") {
		t.Errorf("board body empty; want card title rendered\n%s", out)
	}
	if !strings.Contains(out, "backlog") {
		t.Errorf("column label (lowercased) missing from rendered rows")
	}
	if p.table.Width() <= 0 || p.tlTable.Width() <= 0 {
		t.Errorf("table viewport widths must be > 0: board=%d timeline=%d", p.table.Width(), p.tlTable.Width())
	}

	// Resize must not blank the table (rows repopulate from cache).
	p.SetSize(140, 40)
	if got := len(p.table.Rows()); got != 2 {
		t.Fatalf("rows after resize = %d, want 2", got)
	}
	if !strings.Contains(p.View(), "beta task") {
		t.Error("board body empty after resize")
	}
}

// Ragged rows (more cells than columns) must never reach bubbles/table —
// SetRows via tableutil is the single normalization boundary.
func TestKanbanBoard_RowNormalization(t *testing.T) {
	p := NewKanbanPanel(newFakeKanbanRPC())
	p.SetSize(120, 30)
	cols := len(p.table.Columns())
	p.cards = []kanbanCardRow{
		{ID: "x", ColumnID: "nope", Title: "over-long", Labels: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("updateBoardTable panicked on ragged rows: %v", r)
			}
		}()
		p.updateBoardTable()
	}()
	for _, row := range p.table.Rows() {
		if len(row) != cols {
			t.Fatalf("row width = %d, want %d (column count)", len(row), cols)
		}
	}
	// unknown column id renders the "?" placeholder, not a crash
	if !strings.Contains(p.View(), "over-long") {
		t.Error("normalized row not rendered")
	}
}

// Keybinding -> C4 command mapping with the actor tag the store records.
func TestKanbanBoard_KeybindingCommandMapping(t *testing.T) {
	f := newFakeKanbanRPC()
	seedBoard(f)
	p := buildPanel(t, f)

	// card-one is in c1 (Backlog, pos 0). "l" moves right -> c2.
	drain(t, p, p.Update(kp('l')))
	call, ok := f.lastCall("kanban.card.move")
	if !ok {
		t.Fatal("l did not issue kanban.card.move")
	}
	params, _ := call.params.(map[string]any)
	if params["card_id"] != "card-one" || params["column_id"] != "c2" {
		t.Errorf("move params = %v, want card-one -> c2", params)
	}
	if params["actor"] != "tui" {
		t.Errorf("move actor = %v, want %q", params["actor"], "tui")
	}

	// "h" from cursor row 0 (c1) has no left column: refusal note, no call.
	before := len(f.recordedCalls())
	drain(t, p, p.Update(kp('h')))
	if _, ok := f.lastCall("kanban.card.move"); !ok {
		t.Fatal("earlier move call missing")
	}
	if got := len(f.recordedCalls()); got != before {
		t.Errorf("left-edge move should not call the daemon (calls before=%d after=%d)", before, got)
	}
	if !strings.Contains(p.View(), "no column to the left") {
		t.Error("left-edge refusal missing from view")
	}

	// "a" opens the assign prompt; typed id + enter issues kanban.card.assign.
	_ = p.Update(kp('a'))
	for _, r := range "bot9" {
		_ = p.Update(kp(r))
	}
	drain(t, p, p.Update(kEnter()))
	call, ok = f.lastCall("kanban.card.assign")
	if !ok {
		t.Fatal("assign prompt enter did not issue kanban.card.assign")
	}
	params, _ = call.params.(map[string]any)
	if params["employee_id"] != "bot9" {
		t.Errorf("assign params = %v, want employee_id bot9", params)
	}

	// "c" opens the comment prompt -> kanban.card.comment with body.
	_ = p.Update(kp('c'))
	_ = p.Update(kp('x'))
	drain(t, p, p.Update(kEnter()))
	if _, ok := f.lastCall("kanban.card.comment"); !ok {
		// inputBuf "x" submits; empty-after-trim guard shouldn't trigger
		t.Fatal("comment prompt enter did not issue kanban.card.comment")
	}

	// "R" issues the reconcile mutation.
	drain(t, p, p.Update(kp('R')))
	if _, ok := f.lastCall("kanban.reconcile.now"); !ok {
		t.Fatal("R did not issue kanban.reconcile.now")
	}
}

// C10 lease rule A: the refusal line must show holder + age and offer the
// takeover bypass; "t" then "y" replays the refused move as takeover with
// the destination column.
func TestKanbanBoard_LeaseRefusalSurfaced(t *testing.T) {
	f := newFakeKanbanRPC()
	seedBoard(f)
	since := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	f.setError("kanban.card.move",
		fmt.Sprintf("lease held by episode-77 since %s", since))
	p := buildPanel(t, f)

	drain(t, p, p.Update(kp('l')))
	out := p.View()
	if !strings.Contains(out, "lease held by episode-77") {
		t.Errorf("lease holder missing from refusal line:\n%s", out)
	}
	if !strings.Contains(out, "1h 30m") {
		t.Errorf("lease age missing from refusal line (want 1h 30m):\n%s", out)
	}
	if !strings.Contains(out, "takeover") {
		t.Error("takeover hint missing from lease refusal")
	}

	// takeover confirm: t -> y replays the move via kanban.card.takeover
	_ = p.Update(kp('t'))
	drain(t, p, p.Update(kp('y')))
	call, ok := f.lastCall("kanban.card.takeover")
	if !ok {
		t.Fatal("t/y did not issue kanban.card.takeover")
	}
	params, _ := call.params.(map[string]any)
	if params["card_id"] != "card-one" || params["to_column_id"] != "c2" {
		t.Errorf("takeover params = %v, want card-one replayed to c2", params)
	}

	// n cancels without a new call.
	_ = p.Update(kp('t'))
	before := len(f.recordedCalls())
	drain(t, p, p.Update(kp('n')))
	if len(f.recordedCalls()) != before {
		t.Error("n on takeover confirm must not call the daemon")
	}
}

func TestKanbanBoard_WIPRefusalSurfaced(t *testing.T) {
	f := newFakeKanbanRPC()
	seedBoard(f)
	f.setError("kanban.card.move", "WIP limit reached for column c2: 2")
	p := buildPanel(t, f)

	drain(t, p, p.Update(kp('l')))
	out := p.View()
	if !strings.Contains(out, "wip full") || !strings.Contains(out, "limit 2") {
		t.Errorf("wip refusal missing column limit:\n%s", out)
	}
	// A wip refusal does NOT offer takeover (not a lease hold).
	if strings.Contains(out, "press t to takeover") {
		t.Error("wip refusal must not advertise the takeover bypass")
	}
}

// Detail view: enter drills in; kanban.card.get + kanban.card.timeline
// rows render in a sized timeline table.
func TestKanbanBoard_CardDetailTimeline(t *testing.T) {
	f := newFakeKanbanRPC()
	seedBoard(f)
	f.setResult("kanban.card.get", map[string]any{
		"Card": map[string]any{ // pascal spelling (C1 structs have no json tags)
			"ID": "card-one", "ColumnID": "c1", "Title": "alpha task",
			"Ref":    map[string]any{"Kind": "goal", "ID": "goal-1234567890"},
			"Labels": []any{"bug"},
		},
	})
	f.setResult("kanban.card.timeline", map[string]any{
		"events": []any{
			map[string]any{"kind": "comment", "actor": "human", "body": "moving it",
				"created_at": time.Now().Format(time.RFC3339)},
			map[string]any{"Kind": "move", "Actor": "tui", "Body": "c1 -> c2",
				"CreatedAt": time.Now().Format(time.RFC3339)},
		},
	})
	p := buildPanel(t, f)

	drain(t, p, p.Update(kEnter()))
	if p.subView != kanbanViewDetail {
		t.Fatalf("enter did not drill into detail (subView=%v)", p.subView)
	}
	out := p.View()
	for _, want := range []string{"alpha task", "moving it", "c1 -> c2", "comment", "move"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view missing %q:\n%s", want, out)
		}
	}
	if len(p.tlTable.Rows()) != 2 {
		t.Errorf("timeline rows = %d, want 2", len(p.tlTable.Rows()))
	}
	// esc walks back to the board and refetches.
	drain(t, p, p.Update(kEsc()))
	if p.subView != kanbanViewBoard {
		t.Fatal("esc must return to the board view")
	}
}

// Disconnected client surfaces the shared not-connected error, never a
// panic (nil-guard invariant).
func TestKanbanBoard_NotConnectedGuard(t *testing.T) {
	p := NewKanbanPanel(nilRPCFalse{})
	p.SetSize(120, 30)
	drain(t, p, p.Init())
	if !strings.Contains(p.View(), ErrNotConnected) {
		t.Errorf("offline panel must show %q", ErrNotConnected)
	}
}

type nilRPCFalse struct{}

func (nilRPCFalse) Call(string, any) (json.RawMessage, error) { return nil, nil }
func (nilRPCFalse) IsConnected() bool                         { return false }

// Palette registration: the command palette carries the board entry on
// key "b", and DefaultClientConfig binds ViewKanban so the manifest test
// (05/06) sees a live key like every other palette item.
func TestKanbanBoard_PaletteRegistration(t *testing.T) {
	m := CommandPaletteModal(DefaultStyles(), DefaultClientConfig())
	var found *ModalItem
	for i := range m.items {
		if m.items[i].Label == "kanban board" {
			found = &m.items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("palette missing 'kanban board' entry")
	}
	if found.Key != "b" {
		t.Errorf("kanban palette key = %q, want \"b\" (free of the a/c/e/f/i/m/n/o/p/q/r/s/t/u/v/y bindings)", found.Key)
	}
	if got := DefaultClientConfig().Keybindings.CommandPalette.ViewKanban; got != "b" {
		t.Errorf("DefaultClientConfig ViewKanban = %q, want \"b\"", got)
	}
	// The label is lowercase (repo UI rule).
	if strings.ToLower(found.Label) != found.Label {
		t.Errorf("palette label %q not lowercase", found.Label)
	}
}

// Refusal decoding is pure (no panel state) — pin the C10 contract lines
// and the age formatter directly.
func TestKanbanBoard_RefusalMessageDecoding(t *testing.T) {
	since := time.Now().Add(-30 * time.Second).Format(time.RFC3339)
	lease := kanbanRefusalMessage(fmt.Errorf("lease held by ep-1 since %s", since))
	if !strings.Contains(lease, "lease held by ep-1") || !strings.Contains(lease, "30s") {
		t.Errorf("lease decode = %q", lease)
	}
	if lease != strings.ToLower(strings.ReplaceAll(lease, "WIP", "wip")) {
		t.Errorf("lease line not lowercase: %q", lease)
	}
	wip := kanbanRefusalMessage(fmt.Errorf("WIP limit reached for column c9: 3"))
	if !strings.Contains(wip, "wip full: column c9 limit 3") {
		t.Errorf("wip decode = %q", wip)
	}
	other := kanbanRefusalMessage(fmt.Errorf("missing grant board.mutate"))
	if other != "missing grant board.mutate" {
		t.Errorf("passthrough decode = %q", other)
	}
	if kanbanRefusalMessage(nil) != "" {
		t.Error("nil error must decode to empty line")
	}
}
