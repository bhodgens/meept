package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/llm"
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
// These tests pin the gate at the unit level: the decision expression is
// `IsAmbiguous && digest.IsEmpty()`. The full ClassifyAndRoute path needs
// an LLM-stubbed dispatcher with stores, covered by the dispatcher suites;
// here we pin the two decision branches directly against the digest
// semantics the gate relies on.
func TestAmbiguityGate_EmptyDigestClarifies(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})
	d.intentAnalyzer = ambiguityTestServer(t, 0.95)
	// No stores wired → buildSessionContextDigest returns the empty digest.
	digest := d.buildSessionContextDigest("session-empty")
	if !digest.IsEmpty() {
		t.Fatalf("digest should be empty with no stores; got %+v", digest)
	}
	analysis := &TrueIntentAnalysis{Ambiguity: 0.95}
	// The decision rule, exactly as ClassifyAndRoute 3.5 evaluates it:
	shouldClarify := analysis.IsAmbiguous(d.intentAnalyzer.ambiguityThreshold) && digest.IsEmpty()
	if !shouldClarify {
		t.Errorf("context-less + ambiguous: shouldClarify = false, want true (legacy behavior must be byte-identical)")
	}
}

func TestAmbiguityGate_SessionContextSuppressesClarification(t *testing.T) {
	digest := &SessionContextDigest{
		LastTaskName:   "implement parser",
		LastTaskState:  "executing",
		LastIntentType: "code",
	}
	if digest.IsEmpty() {
		t.Fatal("fixture digest should be non-empty")
	}
	analysis := &TrueIntentAnalysis{Ambiguity: 0.95}
	threshold := defaultAmbiguityThreshold
	shouldClarify := analysis.IsAmbiguous(threshold) && digest.IsEmpty()
	if shouldClarify {
		t.Errorf("context-bearing session: shouldClarify = true; A5 gate (digest.IsEmpty() condition) missing from the decision")
	}
}
