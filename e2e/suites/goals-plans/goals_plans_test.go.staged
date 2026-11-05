//go:build e2e

// Suite goals-plans: the 02-goals-plans branch e2e gate (G2/G3/G4 rows of
// docs/plans/20261102-dsh-convergence/02-goals-plans/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts through the live RPC/CLI
// surface, never mocks. Parked as .staged until the branch's implementation
// leaves land (voice precedent 537c92fd): activation is
// mv *_test.go.staged -> *_test.go plus the e2e/manifest.json entries
// ("goals-plans" suite + path_map for internal/employee/,
// internal/tools/builtin/goal tools) the orchestrator adds at that time.
package goals_plans

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestGoalTools_RegisteredAndAuthorityGated (G2, F4a, contract C4b):
// goal_get/goal_create/goal_update are registered model tools. goal_get
// answers before any goal exists; goal_create WITHOUT direct human
// authority is refused at the tool boundary and the refusal reaches the
// model.
func TestGoalTools_RegisteredAndAuthorityGated(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "goals-e2e")
	sessionID := s.CreateSession(t, "goal-tools", s.ProjectDir)

	// goal_get must be REGISTERED: an empty/none result, never
	// tool-not-found.
	reply := s.ChatTurn(t, sessionID,
		"Call the goal_get tool once and report its raw result verbatim.",
		3*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "no such tool") {
		t.Fatalf("goal_get not registered: %s", reply)
	}

	// goal_create without direct human authority must be refused (C4b
	// authority gate) and the refusal must surface to the model.
	reply = s.ChatTurn(t, sessionID,
		"Call the goal_create tool with objective \"probe objective\" and report its raw result verbatim.",
		3*time.Minute)
	lower = strings.ToLower(reply)
	if !strings.Contains(lower, "authority") && !strings.Contains(lower, "denied") &&
		!strings.Contains(lower, "requires") && !strings.Contains(lower, "permission") {
		t.Errorf("goal_create without direct human authority was not refused: %s", reply)
	}
}

// TestRoundDriver_AutonomousContinuationAndAutoBlock (G3, F4b, contract C4c):
// a goal created with max rounds = 2 advances to round 2 with ZERO human
// turns after quiescence, then blocks with code round-limit. The human
// (test) creates the goal once through the CLI - authority is direct; from
// then on no human turn is submitted.
func TestRoundDriver_AutonomousContinuationAndAutoBlock(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		// Keep the loop deterministic: no cloud, scratch classifier off.
		"llm.session_affinity": false,
	}))
	s.RegisterProject(t, "rounds-e2e")

	// The employee must exist before a goal can bind to it. The CLI
	// surface (AGENTS.md): meept agents goal-create <employee-id>
	// --title T --mandate M [--max-rounds N].
	out, errOut := s.RunCLI(t, 60*time.Second, true,
		"agents", "goal-create", "e2e-round-probe",
		"--title", "round-cap-probe",
		"--mandate", "Reply with the current round number only.",
		"--max-rounds", "2")
	if strings.Contains(strings.ToLower(out+errOut), "error") &&
		!strings.Contains(strings.ToLower(out+errOut), "no such employee") {
		t.Fatalf("agents goal-create: %s %s", out, errOut)
	}

	// Zero human turns from here: poll goal state through the CLI until
	// it blocks with round-limit (the driver runs on quiescence/idle).
	deadline := time.Now().Add(5 * time.Minute)
	for {
		out, _ = s.RunCLI(t, 30*time.Second, true, "agents", "goals")
		if strings.Contains(out, "round-cap-probe") &&
			strings.Contains(out, "blocked") && strings.Contains(out, "round-limit") {
			return // auto-blocked at the cap with the right reason code
		}
		if time.Now().After(deadline) {
			t.Fatalf("goal never auto-blocked at MaxRounds=2; last state:\n%s", out)
		}
		time.Sleep(2 * time.Second)
	}
}

// TestPlanMode_PresentForReviewApprove (G4, F5, contract C4d):
// /plan enters plan mode; exit_plan_mode with a markdown "#"-headed plan
// presents Approve/Keep-planning; Approve lands the plan through the
// EXISTING plan gate (internal/plan Manager.ApprovePlan) and plan mode
// exits. Plan mode is DISTINCT from goal mode.
func TestPlanMode_PresentForReviewApprove(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "planmode-e2e")
	sessionID := s.CreateSession(t, "plan-mode", s.ProjectDir)

	// /plan must be a registered command (not "unknown command").
	reply := s.ChatTurn(t, sessionID, "/plan", 30*time.Second)
	if strings.Contains(strings.ToLower(reply), "unknown command") {
		t.Fatalf("/plan command not registered: %s", reply)
	}

	// Drive a plan presentation: the model must call exit_plan_mode with a
	// markdown plan and the daemon must surface the Approve decision.
	reply = s.ChatTurn(t, sessionID,
		"Create a one-step plan with the heading \"# probe plan\" and present it for review using the exit_plan_mode tool.",
		3*time.Minute)
	if !strings.Contains(strings.ToLower(reply), "approve") {
		t.Fatalf("no Approve/Keep-planning decision presented: %s", reply)
	}

	// Approve via the EXISTING plan gate CLI: meept plans list / approve <id>.
	out, errOut := s.RunCLI(t, 60*time.Second, true, "plans", "list")
	combined := out + errOut
	if !strings.Contains(combined, "probe plan") {
		t.Fatalf("presented plan missing from plans list:\n%s", combined)
	}
	planID := firstFieldOfLineContaining(combined, "probe plan")
	if planID == "" {
		t.Fatalf("no plan id on the 'probe plan' row:\n%s", combined)
	}
	out, errOut = s.RunCLI(t, 60*time.Second, true, "plans", "approve", planID)
	if strings.Contains(strings.ToLower(out+errOut), "error") {
		t.Fatalf("plans approve %s: %s %s", planID, out, errOut)
	}

	// The plan row moved to an approved state.
	out, _ = s.RunCLI(t, 60*time.Second, true, "plans", "show", planID)
	if !strings.Contains(strings.ToLower(out), "approved") {
		t.Errorf("plan %s not in approved state after approve:\n%s", planID, out)
	}

	// Plan mode is OFF after approval: the next turn is a normal chat turn.
	reply = s.ChatTurn(t, sessionID, "Reply with exactly: OK", 30*time.Second)
	if strings.Contains(strings.ToLower(reply), "plan mode") &&
		strings.Contains(strings.ToLower(reply), "active") {
		t.Errorf("plan mode still active after approval: %s", reply)
	}
}

// firstFieldOfLineContaining returns the first whitespace-delimited field of
// the first line containing needle (the plan id column), or "".
func firstFieldOfLineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}
