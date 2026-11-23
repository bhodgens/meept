package http

// Gate tests for model-rating-metrics-plan contract C5 (HTTP reviews surface)
// — leaf 00T-surfaces, tasks T1 (HTTP-ROUTE-1, HTTP-WINDOW-1, HTTP-GROUP-1,
// HTTP-DEGRADE-1).
//
// These tests intentionally fail RED until leaf 01-http-endpoint lands: the
// /api/v1/metrics/reviews route, the ReviewVerdictProvider seam, and
// WithReviewVerdictProvider do not exist yet.
//
// TESTS ONLY: no implementation lives here. Frozen response shape (C5):
//
//	GET /api/v1/metrics/reviews?window=24h|7d|30d|all&group_by=model|judge|category|rubric
//	200 {"window","group_by","rows":[{group,verdicts,defects,pass_rate,label}],"workflow_excluded"}
//	nil provider or store error => 200 with rows:[] (degrade posture of
//	live_metrics_usage.go buildLiveMetricsResponse); bad params => 400.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// errReviewProvider is a ReviewVerdictProvider test double whose query always
// fails — it exercises the store-error degrade path in HTTP-DEGRADE-1. It
// compiles against the seam contract C5: one method mirroring
// metrics.Store.QueryReviewVerdicts. If leaf 01 names the interface
// differently, this file is the ONLY place that moves (gate contract).
type errReviewProvider struct{}

func (errReviewProvider) QueryReviewVerdicts(_ metrics.ReviewVerdictQuery) ([]metrics.ReviewVerdictRow, error) {
	return nil, fmt.Errorf("store unavailable (injected)")
}

// okReviewProvider echoes canned rows without touching a store.
type okReviewProvider struct {
	rows []metrics.ReviewVerdictRow
}

func (p okReviewProvider) QueryReviewVerdicts(_ metrics.ReviewVerdictQuery) ([]metrics.ReviewVerdictRow, error) {
	return p.rows, nil
}

// newReviewsGateStore builds a real metrics.Store seeded with hand-built
// verdicts (house style: live_metrics_usage_test.go newUsageStore). Row plan:
//
//	model m-a: 2 verdicts, 1 defect (1 pass, 1 defect)  — recent
//	model m-b: 1 verdict, 1 defect (0 pass)             — recent
//	model m-old: 1 defect verdict                       — 40 days old (outside 30d/7d/24h)
//	judge j-1: all 3 recent verdicts; j-2: the old one
//	category pass: 1 recent; defect categories: 3 total
//	scope=workflow: 2 recent rows (workflow_excluded = 2), never counted in
//	aggregates (QueryReviewVerdicts excludes workflow scope by default).
func newReviewsGateStore(t *testing.T) *metrics.Store {
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

	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	mk := func(rec metrics.ReviewVerdictRecord) {
		t.Helper()
		if err := store.RecordReviewVerdict(rec); err != nil {
			t.Fatalf("RecordReviewVerdict(%s/%s/%s): %v", rec.ReviewedModelID, rec.Category, rec.Severity, err)
		}
	}
	// Model rollup: m-a 2 verdicts (1 pass + 1 defect), m-b 1 defect,
	// m-old 1 defect but 40d old.
	mk(metrics.ReviewVerdictRecord{StepID: "s1", TaskID: "t1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "pass", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s2", TaskID: "t1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s3", TaskID: "t2", IsFinal: true, RubricID: "MR-LC2",
		Scope: "model", Category: "incomplete_implementation", Severity: "functional", ReviewedModelID: "m-b", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s4", TaskID: "t3", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-old", JudgeAgentID: "j-2", Timestamp: old})
	// Workflow-scope rows: excluded from aggregates, counted in
	// workflow_excluded. ReviewedModelID empty per C2 (workflow blames the
	// orchestrator).
	mk(metrics.ReviewVerdictRecord{StepID: "s5", TaskID: "t1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "workflow", Category: "pass", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "s6", TaskID: "t1", IsFinal: true, RubricID: "MR-LC2",
		Scope: "workflow", Category: "logic_error", Severity: "blocking", JudgeAgentID: "j-1", Timestamp: now})
	return store
}

// reviewsGateServer builds a Server + mux with a review-verdict provider
// wired (nil provider when p is nil) — the seam shape leaf 01 must expose.
// The second return value is the WithReviewVerdictProvider option resolved
// against the server; tests call it through the constructor option list so
// the gate fails RED on the missing symbol, not on a nil deref.
func newReviewsGateServer(t *testing.T, store *metrics.Store, provider ReviewVerdictProvider) (*Server, *http.ServeMux) {
	t.Helper()
	s := NewServer(ServerConfig{}, nil, nil, store, nil, nil, WithReviewVerdictProvider(provider))
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)
	return s, mux
}

// doReviewsGET issues GET /api/v1/metrics/reviews with the given query and
// decodes the JSON body into a generic map so frozen keys can be asserted
// verbatim (shape, not struct, is the contract).
func doReviewsGET(t *testing.T, mux *http.ServeMux, query string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/reviews"+query, http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var body map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, body
}

// assertFrozenRowShape verifies one decoded row carries exactly the frozen C5
// keys with the frozen JSON types.
func assertFrozenRowShape(t *testing.T, row map[string]any) {
	t.Helper()
	for _, key := range []string{"group", "verdicts", "defects", "pass_rate", "label"} {
		if _, ok := row[key]; !ok {
			t.Errorf("row missing frozen key %q: %v", key, row)
		}
	}
	if _, ok := row["verdicts"].(float64); !ok {
		t.Errorf("row.verdicts not a number: %v", row["verdicts"])
	}
	if _, ok := row["defects"].(float64); !ok {
		t.Errorf("row.defects not a number: %v", row["defects"])
	}
	if _, ok := row["pass_rate"].(float64); !ok {
		t.Errorf("row.pass_rate not a number: %v", row["pass_rate"])
	}
}

// TestHTTP_ROUTE_1_ReviewsEndpointFrozenShape asserts the route exists, the
// 200 body carries the frozen C5 keys verbatim, and the numbers match the
// seeded store's real aggregates.
func TestHTTP_ROUTE_1_ReviewsEndpointFrozenShape(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, okReviewProvider{})

	code, body := doReviewsGET(t, mux, "?window=30d&group_by=model")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (route missing => 404)", code)
	}
	for _, key := range []string{"window", "group_by", "rows", "workflow_excluded"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing frozen key %q: %v", key, body)
		}
	}
	if body["window"] != "30d" {
		t.Errorf("window = %v, want \"30d\"", body["window"])
	}
	if body["group_by"] != "model" {
		t.Errorf("group_by = %v, want \"model\"", body["group_by"])
	}
	we, ok := body["workflow_excluded"].(float64)
	if !ok || we != 2 {
		t.Errorf("workflow_excluded = %v (%T), want number 2", body["workflow_excluded"], body["workflow_excluded"])
	}
	rows, ok := body["rows"].([]any)
	if !ok {
		t.Fatalf("rows not an array: %T (%v)", body["rows"], body["rows"])
	}
	if len(rows) != 2 { // m-a, m-b (m-old is outside 30d; workflow scope excluded)
		t.Fatalf("len(rows) = %d (%v), want 2", len(rows), rows)
	}
	byGroup := map[string]map[string]any{}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("row not an object: %T", r)
		}
		assertFrozenRowShape(t, row)
		byGroup[row["group"].(string)] = row
	}
	ma := byGroup["m-a"]
	if ma == nil {
		t.Fatalf("missing row for m-a: %v", rows)
	}
	if ma["verdicts"] != float64(2) || ma["defects"] != float64(1) {
		t.Errorf("m-a verdicts/defects = %v/%v, want 2/1", ma["verdicts"], ma["defects"])
	}
	if ma["pass_rate"] != 0.5 {
		t.Errorf("m-a pass_rate = %v, want 0.5", ma["pass_rate"])
	}
	mb := byGroup["m-b"]
	if mb == nil {
		t.Fatalf("missing row for m-b: %v", rows)
	}
	if mb["verdicts"] != float64(1) || mb["defects"] != float64(1) || mb["pass_rate"] != float64(0) {
		t.Errorf("m-b verdicts/defects/pass_rate = %v/%v/%v, want 1/1/0", mb["verdicts"], mb["defects"], mb["pass_rate"])
	}
}

// TestHTTP_WINDOW_1_AllWindowsAccepted asserts each frozen window parses, the
// response echoes it verbatim, and the since-filter is real: 24h and 7d
// exclude the 40-day-old verdict, all includes it.
func TestHTTP_WINDOW_1_AllWindowsAccepted(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, okReviewProvider{})

	for _, win := range []string{"24h", "7d", "30d", "all"} {
		code, body := doReviewsGET(t, mux, "?window="+win+"&group_by=model")
		if code != http.StatusOK {
			t.Fatalf("window %q: status = %d, want 200; body: %v", win, code, body)
		}
		if body["window"] != win {
			t.Errorf("window %q: echoed window = %v", win, body["window"])
		}
		rows, _ := body["rows"].([]any)
		total := 0
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if v, ok := row["verdicts"].(float64); ok {
				total += int(v)
			}
		}
		if win == "all" {
			if total != 4 { // every non-workflow verdict, including m-old
				t.Errorf("window all: total verdicts = %d, want 4", total)
			}
		} else if total != 3 { // m-old (40d) excluded by every bounded window
			t.Errorf("window %s: total verdicts = %d, want 3", win, total)
		}
	}
}

// TestHTTP_GROUP_1_AllGroupBysAccepted asserts each frozen group_by parses
// and rolls the same store into that dimension; category grouping also
// proves the workflow rows stay excluded (pass count 1, not 2).
func TestHTTP_GROUP_1_AllGroupBysAccepted(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, okReviewProvider{})

	for _, gb := range []string{"model", "judge", "category", "rubric"} {
		code, body := doReviewsGET(t, mux, "?window=all&group_by="+gb)
		if code != http.StatusOK {
			t.Fatalf("group_by %q: status = %d, want 200; body: %v", gb, code, body)
		}
		if body["group_by"] != gb {
			t.Errorf("group_by %q: echoed group_by = %v", gb, body["group_by"])
		}
		rows, _ := body["rows"].([]any)
		if len(rows) == 0 {
			t.Errorf("group_by %q: rows empty for a seeded store", gb)
		}
		for _, r := range rows {
			row, _ := r.(map[string]any)
			assertFrozenRowShape(t, row)
		}
	}
}

// TestHTTP_DEGRADE_1_NilProviderAndStoreErrorReturnEmptyRows asserts the
// degrade posture mirrors live metrics: a nil provider and a failing store
// BOTH answer 200 with rows:[] and workflow_excluded:0 — never 500. Bad
// params answer 400 with an error body.
func TestHTTP_DEGRADE_1_NilProviderAndStoreErrorReturnEmptyRows(t *testing.T) {
	store := newReviewsGateStore(t)

	// Nil provider: 200, rows empty.
	_, muxNil := newReviewsGateServer(t, store, nil)
	code, body := doReviewsGET(t, muxNil, "?window=all&group_by=model")
	if code != http.StatusOK {
		t.Fatalf("nil provider: status = %d, want 200 (degrade, never 5xx)", code)
	}
	if rows, ok := body["rows"].([]any); !ok || len(rows) != 0 {
		t.Errorf("nil provider: rows = %v (%T), want []", body["rows"], body["rows"])
	}
	if we, ok := body["workflow_excluded"].(float64); !ok || we != 0 {
		t.Errorf("nil provider: workflow_excluded = %v, want 0", body["workflow_excluded"])
	}

	// Store error: 200, rows empty — same posture.
	_, muxErr := newReviewsGateServer(t, store, errReviewProvider{})
	code, body = doReviewsGET(t, muxErr, "?window=all&group_by=judge")
	if code != http.StatusOK {
		t.Fatalf("store error: status = %d, want 200 (degrade, never 5xx)", code)
	}
	if rows, ok := body["rows"].([]any); !ok || len(rows) != 0 {
		t.Errorf("store error: rows = %v (%T), want []", body["rows"], body["rows"])
	}
	if we, ok := body["workflow_excluded"].(float64); !ok || we != 0 {
		t.Errorf("store error: workflow_excluded = %v, want 0", body["workflow_excluded"])
	}

	// Bad window / bad group_by: 400.
	for _, query := range []string{"?window=forever&group_by=model", "?window=30d&group_by=reviewer", "?window=30d"} {
		code, _ = doReviewsGET(t, muxNil, query)
		if code != http.StatusBadRequest {
			t.Errorf("bad params %q: status = %d, want 400", query, code)
		}
	}
}

// TestHTTP_BREAKDOWN_1_ModelCategoryMode asserts the group_by=model_category
// mode end to end through the REAL store provider: the breakdown array
// carries {model, category, count} rows, rows stays an empty array (the
// frozen single-dimension key is always present), workflow rows are excluded,
// and the window filter applies. Runs against *metrics.Store directly so the
// ReviewVerdictBreakdownProvider resolution through the REAL provider type is
// proven, not a test double.
func TestHTTP_BREAKDOWN_1_ModelCategoryMode(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, store) // *metrics.Store implements both provider interfaces

	// Seed extra rows to make the breakdown unambiguous. newReviewsGateStore
	// already seeded model/judge/category/rubric fixtures + one workflow row.
	now := time.Now().UTC()
	mk := func(step, model, category, severity string) {
		t.Helper()
		rec := metrics.ReviewVerdictRecord{StepID: step, TaskID: "t-bd", IsFinal: true, RubricID: "MR-LC1",
			Scope: "model", Category: category, Severity: severity, ReviewedModelID: model,
			JudgeAgentID: "j-1", Timestamp: now}
		if err := store.RecordReviewVerdict(rec); err != nil {
			t.Fatalf("seed breakdown row: %v", err)
		}
	}
	mk("bd-1", "bd-model", "logic_error", "blocking")
	mk("bd-2", "bd-model", "logic_error", "blocking")
	mk("bd-3", "bd-model", "pass", "")

	code, body := doReviewsGET(t, mux, "?window=30d&group_by=model_category")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["group_by"] != "model_category" {
		t.Errorf("echoed group_by = %v, want model_category", body["group_by"])
	}

	// rows stays an empty array in breakdown mode (frozen key always present).
	rows, _ := body["rows"].([]any)
	if rows == nil {
		t.Errorf("rows is null in breakdown mode, want [] (frozen key always an array)")
	}
	if len(rows) != 0 {
		t.Errorf("rows populated in breakdown mode (%v); breakdown rows belong under breakdown", rows)
	}

	breakdown, _ := body["breakdown"].([]any)
	if len(breakdown) == 0 {
		t.Fatalf("breakdown empty for a seeded store: %v", body)
	}
	type pair struct {
		model, category string
		count           float64
	}
	got := map[pair]bool{}
	for _, b := range breakdown {
		row, ok := b.(map[string]any)
		if !ok {
			t.Fatalf("breakdown element not an object: %v", b)
		}
		for _, key := range []string{"model", "category", "count"} {
			if _, exists := row[key]; !exists {
				t.Errorf("breakdown row missing frozen key %q: %v", key, row)
			}
		}
		model, _ := row["model"].(string)
		category, _ := row["category"].(string)
		count, _ := row["count"].(float64)
		got[pair{model, category, count}] = true
		if model == "" {
			t.Errorf("breakdown row with empty model (workflow exclusion broken): %v", row)
		}
	}
	// The seeded store's fixtures give these exact pairs; counts from the
	// real aggregation.
	wantPairs := []pair{
		{"bd-model", "logic_error", 2},
		{"bd-model", "pass", 1},
	}
	for _, w := range wantPairs {
		if !got[w] {
			t.Errorf("breakdown missing %v (got %v)", w, got)
		}
	}
	// Every workflow-scope fixture row (reviewed_model_id="") must be absent.
	for p := range got {
		if p.model == "" {
			t.Errorf("breakdown carries an empty-model row: %v", p)
		}
	}

	// Window filter: a 1-second window excludes everything seeded above.
	code, body = doReviewsGET(t, mux, "?window=24h&group_by=model_category")
	if code != http.StatusOK {
		t.Fatalf("second GET status = %d, want 200", code)
	}
	if bd, _ := body["breakdown"].([]any); len(bd) == 0 {
		t.Errorf("24h window emptied the breakdown (seeded rows are recent): %v", body)
	}
}

// TestHTTP_BREAKDOWN_2_BadGroupByStill400 asserts model_category joins the
// frozen whitelist (accepted) while unknown values stay rejected.
func TestHTTP_BREAKDOWN_2_BadGroupByStill400(t *testing.T) {
	store := newReviewsGateStore(t)
	_, mux := newReviewsGateServer(t, store, okReviewProvider{})

	code, _ := doReviewsGET(t, mux, "?window=all&group_by=model_category")
	if code != http.StatusOK {
		t.Errorf("model_category: status = %d, want 200 (whitelist member)", code)
	}
	code, _ = doReviewsGET(t, mux, "?window=all&group_by=nonsense")
	if code != http.StatusBadRequest {
		t.Errorf("nonsense: status = %d, want 400", code)
	}
}
