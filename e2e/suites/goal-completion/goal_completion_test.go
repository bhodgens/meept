//go:build e2e

// Suite goal-completion (manifest scenarios goal-completion-01..04): the
// goal→plan→gate→health lifecycle at the daemon boundary. Declared goals
// fulfil through the goal loop (plan row goal_id, gate command execution,
// health assessment), goals create via RPC, chat-path plans anchor on the
// implicit-goal precedence (exactly-one active goal / no employee), and
// agents.steer re-routes a live goal-loop episode while returning a no-op
// for an idle employee.
package goalcompletion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// waitGoalNotUnknown polls the employee_goals store until the goal's health
// is no longer "unknown" (the goal row has been assessed).
func waitGoalNotUnknown(t *testing.T, s *harness.Stack, goalID string, timeout time.Duration) GoalHealthRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		for _, row := range Goals(t, s) {
			if row.ID == goalID {
				last = row.Health
				if row.Health != "unknown" && row.Health != "" {
					return row
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("goal %s health never left unknown within %s (last %q)", goalID, timeout, last)
	return GoalHealthRow{}
}

// ---------------------------------------------------------------------------
// goal-completion-01 (declare-fulfil): a tier-3 employee whose goal carries a
// gate command fulfils through the goal loop — the plan row carries goal_id,
// the gate command executes, and the goal's health is assessed.
// ---------------------------------------------------------------------------

// TestGoalCompletion01DeclaredGoalFulfils drives one full declare→trigger→
// fulfil cycle. Tier 3 + an escalation trigger routes the first (escalating)
// candidate through Plan(), stamping goal_id on the stored plan row; a second
// benign candidate runs the pure-autonomy Execute→Reflect path where the
// completion gate command executes and the goal's health is assessed.
func TestGoalCompletion01DeclaredGoalFulfils(t *testing.T) {
	const (
		empID    = "gc-fulfil-emp"
		escMark  = "gcescalate-marker"
		goalMark = "GCFULFIL-01"
	)

	// The gate command appends to a marker file — the durable observable
	// that the gate command executed (the loop logs gate results at debug
	// only, and the audit chain stores the output's hash, not the raw text).
	s := goalStack(t, nil)
	gateLog := filepath.Join(s.StateDir, "gate-ran-01.log")
	gateCmd := "echo gate-ran >> " + gateLog

	hireEmployee(t, s, empID, 3, escMark)
	goalID := createGoal(t, s, empID,
		"keep the "+goalMark+" fixture green",
		"pursue the "+goalMark+" mandate to completion",
		gateCmd)

	// GoalLoops register at boot only: restart so the new employee gets one.
	stop := restartDaemon(t, s)
	defer stop()

	// Script the goal-loop LLM turns. The ASSESS user prompt embeds the
	// trigger payload; we distinguish the escalating candidate (trigger #1)
	// from the benign one (trigger #2) by payload markers.
	s.Fake.ScriptOnce(harness.MessageContains("first-probe"),
		harness.TextResponse(assessReply(
			"escalating: "+goalMark,
			"the escalating candidate for "+goalMark,
			"perform the risky "+escMark+" action now")))
	s.Fake.ScriptN(10, harness.MessageContains("second-probe"),
		harness.TextResponse(assessReply(
			"benign: "+goalMark,
			"the benign candidate for "+goalMark,
			"perform the safe "+goalMark+" action")))
	// REFLECT turns (user prompt starts "# execution outcome").
	s.Fake.ScriptN(10, harness.MessageContains("execution outcome"),
		harness.TextResponse(reflectReply()))
	// Any executor turn (tool-bearing): plain finish, no tool calls needed.
	s.Fake.SetPostToolText("done — the action was performed.")
	s.Fake.SetChatText("ok")

	// Trigger #1: escalating candidate → Plan() → plans.db row with goal_id.
	rpcResult(t, s, "agents.trigger", map[string]any{
		"id":      empID,
		"payload": map[string]any{"source": "manual", "round": "first-probe"},
	})

	var planRow PlanGoalRow
	harness.WaitFor(t, 60*time.Second, "plans.db plan row with goal_id set", func() bool {
		for _, row := range Plans(t, PlansDBPath(s)) {
			if row.GoalID == goalID {
				planRow = row
				return true
			}
		}
		return false
	})
	if !strings.Contains(planRow.Title, goalMark) {
		t.Fatalf("plan row title %q does not carry the goal marker %q", planRow.Title, goalMark)
	}

	// Trigger #2: benign candidate → Execute → Reflect → gate + health.
	rpcResult(t, s, "agents.trigger", map[string]any{
		"id":      empID,
		"payload": map[string]any{"source": "manual", "round": "second-probe"},
	})

	// The gate command executes in the loop's gate workdir; wait for the
	// side-effect file it appends to.
	harness.WaitFor(t, 60*time.Second, "gate command side-effect file", func() bool {
		_, err := os.Stat(gateLog)
		return err == nil
	})
	data, err := os.ReadFile(gateLog)
	if err != nil || !strings.Contains(string(data), "gate-ran") {
		t.Fatalf("gate log %q missing or wrong content (%v): %q", gateLog, err, string(data))
	}

	// The goal's health is assessed (no longer unknown) — via the store row.
	row := waitGoalNotUnknown(t, s, goalID, 60*time.Second)
	if row.State != "active" {
		t.Fatalf("goal %s state = %q, want active", goalID, row.State)
	}

	// ...and via the RPC surface the CLI consumes.
	goal := goalListByID(t, s, empID, goalID)
	if goal == nil {
		t.Fatalf("agents.goals.list lost goal %s after fulfilment", goalID)
	}
	// GoalHealth is an int on the wire (0=healthy, 1=at_risk, 2=broken, 3=unknown).
	health, _ := goal["health"].(float64)
	if int(health) == 3 {
		t.Fatalf("agents.goals.list still reports goal %s health unknown: %v", goalID, goal)
	}
}

// ---------------------------------------------------------------------------
// goal-completion-02 (create-via-rpc): agents.goals.create on a running
// daemon registers the goal; agents.goals.list shows it.
// ---------------------------------------------------------------------------

func TestGoalCompletion02GoalCreateViaRPC(t *testing.T) {
	s := goalStack(t, nil)
	const empID = "gc-create-emp"

	hireEmployee(t, s, empID, 1, "")
	gid := createGoal(t, s, empID,
		"GCCREATE-02 goal",
		"the GCCREATE-02 mandate",
		"")

	goal := goalListByID(t, s, empID, gid)
	if goal == nil {
		t.Fatalf("agents.goals.list does not show goal %s after agents.goals.create; list: %v", gid,
			rpcResult(t, s, "agents.goals.list", map[string]any{"employee_id": empID}))
	}
	if got := str(goal, "title"); got != "GCCREATE-02 goal" {
		t.Fatalf("goal title = %q, want %q", got, "GCCREATE-02 goal")
	}
	if got := str(goal, "employee_id"); got != empID {
		t.Fatalf("goal employee_id = %q, want %q", got, empID)
	}
	// A fresh goal has never been assessed: health starts unknown and the
	// state starts active (wire ints: state 0=active, health 3=unknown).
	if state, _ := goal["state"].(float64); int(state) != 0 {
		t.Fatalf("fresh goal state = %v, want active(0)", goal["state"])
	}
	if health, _ := goal["health"].(float64); int(health) != 3 {
		t.Fatalf("fresh goal health = %v, want unknown(3)", goal["health"])
	}
}

// ---------------------------------------------------------------------------
// goal-completion-03 (implicit anchoring): a chat turn creating a plan on an
// install with exactly one employee + one active goal anchors the plan row
// on that goal; with no employee the plan row's goal_id stays empty.
// ---------------------------------------------------------------------------

func TestGoalCompletion03ImplicitGoalAnchoring(t *testing.T) {
	// (a) exactly one employee + one active goal → the plan row's goal_id
	// matches that goal (C6 branch 2).
	s := goalStack(t, map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	})
	const empID = "gc-anchor-emp"
	hireEmployee(t, s, empID, 1, "")
	goalID := createGoal(t, s, empID,
		"GCANCHOR-03 goal",
		"the GCANCHOR-03 mandate",
		"")

	sessionID := s.CreateSession(t, "gc-anchor-03", s.ProjectDir)
	s.ChatTurn(t, sessionID, "help me understand how index funds work", 90*time.Second)

	var anchored PlanGoalRow
	harness.WaitFor(t, 60*time.Second, "chat-created plan row anchored on the sole active goal", func() bool {
		for _, row := range Plans(t, PlansDBPath(s)) {
			if row.GoalID == goalID {
				anchored = row
				return true
			}
		}
		return false
	})
	if anchored.ID == "" {
		t.Fatal("anchored plan row has empty id")
	}

	// (b) no employee → the chat-created plan row carries an empty goal_id.
	s2 := goalStack(t, map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	})
	sessionID2 := s2.CreateSession(t, "gc-anchor-03-b", s2.ProjectDir)
	s2.ChatTurn(t, sessionID2, "help me understand how compound interest works", 90*time.Second)

	var unanchored PlanGoalRow
	harness.WaitFor(t, 60*time.Second, "chat-created plan row on the employee-less install", func() bool {
		rows := Plans(t, PlansDBPath(s2))
		if len(rows) > 0 {
			unanchored = rows[len(rows)-1]
			return true
		}
		return false
	})
	if unanchored.GoalID != "" {
		t.Fatalf("plan row on an employee-less install carries goal_id %q, want empty (row: %+v)",
			unanchored.GoalID, unanchored)
	}
	if goals := Goals(t, s2); len(goals) != 0 {
		t.Fatalf("employee-less install has goal rows: %+v", goals)
	}
}

// ---------------------------------------------------------------------------
// goal-completion-04 (steer): a tier-1 employee mid-trigger receives
// agents.steer → steer_queued=true, the daemon log shows the steering queued,
// and the re-entered assess flow carries the steering content; steering an
// idle employee returns steer_queued=false with no error.
// ---------------------------------------------------------------------------

func TestGoalCompletion04SteerMidTriggerAndIdleNoop(t *testing.T) {
	s := goalStack(t, nil)

	const (
		activeEmp = "gc-steer-emp"
		idleEmp   = "gc-idle-emp"
		steerMark = "GCSTEER-04-pivot-to-postgres"
	)

	// Both employees must exist before boot so both get registered GoalLoops.
	// The harness boots during Start, so create them, then restart.
	hireEmployee(t, s, activeEmp, 1, "")
	hireEmployee(t, s, idleEmp, 1, "")
	createGoal(t, s, activeEmp,
		"GCSTEER-04 goal",
		"the GCSTEER-04 mandate",
		"")
	stop := restartDaemon(t, s)
	defer stop()

	// Script the goal-loop LLM turns: assess replies with one candidate;
	// reflect replies healthy. The executor turn needs no tool calls.
	s.Fake.ScriptN(10, harness.MessageContains("trigger event"),
		harness.TextResponse(assessReply(
			"steer probe action",
			"the steer-probe candidate",
			"perform the GCSTEER-04 baseline action")))
	s.Fake.ScriptN(10, harness.MessageContains("execution outcome"),
		harness.TextResponse(reflectReply()))
	s.Fake.SetPostToolText("done — the baseline action was performed.")
	s.Fake.SetChatText("ok")

	// Count assess-shaped requests before we start.
	assessRequests := func() int {
		n := 0
		for _, body := range s.Fake.Requests() {
			raw, err := jsonMarshal(body)
			if err == nil && strings.Contains(raw, "trigger event") {
				n++
			}
		}
		return n
	}

	// Trigger in the background (agents.trigger runs the full decide round
	// synchronously); wait until its assess turn reaches the fake LLM so the
	// steer lands mid-trigger.
	done := make(chan error, 1)
	go func() {
		_, err := rpcCall(t, s, "agents.trigger", map[string]any{
			"id":      activeEmp,
			"payload": map[string]any{"source": "manual", "round": "steer-probe"},
		})
		done <- err
	}()

	harness.WaitFor(t, 60*time.Second, "the trigger's assess turn to reach the fake LLM", func() bool {
		return assessRequests() > 0
	})

	// Mid-trigger steer: SteerEmployee serializes on the same per-employee
	// invoke mutex as Trigger, so this call queues the steer and then drains
	// it by re-entering Decide with Source="steer". steer_queued=true.
	steer := rpcResult(t, s, "agents.steer", map[string]any{
		"employee_id": activeEmp,
		"content":     "pivot the " + steerMark + " episode to the database lane",
	})
	if queued, _ := steer["steer_queued"].(bool); !queued {
		t.Fatalf("agents.steer mid-trigger steer_queued = %v, want true: %v", steer["steer_queued"], steer)
	}
	if err := <-done; err != nil {
		t.Fatalf("agents.trigger failed: %v", err)
	}

	// The daemon log carries the queued-steer record (source + employee).
	harness.WaitFor(t, 60*time.Second, "daemon log goal-episode steering record", func() bool {
		return strings.Contains(daemonLog(s), "goal episode steering queued") &&
			strings.Contains(daemonLog(s), activeEmp)
	})

	// The re-entered assess flow renders the steering text as a first-class
	// instruction line in the user prompt (source="steer" branch).
	harness.WaitFor(t, 60*time.Second, "steered assess turn carrying the steering text", func() bool {
		for _, body := range s.Fake.Requests() {
			raw, err := jsonMarshal(body)
			if err == nil && strings.Contains(raw, "User steering: pivot the "+steerMark) {
				return true
			}
		}
		return false
	})

	// Idle employee: no goal, no parked episode → quiet no-op, no error.
	idle := rpcResult(t, s, "agents.steer", map[string]any{
		"employee_id": idleEmp,
		"content":     "steering an idle employee is a no-op",
	})
	if queued, _ := idle["steer_queued"].(bool); queued {
		t.Fatalf("agents.steer on an idle employee steer_queued = true, want false: %v", idle)
	}
	if got := str(idle, "status"); got != "ok" {
		t.Fatalf("idle steer status = %q, want ok (no error): %v", got, idle)
	}
}
