package metrics_test

// Gate tests for master.md contract C2 (verdict store) - leaf 00T-contracts.
// These tests intentionally fail RED until leaf 02-verdict-store lands
// RecordReviewVerdict / QueryReviewVerdicts / FinalVerdictsForStep /
// RetentionTables. TESTS ONLY: no implementation lives here.
//
// All stores are built on t.TempDir(); no daemon, no bus, no network.

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/modelrating"
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

// newGateStoreAt is newGateStore with an explicit db path (the migration
// test reopens the SAME file to exercise the schema migration path).
func newGateStoreAt(t *testing.T, dbPath string) *metrics.Store {
	t.Helper()
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(%s): %v", dbPath, err)
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

// VR-L7-1 (bughunt 2026-11-24 L7): rollups count FINAL cycles only. A
// needs_info review round (is_final=0) is superseded by a later cycle of the
// same step; counting its rows inflated defect totals and deflated pass
// rates. Applies to QueryReviewVerdicts (both scope variants),
// CountReviewVerdicts, and QueryReviewVerdictBreakdown. FinalVerdictsForStep
// stays full-fidelity — it reads the last cycle's raw rows regardless of the
// finality flag.
func TestVR_L7_1_RollupsCountFinalCyclesOnly(t *testing.T) {
	store := newGateStore(t)
	now := time.Now().UTC()

	mk := func(r metrics.ReviewVerdictRecord) {
		t.Helper()
		r.Timestamp = now
		if err := store.RecordReviewVerdict(r); err != nil {
			t.Fatalf("RecordReviewVerdict(%s/%s final=%v): %v", r.ReviewedModelID, r.Category, r.IsFinal, err)
		}
	}
	// l-m: one FINAL defect + one FINAL pass, plus two NON-FINAL (needs_info)
	// defect rows that must not appear in any rollup.
	mk(rec("l1", 1, true, "l-m", "j", "MR-LC1", "model", "logic_error", "blocking"))
	mk(rec("l2", 1, true, "l-m", "j", "", "model", "pass", ""))
	mk(rec("l1", 0, false, "l-m", "j", "MR-LC1", "model", "logic_error", "blocking"))
	mk(rec("l1", 0, false, "l-m", "j", "MR-HD1", "model", "security_defect", "critical"))
	// Non-final workflow row: excluded from totals too (both filters AND in).
	mk(rec("l3", 0, false, "", "j", "MR-WF1", "workflow", "incomplete_implementation", "blocking"))
	mk(rec("l4", 1, true, "", "j", "MR-WF1", "workflow", "incomplete_implementation", "blocking"))

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model): %v", err)
	}
	rl, ok := findRow(t, rows, "l-m")
	if !ok {
		t.Fatalf("no group row for l-m in %v", rows)
	}
	// 2 finals (1 defect + 1 pass); the 2 non-final defects are gone.
	wantCounts(t, rl, 2, 1, 0.5)

	incl, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model", IncludeWorkflow: true})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts(model, IncludeWorkflow): %v", err)
	}
	var total int64
	for _, r := range incl {
		total += r.Verdicts
	}
	// 3 finals total (l-m 2 + workflow 1 final); the non-final workflow row
	// and l-m's two non-final rows are excluded.
	if total != 3 {
		t.Errorf("IncludeWorkflow finals total = %d, want 3 (rows: %+v)", total, incl)
	}

	cnt, err := store.CountReviewVerdicts(metrics.ReviewVerdictQuery{})
	if err != nil {
		t.Fatalf("CountReviewVerdicts: %v", err)
	}
	if cnt != 2 {
		t.Errorf("CountReviewVerdicts (non-workflow) = %d, want 2 finals", cnt)
	}
	cntAll, err := store.CountReviewVerdicts(metrics.ReviewVerdictQuery{IncludeWorkflow: true})
	if err != nil {
		t.Fatalf("CountReviewVerdicts(IncludeWorkflow): %v", err)
	}
	if cntAll != 3 {
		t.Errorf("CountReviewVerdicts(IncludeWorkflow) = %d, want 3 finals", cntAll)
	}

	breakdown, err := store.QueryReviewVerdictBreakdown(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("QueryReviewVerdictBreakdown: %v", err)
	}
	bd := map[string]int64{}
	for _, r := range breakdown {
		if r.Model == "l-m" {
			bd[r.Category] = r.Count
		}
	}
	if bd["logic_error"] != 1 {
		t.Errorf("breakdown l-m/logic_error = %d, want 1 (non-final cycle-0 row excluded): %+v", bd["logic_error"], breakdown)
	}
	if bd["security_defect"] != 0 {
		t.Errorf("breakdown l-m/security_defect = %d, want 0 (non-final-only category): %+v", bd["security_defect"], breakdown)
	}

	// Full-fidelity raw read: FinalVerdictsForStep still returns the LAST
	// cycle's rows untouched by the rollup filter (l1's final cycle is 1).
	raw, err := store.FinalVerdictsForStep("l1")
	if err != nil {
		t.Fatalf("FinalVerdictsForStep(l1): %v", err)
	}
	if len(raw) != 1 || raw[0].Category != "logic_error" || !raw[0].IsFinal {
		t.Errorf("FinalVerdictsForStep(l1) = %+v, want the single final-cycle row", raw)
	}
}

// VR-VERSION-1: every recorded verdict stamps the current rubric catalog
// version when RubricVersion is empty on input, and honors an explicit
// version when set. Purpose: a future catalog bump must be visible in stored
// history, never silently mixed into aggregates.
func TestVR_VERSION_1_RubricVersionStamping(t *testing.T) {
	store := newGateStore(t)

	// Producer path: empty version -> current catalog version.
	if err := store.RecordReviewVerdict(rec("sv1", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "pass", "")); err != nil {
		t.Fatalf("RecordReviewVerdict: %v", err)
	}
	// Explicit version: pinned verbatim (test/migration path).
	explicit := rec("sv2", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "blocking")
	explicit.RubricVersion = "0"
	if err := store.RecordReviewVerdict(explicit); err != nil {
		t.Fatalf("RecordReviewVerdict(explicit): %v", err)
	}

	rows := []struct {
		StepID        string `db:"step_id"`
		RubricVersion string `db:"rubric_version"`
	}{}
	if err := store.DB().Select(&rows,
		`SELECT step_id, rubric_version FROM model_review_verdicts ORDER BY step_id`); err != nil {
		t.Fatalf("select rubric_version: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		switch r.StepID {
		case "sv1":
			if r.RubricVersion != modelrating.RubricCatalogVersion {
				t.Errorf("sv1 rubric_version = %q, want current catalog version %q (empty input stamps current)", r.RubricVersion, modelrating.RubricCatalogVersion)
			}
		case "sv2":
			if r.RubricVersion != "0" {
				t.Errorf("sv2 rubric_version = %q, want explicit \"0\"", r.RubricVersion)
			}
		}
	}
}

// VR-VERSION-2 (migration): a database created by the PRE-rubric_version
// schema (no column) migrates on open — the column appears, defaults to
// version 1, and pre-column rows are backfilled so they stay inside
// version-filtered aggregates instead of silently dropping out.
func TestVR_VERSION_2_MigrationBackfillsExistingRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	// Build the store once to create the table with the CURRENT schema and
	// one recorded verdict, then simulate a legacy database: recreate the
	// table WITHOUT rubric_version and move the row into it through raw SQL
	// (the only way to produce a pre-migration row).
	legacy := newGateStoreAt(t, dbPath)
	if err := legacy.RecordReviewVerdict(rec("legacy-1", 1, true, "glm-x", "judge-a", "MR-LC1", "model", "logic_error", "blocking")); err != nil {
		t.Fatalf("RecordReviewVerdict(legacy seed): %v", err)
	}
	if _, err := legacy.DB().Exec(`
		CREATE TABLE IF NOT EXISTS model_review_verdicts_legacy (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			step_id TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			cycle INTEGER NOT NULL DEFAULT 0,
			is_final BOOLEAN NOT NULL DEFAULT 0,
			rubric_id TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT '',
			reviewed_model_id TEXT NOT NULL DEFAULT '',
			judge_agent_id TEXT NOT NULL DEFAULT '',
			judge_model_id TEXT NOT NULL DEFAULT '',
			orchestrator_agent_id TEXT NOT NULL DEFAULT '',
			orchestrator_model_id TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0,
			trace_id TEXT NOT NULL DEFAULT ''
		)`, nil); err == nil {
		_ = err
	}
	// Copy the real table's rows into the legacy shape, drop the real table,
	// rename the legacy one in: the database now looks exactly like a
	// pre-rubric_version deployment with one recorded verdict.
	if _, err := legacy.DB().Exec(`
		INSERT INTO model_review_verdicts_legacy
			(timestamp, step_id, task_id, cycle, is_final, rubric_id, scope, category, severity,
			 reviewed_model_id, judge_agent_id, judge_model_id, confidence, trace_id)
			SELECT timestamp, step_id, task_id, cycle, is_final, rubric_id, scope, category, severity,
			       reviewed_model_id, judge_agent_id, judge_model_id, confidence, trace_id
			FROM model_review_verdicts`, nil); err != nil {
		t.Fatalf("seed legacy copy: %v", err)
	}
	_ = legacy.Close()
	_ = dbRawExec(t, dbPath, `DROP TABLE model_review_verdicts`)
	_ = dbRawExec(t, dbPath, `ALTER TABLE model_review_verdicts_legacy RENAME TO model_review_verdicts`)

	// Reopen: the migration must add rubric_version, backfill the legacy row
	// to version 1, and keep the row queryable.
	reopened := newGateStoreAt(t, dbPath)
	rows := []struct {
		StepID        string `db:"step_id"`
		RubricVersion string `db:"rubric_version"`
	}{}
	if err := reopened.DB().Select(&rows,
		`SELECT step_id, rubric_version FROM model_review_verdicts`); err != nil {
		t.Fatalf("select after migration (rubric_version missing?): %v", err)
	}
	for _, r := range rows {
		if r.RubricVersion != "1" {
			t.Errorf("legacy row %s rubric_version = %q, want backfilled \"1\" (pre-column rows were graded under version 1)", r.StepID, r.RubricVersion)
		}
	}

	// The migrated store still aggregates: the legacy row participates in
	// model rollups (it would vanish from a WHERE rubric_version != '' bug).
	agg, err := reopened.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts after migration: %v", err)
	}
	found := false
	for _, a := range agg {
		if a.Group == "glm-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("migrated legacy row missing from model rollup: %+v", agg)
	}
}

// dbRawExec runs one statement against a sqlite db path (test helper for the
// legacy-shape simulation; stores must be closed first).
func dbRawExec(t *testing.T, dbPath, stmt string) error {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	_, err = db.Exec(stmt, nil)
	return err
}

// VR-BREAKDOWN-1: QueryReviewVerdictBreakdown aggregates per (reviewed
// model, category), excludes workflow rows structurally, honors the window,
// and orders model ASC then count DESC then category ASC.
func TestVR_BREAKDOWN_1_ModelCategoryBreakdown(t *testing.T) {
	store := newGateStore(t)
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)

	mk := func(r metrics.ReviewVerdictRecord) {
		t.Helper()
		if err := store.RecordReviewVerdict(r); err != nil {
			t.Fatalf("RecordReviewVerdict(%s/%s): %v", r.ReviewedModelID, r.Category, err)
		}
	}
	// m-a: 2 logic_error, 1 pass. m-b: 1 logic_error. m-old: 1 old defect
	// (window exclusion). workflow rows: excluded structurally.
	mk(rec("b1", 1, true, "m-a", "j", "MR-LC1", "model", "logic_error", "blocking"))
	mk(rec("b2", 1, true, "m-a", "j", "MR-LC1", "model", "logic_error", "blocking"))
	mk(rec("b3", 1, true, "m-a", "j", "", "model", "pass", ""))
	mk(rec("b4", 1, true, "m-b", "j", "MR-LC1", "model", "logic_error", "functional"))
	mk(rec("b5", 1, true, "", "j", "MR-WF1", "workflow", "incomplete_implementation", "critical"))
	oldRec := rec("b6", 1, true, "m-a", "j", "MR-LC2", "model", "style_bloat", "cosmetic")
	oldRec.Timestamp = old
	mk(oldRec)

	rows, err := store.QueryReviewVerdictBreakdown(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("QueryReviewVerdictBreakdown: %v", err)
	}

	type pair struct {
		model, category string
	}
	got := map[pair]int64{}
	var order []pair
	for _, r := range rows {
		p := pair{r.Model, r.Category}
		got[p] = r.Count
		order = append(order, p)
	}

	want := map[pair]int64{
		{"m-a", "logic_error"}: 2,
		{"m-a", "pass"}:        1,
		{"m-b", "logic_error"}: 1,
	}
	for p, n := range want {
		if got[p] != n {
			t.Errorf("breakdown %v = %d, want %d (full rows: %+v)", p, got[p], n, rows)
		}
	}
	if n := len(rows); n != 3 {
		t.Errorf("breakdown has %d rows, want 3 (workflow rows excluded structurally; old rows outside the window): %+v", n, rows)
	}
	if _, ok := got[pair{"m-a", "style_bloat"}]; ok {
		t.Errorf("breakdown includes the 40-day-old row despite a 24h window: %+v", rows)
	}
	// Ordering: model ASC first.
	if len(order) > 1 && order[0].model != "m-a" {
		t.Errorf("breakdown ordering: first row %v, want m-a rows first (model ASC)", order[0])
	}

	// Zero Since = unbounded: the old row appears.
	all, err := store.QueryReviewVerdictBreakdown(time.Time{})
	if err != nil {
		t.Fatalf("QueryReviewVerdictBreakdown(unbounded): %v", err)
	}
	foundOld := false
	for _, r := range all {
		if r.Category == "style_bloat" && r.Model == "m-a" {
			foundOld = true
		}
	}
	if !foundOld {
		t.Errorf("unbounded breakdown missing the old style_bloat row: %+v", all)
	}
	// Workflow rows never appear in ANY breakdown window.
	for _, r := range all {
		if r.Model == "" {
			t.Errorf("breakdown carries an empty reviewed_model_id row (workflow exclusion broken): %+v", r)
		}
	}
}
