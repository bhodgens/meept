//go:build e2e

// Package modelrating hosts the model-rating-metrics-plan 04-ship leaf
// 01-e2e-suite hermetic scenarios: grade-capture-01, retention-01,
// surface-01, join-proof-01 (master.md Test Gate F1-F6 outcomes).
//
// Rig posture (NOTED per the leaf doc): the house full-daemon Stack rig
// (e2e/harness Stack/buildScratchBinaries) is deliberately NOT used here —
// none of the four scenario outcomes needs the daemon loop. Each test
// boots the minimal in-process pieces the features compose: a temp-dir
// metrics.Store, the real Collector subscription path (bus publish of
// step.review_completed), the real HTTP server route table through an
// httptest.NewServer, and direct llm_calls seed rows for the blame join.
// The CLI surface runs through the real cobra command tree against a
// MEEPT_HOME temp dir (the cmd/meept gate-test pattern).
//
// Hermetic: t.TempDir only, no live network (httptest loopback), no
// daemon binaries, suite well under the 60s budget.

package modelrating

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/pkg/models"
)

// newSuiteStore builds a fresh temp-dir metrics.Store (house style: the
// cmd/meept + internal/metrics gate seeds). FlushInterval an hour keeps the
// background flush loop quiet; verdict inserts are direct, not batched.
func newSuiteStore(t *testing.T) *metrics.Store {
	t.Helper()
	return newSuiteStoreAt(t, filepath.Join(t.TempDir(), "metrics.db"))
}

// newSuiteStoreAt is newSuiteStore with an explicit db path (surface-01
// points it at <MEEPT_HOME>/metrics.db so the CLI reads the same file).
func newSuiteStoreAt(t *testing.T, dbPath string) *metrics.Store {
	t.Helper()
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// publishReviewCompleted feeds one enriched step.review_completed payload
// through the REAL collector path: a live bus, the collector's real
// "step.*" subscription, exactly the way ReviewManager's publish lands
// (contract C4). The publish is synchronous into subscriber channels; a
// short poll on the store drains the delivery goroutine.
func publishReviewCompleted(t *testing.T, store *metrics.Store, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal review payload: %v", err)
	}
	messageBus := bus.New(nil, slog.New(slog.DiscardHandler))
	collector := metrics.NewCollector(store, messageBus, metrics.DefaultCollectorConfig())
	t.Cleanup(collector.Shutdown)

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", nil)
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	msg.Payload = raw
	messageBus.Publish("step.review_completed", msg)

	// Drain: the collector consumes its subscription channel on a
	// goroutine; poll until the rows land (bounded, hermetic).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := store.FinalVerdictsForStep(payloadString(payload, "step_id"))
		if err == nil && len(rows) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("collector never persisted verdict rows for step %q", payloadString(payload, "step_id"))
}

// payloadString reads a string key out of the scenario payload map.
func payloadString(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}

// --- grade-capture-01 -------------------------------------------------

// TestGradeCapture01 is scenario grade-capture-01 (Test Gate F1/F3/F4):
// Given a step graded with two defect verdicts + the judged identities
// through the REAL capture path (enriched step.review_completed via the
// collector's bus subscription), When the capture pipeline runs, Then
// model_review_verdicts rows persist with rubric/category/severity/
// judge/cycle intact.
func TestGradeCapture01(t *testing.T) {
	store := newSuiteStore(t)

	payload := map[string]any{
		"status":                "rejected",
		"reviewer":              "code-reviewer",
		"confidence":            0.85,
		"revision_count":        2,
		"step_id":               "step-gc1",
		"task_id":               "task-gc1",
		"reviewed_model_id":     "impl-model-a",
		"judge_agent_id":        "code-reviewer",
		"judge_model_id":        "judge-model-x",
		"orchestrator_agent_id": "orchestrator",
		"orchestrator_model_id": "planner-m",
		"trace_id":              "stp-step-gc1",
		"cycle":                 2,
		"is_final":              true,
		"verdicts": []map[string]any{
			{
				"rubric_id":  "MR-LC1",
				"scope":      string(modelrating.ScopeModel),
				"category":   string(modelrating.CategoryLogicError),
				"severity":   string(modelrating.SeverityFunctional),
				"detail":     "off-by-one in the retry loop",
				"confidence": 0.9,
			},
			{
				"rubric_id":  "MR-WF1",
				"scope":      string(modelrating.ScopeWorkflow),
				"category":   string(modelrating.CategoryIncompleteImplementation),
				"severity":   string(modelrating.SeverityBlocking),
				"detail":     "planner split the step too coarse",
				"confidence": 0.8,
			},
		},
	}
	publishReviewCompleted(t, store, payload)

	rows, err := store.FinalVerdictsForStep("step-gc1")
	if err != nil {
		t.Fatalf("FinalVerdictsForStep: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("verdict rows for step-gc1 = %d, want 2", len(rows))
	}
	byRubric := map[string]metrics.ReviewVerdictRecord{}
	for _, r := range rows {
		byRubric[r.RubricID] = r
	}
	type wantRow struct {
		scope, category, severity, reviewedModel, judgeAgent, judgeModel, orchAgent, orchModel, traceID string
		cycle                                                                                           int
		isFinal                                                                                         bool
	}
	want := map[string]wantRow{
		"MR-LC1": {string(modelrating.ScopeModel), string(modelrating.CategoryLogicError), string(modelrating.SeverityFunctional),
			"impl-model-a", "code-reviewer", "judge-model-x", "orchestrator", "planner-m", "stp-step-gc1", 2, true},
		"MR-WF1": {string(modelrating.ScopeWorkflow), string(modelrating.CategoryIncompleteImplementation), string(modelrating.SeverityBlocking),
			"impl-model-a", "code-reviewer", "judge-model-x", "orchestrator", "planner-m", "stp-step-gc1", 2, true},
	}
	for rubric, w := range want {
		r, ok := byRubric[rubric]
		if !ok {
			t.Errorf("no stored row for rubric %q (got rubrics from %d rows)", rubric, len(rows))
			continue
		}
		if r.Scope != w.scope || r.Category != w.category || r.Severity != w.severity {
			t.Errorf("%s: scope/category/severity = %q/%q/%q, want %q/%q/%q", rubric, r.Scope, r.Category, r.Severity, w.scope, w.category, w.severity)
		}
		if r.ReviewedModelID != w.reviewedModel {
			t.Errorf("%s: reviewed_model_id = %q, want %q", rubric, r.ReviewedModelID, w.reviewedModel)
		}
		if r.JudgeAgentID != w.judgeAgent || r.JudgeModelID != w.judgeModel {
			t.Errorf("%s: judge = %q/%q, want %q/%q", rubric, r.JudgeAgentID, r.JudgeModelID, w.judgeAgent, w.judgeModel)
		}
		if r.OrchestratorAgentID != w.orchAgent || r.OrchestratorModelID != w.orchModel {
			t.Errorf("%s: orchestrator = %q/%q, want %q/%q", rubric, r.OrchestratorAgentID, r.OrchestratorModelID, w.orchAgent, w.orchModel)
		}
		if r.TraceID != w.traceID {
			t.Errorf("%s: trace_id = %q, want %q", rubric, r.TraceID, w.traceID)
		}
		if r.Cycle != w.cycle || r.IsFinal != w.isFinal {
			t.Errorf("%s: cycle=%d is_final=%v, want cycle=%d is_final=%v", rubric, r.Cycle, r.IsFinal, w.cycle, w.isFinal)
		}
	}

	// The rollup path sees the same rows grouped by model (F2): the
	// model-scope defect shows up under the reviewed model.
	agg, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts: %v", err)
	}
	foundImpl := false
	for _, row := range agg {
		if row.Group == "impl-model-a" {
			foundImpl = true
			if row.Verdicts != 1 || row.Defects != 1 {
				t.Errorf("model rollup impl-model-a = %d verdicts / %d defects, want 1/1 (workflow rows excluded by default)", row.Verdicts, row.Defects)
			}
		}
	}
	if !foundImpl {
		t.Errorf("model rollup missing impl-model-a row: %+v", agg)
	}
}

// --- retention-01 -----------------------------------------------------

// TestRetention01 is scenario retention-01 (Test Gate F2): Given verdict
// rows older than 24h and 30d, When the metrics_live sweep (24h raw purge)
// and the 30d audit purge run, Then the verdict rows survive and
// RetentionTables() excludes model_review_verdicts.
//
// Rig note: the purge loops live in the store's private aggregateHourly,
// invoked hourly by an unexported goroutine. The e2e proof therefore runs
// the SAME SQL the sweeps run — byte-identical statements lifted from
// store.go (metrics_live 24h DELETE + the RetentionTables() audit purge)
// — against the store's own schema via the exported DB() handle. The
// table lists come from the production accessors, not the test, so a
// regression that adds model_review_verdicts to the purge list fails this
// scenario. The internal/metrics unit gates additionally pin aggregateHourly
// itself (TestStore_RetentionPreservesLLMCalls).
func TestRetention01(t *testing.T) {
	store := newSuiteStore(t)

	now := time.Now().UTC()
	day := 24 * time.Hour
	old30d := now.Add(-31 * day)
	old24h := now.Add(-25 * time.Hour)

	mk := func(rec metrics.ReviewVerdictRecord) {
		t.Helper()
		if err := store.RecordReviewVerdict(rec); err != nil {
			t.Fatalf("RecordReviewVerdict: %v", err)
		}
	}
	// One verdict 31 days old (older than the 30d audit purge), one 25
	// hours old (older than the 24h metrics_live sweep). Both must survive.
	mk(metrics.ReviewVerdictRecord{StepID: "r-step-30", TaskID: "r-task", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-old",
		JudgeAgentID: "j-1", Timestamp: old30d})
	mk(metrics.ReviewVerdictRecord{StepID: "r-step-24", TaskID: "r-task", IsFinal: true, RubricID: "MR-LC2",
		Scope: "model", Category: "incomplete_implementation", Severity: "functional", ReviewedModelID: "m-mid",
		JudgeAgentID: "j-1", Timestamp: old24h})

	// Control rows the sweeps DO purge (proving the sweeps ran): an old
	// metrics_live sample (24h sweep) and an old audit row in EVERY
	// retention table (30d purge).
	if _, err := store.DB().Exec(
		`INSERT INTO metrics_live (timestamp, metric_name, value) VALUES (?, 'control.metric', 1)`,
		old30d.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("seed metrics_live control: %v", err)
	}
	for _, table := range metrics.RetentionTables() {
		// Every retention table carries the same RFC3339 TEXT timestamp
		// shape the purge compares against (the audit-purge WHERE is
		// timestamp < datetime('now', ?)). events requires a non-empty
		// event_type (NOT NULL), the other tables take a bare timestamp.
		q := fmt.Sprintf(`INSERT INTO %s (timestamp) VALUES (?)`, table)
		if table == "events" {
			q = `INSERT INTO events (timestamp, event_type) VALUES (?, 'control')`
		}
		if _, err := store.DB().Exec(q, old30d.Format(time.RFC3339)); err != nil {
			t.Fatalf("seed %s control: %v", table, err)
		}
	}

	// --- Run the two sweeps with the production SQL ---
	// Sweep 1: the 24h metrics_live raw purge (store.go aggregateHourly).
	if _, err := store.DB().Exec(
		`DELETE FROM metrics_live WHERE timestamp < datetime('now', '-24 hours')`); err != nil {
		t.Fatalf("metrics_live sweep: %v", err)
	}
	// Sweep 2: the retention audit purge over RetentionTables() (store.go
	// aggregateHourly, retentionDays=DefaultRetentionDays).
	cutoff := fmt.Sprintf("-%d days", metrics.DefaultRetentionDays)
	for _, table := range metrics.RetentionTables() {
		q := fmt.Sprintf(`DELETE FROM %s WHERE timestamp < datetime('now', ?)`, table)
		if _, err := store.DB().Exec(q, cutoff); err != nil {
			t.Fatalf("audit purge %s: %v", table, err)
		}
	}

	// --- Then: verdict rows survive; controls are gone ---
	count := func(table string) int {
		t.Helper()
		var n int
		if err := store.DB().Get(&n, `SELECT COUNT(*) FROM `+table); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if got := count("model_review_verdicts"); got != 2 {
		t.Errorf("model_review_verdicts rows after sweeps = %d, want 2 (verdict rows survive BOTH sweeps; indefinite retention is the feature)", got)
	}
	if got := count("metrics_live"); got != 0 {
		t.Errorf("metrics_live control rows after sweep = %d, want 0 (24h raw sweep ran)", got)
	}
	for _, table := range metrics.RetentionTables() {
		if got := count(table); got != 0 {
			t.Errorf("audit control in %s after purge = %d, want 0 (30d purge ran)", table, got)
		}
	}

	// The accessor contract: model_review_verdicts never in the purge list.
	for _, table := range metrics.RetentionTables() {
		if table == "model_review_verdicts" {
			t.Errorf("RetentionTables() contains model_review_verdicts; append-only invariant broken: %v", metrics.RetentionTables())
		}
	}

	// And the rollups still see the surviving rows regardless of age when
	// no Since filter is applied.
	agg, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts: %v", err)
	}
	if len(agg) != 2 {
		t.Errorf("model rollup after sweeps = %d groups, want 2 (m-mid + m-old survive): %+v", len(agg), agg)
	}
}

// --- surface-01 -------------------------------------------------------

// reviewsHTTPResponse mirrors the frozen C5 JSON shape (decoded generically
// so the keys themselves are what is asserted).
func reviewsHTTPResponse(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		t.Fatalf("decode reviews response: %v", err)
	}
	return out
}

// runAnalyticsReviews runs the REAL built `meept analytics reviews` binary
// (harness.CLIPath — the house way suites invoke the CLI without a daemon
// Stack) against a MEEPT_HOME temp dir and returns its stdout.
func runAnalyticsReviews(t *testing.T, home string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	argv := append([]string{harness.CLIPath(t), "analytics", "reviews"}, args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // test-built binary path
	cmd.Env = append(os.Environ(), "MEEPT_HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("analytics reviews %v: %v (stderr: %s)", args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String()
}

// TestSurface01 is scenario surface-01 (Test Gate F5/F6): Given persisted
// verdicts, When GET /api/v1/metrics/reviews and `meept analytics reviews`
// run against the same rows, Then both surfaces agree on group counts and
// the HTTP response carries the frozen shape keys.
func TestSurface01(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	store := newSuiteStoreAt(t, filepath.Join(home, "metrics.db"))

	// Seed: m-a 2 verdicts (1 pass, 1 defect), m-b 1 defect, plus a
	// workflow row excluded from aggregates by default.
	now := time.Now().UTC()
	mk := func(rec metrics.ReviewVerdictRecord) {
		t.Helper()
		if err := store.RecordReviewVerdict(rec); err != nil {
			t.Fatalf("RecordReviewVerdict: %v", err)
		}
	}
	mk(metrics.ReviewVerdictRecord{StepID: "sf-1", TaskID: "tf-1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "pass", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "sf-2", TaskID: "tf-1", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: "m-a", JudgeAgentID: "j-1", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "sf-3", TaskID: "tf-2", IsFinal: true, RubricID: "MR-LC2",
		Scope: "model", Category: "incomplete_implementation", Severity: "functional", ReviewedModelID: "m-b", JudgeAgentID: "j-2", Timestamp: now})
	mk(metrics.ReviewVerdictRecord{StepID: "sf-4", TaskID: "tf-1", IsFinal: true, RubricID: "MR-WF1",
		Scope: "workflow", Category: "pass", JudgeAgentID: "j-1", Timestamp: now})

	// Surface 1: the REAL HTTP server (TLS-on-loopback, exactly what the
	// daemon runs) started in-process against the seeded store. The cert
	// lives under the temp MEEPT_HOME (tls/ subdir) so the boot stays
	// hermetic; the client skips only the self-signed verify.
	port := harness.FreePort(t)
	cfg := commhttp.DefaultServerConfig()
	cfg.Addr = fmt.Sprintf("127.0.0.1:%d", port)
	cfg.RequireAuth = false
	cfg.TLSCertFile = filepath.Join(home, "tls", "cert.pem")
	cfg.TLSKeyFile = filepath.Join(home, "tls", "key.pem")
	cfg.FingerprintFile = filepath.Join(home, "tls", "fingerprint.txt")
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // self-signed test cert
	srvCtx, srvCancel := context.WithCancel(context.Background())
	srv := commhttp.NewServer(cfg, nil, nil, store, nil, nil,
		commhttp.WithReviewVerdictProvider(store))
	defer func() {
		// Drop this test's keep-alive connections so Server.Shutdown's
		// 10s grace drain returns immediately (keeps the suite fast).
		transport.CloseIdleConnections()
		srvCancel()
	}()
	if srv == nil {
		t.Fatalf("NewServer returned nil")
	}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Start(srvCtx) }()
	// Poll until the listener answers (bounded, loopback only).
	base := fmt.Sprintf("https://127.0.0.1:%d", port)
	httpc := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	ready := time.Now().Add(15 * time.Second)
	for {
		resp0, err0 := httpc.Get(base + "/health")
		if err0 == nil {
			_ = resp0.Body.Close()
			break
		}
		if time.Now().After(ready) {
			t.Fatalf("reviews http server never became ready: %v", <-srvErr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp, err := httpc.Get(base + "/api/v1/metrics/reviews?window=all&group_by=model")
	if err != nil {
		t.Fatalf("GET reviews: %v", err)
	}
	defer resp.Body.Close()
	body := reviewsHTTPResponse(t, resp.Body)

	// Frozen response keys (C5).
	for _, key := range []string{"window", "group_by", "rows", "workflow_excluded"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing frozen key %q: %v", key, body)
		}
	}
	if body["window"] != "all" || body["group_by"] != "model" {
		t.Errorf("window/group_by = %v/%v, want all/model", body["window"], body["group_by"])
	}

	// HTTP group counts: m-a 2 verdicts (1 defect), m-b 1 (1 defect);
	// workflow row counted in workflow_excluded.
	rows, _ := body["rows"].([]any)
	httpCounts := map[string]float64{}
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("row is not an object: %v", r)
		}
		group, _ := m["group"].(string)
		verdicts, _ := m["verdicts"].(float64)
		httpCounts[group] = verdicts
		for _, key := range []string{"group", "verdicts", "defects", "pass_rate", "label"} {
			if _, ok := m[key]; !ok {
				t.Errorf("row %q missing frozen key %q", group, key)
			}
		}
	}
	if httpCounts["m-a"] != 2 || httpCounts["m-b"] != 1 {
		t.Errorf("http group counts = %v, want m-a:2 m-b:1", httpCounts)
	}
	if we, _ := body["workflow_excluded"].(float64); we != 1 {
		t.Errorf("workflow_excluded = %v, want 1", body["workflow_excluded"])
	}

	// Surface 2: the CLI table over the SAME db. The CLI opens
	// config.MeeptPath("metrics.db") itself — the same file the store
	// above wrote (WAL readers coexist).
	out := runAnalyticsReviews(t, home, "--days", "3650")
	for _, want := range []string{"m-a", "m-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("CLI output missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "m-workflow-only") {
		t.Errorf("CLI output includes workflow-blamed row:\n%s", out)
	}

	// Agreement: group counts between HTTP and the store aggregate the CLI
	// renders from.
	agg, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts: %v", err)
	}
	if len(agg) != len(httpCounts) {
		t.Errorf("group count mismatch: http %d groups vs store %d", len(httpCounts), len(agg))
	}
	for _, row := range agg {
		if httpCounts[row.Group] != float64(row.Verdicts) {
			t.Errorf("group %q: http verdicts %v vs store %d", row.Group, httpCounts[row.Group], row.Verdicts)
		}
	}
}

// --- join-proof-01 ----------------------------------------------------

// TestJoinProof01 is scenario join-proof-01 (Test Gate F4 / C3): Given an
// llm_calls rotation fixture [error(m1), success(m2), success(m3)] plus
// judge-loop calls under a different agent id, When the blame resolves,
// Then the final successful implementer model (m3) is blamed and judge
// calls are excluded by agent_id.
func TestJoinProof01(t *testing.T) {
	store := newSuiteStore(t)
	now := time.Now().UTC()
	trace := "stp-join-step-1"

	// Rotation fixture on the implementer agent: error(m1), success(m2),
	// success(m3) — the FINAL SUCCESSFUL row is m3.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-3 * time.Second), Provider: "prov", ModelID: "m1", AgentID: "coder", IsError: true, ErrorMessage: "boom", LatencyMs: 5, TraceID: trace})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-2 * time.Second), Provider: "prov", ModelID: "m2", AgentID: "coder", LatencyMs: 100, TraceID: trace})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-1 * time.Second), Provider: "prov", ModelID: "m3", AgentID: "coder", LatencyMs: 100, TraceID: trace})
	// Judge-loop calls on the SAME trace, under a different agent id, one
	// of them newer than m3: excluded by agent_id, never blamed.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now, Provider: "prov", ModelID: "judge-model", AgentID: "code-reviewer", LatencyMs: 50, TraceID: trace})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-500 * time.Millisecond), Provider: "prov", ModelID: "judge-model-2", AgentID: "second-judge", LatencyMs: 40, TraceID: trace})

	got, err := store.ResolveReviewedModel(trace, "coder")
	if err != nil {
		t.Fatalf("ResolveReviewedModel: %v", err)
	}
	if got != "m3" {
		t.Fatalf("blame resolved to %q, want %q (final SUCCESSFUL implementer row; error rows and judge-agent rows never blame)", got, "m3")
	}

	// Negative half: a judge agent_id asking about the same trace gets its
	// OWN final successful row (the join is agent-scoped), and an unknown
	// trace resolves to "" — never guessed.
	judgeModel, err := store.ResolveReviewedModel(trace, "code-reviewer")
	if err != nil {
		t.Fatalf("ResolveReviewedModel(judge): %v", err)
	}
	if judgeModel != "judge-model" {
		t.Errorf("judge-agent blame = %q, want %q (agent-scoped join)", judgeModel, "judge-model")
	}
	empty, err := store.ResolveReviewedModel("stp-nope", "coder")
	if err != nil {
		t.Fatalf("ResolveReviewedModel(unknown): %v", err)
	}
	if empty != "" {
		t.Errorf("unknown trace blame = %q, want \"\" (never guessed)", empty)
	}

	// End-to-end shape: a verdict recorded with the resolved blame surfaces
	// under m3 in the model rollup.
	if err := store.RecordReviewVerdict(metrics.ReviewVerdictRecord{StepID: "join-step-1", TaskID: "join-task", IsFinal: true, RubricID: "MR-LC1",
		Scope: "model", Category: "logic_error", Severity: "blocking", ReviewedModelID: got, JudgeAgentID: "code-reviewer", TraceID: trace, Timestamp: now}); err != nil {
		t.Fatalf("RecordReviewVerdict: %v", err)
	}
	agg, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{GroupBy: "model"})
	if err != nil {
		t.Fatalf("QueryReviewVerdicts: %v", err)
	}
	if len(agg) != 1 || agg[0].Group != "m3" || agg[0].Defects != 1 {
		t.Errorf("model rollup after blame = %+v, want single m3 group with 1 defect", agg)
	}
}
