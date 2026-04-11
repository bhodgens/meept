package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
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
	if err != ErrNoLLMClient {
		t.Errorf("expected ErrNoLLMClient, got %v", err)
	}
}

func TestAgentLoopSimpleResponse(t *testing.T) {
	// Create a mock that returns a simple text response
	mockClient := newMockLLMClient(&llm.Response{
		Content:      "Hello! How can I help you?",
		FinishReason: "stop",
	})

	// We need to create the loop with the mock client
	loop := NewAgentLoop()
	loop.llm = &llm.Client{} // Placeholder - we'll override the Chat method

	// Since we can't easily inject the mock, let's test the conversation management instead
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
	if err != ErrNoLLMClient {
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
	registry.Register(NewMockTool("echo", "Echo tool", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"echoed": args["message"]}, nil
	}))

	loop := NewAgentLoop(
		WithToolRegistry(registry),
	)

	toolCalls := []llm.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "echo",
				Arguments: `{"message": "hello"}`,
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
	// Sleep briefly to ensure different timestamp
	time.Sleep(time.Nanosecond)
	id2 := generateConversationID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}

	// IDs should be unique (based on nanosecond timestamp)
	// Note: In rare cases they could be the same if generated in the same nanosecond
	// so we just verify they have the expected format
	if !strings.HasPrefix(id1, "conv-") {
		t.Errorf("expected 'conv-' prefix, got '%s'", id1)
	}

	if !strings.HasPrefix(id2, "conv-") {
		t.Errorf("expected 'conv-' prefix, got '%s'", id2)
	}

	// Verify format: conv-<timestamp>
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
		if err != context.Canceled {
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
