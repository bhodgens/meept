//go:build e2e

// Package approvalwait is the 01-safety TEST-GATE T10 e2e suite for the
// C7 watchdog park exemption (W5/W25; master matrix F5, scenarios
// approval-wait-01..02).
//
// Boots-leg test through the real hermetic daemon with a fast watchdog
// (stale_after_seconds=5, interval_seconds=2): a turn whose task parks at
// awaiting_approval must NOT be reaped while it waits for the operator;
// approving it afterwards completes the SAME turn honestly.
//
// RED today: the watchdog reaps staleness-tracked turns regardless of
// task state, so the awaiting_approval turn is failed with
// handler_case=turn_reaped before any operator action.

package approvalwait

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// waitStack boots the daemon with the approval gate on and a fast
// liveness reaper so the exemption window is observable in seconds.
func waitStack(t *testing.T) *harness.Stack {
	t.Helper()
	// The approval pin lives on the STRATEGIC task gate
	// (orchestrator.task_approval_step_threshold, default 5): a 6-step
	// plan must park the TASK row at awaiting_approval. plans.mode must
	// stay OFF for this pin: the dispatcher's plan-manager lane (5.5)
	// intercepts planning intents whenever mode is "always"/threshold and
	// synthesizes a docs plan instead of dispatching the strategic
	// planner, so the scripted 6-step planner response and the task-level
	// gate are never reached (with mode "always" the plan completes with
	// zero phases - observed 2026-11-24).
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                                     "off",
		"plans.approval.require_approval":                false,
		"orchestrator.turn_watchdog.enabled":             true,
		"orchestrator.turn_watchdog.stale_after_seconds": 5,
		"orchestrator.turn_watchdog.interval_seconds":    2,
	}))
}

// scriptSixStepPlan scripts a 6-step plan so the dispatched task trips
// the strategic approval gate (default threshold 5).
func scriptSixStepPlan(s *harness.Stack, marker string) {
	steps := `{"steps":[`
	for i := 0; i < 6; i++ {
		if i > 0 {
			steps += ","
		}
		steps += `{"description":"` + marker + ` step ` + string(rune('0'+i)) + `: do the work","tool_hint":"analyze","depends_on":[]}`
	}
	steps += `]}`
	s.Fake.SetPlannerResponse(steps)
	s.Fake.SetPostToolText("The requested work is complete.")
}

// terminalEvent mirrors the C1 fields this suite asserts on.
type terminalEvent struct {
	TurnID      string `json:"turn_id"`
	TaskID      string `json:"task_id"`
	HandlerCase string `json:"handler_case"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

// collectTerminals drains every buffered turn.terminal event so far.
func collectTerminals(t *testing.T, client *harness.RPCClient, subID string) []terminalEvent {
	t.Helper()
	raw := client.CallResult("bus.poll", map[string]any{"subscription_id": subID})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal poll result: %v", err)
	}
	var page struct {
		Events []struct {
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("decode poll page: %v", err)
	}
	var out []terminalEvent
	for _, ev := range page.Events {
		if ev.Topic != "turn.terminal" {
			continue
		}
		var te terminalEvent
		if json.Unmarshal(ev.Payload, &te) == nil {
			out = append(out, te)
		}
	}
	return out
}

// TestApprovalWaitNotReaped pins C7 end to end: the awaiting_approval
// turn survives several watchdog passes (>= 3x stale_after), then
// task.approve completes it under the SAME turn id. RED today: the
// reaper fails the waiting turn (turn_reaped) inside the window.
func TestApprovalWaitNotReaped(t *testing.T) {
	s := waitStack(t)
	s.RegisterProject(t, "wait-project")
	sessionID := s.CreateSession(t, "approval-wait", s.ProjectDir)
	const marker = "WAITGATE"
	scriptSixStepPlan(s, marker)

	client := harness.DialRPC(t, s.SocketPath)
	sub := client.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}

	// The prompt must stay COMPOUND (plan + work halves) so the dispatcher
	// creates a task whose approval gate the pin tests; a plain plan
	// prompt now routes to the plan-manager docs lane (no task row).
	// The "don't implement it" clause vetoes the intent auto-seal
	// (PlanAutoSealFromIntent, usability residual 07), so the compiled
	// plan parks at awaiting_approval instead of sealing through.
	ack := s.SubmitChatHTTP(t, sessionID, marker+": plan the multi-step rework, don't implement it")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}

	var taskID string
	harness.WaitFor(t, 60*time.Second, "gated task awaiting_approval", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if strings.Contains(row.Name, marker) && row.State == "awaiting_approval" {
				taskID = row.ID
				return true
			}
		}
		return false
	})

	// Hold the approval wait across >= 3 stale windows (15s > 3x5s).
	// Any failed terminal for the turn inside this window is the defect.
	deadline := time.Now().Add(16 * time.Second)
	for time.Now().Before(deadline) {
		for _, te := range collectTerminals(t, client, subID) {
			if te.TurnID == turnID && te.Status == "failed" {
				t.Fatalf("CURRENT DEFECT (C7): awaiting_approval turn %s was reaped while waiting for approval (handler_case=%s error=%q); planning/awaiting_approval/awaiting_seal turns are EXEMPT from staleness reaping",
					turnID, te.HandlerCase, te.Error)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// The task must still be parked, not failed by the reaper.
	state := ""
	for _, row := range harness.Tasks(t, s.TasksDBPath()) {
		if row.ID == taskID {
			state = row.State
		}
	}
	if state != "awaiting_approval" {
		t.Fatalf("task %s state after the wait window = %q, want awaiting_approval (the reaper must not touch the parked plan)", taskID, state)
	}

	// Approve: the SAME turn completes honestly (no fresh-turn re-relay).
	client.CallResult("task.approve", map[string]any{"task_id": taskID})
	deadline = time.Now().Add(120 * time.Second)
	var done *terminalEvent
	for time.Now().Before(deadline) {
		for _, te := range collectTerminals(t, client, subID) {
			if te.TurnID == turnID && te.Status != "parked" {
				copy := te
				done = &copy
			}
		}
		if done != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if done == nil {
		t.Fatalf("no terminal for approved turn %s within 120s; the approval path must complete the originating turn", turnID)
	}
	if done.Status != "completed" {
		t.Errorf("approved turn terminal status = %q (error=%q), want completed", done.Status, done.Error)
	}
}

// TestWatchdogStillReapsNonExemptTurn is the control arm: with the same
// fast reaper config, an ordinary stalled turn (no approval gate) is
// still honestly failed - the exemption must not disarm the reaper.
// This arm is expected GREEN today (the reaper works); it guards against
// an over-broad exemption fix.
func TestWatchdogStillReapsNonExemptTurn(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"orchestrator.turn_watchdog.enabled":             true,
		"orchestrator.turn_watchdog.stale_after_seconds": 5,
		"orchestrator.turn_watchdog.interval_seconds":    2,
	}))
	s.RegisterProject(t, "wait-project")
	sessionID := s.CreateSession(t, "reap-control", s.ProjectDir)

	// Stall the executor lane: every executor request answers HTTP 500,
	// so the turn makes no progress and the liveness reaper owns it.
	s.Fake.Script(harness.IsExecutorRequest(), harness.HTTPStatusResponse(500, `{"error":"e2e stall injection"}`))

	client := harness.DialRPC(t, s.SocketPath)
	sub := client.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}

	ack := s.SubmitChatHTTP(t, sessionID, "create a file named stalled.txt containing stall probe")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		for _, te := range collectTerminals(t, client, subID) {
			if te.TurnID == turnID && te.Status == "failed" {
				return // honest failed terminal: the reaper (or error relay) owns the stall
			}
			if te.TurnID == turnID && te.Status == "completed" {
				t.Fatalf("stalled turn completed despite the executor fault injection: %+v", te)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("stalled turn %s never reached a failed terminal within 120s; the watchdog must still reap NON-exempt turns", turnID)
}
