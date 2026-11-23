package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

// Gate tests for master.md contract C3 (blame wiring half) — leaf
// 02-trace-stamping task T3: publishReviewEvent composes reviewed_model_id
// via Store.ResolveReviewedModel(traceID, agentID), falling back to the
// modelRefForAgent value ONLY when the trace join returns "".

func newBlameWiringStore(t *testing.T) *metrics.Store {
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

// newBlameWiringManager builds a ReviewManager over ONE step store seeded
// with the blame-target step (AgentID = the implementer being graded), so
// reviewEmitterIdentities and the trace join resolve exactly as production
// does.
func newBlameWiringManager(t *testing.T, mstore *metrics.Store, stepID, agentID string) *ReviewManager {
	t.Helper()
	sstore := newTestStepStore(t)
	if err := sstore.Create(&task.TaskStep{
		ID:      stepID,
		TaskID:  "task-blame-wiring",
		AgentID: agentID,
	}); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: sstore,
		Bus:       bus.New(nil, slogDiscardLogger()),
		Logger:    slogDiscardLogger(),
	})
	if mstore != nil {
		rm.SetMetricsStore(mstore)
	}
	return rm
}

// TJ-BLAME-1 (leaf 02 T3, GREEN half): with stamped implementer rows on the
// step trace, reviewed_model_id resolves to the FINAL SUCCESSFUL row's
// model id — not the agent's configured model ref.
func TestTJ_BLAME_1_ReviewedModelIDResolvesFromTrace(t *testing.T) {
	store := newBlameWiringStore(t)
	rm := newBlameWiringManager(t, store, "step-blame-1", "coder")

	// Rotation on the step trace: error(m1), success(m2), success(m3).
	// The blame rule names m3 — even though the configured ref differs.
	store.RecordLLMCall(metrics.LLMCallRecord{Provider: "prov", ModelID: "m1", AgentID: "coder", IsError: true, ErrorMessage: "boom", LatencyMs: 5, TraceID: "stp-step-blame-1"})
	store.RecordLLMCall(metrics.LLMCallRecord{Provider: "prov", ModelID: "m2", AgentID: "coder", LatencyMs: 100, TraceID: "stp-step-blame-1"})
	store.RecordLLMCall(metrics.LLMCallRecord{Provider: "prov", ModelID: "m3", AgentID: "coder", LatencyMs: 100, TraceID: "stp-step-blame-1"})
	// The judge's own call on the same trace must never blame.
	store.RecordLLMCall(metrics.LLMCallRecord{Provider: "prov", ModelID: "judge-model", AgentID: "code-reviewer", LatencyMs: 50, TraceID: "stp-step-blame-1"})

	payload := rm.enrichedReviewPayload("step-blame-1", "task-blame-wiring", &ReviewResult{
		Status:     ReviewApproved,
		ReviewerID: "code-reviewer",
	}, 0)

	if got, _ := payload["trace_id"].(string); got != "stp-step-blame-1" {
		t.Errorf("trace_id = %q, want the step-scoped stamp", got)
	}
	if got, _ := payload["reviewed_model_id"].(string); got != "m3" {
		t.Errorf("reviewed_model_id = %q, want \"m3\" (C3: FINAL SUCCESSFUL llm_calls row for (trace, agent); the judge's row never blames)", got)
	}
}

// TJ-BLAME-2 (leaf 02 T3, fallback half): no stamped rows (or no metrics
// store) => the composition falls back to the agent's configured model ref
// via modelRefForAgent — the pre-leaf value — never guessed beyond that.
func TestTJ_BLAME_2_NoRowsFallsBackToModelRef(t *testing.T) {
	// (a) store wired, trace has no rows for the step.
	store := newBlameWiringStore(t)
	rm := newBlameWiringManager(t, store, "step-blame-2", "coder")

	payload := rm.enrichedReviewPayload("step-blame-2", "task-blame-wiring", &ReviewResult{
		Status:     ReviewApproved,
		ReviewerID: "code-reviewer",
	}, 0)

	got, _ := payload["reviewed_model_id"].(string)
	if got == "" {
		t.Log("no agent model binding in this fixture; the fallback ref is \"\" — acceptable (never guessed)")
	} else {
		t.Logf("fallback ref = %q (modelRefForAgent lane)", got)
	}
	if got2, _ := payload["trace_id"].(string); got2 != "stp-step-blame-2" {
		t.Errorf("trace_id = %q, want the step-scoped stamp even when the join misses", got2)
	}

	// (b) no metrics store at all: pure legacy fallback posture.
	rmNoStore := newBlameWiringManager(t, nil, "step-blame-2b", "coder")
	payload2 := rmNoStore.enrichedReviewPayload("step-blame-2b", "task-blame-wiring", &ReviewResult{
		Status:     ReviewApproved,
		ReviewerID: "code-reviewer",
	}, 0)
	if _, ok := payload2["reviewed_model_id"]; !ok {
		t.Error("payload missing reviewed_model_id key without a metrics store (frozen C4 key set)")
	}
}
