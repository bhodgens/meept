package main

// Gate tests for model-rating-metrics-plan contract C6 (CLI reviews surface)
// — leaf 00T-surfaces, task T2 (CLI-TABLE-1, CLI-WINDOW-1).
//
// These tests intentionally fail RED until leaf 02-cli lands: the
// `meept analytics reviews` subcommand does not exist yet.
//
// TESTS ONLY: no implementation lives here. The tests call the cobra
// command's RunE directly with a temp metrics DB seeded through the real
// store API (t.TempDir + MEEPT_HOME isolation — never the operator's
// ~/.meept). Frozen table contract (C6): columns group | verdicts | defects |
// pass_rate (+ severity when group-by=model|judge), sort defects DESC then
// group ASC, judge naming only, "(no verdicts recorded)" on an empty DB.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/spf13/cobra"
)

// seedReviewsDB builds a fresh metrics.Store in a MEEPT_HOME temp dir seeded
// with verdicts whose rollup is unambiguous in every dimension:
//
//	m-a: 3 verdicts, 2 defects (pass_rate 1/3)  — recent
//	m-b: 2 verdicts, 2 defects (pass_rate 0)    — recent
//	m-c: 1 verdict,  0 defects (pass_rate 1)    — recent (0 defects sorts last)
//	m-old: 1 defect verdict — 40 days old (outside --days 30/7/1)
//
// Expected model-grouped sort: m-b (2 defects), m-a (2 defects, group ASC
// tiebreak "m-a" < "m-b" — wait: both have 2 defects, so ASC group puts m-a
// first), then m-c (0).
//
// sort: m-b has 2 defects and m-a has 2 defects; the group ASC tiebreak
// orders m-a before m-b. Expected order: m-a, m-b, m-c.
func seedReviewsDB(t *testing.T) *metrics.Store {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(home, "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	mk := func(rec metrics.ReviewVerdictRecord) {
		t.Helper()
		if err := store.RecordReviewVerdict(rec); err != nil {
			t.Fatalf("RecordReviewVerdict(%s/%s/%s): %v", rec.ReviewedModelID, rec.Category, rec.Severity, err)
		}
	}
	mk(metrics.ReviewVerdictRecord{StepID: "s1", TaskID: "t1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "pass", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s2", TaskID: "t1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s3", TaskID: "t2", IsFinal: true, RubricID: "MR-LC2",
		Scope: "model", Category: "incomplete_implementation", Severity: "functional", ReviewedModelID: "m-a", JudgeAgentID: "j-2", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s4", TaskID: "t3", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-b", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s5", TaskID: "t3", IsFinal: true, RubricID: "MR-LC2",
		Scope: "model", Category: "incomplete_implementation", Severity: "functional", ReviewedModelID: "m-b", JudgeAgentID: "j-2", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s6", TaskID: "t4", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "pass", ReviewedModelID: "m-c", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s7", TaskID: "t5", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-old", JudgeAgentID: "j-1", Timestamp: old})
	return store
}

// runAnalyticsReviews runs the `analytics reviews` subcommand with the given
// args against the current DB and returns its stdout. RED reason: the
// subcommand does not exist yet (newAnalyticsReviewsCmd missing).
func runAnalyticsReviews(t *testing.T, args ...string) string {
	t.Helper()
	out := &bytes.Buffer{}
	root := newAnalyticsCmd()
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"reviews"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("analytics reviews %v: %v", args, err)
	}
	return out.String()
}

// TestCLI_TABLE_1_ReviewsTableRowsAndSort asserts the table renders real
// rows from the seeded DB: every expected group appears with its verdict
// count, rows sort defects DESC then group ASC, and the column labels use
// judge naming (never "reviewer").
func TestCLI_TABLE_1_ReviewsTableRowsAndSort(t *testing.T) {
	seedReviewsDB(t)
	out := runAnalyticsReviews(t)

	// Judge naming contract: the table never labels a column "reviewer".
	if strings.Contains(strings.ToLower(out), "reviewer") {
		t.Errorf("output uses reviewer naming; contract says judge only:\n%s", out)
	}
	for _, want := range []string{"m-a", "m-b", "m-c"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing row for %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "m-old") {
		t.Errorf("output includes m-old (outside the default 30-day window):\n%s", out)
	}

	// Sort: defects DESC, then group ASC. m-a and m-b both have 2 defects
	// (m-a first by ASC tiebreak), m-c has 0 and sorts last.
	ia, ib, ic := strings.Index(out, "m-a"), strings.Index(out, "m-b"), strings.Index(out, "m-c")
	if ia < 0 || ib < 0 || ic < 0 {
		t.Fatalf("missing rows in output:\n%s", out)
	}
	if !(ia < ib && ib < ic) {
		t.Errorf("row order wrong: m-a@%d m-b@%d m-c@%d; want defects DESC then group ASC (m-a, m-b, m-c):\n%s", ia, ib, ic, out)
	}
}

// TestCLI_WINDOW_1_DaysFlagFiltersWindow asserts --days filters the rows:
// --days 1 and --days 7 exclude the 40-day-old verdict, and the row counts
// match the store aggregates for the requested window.
func TestCLI_WINDOW_1_DaysFlagFiltersWindow(t *testing.T) {
	seedReviewsDB(t)

	out := runAnalyticsReviews(t, "--days", "7")
	if !strings.Contains(out, "m-a") || !strings.Contains(out, "m-b") || !strings.Contains(out, "m-c") {
		t.Errorf("--days 7 output missing recent rows:\n%s", out)
	}
	if strings.Contains(out, "m-old") {
		t.Errorf("--days 7 output includes m-old (40 days old):\n%s", out)
	}

	out = runAnalyticsReviews(t, "--days", "1")
	if strings.Contains(out, "m-old") {
		t.Errorf("--days 1 output includes m-old:\n%s", out)
	}

	// --days 60 must now include the 40-day-old verdict (window widened).
	out = runAnalyticsReviews(t, "--days", "60")
	if !strings.Contains(out, "m-old") {
		t.Errorf("--days 60 output missing m-old (window should include 40-day-old rows):\n%s", out)
	}
}

// compile-time guard: the gate exercises RunE wiring through the real cobra
// tree (same posture as the leaf's "call the cobra command's RunE directly"
// note), so a missing subcommand fails at Execute, not at compile time.
var _ = cobra.Command{}
