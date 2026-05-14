package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

func TestExecutionResult(t *testing.T) {
	t.Run("ToJSON success", func(t *testing.T) {
		result := &ExecutionResult{
			ToolCallID: "call_123",
			Success:    true,
			Result:     map[string]string{"key": "value"},
		}

		jsonStr := result.ToJSON()
		if jsonStr == "" {
			t.Error("expected non-empty JSON")
		}

		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Errorf("failed to parse JSON: %v", err)
		}

		if parsed["success"] != true {
			t.Error("expected success=true")
		}
	})

	t.Run("ToJSON error", func(t *testing.T) {
		result := &ExecutionResult{
			ToolCallID: "call_456",
			Success:    false,
			Error:      "something went wrong",
		}

		jsonStr := result.ToJSON()

		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Errorf("failed to parse JSON: %v", err)
		}

		if parsed["success"] != false {
			t.Error("expected success=false")
		}

		if parsed["error"] != "something went wrong" {
			t.Error("expected error message")
		}
	})

	t.Run("ToChatMessage", func(t *testing.T) {
		result := &ExecutionResult{
			ToolCallID: "call_789",
			Success:    true,
			Result:     "test result",
		}

		msg := result.ToChatMessage()

		if msg.Role != llm.RoleTool {
			t.Errorf("expected role=%s, got %s", llm.RoleTool, msg.Role)
		}

		if msg.ToolCallID != "call_789" {
			t.Errorf("expected tool_call_id='call_789', got '%s'", msg.ToolCallID)
		}

		if msg.Content == "" {
			t.Error("expected non-empty content")
		}
	})
}

func TestPlaceholderToolRegistry(t *testing.T) {
	registry := NewPlaceholderToolRegistry()

	// Register a mock tool
	mockTool := NewMockTool("test_tool", "A test tool", nil)
	registry.Register(mockTool)

	// Get the tool
	tool := registry.Get("test_tool")
	if tool == nil {
		t.Fatal("expected to find registered tool")
	}

	if tool.Name() != "test_tool" {
		t.Errorf("expected name='test_tool', got '%s'", tool.Name())
	}

	// Get non-existent tool
	missing := registry.Get("missing_tool")
	if missing != nil {
		t.Error("expected nil for missing tool")
	}

	// List tools
	tools := registry.List()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

func TestMockTool(t *testing.T) {
	// Test with default behavior
	tool := NewMockTool("default_tool", "Default behavior", nil)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}

	if resultMap["mock"] != true {
		t.Error("expected mock=true")
	}

	// Test with custom behavior
	customTool := NewMockTool("custom_tool", "Custom behavior", func(ctx context.Context, args map[string]any) (any, error) {
		return args["input"], nil
	})

	result, err = customTool.Execute(context.Background(), map[string]any{"input": "test"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result != "test" {
		t.Errorf("expected 'test', got '%v'", result)
	}
}

func TestExecutorNoRegistry(t *testing.T) {
	executor := NewExecutor(nil, nil)

	toolCall := llm.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "any_tool",
			Arguments: "{}",
		},
	}

	result := executor.Execute(context.Background(), toolCall)

	if result.Success {
		t.Error("expected failure when no registry")
	}

	if result.Error != "tool registry not configured" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestExecutorUnknownTool(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	executor := NewExecutor(registry, nil)

	toolCall := llm.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "unknown_tool",
			Arguments: "{}",
		},
	}

	result := executor.Execute(context.Background(), toolCall)

	if result.Success {
		t.Error("expected failure for unknown tool")
	}

	if result.Error != "unknown tool: unknown_tool" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestExecutorInvalidArguments(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("test_tool", "Test", nil))
	executor := NewExecutor(registry, nil)

	toolCall := llm.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "test_tool",
			Arguments: "not valid json",
		},
	}

	result := executor.Execute(context.Background(), toolCall)

	if result.Success {
		t.Error("expected failure for invalid JSON")
	}

	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestExecutorSuccess(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "File read", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{
			"content": "test content",
		}, nil
	}))

	// Create security checker with default config
	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker)

	toolCall := llm.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: `{"path": "/tmp/test.txt"}`,
		},
	}

	result := executor.Execute(context.Background(), toolCall)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}

	if resultMap["content"] != "test content" {
		t.Errorf("expected content='test content', got '%v'", resultMap["content"])
	}
}

func TestExecutorToolError(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "Fails", func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("intentional failure")
	}))

	// Create security checker with default config
	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker)

	toolCall := llm.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: `{"path": "/tmp/test.txt"}`,
		},
	}

	result := executor.Execute(context.Background(), toolCall)

	if result.Success {
		t.Error("expected failure")
	}

	if result.Error != "tool execution failed: intentional failure" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestExecutorWithSecurity(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "Read file", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "file content"}, nil
	}))

	// Create security checker that blocks certain paths
	// Note: The security checker uses prefix matching for directory paths,
	// so /home allows anything under /home/
	securityCfg := security.Config{
		BlockedPaths:   []string{"/etc"},
		AllowedPaths:   []string{"/home"},
		BlockFinancial: true,
	}
	checker := security.NewPermissionChecker(securityCfg)

	executor := NewExecutor(registry, checker)

	t.Run("allowed path", func(t *testing.T) {
		toolCall := llm.ToolCall{
			ID:   "call_allowed",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "file_read",
				Arguments: `{"path": "/home/user/file.txt"}`,
			},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Errorf("expected success, got error: %s", result.Error)
		}
	})

	t.Run("blocked path", func(t *testing.T) {
		toolCall := llm.ToolCall{
			ID:   "call_blocked",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "file_read",
				Arguments: `{"path": "/etc/passwd"}`,
			},
		}

		result := executor.Execute(context.Background(), toolCall)
		if result.Success {
			t.Error("expected failure for blocked path")
		}
	})
}

func TestExecuteAll(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "File read", func(ctx context.Context, args map[string]any) (any, error) {
		return "result1", nil
	}))
	registry.Register(NewMockTool("memory_read", "Memory read", func(ctx context.Context, args map[string]any) (any, error) {
		return "result2", nil
	}))

	// Create security checker with default config
	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(2))

	toolCalls := []llm.ToolCall{
		{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "file_read", Arguments: "{}"},
		},
		{
			ID:       "call_2",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "memory_read", Arguments: "{}"},
		},
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for i, result := range results {
		if !result.Success {
			t.Errorf("result %d failed: %s", i, result.Error)
		}
	}
}

func TestExecuteAllContextCancellation(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("slow_tool", "Slow", func(ctx context.Context, _ map[string]any) (any, error) {
		select {
		case <-time.After(5 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	executor := NewExecutor(registry, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	toolCalls := []llm.ToolCall{
		{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "slow_tool", Arguments: "{}"},
		},
	}

	results := executor.ExecuteAll(ctx, toolCalls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Result should indicate failure due to context cancellation
	if results[0].Success {
		t.Error("expected failure due to context cancellation")
	}
}

func TestExecuteSequential(t *testing.T) {
	executionOrder := make([]string, 0)
	mu := new(struct {
		order []string
	})
	mu.order = executionOrder

	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "File read", func(ctx context.Context, args map[string]any) (any, error) {
		mu.order = append(mu.order, "first")
		return "first", nil
	}))
	registry.Register(NewMockTool("memory_read", "Memory read", func(ctx context.Context, args map[string]any) (any, error) {
		mu.order = append(mu.order, "second")
		return "second", nil
	}))

	// Create security checker with default config
	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker)

	toolCalls := []llm.ToolCall{
		{
			ID:       "call_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "file_read", Arguments: "{}"},
		},
		{
			ID:       "call_2",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "memory_read", Arguments: "{}"},
		},
	}

	results := executor.ExecuteSequential(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, result := range results {
		if !result.Success {
			t.Errorf("unexpected failure: %s", result.Error)
		}
	}
}

func TestResultsToChatMessages(t *testing.T) {
	results := []*ExecutionResult{
		{ToolCallID: "call_1", Success: true, Result: "result1"},
		{ToolCallID: "call_2", Success: false, Error: "error2"},
	}

	messages := ResultsToChatMessages(results)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	for i, msg := range messages {
		if msg.Role != llm.RoleTool {
			t.Errorf("message %d: expected role=tool, got %s", i, msg.Role)
		}
		if msg.ToolCallID == "" {
			t.Errorf("message %d: expected tool_call_id", i)
		}
		if msg.Content == "" {
			t.Errorf("message %d: expected content", i)
		}
	}
}

func TestToolActionMap(t *testing.T) {
	// Verify the action map contains expected mappings
	expectedMappings := map[string]string{
		"shell":          "shell_execute",
		"file_read":      "file_read",
		"file_write":     "file_write",
		"file_delete":    "file_delete",
		"list_directory": "file_read",
		"web_search":     "network_request",
		"web_fetch":      "network_request",
	}

	for tool, expectedAction := range expectedMappings {
		action, ok := ToolActionMap[tool]
		if !ok {
			t.Errorf("missing mapping for tool: %s", tool)
			continue
		}
		if action != expectedAction {
			t.Errorf("tool %s: expected action=%s, got %s", tool, expectedAction, action)
		}
	}
}

func TestSummarizeArgs(t *testing.T) {
	t.Run("short args", func(t *testing.T) {
		args := map[string]any{"key": "value"}
		summary := summarizeArgs(args)
		if summary == "" {
			t.Error("expected non-empty summary")
		}
		if len(summary) > 200 {
			t.Error("summary should not exceed 200 chars")
		}
	})

	t.Run("long args", func(t *testing.T) {
		longValue := make([]byte, 1000)
		for i := range longValue {
			longValue[i] = 'x'
		}
		args := map[string]any{"key": string(longValue)}

		summary := summarizeArgs(args)
		if len(summary) > 203 { // 200 + "..."
			t.Errorf("summary too long: %d chars", len(summary))
		}
	})
}

func TestToCompressedJSON(t *testing.T) {
	t.Run("small result no compression", func(t *testing.T) {
		result := &ExecutionResult{
			ToolCallID: "call_123",
			Success:    true,
			Result:     "small result",
		}

		// With large token budget, should return full JSON
		compressed := result.ToCompressedJSON(10000)
		full := result.ToJSON()

		if compressed != full {
			t.Error("small result should not be compressed")
		}
	})

	t.Run("large string result compressed", func(t *testing.T) {
		// Create a large string result
		largeContent := make([]byte, 50000)
		for i := range largeContent {
			largeContent[i] = byte('a' + (i % 26))
		}

		result := &ExecutionResult{
			ToolCallID: "call_456",
			Success:    true,
			Result:     string(largeContent),
		}

		// With small token budget, should compress
		compressed := result.ToCompressedJSON(1000) // ~4000 chars
		full := result.ToJSON()

		if len(compressed) >= len(full) {
			t.Errorf("compressed (%d) should be smaller than full (%d)", len(compressed), len(full))
		}

		// Should still be valid JSON
		var parsed map[string]any
		if err := json.Unmarshal([]byte(compressed), &parsed); err != nil {
			t.Errorf("compressed result should be valid JSON: %v", err)
		}
	})

	t.Run("large map result compressed", func(t *testing.T) {
		// Create a result with large map values
		result := &ExecutionResult{
			ToolCallID: "call_789",
			Success:    true,
			Result: map[string]any{
				"short": "value",
				"long":  string(make([]byte, 20000)),
			},
		}

		compressed := result.ToCompressedJSON(500)
		full := result.ToJSON()

		if len(compressed) >= len(full) {
			t.Errorf("compressed (%d) should be smaller than full (%d)", len(compressed), len(full))
		}

		// Should still be valid JSON
		var parsed map[string]any
		if err := json.Unmarshal([]byte(compressed), &parsed); err != nil {
			t.Errorf("compressed result should be valid JSON: %v", err)
		}
	})
}

func TestTruncateWithMarker(t *testing.T) {
	// Test short string - no truncation
	short := "Hello, World!"
	result := truncateWithMarker(short, 100)
	if result != short {
		t.Errorf("short string should not be truncated")
	}

	// Test long string - should truncate with marker
	long := make([]byte, 1000)
	for i := range long {
		long[i] = byte('a' + (i % 26))
	}
	result = truncateWithMarker(string(long), 200)

	if len(result) > 250 { // Some overhead for marker
		t.Errorf("truncated result too long: %d", len(result))
	}

	if !containsSubstring(result, "truncated") {
		t.Error("truncated result should contain truncation marker")
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockStreamingTool is a mock tool that implements both Tool and StreamingTool.
type mockStreamingTool struct {
	name         string
	description  string
	result       any
	err          error
	updates      []tools.ProgressUpdate
}

func newMockStreamingTool(name string, result any, err error, updates []tools.ProgressUpdate) *mockStreamingTool {
	return &mockStreamingTool{
		name:        name,
		description: "streaming mock",
		result:      result,
		err:         err,
		updates:     updates,
	}
}

func (t *mockStreamingTool) Name() string        { return t.name }
func (t *mockStreamingTool) Description() string { return t.description }
func (t *mockStreamingTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (t *mockStreamingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.result, t.err
}
func (t *mockStreamingTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	for _, u := range t.updates {
		onUpdate(u)
	}
	return t.result, t.err
}

// mockTerminatingTool is a mock tool that implements both Tool and TerminatingTool.
type mockTerminatingTool struct {
	name        string
	description string
	result      any
	err         error
	terminate   bool
}

func newMockTerminatingTool(name string, result any, terminate bool) *mockTerminatingTool {
	return &mockTerminatingTool{
		name:        name,
		description: "terminating mock",
		result:      result,
		terminate:   terminate,
	}
}

func (t *mockTerminatingTool) Name() string        { return t.name }
func (t *mockTerminatingTool) Description() string { return t.description }
func (t *mockTerminatingTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (t *mockTerminatingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.result, t.err
}
func (t *mockTerminatingTool) TerminateHint(args map[string]any) bool {
	return t.terminate
}

func TestStreamingToolDetection(t *testing.T) {
	t.Run("streaming tool emits progress via bus", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		progressUpdates := []tools.ProgressUpdate{
			{Message: "starting...", Percent: 10},
			{Message: "halfway...", Percent: 50},
			{Message: "done", Percent: 100},
		}
		streamingMock := newMockStreamingTool("platform_status", "result", nil, progressUpdates)
		registry.Register(streamingMock)

		// Create a bus and subscribe to progress events
		testBus := bus.New(nil, nil)
		sub := testBus.Subscribe("test-executor", "tool.execution.progress")
		defer testBus.Unsubscribe(sub)

		// nil security uses fail-closed which allows platform_status
		executor := NewExecutor(registry, nil, WithExecutorBus(testBus))

		toolCall := llm.ToolCall{
			ID:       "call_stream_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if result.Result != "result" {
			t.Errorf("expected result='result', got %v", result.Result)
		}

		// Verify progress events were published
		received := 0
		for {
			select {
			case msg := <-sub.Channel:
				received++
				if msg.Topic != "tool.execution.progress" {
					t.Errorf("expected topic=tool.execution.progress, got %s", msg.Topic)
				}
				if received >= 3 {
					goto doneStream
				}
			default:
				goto doneStream
			}
		}
	doneStream:
		if received != 3 {
			t.Errorf("expected 3 progress events, got %d", received)
		}
	})

	t.Run("streaming tool falls back to Execute when no bus", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		progressUpdates := []tools.ProgressUpdate{
			{Message: "starting...", Percent: 10},
		}
		streamingMock := newMockStreamingTool("platform_status", "result", nil, progressUpdates)
		registry.Register(streamingMock)

		// nil security uses fail-closed which allows platform_status
		executor := NewExecutor(registry, nil) // No bus

		toolCall := llm.ToolCall{
			ID:       "call_stream_2",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if result.Result != "result" {
			t.Errorf("expected result='result', got %v", result.Result)
		}
	})
}

func TestTerminateHintPropagation(t *testing.T) {
	t.Run("terminating tool sets Terminate=true", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		registry.Register(newMockTerminatingTool("platform_status", "final answer", true))

		// nil security: fail-closed allows platform_status
		executor := NewExecutor(registry, nil)

		toolCall := llm.ToolCall{
			ID:       "call_term_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if !result.Terminate {
			t.Error("expected Terminate=true for terminating tool")
		}
	})

	t.Run("non-terminating tool sets Terminate=false", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		registry.Register(newMockTerminatingTool("platform_status", "intermediate result", false))

		executor := NewExecutor(registry, nil)

		toolCall := llm.ToolCall{
			ID:       "call_nonterm_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if result.Terminate {
			t.Error("expected Terminate=false for non-terminating tool")
		}
	})

	t.Run("ToolResult with Terminate=true propagates", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		registry.Register(NewMockTool("platform_status", "Test", func(ctx context.Context, args map[string]any) (any, error) {
			return &tools.ToolResult{
				Success:   true,
				Result:    "final answer",
				Terminate: true,
			}, nil
		}))

		executor := NewExecutor(registry, nil)

		toolCall := llm.ToolCall{
			ID:       "call_tr_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if !result.Terminate {
			t.Error("expected Terminate=true from ToolResult.Terminate")
		}
	})

	t.Run("regular tool has Terminate=false", func(t *testing.T) {
		registry := NewPlaceholderToolRegistry()
		registry.Register(NewMockTool("platform_status", "Test", func(ctx context.Context, args map[string]any) (any, error) {
			return "normal result", nil
		}))

		executor := NewExecutor(registry, nil)

		toolCall := llm.ToolCall{
			ID:       "call_reg_1",
			Type:     "function",
			Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
		}

		result := executor.Execute(context.Background(), toolCall)
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}
		if result.Terminate {
			t.Error("expected Terminate=false for regular tool")
		}
	})
}

func TestShouldTerminate(t *testing.T) {
	t.Run("empty results", func(t *testing.T) {
		if ShouldTerminate(nil) {
			t.Error("expected false for nil results")
		}
		if ShouldTerminate([]*ExecutionResult{}) {
			t.Error("expected false for empty results")
		}
	})

	t.Run("all terminate", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			{ToolCallID: "c2", Success: true, Terminate: true},
		}
		if !ShouldTerminate(results) {
			t.Error("expected true when all results have Terminate=true")
		}
	})

	t.Run("mixed results", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			{ToolCallID: "c2", Success: true, Terminate: false},
		}
		if ShouldTerminate(results) {
			t.Error("expected false when not all results have Terminate=true")
		}
	})

	t.Run("nil result in batch", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
			nil,
		}
		if ShouldTerminate(results) {
			t.Error("expected false when batch contains nil result")
		}
	})

	t.Run("single terminating result", func(t *testing.T) {
		results := []*ExecutionResult{
			{ToolCallID: "c1", Success: true, Terminate: true},
		}
		if !ShouldTerminate(results) {
			t.Error("expected true for single terminating result")
		}
	})
}

func TestCacheHitProgressEvent(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("memory_search", "Cached", func(ctx context.Context, args map[string]any) (any, error) {
		return "cached result", nil
	}))

	testBus := bus.New(nil, nil)
	sub := testBus.Subscribe("test-cache", "tool.execution.progress")
	defer testBus.Unsubscribe(sub)

	cache := NewResultCache(CacheConfig{
		MaxEntries:   100,
		DefaultTTL:   5 * time.Minute,
		EnabledTools: []string{"memory_search"},
	}, nil)
	// nil security: fail-closed allows memory_search
	executor := NewExecutor(registry, nil, WithExecutorCache(cache), WithExecutorBus(testBus))

	// First call populates cache
	toolCall := llm.ToolCall{
		ID:       "call_cache_1",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: "memory_search", Arguments: "{}"},
	}
	result1 := executor.Execute(context.Background(), toolCall)
	if !result1.Success {
		t.Fatalf("first call: expected success, got error: %s", result1.Error)
	}
	if result1.Cached {
		t.Error("first call should not be cached")
	}

	// Second call hits cache
	toolCall2 := llm.ToolCall{
		ID:       "call_cache_2",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: "memory_search", Arguments: "{}"},
	}
	result2 := executor.Execute(context.Background(), toolCall2)
	if !result2.Success {
		t.Fatalf("second call: expected success, got error: %s", result2.Error)
	}
	if !result2.Cached {
		t.Error("second call should be cached")
	}

	// Verify cache-hit progress event was published
	select {
	case msg := <-sub.Channel:
		if msg.Topic != "tool.execution.progress" {
			t.Errorf("expected topic=tool.execution.progress, got %s", msg.Topic)
		}
	default:
		t.Error("expected cache-hit progress event")
	}
}

func TestWithExecutorBusNilGuard(t *testing.T) {
	testBus := bus.New(nil, nil)
	executor := NewExecutor(nil, nil, WithExecutorBus(testBus))
	if executor.bus != testBus {
		t.Error("expected bus to be set")
	}

	// Nil bus should not panic
	executor2 := NewExecutor(nil, nil, WithExecutorBus(nil))
	if executor2.bus != nil {
		t.Error("expected nil bus")
	}
}
