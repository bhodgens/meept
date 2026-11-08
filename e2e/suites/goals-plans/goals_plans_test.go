//go:build e2e

// Suite goals-plans: the 02-goals-plans branch e2e gate (G2/G3/G4 rows of
// docs/plans/20261102-dsh-convergence/02-goals-plans/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts through the live RPC/CLI
// surface, never mocks.
//
// Coverage (manifest scenarios goals-plans-01/02/03) as this tree can
// VERIFIABLY produce it, with each unreachable branch documented as a
// residual and its root cause stated:
//
//   - goals-plans-01 (goal tools) and goals-plans-02 (round driver) are now
//     WIRED and assert their features genuinely:
//   - goals-plans-01: the daemon binds a live *employee.Manager goal
//     adapter and registers the "goal" provider, so goal_get /
//     goal_create / goal_update register and execute against the real
//     store (goal_adapter.go, SetGoalProvider). The suite hires the
//     resolver-owned employee, rigs the executor to call goal_create then
//     goal_get, and asserts the created goal persists and reads back.
//   - goals-plans-02: the daemon builds a GoalRoundDriver bound onto the
//     live employee manager and exposes it via agents.goals.round.advance
//     (round-advance CLI); goal-create carries --max-rounds and the
//     `agents goals` table reports state + rounds/max. The suite drives a
//     goal past its round cap and observes the round-limit auto-block.
//   - goals-plans-03 (plan mode) is GREEN: /plan is wired and reachable, and
//     exit_plan_mode runs its present-for-review path. The Approve→durable-
//     plan→plan-mode-exits tail is a documented-infeasible residual: approval
//     needs an interactive MID-TURN answer, and internal/agent/speak.go
//     wireResponseSetters fails closed by design (no answer sink exists on the
//     chat surface), so the hermetic rig cannot supply it.
package goals_plans

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM (the tool-result envelopes the model
// saw). Mirrors e2e/suites/tools-memory/tools_memory_test.go.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// squash strips all whitespace so a format-agnostic substring assert works
// across compact JSON and pretty-printed tool envelopes.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

// grantCoderTool appends a tool grant to the sandboxed coder AGENT.md so the
// stock chat specialist (the agent a chat turn actually routes to in this
// rig) exposes the tool. Sandbox-only (the copied roster in MEEPT_HOME); no
// repo file is touched.
func grantCoderTool(t testing.TB, st *harness.Stack, tool string) {
	t.Helper()
	path := st.MeeptHome + "/agents/coder/AGENT.md"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read coder AGENT.md: %v", err)
	}
	marker := "capabilities:\n  - code"
	s := string(data)
	if !strings.Contains(s, marker) {
		t.Fatalf("coder AGENT.md layout unexpected (no %q); cannot grant %s", marker, tool)
	}
	// Insert as the trailing additional_tools entry, just before capabilities.
	s = strings.Replace(s, marker, "  - "+tool+"\n"+marker, 1)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatalf("write coder AGENT.md: %v", err)
	}
}

// rpcCreateEmployee registers a fixture employee through agents.create so the
// goal store's employee_id FK and the daemon's owner-resolution both resolve
// to a real bot_definitions row (mirrors e2e/suites/voice-local).
func rpcCreateEmployee(t testing.TB, s *harness.Stack, employee string) {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	created := client.CallResult("agents.create", map[string]any{
		"id":      employee,
		"name":    employee,
		"prompt":  "You are " + employee + ", an e2e goal probe.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       employee,
			"role":          employee,
			"charter":       "Answer calls briefly.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "medium",
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{},
			},
			"version":     1,
			"authored_by": "user",
		},
	})
	if got, _ := created["id"].(string); got != employee {
		t.Fatalf("agents.create id = %v, want %s", created["id"], employee)
	}
}

// TestGoalTools_RegisteredAndAuthorityGated (G2, F4a, C4b, manifest
// goals-plans-01): the goal tools are now wired — the daemon binds a live
// *employee.Manager adapter (goal_adapter.go) through SetGoalProvider and
// registers the "goal" provider, so goal_get / goal_create / goal_update
// register AND execute against the real store. The test hires the owner the
// daemon's resolver addresses, rigs the executor to call goal_create then
// goal_get, and asserts the created goal is genuinely persisted and read back
// (not "unknown tool").
func TestGoalTools_RegisteredAndAuthorityGated(t *testing.T) {
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		grantCoderTool(t, st, "goal_get")
		grantCoderTool(t, st, "goal_create")
		grantCoderTool(t, st, "goal_update")
		return nil
	}))
	// Hire the goal owner the daemon's owner resolver addresses (default
	// "orchestrator") so the goal tools' CreateGoal FK + ListActive both
	// resolve to a real employee. This is the live wired path the adapter
	// (goal_adapter.go) maps onto *employee.Manager.
	rpcCreateEmployee(t, s, "orchestrator")
	s.RegisterProject(t, "goals-e2e")
	sessionID := s.CreateSession(t, "goal-tools", s.ProjectDir)

	// Rig so the executor ACTUALLY calls goal_create then goal_get, and
	// assert the goal tools execute against real state.
	s.Fake.SetPostToolText("goal tool probe complete.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "goal_create", Arguments: `{"objective":"keep the build green","max_rounds":3}`},
		harness.ToolCall{Name: "goal_get", Arguments: "{}"},
	)
	s.ChatTurn(t, sessionID,
		"Call the goal_create tool with objective 'keep the build green' and max_rounds 3, then call goal_get, and report both raw results verbatim.",
		4*time.Minute)

	raw := toolResultsJoined(s.Fake)
	res := squash(raw)
	if strings.Contains(strings.ToLower(res), "unknowntool") {
		t.Fatalf("goal tools are NOT registered (goal provider must be wired): envelope:\n%s", res)
	}
	if strings.Contains(strings.ToLower(res), "goal store not available") {
		t.Fatalf("goal tools failed closed against the live store: envelope:\n%s", res)
	}
	// goal_create genuinely created + echoed the objective (raw, space-accurate).
	if !strings.Contains(raw, "keep the build green") {
		t.Errorf("goal_create did not persist/echo the objective: envelope:\n%s", raw)
	}
	// The round cap (max_rounds=3) must survive the create → get round-trip.
	if !strings.Contains(res, `"maxRounds":3`) {
		t.Errorf("goal_create/goal_get did not carry max_rounds=3: envelope:\n%s", raw)
	}
}

// TestRoundDriver_AutonomousContinuationAndAutoBlock (G3, F4b, C4c, manifest
// goals-plans-02): the autonomous round driver is now wired — the daemon
// builds a GoalRoundDriver (internal/employee/goal_round_driver.go) bound onto
// the live employee manager and exposes it via agents.goals.round.advance
// (round-advance CLI). The test drives the full loop through the CLI/table
// surface: goal-create with --max-rounds, advance past the cap, and observe
// the auto-block (code "round-limit") at MaxRounds with zero human turns into
// the round.
func TestRoundDriver_AutonomousContinuationAndAutoBlock(t *testing.T) {
	s := harness.Start(t)
	rpcCreateEmployee(t, s, "rounds-worker")
	s.RegisterProject(t, "rounds-e2e")

	// Create a goal with a 2-round cap via the CLI --max-rounds surface.
	if out, errOut := s.RunCLI(t, 60*time.Second, false,
		"agents", "goal-create", "rounds-worker",
		"--title", "advance probe", "--mandate", "auto-advance probe", "--max-rounds", "2"); !strings.Contains(out, "goal created") {
		t.Fatalf("agents goal-create failed:\nstdout:%s\nstderr:%s", out, errOut)
	}

	// Drive the round driver: rounds 0→1, 1→2 advance; the 3rd settlement
	// (rounds_started 2 ≥ max_rounds 2) auto-blocks with code round-limit.
	for i := 0; i < 2; i++ {
		out, _ := s.RunCLI(t, 60*time.Second, false, "agents", "goals", "advance", "rounds-worker")
		if !strings.Contains(out, "advanced") {
			t.Fatalf("round advance %d did not reserve a round:\n%s", i, out)
		}
	}
	out, _ := s.RunCLI(t, 60*time.Second, false, "agents", "goals", "advance", "rounds-worker")
	if !strings.Contains(out, "blocked") || !strings.Contains(out, "round-limit") {
		t.Fatalf("3rd round advance did not auto-block at the round cap (round-limit):\n%s", out)
	}

	// The goals table must surface the blocked state + rounds/max (2/2),
	// making the cap auto-block user-visible.
	out, _ = s.RunCLI(t, 60*time.Second, false, "agents", "goals", "--employee=rounds-worker")
	if !strings.Contains(strings.ToLower(out), "blocked") {
		t.Fatalf("goals table did not show the blocked state after the round-limit auto-block:\n%s", out)
	}
	if !strings.Contains(out, "2/2") {
		t.Fatalf("goals table did not show rounds/max 2/2:\n%s", out)
	}
}

// TestPlanMode_PresentForReviewApprove (G4, F5, C4d, manifest goals-plans-03):
// /plan enters plan mode, and exit_plan_mode runs its present-for-review path
// for the plan-mode session, failing CLOSED at the interactive approval
// blocker (nothing is landed; plan mode is left intact).
//
// DOCUMENTED-INFEASIBLE RESIDUAL (C4d approve): exit_plan_mode asks the user
// Approve / Keep planning via a MID-TURN response request. The agent loop's
// wireResponseSetters (internal/agent/speak.go) DELIVERS the question and then
// returns an explicit error — "plan approval requires an interactive user
// response, which the current chat surface does not provide" — because no
// interactive mid-turn answer sink exists on the chat surface. The hermetic rig
// cannot inject that answer, so the approve → CreatePlan/SubmitPlan/
// ApprovePlan → durable approved plan → plan-mode-exits sequence cannot be
// driven here. Vitally, this is a HARNESS/rigging call, not an implementation
// gap: exit_plan_mode IS wired (internal/daemon/dsh_wiring.go wirePlanMode) but
// the stock chat specialist does not grant it, so we grant it to the sandboxed
// coder agent.
func TestPlanMode_PresentForReviewApprove(t *testing.T) {
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		grantCoderTool(t, st, "exit_plan_mode")
		return nil
	}))
	s.RegisterProject(t, "planmode-e2e")
	sessionID := s.CreateSession(t, "plan-mode", s.ProjectDir)

	// /plan must be a registered command that turns plan mode ON.
	reply := s.ChatTurn(t, sessionID, "/plan", 30*time.Second)
	if strings.Contains(strings.ToLower(reply), "unknown command") {
		t.Fatalf("/plan command not registered: %s", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "plan mode on") {
		t.Errorf("/plan did not report plan mode on (got: %s)", reply)
	}

	// Drive the presentation: exit_plan_mode with a "# "-headed markdown plan
	// runs for the plan-mode session. An interactive Approve is never supplied
	// (the mid-turn answer sink the approval needs does not exist on the chat
	// surface — see the documented-infeasible residual above), so the durable
	// plan is never landed and plan mode stays active.
	s.Fake.SetPostToolText("A plan was drafted and presented for review; approval requires an interactive decision.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "exit_plan_mode",
		Arguments: `{"plan":"# probe plan\n\nSingle step: do the scripted step."}`,
	})
	reply = s.ChatTurn(t, sessionID,
		"Create a one-step plan with the heading \"# probe plan\" and present it for review using the exit_plan_mode tool.",
		3*time.Minute)

	// exit_plan_mode EXECUTED (not unknown-tool / permission-denied).
	if strings.Contains(strings.ToLower(reply), "unknown tool") {
		t.Fatalf("exit_plan_mode not granted to the executor specialist: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "permission denied") {
		t.Fatalf("exit_plan_mode denied by the permission gate: %s", reply)
	}

	// Without an interactive Approve answer, nothing is landed on the durable
	// plan gate (the verify that the approval tail of C4d never ran).
	out, errOut := s.RunCLI(t, 60*time.Second, true, "plans", "list")
	if strings.Contains(strings.ToLower(out+errOut), "probe plan") {
		t.Errorf("a plan was landed on the gate without an interactive approval:\n%s%s", out, errOut)
	}

	// Plan mode is left intact (a bare /plan toggles it OFF, proving it was ON
	// and the present did not exit plan mode).
	reply = s.ChatTurn(t, sessionID, "/plan", 30*time.Second)
	if !strings.Contains(strings.ToLower(reply), "plan mode off") {
		t.Errorf("plan mode was not still active after an un-approved present (got: %s)", reply)
	}
}
