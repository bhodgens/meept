package main

// Tests for the C6 kanban CLI (cmd/meept/kanban.go). Pattern follows
// agents_cmd_test.go: a fake transport.Client (embedded interface, Call +
// Close overridden — every other method stays nil and panics if a kanban
// command accidentally reaches for it) driven through the real cobra RunE
// path via the kanbanConnectDaemon seam. Covers: command-tree registration,
// C4 method + payload shape per command, --json passthrough, and the C10
// human error rendering (lease holder+age, wip column+limit, grant denial).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/transport"
)

// --- fake transport -------------------------------------------------------

type recordedKanbanCall struct {
	method string
	params map[string]any
}

type fakeKanbanClient struct {
	transport.Client
	responses map[string]json.RawMessage // method -> canned result
	callErrs  map[string]error           // method -> transport-level error
	calls     []recordedKanbanCall
}

func newFakeKanbanClient() *fakeKanbanClient {
	return &fakeKanbanClient{
		responses: map[string]json.RawMessage{},
		callErrs:  map[string]error{},
	}
}

func (f *fakeKanbanClient) Call(method string, params any) (json.RawMessage, error) {
	if err, ok := f.callErrs[method]; ok {
		return nil, err
	}
	m, _ := params.(map[string]any)
	f.calls = append(f.calls, recordedKanbanCall{method: method, params: m})
	if raw, ok := f.responses[method]; ok {
		return raw, nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeKanbanClient) Close() error { return nil }

var _ transport.Client = (*fakeKanbanClient)(nil)

func (f *fakeKanbanClient) lastCall(t *testing.T) recordedKanbanCall {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("fake client recorded no calls")
	}
	return f.calls[len(f.calls)-1]
}

// withFakeKanban swaps the CLI's dial seam for the fake and restores it on
// cleanup, so RunE bodies execute end-to-end without a daemon.
func withFakeKanban(t *testing.T, f *fakeKanbanClient) {
	t.Helper()
	orig := kanbanConnectDaemon
	kanbanConnectDaemon = func() (transport.Client, error) { return f, nil }
	t.Cleanup(func() { kanbanConnectDaemon = orig })
}

func execKanban(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newKanbanCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mustExecKanban(t *testing.T, f *fakeKanbanClient, args ...string) string {
	t.Helper()
	withFakeKanban(t, f)
	out, err := execKanban(t, args...)
	if err != nil {
		t.Fatalf("kanban %v: unexpected error: %v (output: %s)", args, err, out)
	}
	return out
}

// --- command tree (C6 verbs) ----------------------------------------------

func TestKanbanCommandStructure(t *testing.T) {
	cmd := newKanbanCmd()
	if cmd == nil {
		t.Fatal("expected non-nil kanban command")
	}
	if cmd.Name() != "kanban" {
		t.Errorf("kanban command name: got %q, want %q", cmd.Name(), "kanban")
	}

	subs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subs[sub.Name()] = true
	}
	for _, want := range []string{"list", "show", "board", "create", "move", "takeover", "wip", "reconcile"} {
		if !subs[want] {
			t.Errorf("kanban command missing C6 verb %q", want)
		}
	}

	// All C4 RPC methods reachable: assign/comment/timeline ride the verb
	// tree on top of the eight C6 names.
	for _, want := range []string{"assign", "comment", "timeline"} {
		if !subs[want] {
			t.Errorf("kanban command missing card sub-verb %q", want)
		}
	}

	// board sub-tree.
	boardCmd := findSubcommand(t, cmd, "board")
	boardSubs := map[string]bool{}
	for _, sub := range boardCmd.Commands() {
		boardSubs[sub.Name()] = true
	}
	for _, want := range []string{"create", "list", "columns", "cards"} {
		if !boardSubs[want] {
			t.Errorf("kanban board missing subcommand %q", want)
		}
	}

	// --json flag on every leaf that renders a table or a mutation.
	for _, path := range [][]string{
		{"list"}, {"show"}, {"board", "list"}, {"board", "columns"}, {"board", "cards"},
		{"board", "create"}, {"create"}, {"move"}, {"takeover"}, {"wip"},
		{"assign"}, {"comment"}, {"timeline"}, {"reconcile"},
	} {
		c := cmd
		for _, name := range path {
			c = findSubcommand(t, c, name)
		}
		if c.Flags().Lookup("json") == nil {
			t.Errorf("kanban %s missing --json flag", strings.Join(path, " "))
		}
	}
}

func TestKanbanCommandRegisteredOnFreshRoot(t *testing.T) {
	root := &cobra.Command{Use: "meept"}
	root.AddCommand(newKanbanCmd())
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "kanban" {
			found = true
		}
	}
	if !found {
		t.Error("newKanbanCmd did not attach to a root command")
	}
}

// --- board group: happy paths ---------------------------------------------

func TestKanbanList_BoardsHappyPath(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.board.list"] = json.RawMessage(`{"boards":[{"id":"b1","name":"delivery"},{"id":"b2","name":"intake"}]}`)

	out := mustExecKanban(t, f, "list")

	last := f.lastCall(t)
	if last.method != "kanban.board.list" {
		t.Errorf("method: got %q, want kanban.board.list", last.method)
	}
	for _, want := range []string{"b1", "delivery", "b2", "intake"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestKanbanBoardList_SameMethodAsTopList(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.board.list"] = json.RawMessage(`{"boards":[{"id":"b1","name":"delivery"}]}`)
	out := mustExecKanban(t, f, "board", "list")
	if !strings.Contains(out, "delivery") {
		t.Errorf("board list output missing board name: %s", out)
	}
}

func TestKanbanBoardColumns_HappyPath(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.column.list"] = json.RawMessage(`{"columns":[{"id":"c1","board_id":"b1","name":"todo","position":0,"wip":3},{"id":"c2","board_id":"b1","name":"doing","position":1,"wip":0}]}`)

	out := mustExecKanban(t, f, "board", "columns", "b1")

	last := f.lastCall(t)
	if last.method != "kanban.column.list" || last.params["board_id"] != "b1" {
		t.Errorf("call = %s %v, want kanban.column.list {board_id: b1}", last.method, last.params)
	}
	if !strings.Contains(out, "todo") || !strings.Contains(out, "3") {
		t.Errorf("output missing column data: %s", out)
	}
	if !strings.Contains(out, "no limit") {
		t.Errorf("wip 0 should render as 'no limit': %s", out)
	}
}

func TestKanbanBoardCards_HappyPath(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.card.list"] = json.RawMessage(`{"cards":[{"id":"k1","title":"ship v2","column_id":"c1","labels":["urgent"],"employee_id":"coder","order":2}]}`)

	out := mustExecKanban(t, f, "board", "cards", "c1")

	last := f.lastCall(t)
	if last.method != "kanban.card.list" || last.params["column_id"] != "c1" {
		t.Errorf("call = %s %v, want kanban.card.list {column_id: c1}", last.method, last.params)
	}
	for _, want := range []string{"k1", "ship v2", "urgent", "coder"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestKanbanShow_BoardViewComposition(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.column.list"] = json.RawMessage(`{"columns":[{"id":"c1","name":"todo","wip":0},{"id":"c2","name":"doing","wip":2}]}`)
	f.responses["kanban.card.list"] = json.RawMessage(`{"cards":[{"id":"k1","title":"alpha","labels":["x"]}]}`)

	out := mustExecKanban(t, f, "show", "b1")

	// One column.list then one card.list per column.
	if len(f.calls) != 3 {
		t.Fatalf("expected 3 calls (columns + 2x cards), got %d: %v", len(f.calls), f.calls)
	}
	if f.calls[0].method != "kanban.column.list" {
		t.Errorf("first call: got %q", f.calls[0].method)
	}
	if f.calls[1].params["column_id"] != "c1" || f.calls[2].params["column_id"] != "c2" {
		t.Errorf("card.list scoped to wrong columns: %v", f.calls[1:])
	}
	for _, want := range []string{"todo", "doing", "alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("board view missing %q: %s", want, out)
		}
	}
}

func TestKanbanShow_CardFlag(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.card.get"] = json.RawMessage(`{"card":{"id":"k1","title":"alpha","board_id":"b1","column_id":"c1","goal_id":"g7","ref":{"kind":"goal","id":"g7"}}}`)

	out := mustExecKanban(t, f, "show", "b1", "--card", "k1")

	last := f.lastCall(t)
	if last.method != "kanban.card.get" || last.params["card_id"] != "k1" {
		t.Errorf("call = %s %v, want kanban.card.get {card_id: k1}", last.method, last.params)
	}
	for _, want := range []string{"k1", "alpha", "g7"} {
		if !strings.Contains(out, want) {
			t.Errorf("card view missing %q: %s", want, out)
		}
	}
}

// --- card group: create/move/takeover/assign/comment/timeline -------------

func TestKanbanCreateCard_Payload(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.card.create"] = json.RawMessage(`{"id":"k9","board_id":"b1","column_id":"c1","title":"new thing"}`)

	out := mustExecKanban(t, f, "create", "b1", "c1", "new thing", "--label", "a", "--label", "b")

	last := f.lastCall(t)
	if last.method != "kanban.card.create" {
		t.Fatalf("method: got %q", last.method)
	}
	if last.params["board_id"] != "b1" || last.params["column_id"] != "c1" || last.params["title"] != "new thing" {
		t.Errorf("params = %v, want board_id/column_id/title positionals", last.params)
	}
	labels, ok := last.params["labels"].([]string)
	if !ok {
		if arr, isArr := last.params["labels"].([]any); isArr {
			labels = make([]string, 0, len(arr))
			for _, v := range arr {
				labels = append(labels, fmt.Sprint(v))
			}
		}
	}
	if len(labels) != 2 || labels[0] != "a" || labels[1] != "b" {
		t.Errorf("labels = %v, want [a b]", last.params["labels"])
	}
	if !strings.Contains(out, "k9") {
		t.Errorf("create output missing assigned id: %s", out)
	}
}

func TestKanbanMoveCard_Payload(t *testing.T) {
	f := newFakeKanbanClient()
	out := mustExecKanban(t, f, "move", "k1", "c2", "--order", "3")

	last := f.lastCall(t)
	if last.method != "kanban.card.move" {
		t.Fatalf("method: got %q", last.method)
	}
	if last.params["card_id"] != "k1" || last.params["column_id"] != "c2" {
		t.Errorf("params = %v, want card_id=k1 column_id=c2", last.params)
	}
	if last.params["order"] != 3 {
		t.Errorf("order param = %v (%T), want int 3", last.params["order"], last.params["order"])
	}
	if !strings.Contains(out, "card moved") {
		t.Errorf("move output: %s", out)
	}
}

func TestKanbanMove_OrderOmittedWhenUnset(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "move", "k1", "c2")
	if _, has := f.lastCall(t).params["order"]; has {
		t.Error("order key must be omitted when --order is unset")
	}
}

func TestKanbanTakeover_Payload(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "takeover", "k1", "--column", "done")

	last := f.lastCall(t)
	if last.method != "kanban.card.takeover" || last.params["card_id"] != "k1" || last.params["column_id"] != "done" {
		t.Errorf("call = %s %v, want kanban.card.takeover {card_id: k1, column_id: done}", last.method, last.params)
	}
}

func TestKanbanAssign_Payload(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "assign", "k1", "coder")

	last := f.lastCall(t)
	if last.method != "kanban.card.assign" || last.params["card_id"] != "k1" || last.params["employee_id"] != "coder" {
		t.Errorf("call = %s %v, want kanban.card.assign {card_id: k1, employee_id: coder}", last.method, last.params)
	}
}

func TestKanbanComment_JoinsPositionals(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "comment", "k1", "blocked", "on", "review")

	last := f.lastCall(t)
	if last.method != "kanban.card.comment" || last.params["card_id"] != "k1" || last.params["body"] != "blocked on review" {
		t.Errorf("call = %s %v, want kanban.card.comment with joined body", last.method, last.params)
	}
}

func TestKanbanTimeline_Renders(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.card.timeline"] = json.RawMessage(`{"events":[{"id":"e1","card_id":"k1","kind":"comment","actor":"alice","body":"hi\nthere","created_at":"2026-11-15T10:00:00Z"}]}`)

	out := mustExecKanban(t, f, "timeline", "k1")

	last := f.lastCall(t)
	if last.method != "kanban.card.timeline" || last.params["card_id"] != "k1" {
		t.Errorf("call = %s %v, want kanban.card.timeline {card_id: k1}", last.method, last.params)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "hi there") {
		t.Errorf("timeline output missing event body (newline-collapsed): %s", out)
	}
}

// --- wip + board create + reconcile ----------------------------------------

func TestKanbanWip_PayloadAndValidation(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "wip", "c1", "3")

	last := f.lastCall(t)
	if last.method != "kanban.column.set_wip" {
		t.Fatalf("method: got %q", last.method)
	}
	if last.params["column_id"] != "c1" {
		t.Errorf("params = %v, want column_id=c1", last.params)
	}
	if last.params["wip"] != 3 {
		t.Errorf("wip param = %v (%T), want int 3", last.params["wip"], last.params["wip"])
	}

	// 0 is legal (no limit).
	withFakeKanban(t, newFakeKanbanClient())
	if _, err := execKanban(t, "wip", "c1", "0"); err != nil {
		t.Errorf("wip limit 0 should be accepted: %v", err)
	}

	// Non-integer and negative limits fail client-side with no RPC call.
	f2 := newFakeKanbanClient()
	withFakeKanban(t, f2)
	if _, err := execKanban(t, "wip", "c1", "abc"); err == nil {
		t.Error("non-integer wip limit should error")
	}
	if _, err := execKanban(t, "wip", "c1", "-1"); err == nil {
		t.Error("negative wip limit should error")
	}
	if len(f2.calls) != 0 {
		t.Errorf("invalid limits must not reach the daemon, got calls: %v", f2.calls)
	}
}

func TestKanbanBoardCreate_Payload(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.board.create"] = json.RawMessage(`{"id":"b7","name":"ops"}`)

	out := mustExecKanban(t, f, "board", "create", "ops", "--id", "b7")

	last := f.lastCall(t)
	if last.method != "kanban.board.create" || last.params["name"] != "ops" || last.params["id"] != "b7" {
		t.Errorf("call = %s %v, want kanban.board.create {name: ops, id: b7}", last.method, last.params)
	}
	if !strings.Contains(out, "b7") {
		t.Errorf("board create output missing id: %s", out)
	}
}

func TestKanbanBoardCreate_OMitsIDWhenUnset(t *testing.T) {
	f := newFakeKanbanClient()
	mustExecKanban(t, f, "board", "create", "ops")
	if _, has := f.lastCall(t).params["id"]; has {
		t.Error("id key must be omitted when --id is unset (daemon generates)")
	}
}

func TestKanbanReconcile_StatsRender(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.reconcile.now"] = json.RawMessage(`{"created":4,"moved":2,"fixed":1,"deleted":0}`)

	out := mustExecKanban(t, f, "reconcile")

	if f.lastCall(t).method != "kanban.reconcile.now" {
		t.Errorf("method: got %q", f.lastCall(t).method)
	}
	for _, want := range []string{"4 created", "2 moved", "1 fixed", "0 deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("reconcile output missing %q: %s", want, out)
		}
	}
}

// --- --json passthrough -----------------------------------------------------

func TestKanbanJSONPassthrough_BoardList(t *testing.T) {
	f := newFakeKanbanClient()
	canned := `{"boards":[{"id":"b1","name":"delivery"}]}`
	f.responses["kanban.board.list"] = json.RawMessage(canned)

	out := mustExecKanban(t, f, "list", "--json")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out)
	}
	boards, ok := parsed["boards"].([]any)
	if !ok || len(boards) != 1 {
		t.Fatalf("--json passthrough lost the boards array: %s", out)
	}
	b := boards[0].(map[string]any)
	if b["id"] != "b1" || b["name"] != "delivery" {
		t.Errorf("--json board = %v, want the daemon's exact shape", b)
	}
}

func TestKanbanJSONPassthrough_TablesBypassed(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.card.get"] = json.RawMessage(`{"card":{"id":"k1","title":"alpha"}}`)

	out := mustExecKanban(t, f, "show", "b1", "--card", "k1", "--json")

	if strings.Contains(out, "id:") {
		t.Errorf("--json must not render the key/value card table: %s", out)
	}
	if !strings.Contains(out, "\"alpha\"") {
		t.Errorf("--json output missing raw field: %s", out)
	}
}

func TestKanbanJSONPassthrough_ShowBoard(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.column.list"] = json.RawMessage(`{"columns":[{"id":"c1","name":"todo","wip":0}]}`)

	out := mustExecKanban(t, f, "show", "b1", "--json")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("--json show output invalid: %v\n%s", err, out)
	}
	if _, ok := parsed["columns"].([]any); !ok {
		t.Errorf("--json show lost the columns array: %s", out)
	}
	// --json short-circuits before the per-column card.list calls.
	if len(f.calls) != 1 {
		t.Errorf("--json show should make exactly 1 call, got %v", f.calls)
	}
}

// --- C10 human error rendering ----------------------------------------------

func TestKanbanErrorMapping(t *testing.T) {
	since := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339)
	cases := []struct {
		name        string
		raw         string
		wantSubs    []string
		dontWantSub []string
	}{
		{
			name: "lease held shows holder + age",
			raw:  "lease held by emp-7 since " + since,
			wantSubs: []string{
				"emp-7", "1h30m", "takeover",
			},
			dontWantSub: []string{"unknown"},
		},
		{
			name:     "lease held without a parseable age still names holder",
			raw:      "kanban: lease held by emp-9",
			wantSubs: []string{"emp-9", "unknown"},
		},
		{
			name:     "wip full shows column + limit",
			raw:      "WIP limit reached for column colB: 2",
			wantSubs: []string{"colB", "2"},
		},
		{
			name:     "grant denial names the required grant",
			raw:      "authorization denied: missing grant board.takeover",
			wantSubs: []string{"board.takeover"},
		},
		{
			name:     "403-style denial with board.read grant",
			raw:      "forbidden: principal lacks board.read",
			wantSubs: []string{"board.read"},
		},
		{
			name:     "unknown errors pass through",
			raw:      "dial unix /tmp/x.sock: no such file",
			wantSubs: []string{"dial unix"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := kanbanError(errors.New(tc.raw))
			msg := err.Error()
			for _, want := range tc.wantSubs {
				if !strings.Contains(msg, want) {
					t.Errorf("mapped error %q missing %q (source: %q)", msg, want, tc.raw)
				}
			}
			for _, no := range tc.dontWantSub {
				if strings.Contains(msg, no) {
					t.Errorf("mapped error %q contains forbidden %q", msg, no)
				}
			}
		})
	}
}

func TestKanbanMove_LeaseHeldSurfacesThroughRunE(t *testing.T) {
	f := newFakeKanbanClient()
	since := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	f.callErrs["kanban.card.move"] = fmt.Errorf("lease held by emp-7 since %s", since)

	withFakeKanban(t, f)
	out, err := execKanban(t, "move", "k1", "c2")
	if err == nil {
		t.Fatalf("lease-held move should error (output: %s)", out)
	}
	for _, want := range []string{"emp-7", "2h0m", "takeover"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("move error %q missing %q", err.Error(), want)
		}
	}
}

func TestKanbanMove_WIPFullSurfacesThroughRunE(t *testing.T) {
	f := newFakeKanbanClient()
	f.callErrs["kanban.card.move"] = errors.New("WIP limit reached for column doing: 5")

	withFakeKanban(t, f)
	_, err := execKanban(t, "move", "k1", "c2")
	if err == nil {
		t.Fatal("wip-full move should error")
	}
	for _, want := range []string{"doing", "5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("move error %q missing %q", err.Error(), want)
		}
	}
}

func TestKanbanTakeover_AuthzDeniedThroughRunE(t *testing.T) {
	f := newFakeKanbanClient()
	f.callErrs["kanban.card.takeover"] = errors.New("missing grant board.takeover")

	withFakeKanban(t, f)
	_, err := execKanban(t, "takeover", "k1")
	if err == nil {
		t.Fatal("grant-denied takeover should error")
	}
	if !strings.Contains(err.Error(), "board.takeover") {
		t.Errorf("takeover error %q must name the required grant", err.Error())
	}
}

// A handler that answers with a 200-level reply carrying {"error": msg}
// (the intake_status/plans.go envelope convention) must surface as an error,
// not render an empty table.
func TestKanbanResultEnvelopeErrorSurfaces(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.board.list"] = json.RawMessage(`{"error":"missing grant board.read"}`)

	withFakeKanban(t, f)
	_, err := execKanban(t, "list")
	if err == nil {
		t.Fatal("envelope error should fail the command")
	}
	if !strings.Contains(err.Error(), "board.read") {
		t.Errorf("envelope error %q should be C10-mapped, naming the grant", err.Error())
	}
}

// Bare top-level arrays are a legal handler reply shape; decodeResultMap
// normalises them so the table renderers still find rows.
func TestKanbanBareArrayResultRenders(t *testing.T) {
	f := newFakeKanbanClient()
	f.responses["kanban.board.list"] = json.RawMessage(`[{"id":"b1","name":"solo"}]`)

	out := mustExecKanban(t, f, "list")
	if !strings.Contains(out, "b1") || !strings.Contains(out, "solo") {
		t.Errorf("bare-array board list not rendered: %s", out)
	}
}

// --- argument validation ------------------------------------------------------

func TestKanbanArgValidation(t *testing.T) {
	f := newFakeKanbanClient()
	withFakeKanban(t, f)

	if _, err := execKanban(t, "move", "only-one"); err == nil {
		t.Error("move with 1 positional should fail ExactArgs(2)")
	}
	if _, err := execKanban(t, "list", "unexpected"); err == nil {
		t.Error("list takes no positionals")
	}
	if _, err := execKanban(t, "comment", "k1"); err == nil {
		t.Error("comment with no text should fail MinimumNArgs(2)")
	}
	if len(f.calls) != 0 {
		t.Errorf("arg validation failures must not reach the daemon: %v", f.calls)
	}
}
