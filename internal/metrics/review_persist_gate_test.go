package metrics_test

// Gate tests for master.md contracts C4 (collector persistence) and C3
// (blame rule) — leaf 00T-capture, task T3 (EM-PERSIST-1, TJ-ROTATION-1).
//
// These tests intentionally fail RED until leaf 01-verdict-emission lands the
// collector enrichment: recordReviewMetrics (collector.go) unmarshals only the
// legacy keys today and ignores verdicts/identities entirely.
//
// TESTS ONLY: no implementation lives here. The collector is constructed
// against a temp-dir store and fed a hand-built BusMessage; no daemon, no bus.

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/pkg/models"
)

func newPersistGateStore(t *testing.T) *metrics.Store {
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

// enrichedReviewPayload builds the C4 wire shape a leaf-01 producer publishes:
// verdicts plus the identity/cycle/finality keys, on the step.review_completed
// topic the collector subscribes to.
func enrichedReviewPayload(t *testing.T) json.RawMessage {
	t.Helper()
	payload := map[string]any{
		"status":                "rejected",
		"reviewer":              "code-reviewer",
		"confidence":            0.85,
		"revision_count":        2,
		"step_id":               "step-persist-1",
		"task_id":               "task-persist-1",
		"reviewed_model_id":     "glm-serve-x",
		"judge_agent_id":        "code-reviewer",
		"judge_model_id":        "gpt-judge",
		"orchestrator_agent_id": "orchestrator",
		"orchestrator_model_id": "planner-m",
		"trace_id":              "stp-step-persist-1",
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
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

// handleReviewMessage feeds one hand-built BusMessage through the collector's
// step.review_completed path. NewCollector(nil bus) would never subscribe, so
// the message goes through the bus exactly the way the ReviewManager publish
// does: the real bus, the collector's real "step.*" subscription.
func handleReviewMessage(t *testing.T, store *metrics.Store, raw json.RawMessage) {
	t.Helper()
	messageBus := bus.New(nil, slog.New(slog.DiscardHandler))
	c := metrics.NewCollector(store, messageBus, metrics.DefaultCollectorConfig())
	defer c.Shutdown()

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", nil)
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	msg.Payload = raw
	messageBus.Publish("step.review_completed", msg)
}

// EM-PERSIST-1 (00T-capture T3): Given an enriched step.review_completed
// message; When the collector handles it; Then one model_review_verdicts row
// per verdict, carrying the identities (reviewed/judge/orchestrator model+agent,
// trace, cycle, finality) verbatim from the payload.
//
// RED today: the collector's review unmarshal struct has only the legacy keys;
// the new keys are silently dropped and no verdict rows are written.
func TestEM_PERSIST_1_CollectorPersistsVerdictRows(t *testing.T) {
	store := newPersistGateStore(t)
	handleReviewMessage(t, store, enrichedReviewPayload(t))

	// One row per verdict, keyed to the step, with identities.
	rows, err := store.FinalVerdictsForStep("step-persist-1")
	if err != nil {
		t.Fatalf("FinalVerdictsForStep: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d verdict rows for step-persist-1, want 2 (one per payload verdict) — the collector ignored the enriched keys", len(rows))
	}
	byRubric := map[string]metrics.ReviewVerdictRecord{}
	for _, r := range rows {
		byRubric[r.RubricID] = r
	}
	want := map[string]struct {
		scope    string
		category string
		severity string
	}{
		"MR-LC1": {string(modelrating.ScopeModel), string(modelrating.CategoryLogicError), string(modelrating.SeverityFunctional)},
		"MR-WF1": {string(modelrating.ScopeWorkflow), string(modelrating.CategoryIncompleteImplementation), string(modelrating.SeverityBlocking)},
	}
	for rubric, w := range want {
		r, ok := byRubric[rubric]
		if !ok {
			t.Errorf("no row for rubric %q in %d stored rows", rubric, len(rows))
			continue
		}
		if r.StepID != "step-persist-1" || r.TaskID != "task-persist-1" {
			t.Errorf("%s: step/task = %q/%q, want step-persist-1/task-persist-1", rubric, r.StepID, r.TaskID)
		}
		if r.Scope != w.scope || r.Category != w.category || r.Severity != w.severity {
			t.Errorf("%s: scope/category/severity = %q/%q/%q, want %q/%q/%q", rubric, r.Scope, r.Category, r.Severity, w.scope, w.category, w.severity)
		}
		if r.ReviewedModelID != "glm-serve-x" {
			t.Errorf("%s: reviewed_model_id = %q, want %q", rubric, r.ReviewedModelID, "glm-serve-x")
		}
		if r.JudgeAgentID != "code-reviewer" || r.JudgeModelID != "gpt-judge" {
			t.Errorf("%s: judge = %q/%q, want code-reviewer/gpt-judge", rubric, r.JudgeAgentID, r.JudgeModelID)
		}
		if r.OrchestratorAgentID != "orchestrator" || r.OrchestratorModelID != "planner-m" {
			t.Errorf("%s: orchestrator = %q/%q, want orchestrator/planner-m", rubric, r.OrchestratorAgentID, r.OrchestratorModelID)
		}
		if r.TraceID != "stp-step-persist-1" {
			t.Errorf("%s: trace_id = %q, want %q", rubric, r.TraceID, "stp-step-persist-1")
		}
		if r.Cycle != 2 || !r.IsFinal {
			t.Errorf("%s: cycle=%d is_final=%v, want cycle=2 is_final=true", rubric, r.Cycle, r.IsFinal)
		}
	}
}

// TJ-ROTATION-1 (00T-capture T4, blame half): the C3 rotation rule —
// ResolveReviewedModel picks the FINAL SUCCESSFUL llm_calls row for
// (traceID, agentID); error rows are never blamed; the judge's own rows are
// excluded by agent_id; no rows => "" (never guessed).
//
// RED note: this half compiles and passes against the branch-01 store today —
// it is the gate half of T4 that leaf 02's emitter wiring must keep green
// when reviewed_model_id starts being resolved from the trace.
func TestTJ_ROTATION_1_ResolveReviewedModelPicksFinalSuccessful(t *testing.T) {
	store := newPersistGateStore(t)
	now := time.Now()

	// Implementer rotation on trace stp-rot: error(m1), success(m2), success(m3).
	// Final SUCCESSFUL row is m3 — the blame target.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-3 * time.Second), Provider: "prov", ModelID: "m1", AgentID: "coder", IsError: true, ErrorMessage: "boom", LatencyMs: 5, TraceID: "stp-rot"})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-2 * time.Second), Provider: "prov", ModelID: "m2", AgentID: "coder", LatencyMs: 100, TraceID: "stp-rot"})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now.Add(-1 * time.Second), Provider: "prov", ModelID: "m3", AgentID: "coder", LatencyMs: 100, TraceID: "stp-rot"})
	// The judge's own call on the SAME trace: excluded by agent_id.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now, Provider: "prov", ModelID: "judge-model", AgentID: "code-reviewer", LatencyMs: 50, TraceID: "stp-rot"})

	got, err := store.ResolveReviewedModel("stp-rot", "coder")
	if err != nil {
		t.Fatalf("ResolveReviewedModel: %v", err)
	}
	if got != "m3" {
		t.Fatalf("ResolveReviewedModel(stp-rot, coder) = %q, want %q (the FINAL SUCCESSFUL row; error rows and later rows for other agents never blame)", got, "m3")
	}

	// Error-only rotation: no successful row => "" with nil error.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now, Provider: "prov", ModelID: "m1", AgentID: "coder", IsError: true, ErrorMessage: "boom", LatencyMs: 5})
	// (only failed rows exist for this trace)
	got, err = store.ResolveReviewedModel("stp-err-only", "coder")
	if err != nil {
		t.Fatalf("ResolveReviewedModel(err-only): %v", err)
	}
	if got != "" {
		t.Errorf("ResolveReviewedModel on a trace with no successful rows = %q, want \"\" (the reviewed model is never guessed)", got)
	}

	// Unknown trace: "" with nil error.
	got, err = store.ResolveReviewedModel("stp-unknown", "coder")
	if err != nil {
		t.Fatalf("ResolveReviewedModel(unknown): %v", err)
	}
	if got != "" {
		t.Errorf("ResolveReviewedModel(unknown trace) = %q, want \"\"", got)
	}
}

// EM-PERSIST-1 (degradation half, C4): a collector-level store failure must
// never fail the review path — persistence degrades with a Warn. Pinned here
// as the vocabulary gate: the collector writes rows through RecordReviewVerdict,
// whose C1 validation is the failure surface leaf 01 must wrap as Warn.
func TestEM_PERSIST_1_InvalidVocabularyDegradesNotFails(t *testing.T) {
	store := newPersistGateStore(t)
	// An out-of-vocabulary verdict row is rejected by the store (C1) — the
	// collector's per-verdict write path must treat that as a Warn, not a
	// review-path failure. The gate pins the store-side rejection; leaf 01
	// owns the Warn posture on top.
	err := store.RecordReviewVerdict(metrics.ReviewVerdictRecord{
		StepID: "step-bad-vocab", TaskID: "task-bad-vocab", Cycle: 1, IsFinal: true,
		RubricID: "MR-LC1", Scope: "model", Category: "not_a_category", Severity: "blocking",
		JudgeAgentID: "j", TraceID: "stp-bad", Timestamp: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("RecordReviewVerdict accepted an out-of-vocabulary category; C1 validation must reject it")
	}
}
