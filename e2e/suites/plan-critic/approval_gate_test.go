//go:build e2e

// 06-planning TEST-GATE T5 (F13a): approval-gate scenarios at the daemon
// boundary, extending the plan-critic suite.
//
// RED today for missing-implementation reasons:
//   - orchestrator.task_approval_step_threshold is not a config key
//     (config get errors "field not found"); the threshold is hardcoded
//     and clamped <=0 -> 5 (strategic.go), so 0 cannot disable the gate.
//   - task.reject APPROVES instead of rejecting (task_approval.go
//     handleReject calls ApproveFunc) and there is no task.rejected ->
//     turn.terminal relay; a rejected plan still executes (C2 violation).
//   - the TUI/GUI never surface task.pending_approval (orphan topic), so
//     the gate is invisible end to end.
//
// These scenarios drive the REAL hermetic daemon: a >=threshold-step plan
// parks at awaiting_approval, approve executes, reject terminalizes
// WITHOUT execution (load-bearing C2), and threshold=0 disables the gate.
//
// Self-contained: helpers are gate-prefixed so this file compiles alone.
// GREEN after 06-planning/01 + 01-safety/01 (C2).

package plancritic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// gateApprovalRPC is one persistent framed JSON-RPC connection (bus
// subscriptions die with their connection, so subscribe+poll share it).
type gateApprovalRPC struct {
	conn   net.Conn
	reader *bufio.Reader
}

func gateDialApproval(t *testing.T, s *harness.Stack) *gateApprovalRPC {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial unix %s: %v", s.SocketPath, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &gateApprovalRPC{conn: conn, reader: bufio.NewReader(conn)}
}

func (c *gateApprovalRPC) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": time.Now().UnixNano(), "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal %s: %v", method, err)
	}
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write frame length: %v", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	line, err := c.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read frame length: %v", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("invalid frame length %q: %v", line, err)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.reader, buf); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}
	if resp.Error != nil {
		t.Fatalf("%s rpc error: %s", method, resp.Error.Message)
	}
	return resp.Result
}

// gateConfigGet runs `meept config get <key>` and returns combined output
// plus whether the command succeeded (a missing key errors today).
func gateConfigGet(t *testing.T, s *harness.Stack, key string) (string, bool) {
	t.Helper()
	stdout, stderr := s.RunCLI(t, 30*time.Second, true, "config", "get", key)
	out := stdout + stderr
	ok := !strings.Contains(strings.ToLower(out), "not found") &&
		!strings.Contains(strings.ToLower(out), "error") &&
		strings.TrimSpace(stdout) != ""
	return out, ok
}

// gatePlanStack boots a daemon whose plans always require approval and
// whose strategic approval threshold is low enough that a scripted
// multi-step plan trips the gate.
func gatePlanStack(t *testing.T, threshold int) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                                "always",
		"plans.approval.require_approval":           true,
		"orchestrator.task_approval_step_threshold": threshold,
	}))
}

// gateSubmitPlanTurn submits a work request that decomposes into >=5 steps
// (the fake planner is scripted with a 6-step plan) and returns the turn id
// plus the dispatched task id once it appears.
func gateSubmitPlanTurn(t *testing.T, s *harness.Stack, sessionID, marker string) (string, string) {
	t.Helper()
	steps := make([]map[string]any, 0, 6)
	for i := 0; i < 6; i++ {
		steps = append(steps, map[string]any{
			"description": fmt.Sprintf("%s step %d: do the work", marker, i),
			"tool_hint":   "analyze",
			"depends_on":  []int{},
		})
	}
	planJSON, _ := json.Marshal(map[string]any{"steps": steps})
	s.Fake.SetPlannerResponse(string(planJSON))
	s.Fake.SetPostToolText("The requested work is complete.")

	ack := s.SubmitChatHTTP(t, sessionID, marker+": plan and execute the multi-step rework")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("gate: ack missing turn_id: %+v", ack)
	}
	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for the planning turn", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if strings.Contains(row.Name, marker) {
				taskID = row.ID
				return true
			}
		}
		return false
	})
	return turnID, taskID
}

// gateWaitTaskState polls tasks.db until the task reaches the given state.
func gateWaitTaskState(t *testing.T, s *harness.Stack, taskID, state string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID && row.State == state {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// TestApprovalGateThresholdConfig pins F13a threshold config at the live
// config surface: orchestrator.task_approval_step_threshold is readable,
// defaults to 5, and accepts 0 (disable). RED today: the key does not
// exist (config get errors).
func TestApprovalGateThresholdConfig(t *testing.T) {
	s := harness.Start(t)
	if !s.HealthOK() {
		t.Fatal("gate: daemon did not boot healthy")
	}
	out, ok := gateConfigGet(t, s, "orchestrator.task_approval_step_threshold")
	if !ok {
		t.Fatalf("F13a: orchestrator.task_approval_step_threshold is not a config key (config get output: %q); the approval threshold must be operator-configurable (default 5, 0 disables)", out)
	}
	if !strings.Contains(out, "5") {
		t.Errorf("F13a: default task_approval_step_threshold = %q, want the preserved default 5", out)
	}

	// Explicit 0 must round-trip (disable), not be rejected or clamped.
	s0 := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"orchestrator.task_approval_step_threshold": 0,
	}))
	if !s0.HealthOK() {
		t.Fatal("gate: daemon with threshold=0 did not boot healthy")
	}
	out0, ok0 := gateConfigGet(t, s0, "orchestrator.task_approval_step_threshold")
	if !ok0 || !strings.Contains(out0, "0") {
		t.Fatalf("F13a: threshold=0 overlay did not round-trip (output %q); 0 must disable the gate, not clamp to 5", out0)
	}
}

// TestApprovalGateParksThenApproveExecutes pins the visible-gate happy
// path: a >=threshold-step plan parks at awaiting_approval (the gate is
// real), and task.approve moves it to executing. RED today: the gate
// parks but no surface shows it; the approve RPC works, so this pins the
// state machine the card drives. The park assertion is RED only if the
// threshold config is absent (the overlay key is ignored today, but the
// default 5 still gates a 6-step plan, so the park holds; the approve
// path is GREEN-ready). The load-bearing RED is the reject scenario.
func TestApprovalGateParksThenApproveExecutes(t *testing.T) {
	s := gatePlanStack(t, 3)
	sessionID := s.CreateSession(t, "gate-approve", s.ProjectDir)
	_, taskID := gateSubmitPlanTurn(t, s, sessionID, "GATEAPPROVE")

	if !gateWaitTaskState(t, s, taskID, "awaiting_approval", 30*time.Second) {
		t.Fatalf("F13a: 6-step plan with threshold 3 did not park at awaiting_approval (task %s); the complexity gate must hold", taskID)
	}

	rpc := gateDialApproval(t, s)
	rpc.call(t, "task.approve", map[string]any{"task_id": taskID})

	if !gateWaitTaskState(t, s, taskID, "executing", 30*time.Second) {
		// executing may already have advanced to completed; accept any
		// post-approval non-awaiting state.
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID && row.State != "awaiting_approval" && row.State != "rejected" {
				return
			}
		}
		t.Fatalf("F13a: task.approve did not resume the gated plan (task %s still awaiting)", taskID)
	}
}

// TestApprovalGateRejectNeverExecutes is the LOAD-BEARING C2 e2e pin: a
// gated plan rejected through task.reject lands in rejected, publishes a
// task.rejected -> turn.terminal failed relay, and NEVER executes (zero
// step jobs run). RED today: handleReject calls ApproveFunc, so the
// rejected plan executes instead.
func TestApprovalGateRejectNeverExecutes(t *testing.T) {
	s := gatePlanStack(t, 3)
	sessionID := s.CreateSession(t, "gate-reject", s.ProjectDir)

	// Subscribe to turn.terminal BEFORE submit (collector ordering).
	rpc := gateDialApproval(t, s)
	subRaw := rpc.call(t, "bus.subscribe", map[string]any{"topics": []string{"turn.terminal", "task.rejected"}})
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack: %s (%v)", subRaw, err)
	}

	turnID, taskID := gateSubmitPlanTurn(t, s, sessionID, "GATEREJECT")
	if !gateWaitTaskState(t, s, taskID, "awaiting_approval", 30*time.Second) {
		t.Fatalf("C2: gated plan did not park at awaiting_approval (task %s)", taskID)
	}

	rejectRaw := rpc.call(t, "task.reject", map[string]any{
		"task_id": taskID,
		"reason":  "too risky to run unattended",
	})
	var rejectRes struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rejectRaw, &rejectRes); err != nil {
		t.Fatalf("task.reject result: %s (%v)", rejectRaw, err)
	}
	if rejectRes.Status != "rejected" {
		t.Fatalf("C2: task.reject status = %q, want rejected", rejectRes.Status)
	}

	// The task must be rejected, NOT executing/completed.
	if !gateWaitTaskState(t, s, taskID, "rejected", 15*time.Second) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID {
				t.Fatalf("C2 LOAD-BEARING: rejected task %s is in state %q, want rejected (reject must never execute)", taskID, row.State)
			}
		}
		t.Fatalf("C2: task %s vanished after reject", taskID)
	}

	// Zero step jobs executed: no step reached a completed/running state.
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		if st.State == "completed" || st.State == "approved" || st.State == "running" {
			t.Fatalf("C2 LOAD-BEARING: rejected plan executed step %s (state %q); reject must never run any step", st.ID, st.State)
		}
	}

	// turn.terminal failed relay under the originating turn id (C2/C6).
	deadline := time.Now().Add(15 * time.Second)
	var sawRelay bool
	for time.Now().Before(deadline) && !sawRelay {
		pollRaw := rpc.call(t, "bus.poll", map[string]any{"subscription_id": sub.SubscriptionID})
		var resp struct {
			Events []struct {
				Topic   string          `json:"topic"`
				Payload json.RawMessage `json:"payload"`
			} `json:"events"`
		}
		if json.Unmarshal(pollRaw, &resp) == nil {
			for _, ev := range resp.Events {
				if ev.Topic != "turn.terminal" {
					continue
				}
				var p struct {
					TurnID      string `json:"turn_id"`
					Status      string `json:"status"`
					HandlerCase string `json:"handler_case"`
					Error       string `json:"error"`
				}
				if json.Unmarshal(ev.Payload, &p) == nil && p.TurnID == turnID && p.Status == "failed" {
					sawRelay = true
					if p.HandlerCase != "task_rejected_relay" {
						t.Errorf("C2: rejected relay handler_case = %q, want task_rejected_relay", p.HandlerCase)
					}
					if !strings.Contains(p.Error, "too risky") {
						t.Errorf("C2: rejected relay error = %q, want the rejection reason forwarded", p.Error)
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !sawRelay {
		t.Fatalf("C2/C6: no turn.terminal failed relay for rejected turn %s (task.rejected must terminalize the turn)", turnID)
	}
}

// TestApprovalGateThresholdZeroDisables pins that threshold=0 truly
// disables the gate end to end: a 6-step plan executes without parking at
// awaiting_approval. RED today: the overlay key is ignored AND the
// planner clamps 0 -> 5, so the plan still parks.
func TestApprovalGateThresholdZeroDisables(t *testing.T) {
	s := gatePlanStack(t, 0)
	sessionID := s.CreateSession(t, "gate-zero", s.ProjectDir)
	_, taskID := gateSubmitPlanTurn(t, s, sessionID, "GATEZERO")

	// With the gate disabled the task must NOT park at awaiting_approval;
	// it proceeds toward execution/completion.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID {
				if row.State == "awaiting_approval" {
					t.Fatalf("F13a: threshold=0 still parked task %s at awaiting_approval; 0 must disable the gate (clamp resurrects it today)", taskID)
				}
				if row.State == "executing" || row.State == "completed" {
					return // gate disabled: plan proceeded.
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("F13a: threshold=0 task %s never proceeded past planning (gate not disabled)", taskID)
}
