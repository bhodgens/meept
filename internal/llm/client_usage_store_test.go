package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// newUsageTestStore builds a metrics.db store bound to a temp dir for
// client-wiring tests.
func newUsageTestStore(t *testing.T) *appmetrics.Store {
	t.Helper()
	s, err := appmetrics.NewStore(&appmetrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestClientChat_RecordsUsageStore drives Client.Chat against a mock server
// that returns usage incl. prompt_tokens_details.cached_tokens, and verifies
// the llm_calls row + model_performance rollup land with agent attribution.
func TestClientChat_RecordsUsageStore(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0,
					"message":       map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens": 1000, "completion_tokens": 50, "total_tokens": 1050,
				"prompt_tokens_details": map[string]any{"cached_tokens": 800},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	store := newUsageTestStore(t)
	client := NewClient(&ModelConfig{BaseURL: server.URL, ModelID: "test-model", ProviderID: "prov-x"})
	client.SetUsageStore(store)

	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithAgentScope("coder"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// recordUsageStore writes async; poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	var got []appmetrics.LLMUsageRow
	for time.Now().Before(deadline) {
		rows, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
		if qerr == nil && len(rows) == 1 {
			got = rows
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 usage row, got %d", len(got))
	}
	r := got[0]
	if r.Provider != "prov-x" || r.AgentID != "coder" || r.Calls != 1 ||
		r.TokensSent != 1000 || r.TokensRecv != 50 || r.TokensCached != 800 || r.Errors != 0 {
		t.Errorf("usage row: got %+v", r)
	}
}

// TestClientChat_NoUsageStore ensures a client without a usage store is
// unaffected (nil-receiver discipline).
func TestClientChat_NoUsageStore(t *testing.T) {
	client := NewClient(&ModelConfig{BaseURL: "http://localhost", ModelID: "m"})
	client.SetUsageStore(nil) // must be a safe no-op
	// No store attached: recordUsageStore no-ops. Nothing to assert beyond
	// "did not panic"; skip an actual request (no server).
}
