package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newMockChatServer returns an OpenAI-compatible chat completions server that
// counts every received request and answers with a deterministic completion.
func newMockChatServer(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		resp := ChatResponse{
			ID:    "chatcmpl-prefix-test",
			Model: "test-model",
			Choices: []Choice{{
				Index: 0,
				Message: ResponseMessage{
					Role:    "assistant",
					Content: json.RawMessage(`"mock answer"`),
				},
				FinishReason: "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// TestClientCacheUsesPrefixAwareKey verifies that the live client cache
// Get/Put path keys on BuildPrefixAwareKey output (static:dynamic:messages)
// instead of the legacy whole-message-list hash:
//
//   - a pre-seeded cache entry under the prefix-aware key must be returned
//     by Chat without hitting the server (Get path uses the prefix-aware key);
//   - a served response must be stored under the prefix-aware key (Put path);
//   - an identical re-ask must HIT;
//   - a call with a shared system prompt but a different trailing user
//     message must MISS yet share the same static key component.
func TestClientCacheUsesPrefixAwareKey(t *testing.T) {
	var serverCalls atomic.Int64
	server := newMockChatServer(t, &serverCalls)
	defer server.Close()

	cacheCfg := CacheConfig{
		L1MaxEntries: 100,
		L2Enabled:    false,
		DefaultTTL:   5 * time.Minute,
		CleanupFreq:  time.Minute,
		Enabled:      true,
	}
	coordinator, err := NewTokenCacheCoordinator(cacheCfg)
	if err != nil {
		t.Fatalf("NewTokenCacheCoordinator: %v", err)
	}
	defer coordinator.Close()

	client := NewClient(&ModelConfig{
		BaseURL:   server.URL,
		ModelID:   "test-model",
		MaxTokens: 10,
	}, WithTokenCache(coordinator))

	systemPrompt := "STATIC SYSTEM PROMPT"
	messagesA := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "question A"},
	}
	messagesB := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "question B"},
	}

	keyBuilder := NewCacheKeyBuilder(true)
	expectedKeyA := keyBuilder.BuildPrefixAwareKey("test-model", []string{systemPrompt}, messagesA)
	expectedKeyB := keyBuilder.BuildPrefixAwareKey("test-model", []string{systemPrompt}, messagesB)

	// Static component must be shared: same system prompt, different messages.
	if expectedKeyA.PromptHash[:16] != expectedKeyB.PromptHash[:16] {
		t.Fatalf("static component differs for shared system prompt: %q vs %q",
			expectedKeyA.PromptHash[:16], expectedKeyB.PromptHash[:16])
	}

	// --- Get path: pre-seed under the prefix-aware key and expect a hit ---
	seeded := &Response{Content: "seeded-prefix-aware"}
	coordinator.Put(context.Background(), expectedKeyA, seeded)

	resp, err := client.Chat(context.Background(), messagesA)
	if err != nil {
		t.Fatalf("Chat (seeded lookup) failed: %v", err)
	}
	if resp.Content != "seeded-prefix-aware" {
		t.Fatalf("Get path did not use the prefix-aware key: got %q, want seeded response", resp.Content)
	}
	if n := serverCalls.Load(); n != 0 {
		t.Fatalf("seeded lookup hit the server %d time(s); Get must resolve the prefix-aware key from cache", n)
	}

	// --- Put path: a served call must be stored under the prefix-aware key ---
	if _, err := client.Chat(context.Background(), messagesB); err != nil {
		t.Fatalf("Chat (messagesB) failed: %v", err)
	}
	if n := serverCalls.Load(); n != 1 {
		t.Fatalf("messagesB server calls = %d, want 1", n)
	}
	if found := coordinator.Inspect(expectedKeyB.PromptHash); len(found) == 0 {
		t.Fatal("Put path did not store the response under the prefix-aware key")
	}

	// --- Identical re-ask must HIT ---
	if _, err := client.Chat(context.Background(), messagesB); err != nil {
		t.Fatalf("Chat (messagesB re-ask) failed: %v", err)
	}
	if n := serverCalls.Load(); n != 1 {
		t.Fatalf("identical re-ask hit the server (calls = %d), want cache HIT", n)
	}
	stats := coordinator.Stats()
	if stats.Hits < 2 {
		t.Fatalf("cache Hits = %d, want >= 2 (seeded lookup + re-ask)", stats.Hits)
	}
}
