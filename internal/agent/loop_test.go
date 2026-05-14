package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// mockLLMClient is a mock LLM client for testing.
type mockLLMClient struct {
	responses   []*llm.Response
	callCount   int
	lastRequest []llm.ChatMessage
}

func newMockLLMClient(responses ...*llm.Response) *mockLLMClient {
	return &mockLLMClient{
		responses: responses,
	}
}

func (m *mockLLMClient) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.lastRequest = messages
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockLLMClient) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *mockLLMClient) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "mock-model"}
}

// mockChatter implements llm.Chatter for testing the terminate path.
type mockChatter struct {
	responses []*llm.Response
	callCount int
}

func newMockChatter(responses ...*llm.Response) *mockChatter {
	return &mockChatter{responses: responses}
}

func (m *mockChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *mockChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "mock-chatter"}
}

func TestNewAgentLoop(t *testing.T) {
	loop := NewAgentLoop()

	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}

	config := loop.GetConfig()
	if config.MaxIterations != DefaultMaxIterations {
		t.Errorf("expected MaxIterations=%d, got %d", DefaultMaxIterations, config.MaxIterations)
	}
}

func TestAgentLoopWithOptions(t *testing.T) {
	customConfig := AgentConfig{
		MaxIterations: 5,
		Constitution:  "Custom constitution",
	}

	loop := NewAgentLoop(
		WithAgentConfig(customConfig),
	)

	config := loop.GetConfig()
	if config.MaxIterations != 5 {
		t.Errorf("expected MaxIterations=5, got %d", config.MaxIterations)
	}
	if config.Constitution != "Custom constitution" {
		t.Errorf("expected custom constitution")
	}
}

func TestAgentLoopNoLLMClient(t *testing.T) {
	loop := NewAgentLoop()

	_, err := loop.RunOnce(context.Background(), "Hello", "test-conv")
	if !errors.Is(err, ErrNoLLMClient) {
		t.Errorf("expected ErrNoLLMClient, got %v", err)
	}
}

func TestConversationAndMockClient(t *testing.T) {
	// Create a mock that returns a simple text response
	mockClient := newMockLLMClient(&llm.Response{
		Content:      "Hello! How can I help you?",
		FinishReason: "stop",
	})

	// Test conversation management
	loop := NewAgentLoop()
	conv := loop.conversations.Get("test-conv")
	conv.AddUserMessage("Hello")

	if conv.Len() != 1 {
		t.Errorf("expected 1 message, got %d", conv.Len())
	}

	// Verify mock client works
	resp, err := mockClient.Chat(context.Background(), nil)
	if err != nil {
		t.Errorf("mock client error: %v", err)
	}
	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("unexpected response: %s", resp.Content)
	}
}

func TestAgentLoopBuildSystemPrompt(t *testing.T) {
	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Constitution: "Test constitution",
			Restrictions: "Test restrictions",
			Purpose:      "Test purpose",
			Personality:  "Test personality",
		}),
	)

	prompt := loop.buildSystemPrompt()

	if !strings.Contains(prompt, "Test constitution") {
		t.Error("prompt missing constitution")
	}
	if !strings.Contains(prompt, "Test restrictions") {
		t.Error("prompt missing restrictions")
	}
	if !strings.Contains(prompt, "Test purpose") {
		t.Error("prompt missing purpose")
	}
	if !strings.Contains(prompt, "Test personality") {
		t.Error("prompt missing personality")
	}
}

func TestAgentLoopBuildSystemPromptWithOverride(t *testing.T) {
	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			SystemPromptOveride: "Complete custom prompt",
		}),
	)

	prompt := loop.buildSystemPrompt()

	if prompt != "Complete custom prompt" {
		t.Errorf("expected override prompt, got: %s", prompt)
	}
}

func TestAgentLoopBuildSystemPromptWithToolRegistry(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("test_tool", "A test tool", nil))

	loop := NewAgentLoop(
		WithToolRegistry(registry),
	)

	prompt := loop.buildSystemPrompt()

	// Tool descriptions should NOT be in the system prompt since they are
	// sent via the API's tools parameter to avoid duplication.
	if strings.Contains(prompt, "test_tool") {
		t.Error("system prompt should not contain tool descriptions (they are sent via API tools parameter)")
	}
}

func TestAgentLoopConversationManagement(t *testing.T) {
	loop := NewAgentLoop()

	// Get a conversation
	conv1 := loop.GetConversation("conv1")
	if conv1 != nil {
		t.Error("expected nil for non-existent conversation")
	}

	// Create through conversations store
	loop.conversations.Get("conv2")

	conv2 := loop.GetConversation("conv2")
	if conv2 == nil {
		t.Error("expected to find conversation")
	}

	// Clear conversation
	loop.ClearConversation("conv2")

	conv2After := loop.GetConversation("conv2")
	if conv2After != nil {
		t.Error("expected conversation to be cleared")
	}
}

func TestAgentLoopSetConfig(t *testing.T) {
	loop := NewAgentLoop()

	newConfig := AgentConfig{
		MaxIterations: 15,
		Constitution:  "New constitution",
	}

	loop.SetConfig(newConfig)

	config := loop.GetConfig()
	if config.MaxIterations != 15 {
		t.Errorf("expected MaxIterations=15, got %d", config.MaxIterations)
	}
}

func TestAgentLoopHandleMessage(t *testing.T) {
	loop := NewAgentLoop()

	// Without LLM client, should return error
	_, err := loop.HandleMessage(context.Background(), "Hello")
	if !errors.Is(err, ErrNoLLMClient) {
		t.Errorf("expected ErrNoLLMClient, got %v", err)
	}
}

func TestAgentLoopExecuteToolCalls(t *testing.T) {
	// Test with no executor
	loop := NewAgentLoop()

	toolCalls := []llm.ToolCall{
		{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "test_tool", Arguments: "{}"},
		},
	}

	results := loop.executeToolCalls(context.Background(), toolCalls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Success {
		t.Error("expected failure when executor not configured")
	}

	if results[0].Error != "tool execution not configured" {
		t.Errorf("unexpected error: %s", results[0].Error)
	}
}

func TestAgentLoopWithExecutor(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "File read", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "test content"}, nil
	}))

	// Create security checker with default config
	secChecker := security.NewPermissionChecker(security.Config{})

	loop := NewAgentLoop(
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
	)

	toolCalls := []llm.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "file_read",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}

	results := loop.executeToolCalls(context.Background(), toolCalls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got error: %s", results[0].Error)
	}
}

func TestAgentMessage(t *testing.T) {
	msg := AgentMessage{
		ID:             "msg-123",
		ConversationID: "conv-456",
		Content:        "Hello",
		Source:         "cli",
	}

	if msg.ID != "msg-123" {
		t.Errorf("expected ID='msg-123', got '%s'", msg.ID)
	}
}

func TestAgentResponse(t *testing.T) {
	resp := AgentResponse{
		ConversationID: "conv-456",
		Content:        "Hi there",
		Error:          nil,
		ReplyTo:        "msg-123",
	}

	if resp.ConversationID != "conv-456" {
		t.Error("unexpected conversation ID")
	}
}

func TestGenerateConversationID(t *testing.T) {
	id1 := generateConversationID()
	id2 := generateConversationID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}

	// Verify IDs are unique
	if id1 == id2 {
		t.Errorf("expected unique IDs, but got identical: %s", id1)
	}

	// Verify format: conv-<timestamp>
	if !strings.HasPrefix(id1, "conv-") {
		t.Errorf("expected 'conv-' prefix, got '%s'", id1)
	}

	if !strings.HasPrefix(id2, "conv-") {
		t.Errorf("expected 'conv-' prefix, got '%s'", id2)
	}

	if len(id1) < 10 {
		t.Errorf("ID too short: %s", id1)
	}
}

func TestDefaultAgentConfig(t *testing.T) {
	cfg := DefaultAgentConfig()

	if cfg.MaxIterations != DefaultMaxIterations {
		t.Errorf("expected MaxIterations=%d, got %d", DefaultMaxIterations, cfg.MaxIterations)
	}

	if cfg.Timeout != DefaultTimeout {
		t.Errorf("expected Timeout=%v, got %v", DefaultTimeout, cfg.Timeout)
	}

	if cfg.Constitution == "" {
		t.Error("expected non-empty default constitution")
	}
}

func TestAgentLoopRunChannel(t *testing.T) {
	loop := NewAgentLoop()

	messages := make(chan *AgentMessage, 1)
	responses := make(chan *AgentResponse, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start the loop in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(ctx, messages, responses)
	}()

	// Close the channel to signal completion
	close(messages)

	// Wait for the loop to finish
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("loop did not finish in time")
	}
}

func TestAgentLoopRunWithContextCancel(t *testing.T) {
	loop := NewAgentLoop()

	messages := make(chan *AgentMessage)
	responses := make(chan *AgentResponse, 1)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(ctx, messages, responses)
	}()

	// Cancel the context
	cancel()

	// Wait for the loop to finish
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("loop did not finish in time")
	}
}

func TestErrorConstants(t *testing.T) {
	// Verify error constants are defined
	if ErrMaxIterationsReached == nil {
		t.Error("ErrMaxIterationsReached should not be nil")
	}
	if ErrContextCancelled == nil {
		t.Error("ErrContextCancelled should not be nil")
	}
	if ErrNoLLMClient == nil {
		t.Error("ErrNoLLMClient should not be nil")
	}

	// Verify they have meaningful messages
	if ErrMaxIterationsReached.Error() == "" {
		t.Error("ErrMaxIterationsReached should have a message")
	}
}

func TestAgentLoopPublishAction(t *testing.T) {
	// Test without bus (should not panic)
	loop := NewAgentLoop()

	toolCalls := []llm.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "test_tool",
				Arguments: "{}",
			},
		},
	}

	// Should not panic
	loop.publishAction("conv-1", 1, toolCalls)
}

func TestAgentLoopPublishResult(t *testing.T) {
	// Test without bus (should not panic)
	loop := NewAgentLoop()

	results := []*ExecutionResult{
		{
			ToolCallID: "call_1",
			Success:    true,
			Result:     "test",
		},
	}

	// Should not panic
	loop.publishResult("conv-1", 1, results)
}

func TestAgentLoopDiscoverRelevantSkills(t *testing.T) {
	loop := NewAgentLoop()

	// Without capability index, should return nil
	result := loop.discoverRelevantSkills("write code", 0.5)
	if result != nil {
		t.Error("expected nil when no capability index configured")
	}
}

func TestAgentLoopSetCapabilityIndex(t *testing.T) {
	loop := NewAgentLoop()

	// Initially should be nil
	if loop.capabilityIndex != nil {
		t.Error("capabilityIndex should be nil initially")
	}

	// Create a mock capability index
	idx := createTestCapabilityIndex()
	loop.SetCapabilityIndex(idx)

	// Should now be set
	if loop.capabilityIndex == nil {
		t.Error("capabilityIndex should be set after SetCapabilityIndex")
	}
}

func TestAgentLoopSetSkillLoader(t *testing.T) {
	loop := NewAgentLoop()

	// Initially should be nil
	if loop.skillLoader != nil {
		t.Error("skillLoader should be nil initially")
	}

	// Note: We can't easily test SetSkillLoader without a full skill index setup
	// Just verify it doesn't panic
	loop.SetSkillLoader(nil)
}

func TestAgentLoopDiscoverRelevantSkillsWithIndex(t *testing.T) {
	loop := NewAgentLoop()

	// Create and set a test capability index
	idx := createTestCapabilityIndex()
	loop.SetCapabilityIndex(idx)

	// Should find skills matching input
	result := loop.discoverRelevantSkills("code review", 0.3)
	if result == nil {
		t.Error("expected to find skills matching 'code review'")
	}

	// With high threshold, might not find matches
	result = loop.discoverRelevantSkills("xyz random", 0.9)
	if len(result) > 0 {
		t.Log("Unexpected match for random input:", result[0].Entry.Name)
	}
}

func TestBuildSkillContextSection(t *testing.T) {
	loop := NewAgentLoop()

	// Empty discovered list should return empty string
	result := loop.buildSkillContextSection(context.Background(), nil)
	if result != "" {
		t.Error("expected empty string for nil discovered skills")
	}

	result = loop.buildSkillContextSection(context.Background(), []*DiscoveredSkill{})
	if result != "" {
		t.Error("expected empty string for empty discovered skills")
	}
}

func TestFormatSkillForPrompt(t *testing.T) {
	skill := &skills.Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Body:        "This is the skill body with instructions.",
	}

	formatted := formatSkillForPrompt(skill)

	if !strings.Contains(formatted, "test-skill") {
		t.Error("formatted skill should contain name")
	}
	if !strings.Contains(formatted, "A test skill") {
		t.Error("formatted skill should contain description")
	}
	if !strings.Contains(formatted, "This is the skill body") {
		t.Error("formatted skill should contain body")
	}
}

// createTestCapabilityIndex creates a minimal capability index for testing.
func createTestCapabilityIndex() *skills.CapabilityIndex {
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "code-review",
		Description: "Review code for quality and best practices",
		Tags:        []string{"coding", "review"},
		Examples:    []string{"review this code", "check code quality"},
	})
	idx.Index(&skills.SkillIndexEntry{
		Name:        "test-runner",
		Description: "Run tests and verify functionality",
		Tags:        []string{"testing"},
		Examples:    []string{"run the tests", "execute test suite"},
	})

	return skills.BuildCapabilityIndex(idx)
}

func TestRecallModeDisabledGatesMemoryTools(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	// Register a memory tool and a non-memory tool
	registry.Register(NewMockTool("memory_search", "search memories", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"results": []any{}}, nil
	}))
	registry.Register(NewMockTool("memory_store", "store memories", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"success": true}, nil
	}))
	registry.Register(NewMockTool("file_read", "read files", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "test"}, nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Memory: AgentMemoryConfig{
				RecallMode: RecallModeDisabled,
			},
		}),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
	)
	loop.executor = NewExecutor(registry, secChecker)

	toolCalls := []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "memory_search", Arguments: `{"query":"test"}`}},
		{ID: "tc-2", Function: llm.ToolCallFunction{Name: "file_read", Arguments: `{"path":"/tmp/test"}`}},
		{ID: "tc-3", Function: llm.ToolCallFunction{Name: "memory_store", Arguments: `{"content":"test"}`}},
	}

	results := loop.executeToolCalls(context.Background(), toolCalls)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First tool (memory_search) should be blocked
	if results[0].Success {
		t.Error("memory_search should be blocked when recall mode is disabled")
	}
	if results[0].ToolCallID != "tc-1" {
		t.Errorf("expected tool call ID tc-1, got %s", results[0].ToolCallID)
	}
	if !strings.Contains(results[0].Error, "blocked") {
		t.Errorf("expected blocked error, got: %s", results[0].Error)
	}

	// Second tool (file_read) should succeed
	if !results[1].Success {
		t.Errorf("file_read should succeed, got error: %s", results[1].Error)
	}

	// Third tool (memory_store) should be blocked
	if results[2].Success {
		t.Error("memory_store should be blocked when recall mode is disabled")
	}
}

func TestRecallModeAutoAllowsMemoryTools(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("memory_search", "search memories", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"results": []any{}}, nil
	}))

	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Memory: AgentMemoryConfig{
				RecallMode: RecallModeAuto,
			},
		}),
		WithToolRegistry(registry),
	)
	loop.executor = NewExecutor(registry, nil)

	toolCalls := []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "memory_search", Arguments: `{"query":"test"}`}},
	}

	results := loop.executeToolCalls(context.Background(), toolCalls)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Memory tool should succeed when recall mode is auto
	if !results[0].Success {
		t.Errorf("memory_search should succeed with auto mode, got error: %s", results[0].Error)
	}
}

func TestSnapshotCachingEnabledControlsFreeze(t *testing.T) {
	// Test that the default config has SnapshotCachingEnabled=true (backwards compat)
	defaultCfg := DefaultAgentConfig()
	if !defaultCfg.Memory.SnapshotCachingEnabled {
		t.Error("default SnapshotCachingEnabled should be true for backwards compatibility")
	}

	// Test that the config can disable it
	cfg := AgentConfig{
		Memory: AgentMemoryConfig{
			RecallMode:             RecallModeAuto,
			SnapshotCachingEnabled: false,
		},
	}
	loop := NewAgentLoop(WithAgentConfig(cfg))

	if loop.config.Memory.SnapshotCachingEnabled {
		t.Error("SnapshotCachingEnabled should be false when explicitly set")
	}
}

func TestShouldAutoInject(t *testing.T) {
	tests := []struct {
		mode     MemoryRecallMode
		expected bool
	}{
		{RecallModeAuto, true},
		{RecallModeOnQuery, false},
		{RecallModeHybrid, true},
		{RecallModeDisabled, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			loop := NewAgentLoop(WithAgentConfig(AgentConfig{
				Memory: AgentMemoryConfig{RecallMode: tt.mode},
			}))
			got := loop.shouldAutoInject()
			if got != tt.expected {
				t.Errorf("shouldAutoInject(%s) = %v, want %v", tt.mode, got, tt.expected)
			}
		})
	}
}

func TestShouldFetchOnQuery(t *testing.T) {
	tests := []struct {
		mode     MemoryRecallMode
		expected bool
	}{
		{RecallModeAuto, false},
		{RecallModeOnQuery, true},
		{RecallModeHybrid, true},
		{RecallModeDisabled, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			loop := NewAgentLoop(WithAgentConfig(AgentConfig{
				Memory: AgentMemoryConfig{RecallMode: tt.mode},
			}))
			got := loop.shouldFetchOnQuery()
			if got != tt.expected {
				t.Errorf("shouldFetchOnQuery(%s) = %v, want %v", tt.mode, got, tt.expected)
			}
		})
	}
}

func TestAgentLoop_PublishTokenUsage(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())

	// Subscribe to llm.tokens.used
	sub := bus.Subscribe("test", "llm.tokens.used")
	defer bus.Unsubscribe(sub)

	loop := NewAgentLoop(WithMessageBus(bus))

	// Publish token usage
	loop.publishTokenUsage("conv-1", 1500)

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if tokens, ok := payload["total_tokens"].(float64); !ok || tokens != 1500 {
			t.Errorf("expected 1500 tokens, got %v", payload["total_tokens"])
		}
		if convID, ok := payload["conversation_id"].(string); !ok || convID != "conv-1" {
			t.Errorf("expected conversation_id=conv-1, got %v", payload["conversation_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for token usage event")
	}
}

func TestEmitToolExecutionStart(t *testing.T) {
	testBus := bus.New(nil, slogDiscardLogger())

	// Subscribe to the legacy "agent.action" topic that the bridge publishes to.
	sub := testBus.Subscribe("test-emit-action", "agent.action")
	defer testBus.Unsubscribe(sub)

	emitter := NewEventEmitter("test-agent", testBus, slogDiscardLogger())

	emitter.Emit(context.Background(), AgentEventToolExecutionStart, ToolExecutionStartData{
		ToolCallID: "call_42",
		ToolName:   "file_read",
		Arguments:  `{"path":"/tmp/test.txt"}`,
	})

	select {
	case msg := <-sub.Channel:
		var raw map[string]any
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if raw["type"] != string(AgentEventToolExecutionStart) {
			t.Errorf("expected type %s, got %v", AgentEventToolExecutionStart, raw["type"])
		}
		data, ok := raw["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data to be map, got %T", raw["data"])
		}
		if data["tool_name"] != "file_read" {
			t.Errorf("expected tool_name=file_read, got %v", data["tool_name"])
		}
		if data["tool_call_id"] != "call_42" {
			t.Errorf("expected tool_call_id=call_42, got %v", data["tool_call_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for agent.action legacy topic from emitter bridge")
	}
}

func TestEmitToolExecutionEnd(t *testing.T) {
	testBus := bus.New(nil, slogDiscardLogger())

	sub := testBus.Subscribe("test-emit-result", "agent.result")
	defer testBus.Unsubscribe(sub)

	emitter := NewEventEmitter("test-agent", testBus, slogDiscardLogger())

	emitter.Emit(context.Background(), AgentEventToolExecutionEnd, ToolExecutionEndData{
		ToolCallID: "call_99",
		ToolName:   "file_read",
		Success:    true,
		Result:     "file contents here",
	})

	select {
	case msg := <-sub.Channel:
		var raw map[string]any
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if raw["type"] != string(AgentEventToolExecutionEnd) {
			t.Errorf("expected type %s, got %v", AgentEventToolExecutionEnd, raw["type"])
		}
		data, ok := raw["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data to be map, got %T", raw["data"])
		}
		if data["tool_name"] != "file_read" {
			t.Errorf("expected tool_name=file_read, got %v", data["tool_name"])
		}
		if data["success"] != true {
			t.Errorf("expected success=true, got %v", data["success"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for agent.result legacy topic from emitter bridge")
	}
}

func TestEmitAfterProviderResponse(t *testing.T) {
	testBus := bus.New(nil, slogDiscardLogger())

	sub := testBus.Subscribe("test-emit-tokens", "llm.tokens.used")
	defer testBus.Unsubscribe(sub)

	emitter := NewEventEmitter("test-agent", testBus, slogDiscardLogger())

	emitter.Emit(context.Background(), AgentEventAfterProviderResponse, AfterProviderResponseData{
		ModelID:        "mock-model",
		StatusCode:     200,
		ResponseTokens: 1500,
		Latency:        120 * time.Millisecond,
	})

	select {
	case msg := <-sub.Channel:
		var raw map[string]any
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if raw["type"] != string(AgentEventAfterProviderResponse) {
			t.Errorf("expected type %s, got %v", AgentEventAfterProviderResponse, raw["type"])
		}
		data, ok := raw["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data to be map, got %T", raw["data"])
		}
		if tokens, _ := data["response_tokens"].(float64); tokens != 1500 {
			t.Errorf("expected response_tokens=1500, got %v", data["response_tokens"])
		}
		if data["model_id"] != "mock-model" {
			t.Errorf("expected model_id=mock-model, got %v", data["model_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for llm.tokens.used legacy topic from emitter bridge")
	}
}

func TestEmitTurnStart(t *testing.T) {
	testBus := bus.New(nil, slogDiscardLogger())

	sub := testBus.Subscribe("test-emit-progress", "agent.progress")
	defer testBus.Unsubscribe(sub)

	emitter := NewEventEmitter("test-agent", testBus, slogDiscardLogger())

	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{
		TurnNumber:       3,
		TotalTokensSoFar: 4500,
		MessagesCount:    12,
		ToolCount:        2,
	})

	select {
	case msg := <-sub.Channel:
		var raw map[string]any
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if raw["type"] != string(AgentEventTurnStart) {
			t.Errorf("expected type %s, got %v", AgentEventTurnStart, raw["type"])
		}
		data, ok := raw["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data to be map, got %T", raw["data"])
		}
		if turn, _ := data["turn_number"].(float64); turn != 3 {
			t.Errorf("expected turn_number=3, got %v", data["turn_number"])
		}
		if tokens, _ := data["total_tokens_so_far"].(float64); tokens != 4500 {
			t.Errorf("expected total_tokens_so_far=4500, got %v", data["total_tokens_so_far"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for agent.progress legacy topic from emitter bridge")
	}
}

func TestBuildTerminateResponse(t *testing.T) {
	t.Run("single successful result", func(t *testing.T) {
		loop := NewAgentLoop()
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Result: map[string]any{"answer": 42}},
		}
		got := loop.buildTerminateResponse(results)
		if !strings.Contains(got, `"answer"`) || !strings.Contains(got, "42") {
			t.Errorf("expected result content, got: %s", got)
		}
	})

	t.Run("multiple successful results joined", func(t *testing.T) {
		loop := NewAgentLoop()
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Result: "first"},
			{ToolCallID: "c2", Success: true, Result: "second"},
		}
		got := loop.buildTerminateResponse(results)
		if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
			t.Errorf("expected both results, got: %s", got)
		}
	})

	t.Run("skips failed results", func(t *testing.T) {
		loop := NewAgentLoop()
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: false, Error: "failed"},
			{ToolCallID: "c2", Success: true, Result: "ok"},
		}
		got := loop.buildTerminateResponse(results)
		if !strings.Contains(got, "ok") {
			t.Errorf("expected successful result, got: %s", got)
		}
		if strings.Contains(got, "failed") {
			t.Error("should not contain failed result")
		}
	})

	t.Run("all failed returns done", func(t *testing.T) {
		loop := NewAgentLoop()
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: false, Error: "err"},
		}
		got := loop.buildTerminateResponse(results)
		if got != "done" {
			t.Errorf("expected 'done', got: %s", got)
		}
	})
}

func TestShouldTerminate_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty slice returns false", func(t *testing.T) {
		if ShouldTerminate(nil) {
			t.Error("expected false for nil slice")
		}
		if ShouldTerminate([]*ExecutionResult{}) {
			t.Error("expected false for empty slice")
		}
	})

	t.Run("all terminate true returns true", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			{ToolCallID: "c2", Success: true, Terminate: true},
		}
		if !ShouldTerminate(results) {
			t.Error("expected true when all results have Terminate=true")
		}
	})

	t.Run("mixed terminate returns false", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			{ToolCallID: "c2", Success: true, Terminate: false},
		}
		if ShouldTerminate(results) {
			t.Error("expected false when not all results have Terminate=true")
		}
	})

	t.Run("nil result in slice returns false", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			nil,
		}
		if ShouldTerminate(results) {
			t.Error("expected false when a result is nil")
		}
	})

	t.Run("single result with terminate true", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
		}
		if !ShouldTerminate(results) {
			t.Error("expected true for single terminating result")
		}
	})

	t.Run("single result with terminate false", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: false},
		}
		if ShouldTerminate(results) {
			t.Error("expected false for single non-terminating result")
		}
	})
}

func TestShouldTerminate_IntegrationWithLoopPath(t *testing.T) {
	t.Parallel()

	// Verify that when ShouldTerminate returns true, the loop returns
	// buildTerminateResponse output without making a second LLM call.
	// This uses a mockChatter that would fail if called a second time
	// (by providing only one response), proving the follow-up never happens.

	chatter := newMockChatter(
		&llm.Response{
			Content:      "checking platform status",
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "tc-1", Type: "function", Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"}},
			},
		},
		// No second response -- if the loop calls Chat() again, it panics with
		// "no more mock responses", proving termination skipped the follow-up.
	)

	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockTerminatingLoopTool{name: "platform_status"})

	secChecker := security.NewPermissionChecker(security.Config{})
	testBus := bus.New(nil, slogDiscardLogger())
	executor := NewExecutor(registry, secChecker)

	loop := NewAgentLoop(
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(testBus),
		WithAgentConfig(AgentConfig{MaxIterations: 10}),
	)
	loop.executor = executor

	response, err := loop.RunOnce(context.Background(), "show status", "conv-integration")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Response must come from buildTerminateResponse, not from an LLM follow-up
	if !strings.Contains(response, "final answer from terminating tool") {
		t.Errorf("expected terminate response content, got: %s", response)
	}

	// The chatter must have been called exactly once
	if chatter.callCount != 1 {
		t.Errorf("expected exactly 1 LLM call (terminate should skip follow-up), got %d",
			chatter.callCount)
	}
}

// mockTerminatingLoopTool returns a ToolResult with Terminate=true for the loop test.
type mockTerminatingLoopTool struct {
	name string
}

func (t *mockTerminatingLoopTool) Name() string        { return t.name }
func (t *mockTerminatingLoopTool) Description() string { return "terminating test tool" }
func (t *mockTerminatingLoopTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (t *mockTerminatingLoopTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return &tools.ToolResult{
		Success:   true,
		Result:    "final answer from terminating tool",
		Terminate: true,
	}, nil
}

func TestAgentLoop_TerminatePathReturnsToolResults(t *testing.T) {
	// Set up a terminating tool in the registry
	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockTerminatingLoopTool{name: "platform_status"})

	secChecker := security.NewPermissionChecker(security.Config{})

	// Create an executor with the terminating tool
	executor := NewExecutor(registry, secChecker)

	// Create the loop with the executor
	loop := NewAgentLoop(
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
	)
	loop.executor = executor

	// Execute tool calls directly and verify the terminate path
	toolCalls := []llm.ToolCall{
		{ID: "tc-1", Type: "function", Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"}},
	}

	results := loop.executeToolCalls(context.Background(), toolCalls)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got error: %s", results[0].Error)
	}
	if !results[0].Terminate {
		t.Error("expected Terminate=true from terminating tool")
	}

	// Verify ShouldTerminate correctly identifies all-terminate batch
	if !ShouldTerminate(results) {
		t.Error("ShouldTerminate should return true when all results have Terminate=true")
	}

	// Verify buildTerminateResponse produces expected output
	response := loop.buildTerminateResponse(results)
	if !strings.Contains(response, "final answer from terminating tool") {
		t.Errorf("expected tool result in response, got: %s", response)
	}
}

func TestAgentLoop_TerminatePathSkipsLLMFollowUp(t *testing.T) {
	// This test verifies that when all tools signal termination,
	// no additional LLM call is needed. We use the mock chatter
	// and verify it is never called (callCount stays 0).
	chatter := newMockChatter(
		// First response: LLM returns a tool call to the terminating tool
		&llm.Response{
			Content:      "I will look that up",
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "tc-term", Type: "function", Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"}},
			},
		},
		// Second response: should never be reached due to termination
		&llm.Response{
			Content:      "this should never be reached",
			FinishReason: "stop",
		},
	)

	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockTerminatingLoopTool{name: "platform_status"})

	secChecker := security.NewPermissionChecker(security.Config{})
	testBus := bus.New(nil, slogDiscardLogger())
	executor := NewExecutor(registry, secChecker)

	loop := NewAgentLoop(
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(testBus),
		WithAgentConfig(AgentConfig{MaxIterations: 10}),
	)
	loop.executor = executor

	// Run the loop
	response, err := loop.RunOnce(context.Background(), "test query", "conv-term-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The response should contain the tool result, not the LLM follow-up
	if !strings.Contains(response, "final answer from terminating tool") {
		t.Errorf("expected tool result in response, got: %s", response)
	}

	// The LLM chatter should have been called exactly once (for the initial tool call response)
	// The second response should NOT have been consumed because termination skipped the follow-up
	if chatter.callCount != 1 {
		t.Errorf("expected exactly 1 LLM call, got %d (terminate should skip follow-up)", chatter.callCount)
	}
}
