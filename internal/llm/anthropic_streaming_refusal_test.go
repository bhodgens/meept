package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Bughunt F14 pin: an Anthropic STREAMING refusal must attribute to the
// request-selected model (WithModelOverride / WithResolvedModel — the
// effective cfg computed per request), not the client's configured default
// stored in c.config. Combines a model override with a refusal: the
// RefusalError's ModelID must name the override model, and its Usage must
// carry the stream's reported tokens (bughunt F12's streaming half).
func TestAnthropicStreamingRefusalAttributesRequestSelectedModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The wire request must carry the overridden model (request-scoped
		// selection reached the wire), while the client's stored config
		// keeps a DIFFERENT default — the stale attribution the fix kills.
		var body struct {
			Model string `json:"model"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if body.Model != "claude-selected" {
			t.Errorf("wire model = %q, want claude-selected", body.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":11}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"id\":\"b1\"}}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"},\"usage\":{\"output_tokens\":3}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	c := NewAnthropicClient(&ModelConfig{
		BaseURL:    srv.URL,
		ProviderID: ProviderIDAnthropic,
		ModelID:    "claude-configured-default",
		APIKey:     "k",
		MaxTokens:  64,
	}, WithAnthropicLogger(discardLogger()))

	// Request-scoped selection: the override names claude-selected. The
	// streaming path is exercised via ChatWithProgress (Chat uses the
	// non-streaming wire format).
	resp, err := c.ChatWithProgress(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {},
		WithModelOverride(&ModelConfig{ProviderID: ProviderIDAnthropic, ModelID: "claude-selected"}))
	if resp != nil {
		t.Fatalf("refusal must return a nil response, got %+v", resp)
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}
	if refusal.Source != "stop_reason" {
		t.Errorf("refusal source = %q, want stop_reason", refusal.Source)
	}
	if refusal.ModelID != "claude-selected" {
		t.Errorf("refusal ModelID = %q, want claude-selected (the request-selected model; pre-F14 this reported claude-configured-default)", refusal.ModelID)
	}
	if refusal.Usage.PromptTokens != 11 || refusal.Usage.CompletionTokens != 3 {
		t.Errorf("refusal Usage = %d/%d, want 11 prompt / 3 completion (F12: refusal must carry usage)", refusal.Usage.PromptTokens, refusal.Usage.CompletionTokens)
	}
}
