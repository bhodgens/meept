package metrics

// L5 (bughunt 2026-11-24): EXPLAIN QUERY PLAN teeth for the windowed
// model x category breakdown read. The table is append-only with indefinite
// retention (VR-RET-1), so the pre-fix plan (skip-search via
// idx_mrv_model_ts, then a temp B-tree to re-sort into GROUP BY order)
// degrades forever as the table grows. The fix is the partial composite
// index idx_mrv_breakdown_window (reviewed_model_id, category, timestamp)
// WHERE scope != 'workflow' added by initReviewVerdictSchema: GROUP BY
// equality columns first, window range column last, so the windowed
// GROUP BY rides the index order.
//
// The seed spreads ~20k rows over an 84-day span so the pinned 24h window
// selects a narrow slice — the regime where the planner chooses (and needs)
// the new index. ANALYZE mirrors the stats a long-lived DB accumulates;
// empirical probes show the index also removes the GROUP BY temp B-tree
// stats-free (as a covering scan).
//
// Teeth: with the index the plan references idx_mrv_breakdown_window and
// carries NO "TEMP B-TREE FOR GROUP BY"; with the index dropped (and stats
// rebuilt) the identical query regresses to a temp GROUP BY B-tree — proving
// the positive assertion detects the bug it pins. Result equality is
// checked against an expectation map built during seeding, so the partial
// index is proven access-path-only (same rows with and without it).

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// planDetail is one EXPLAIN QUERY PLAN row.
type planDetail struct {
	usesWindowIndex bool
	groupByTempTree bool
	tableScan       bool // bare SCAN of model_review_verdicts (no USING INDEX)
}

// explainBreakdown runs EXPLAIN QUERY PLAN on the exact breakdown SQL the
// read path executes (shared builder) and summarizes the plan.
func explainBreakdown(t *testing.T, db *sqlx.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan rows: %v", err)
	}
	return details
}

func summarizePlan(details []string) planDetail {
	var p planDetail
	for _, d := range details {
		if strings.Contains(d, "idx_mrv_breakdown_window") {
			p.usesWindowIndex = true
		}
		if strings.Contains(d, "TEMP B-TREE FOR GROUP BY") {
			p.groupByTempTree = true
		}
		if strings.HasPrefix(d, "SCAN model_review_verdicts") && !strings.Contains(d, "USING INDEX") {
			p.tableScan = true
		}
	}
	return p
}

// newPlanStore builds a real Store seeded with 25 models x 4 categories x 200
// verdict rows (20k) spread deterministically over 84 days, plus a 40-day-old
// out-of-window model row and two recent workflow rows. It returns the store
// and the expected in-window breakdown (map model/category -> count) for the
// 24h window the plan test pins.
func newPlanStore(t *testing.T) (*Store, map[[2]string]int64) {
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

	now := time.Now().UTC()
	spanMin := 83 * 24 * 60 // 83 days of minutes between first and last row
	start := now.Add(-84 * 24 * time.Hour)
	cats := []string{"pass", "logic_error", "style_bloat", "incomplete_implementation"}
	windowCut := now.Add(-24 * time.Hour)

	expected := map[[2]string]int64{}
	tx, err := store.db.Beginx()
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	const total = 25 * 4 * 200
	i := 0
	for m := 0; m < 25; m++ {
		model := fmt.Sprintf("m-%02d", m)
		for _, c := range cats {
			sev := ""
			if c != "pass" {
				sev = "blocking"
			}
			for k := 0; k < 200; k++ {
				ts := start.Add(time.Duration(i*spanMin/(total-1)) * time.Minute)
				if _, err := tx.Exec(
					`INSERT INTO model_review_verdicts
						(timestamp, step_id, task_id, cycle, is_final, rubric_id,
						 scope, category, severity, reviewed_model_id,
						 judge_agent_id, rubric_version)
					 VALUES (?, ?, ?, 1, 1, 'MR-LC1', 'model', ?, ?, ?, 'j-1', '1')`,
					ts.UTC().Format(time.RFC3339), fmt.Sprintf("seed-%d", i), "seed-task", c, sev, model,
				); err != nil {
					t.Fatalf("seed row %d: %v", i, err)
				}
				if !ts.Before(windowCut) {
					expected[[2]string{model, c}]++
				}
				i++
			}
		}
	}
	// Out-of-window model-scope row (40d old) and recent workflow rows —
	// neither may appear in any breakdown result.
	if _, err := tx.Exec(
		`INSERT INTO model_review_verdicts
			(timestamp, step_id, task_id, cycle, is_final, rubric_id,
			 scope, category, severity, reviewed_model_id, judge_agent_id, rubric_version)
		 VALUES (?, 'seed-old', 'seed-task', 1, 1, 'MR-LC2', 'model', 'logic_error', 'blocking', 'm-old', 'j-1', '1')`,
		now.Add(-40*24*time.Hour).UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	for w := 0; w < 2; w++ {
		if _, err := tx.Exec(
			`INSERT INTO model_review_verdicts
				(timestamp, step_id, task_id, cycle, is_final, rubric_id,
				 scope, category, severity, reviewed_model_id, judge_agent_id, rubric_version)
			 VALUES (?, ?, 'seed-task', 1, 1, 'MR-WF1', 'workflow', 'logic_error', 'blocking', '', 'j-1', '1')`,
			now.Add(-time.Duration(w)*time.Hour).UTC().Format(time.RFC3339),
			fmt.Sprintf("seed-wf-%d", w),
		); err != nil {
			t.Fatalf("seed workflow row: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
	if _, err := store.db.Exec(`ANALYZE`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}
	return store, expected
}

// assertBreakdownMatches checks the store's windowed breakdown equals the
// seeded expectation (no out-of-scope rows, exact counts).
func assertBreakdownMatches(t *testing.T, store *Store, since time.Time, expected map[[2]string]int64, label string) {
	t.Helper()
	rows, err := store.QueryReviewVerdictBreakdown(since)
	if err != nil {
		t.Fatalf("QueryReviewVerdictBreakdown (%s): %v", label, err)
	}
	got := map[[2]string]int64{}
	for _, r := range rows {
		if r.Model == "m-old" || r.Model == "" {
			t.Errorf("(%s) breakdown leaked out-of-scope row: %+v", label, r)
		}
		got[[2]string{r.Model, r.Category}] = r.Count
	}
	if len(got) != len(expected) {
		t.Errorf("(%s) breakdown pairs = %d, want %d (rows: %+v)", label, len(got), len(expected), rows)
	}
	for p, n := range expected {
		if got[p] != n {
			t.Errorf("(%s) breakdown %v = %d, want %d", label, p, got[p], n)
		}
	}
}

// TestVR_BREAKDOWN_PLAN_1_WindowedBreakdownUsesIndex is the L5 pin: the
// windowed breakdown query must use idx_mrv_breakdown_window with no temp
// B-tree for the GROUP BY, and must REGRESS to a temp GROUP BY B-tree once
// the index is dropped (teeth). Results stay identical either way.
func TestVR_BREAKDOWN_PLAN_1_WindowedBreakdownUsesIndex(t *testing.T) {
	store, expected := newPlanStore(t)
	since := time.Now().UTC().Add(-24 * time.Hour)
	query, args := reviewVerdictBreakdownSQL(since)

	details := explainBreakdown(t, store.db, query, args...)
	t.Logf("with-index plan:\n  %s", strings.Join(details, "\n  "))
	p := summarizePlan(details)
	if !p.usesWindowIndex {
		t.Errorf("windowed breakdown plan does not use idx_mrv_breakdown_window: %v", details)
	}
	if p.groupByTempTree {
		t.Errorf("windowed breakdown plan pays a temp B-tree for GROUP BY (the L5 full-scan/re-sort cost): %v", details)
	}
	if p.tableScan {
		t.Errorf("windowed breakdown plan does a bare table SCAN: %v", details)
	}

	// Results unchanged by the access path (positive shape check).
	assertBreakdownMatches(t, store, since, expected, "with index")

	// Teeth: drop the index, rebuild stats — the identical query must regress
	// to the pre-fix plan (temp B-tree for GROUP BY). If this passes anyway,
	// the positive assertions above are testing nothing.
	if _, err := store.db.Exec(`DROP INDEX IF EXISTS idx_mrv_breakdown_window`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if _, err := store.db.Exec(`ANALYZE`); err != nil {
		t.Fatalf("re-ANALYZE: %v", err)
	}
	dropped := explainBreakdown(t, store.db, query, args...)
	t.Logf("dropped-index plan:\n  %s", strings.Join(dropped, "\n  "))
	if !summarizePlan(dropped).groupByTempTree {
		t.Errorf("negative control failed: with idx_mrv_breakdown_window dropped the plan still avoids a temp GROUP BY B-tree, so the pin has no teeth: %v", dropped)
	}

	// Same results without the index (partial index = access path only).
	assertBreakdownMatches(t, store, since, expected, "index dropped")
}

// TestVR_BREAKDOWN_PLAN_2_IndexCreatedIdempotently proves the L5 index ships
// through the idempotent initReviewVerdictSchema pattern: re-running the
// schema on an existing database (next daemon open) keeps exactly one index
// and does not error — no migration-registry step involved.
func TestVR_BREAKDOWN_PLAN_2_IndexCreatedIdempotently(t *testing.T) {
	store, _ := newPlanStore(t) // schema ran once on NewStore

	// Re-run the schema script against the live handle (simulates the next
	// daemon open on an existing DB).
	if err := initReviewVerdictSchema(store.db.DB); err != nil {
		t.Fatalf("re-init schema on existing DB: %v", err)
	}
	var count int
	if err := store.db.Get(&count,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_mrv_breakdown_window'`); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Errorf("idx_mrv_breakdown_window count = %d, want 1 after schema re-run on existing DB", count)
	}
}
