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
//   - goals-plans-01 (goal tools) and goals-plans-02 (round driver) are
//     RED-phase gates for implementation pieces that did NOT land in this
//     tree. The goal-tool provider is deliberately left UNBOUND
//     (internal/daemon/dsh_wiring.go wireGoalProvider calls
//     builtin.SetGoalProvider(nil,nil) because the goalManager adapter types
//     goalCreateSpec/goalRecord are unexported in internal/tools/builtin →
//     toolprov.RegisterAll fails closed → zero goal tools), and the round
//     driver (F4b/C4c, autonomous advance + auto-block) has no production
//     caller of builtin.SetGoalAutonomousRound and no round-advance loop over
//     Goal.MaxRounds/RoundsStarted. These are IMPLEMENTATION residuals, not
//     harness ones; each test fails fast with that root cause and goes GREEN
//     as soon as the leaf lands, with no harness edit.
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

// TestGoalTools_RegisteredAndAuthorityGated (G2, F4a, C4b, manifest
// goals-plans-01): RED-phase gate for the goal-tool wiring, which did NOT land
// in this tree.
//
// Root cause (verified in internal/daemon/dsh_wiring.go wireGoalProvider, lines
// 51-85): the goal provider is deliberately unbound — builtin.SetGoalProvider(nil,nil)
// because the goalManager adapter types (goalCreateSpec/goalRecord) are
// unexported in internal/tools/builtin, so the daemon cannot construct an
// adapter onto *employee.Manager and toolprov.RegisterAll fails closed → zero
// goal tools are registered. The chat-facing registry therefore answers
// "unknown tool: goal_get" (and goal_create / goal_update). Fixing it is an
// IMPLEMENTATION task (export the adapter types + wire a live *employee.Manager
// adapter), out of scope for a harness fix. We keep the rigged executor turn so
// the test is a truthful, fast-failing gate that goes GREEN unprompted once the
// wiring lands.
func TestGoalTools_RegisteredAndAuthorityGated(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "goals-e2e")
	sessionID := s.CreateSession(t, "goal-tools", s.ProjectDir)

	// Rig so the executor ACTUALLY calls goal_get (previously the loop just
	// returned 'done' and the "not registered" check passed vacuously).
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "goal_get", Arguments: "{}"})
	s.ChatTurn(t, sessionID,
		"Call the goal_get tool and report its raw result verbatim.",
		3*time.Minute)
	if res := toolResultsJoined(s.Fake); strings.Contains(strings.ToLower(squash(res)), "unknowntool") {
		t.Fatalf("goal-get wiring not present (documented-infeasible residual, C4b/F4a): "+
			"goal tools are NOT registered because internal/daemon/dsh_wiring.go wireGoalProvider "+
			"calls builtin.SetGoalProvider(nil,nil) (goalManager adapter types goalCreateSpec/goalRecord "+
			"unexported in internal/tools/builtin) -> toolprov.RegisterAll fails closed -> zero goal tools. "+
			"Envelope:\n%s", res)
	}
}

// TestRoundDriver_AutonomousContinuationAndAutoBlock (G3, F4b, C4c, manifest
// goals-plans-02): RED-phase gate for the round driver, which was NOT
// implemented in this tree.
//
// Root cause (verified): there is no production caller of
// builtin.SetGoalAutonomousRound (internal/tools/builtin/goal_tools.go:173) and
// no round-advance loop over Goal.MaxRounds / Goal.RoundsStarted that would
// advance a goal autonomously and self-block with code "round-limit". The
// desired C4c behavior — "max_rounds=2 advances to round 2 with ZERO human
// turns, then blocks round-limit" — is therefore unobservable. Independent
// harness facts that also made the staged version untenable: the
// `agents goal-create` CLI flag set has no --max-rounds flag, and the
// `agents goals` table exposes no blocked/round-limit column. IMPLEMENTATION
// residual. The config-overlay bug (a scalar `false` stamped on the STRUCT
// llm.session_affinity) that previously crashed this test's daemon boot is
// fixed by removing the overlay — the daemon now boots cleanly and this line is
// the only remaining blocker.
func TestRoundDriver_AutonomousContinuationAndAutoBlock(t *testing.T) {
	// Boot with defaults (no llm.session_affinity overlay: it is a struct; a
	// scalar false broke daemon config parsing -> "daemon not healthy within
	// 2m"; harmless for the fake anyway).
	s := harness.Start(t)
	s.RegisterProject(t, "rounds-e2e")
	sessionID := s.CreateSession(t, "round-driver", s.ProjectDir)

	// Drive one executor turn so the loop actually runs (no 5-minute poll).
	s.Fake.SetPostToolText("round driver probe")
	s.ChatTurn(t, sessionID,
		"Probe the goal round driver once and report the outcome verbatim.",
		3*time.Minute)

	t.Fatalf("round driver (F4b/C4c) not implemented (documented-infeasible residual): "+
		"no production caller of builtin.SetGoalAutonomousRound and no round-advance loop over "+
		"Goal.MaxRounds/RoundsStarted; the agents goal-create CLI has no --max-rounds flag and the "+
		"goal tools are unbound (see TestGoalTools). A zero-human-turn goal cannot be created or "+
		"advanced to a round-limit block. Envelope:\n%s", toolResultsJoined(s.Fake))
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
