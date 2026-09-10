package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// ambiguityTestServer returns an intent analyzer backed by an LLM stub
// that always answers with the given ambiguity score, plus the server
// (for cleanup). Mirrors the analyzer's OpenAI-compatible chat shape.
func ambiguityTestServer(t *testing.T, ambiguity float64) *IntentAnalyzer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":      "1",
			"object":  "chat.completion",
			"created": 1,
			"model":   "stub",
			"choices": []map[string]any{{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": mustJSON(t, TrueIntentAnalysis{Goal: "unclear", Ambiguity: ambiguity, Category: "chat", Confidence: 0.4}),
				},
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:   srv.URL + "/v1",
		ModelID:   "stub",
		APIKey:    "test-key",
		MaxTokens: 256,
	})
	return NewIntentAnalyzer(client, nil)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(b)
}

// Session-continuity A5 (bughunt 2026-09-08 item 14 / debt 3): the
// ambiguity short-circuit must fire ONLY for context-less sessions.
//
// Bughunt 2026-09-10 L12: these tests originally pinned a LOCAL copy of
// the decision expression (`IsAmbiguous && digest.IsEmpty()`), so a
// regression in the real gate could not fail them. They now exercise the
// real gates — ClassifyAndRoute 3.5 and ResumeAfterClarification — against
// a stubbed analyzer, pinning both decision branches:
//
//   - ambiguous + empty digest → clarification (follow-up question)
//   - ambiguous + context (non-empty digest) → proceed to classification

// TestAmbiguityGate_EmptyDigestClarifies drives the REAL ClassifyAndRoute
// gate: a context-less session whose analyzer verdict is ambiguous must
// clarification-gate (legacy behavior byte-identical for the empty-digest
// path — the pre-tree behavior these tests were written to protect).
func TestAmbiguityGate_EmptyDigestClarifies(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
	d.intentAnalyzer = ambiguityTestServer(t, 0.95)

	// No stores wired → the built digest is empty; assert that
	// precondition against the digest the gate itself will build.
	digest := d.buildSessionContextDigest("session-empty")
	if !digest.IsEmpty() {
		t.Fatalf("digest should be empty with no stores; got %+v", digest)
	}

	res, err := d.ClassifyAndRoute(context.Background(), "did the change get made?", "session-empty", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || !res.ClarificationNeeded {
		t.Fatalf("context-less + ambiguous: ClassifyAndRoute did not clarification-gate; got %+v", res)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentClarify) {
		t.Fatalf("gate result intent = %+v, want clarify", res.Intent)
	}
}

// TestAmbiguityGate_SessionContextSuppressesClarification drives the REAL
// ClassifyAndRoute gate from the other side: a session with real context
// (tracked task + step result) must NOT clarification-gate an ambiguous
// input — history-aware classification proceeds.
func TestAmbiguityGate_SessionContextSuppressesClarification(t *testing.T) {
	cs := newCaptureServer(t, `{"goal":"clarify follow-up","ambiguity":0.95,"scope":"narrow","category":"clarification","suggested_questions":["Which change do you mean?"],"confidence":0.9}`)
	d := newDigestCaptureDispatcher(t, cs)

	seedDigestTask(t, d, "task-gate", "implement parser", "session-ctx",
		task.StateCompleted, time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), "coder")
	seedDigestStep(t, d.taskRegistry, "task-gate", 0, task.StepCompleted, "Implemented the fix.")

	digest := d.buildSessionContextDigest("session-ctx")
	if digest.IsEmpty() {
		t.Fatal("fixture digest should be non-empty")
	}

	res, err := d.ClassifyAndRoute(context.Background(), "did the change get made?", "session-ctx", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res != nil && res.ClarificationNeeded {
		t.Fatalf("context-bearing session clarification-gated an ambiguous input; A5 gate (digest.IsEmpty() condition) missing from the real decision")
	}
}
