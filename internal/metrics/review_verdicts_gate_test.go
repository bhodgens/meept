package metrics_test

// Gate tests for master.md contract C2 (verdict store) - leaf 00T-contracts.
// These tests intentionally fail RED until leaf 02-verdict-store lands
// RecordReviewVerdict / QueryReviewVerdicts / FinalVerdictsForStep /
// RetentionTables. TESTS ONLY: no implementation lives here.
//
// All stores are built on t.TempDir(); no daemon, no bus, no network.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

func newGateStore(t *testing.T) *metrics.Store {
	t.Helper()
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func rec(stepID string, cycle int, isFinal bool, model, judge, rubric, scope, category, severity string) metrics.ReviewVerdictRecord {
	return metrics.ReviewVerdictRecord{
		StepID:          stepID,
		TaskID:          "task-1",
		Cycle:           cycle,
		IsFinal:         isFinal,
		RubricID:        rubric,
		Scope:           scope,
		Category:        category,
		Severity:        severity,
		ReviewedModelID: model,
		JudgeAgentID:    judge,
		JudgeModelID:    "gpt-judge",
		Confidence:      0.9,
		TraceID:         "trace-" + stepID,
		Timestamp:       time.Now().UTC(),
	}
}

// VR-REC-1: RecordReviewVerdict accepts the valid scope/category/severity
// vocabulary and rejects invalid combos with an error.
func TestVR_REC_1_RecordVocabulary(t *testing.T) {
	store := newGateStore(t)

	valid := []metrics.ReviewVerdictRecord{
		rec("s1", 0, false, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "blocking"),
		rec("s1", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "pass", ""),
		rec("w1", 0, true, "", "judge-a", "MR-WF1", "workflow", "incomplete_implementation", "critical"),
	}
	for i, r := range valid {
		if err := store.RecordReviewVerdict(r); err != nil {
			t.Fatalf("valid record %d (%+v) rejected: %v", i, r, err)
		}
	}

	invalid := []struct {
		note string
		rec  metrics.ReviewVerdictRecord
	}{
		{"bad scope", rec("s2", 0, false, "glm-x", "judge-a", "MR-LC1", "global", "logic_error", "blocking")},
		{"bad category", rec("s3", 0, false, "glm-x", "judge-a", "MR-LC1", "model", "defect", "blocking")},
		{"bad severity", rec("s4", 0, false, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "minor")},
		{"pass with severity", rec("s5", 0, false, "glm-x", "judge-a", "MR-LC1", "model", "pass", "critical")},
		{"defect with empty severity", rec("s6", 0, false, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "")},
	}
	for _, c := range invalid {
		if err := store.RecordReviewVerdict(c.rec); err == nil {
			t.Errorf("invalid record (%s) must be rejected", c.note)
		}
	}
}

// seedQueryFixtures records a fixed mixed-scope/mixed-judge data set:
//
//	model-scope, judge-a, model glm-x: 2 pass + 2 defects across categories
//	model-scope, judge-b, model glm-y: 1 defect
//	workflow-scope (orchestrator rows, empty reviewed model): 1 pass + 1 defect
func seedQueryFixtures(t *testing.T, store *metrics.Store) {
	t.Helper()
	rows := []metrics.ReviewVerdictRecord{
		rec("q1", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "pass", ""),
		rec("q2", 1, true, "glm-x", "judge-a", "MR-TD1", "model", "pass", ""),
		rec("q3", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "blocking"),
		rec("q4", 1, true, "glm-x", "judge-a", "MR-HD1", "model", "security_defect", "critical"),
		rec("q5", 1, true, "glm-y", "judge-b", "MR-GZ1", "model", "logic_error", "functional"),
		rec("wq1", 1, true, "", "judge-a", "MR-WF1", "workflow", "pass", ""),
		rec("wq2", 1, true, "", "judge-b", "MR-WF2", "workflow", "incomplete_implementation", "blocking"),
	}
	for i, r := range rows {
		if err := store.RecordReviewVerdict(r); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
}

func findRow(t *testing.T, rows []metrics.ReviewVerdictRow, group string) (metrics.ReviewVerdictRow, bool) {
	t.Helper()
	for _, r := range rows {
		if r.Group == group {
			return r, true
		}
	}
	return metrics.ReviewVerdictRow{}, false
}

func wantCounts(t *testing.T, r metrics.ReviewVerdictRow, verdicts, defects int64, passRate float64) {
	t.Helper()
	if r.Verdicts != verdicts || r.Defects != defects {
		t.Errorf("group %q: Verdicts/Defects = %d/%d, want %d/%d", r.Group, r.Verdicts, r.Defects, verdicts, defects)
	}
	if r.PassRate != passRate {
		t.Errorf("group %q: PassRate = %v, want %v", r.Group, r.PassRate, passRate)
	}
}

// VR-Q-MODEL-1: GroupBy "model" counts per reviewed model; pass rows form the
// pass_rate denominator; workflow rows are excluded unless IncludeWorkflow.
func TestVR_Q_MODEL_1_GroupByModel(t *testing.T) {
	store := newGateStore(t)
	seedQueryFixtures(t, store)

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model): %v", err)
	}
	rx, ok := findRow(t, rows, "glm-x")
	if !ok {
		t.Fatalf("no group row for glm-x in %v", rows)
	}
	// 4 finals for glm-x, 2 pass => PassRate 0.5.
	wantCounts(t, rx, 4, 2, 0.5)
	ry, ok := findRow(t, rows, "glm-y")
	if !ok {
		t.Fatalf("no group row for glm-y in %v", rows)
	}
	wantCounts(t, ry, 1, 1, 0)

	// Default (IncludeWorkflow=false): no workflow row may appear.
	for _, r := range rows {
		if r.Group == "" || r.Group == "orchestrator" {
			t.Errorf("workflow-scope rows leaked into per-model group: %+v", r)
		}
	}

	wfRows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model", IncludeWorkflow: true})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model, IncludeWorkflow): %v", err)
	}
	if len(wfRows) <= len(rows) {
		t.Errorf("IncludeWorkflow must add workflow rows: got %d rows vs %d default", len(wfRows), len(rows))
	}
	if wf, ok := findRow(t, wfRows, ""); ok {
		// Workflow rows blame the orchestrator (empty reviewed model).
		wantCounts(t, wf, 2, 1, 0.5)
	}
}

// VR-Q-JUDGE-1: GroupBy "judge" counts per judge agent.
func TestVR_Q_JUDGE_1_GroupByJudge(t *testing.T) {
	store := newGateStore(t)
	seedQueryFixtures(t, store)

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "judge"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(judge): %v", err)
	}
	ra, ok := findRow(t, rows, "judge-a")
	if !ok {
		t.Fatalf("no group row for judge-a in %v", rows)
	}
	// judge-a: 2 model-scope pass + 2 model-scope defects (workflow excluded by default).
	wantCounts(t, ra, 4, 2, 0.5)
	rb, ok := findRow(t, rows, "judge-b")
	if !ok {
		t.Fatalf("no group row for judge-b in %v", rows)
	}
	wantCounts(t, rb, 1, 1, 0)

	// Invalid GroupBy must error.
	if _, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "bogus"}); err == nil {
		t.Error("invalid GroupBy \"bogus\" must error")
	}
}

// VR-Q-CATEGORY-1: GroupBy "category" counts per category.
func TestVR_Q_CATEGORY_1_GroupByCategory(t *testing.T) {
	store := newGateStore(t)
	seedQueryFixtures(t, store)

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "category"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(category): %v", err)
	}
	if rp, ok := findRow(t, rows, "pass"); ok {
		// 2 model-scope pass rows (workflow pass excluded by default).
		wantCounts(t, rp, 2, 0, 1)
	}
	if rl, ok := findRow(t, rows, "logic_error"); ok {
		wantCounts(t, rl, 2, 2, 0)
	}
	if rs, ok := findRow(t, rows, "security_defect"); ok {
		wantCounts(t, rs, 1, 1, 0)
	}
}

// VR-Q-RUBRIC-1: GroupBy "rubric" counts per rubric ID.
func TestVR_Q_RUBRIC_1_GroupByRubric(t *testing.T) {
	store := newGateStore(t)
	seedQueryFixtures(t, store)

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "rubric"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(rubric): %v", err)
	}
	if rr, ok := findRow(t, rows, "MR-LC1"); ok {
		// 1 pass + 1 logic_error for MR-LC1.
		wantCounts(t, rr, 2, 1, 0.5)
	}
	if rt, ok := findRow(t, rows, "MR-TD1"); ok {
		wantCounts(t, rt, 1, 0, 1)
	}
}

// VR-RET-1: model_review_verdicts is append-only and NEVER in the retention
// purge list. Asserts against the exported RetentionTables() accessor (the
// accessor is part of C2's allowed additive surface; its absence is the RED).
func TestVR_RET_1_RetentionExcludesVerdicts(t *testing.T) {
	tables := metrics.RetentionTables()
	for _, table := range tables {
		if table == "model_review_verdicts" {
			t.Fatalf("retentionTables must exclude model_review_verdicts; got %v", tables)
		}
	}
}

// VR-FINAL-1: FinalVerdictsForStep returns only the last cycle's rows for a
// step.
func TestVR_FINAL_1_FinalVerdictsForStep(t *testing.T) {
	store := newGateStore(t)

	// Cycles 0,1,2 for step f1; cycle 2 is final.
	for _, cycle := range []int{0, 1, 2} {
		isFinal := cycle == 2
		if err := store.RecordReviewVerdict(rec("f1", cycle, isFinal, "glm-x", "judge-a", "MR-LC1", "model", "pass", "")); err != nil {
			t.Fatalf("record cycle %d: %v", cycle, err)
		}
	}
	// A non-final extra rubric row on the final cycle.
	if err := store.RecordReviewVerdict(rec("f1", 2, true, "glm-x", "judge-a", "MR-TD1", "model", "style_bloat", "cosmetic")); err != nil {
		t.Fatalf("record final extra row: %v", err)
	}
	// A different step must not leak in.
	if err := store.RecordReviewVerdict(rec("f2", 2, true, "glm-x", "judge-a", "MR-LC1", "model", "pass", "")); err != nil {
		t.Fatalf("record other step: %v", err)
	}

	got, err := store.FinalVerdictsForStep("f1")
	if err != nil {
		t.Fatalf("FinalVerdictsForStep(f1): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("FinalVerdictsForStep(f1) returned %d rows, want 2 (final-cycle rows only): %+v", len(got), got)
	}
	for _, r := range got {
		if !r.IsFinal {
			t.Errorf("row %+v is not marked final", r)
		}
		if r.StepID != "f1" {
			t.Errorf("row %+v leaked from another step", r)
		}
	}
	cycles := map[int]bool{}
	for _, r := range got {
		cycles[r.Cycle] = true
	}
	if len(cycles) != 1 {
		t.Errorf("final rows span multiple cycles: %v", cycles)
	}
	maxCycle := 0
	for c := range cycles {
		if c > maxCycle {
			maxCycle = c
		}
	}
	if maxCycle != 2 {
		t.Errorf("final rows from cycle %d, want the last cycle 2", maxCycle)
	}
}

// VR-SCOPE-1: workflow-scope verdicts are excluded from per-model aggregates
// by default (explicit exclusion; IncludeWorkflow opts in).
func TestVR_SCOPE_1_WorkflowExclusionExplicit(t *testing.T) {
	store := newGateStore(t)

	if err := store.RecordReviewVerdict(rec("sc1", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "pass", "")); err != nil {
		t.Fatalf("record model row: %v", err)
	}
	if err := store.RecordReviewVerdict(rec("sc2", 1, true, "", "judge-a", "MR-WF1", "workflow", "incomplete_implementation", "blocking")); err != nil {
		t.Fatalf("record workflow row: %v", err)
	}

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model): %v", err)
	}
	rx, ok := findRow(t, rows, "glm-x")
	if !ok {
		t.Fatalf("no group row for glm-x in %v", rows)
	}
	// Workflow defect must not inflate glm-x: 1 verdict, 0 defects, PassRate 1.
	wantCounts(t, rx, 1, 0, 1)

	incl, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model", IncludeWorkflow: true})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model, IncludeWorkflow): %v", err)
	}
	total := int64(0)
	for _, r := range incl {
		total += r.Verdicts
	}
	if total != 2 {
		t.Errorf("IncludeWorkflow totals = %d verdicts, want 2 (model + workflow rows)", total)
	}
}
