package metrics

// M11 (bughunt 2026-11-27, T-5) pins for the agent-dimension breakdown
// window index idx_mrv_breakdown_agent_window. Mirrors the L5 pin shape in
// review_verdicts_plan_test.go: the audit's probe showed the dim=agent
// breakdown planning through idx_mrv_model_ts with a
// "USE TEMP B-TREE FOR GROUP BY" (equality columns reviewed_model_id,
// agent_id never both covered by one index); the new index removes the
// re-sort the same way idx_mrv_breakdown_window does for category.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newAgentPlanStore seeds a small store whose rows carry agent_id values
// (the L6 column the agent dimension groups on) and ANALYZEs, mirroring
// the stats a long-lived DB accumulates. Unlike the L5 fixture this does
// not need 20k rows: the pinned contract is index USE (the plan must
// reference the agent window index), and the temp-GROUP-BY absence is
// asserted as a teeth-checked positive exactly like the L5 pin's contract
// (issue #178: the contract is the access-path upgrade).
func newAgentPlanStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	tx, err := store.db.Beginx()
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	i := 0
	for m := 0; m < 6; m++ {
		for a := 0; a < 6; a++ {
			for k := 0; k < 40; k++ {
				ts := now.Add(-time.Duration(i) * time.Minute)
				if _, err := tx.Exec(
					`INSERT INTO model_review_verdicts
						(timestamp, step_id, task_id, cycle, is_final, rubric_id,
						 scope, category, severity, reviewed_model_id, agent_id,
						 judge_agent_id, rubric_version)
					 VALUES (?, ?, 'seed-task', 1, 1, 'MR-LC1', 'model', 'logic_error',
					         'blocking', ?, ?, 'j-1', '1')`,
					ts.Format(time.RFC3339), fmt.Sprintf("seed-a-%d", i),
					fmt.Sprintf("m-%02d", m), fmt.Sprintf("agent-%02d", a),
				); err != nil {
					t.Fatalf("seed row %d: %v", i, err)
				}
				i++
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
	if _, err := store.db.Exec(`ANALYZE`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}
	return store
}

// agentPlanDetail summarizes the plan rows relevant to the agent-dimension
// pin.
type agentPlanDetail struct {
	usesAgentIndex  bool
	groupByTempTree bool
}

func summarizeAgentPlan(details []string) agentPlanDetail {
	var p agentPlanDetail
	for _, d := range details {
		if strings.Contains(d, "idx_mrv_breakdown_agent_window") {
			p.usesAgentIndex = true
		}
		if strings.Contains(d, "TEMP B-TREE FOR GROUP BY") {
			p.groupByTempTree = true
		}
	}
	return p
}

// TestVR_BREAKDOWN_PLAN_3_AgentBreakdownUsesAgentIndex is the M11 pin: the
// dim=agent breakdown SQL must plan through idx_mrv_breakdown_agent_window,
// and with the index dropped (stats rebuilt) the identical query must
// regress to a temp GROUP BY B-tree — the exact defect shape the audit
// probe recorded ("SEARCH ... USING INDEX idx_mrv_model_ts | USE TEMP
// B-TREE FOR GROUP BY"). Without the teeth half, a plan pin that only
// asserts index presence could pass with the index never consulted.
func TestVR_BREAKDOWN_PLAN_3_AgentBreakdownUsesAgentIndex(t *testing.T) {
	store := newAgentPlanStore(t)
	since := time.Time{} // zero since: full-table window, range bound trivially satisfiable
	query, args := reviewVerdictBreakdownSQL(since, "agent")

	details := explainBreakdown(t, store.db, query, args...)
	t.Logf("with-agent-index plan:\n  %s", strings.Join(details, "\n  "))
	p := summarizeAgentPlan(details)
	if !p.usesAgentIndex {
		t.Errorf("agent breakdown plan does not use idx_mrv_breakdown_agent_window: %v", details)
	}
	if p.groupByTempTree {
		t.Errorf("agent breakdown plan pays USE TEMP B-TREE FOR GROUP BY (the T-5 defect): %v", details)
	}

	// Results unchanged by the access path: the breakdown over the seeded
	// fixture must return every (model, agent) pair with its full count.
	rows, err := store.QueryReviewVerdictModelAgentBreakdown(since)
	if err != nil {
		t.Fatalf("QueryReviewVerdictModelAgentBreakdown: %v", err)
	}
	if len(rows) != 6*6 {
		t.Fatalf("breakdown pairs = %d, want 36 (6 models x 6 agents)", len(rows))
	}
	for _, r := range rows {
		if r.Count != 40 {
			t.Errorf("pair (%s, %s) count = %d, want 40", r.Model, r.Agent, r.Count)
		}
	}

	// Teeth: drop the agent index, rebuild stats — the identical query must
	// regress to the temp GROUP BY B-tree. If this passes anyway, the
	// positive assertions above are testing nothing.
	if _, err := store.db.Exec(`DROP INDEX IF EXISTS idx_mrv_breakdown_agent_window`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if _, err := store.db.Exec(`ANALYZE`); err != nil {
		t.Fatalf("re-ANALYZE: %v", err)
	}
	dropped := explainBreakdown(t, store.db, query, args...)
	t.Logf("dropped-index plan:\n  %s", strings.Join(dropped, "\n  "))
	if !summarizeAgentPlan(dropped).groupByTempTree {
		t.Errorf("negative control failed: with idx_mrv_breakdown_agent_window dropped the plan still avoids a temp GROUP BY B-tree, so the pin has no teeth: %v", dropped)
	}

	// Same results without the index (access path only, never the data).
	rows, err = store.QueryReviewVerdictModelAgentBreakdown(since)
	if err != nil {
		t.Fatalf("post-drop breakdown: %v", err)
	}
	if len(rows) != 36 {
		t.Errorf("post-drop breakdown pairs = %d, want 36", len(rows))
	}
}

// TestVR_BREAKDOWN_PLAN_4_AgentIndexSurvivesLegacyReopen is the migration
// half of the M11 pin: the agent window index is created AFTER the
// agent_id ALTER precisely because existing databases reopen with a
// pre-agent_id table (the DDL block would fail with "no such column" if
// the index lived there — exactly the failure the first cut of this index
// hit against TestVR_VERSION_2/TestVR_AGENT_3). A fresh store opened over
// a legacy-shape DB (table rebuilt without agent_id, then renamed in —
// the gate test's mechanism, inlined) must come up with the index present
// and exactly one copy, and a schema re-run stays idempotent.
func TestVR_BREAKDOWN_PLAN_4_AgentIndexSurvivesLegacyReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	// Phase 1: build a store with a verdict row, then swap the table for a
	// pre-agent_id legacy shape (same mechanism as TestVR_AGENT_3).
	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := store.DB().Exec(`
		CREATE TABLE mrv_nocol AS
		SELECT step_id, task_id, cycle, is_final, rubric_id, scope, category,
		       severity, reviewed_model_id, judge_agent_id, judge_model_id,
		       orchestrator_agent_id, orchestrator_model_id, confidence,
		       trace_id, rubric_version, timestamp
		FROM model_review_verdicts`); err != nil {
		t.Fatalf("rebuild without agent_id: %v", err)
	}
	if _, err := store.DB().Exec(`DROP TABLE model_review_verdicts`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := store.DB().Exec(`ALTER TABLE mrv_nocol RENAME TO model_review_verdicts`); err != nil {
		t.Fatalf("rename legacy table: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close phase-1 store: %v", err)
	}

	// Phase 2: reopen — ALTER adds agent_id, backfill runs, and ONLY THEN
	// the M11 index is created on the widened table.
	reopened, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("reopen legacy DB: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	var count int
	if err := reopened.DB().Get(&count,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_mrv_breakdown_agent_window'`); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Errorf("idx_mrv_breakdown_agent_window count = %d, want 1 after legacy reopen", count)
	}

	// Schema re-run (next daemon open) stays idempotent: still exactly one.
	if err := initReviewVerdictSchema(reopened.db.DB); err != nil {
		t.Fatalf("re-init schema on legacy DB: %v", err)
	}
	if err := reopened.DB().Get(&count,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_mrv_breakdown_agent_window'`); err != nil {
		t.Fatalf("query sqlite_master post re-init: %v", err)
	}
	if count != 1 {
		t.Errorf("idx_mrv_breakdown_agent_window count = %d after schema re-run, want 1", count)
	}
}
