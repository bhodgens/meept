package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// chatHandler returns an OpenAI-shaped handler that records request headers
// for later assertions.
func extraHeaderChatHandler(t *testing.T, seen *map[string]string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = map[string]string{
			"X-OpenCode-Session": r.Header.Get("X-OpenCode-Session"),
			"X-Static":           r.Header.Get("X-Static"),
		}
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
	})
}

func TestApplyExtraHeadersModelConfig(t *testing.T) {
	c := NewClient(&ModelConfig{
		BaseURL:      "http://example.test/v1",
		ModelID:      "test-model",
		ExtraHeaders: map[string]string{"X-Static": "v1", "X-OpenCode-Session": sessionIDHeaderSentinel},
	})
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c.applyExtraHeaders(req, c.config, "session-abc123")
	if got := req.Header.Get("X-OpenCode-Session"); got != "session-abc123" {
		t.Errorf("X-OpenCode-Session = %q, want session-abc123", got)
	}
	if got := req.Header.Get("X-Static"); got != "v1" {
		t.Errorf("X-Static = %q, want v1", got)
	}
}

func TestApplyExtraHeadersSessionSentinelOmittedWhenEmpty(t *testing.T) {
	// No WithTaskScope → empty sessionID → sentinel header must NOT be sent
	// (never ship an empty x-opencode-session).
	c := NewClient(&ModelConfig{
		BaseURL:      "http://example.test/v1",
		ModelID:      "test-model",
		ExtraHeaders: map[string]string{"X-OpenCode-Session": sessionIDHeaderSentinel, "X-Static": "v1"},
	})
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c.applyExtraHeaders(req, c.config, "")
	if got := req.Header.Get("X-OpenCode-Session"); got != "" {
		t.Errorf("X-OpenCode-Session = %q, want empty (header omitted)", got)
	}
	if got := req.Header.Get("X-Static"); got != "v1" {
		t.Errorf("X-Static = %q, want v1", got)
	}
}

func TestApplyExtraHeadersClientOptionOverridesModelConfig(t *testing.T) {
	// Client-level extras (WithExtraHeaders) win per key over ModelConfig
	// extras (config-driven).
	c := NewClient(&ModelConfig{
		BaseURL:      "http://example.test/v1",
		ModelID:      "test-model",
		ExtraHeaders: map[string]string{"X-Static": "from-config"},
	}, WithExtraHeaders(map[string]string{"X-Static": "from-option"}))
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c.applyExtraHeaders(req, c.config, "")
	if got := req.Header.Get("X-Static"); got != "from-option" {
		t.Errorf("X-Static = %q, want from-option", got)
	}
}

func TestApplyExtraHeadersEmptyNoOps(t *testing.T) {
	c := NewClient(&ModelConfig{BaseURL: "http://example.test/v1", ModelID: "test-model"})
	req, err := http.NewRequest(http.MethodPost, "http://example.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c.applyExtraHeaders(req, c.config, "session-abc123")
	if got := len(req.Header); got != 0 {
		t.Errorf("header count = %d, want 0 (no extras configured)", got)
	}
}

func TestChatNonStreamingExtraHeadersOnWire(t *testing.T) {
	var seen map[string]string
	server := httptest.NewServer(extraHeaderChatHandler(t, &seen))
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:      server.URL,
		ModelID:      "test-model",
		MaxTokens:    10,
		ExtraHeaders: map[string]string{"X-OpenCode-Session": sessionIDHeaderSentinel},
	})
	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "session-abc123"))
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if seen["X-OpenCode-Session"] != "session-abc123" {
		t.Errorf("wire X-OpenCode-Session = %q, want session-abc123", seen["X-OpenCode-Session"])
	}
}

func TestChatStreamingExtraHeadersOnWire(t *testing.T) {
	var seen map[string]string
	sse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = map[string]string{"X-OpenCode-Session": r.Header.Get("X-OpenCode-Session")}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	server := httptest.NewServer(sse)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:      server.URL,
		ModelID:      "test-model",
		MaxTokens:    10,
		ExtraHeaders: map[string]string{"X-OpenCode-Session": sessionIDHeaderSentinel},
	})
	_, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil }, WithTaskScope("task-1", "session-abc123"))
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback failed: %v", err)
	}
	if seen["X-OpenCode-Session"] != "session-abc123" {
		t.Errorf("wire X-OpenCode-Session = %q, want session-abc123", seen["X-OpenCode-Session"])
	}
}

func TestExpandEnvVarsPreservesSessionSentinel(t *testing.T) {
	// The load-time expander must NOT eat the ${session_id} sentinel even
	// when SESSION_ID / session_id happen to exist in the environment.
	t.Setenv("SESSION_ID", "leaked-env-value")
	t.Setenv("session_id", "leaked-env-value")
	out := expandEnvVars(`{"h":"${session_id}"}`)
	if out != `{"h":"${session_id}"}` {
		t.Errorf("expandEnvVars ate the sentinel: %q", out)
	}
}

func TestModelConfigFromMergesExtraHeaders(t *testing.T) {
	provider := ProviderConfig{
		API: "openai",
		Options: ProviderOptionsConfig{
			BaseURL:      "https://example.test/v1",
			ExtraHeaders: map[string]string{"X-Provider-Level": "p", "X-Shared": "p"},
		},
		Models: map[string]ModelDef{
			"m1": {
				Name:         "m1",
				ExtraHeaders: map[string]string{"X-Shared": "m", "X-Model-Level": "m"},
			},
		},
	}
	mc := modelConfigFrom("prov", "m1", provider, provider.Models["m1"])
	if mc.ExtraHeaders["X-Provider-Level"] != "p" {
		t.Errorf("X-Provider-Level = %q, want p", mc.ExtraHeaders["X-Provider-Level"])
	}
	if mc.ExtraHeaders["X-Shared"] != "m" {
		t.Errorf("X-Shared = %q, want m (model overrides provider)", mc.ExtraHeaders["X-Shared"])
	}
	if mc.ExtraHeaders["X-Model-Level"] != "m" {
		t.Errorf("X-Model-Level = %q, want m", mc.ExtraHeaders["X-Model-Level"])
	}
}

func TestMergeProviderConfigMergesExtraHeaders(t *testing.T) {
	base := ProviderConfig{
		API: "openai",
		Options: ProviderOptionsConfig{
			BaseURL:      "https://base.test/v1",
			ExtraHeaders: map[string]string{"X-Base-Only": "b", "X-Shared": "b"},
		},
	}
	overlay := ProviderConfig{
		Options: ProviderOptionsConfig{
			BaseURL:      "https://overlay.test/v1",
			ExtraHeaders: map[string]string{"X-Shared": "o", "X-Overlay-Only": "o"},
		},
	}
	merged := mergeProviderConfig(base, overlay)
	if merged.Options.ExtraHeaders["X-Base-Only"] != "b" {
		t.Errorf("X-Base-Only = %q, want b (base entry survives)", merged.Options.ExtraHeaders["X-Base-Only"])
	}
	if merged.Options.ExtraHeaders["X-Shared"] != "o" {
		t.Errorf("X-Shared = %q, want o (overlay wins)", merged.Options.ExtraHeaders["X-Shared"])
	}
	if merged.Options.ExtraHeaders["X-Overlay-Only"] != "o" {
		t.Errorf("X-Overlay-Only = %q, want o", merged.Options.ExtraHeaders["X-Overlay-Only"])
	}
}

func TestResolveModelRefParsesExtraHeaders(t *testing.T) {
	cfg := &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"opencode": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL:      "https://opencode.ai/zen/v1",
					ExtraHeaders: map[string]string{"x-opencode-session": sessionIDHeaderSentinel},
				},
				Models: map[string]ModelDef{
					"kimi": {Name: "kimi-k2.6"},
				},
			},
		},
	}
	mc := ResolveModelRef("opencode/kimi", cfg)
	if mc == nil {
		t.Fatal("ResolveModelRef returned nil")
	}
	if got := mc.ExtraHeaders["x-opencode-session"]; got != sessionIDHeaderSentinel {
		t.Errorf("x-opencode-session = %q, want sentinel %q", got, sessionIDHeaderSentinel)
	}
}
