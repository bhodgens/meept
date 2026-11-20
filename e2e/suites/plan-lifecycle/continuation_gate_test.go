//go:build e2e

// Plan-lifecycle continuation extension: 02-routing TEST-GATE T7
// (W13; R7/A9; master C3/C5, matrix F8, scenario plan-lifecycle-04).
//
// A refinement follow-up to a PARKED PLANNING task (plan-compiler draft
// awaiting seal) must continue the SAME task: the draft version bumps
// via SaveDraft and NO new task row appears. Today the dispatcher has
// no continuation branch - the follow-up classifies as work and mints a
// second task while the parked draft goes stale.
//
// The parked draft is produced through the REAL pipeline: plans.mode
// always + plan_compiler_enabled routes the first turn into the
// brainstorm draft (seedDraftFromRequest), which awaits plan.seal.

package planlifecycle

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// draftGateStack boots with the plan-compiler pipeline enabled so the
// strategic planner seeds a brainstorm draft and parks at the seal step.
func draftGateStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.plan_compiler_enabled": true,
	}))
}

// planningTaskID finds the session's task in the planning state via the
// tasks.db rows the harness exposes (state column is the end state).
func planningTaskID(t *testing.T, s *harness.Stack, timeout time.Duration) string {
	t.Helper()
	var found string
	harness.WaitFor(t, timeout, "a planning-state task row", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.State == "planning" {
				found = row.ID
				return true
			}
		}
		return false
	})
	return found
}

// draftVersion reads the parked task's draft version through plan.draft.
func draftVersion(t *testing.T, s *harness.Stack, taskID string) int {
	t.Helper()
	got := rpc(t, s, "plan.draft", map[string]any{"task_id": taskID})
	v, _ := got["version"].(float64)
	return int(v)
}

// TestPlanLifecycle04RefinementContinuesParkedDraft pins the F8
// continuation contract end to end: plan request -> parked draft ->
// refinement follow-up -> SAME task's draft advances, no new task.
func TestPlanLifecycle04RefinementContinuesParkedDraft(t *testing.T) {
	s := draftGateStack(t)
	sessionID := s.CreateSession(t, "plan-refine", s.ProjectDir)

	// Turn 1: a plan-shaped request seeds the brainstorm draft and parks.
	// The classifier is scripted to the code lane: IntentCode.SuggestedMode
	// is "plan", which routes the strategic planner into the compiler
	// pipeline's seedDraftFromRequest branch (the draft awaits plan.seal).
	s.Fake.Script(harness.IsClassifierRequest(),
		harness.ClassifierJSONResponse(`{"intent":"code","confidence":0.9}`))
	// Turn 1 rides the fire-and-forget submit too: the parked draft's
	// turn never finalizes (it awaits plan.seal), so the CLI await path
	// would block to its ceiling. The draft seed is the end state.
	ack1 := s.SubmitChatHTTP(t, sessionID,
		"plan a translator script covering three languages with a cli entry point")
	if tid, _ := ack1["turn_id"].(string); tid == "" {
		t.Fatalf("plan submit ack missing turn_id: %+v", ack1)
	}
	s.Fake.ResetScripts()
	taskID := planningTaskID(t, s, 30*time.Second)
	if taskID == "" {
		t.Fatal("no planning-state task after the plan request")
	}
	versionBefore := draftVersion(t, s, taskID)
	if versionBefore < 1 {
		t.Fatalf("parked task %s has no seeded draft (version %d)", taskID, versionBefore)
	}
	tasksBefore := len(harness.Tasks(t, s.TasksDBPath()))

	// Turn 2: the refinement follow-up (non-command, references the
	// parked draft's scope). It must SaveDraft on the SAME task. Submit
	// fire-and-forget (the ack is immediate); the continuation contract
	// is asserted on the store end state, not the reply text.
	ack := s.SubmitChatHTTP(t, sessionID, "make it five languages instead")
	if tid, _ := ack["turn_id"].(string); tid == "" {
		t.Fatalf("refinement submit ack missing turn_id: %+v", ack)
	}

	// End state 1: no new task row (give the dispatch a bounded window
	// to settle; a wrongly-minted task appears within it).
	deadline := time.Now().Add(20 * time.Second)
	var tasksAfter []harness.TaskRow
	for time.Now().Before(deadline) {
		tasksAfter = harness.Tasks(t, s.TasksDBPath())
		if len(tasksAfter) > tasksBefore {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(tasksAfter) > tasksBefore {
		var extra []string
		for _, row := range tasksAfter {
			if row.ID != taskID {
				extra = append(extra, row.ID+" ("+row.Name+")")
			}
		}
		t.Errorf("refinement follow-up minted new task(s) %v; a refinement of a parked planning draft must continue task %s via SaveDraft (W13/F8)",
			extra, taskID)
	}

	// End state 2: the SAME task's draft version advanced.
	versionAfter := draftVersion(t, s, taskID)
	if versionAfter <= versionBefore {
		t.Errorf("parked draft version stayed at %d after the refinement; SaveDraft must run on task %s with the full follow-up text (C3)",
			versionBefore, taskID)
	}

	// End state 3: the dispatch trace records the continuation under the
	// session (C5 handler_case evidence), queryable by the primary id.
	client := harness.DialRPC(t, s.SocketPath)
	raw := client.CallResult("session.dispatch_trace", map[string]any{"session_id": sessionID})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	var trace struct {
		Count   int `json:"count"`
		Entries []struct {
			HandlerCase string `json:"handler_case"`
			TaskID      string `json:"task_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if trace.Count == 0 {
		t.Error("dispatch trace by primary session id returned 0 rows: the continuation must be observable evidence (C5 dual-key)")
	}
}
