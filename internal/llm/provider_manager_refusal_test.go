package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Bughunt F13 pin: a *RefusalError passing through the ProviderManager is
// NOT a provider health failure (AGENTS.md invariant "refusal is not a
// failure"). The manager must return the refusal unchanged and untouched:
// no recordFailure (Health stays Healthy, ConsecutiveFails 0) and NO
// rotation to the next provider (the agent loop's one-hop fallback policy
// owns the retry).
func TestProviderManager_RefusalNotAHealthFailure(t *testing.T) {
	var primaryCalls, backupCalls atomic.Int32

	// Primary serves HTTP 200 with a content_filter finish reason — the
	// leaf-01 refusal signal (a refusal, not an HTTP error).
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-refuse",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": ""}, "finish_reason": "content_filter"}],
			"usage": {"prompt_tokens": 9, "completion_tokens": 0, "total_tokens": 9}
		}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-b","choices":[{"index":0,"message":{"role":"assistant","content":"Backup"},"finish_reason":"stop"}]}`))
	}))
	defer backupServer.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primaryServer.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backupServer.URL, ModelID: "m2"},
		},
		FailoverTimeout: 5 * time.Second,
	})

	resp, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if resp != nil {
		t.Fatalf("refusal must return a nil response, got %+v", resp)
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}

	// NO rotation: the backup must never be dialed.
	if got := backupCalls.Load(); got != 0 {
		t.Errorf("backup calls = %d, want 0 (refusal must not rotate to another provider)", got)
	}
	if got := primaryCalls.Load(); got != 1 {
		t.Errorf("primary calls = %d, want 1 (no internal retry either)", got)
	}

	// Health untouched: refusal is not a failure.
	for _, h := range pm.GetProviderHealth() {
		if h.ProviderID != "primary" {
			continue
		}
		if h.Status != ProviderStatusHealthy {
			t.Errorf("primary status = %s, want %s (refusal is not a health failure)", h.Status, ProviderStatusHealthy)
		}
		if h.ConsecutiveFails != 0 || h.FailureCount != 0 {
			t.Errorf("primary failure counters = consecutive:%d total:%d, want 0/0", h.ConsecutiveFails, h.FailureCount)
		}
	}
}
