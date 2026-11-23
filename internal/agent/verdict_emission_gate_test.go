package agent

// Gate tests for master.md contracts C4 (verdict emission + persistence) —
// leaf 00T-capture, tasks T1 (EM-PAYLOAD-1) and T2 (EM-PRODUCER-1).
//
// These tests intentionally fail RED until leaf 01-verdict-emission lands:
//   - ReviewResult.Verdicts does not exist yet (compile error = RED for a
//     missing-implementation reason),
//   - publishReviewEvent does not add the frozen payload keys yet,
//   - the evidence gate attaches no verdicts yet.
//
// TESTS ONLY: no implementation lives here. Never weaken the frozen key
// names (C4) — they are wire values persisted by the collector.
//
// Producer-site rule (C4 / issue #168): ReviewManager + review_evidence_gate.go
// EMIT; pair_manager.go (and the inline-review lane) are EXCLUDED in v1.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// gateVerdicts is the fixture the emitter must carry verbatim onto the wire.
func gateVerdicts() []modelrating.Verdict {
	return []modelrating.Verdict{
		{
			RubricID:   modelrating.RubricID("MR-LC1"),
			Scope:      modelrating.ScopeModel,
			Category:   modelrating.CategoryLogicError,
			Severity:   modelrating.SeverityFunctional,
			Detail:     "off-by-one in the retry loop",
			Confidence: 0.9,
		},
		{
			RubricID:   modelrating.RubricID("MR-WF1"),
			Scope:      modelrating.ScopeWorkflow,
			Category:   modelrating.CategoryIncompleteImplementation,
			Severity:   modelrating.SeverityBlocking,
			Detail:     "planner split the step too coarse",
			Confidence: 0.8,
		},
	}
}

// waitGateBusMsg drains one message off a subscription with a bounded wait so
// a dropped publish fails as a timeout, never as a hang.
func waitGateBusMsg(t *testing.T, sub *bus.Subscriber, desc string) *models.BusMessage {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s (publish dropped or key enrichment missing)", desc)
		return nil
	}
}

// assertFrozenReviewPayload pins contract C4 verbatim: the enriched review
// payload carries the frozen keys with exact names, on BOTH review topics.
func assertFrozenReviewPayload(t *testing.T, raw json.RawMessage, topic, judge string, wantCycle float64, wantFinal bool) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s: unmarshal payload: %v", topic, err)
	}

	// Frozen key set (C4). Names are wire values; renames are defects.
	frozenKeys := []string{
		"verdicts",
		"reviewed_model_id",
		"judge_agent_id",
		"judge_model_id",
		"orchestrator_agent_id",
		"orchestrator_model_id",
		"trace_id",
		"cycle",
		"is_final",
	}
	for _, key := range frozenKeys {
		if _, ok := payload[key]; !ok {
			t.Errorf("%s: payload missing frozen key %q (got keys %v)", topic, key, gatePayloadKeys(payload))
		}
	}

	if got, ok := payload["judge_agent_id"].(string); !ok || got != judge {
		t.Errorf("%s: judge_agent_id = %v, want %q", topic, payload["judge_agent_id"], judge)
	}
	if got, ok := payload["cycle"].(float64); !ok || got != wantCycle {
		t.Errorf("%s: cycle = %v, want %v", topic, payload["cycle"], wantCycle)
	}
	if got, ok := payload["is_final"].(bool); !ok || got != wantFinal {
		t.Errorf("%s: is_final = %v, want %v", topic, payload["is_final"], wantFinal)
	}

	// verdicts: array of {rubric_id, scope, category, severity, detail,
	// confidence} with the frozen snake_case element names (C4). Marshaling
	// the Go struct field names (RubricID, ...) instead is a defect.
	verdicts, ok := payload["verdicts"].([]any)
	if !ok {
		t.Fatalf("%s: verdicts = %v, want an array", topic, payload["verdicts"])
	}
	if len(verdicts) != 2 {
		t.Fatalf("%s: verdicts has %d elements, want 2", topic, len(verdicts))
	}
	byRubric := map[string]map[string]any{}
	for _, v := range verdicts {
		elem, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s: verdict element %v is not an object", topic, v)
		}
		for _, key := range []string{"rubric_id", "scope", "category", "severity", "detail", "confidence"} {
			if _, ok := elem[key]; !ok {
				t.Errorf("%s: verdict element missing frozen key %q (got %v)", topic, key, gatePayloadKeys(elem))
			}
		}
		id, _ := elem["rubric_id"].(string)
		byRubric[id] = elem
	}
	want := map[string]struct {
		scope      string
		category   string
		severity   string
		detail     string
		confidence float64
	}{
		"MR-LC1": {string(modelrating.ScopeModel), string(modelrating.CategoryLogicError), string(modelrating.SeverityFunctional), "off-by-one in the retry loop", 0.9},
		"MR-WF1": {string(modelrating.ScopeWorkflow), string(modelrating.CategoryIncompleteImplementation), string(modelrating.SeverityBlocking), "planner split the step too coarse", 0.8},
	}
	for rubric, w := range want {
		elem, ok := byRubric[rubric]
		if !ok {
			t.Errorf("%s: no verdict element for rubric %q", topic, rubric)
			continue
		}
		if got, _ := elem["scope"].(string); got != w.scope {
			t.Errorf("%s: %s scope = %q, want %q", topic, rubric, got, w.scope)
		}
		if got, _ := elem["category"].(string); got != w.category {
			t.Errorf("%s: %s category = %q, want %q", topic, rubric, got, w.category)
		}
		if got, _ := elem["severity"].(string); got != w.severity {
			t.Errorf("%s: %s severity = %q, want %q", topic, rubric, got, w.severity)
		}
		if got, _ := elem["detail"].(string); got != w.detail {
			t.Errorf("%s: %s detail = %q, want %q", topic, rubric, got, w.detail)
		}
		if got, _ := elem["confidence"].(float64); got != w.confidence {
			t.Errorf("%s: %s confidence = %v, want %v", topic, rubric, got, w.confidence)
		}
	}
}

func gatePayloadKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// EM-PAYLOAD-1 (00T-capture T1): Given a ReviewResult with verdicts; When
// publishReviewEvent runs; Then the payload carries verdicts + judge/reviewed/
// cycle/is_final keys with the exact frozen C4 names — on BOTH the
// step.review_completed and task.review_completed messages (identical
// payloads; the TUI/ChatHandler subscribers read the task.* twin).
func TestEM_PAYLOAD_1_FrozenPayloadKeys(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: newTestStepStore(t),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	stepSub := msgBus.Subscribe("em-payload-step", "step.review_completed")
	taskSub := msgBus.Subscribe("em-payload-task", "task.review_completed")

	// Terminal approved pass: is_final true, cycle = the revision count.
	result := &ReviewResult{
		Status:     ReviewApproved,
		Feedback:   "approved with findings",
		Confidence: 0.9,
		ReviewerID: "test-reviewer",
		Verdicts:   gateVerdicts(),
	}
	rm.publishReviewEvent("step-em-1", "task-em-1", result, 2)

	stepMsg := waitGateBusMsg(t, stepSub, "step.review_completed message")
	taskMsg := waitGateBusMsg(t, taskSub, "task.review_completed message")
	assertFrozenReviewPayload(t, stepMsg.Payload, "step.review_completed", "test-reviewer", 2, true)
	assertFrozenReviewPayload(t, taskMsg.Payload, "task.review_completed", "test-reviewer", 2, true)

	// Non-terminal pass: needs_info is never final.
	stepSub2 := msgBus.Subscribe("em-payload-step-2", "step.review_completed")
	info := &ReviewResult{
		Status:     ReviewNeedsInfo,
		Feedback:   "need the test output",
		Confidence: 1.0,
		ReviewerID: "test-reviewer",
		Verdicts:   gateVerdicts(),
	}
	rm.publishReviewEvent("step-em-2", "task-em-2", info, 1)
	infoMsg := waitGateBusMsg(t, stepSub2, "needs_info step.review_completed message")
	assertFrozenReviewPayload(t, infoMsg.Payload, "step.review_completed(needs_info)", "test-reviewer", 1, false)
}

// EM-PRODUCER-1 (00T-capture T2): producer-site rule (C4, issue #168).
// Given the evidence-gate path; When it constructs a ReviewResult; Then
// verdicts attach (defect category, model scope). Given the EXCLUDED v1
// producers (pair_manager.go's inline review parse — review_tools.go does not
// exist in this tree); Then no verdicts.
func TestEM_PRODUCER_1_ProducerScopeExclusions(t *testing.T) {
	// Emitting site: the file-evidence gate's deterministic rejection must
	// carry a defect verdict (category per finding), model scope.
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: newTestStepStore(t),
		Logger:    slogDiscardLogger(),
	})
	step := &task.TaskStep{
		ID:          "step-em-prod",
		TaskID:      "task-em-prod",
		ToolHint:    "file_write",
		Description: "write the quarterly report to report.txt",
		Result:      "I wrote the file successfully",
	}
	verdict := rm.fileEvidenceGate(step, DefaultReviewPolicy())
	if verdict == nil {
		t.Fatal("fileEvidenceGate returned nil for a file-shaped step with zero file evidence; want a deterministic rejection")
	}
	if verdict.Status != ReviewRejected {
		t.Fatalf("gate status = %v, want ReviewRejected", verdict.Status)
	}
	if len(verdict.Verdicts) == 0 {
		t.Fatal("evidence-gate ReviewResult attaches no verdicts (C4: the gate is an emitting producer)")
	}
	for _, v := range verdict.Verdicts {
		if v.Scope != modelrating.ScopeModel {
			t.Errorf("gate verdict scope = %q, want %q", v.Scope, modelrating.ScopeModel)
		}
		if v.Category == modelrating.CategoryPass {
			t.Errorf("gate verdict for a rejection carries category %q, want a defect category", v.Category)
		}
		if err := v.Validate(); err != nil {
			t.Errorf("gate verdict %s invalid: %v", v.RubricID, err)
		}
	}

	// Excluded site (v1): the pair manager's review parse must NOT attach
	// verdicts. (review_tools.go does not exist in this tree; pair_manager.go
	// is the pinned exclusion surface.)
	pm := NewPairManager(PairManagerConfig{})
	pairResult := pm.parseReviewOutput("approved: all requirements met")
	if len(pairResult.Verdicts) != 0 {
		t.Errorf("pair_manager ReviewResult carries %d verdicts; pair_manager is a v1-EXCLUDED producer (C4) and must emit none",
			len(pairResult.Verdicts))
	}
}
