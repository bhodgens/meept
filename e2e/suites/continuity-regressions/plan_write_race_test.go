//go:build e2e

// Plan-write serialization regressions (commit 9f503122): the evolver
// approval path runs TWO concurrent read-modify-write rewriters on the same
// plan.md — PlanManager.ApprovePlan's Synthesize (UpdatePlanStatus →
// executing status + phase states) and the approval bridge's actuator
// (markPlanApplied → the durable `- applied:` marker). Before the fix,
// non-atomic writes / a shared scratch name / unordered renames let one
// side clobber the other (the applied marker vanished while both writers
// reported success; the bridge classified a partially-written file as
// non-evolver and dropped the event).
//
// The scenario drives the REAL daemon path end to end, N times: seed a low
// performer into skills.db + a skill fixture → `skills.evolve` → pass C
// proposes the archive → a stamped plan is created in the evolver sink and
// auto-approved (RequireApproval=false) → bridge + Synthesize race. After
// each iteration the plan file must contain BOTH the origin stamp AND the
// applied marker AND the executing status — neither writer's effect lost.
// The applied marker landing is also the proof the actuator trigger was
// not dropped.
package continuityregressions

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// planWriteIterations is the race-loop size. The original bug reproduced
// 1-in-30 at -cpu=2, so a single approve proves nothing; the interleaving
// is looped in-test. Each iteration is a fresh skill+plan, so every pass
// exercises the full approve→(bridge ∥ synthesize) interleaving.
const planWriteIterations = 10

// TestPlanWriteSerialization_ApproveRaceKeepsOriginAndAppliedMarker runs
// the evolver approve path in a loop and asserts the conjunction invariant
// on the plan file after every iteration. Manifest scenarios
// continuity-regressions-01..02.
func TestPlanWriteSerialization_ApproveRaceKeepsOriginAndAppliedMarker(t *testing.T) {
	// Evolver on, auto_apply off (the default): proposals become stamped
	// plans; plans auto-approve (plans.approval.require_approval default
	// false) which is exactly the concurrent approve the fix targets.
	s := newSandbox(t, withMeeptOverlay(`
  "skills": {
    "enabled": true,
    "evolver": {
      "enabled": true,
      "min_effectiveness": 0.5,
      "plan_dir": "__MEEPT_HOME__/plans/evolver",
    },
  },
  "plans": { "approval": { "require_approval": false } },
`))

	for i := range planWriteIterations {
		skillName := fmt.Sprintf("e2e-archive-skill-%02d", i)
		// The daemon derives the proposal id itself:
		// evo-<action>-<sanitized skill name>-<process-wide seq> — the
		// test matches the skill-bearing prefix, never the sequence.

		// 1. Skill fixture in the Writer's own root (legacy tier fallback:
		// the skill lives in no discovery tier, so the Writer resolves it
		// to <data_dir>/skills/<name>/SKILL.md and archives in place).
		skillDir := filepath.Join(s.StateDir, "skills", skillName)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("iter %d: mkdir skill dir: %v", i, err)
		}
		skillMD := "---\nname: " + skillName + "\ndescription: plan-write race fixture skill\n---\n\nbody of " + skillName + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
			t.Fatalf("iter %d: write SKILL.md: %v", i, err)
		}

		// 2. Seed a low performer: inject_count >= ArchiveMinInjections
		// (10) and effectiveness < min_effectiveness (0.5) — the pass C
		// prune gate — into the tracker's skills.db.
		s.seedLowPerformer(skillName)

		// 3. Run one evolution cycle synchronously: pass C proposes the
		// archive, the sink plan is created, stamped, submitted, and
		// AUTO-APPROVED — publishing plan.approved (the bridge's trigger)
		// and running Synthesize's UpdatePlanStatus rewrite concurrently
		// with the bridge's markPlanApplied. This is the race.
		result := s.rpc("skills.evolve", nil)
		planned, _ := result["planned"].(float64)
		if planned < 1 {
			t.Fatalf("iter %d: evolution cycle planned %v proposals, want >= 1 (result: %v)\ndaemon log tail:\n%s",
				i, planned, result, s.logTail())
		}

		// 4. Locate the plan the cycle created (sink dir) and move it into
		// pending_approval — the human-gate state ApprovePlan's conditional
		// transition requires. (The evolver parks plans in draft; the
		// production CLI has no submit surface for sink plans, so the test
		// plays the gate: the thing under test is the CONCURRENT approve —
		// event → bridge actuator ∥ Synthesize rewrite — not the state
		// bookkeeping.)
		planPath, proposalID := s.findEvolverPlanFile(skillName)
		s.setPlanStatePending(proposalID)

		// 5. Approve through the REAL RPC surface: PlanManager.ApprovePlan
		// publishes plan.approved (the bridge's trigger) and runs
		// Synthesize's UpdatePlanStatus rewrite CONCURRENTLY with the
		// bridge's markPlanApplied. This is the race.
		s.rpc("plan.approve", map[string]any{
			"plan_id":    s.planIDFor(proposalID),
			"session_id": "e2e-continuity",
			"by":         "e2e",
		})

		// 6. Quiesce: the applied marker must land (the actuator ran —
		// the plan.approved event was NOT dropped).
		content := s.waitPlanApplied(planPath, 30*time.Second)

		// 7. The conjunction invariant: BOTH writers' effects survived.
		if !strings.Contains(content, "- origin: skill-evolver") {
			t.Fatalf("iter %d: plan file lost the evolver origin stamp (synthesis clobbered provenance):\n%s", i, content)
		}
		if !strings.Contains(content, "- proposal_id: "+proposalID) {
			t.Fatalf("iter %d: plan file lost the proposal stamp:\n%s", i, content)
		}
		if !strings.Contains(content, "- action: archive") {
			t.Fatalf("iter %d: plan file lost the action stamp:\n%s", i, content)
		}
		if !strings.Contains(content, "\n- applied: ") {
			t.Fatalf("iter %d: plan file lost the applied marker (actuator write clobbered):\n%s", i, content)
		}
		if !strings.Contains(content, "- status: executing") {
			t.Fatalf("iter %d: plan file lost the executing status (actuator write clobbered the synthesis rewrite):\n%s", i, content)
		}

		// 8. The actuator actually archived the skill (leaf-02 semantics —
		// proves the marker is not a false positive).
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Fatalf("iter %d: skill %s still present after archived application (err=%v)", i, skillName, err)
		}

		// 9. Retire the usage row so the next iteration's cycle only
		// proposes the next skill (GetLowPerformers is DB-driven).
		s.clearUsageRow(skillName)
	}
}

// setPlanStatePending moves the sink plan row from draft into
// pending_approval (the human gate the approve RPC's conditional transition
// requires). The evolver parks plans in draft and no CLI surface submits
// sink plans, so the test plays the gate operator. Direct SQL against the
// sink store — the same fixture-seaming class as the skills.db seeding.
func (s *sandbox) setPlanStatePending(proposalID string) {
	t := s.t
	planID := s.planIDFor(proposalID)
	dbPath := filepath.Join(s.StateDir, "plans-evolver.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open plans-evolver.db: %v", err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE plans SET state = 'pending_approval' WHERE id = ? AND state = 'draft'`, planID)
	if err != nil {
		t.Fatalf("move plan %s to pending_approval: %v", planID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("plan %s not moved to pending_approval (rows=%d)", planID, n)
	}
}

// planIDFor recovers the sink plan's id for a proposal id by matching the
// stamped plan files under the evolver plan dir.
func (s *sandbox) planIDFor(proposalID string) string {
	t := s.t
	sinkDir := filepath.Join(s.MeeptHome, "plans", "evolver")
	entries, err := os.ReadDir(sinkDir)
	if err != nil {
		t.Fatalf("read plan sink: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sinkDir, entry.Name())) //nolint:gosec // test-read of a sandbox plan path
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- plan_id: ") && strings.Contains(string(data), "- proposal_id: "+proposalID) {
				return strings.TrimSpace(strings.TrimPrefix(line, "- plan_id: "))
			}
		}
	}
	t.Fatalf("no plan_id stamp found for proposal %s under %s", proposalID, sinkDir)
	return ""
}

// seedLowPerformer inserts one skill_usage row into the tracker's DB
// (created by the daemon at boot; opened read-write from the test process
// with the same pure-Go driver the daemon uses).
func (s *sandbox) seedLowPerformer(skillName string) {
	t := s.t
	dbPath := filepath.Join(s.StateDir, "skills.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open skills.db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT OR REPLACE INTO skill_usage
		(skill_name, inject_count, positive_count, negative_count, neutral_count, last_injected_at, last_used_at, effectiveness)
		VALUES (?, 15, 0, 1, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0.05)`, skillName)
	if err != nil {
		t.Fatalf("seed skill_usage row for %s: %v", skillName, err)
	}
}

// clearUsageRow removes the seeded row so a later cycle cannot re-propose
// the (already archived) skill.
func (s *sandbox) clearUsageRow(skillName string) {
	t := s.t
	dbPath := filepath.Join(s.StateDir, "skills.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open skills.db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM skill_usage WHERE skill_name = ?`, skillName); err != nil {
		t.Fatalf("clear skill_usage row for %s: %v", skillName, err)
	}
}

// findEvolverPlanFile polls the evolver plan sink directory for the archive
// plan carrying the given skill name in its proposal id, and returns
// (file path, proposal id).
// NOTE: deliberately not via `plan.list` — the sink store stores an empty
// project id as NULL while ListPlans filters `WHERE project_id = ?` with
// the empty string, so sink plans never surface there (observed while
// writing this suite; flagged to the repo as a separate finding).
func (s *sandbox) findEvolverPlanFile(skillName string) (string, string) {
	t := s.t
	sinkDir := filepath.Join(s.MeeptHome, "plans", "evolver")
	marker := "- proposal_id: evo-archive-" + skillName + "-"
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(sinkDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				fp := filepath.Join(sinkDir, entry.Name())
				data, err := os.ReadFile(fp) //nolint:gosec // test-read of a sandbox plan path
				if err != nil || !strings.Contains(string(data), marker) {
					continue
				}
				// Recover the full proposal id ("evo-archive-<skill>-<seq>")
				// from the stamped line for the provenance assertion.
				proposalID := ""
				for _, line := range strings.Split(string(data), "\n") {
					if after, ok := strings.CutPrefix(strings.TrimSpace(line), "- proposal_id: "); ok {
						proposalID = strings.TrimSpace(after)
						break
					}
				}
				return fp, proposalID
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no plan file carrying proposal %q appeared in %s\ndaemon log tail:\n%s", marker, sinkDir, s.logTail())
	return "", ""
}
