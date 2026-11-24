//go:build e2e

// Package taskapproval is the 01-safety TEST-GATE T10 e2e suite for the
// C2 task approval surface (W1; master matrix F1, scenarios
// task-approval-01..02).
//
// Boots-leg tests through the full hermetic stack: a gated plan parks at
// awaiting_approval, then task.reject must REJECT (never execute) and
// relay an honest failed turn.terminal under the originating turn id.
//
// RED today: handleReject calls ApproveFunc (internal/rpc/task_approval.go),
// so a rejection EXECUTES the plan; no task.rejected -> turn.terminal relay
// exists; the C2 HTTP twins are unrouted (404).

package taskapproval

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// gatedStack boots a daemon whose plans always require approval, so the
// scripted 6-step plan trips the strategic approval gate (default
// threshold 5).
func gatedStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	}))
}

// scriptSixStepPlan scripts the fake planner with a 6-step plan so the
// dispatched task parks at awaiting_approval (same shape as the committed
// tui-flows approval gate fixture).
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

// waitGatedTask polls tasks.db until the marker task parks at
// awaiting_approval and returns its id.
func waitGatedTask(t *testing.T, s *harness.Stack, marker string) string {
	t.Helper()
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
	return taskID
}

// taskStateNow reads the current state of one task row ("" when absent).
func taskStateNow(t *testing.T, s *harness.Stack, taskID string) string {
	t.Helper()
	for _, row := range harness.Tasks(t, s.TasksDBPath()) {
		if row.ID == taskID {
			return row.State
		}
	}
	return ""
}

// terminalEvent mirrors the frozen C1 turn.terminal payload fields this
// suite asserts on.
type terminalEvent struct {
	TurnID          string `json:"turn_id"`
	TaskID          string `json:"task_id"`
	HandlerCase     string `json:"handler_case"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	RejectionReason string `json:"rejection_reason"`
}

// pollTerminals drains one bus.poll page into terminal events.
func pollTerminals(t *testing.T, client *harness.RPCClient, subID string) []terminalEvent {
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

// TestRejectDoesNotExecutePlan is the W1 load-bearing e2e pin: a gated
// plan rejected through task.reject must NOT execute, and the originating
// turn must receive turn.terminal status=failed
// handler_case=task_rejected_relay with the rejection reason (C1/C2).
// RED today: reject approves (task moves to executing) and no relay fires.
func TestRejectDoesNotExecutePlan(t *testing.T) {
	s := gatedStack(t)
	s.RegisterProject(t, "approval-project")
	sessionID := s.CreateSession(t, "reject-honesty", s.ProjectDir)
	const marker = "REJGATE"
	scriptSixStepPlan(s, marker)

	client := harness.DialRPC(t, s.SocketPath)
	sub := client.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}

	ack := s.SubmitChatHTTP(t, sessionID, marker+": plan and execute the multi-step rework")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}
	taskID := waitGatedTask(t, s, marker)

	const reason = "e2e rejection: plan touches production, do not execute"
	res := client.CallResult("task.reject", map[string]any{
		"task_id": taskID,
		"reason":  reason,
	})
	if status, _ := res["status"].(string); status != "rejected" {
		t.Fatalf("task.reject status = %v, want rejected (C2): %v", res["status"], res)
	}

	// The rejected plan must never execute. Today handleReject calls
	// ApproveFunc, which schedules the pending steps immediately.
	time.Sleep(3 * time.Second)
	if state := taskStateNow(t, s, taskID); state == "executing" || state == "completed" {
		t.Errorf("CURRENT DEFECT (W1): task.reject left task %s in state %q - the rejection APPROVED and executed the plan (handleReject calls ApproveFunc)", taskID, state)
	}

	// The originating turn must get an honest failed terminal relay.
	deadline := time.Now().Add(15 * time.Second)
	var relay *terminalEvent
	for time.Now().Before(deadline) {
		for _, te := range pollTerminals(t, client, subID) {
			if te.TurnID == turnID && te.Status == "failed" {
				copy := te
				relay = &copy
			}
		}
		if relay != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if relay == nil {
		t.Fatalf("CURRENT DEFECT: no failed turn.terminal for rejected turn %s (task %s): C2 requires the task.rejected relay under the ORIGINATING turn id", turnID, taskID)
	}
	if relay.HandlerCase != "task_rejected_relay" {
		t.Errorf("relay handler_case = %q, want task_rejected_relay (C1/C2)", relay.HandlerCase)
	}
	if !strings.Contains(relay.Error, reason) && !strings.Contains(relay.RejectionReason, reason) {
		t.Errorf("relay carries neither error nor rejection_reason quoting %q (C1 additive key): %+v", reason, relay)
	}
}

// TestTaskApprovalHTTPTwins pins the C2 HTTP surface: POST
// /api/v1/tasks/{id}/approve and /reject must be ROUTED (an unknown task
// may error, but the route itself must exist). RED today: both 404.
func TestTaskApprovalHTTPTwins(t *testing.T) {
	s := harness.Start(t)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
	for _, verb := range []string{"approve", "reject"} {
		body := []byte(`{"reason":"e2e http twin probe"}`)
		resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/tasks/task-nope/"+verb, "application/json", bytes.NewReader(body)) //nolint:noctx // bounded gate call
		if err != nil {
			t.Fatalf("POST tasks/%s: %v", verb, err)
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read tasks/%s body: %v", verb, readErr)
		}
		// The route must EXIST. mux answers a truly unrouted path with a
		// bodyless 404/405; the handler's honest refusal for an unknown
		// task carries "task not found" in the body (error mapping in
		// internal/comm/http/task_approval.go). A handler-shaped 404
		// therefore PROVES routing; a bodyless 404 proves it absent.
		routed := strings.Contains(string(respBody), "task not found") ||
			(resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed)
		if !routed {
			t.Errorf("CURRENT DEFECT: POST /api/v1/tasks/{id}/%s answered %d - the C2 HTTP twin is unrouted: %s", verb, resp.StatusCode, string(respBody))
		}
	}
}
