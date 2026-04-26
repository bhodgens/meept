package llm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAnthropicClient_AdaptiveTimeout verifies that when a
// *metrics.Calculator is configured, the client consults it before issuing
// a request. The timeout is applied via context.WithTimeout (LLM-3 FIX:
// not by mutating the shared httpClient.Timeout) so concurrent calls
// are safe. The concrete Calculator returns the static default while the
// store is in warmup; we assert the context received the correct deadline.
func TestAnthropicClient_AdaptiveTimeout(t *testing.T) {

	// Capture the deadline from the HTTP request's context.
	var capturedDeadline time.Time
	var hasDeadline bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		hasDeadline = ok
		if ok {
			capturedDeadline = deadline
		}
		resp := map[string]any{
			"id":          "m_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer server.Close()

	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    server.URL,
		APIKey:     "test",
		MaxTokens:  128,
	}

	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	calc := metrics.NewCalculator(store, metrics.AdaptiveTimeoutConfig{
		Enabled:        true,
		MinTimeout:     time.Second,
		MaxTimeout:     10 * time.Second,
		WarmupRequests: 10,
		WindowHours:    1,
	})

	c := NewAnthropicClient(cfg,
		WithAnthropicMetricsStore(store),
		WithAnthropicTimeoutCalculator(calc),
	)

	// Store original timeout so we can assert it is NOT mutated by the adaptive path.
	originalTimeout := c.httpClient.Timeout

	_, err = c.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// The adaptive timeout path should have set a deadline on the request context.
	if !hasDeadline {
		t.Error("request context has no deadline; adaptive timeout was not applied")
	}

	// The deadline should be approximately originalTimeout from now.
	expectedWall := time.Now().Add(-originalTimeout).Add(-5 * time.Second)
	if capturedDeadline.Before(expectedWall) {
		t.Errorf("captured deadline %v is too early; expected >= %v",
			capturedDeadline, expectedWall)
	}

	// The HTTP client's timeout must not have been mutated (LLM-3 FIX verification).
	if c.httpClient.Timeout != originalTimeout {
		t.Errorf("httpClient.Timeout was mutated from %v to %v (should be unchanged)",
			originalTimeout, c.httpClient.Timeout)
	}
}

// TestAnthropicClient_RecordsMetrics verifies that after a Chat call, a
// corresponding record is written to the metrics store.
func TestAnthropicClient_RecordsMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":          "m_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer server.Close()

	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	store.StartBackground(context.Background())

	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    server.URL,
		APIKey:     "test",
		MaxTokens:  128,
	}
	c := NewAnthropicClient(cfg, WithAnthropicMetricsStore(store))

	if _, err := c.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Record() is async; poll briefly for the row to appear.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := store.RefreshStats(context.Background()); err != nil {
			t.Fatalf("RefreshStats: %v", err)
		}
		stats, err := store.GetStats(context.Background(), "anthropic", "claude-test", 1)
		if err != nil {
			t.Fatalf("GetStats: %v", err)
		}
		if stats != nil && stats.RequestCount > 0 {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no metrics recorded after Chat")
}

// TestBroker_InjectsMetricsAndTimeout verifies that NewModelBroker passes
// MetricsStore / TimeoutCalc through to the Chatter it builds.
func TestBroker_InjectsMetricsAndTimeout(t *testing.T) {
	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	calc := metrics.NewCalculator(store, metrics.AdaptiveTimeoutConfig{
		Enabled:        true,
		MinTimeout:     time.Second,
		MaxTimeout:     10 * time.Second,
		WarmupRequests: 10,
		WindowHours:    1,
	})

	b := &ModelBroker{
		entries: make(map[string]*brokerEntry),
		config: BrokerConfig{
			MetricsStore: store,
			TimeoutCalc:  calc,
		},
		logger: discardLogger(),
	}

	anthropicCfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-foo",
		BaseURL:    "https://api.anthropic.example",
	}
	openaiCfg := &ModelConfig{
		ProviderID: "openai",
		ModelID:    "gpt-foo",
		BaseURL:    "https://api.openai.example",
	}

	ac, ok := b.newChatterFor(anthropicCfg).(*AnthropicClient)
	if !ok {
		t.Fatal("expected AnthropicClient")
	}
	if ac.metricsStore != store {
		t.Error("Anthropic metricsStore not wired")
	}
	if ac.timeoutCalc != calc {
		t.Error("Anthropic timeoutCalc not wired")
	}

	oc, ok := b.newChatterFor(openaiCfg).(*Client)
	if !ok {
		t.Fatal("expected OpenAI Client")
	}
	if oc.metricsStore != store {
		t.Error("Client metricsStore not wired")
	}
	if oc.timeoutCalc != calc {
		t.Error("Client timeoutCalc not wired")
	}
}

// TestAnthropicClient_BuildRequest_ToolResultPlacement verifies that tool results
// are placed in user messages (LLM-1 FIX) and that index mapping is correct
// when system messages cause index divergence between messages and apiMessages (LLM-2 FIX).
func TestAnthropicClient_BuildRequest_ToolResultPlacement(t *testing.T) {
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    "https://api.anthropic.com",
		APIKey:     "test",
		MaxTokens:  1024,
	}
	c := NewAnthropicClient(cfg)

	// Test case: System message + User + Assistant with tool calls + Tool result
	// This is the scenario that breaks with incorrect index mapping
	messages := []ChatMessage{
		{Role: RoleSystem, Content: "You are a helpful assistant."},
		{Role: RoleUser, Content: "What's the weather?"},
		{Role: RoleAssistant, Content: "Let me check.", ToolCalls: []ToolCall{
			{
				ID:   "tool_123",
				Type: "function",
				Function: ToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"location": "Seattle"}`,
				},
			},
		}},
		{Role: RoleTool, Content: "72F and sunny", ToolCallID: "tool_123"},
		{Role: RoleAssistant, Content: "The weather is 72F and sunny."},
	}

	req, err := c.buildRequest(messages, &chatOptions{maxTokens: 1024}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	// Verify system prompt was extracted
	if req.System != "You are a helpful assistant." {
		t.Errorf("System prompt not extracted correctly, got: %q", req.System)
	}

	// Verify apiMessages structure:
	// [0] user: "What's the weather?"
	// [1] assistant: "Let me check." + tool_use
	// [2] user: tool_result
	// [3] assistant: "The weather is 72F and sunny."
	if len(req.Messages) != 4 {
		t.Fatalf("Expected 4 apiMessages, got %d", len(req.Messages))
	}

	// Check message roles
	expectedRoles := []string{"user", "assistant", "user", "assistant"}
	for i, expected := range expectedRoles {
		if req.Messages[i].Role != expected {
			t.Errorf("Message[%d] role: expected %q, got %q", i, expected, req.Messages[i].Role)
		}
	}

	// LLM-1 FIX: Verify tool result is in a user message (index 2)
	toolResultMsg := req.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Errorf("Tool result should be in user message, got role: %q", toolResultMsg.Role)
	}
	if len(toolResultMsg.Content) != 1 {
		t.Fatalf("Tool result message should have 1 content block, got %d", len(toolResultMsg.Content))
	}
	if toolResultMsg.Content[0].Type != "tool_result" {
		t.Errorf("Tool result content type: expected 'tool_result', got %q", toolResultMsg.Content[0].Type)
	}
	if toolResultMsg.Content[0].ToolUseID != "tool_123" {
		t.Errorf("Tool result ToolUseID: expected 'tool_123', got %q", toolResultMsg.Content[0].ToolUseID)
	}

	// LLM-2 FIX: Verify assistant message with tool calls has correct content
	// (index mapping should correctly find apiMessages[1] for messages[2])
	assistantWithTools := req.Messages[1]
	if assistantWithTools.Role != "assistant" {
		t.Errorf("Expected assistant role at index 1, got %q", assistantWithTools.Role)
	}
	// Should have text + tool_use
	if len(assistantWithTools.Content) != 2 {
		t.Fatalf("Assistant with tools should have 2 content blocks, got %d", len(assistantWithTools.Content))
	}
	// First content should be text
	if assistantWithTools.Content[0].Type != "text" {
		t.Errorf("First content should be text, got %q", assistantWithTools.Content[0].Type)
	}
	if assistantWithTools.Content[0].Text != "Let me check." {
		t.Errorf("Text content mismatch: got %q", assistantWithTools.Content[0].Text)
	}
	// Second content should be tool_use
	if assistantWithTools.Content[1].Type != "tool_use" {
		t.Errorf("Second content should be tool_use, got %q", assistantWithTools.Content[1].Type)
	}
	if assistantWithTools.Content[1].ID != "tool_123" {
		t.Errorf("Tool use ID mismatch: got %q", assistantWithTools.Content[1].ID)
	}
	if assistantWithTools.Content[1].Name != "get_weather" {
		t.Errorf("Tool use name mismatch: got %q", assistantWithTools.Content[1].Name)
	}
}

// TestAnthropicClient_BuildRequest_MultipleSystemMessages verifies correct handling
// when multiple system messages are present (they should all be concatenated).
func TestAnthropicClient_BuildRequest_MultipleSystemMessages(t *testing.T) {
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    "https://api.anthropic.com",
		APIKey:     "test",
		MaxTokens:  1024,
	}
	c := NewAnthropicClient(cfg)

	messages := []ChatMessage{
		{Role: RoleSystem, Content: "System message 1."},
		{Role: RoleSystem, Content: "System message 2."},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there", ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "greet", Arguments: "{}"}},
		}},
	}

	req, err := c.buildRequest(messages, &chatOptions{maxTokens: 1024}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	// System prompts should be concatenated
	expectedSystem := "System message 1.\n\nSystem message 2."
	if req.System != expectedSystem {
		t.Errorf("System prompt mismatch:\nExpected: %q\nGot: %q", expectedSystem, req.System)
	}

	// apiMessages should only have user and assistant (2 messages)
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 apiMessages (system excluded), got %d", len(req.Messages))
	}

	// Verify user message is at index 0
	if req.Messages[0].Role != "user" {
		t.Errorf("Expected user at index 0, got %q", req.Messages[0].Role)
	}

	// LLM-2 FIX: Verify assistant tool calls are patched correctly despite 2 system messages
	// messages[3] maps to apiMessages[1] (not apiMessages[3])
	if req.Messages[1].Role != "assistant" {
		t.Errorf("Expected assistant at index 1, got %q", req.Messages[1].Role)
	}
	if len(req.Messages[1].Content) != 2 {
		t.Fatalf("Expected 2 content blocks (text + tool_use), got %d", len(req.Messages[1].Content))
	}
	if req.Messages[1].Content[1].Type != "tool_use" {
		t.Errorf("Expected tool_use content, got %q", req.Messages[1].Content[1].Type)
	}
}
