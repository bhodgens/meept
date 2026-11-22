//go:build e2e

// 06-planning TEST-GATE T5 (F13b/F13c): plan-failure delivery (C6) and the
// compiler draft/seal surface at the daemon boundary, extending the
// plan-lifecycle suite.
//
// RED today for missing-implementation reasons:
//   - failPlan (manager.go) publishes NO plan.failed event; a plan whose
//     phase failed terminalizes in the store but no client learns of it
//     (C6 requires plan.failed {plan_id, session_id, reason} via
//     PublishBlockingT). The bus surface has zero plan.failed publishers.
//   - the compiler draft/seal RPCs (plan.draft/plan.seal) exist but the
//     draft has no chat-visible status and the HTTP twins are absent; a
//     raw scaffold parks forever (P1). The seal path returns compile
//     problems inline (status:"problems") - pinned here so the GUI seal
//     surface (06-planning/02) has a stable daemon contract.
//
// Self-contained: helpers are gate-prefixed. GREEN after 06-planning/02+03
// and 01-safety/03 (C6).

package planlifecycle

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// gateLifecycleStack boots a daemon whose plans always require approval.
func gateLifecycleStack(t *testing.T, extra map[string]any) *harness.Stack {
	t.Helper()
	overlay := map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	}
	for k, v := range extra {
		overlay[k] = v
	}
	return harness.Start(t, harness.WithConfigOverlay(overlay))
}

// gateSubscribe subscribes to topics on a persistent connection and returns
// the subscription id plus a poll closure (subscriptions die with their
// connection, so the caller keeps the returned session alive).
func gateSubscribe(t *testing.T, s *harness.Stack, topics ...string) (string, func() []map[string]any) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	sub := rpc.CallResult("bus.subscribe", map[string]any{"topics": topics})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}
	poll := func() []map[string]any {
		resp := rpc.CallResult("bus.poll", map[string]any{"subscription_id": subID})
		raw, _ := resp["events"].([]any)
		var out []map[string]any
		for _, e := range raw {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return subID, poll
}

// TestPlanFailedEventDelivered pins C6: a plan whose phase failed publishes
// plan.failed {plan_id, session_id, reason} so clients learn of the failure
// directly (not only via the watchdog). RED today: failPlan publishes no
// event. Drives a real dispatch-path plan to a failed phase through the
// fake-LLM doomed-write rig (the async-turn-03 pattern).
func TestPlanFailedEventDelivered(t *testing.T) {
	s := gateLifecycleStack(t, nil)
	sessionID := s.CreateSession(t, "plan-failed", s.ProjectDir)

	_, poll := gateSubscribe(t, s, "plan.failed", "plan.phase_state")

	// A plan-mode request creates a pending_approval plan; approve it, then
	// drive its synthesized task to a failed step via a doomed file write.
	s.ChatTurn(t, sessionID, "help me rework the storage layer end to end", 90*time.Second)
	filePath := waitForPlanFile(t, s, 30*time.Second)
	planID := requireParses(t, filePath, "").PlanID
	waitForPlanState(t, s, planID, "pending_approval", 30*time.Second)

	// Doom the synthesized task's step: a file write under a blocker FILE
	// fails at the tool on every retry, exhausting the breaker -> failed step.
	blocker := s.ProjectDir + "/gate-blocker"
	if err := os.WriteFile(blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	doomed := blocker + "/forbidden.txt"
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"GATEFAIL: create the forbidden file","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.ScriptN(8,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()), harness.MessageContains("GATEFAIL")),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: `{"path":"` + doomed + `","content":"nope","direct":true}`}))
	s.Fake.SetPostToolText("The requested work could not be completed.")

	rpc(t, s, "plan.approve", map[string]any{"plan_id": planID, "by": "e2e"})
	waitForPlanState(t, s, planID, "executing", 30*time.Second)

	// Wait for the plan to terminalize failed in the store, then assert the
	// plan.failed event was delivered (C6). The store flip is GREEN today;
	// the EVENT is the RED gap.
	deadline := time.Now().Add(120 * time.Second)
	var storeFailed bool
	for time.Now().Before(deadline) && !storeFailed {
		if str(rpc(t, s, "plan.get", map[string]any{"id": planID}), "state") == "failed" {
			storeFailed = true
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !storeFailed {
		t.Fatalf("C6 fixture: plan %s never reached failed state (doomed-step rig did not fire);\ndaemon log tail:\n%s", planID, s.Daemon.LogTail())
	}

	var sawFailed bool
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawFailed {
		for _, ev := range poll() {
			if topic, _ := ev["topic"].(string); topic != "plan.failed" {
				continue
			}
			payload, _ := ev["payload"].(map[string]any)
			if pid, _ := payload["plan_id"].(string); pid == planID {
				sawFailed = true
				if _, ok := payload["reason"]; !ok {
					t.Errorf("C6: plan.failed payload missing reason: %v", payload)
				}
				if _, ok := payload["session_id"]; !ok {
					t.Errorf("C6: plan.failed payload missing session_id: %v", payload)
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !sawFailed {
		t.Fatalf("C6: plan %s failed in the store but NO plan.failed event was delivered (failPlan publishes nothing); clients cannot learn of the failure", planID)
	}
}

// TestCompilerDraftSealSurface pins the compiler draft/seal daemon contract
// the GUI seal surface consumes: with plan_compiler_enabled, a plan-mode
// request seeds a draft (plan.draft get returns markdown), and plan.seal on
// the raw scaffold returns compile problems inline (status:"problems") -
// never auto-sealing. RED today: the draft seeds but there is no chat-visible
// status event; the seal RPC itself works, so this pins the contract the
// surface drives (the problems-inline result is the load-bearing shape).
func TestCompilerDraftSealSurface(t *testing.T) {
	s := gateLifecycleStack(t, map[string]any{
		"plans.plan_compiler_enabled": true,
	})
	sessionID := s.CreateSession(t, "compiler-seal", s.ProjectDir)

	// A plan-mode request under the compiler flag seeds a draft and parks.
	s.ChatTurn(t, sessionID, "plan the storage rework as a brainstorm draft", 90*time.Second)

	// Find the dispatched task (the draft lives in its metadata).
	var taskID string
	harness.WaitFor(t, 30*time.Second, "compiler draft task row", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.State == "planning" || row.State == "awaiting_approval" {
				taskID = row.ID
				return true
			}
		}
		return false
	})
	if taskID == "" {
		t.Fatalf("F13b: no compiler draft task appeared under plan_compiler_enabled")
	}

	// plan.draft get returns the seeded scaffold markdown.
	draft := rpc(t, s, "plan.draft", map[string]any{"task_id": taskID})
	md, _ := draft["markdown"].(string)
	if md == "" {
		t.Fatalf("F13b: plan.draft get returned no markdown for task %s: %v", taskID, draft)
	}
	if !strings.Contains(md, "## Phases") {
		t.Errorf("F13b: seeded draft is not the plan-dialect scaffold (no ## Phases): %q", md)
	}

	// plan.seal on the raw scaffold (empty Decisions/Phases) must return
	// compile problems inline, NOT seal. The scaffold has no phases, so the
	// compiler refuses (problems or an honest error) - never status:sealed.
	sealRaw := harness.DialRPC(t, s.SocketPath).Call("plan.seal", map[string]any{"task_id": taskID})
	if errObj, ok := sealRaw["error"]; ok && errObj != nil {
		// An honest compile error is acceptable (the scaffold cannot compile);
		// the pin is that it did NOT seal.
		t.Logf("F13b: raw scaffold seal refused with error (acceptable): %v", errObj)
	} else {
		result, _ := sealRaw["result"].(map[string]any)
		status, _ := result["status"].(string)
		if status == "sealed" {
			t.Fatalf("F13b LOAD-BEARING: raw scaffold auto-sealed (status:sealed); sealing requires a compileable draft + explicit human action")
		}
		if status == "problems" {
			problems, _ := result["problems"].([]any)
			if len(problems) == 0 {
				t.Errorf("F13b: status:problems but no problems array (GUI renders them inline): %v", result)
			}
			for _, p := range problems {
				pm, _ := p.(map[string]any)
				if _, ok := pm["line"]; !ok {
					t.Errorf("F13b: compile problem missing line (inline render needs line+message): %v", pm)
				}
				if _, ok := pm["message"]; !ok {
					t.Errorf("F13b: compile problem missing message: %v", pm)
				}
			}
		}
	}

	// The draft must still be a draft (not sealed) after the refused seal.
	draft2 := rpc(t, s, "plan.draft", map[string]any{"task_id": taskID})
	if status, _ := draft2["status"].(string); status == "sealed" {
		t.Fatalf("F13b LOAD-BEARING: draft became sealed after a refused seal attempt")
	}
}
