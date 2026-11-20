package tui

// 06-planning TEST-GATE T1 (F13a): the strategic approval gate must be
// visible and actionable in the TUI chat layer.
//
// RED today for missing-implementation reasons:
//   - task.pending_approval is published (internal/agent/strategic.go
//     awaitUserApproval) but the App event switch handles only the
//     plans.db family (plan.submitting/completed/confirmed/rejected,
//     internal/tui/app.go) - the task-gate event is dropped, no card.
//   - the chat card's "/plan approve <id>" hint is dead: executePlan
//     ignores args and calls session.set_mode (command_handler.go), so
//     the hinted action can never approve the gated task.
//   - ApproveTask/RejectTask exist on RPCClient (rpc.go) with zero TUI
//     callers; no approve/reject affordance is reachable from any card.
//
// C2 contract pins: approve -> task.approve{task_id}; reject ->
// task.reject{task_id, reason}; reject NEVER executes (no task.approve
// for the rejected task). The load-bearing reject-never-executes
// assertion lives in TestRejectFromCardNeverExecutes.
//
// Delivery seam: the gate injects EventStreamDataMsg directly (the exact
// message the real bus.poll loop produces) so it pins the App event
// switch + card render + RPC wiring, not the poll transport (pinned
// separately by events_topics_test.go). "task.*" is already in
// DefaultEventStreamConfig, so the subscription covers the topic; the
// gap is the missing switch case.
//
// Gate tests: do not weaken. GREEN after 06-planning/01.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// gateApprovalRecorder is a minimal JSON-RPC server over a unix socket
// that RECORDS every method+params. The rpcStub in headless_test.go
// serves canned results but keeps no call log; the C2 pins need the
// exact wire calls. task.approve / task.reject answer with the C2 result
// shapes; a seeded result map serves specific methods; everything else
// answers {} so init-time fetches stay deterministic.
type gateApprovalRecorder struct {
	mu       sync.Mutex
	calls    []gateApprovalCall
	results  map[string]any
	listener net.Listener
}

type gateApprovalCall struct {
	Method string
	Params map[string]any
}

func newGateApprovalRecorder(t *testing.T) *gateApprovalRecorder {
	t.Helper()
	dir, err := os.MkdirTemp("", "meept-06-gate-*")
	if err != nil {
		t.Fatalf("recorder temp dir: %v", err)
	}
	path := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("recorder listen: %v", err)
	}
	rec := &gateApprovalRecorder{listener: l, results: map[string]any{}}
	go rec.acceptLoop()
	t.Cleanup(func() {
		l.Close()
		os.RemoveAll(dir)
	})
	return rec
}

func (r *gateApprovalRecorder) socketPath() string { return r.listener.Addr().String() }

func (r *gateApprovalRecorder) setResult(method string, result any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results[method] = result
}

func (r *gateApprovalRecorder) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return
		}
		go r.serve(conn)
	}
}

func (r *gateApprovalRecorder) serve(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		var length int
		if _, err := fmt.Fscanf(br, "%d\n", &length); err != nil {
			return
		}
		buf := make([]byte, length)
		if _, err := readFull(br, buf); err != nil {
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(buf, &req); err != nil {
			return
		}
		params := map[string]any{}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				params = map[string]any{}
			}
		}
		r.mu.Lock()
		r.calls = append(r.calls, gateApprovalCall{Method: req.Method, Params: params})
		seeded, hasSeeded := r.results[req.Method]
		r.mu.Unlock()

		var result any = map[string]any{}
		switch {
		case hasSeeded:
			result = seeded
		case req.Method == "task.approve":
			id, _ := params["task_id"].(string)
			result = map[string]any{"task_id": id, "status": "approved"}
		case req.Method == "task.reject":
			id, _ := params["task_id"].(string)
			result = map[string]any{"task_id": id, "status": "rejected"}
		}
		resp, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if err != nil {
			return
		}
		fmt.Fprintf(conn, "%d\n", len(resp))
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

func (r *gateApprovalRecorder) callsFor(method string) []gateApprovalCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []gateApprovalCall
	for _, c := range r.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (r *gateApprovalRecorder) methodNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.Method)
	}
	return out
}

// gateApprovalWorld boots a headless TUI App against the recording RPC
// server and waits for the socket connect (same barrier newHeadlessApp
// uses). The recorder is the C2 wire-assertion surface.
func gateApprovalWorld(t *testing.T) (*headlessProgram, *gateApprovalRecorder) {
	t.Helper()
	rec := newGateApprovalRecorder(t)
	hp := newHeadlessAppSocket(t, rec.socketPath(), 100, 30)
	// IsConnected is an atomic bool - safe to poll while the loop runs
	// (same barrier newHeadlessApp uses).
	deadline := time.Now().Add(5 * time.Second)
	for !hp.app.rpc.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	hp.settle(100, 30)
	return hp, rec
}

// gatePendingApprovalEvent builds the exact payload shape
// StrategicPlanner.awaitUserApproval emits (strategic.go): task_id,
// session_id, steps summaries, total.
func gatePendingApprovalEvent(taskID, sessionID string, total int) BusEvent {
	steps := make([]map[string]string, 0, total)
	for i := 0; i < total; i++ {
		steps = append(steps, map[string]string{
			"sequence":    fmt.Sprintf("%d", i),
			"description": fmt.Sprintf("gate step %d", i),
			"tool_hint":   "code",
		})
	}
	return BusEvent{
		Topic: "task.pending_approval",
		Type:  "event",
		Payload: map[string]any{
			"task_id":    taskID,
			"session_id": sessionID,
			"steps":      steps,
			"total":      float64(total),
		},
	}
}

// driveGateSlash runs the REAL slash command path (ParseSlash ->
// CommandHandler.Execute -> executeSync -> RPC) on the test goroutine
// (RPC I/O only, no model mutation) and delivers the resulting
// CommandResultMsg into the App loop in order. This exercises the
// command implementation the card's hint advertises without needing the
// App's Enter interception (commandHandler is nil in the headless
// constructor; NewApp wires it). The wire assertion is identical to the
// card-key path the 01 leaf lands.
func driveGateSlash(hp *headlessProgram, input string) {
	cmd := NewCommandHandler(hp.app.rpc).Execute(ParseSlash(input))
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		hp.send(msg)
	}
}

func gateTailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// TestPendingApprovalCardRendersInOwningSession pins F13a: a
// task.pending_approval event for the ACTIVE session renders an approval
// card in the chat transcript carrying the task id, the step count, and
// approve/reject affordances. RED today: the App event switch has no
// task.pending_approval case (orphan topic) - the event is dropped, the
// transcript never shows a card.
func TestPendingApprovalCardRendersInOwningSession(t *testing.T) {
	hp, _ := gateApprovalWorld(t)

	const sessID = "sess-gate-card"
	const taskID = "task-gate-card-01"
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "gate card", ConversationID: "conv-gate-card"}})
	hp.settle(100, 30)

	hp.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent(taskID, sessID, 6)}})
	hp.settle(100, 30)

	app := hp.finish(100, 30)
	view := app.View().Content
	if !strings.Contains(view, taskID) {
		t.Fatalf("F13a: no approval card for task.pending_approval (task %s, 6 steps) in the owning session's chat; the topic is an orphan in the App event switch.\ntranscript tail:\n%s", taskID, gateTailLines(view, 25))
	}
	lower := strings.ToLower(view)
	if !strings.Contains(lower, "approve") || !strings.Contains(lower, "reject") {
		t.Fatalf("F13a: approval card lacks approve/reject affordances:\n%s", gateTailLines(view, 25))
	}
	if !strings.Contains(view, "6") {
		t.Fatalf("F13a: approval card does not show the step count (6):\n%s", gateTailLines(view, 25))
	}
}

// TestPendingApprovalCardSessionScoped pins the review checklist: the
// card renders ONLY in the owning session's chat - a pending_approval
// for another session must not flood the active transcript. The positive
// control (owning session renders) fails RED today for the orphan
// reason, proving the negative is not vacuous.
func TestPendingApprovalCardSessionScoped(t *testing.T) {
	hp, _ := gateApprovalWorld(t)

	hp.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-mine", Name: "mine", ConversationID: "conv-mine"}})
	hp.settle(100, 30)

	// Foreign-session event must NOT render into the active chat.
	hp.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent("task-other-session", "sess-other", 7)}})
	hp.settle(100, 30)
	app := hp.finish(100, 30)
	if strings.Contains(app.View().Content, "task-other-session") {
		t.Fatalf("F13a: cross-session approval card flooded the active chat (card must be scoped to the owning session):\n%s", gateTailLines(app.View().Content, 25))
	}

	// Positive control on a fresh world: the owning session's event DOES
	// render (RED today - orphan topic).
	hp2, _ := gateApprovalWorld(t)
	hp2.send(SessionLoadedMsg{Session: &types.Session{ID: "sess-mine", Name: "mine", ConversationID: "conv-mine"}})
	hp2.settle(100, 30)
	hp2.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent("task-mine-session", "sess-mine", 7)}})
	hp2.settle(100, 30)
	app2 := hp2.finish(100, 30)
	if !strings.Contains(app2.View().Content, "task-mine-session") {
		t.Fatalf("F13a: owning-session approval card never rendered (orphan task.pending_approval):\n%s", gateTailLines(app2.View().Content, 25))
	}
}

// TestRejectFromCardNeverExecutes is the LOAD-BEARING C2 pin: rejecting
// from the approval surface calls task.reject{task_id, reason} with the
// reason forwarded verbatim, and NEVER calls task.approve (reject must
// not execute). The handler-side reject-approves bug is owned by
// 01-safety/01; this pins the TUI wiring side of C2. RED today: no card
// exists and /plan ignores its args (session.set_mode), so task.reject
// is never called.
func TestRejectFromCardNeverExecutes(t *testing.T) {
	hp, rec := gateApprovalWorld(t)

	const sessID = "sess-gate-reject"
	const taskID = "task-gate-reject-01"
	const reason = "too risky to run unattended"
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "gate reject", ConversationID: "conv-gate-reject"}})
	hp.settle(100, 30)

	hp.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent(taskID, sessID, 6)}})
	hp.settle(100, 30)

	driveGateSlash(hp, "/plan reject "+taskID+" "+reason)
	hp.settle(100, 30)

	app := hp.finish(100, 30)
	view := app.View().Content
	if !strings.Contains(view, taskID) {
		t.Fatalf("C2: approval card never rendered, reject-from-card unreachable (orphan task.pending_approval):\n%s", gateTailLines(view, 25))
	}
	rejects := rec.callsFor("task.reject")
	if len(rejects) == 0 {
		t.Fatalf("C2: reject from the approval surface never called task.reject (recorded methods: %v)\ntranscript tail:\n%s", rec.methodNames(), gateTailLines(view, 25))
	}
	got := rejects[0]
	if id, _ := got.Params["task_id"].(string); id != taskID {
		t.Errorf("C2: task.reject task_id = %v, want %q", got.Params["task_id"], taskID)
	}
	if r, _ := got.Params["reason"].(string); r != reason {
		t.Errorf("C2: task.reject reason = %v, want the full reason %q forwarded", got.Params["reason"], reason)
	}
	if approves := rec.callsFor("task.approve"); len(approves) != 0 {
		t.Errorf("C2 LOAD-BEARING: reject called task.approve %d time(s) - reject must NEVER execute: %+v", len(approves), approves)
	}
}

// TestApproveFromCardCallsApproveRPC pins the approve half of C2: the
// approve affordance calls task.approve{task_id} exactly once for the
// card's task. RED today: orphan card + dead /plan hint.
func TestApproveFromCardCallsApproveRPC(t *testing.T) {
	hp, rec := gateApprovalWorld(t)

	const sessID = "sess-gate-approve"
	const taskID = "task-gate-approve-01"
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "gate approve", ConversationID: "conv-gate-approve"}})
	hp.settle(100, 30)

	hp.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent(taskID, sessID, 6)}})
	hp.settle(100, 30)

	driveGateSlash(hp, "/plan approve "+taskID)
	hp.settle(100, 30)

	app := hp.finish(100, 30)
	view := app.View().Content
	if !strings.Contains(view, taskID) {
		t.Fatalf("C2: approval card never rendered, approve-from-card unreachable (orphan task.pending_approval):\n%s", gateTailLines(view, 25))
	}
	approves := rec.callsFor("task.approve")
	if len(approves) != 1 {
		t.Fatalf("C2: task.approve calls = %d, want exactly 1 (recorded methods: %v)\ntranscript tail:\n%s", len(approves), rec.methodNames(), gateTailLines(view, 25))
	}
	if id, _ := approves[0].Params["task_id"].(string); id != taskID {
		t.Errorf("C2: task.approve task_id = %v, want %q", approves[0].Params["task_id"], taskID)
	}
}

// TestPlansViewTaskGate pins 06-planning/01 T3: the plans view shows
// task-gate awaiting tasks, not just plans.db rows. Today
// plan.list_by_session returns 0 rows for a task-gate plan (the gate
// lives in tasks.db, not the plan store), so the view is empty. The gate
// publishes the pending_approval event for the session, opens the plans
// view, and asserts the plans TABLE carries a row (header count (1/1))
// with an awaiting-approval state - the sidebar task list is NOT the
// plans view and must not satisfy this pin. RED today: fetchPlans only
// reads plan.list_by_session, which has no task-gate rows.
func TestPlansViewTaskGate(t *testing.T) {
	hp, _ := gateApprovalWorld(t)

	const sessID = "sess-gate-plansview"
	const taskID = "task-gate-plansview-01"
	hp.send(SessionLoadedMsg{Session: &types.Session{ID: sessID, Name: "gate plans", ConversationID: "conv-gate-plansview"}})
	hp.settle(100, 30)

	hp.send(EventStreamDataMsg{Events: []BusEvent{gatePendingApprovalEvent(taskID, sessID, 6)}})
	hp.settle(100, 30)

	switchView(t, hp, "p")
	settleAsync()
	app := hp.finish(100, 30)
	if app.ActiveView() != ViewPlans {
		t.Fatalf("F13a (01 T3): plans view did not open: view=%v", app.ActiveView())
	}
	plansView := app.plans.View()
	// The table header count proves a store-backed row reached the plans
	// model (same seam TestPlansViewRendersCreatedPlan pins e2e-side).
	if !strings.Contains(plansView, "(1/1)") {
		t.Fatalf("F13a (01 T3): plans view shows no task-gate row for awaiting task %s - plan.list_by_session returns 0 rows for the task gate and the view does not merge awaiting_approval tasks (header count (1/1) absent):\n%s", taskID, gateTailLines(plansView, 30))
	}
	if !strings.Contains(plansView, "awaiting") && !strings.Contains(plansView, "approval") {
		t.Fatalf("F13a (01 T3): task-gate row lacks an awaiting-approval state affordance:\n%s", gateTailLines(plansView, 30))
	}
}
