//go:build e2e

// Plan-contract quickplan gates (tree 20261125-plan-contract-quickplan,
// leaf 04; closes bughunt 2026-11-24 M11/M6/H6 acceptance). Manifest
// scenarios plan-lifecycle-07..09.
//
// Contracts under test (master.md D1/D2/D4 + C-2/C-3/C-4):
//   - a dispatched quick_plan turn produces ONE plan row that runs the
//     full lifecycle draft -> pending_approval -> approved -> executing
//     (the plan row IS the recorded contract; never a terminal
//     pending_approval zombie, never an unlinked orphan task twin), the
//     row's task_id is the dispatched task, and the task's completion
//     rolls the plan up to completed (D2);
//   - a sequential compound ("plan X and then implement it") rides the
//     SAME lane with Intent.Method "compound_quickplan" (D4), observable
//     as the turn.terminal classified_by provenance;
//   - approval_requested (D3) is the single pause: the row parks at
//     pending_approval (kind=quick_plan tag) with the legacy "plan
//     created: ... (status: pending_approval)" reply and ZERO tasks;
//     the operator's plan.approve releases the brief INTO the same
//     execution lane as the auto-approve branch (issue #177 owner
//     ruling B, 2026-11-26: resumer -> createTask + AdoptTask ->
//     executing -> rollup completed; the pre-ruling zero-phase
//     instant-complete is the pinned-away bug).
//
// Rig patterns follow the suite's prior waves: classifier/analyzer
// pinned through the fake LLM (harness.Script on the "intent analysis
// assistant" system prompt + SetClassifierOutput), plan discovery via
// the watched markdown roots, store state via plan.get, bus evidence
// via gateSubscribe.

package planlifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// qpStack boots the contract rig: plans.mode "always" (ShouldCreatePlan
// true for every lane, so the quick_plan branch is reachable) with
// require_approval TRUE — the strongest posture for D1: the quick_plan
// false branch must auto-approve THROUGH the config gate (the system
// approves the brief as the recorded contract), while the
// approval_requested branch parks.
func qpStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	}))
	s.RegisterProject(t, "qp-project")
	return s
}

// qpPinQuickPlanAnalyzer pins the intent analyzer (system prompt "intent
// analysis assistant"; distinct from the intent classifier) to an
// unambiguous quickplan verdict with approval_requested absent (the
// legacy oneshot default, D1/D4).
func qpPinQuickPlanAnalyzer(s *harness.Stack) {
	s.Fake.Script(harness.SystemPromptContains("intent analysis assistant"),
		harness.TextResponse(`{"goal":"execute the quickplan brief","ambiguity":0.1,`+
			`"scope":"narrow","category":"quickplan","suggested_questions":[],`+
			`"confidence":0.95,"suggested_mode":"quick_plan"}`))
}

// qpTurnTerminal is one decoded turn.terminal event (the frozen subset
// this suite observes).
type qpTurnTerminal struct {
	TurnID       string
	TaskID       string
	IntentType   string
	HandlerCase  string
	Status       string
	Reply        string
	ClassifiedBy string
}

// qpCollector drains bus events on the subscribed connection into a
// mutex-guarded log, so assertions run against everything delivered so
// far without racing the poll cadence.
type qpCollector struct {
	poll     func() []map[string]any
	terminal []qpTurnTerminal
	topics   []string // "topic:json-ish payload marker" pairs, in arrival order
}

func qpSubscribe(t *testing.T, s *harness.Stack) *qpCollector {
	t.Helper()
	_, poll := gateSubscribe(t, s, "turn.terminal", "plan.submitting", "plan.approved",
		"plan.executing", "plan.completed", "plan.failed")
	return &qpCollector{poll: poll}
}

func (c *qpCollector) drain() {
	for _, ev := range c.poll() {
		topic, _ := ev["topic"].(string)
		payload, _ := ev["payload"].(map[string]any)
		c.topics = append(c.topics, topic)
		if topic == "turn.terminal" {
			tt, _ := payload["turn_id"].(string)
			taskID, _ := payload["task_id"].(string)
			intentType, _ := payload["intent_type"].(string)
			hc, _ := payload["handler_case"].(string)
			status, _ := payload["status"].(string)
			reply, _ := payload["reply"].(string)
			classifiedBy, _ := payload["classified_by"].(string)
			c.terminal = append(c.terminal, qpTurnTerminal{
				TurnID: tt, TaskID: taskID, IntentType: intentType,
				HandlerCase: hc, Status: status, Reply: reply, ClassifiedBy: classifiedBy,
			})
		}
	}
}

// waitFor polls the bus until cond passes or the deadline expires.
func (c *qpCollector) waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	harness.WaitFor(t, timeout, what, func() bool {
		c.drain()
		return cond()
	})
}

// planFiles lists every plan markdown under the watched roots (the same
// roots waitForPlanFile scans) — the zombie-row census.
func planFiles(s *harness.Stack) []string {
	roots := []string{
		filepath.Join(s.MeeptHome, "plans"),
		filepath.Join(s.ProjectDir, "docs", "plans"),
		filepath.Join(s.Work, "docs", "plans"),
	}
	var out []string
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	return out
}

// qpAssertContractEndState pins the shape shared by scenarios 07/08:
// the async ack carried the plan's task (never a "plan created" text
// reply on the async lane), exactly ONE task row exists and it IS the
// plan's bound task, it completes, and the plan rolls up to completed
// through executing — never parked at pending_approval.
func qpAssertContractEndState(t *testing.T, s *harness.Stack, coll *qpCollector,
	ackTurnID, planID string) {
	t.Helper()

	// The ack turn.terminal carries the dispatched task id and the
	// async_dispatch case (the false branch's Response is empty, so the
	// "plan created" direct-response shape never appears on this lane).
	var ack *qpTurnTerminal
	coll.waitFor(t, 30*time.Second, "async ack turn.terminal for "+ackTurnID, func() bool {
		for i := range coll.terminal {
			if coll.terminal[i].TurnID == ackTurnID && coll.terminal[i].TaskID != "" {
				ack = &coll.terminal[i]
				return true
			}
		}
		return false
	})
	if ack.HandlerCase != "async_dispatch" {
		t.Fatalf("contract lane: ack handler_case = %q, want async_dispatch (a quick_plan dispatch is an executed contract, not a direct reply): %+v",
			ack.HandlerCase, ack)
	}
	if strings.Contains(ack.Reply, "plan created") {
		t.Errorf("M11 false-branch ack: async lane carries 'plan created' text — the zombie posture is back: %q", ack.Reply)
	}

	// The plan row already left draft -> pending_approval -> approved
	// inside the dispatch (never parked at the gate) and its task_id IS
	// the dispatched task — the single linkage (D2, C-1 seam). Tolerate
	// a fast fake-LLM completing the task before this first poll: the
	// legal post-ack states are executing/completed; the bus sequence
	// below pins that executing really transited.
	var state string
	var got map[string]any
	harness.WaitFor(t, 30*time.Second, "plan row past the approval gate", func() bool {
		got = rpc(t, s, "plan.get", map[string]any{"id": planID})
		state = str(got, "state")
		return state == "executing" || state == "completed"
	})
	if state == "pending_approval" || state == "draft" {
		t.Fatalf("plan row parked at the gate on the oneshot lane (state %q): %v", state, got)
	}
	if tid := str(got, "task_id"); tid != ack.TaskID {
		t.Fatalf("plan task_id = %q, want the dispatched task %q", tid, ack.TaskID)
	}

	// Exactly one task row — no orphan twin (M11 / M13-adjacent note:
	// the plan lane and the task lane no longer mint independent rows).
	harness.WaitFor(t, 20*time.Second, "the dispatched task row", func() bool {
		return len(harness.Tasks(t, s.TasksDBPath())) >= 1
	})
	// Give a wrongly-minted twin a bounded window to appear.
	time.Sleep(2 * time.Second)
	if rows := harness.Tasks(t, s.TasksDBPath()); len(rows) != 1 {
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID+"("+r.State+")")
		}
		t.Fatalf("quick_plan contract produced %d task rows %v, want exactly 1 (the plan's adopted task %s)",
			len(rows), ids, ack.TaskID)
	}

	// The task completes, and the plan follows via the rollup (D2: plan
	// completion is a rollup of its task — OnTaskCompleted -> completePlan).
	row := harness.WaitTaskCompleted(t, s.TasksDBPath(), ack.TaskID, 180*time.Second)
	if row.State != "completed" {
		t.Fatalf("task %s final state = %q, want completed; row: %+v\nlog tail:\n%s",
			ack.TaskID, row.State, row, s.Daemon.LogTail())
	}
	waitForPlanState(t, s, planID, "completed", 60*time.Second)

	// Bus sequence evidence: submitting -> approved -> executing ->
	// completed for this plan row (the full lifecycle really transited;
	// a row that jumped straight to completed or never left the gate is
	// the zombie/lie this tree deleted).
	coll.drain()
	coll.waitFor(t, 10*time.Second, "plan.completed bus event", func() bool {
		for _, tp := range coll.topics {
			if tp == "plan.completed" {
				return true
			}
		}
		return false
	})
	var idxSubmit, idxApprove, idxExec, idxDone = -1, -1, -1, -1
	for i, tp := range coll.topics {
		switch tp {
		case "plan.submitting":
			if idxSubmit < 0 {
				idxSubmit = i
			}
		case "plan.approved":
			if idxApprove < 0 {
				idxApprove = i
			}
		case "plan.executing":
			if idxExec < 0 {
				idxExec = i
			}
		case "plan.completed":
			idxDone = i
		}
	}
	if idxSubmit < 0 || idxApprove < 0 || idxExec < 0 || idxDone < 0 ||
		!(idxSubmit < idxApprove && idxApprove < idxExec && idxExec < idxDone) {
		t.Errorf("plan lifecycle bus sequence not submitting<approved<executing<completed: indexes %d/%d/%d/%d of topics %v",
			idxSubmit, idxApprove, idxExec, idxDone, coll.topics)
	}

	// The completion relay rides the same turn id and never regurgitates
	// the "plan created" direct-reply shape either.
	var relay *qpTurnTerminal
	coll.waitFor(t, 30*time.Second, "task-completion relay on turn "+ackTurnID, func() bool {
		for i := range coll.terminal {
			ev := coll.terminal[i]
			if ev.TurnID == ackTurnID && ev.Status == "completed" {
				relay = &coll.terminal[i]
				return true
			}
		}
		return false
	})
	if strings.Contains(relay.Reply, "plan created") {
		t.Errorf("M11 false-branch ack: completion relay carries 'plan created': %q", relay.Reply)
	}
}

// ---------------------------------------------------------------------------
// plan-lifecycle-07 (M): dispatched quick_plan -> contract through the plan
// machine (M11/D1/D2)
// ---------------------------------------------------------------------------

// TestQuickPlanDispatchRunsPlanContract pins the C-2 rewrite end to end:
// a dispatched quick_plan turn (classifier + analyzer pinned to the
// quickplan lane, approval_requested absent) drives its plan row
// draft -> approved -> executing (NEVER parked), the row binds the
// dispatched task as its task_id, exactly one task row exists (no
// orphan twin), and task completion rolls the plan up to completed. The
// async lane carries no "plan created" text reply.
func TestQuickPlanDispatchRunsPlanContract(t *testing.T) {
	s := qpStack(t)
	sessionID := s.CreateSession(t, "qp-dispatch", s.ProjectDir)

	s.Fake.SetClassifierOutput(`{"intent":"quickplan","confidence":0.95,"reasoning":"pinned"}`)
	qpPinQuickPlanAnalyzer(s)

	artifact := filepath.Join(s.ProjectDir, "qp-contract.txt")
	s.Fake.EnqueueFileWrite("call-qp07", artifact, "contract evidence")
	s.Fake.SetPostToolText("Created qp-contract.txt with the contract evidence.")

	coll := qpSubscribe(t, s)

	ack := s.SubmitChatHTTP(t, sessionID,
		"Work through the quickplan: create a file named qp-contract.txt containing contract evidence, one at a time without stopping")
	ackTurnID, _ := ack["turn_id"].(string)
	if ackTurnID == "" {
		t.Fatalf("qp07: submit ack missing turn_id: %+v", ack)
	}

	// The dispatch created exactly one plan file (the contract row) and
	// nothing else in the watched roots.
	filePath := waitForPlanFile(t, s, 30*time.Second)
	if files := planFiles(s); len(files) != 1 {
		t.Fatalf("qp07: %d plan files %v, want exactly 1 contract row", len(files), files)
	}
	planID := requireParses(t, filePath, "").PlanID
	if planID == "" {
		t.Fatalf("qp07: plan file %s carries no plan_id", filePath)
	}

	qpAssertContractEndState(t, s, coll, ackTurnID, planID)

	// The brief really executed: the artifact the task's step wrote exists.
	if data, err := os.ReadFile(artifact); err != nil || !strings.Contains(string(data), "contract evidence") {
		t.Fatalf("qp07: contract artifact missing or wrong (err=%v): the adopted task never executed", err)
	}
}

// ---------------------------------------------------------------------------
// plan-lifecycle-08 (M): sequential compound rides the quickplan contract
// lane with method compound_quickplan (H6/D4)
// ---------------------------------------------------------------------------

// TestSequentialCompoundRidesQuickPlanContract pins the D4 merge: "plan
// X and then implement it" is ONE quick_plan contract, not a spec_pair
// parent. Same shape as scenario 07 (single task bound to the row,
// executing -> completed rollup, no "plan created" on the async lane)
// plus the provenance pin: the ack's turn.terminal carries
// intent_type "quickplan" with classified_by "compound_quickplan"
// (leaf-04 surfaces directive from 90c5f993: surfaces treat sequential
// compounds as quickplan rows).
func TestSequentialCompoundRidesQuickPlanContract(t *testing.T) {
	s := qpStack(t)
	sessionID := s.CreateSession(t, "qp-compound", s.ProjectDir)

	// Array-shaped classifier pin: the single-intent parse fails (falls
	// through to the keyword chain) while the multi-intent detector
	// parses BOTH fragments (dispatcher-06 rig). The "plan" fragment is
	// in laneForcesSequentialCompound, so DetectCompound sets
	// CompoundType "sequential" and routeSequentialCompoundAsQuickPlan
	// converts the verdict into IntentQuickPlan + Method
	// "compound_quickplan".
	s.Fake.SetClassifierOutput(`[{"intent":"plan","confidence":0.9},{"intent":"code","confidence":0.85}]`)
	qpPinQuickPlanAnalyzer(s)

	artifact := filepath.Join(s.ProjectDir, "qp-compound.md")
	s.Fake.EnqueueFileWrite("call-qp08", artifact, "quarterly outline")
	s.Fake.SetPostToolText("Created qp-compound.md with the planned outline sections.")

	coll := qpSubscribe(t, s)

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a plan for the quarterly review notes file qp-compound.md and then implement it by writing that file with the outline sections")
	ackTurnID, _ := ack["turn_id"].(string)
	if ackTurnID == "" {
		t.Fatalf("qp08: submit ack missing turn_id: %+v", ack)
	}

	// The compound conversion created the plan row through the SAME
	// branch: exactly one contract file.
	filePath := waitForPlanFile(t, s, 30*time.Second)
	if files := planFiles(s); len(files) != 1 {
		t.Fatalf("qp08: %d plan files %v, want exactly 1 (no spec_pair twin)", len(files), files)
	}
	planID := requireParses(t, filePath, "").PlanID
	if planID == "" {
		t.Fatalf("qp08: plan file %s carries no plan_id", filePath)
	}

	// Provenance: the ack event itself is the pin that the compound
	// verdict became a quickplan contract.
	var ackEv *qpTurnTerminal
	coll.waitFor(t, 30*time.Second, "compound ack turn.terminal for "+ackTurnID, func() bool {
		for i := range coll.terminal {
			if coll.terminal[i].TurnID == ackTurnID && coll.terminal[i].TaskID != "" {
				ackEv = &coll.terminal[i]
				return true
			}
		}
		return false
	})
	if ackEv.IntentType != "quickplan" {
		t.Errorf("qp08: ack intent_type = %q, want quickplan (D4: sequential compound IS quickplan)", ackEv.IntentType)
	}
	if ackEv.ClassifiedBy != "compound_quickplan" {
		t.Errorf("qp08: ack classified_by = %q, want compound_quickplan (leaf-02 method stamp)", ackEv.ClassifiedBy)
	}

	qpAssertContractEndState(t, s, coll, ackTurnID, planID)

	if data, err := os.ReadFile(artifact); err != nil || !strings.Contains(string(data), "quarterly outline") {
		t.Fatalf("qp08: compound artifact missing or wrong (err=%v): the adopted task never executed", err)
	}
}

// ---------------------------------------------------------------------------
// plan-lifecycle-09 (M): approval_requested parks the contract; operator
// approve RELEASES IT INTO EXECUTION (M6/D3 park + issue #177 ruling B)
// ---------------------------------------------------------------------------

// TestApprovalRequestedParksAndOperatorApprove pins the D3 exception and
// the ruling-B post-approve shape end to end: an analyzer verdict with
// approval_requested true parks the quick_plan row at pending_approval
// with the "plan created: ... (status: pending_approval)" reply and ZERO
// task rows (the oneshot default is overridden ONLY here). The operator's
// plan.approve then runs the SAME lane as the auto-approve branch (issue
// #177 owner ruling B, 2026-11-26): the row leaves pending_approval, the
// approve resumer (dispatcher SetPlanManager wiring) mints the strategic
// task, AdoptTask binds it (task_id non-empty, row -> executing), the
// fake planner decomposes and the task runs to completion — and the plan
// rolls up to completed through the task (D2).
//
// The PRE-ruling pin (approve -> Synthesize zero-phase guard -> instant
// completed with no task, "D5-adjacent owner edge PINNED") is DELETED:
// that completion-without-execution shape is the bug the ruling closed,
// and this scenario now FAILS if it ever returns (state completed with an
// empty task_id at any point after approve is asserted against).
func TestApprovalRequestedParksAndOperatorApprove(t *testing.T) {
	s := qpStack(t)
	sessionID := s.CreateSession(t, "qp-park", s.ProjectDir)

	s.Fake.SetClassifierOutput(`{"intent":"quickplan","confidence":0.95,"reasoning":"pinned"}`)
	// Primary vote true short-circuits the C-3 fallback (no second
	// adjudication spend): the pinned analysis carries
	// approval_requested true.
	s.Fake.Script(harness.SystemPromptContains("intent analysis assistant"),
		harness.TextResponse(`{"goal":"plan the rework behind an approval gate","ambiguity":0.1,`+
			`"scope":"narrow","category":"quickplan","suggested_questions":[],`+
			`"confidence":0.95,"suggested_mode":"quick_plan","approval_requested":true}`))

	coll := qpSubscribe(t, s)

	// Fire-and-forget submit (the suite's parked-turn pattern, mirroring
	// the sibling continuation test): the approval_requested turn parks
	// on the plan gate (gate_park_wiring), so the CLI await path would
	// block to its ceiling. The "plan created" reply is observed on the
	// turn.terminal event for the submitted turn instead.
	ack := s.SubmitChatHTTP(t, sessionID,
		"plan the storage rework and implement it only after I approve")
	ackTurnID, _ := ack["turn_id"].(string)
	if ackTurnID == "" {
		t.Fatalf("qp09: submit ack missing turn_id: %+v", ack)
	}

	var parkEvent *qpTurnTerminal
	coll.waitFor(t, 30*time.Second, "park turn.terminal for "+ackTurnID, func() bool {
		for i := range coll.terminal {
			if coll.terminal[i].TurnID == ackTurnID &&
				strings.Contains(coll.terminal[i].Reply, "plan created") {
				parkEvent = &coll.terminal[i]
				return true
			}
		}
		return false
	})
	reply := parkEvent.Reply
	if !strings.Contains(reply, "plan created:") {
		t.Errorf("qp09: park reply missing 'plan created:': %q", reply)
	}
	if !strings.Contains(reply, "(status: pending_approval)") {
		t.Fatalf("qp09: park reply must name the parked status (D3): %q", reply)
	}
	if parkEvent.TaskID != "" {
		t.Errorf("qp09: parked lane must not dispatch a task (turn.terminal task_id=%q)", parkEvent.TaskID)
	}

	filePath := waitForPlanFile(t, s, 30*time.Second)
	planID := requireParses(t, filePath, "").PlanID
	if planID == "" {
		t.Fatalf("qp09: plan file %s carries no plan_id", filePath)
	}
	waitForPlanState(t, s, planID, "pending_approval", 30*time.Second)

	// Ruling B park-time tag: the dispatcher's quick_plan park stamps
	// plans.kind=quick_plan (internal/plan QuickPlanKind) so the approve
	// branch can identify the row as a parked brief. plan.get returns the
	// Plan JSON; the field must be present and tagged BEFORE any approve.
	if kind := str(rpc(t, s, "plan.get", map[string]any{"id": planID}), "kind"); kind != "quick_plan" {
		t.Fatalf("qp09: parked quick_plan row kind = %q, want quick_plan (park-time tag missing — approve cannot branch)", kind)
	}

	// The park creates NO task: execution starts on approve, not before.
	time.Sleep(2 * time.Second)
	if rows := harness.Tasks(t, s.TasksDBPath()); len(rows) != 0 {
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID+"("+r.State+")")
		}
		t.Fatalf("qp09: approval_requested dispatch minted %d task rows %v, want 0 (park posture creates no task)", len(rows), ids)
	}

	// Pin the executor lane BEFORE approve so the released task really
	// runs: the harness default planner JSON yields the scripted single
	// step, the queued write_file tool call executes on the step's loop
	// (same mechanism as scenarios 07/08), and the post-tool text ends
	// the turn.
	artifact := filepath.Join(s.ProjectDir, "qp09-rework.md")
	s.Fake.EnqueueFileWrite("call-qp09", artifact, "rework executed after approve")
	s.Fake.SetPostToolText("Created qp09-rework.md with the rework note.")

	// Operator approves. Ruling B: the row must leave pending_approval and
	// reach EXECUTING with a bound task — never the pre-ruling
	// zero-phase instant-complete (completed with an empty task_id).
	rpc(t, s, "plan.approve", map[string]any{"plan_id": planID, "by": "e2e-operator"})

	// Half 1 (immediate): the approval committed — the row is out of
	// pending_approval within the approve call.
	got := rpc(t, s, "plan.get", map[string]any{"id": planID})
	state := str(got, "state")
	if state == "pending_approval" || state == "draft" {
		t.Fatalf("qp09: plan.approve left the row parked: %v", got)
	}
	// The INSTANT-COMPLETION anti-pin: completed with no task_id is the
	// #177 bug. It must never appear at ANY post-approve observation.
	if state == "completed" && str(got, "task_id") == "" {
		t.Fatalf("qp09: REGRESSION — approve zero-phase-completed the brief (state=completed, task_id empty): %v", got)
	}

	// Half 2 (resumer seam): the row reaches executing with a NON-EMPTY
	// task_id — the approve resumer minted and adopted the strategic task.
	harness.WaitFor(t, 30*time.Second, "post-approve executing with bound task", func() bool {
		got = rpc(t, s, "plan.get", map[string]any{"id": planID})
		state = str(got, "state")
		return (state == "executing" || state == "completed") && str(got, "task_id") != ""
	})
	taskID := str(got, "task_id")
	if taskID == "" {
		t.Fatalf("qp09: post-approve row never bound a task (state %q): the resumer seam is dead: %v", state, got)
	}
	if state == "completed" {
		// Legal only because the whole execute+rollup already ran — the
		// task row must exist and be completed, never the zero-phase lie.
		t.Logf("qp09: fake-LLM raced the rollup: row already completed with task_id=%s (bound-task execution ran; anti-instant-complete pin holds because task_id is non-empty)", taskID)
	}

	// The released task really exists as a task row (no zombie contract:
	// executing must carry a live task).
	harness.WaitFor(t, 20*time.Second, "the approve-resumed task row", func() bool {
		rows := harness.Tasks(t, s.TasksDBPath())
		for _, r := range rows {
			if r.ID == taskID {
				return true
			}
		}
		return false
	})

	// Half 3 (D2 rollup): the turn completes — the task finishes and the
	// plan rolls up to completed through the task's completion (the same
	// rollup the contract scenarios 07/08 pin).
	row := harness.WaitTaskCompleted(t, s.TasksDBPath(), taskID, 180*time.Second)
	if row.State != "completed" {
		t.Fatalf("qp09: resumed task %s final state = %q, want completed; row: %+v\nlog tail:\n%s",
			taskID, row.State, row, s.Daemon.LogTail())
	}
	waitForPlanState(t, s, planID, "completed", 60*time.Second)

	// The brief actually executed: the step's artifact exists.
	if data, err := os.ReadFile(artifact); err != nil || !strings.Contains(string(data), "rework executed after approve") {
		t.Fatalf("qp09: post-approve artifact missing or wrong (err=%v): the approved brief was never executed", err)
	}

	// Bus evidence: the parked window never leaked execution, and the
	// post-approve lifecycle really transited submitting -> approved ->
	// executing -> completed.
	coll.drain()
	coll.waitFor(t, 10*time.Second, "plan.completed bus event after approve", func() bool {
		for _, tp := range coll.topics {
			if tp == "plan.completed" {
				return true
			}
		}
		return false
	})
	var idxSubmit, idxApprove, idxExec, idxDone = -1, -1, -1, -1
	for i, tp := range coll.topics {
		switch tp {
		case "plan.submitting":
			if idxSubmit < 0 {
				idxSubmit = i
			}
		case "plan.approved":
			if idxApprove < 0 {
				idxApprove = i
			}
		case "plan.executing":
			if idxExec < 0 {
				idxExec = i
			}
		case "plan.completed":
			idxDone = i
		}
	}
	if idxSubmit < 0 || idxApprove < 0 || idxExec < 0 || idxDone < 0 ||
		!(idxSubmit < idxApprove && idxApprove < idxExec && idxExec < idxDone) {
		t.Errorf("qp09: post-approve bus sequence not submitting<approved<executing<completed: indexes %d/%d/%d/%d of topics %v",
			idxSubmit, idxApprove, idxExec, idxDone, coll.topics)
	}
}
