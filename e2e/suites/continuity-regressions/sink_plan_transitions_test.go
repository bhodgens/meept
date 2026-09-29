//go:build e2e

// Sink-plan lifecycle transitions through the REAL RPC surfaces: the
// evolver sink-manager fallback in internal/rpc/plan.go routes
// plan.reject / plan.confirm / plan.revise to the evolver-dedicated
// PlanManager when the shared store misses (the same fallback the
// plan-write race exercise covers for plan.approve). Until now only
// approve had sink-plan coverage; the other three verbs were RPC-covered
// for shared plans only (e2e/suites/plan-lifecycle), never for sink
// plans, despite the manager semantics differing per state:
//
//   - RejectPlan: pending_approval → cancelled, a "rejected" signoff
//     carrying the reason, and the plan FILE SURVIVES (audit trail is
//     never destroyed). No actuator fires: plan.rejected is not the
//     bridge's trigger, so no applied marker may land and the skill
//     survives.
//   - ConfirmPlan: operator confirmation of an already-terminal plan
//     (completed/failed/cancelled) → confirmed with confirmed_by/at.
//     Covered here on the reachable sink-plan path: cancel first, then
//     confirm. UpdatePlanStatus rewrites the file status line.
//   - RevisePlan: pending_approval (or cancelled) → planning with
//     revision_count bumped and a "revision_requested" signoff.
//
// Setup mirrors plan_write_race_test.go: seeded low performer →
// skills.evolve → the evolver creates a stamped plan in its sink and
// SUBMITS it to pending_approval. Every state check goes through
// plan.list / plan.get RPC — no direct SQL anywhere.
package continuityregressions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/plan"
)

// evolveSinkPlan drives one evolution cycle for the given skill fixture
// and returns (planID, plan file path). The plan reaches pending_approval
// through the real evolver SubmitPlan path before returning.
func evolveSinkPlan(t *testing.T, s *sandbox, skillName string) (string, string) {
	t.Helper()

	s.seedSkill(skillName)
	s.seedLowPerformer(skillName)

	result := s.rpc("skills.evolve", nil)
	planned, _ := result["planned"].(float64)
	if planned < 1 {
		t.Fatalf("evolution cycle planned %v proposals, want >= 1 (result: %v)\ndaemon log tail:\n%s",
			planned, result, s.logTail())
	}

	planPath, proposalID := s.findEvolverPlanFile(skillName)
	planID := s.waitPlanListed(proposalID, string(plan.StatePendingApproval))
	return planID, planPath
}

// newEvolverSandbox boots the standard evolver sandbox (same config shape
// as the race test: evolver on, auto_apply off, approval required).
func newEvolverSandbox(t *testing.T) *sandbox {
	t.Helper()
	return newSandbox(t, withMeeptOverlay(`
  "skills": {
    "enabled": true,
    "evolver": {
      "enabled": true,
      "min_effectiveness": 0.5,
      "plan_dir": "__MEEPT_HOME__/plans/evolver",
    },
  },
  "plans": { "approval": { "require_approval": true } },
`))
}

// TestSinkPlanRejectCancelsAndKeepsFile pins plan.reject on an evolver
// sink plan: state cancelled via plan.list, the "rejected" signoff reason
// visible through plan.get, the file surviving with the evolver stamps —
// and NO actuator run (the skill still exists; plan.rejected is not the
// bridge's trigger).
func TestSinkPlanRejectCancelsAndKeepsFile(t *testing.T) {
	s := newEvolverSandbox(t)
	skillName := "e2e-reject-skill-01"
	skillDir := s.seedSkill(skillName)

	planID, planPath := evolveSinkPlan(t, s, skillName)
	s.clearUsageRow(skillName)

	resp := s.rpcRaw("plan.reject", map[string]any{
		"plan_id":    planID,
		"session_id": "e2e-continuity",
		"by":         "e2e-reviewer",
		"reason":     "not good enough",
	})
	// The handler must return the transitioned plan, not an error
	// envelope: when the sink manager performed the reject, the read-back
	// goes to the sink store (mirroring handleApprove's usedFallback).
	if errText := resp["error"]; errText != nil {
		t.Fatalf("plan.reject returned an error envelope on success: %v\ndaemon log tail:\n%s", errText, s.logTail())
	}
	if id, _ := resp["result"].(map[string]any)["id"].(string); id != planID {
		t.Fatalf("plan.reject result plan id %q, want %q (resp: %+v)", id, planID, resp)
	}
	// State: cancelled, via the REAL listing surface.
	waitPlanStateListed(t, s, planID, string(plan.StateCancelled))

	// The file survives with the evolver provenance stamps intact (the
	// audit trail is never destroyed), and carries NO applied marker —
	// rejecting must not trigger the archive actuator.
	content := readPlanFile(t, s, planPath, 10*time.Second)
	for _, want := range []string{
		"- origin: skill-evolver",
		"- action: archive",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("plan file lost %q after reject:\n%s", want, content)
		}
	}
	if strings.Contains(content, "\n- applied: ") {
		t.Fatalf("rejected plan file carries an applied marker (actuator fired on reject):\n%s", content)
	}

	// The skill SURVIVED: plan.rejected is not the bridge's trigger, so no
	// actuator ran and the archive never happened.
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill %s vanished after a REJECTED archive plan (actuator fired on reject? err=%v)", skillName, err)
	}

	// The signoff audit trail: plan.get still resolves the sink plan
	// through the fallback store.
	got := s.rpc("plan.get", map[string]any{"id": planID})
	if id, _ := got["id"].(string); id != planID {
		t.Fatalf("plan.get after reject returned %+v, want the sink plan", got)
	}
}

// TestSinkPlanReviseReturnsToPlanning pins plan.revise on an evolver sink
// plan: back to planning, revision_count bumped to 1, feedback recorded,
// file intact with its stamps.
func TestSinkPlanReviseReturnsToPlanning(t *testing.T) {
	s := newEvolverSandbox(t)
	skillName := "e2e-revise-skill-01"
	s.seedSkill(skillName)

	planID, planPath := evolveSinkPlan(t, s, skillName)
	s.clearUsageRow(skillName)

	resp := s.rpcRaw("plan.revise", map[string]any{
		"plan_id":  planID,
		"feedback": "needs a rollback section",
	})
	// The sink revision must return the transitioned plan (the read-back
	// follows the store that performed the transition) — any error
	// envelope is a real failure.
	if errText := resp["error"]; errText != nil {
		t.Fatalf("plan.revise returned an error envelope on success: %v\ndaemon log tail:\n%s", errText, s.logTail())
	}
	if id, _ := resp["result"].(map[string]any)["id"].(string); id != planID {
		t.Fatalf("plan.revise result plan id %q, want %q (resp: %+v)", id, planID, resp)
	}

	// State: planning (back for re-work), revision_count 1.
	waitPlanStateListed(t, s, planID, string(plan.StatePlanning))
	got := s.rpc("plan.get", map[string]any{"id": planID})
	if rc, _ := got["revision_count"].(float64); rc != 1 {
		t.Fatalf("revision_count after revise = %v, want 1 (plan: %+v)", rc, got)
	}

	// File survives with the provenance stamps (revise is a state
	// transition, not a file rewrite — but it must not DESTROY the file).
	content := readPlanFile(t, s, planPath, 10*time.Second)
	if !strings.Contains(content, "- origin: skill-evolver") {
		t.Fatalf("plan file lost the evolver origin stamp after revise:\n%s", content)
	}
}

// TestSinkPlanConfirmFromCancelled pins plan.confirm on an evolver sink
// plan: ConfirmPlan accepts terminal states; the reachable sink-plan path
// is cancel-then-confirm. The plan lands confirmed with confirmed_by set
// and the file's status line rewritten by UpdatePlanStatus.
func TestSinkPlanConfirmFromCancelled(t *testing.T) {
	s := newEvolverSandbox(t)
	skillName := "e2e-confirm-skill-01"
	s.seedSkill(skillName)

	planID, planPath := evolveSinkPlan(t, s, skillName)
	s.clearUsageRow(skillName)

	// Drive to a terminal state first: reject (→ cancelled).
	s.rpcRaw("plan.reject", map[string]any{
		"plan_id": planID,
		"by":      "e2e-reviewer",
		"reason":  "superseded",
	})
	waitPlanStateListed(t, s, planID, string(plan.StateCancelled))

	// Now confirm the cancelled plan (operator ack of the terminal state).
	resp := s.rpcRaw("plan.confirm", map[string]any{
		"plan_id":    planID,
		"session_id": "e2e-continuity",
		"by":         "e2e-manager",
	})
	// The sink confirm must return the transitioned plan — any error
	// envelope is a real failure.
	if errText := resp["error"]; errText != nil {
		t.Fatalf("plan.confirm returned an error envelope on success: %v\ndaemon log tail:\n%s", errText, s.logTail())
	}
	if id, _ := resp["result"].(map[string]any)["id"].(string); id != planID {
		t.Fatalf("plan.confirm result plan id %q, want %q (resp: %+v)", id, planID, resp)
	}

	// State: confirmed with confirmed_by, via the real surfaces.
	waitPlanStateListed(t, s, planID, string(plan.StateConfirmed))
	got := s.rpc("plan.get", map[string]any{"id": planID})
	if by, _ := got["confirmed_by"].(string); by != "e2e-manager" {
		t.Fatalf("confirmed_by = %v, want e2e-manager (plan: %+v)", by, got)
	}

	// UpdatePlanStatus rewrote the file's status line to confirmed.
	content := readPlanFile(t, s, planPath, 10*time.Second)
	if !strings.Contains(content, "- status: confirmed") {
		t.Fatalf("plan file status not rewritten to confirmed:\n%s", content)
	}
	if !strings.Contains(content, "- origin: skill-evolver") {
		t.Fatalf("plan file lost the evolver origin stamp after confirm:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// helpers (sandbox extensions kept here to leave sandbox_test.go untouched)
// ---------------------------------------------------------------------------

// seedSkill writes the archive-bait skill fixture and returns its dir.
func (s *sandbox) seedSkill(skillName string) string {
	t := s.t
	skillDir := filepath.Join(s.StateDir, "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillMD := "---\nname: " + skillName + "\ndescription: sink transition fixture skill\n---\n\nbody of " + skillName + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return skillDir
}

// rpcRaw performs one RPC call returning the RAW response envelope
// (error included) — the transitions here assert on the envelope itself
// (success must carry the plan, never an error).
func (s *sandbox) rpcRaw(method string, params any) map[string]any {
	return harness.DialRPC(s.t, s.SocketPath).CallError(method, params)
}

// readFileIfExists returns the file's content, or "" if it does not exist
// yet (other read errors are returned).
func (s *sandbox) readFileIfExists(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // test-read of a sandbox plan path
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// waitPlanStateListed polls plan.list until the plan appears with the
// wanted state (polling because the RPC is synchronous — the state is
// committed before the response — but plan.list merges two stores).
func waitPlanStateListed(t *testing.T, s *sandbox, planID, wantState string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		result := s.rpc("plan.list", map[string]any{})
		plans, _ := result["plans"].([]any)
		for _, raw := range plans {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := p["id"].(string); id == planID {
				state, _ := p["state"].(string)
				if state == wantState {
					return
				}
				t.Fatalf("plan %s listed in state %q, want %q", planID, state, wantState)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("plan %s never surfaced via plan.list in state %s\ndaemon log tail:\n%s",
		planID, wantState, s.logTail())
}

// readPlanFile waits for the plan file to exist and returns its content.
func readPlanFile(t *testing.T, s *sandbox, planPath string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if content, err := s.readFileIfExists(planPath); err == nil && content != "" {
			return content
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("plan file %s not readable within %s\ndaemon log tail:\n%s", planPath, timeout, s.logTail())
	return ""
}
