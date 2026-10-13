package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturedPayload is an OpenAI-shaped handler that records the decoded
// request body for later assertions.
func capturedPayload(t *testing.T, body *map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		*body = decoded
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1,
			"model":   "test-model",
			"choices": []map[string]any{
				{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "ok"}},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// TestOpenAIPayloadIncludesPromptCacheKey: a request config carrying a
// session id must marshal a top-level "prompt_cache_key" field equal to it.
func TestOpenAIPayloadIncludesPromptCacheKey(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(capturedPayload(t, &body))
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:   server.URL,
		ModelID:   "test-model",
		MaxTokens: 10,
	})
	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "session-abc123"))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	got, ok := body["prompt_cache_key"]
	if !ok {
		t.Fatalf("payload has no prompt_cache_key field: %v", body)
	}
	if got != "session-abc123" {
		t.Errorf("prompt_cache_key = %v, want session-abc123", got)
	}
}

// TestOpenAIPayloadOmitsPromptCacheKeyWithoutSession: absent session id →
// the field is omitted entirely (never an empty string on the wire).
func TestOpenAIPayloadOmitsPromptCacheKeyWithoutSession(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(capturedPayload(t, &body))
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:   server.URL,
		ModelID:   "test-model",
		MaxTokens: 10,
	})
	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if v, present := body["prompt_cache_key"]; present {
		t.Errorf("prompt_cache_key present without session id: %q", v)
	}
}

// TestToolsSerializedStableOrder: the same tool set registered in two
// different insertion orders must produce byte-identical tools JSON.
func TestToolsSerializedStableOrder(t *testing.T) {
	mk := func(names ...string) []ToolDefinition {
		tools := make([]ToolDefinition, 0, len(names))
		for _, n := range names {
			tools = append(tools, NewToolDefinition(n, "desc of "+n, FunctionParameters{
				Type:       "object",
				Properties: map[string]ParameterProperty{},
			}))
		}
		return tools
	}

	toolsA := mk("zeta", "alpha", "mike", "bravo")
	toolsB := mk("bravo", "mike", "alpha", "zeta")

	marshalTools := func(t *testing.T, tools []ToolDefinition) []byte {
		t.Helper()
		c := NewClient(&ModelConfig{BaseURL: "http://example.test/v1", ModelID: "test-model", MaxTokens: 10})
		_, payload, err := c.buildChatRequest(
			[]ChatMessage{{Role: RoleUser, Content: "hi"}},
			c.config, []ChatOption{WithTools(tools)}, false)
		if err != nil {
			t.Fatalf("buildChatRequest: %v", err)
		}
		raw, err := json.Marshal(payload["tools"])
		if err != nil {
			t.Fatalf("marshal tools: %v", err)
		}
		return raw
	}

	jsonA := marshalTools(t, toolsA)
	jsonB := marshalTools(t, toolsB)

	if string(jsonA) != string(jsonB) {
		t.Errorf("tools JSON differs across insertion orders:\nA: %s\nB: %s", jsonA, jsonB)
	}

	// The serialized order must be name-sorted, not insertion order.
	var ordered []map[string]any
	if err := json.Unmarshal(jsonA, &ordered); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	want := []string{"alpha", "bravo", "mike", "zeta"}
	for i, tool := range ordered {
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name != want[i] {
			t.Errorf("tools[%d].function.name = %q, want %q (full order: %v)", i, name, want[i], want)
			break
		}
	}

	// The sort must not mutate the caller's slice (copy before sort).
	if toolsA[0].Function.Name != "zeta" {
		t.Errorf("caller slice mutated by serialization: toolsA[0] = %q, want zeta", toolsA[0].Function.Name)
	}
}
