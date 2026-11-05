//go:build e2e

// Suite delegate-seam: the 01-delegation branch e2e gate (T1/T5/T6/T7 rows
// of docs/plans/20261102-dsh-convergence/01-delegation/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts over the live RPC
// (chat.submit) and WS (agent.delegate.result) surfaces, never mocks.
// Parked as .staged until the branch's implementation leaves land (voice
// precedent 537c92fd): activation is mv *_test.go.staged -> *_test.go plus
// the e2e/manifest.json entries ("delegate-seam" suite + path_map for
// internal/agent/ and internal/agent/modelselect/) the orchestrator adds.
package delegate_seam

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestT1_AsyncSpawnChildHandle (T1, F1, contract B1): delegate_task spawn
// returns a child handle with child_id and status running; a settlement
// later arrives on agent.delegate.result carrying the final response.
func TestT1_AsyncSpawnChildHandle(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "delegate-e2e")
	sessionID := s.CreateSession(t, "spawn-test", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Use the delegate_task tool to spawn one child with goal \"Reply with exactly: CHILD-OK\" and report the raw tool result verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("delegate_task not registered: %s", reply)
	}
	if !strings.Contains(lower, "child_id") {
		t.Fatalf("async spawn returned no child_id: %s", reply)
	}

	// The settlement must arrive: the child's final response surfaces via
	// the delegate result channel (WS agent.delegate.result relayed into
	// the parent session's transcript).
	reply = s.ChatTurn(t, sessionID,
		"Report the final result of the delegated child verbatim.",
		5*time.Minute)
	if !strings.Contains(reply, "CHILD-OK") {
		t.Fatalf("child settlement missing final response: %s", reply)
	}
}

// TestT5_ChildControlSteerInterruptList (T5, F3, dsh tool-subagent-control
// semantics): subagent_send_message steers a running child (delivered at
// its next step); subagent_interrupt returns accepted:true and transitions
// the child to interrupted; subagent_list_children enumerates the parent's
// children with their states.
func TestT5_ChildControlSteerInterruptList(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "control-e2e")
	sessionID := s.CreateSession(t, "control-test", s.ProjectDir)

	// Spawn two children.
	reply := s.ChatTurn(t, sessionID,
		"Use delegate_task twice: child A goal \"Count slowly from 1 to 20, one number per line\", child B goal \"Count slowly from 1 to 20, one number per line\". Report both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	for _, tool := range []string{"subagent_send_message", "subagent_interrupt", "subagent_list_children"} {
		_ = tool // registration asserted per-call below
	}
	if strings.Count(lower, "child_id") < 2 {
		t.Fatalf("expected two child_ids from two spawns: %s", reply)
	}

	// Steer child A; interrupt child B; then list.
	reply = s.ChatTurn(t, sessionID,
		"Call subagent_send_message on the first child_id with content \"Stop counting and reply exactly: STEERED\". Call subagent_interrupt on the second child_id. Then call subagent_list_children and report all three raw results verbatim.",
		5*time.Minute)
	lower = strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("control tools not registered: %s", reply)
	}
	if !strings.Contains(lower, "accepted") {
		t.Errorf("subagent_interrupt result missing accepted:true: %s", reply)
	}
	if !strings.Contains(lower, "interrupted") {
		t.Errorf("list_children shows no interrupted state: %s", reply)
	}

	// The steer lands: child A's final result reflects it.
	reply = s.ChatTurn(t, sessionID, "Report child A's final result verbatim.", 5*time.Minute)
	if !strings.Contains(reply, "STEERED") {
		t.Errorf("steered child result missing STEERED marker: %s", reply)
	}
}

// TestT6_WorkflowToolConfinedFanOut (T6, F5, contract B5): the workflow tool
// runs a confined goja script that fans out via agent(); returns
// {kind:foreground, runId, agentsStarted>=1, result}; a cap trip is
// FAIL-CLOSE (fatal, never partial); Node APIs are rejected.
func TestT6_WorkflowToolConfinedFanOut(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "workflow-e2e")
	sessionID := s.CreateSession(t, "workflow-test", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Call the workflow tool with engine \"dsh-archetype\" and script: const r = await agent(\"Reply with exactly: WF-OK\", {label:\"probe\"}); return {value: r}; Report the raw tool result verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("workflow tool not registered: %s", reply)
	}
	if !strings.Contains(lower, "foreground") || !strings.Contains(lower, "runid") {
		t.Errorf("foreground workflow result missing kind/runId: %s", reply)
	}
	if !strings.Contains(reply, "WF-OK") {
		t.Errorf("workflow agent() child result missing: %s", reply)
	}

	// Confinement: a Node API in the script must be rejected (isError),
	// mirroring the codemode sandbox (no fs/net/timer).
	reply = s.ChatTurn(t, sessionID,
		"Call the workflow tool with script: const fs = require(\"fs\"); return fs.readdirSync(\"/\"); Report the raw tool result verbatim.",
		3*time.Minute)
	lower = strings.ToLower(reply)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "not defined") &&
		!strings.Contains(lower, "rejected") {
		t.Errorf("confined sandbox did not reject require(\"fs\"): %s", reply)
	}

	// Fail-close cap: exceeding max_total_agents must be a fatal
	// INVALID_ARGUMENT-style error, never a partial success.
	reply = s.ChatTurn(t, sessionID,
		"Call the workflow tool with script: const thunks = []; for (let i=0;i<50;i++) thunks.push(() => agent(\"hi\")); await parallel(thunks); return 1; and max_total_agents 5. Report the raw tool result verbatim.",
		3*time.Minute)
	lower = strings.ToLower(reply)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "exceed") &&
		!strings.Contains(lower, "invalid") {
		t.Errorf("cap trip was not fail-close: %s", reply)
	}
}

// TestT7_DescriptorColdResume (T7, F6, contract B6): an interrupted child
// with a persisted descriptor resumes via DelegateManager.Resume(childID)
// (re-spawn from mode/provider/model/seed) and completes; the result
// surfaces on the delegate result channel.
func TestT7_DescriptorColdResume(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "resume-e2e")
	sessionID := s.CreateSession(t, "resume-test", s.ProjectDir)

	// Spawn, interrupt, then resume through the control tools.
	reply := s.ChatTurn(t, sessionID,
		"Use delegate_task to spawn a child with goal \"Count slowly from 1 to 30, one number per line\". Then call subagent_interrupt on its child_id. Report both raw results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "child_id") {
		t.Fatalf("spawn returned no child_id: %s", reply)
	}

	reply = s.ChatTurn(t, sessionID,
		"Resume the interrupted child (subagent_send_message with content \"Continue and finish, then reply exactly: RESUME-OK\") and report its final result verbatim.",
		5*time.Minute)
	if !strings.Contains(reply, "RESUME-OK") {
		t.Fatalf("cold resume did not complete the child: %s", reply)
	}
}
