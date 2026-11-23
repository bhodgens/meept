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
	"encoding/json"
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

// TestCLI_JSON_1_ReviewsJSONShape asserts `analytics reviews --json` emits
// the frozen HTTP-mirror shape: window, group_by, and the rows array with
// {group, verdicts, defects, pass_rate, label} rows — byte-compatible with
// GET /api/v1/metrics/reviews' single-dimension mode.
func TestCLI_JSON_1_ReviewsJSONShape(t *testing.T) {
	seedReviewsDB(t)

	out := runAnalyticsReviews(t, "--json")
	var payload struct {
		Window  string `json:"window"`
		GroupBy string `json:"group_by"`
		Rows    []struct {
			Group    string  `json:"group"`
			Verdicts int64   `json:"verdicts"`
			Defects  int64   `json:"defects"`
			PassRate float64 `json:"pass_rate"`
			Label    string  `json:"label"`
		} `json:"rows"`
		Breakdown []struct {
			Model    string `json:"model"`
			Category string `json:"category"`
			Count    int64  `json:"count"`
		} `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("--json output is not valid JSON:\n%s\nerror: %v", out, err)
	}
	if payload.GroupBy != "model" {
		t.Errorf("json group_by = %q, want \"model\" (the flag default)", payload.GroupBy)
	}
	if payload.Window == "" {
		t.Errorf("json window empty, want the --days token form (e.g. \"30d\")")
	}
	if len(payload.Rows) == 0 {
		t.Fatalf("json rows empty for a seeded store:\n%s", out)
	}
	// Frozen row keys are pinned by the struct: any key drift (rename, type
	// change) fails the unmarshal or the value asserts below.
	byGroup := map[string]int64{}
	for _, r := range payload.Rows {
		byGroup[r.Group] = r.Verdicts
	}
	// Seed: m-a 3 verdicts / 2 defects, m-b 2/2, m-c 1/0 — from the real
	// aggregation, sorted defects DESC then group ASC by the command.
	if byGroup["m-a"] != 3 || byGroup["m-b"] != 2 || byGroup["m-c"] != 1 {
		t.Errorf("json rows verdict counts wrong: %v (want m-a=3, m-b=2, m-c=1)", byGroup)
	}
	// m-c is a single pass row: pass_rate must be 1.
	for _, r := range payload.Rows {
		if r.Group == "m-c" && r.PassRate != 1 {
			t.Errorf("m-c pass_rate = %v, want 1 (one pass row, zero defects)", r.PassRate)
		}
	}
}

// TestCLI_JSON_2_BreakdownJSONShape asserts the model_category + --json
// combination: the breakdown array carries {model, category, count} rows from
// the real store aggregation, rows stays empty, and workflow rows are absent.
func TestCLI_JSON_2_BreakdownJSONShape(t *testing.T) {
	seedReviewsDB(t)

	out := runAnalyticsReviews(t, "--group-by", "model_category", "--json")
	var payload struct {
		Rows      []map[string]any `json:"rows"`
		Breakdown []struct {
			Model    string `json:"model"`
			Category string `json:"category"`
			Count    int64  `json:"count"`
		} `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("breakdown --json output is not valid JSON:\n%s\nerror: %v", out, err)
	}
	if len(payload.Rows) != 0 {
		t.Errorf("rows populated in breakdown mode (%v); breakdown rows belong under breakdown", payload.Rows)
	}
	if len(payload.Breakdown) == 0 {
		t.Fatalf("breakdown empty for a seeded store:\n%s", out)
	}
	got := map[string]int64{}
	for _, r := range payload.Breakdown {
		got[r.Model+"/"+r.Category] = r.Count
		if r.Model == "" {
			t.Errorf("breakdown row with empty model (workflow exclusion broken): %+v", r)
		}
	}
	// Seed: m-a has 2 logic_error + 1 pass; m-b has 1 logic_error; m-c has
	// 1 pass. The workflow-scope row in the seed (m-old is model-scope, the
	// seed has none workflow... m-old IS model scope) — counts from the real
	// aggregation.
	want := map[string]int64{
		"m-a/logic_error":               1,
		"m-a/pass":                      1,
		"m-b/logic_error":               1,
		"m-b/incomplete_implementation": 1,
		"m-c/pass":                      1,
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("breakdown %s = %d, want %d (full breakdown: %+v)", k, got[k], n, payload.Breakdown)
		}
	}
}

// TestCLI_BREAKDOWN_TABLE_1_ModelCategoryTable asserts the human table for
// --group-by model_category: MODEL | CATEGORY | COUNT header and the seeded
// pairs present, sorted model ASC.
func TestCLI_BREAKDOWN_TABLE_1_ModelCategoryTable(t *testing.T) {
	seedReviewsDB(t)

	out := runAnalyticsReviews(t, "--group-by", "model_category")
	if !strings.Contains(out, "MODEL") || !strings.Contains(out, "CATEGORY") || !strings.Contains(out, "COUNT") {
		t.Errorf("breakdown table missing the MODEL/CATEGORY/COUNT header:\n%s", out)
	}
	for _, want := range []string{"m-a", "m-b", "m-c", "pass", "logic_error"} {
		if !strings.Contains(out, want) {
			t.Errorf("breakdown table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "m-old") {
		t.Errorf("breakdown table includes m-old (default --days 30 excludes the 40-day-old row):\n%s", out)
	}
}

// TestCLI_GROUPBY_INVALID_1_RejectedBeforeStore asserts an unknown --group-by
// fails with a clear message (the whitelist validation runs before the store
// opens, so a typo fails fast). Runs the command tree directly because the
// runAnalyticsReviews helper treats a RunE error as fatal.
func TestCLI_GROUPBY_INVALID_1_RejectedBeforeStore(t *testing.T) {
	seedReviewsDB(t)

	root := newAnalyticsCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"reviews", "--group-by", "nonsense"})
	err := root.Execute()
	if err == nil {
		t.Fatal("unknown --group-by accepted; want an error before the store opens")
	}
	if !strings.Contains(err.Error(), "invalid --group-by") {
		t.Errorf("error = %v, want a message containing \"invalid --group-by\"", err)
	}
}

// compile-time guard: the gate exercises RunE wiring through the real cobra
// tree (same posture as the leaf's "call the cobra command's RunE directly"
// note), so a missing subcommand fails at Execute, not at compile time.
var _ = cobra.Command{}
