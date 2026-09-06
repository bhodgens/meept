package llm

import (
	"context"
	"strings"
	"testing"
)

func TestTaskSummarizer_SerializeMessages(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	messages := []ChatMessage{
		{Role: RoleUser, Content: "Fix the bug"},
		{Role: RoleAssistant, Content: "I'll help with that"},
		{Role: RoleTool, Content: "bug fixed"},
	}

	serialized := summarizer.serializeMessages(messages)

	if !strings.Contains(serialized, "[User]: Fix the bug") {
		t.Errorf("expected user message, got %q", serialized)
	}
	if !strings.Contains(serialized, "[Assistant]: I'll help with that") {
		t.Errorf("expected assistant message, got %q", serialized)
	}
	if !strings.Contains(serialized, "[Tool Result]: bug fixed") {
		t.Errorf("expected tool result, got %q", serialized)
	}
}

func TestTaskSummarizer_SerializeMessages_Empty(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)
	serialized := summarizer.serializeMessages(nil)
	if serialized != "" {
		t.Errorf("expected empty string, got %q", serialized)
	}
}

func TestTaskSummarizer_SerializeMessages_SkipsCompacted(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	messages := []ChatMessage{
		{Role: RoleSystem, Content: "[Compacted Context]"},
		{Role: RoleUser, Content: "Hello"},
	}

	serialized := summarizer.serializeMessages(messages)
	if strings.Contains(serialized, "[Compacted Context]") {
		t.Error("expected compacted context to be skipped")
	}
}

func TestTaskSummarizer_SerializeMessages_ToolCalls(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	messages := []ChatMessage{
		{
			Role:    RoleAssistant,
			Content: "Let me call a tool",
			ToolCalls: []ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: ToolCallFunction{
						Name:      "test_tool",
						Arguments: `{"key": "value"}`,
					},
				},
			},
		},
	}

	serialized := summarizer.serializeMessages(messages)
	if !strings.Contains(serialized, "[Tool Call]: test_tool(") {
		t.Errorf("expected tool call, got %q", serialized)
	}
}

func TestTaskSummarizer_SerializeMessages_TruncatesLongToolResults(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	longResult := strings.Repeat("x", 600)
	messages := []ChatMessage{
		{Role: RoleTool, Content: longResult},
	}

	serialized := summarizer.serializeMessages(messages)
	if !strings.Contains(serialized, "...") {
		t.Error("expected long tool result to be truncated")
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"normal", "Sidebar display - background image fix", 50, "Sidebar display - background image fix"},
		{"with quotes", `"Fix the bug"`, 50, "Fix the bug"},
		{"with prefix", "Title: Fix the bug", 50, "Fix the bug"},
		{"multiline", "Fix the bug\nin the sidebar", 50, "Fix the bug"},
		{"truncate", "This is a very long title that needs truncation", 20, "This is a very lo…"},
		{"empty", "", 50, ""},
		{"short max", "Long title", 5, "Lo…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("extractTitle(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestParseHandoffSections(t *testing.T) {
	input := `## Goal
Fix the sidebar

## Current State
Working on it

## Next Steps
Finish the fix`

	sections := parseHandoffSections(input)

	if !strings.Contains(sections["Goal"], "Fix the sidebar") {
		t.Errorf("Goal section mismatch: %q", sections["Goal"])
	}
	if !strings.Contains(sections["Current State"], "Working on it") {
		t.Errorf("Current State mismatch: %q", sections["Current State"])
	}
	if !strings.Contains(sections["Next Steps"], "Finish the fix") {
		t.Errorf("Next Steps mismatch: %q", sections["Next Steps"])
	}
}

func TestParseHandoffSections_NoSections(t *testing.T) {
	input := `No sections here
Just plain text`

	sections := parseHandoffSections(input)
	if len(sections) != 0 {
		t.Errorf("expected no sections, got %d", len(sections))
	}
}

func TestTaskTitleResult_Truncated(t *testing.T) {
	// Test that Truncated flag is set correctly
	result := TaskTitleResult{
		Title:      strings.Repeat("a", 50),
		TokensUsed: 100,
		Truncated:  true,
	}
	if !result.Truncated {
		t.Error("expected Truncated to be true")
	}
}

func TestSessionSummaryResult(t *testing.T) {
	result := SessionSummaryResult{
		Summary:    "Test summary",
		TokensUsed: 50,
	}
	if result.Summary != "Test summary" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Test summary")
	}
}

func TestHandoffResult_Sections(t *testing.T) {
	result := HandoffResult{
		Summary:    "Handoff summary",
		TokensUsed: 75,
		Sections: map[string]string{
			"Goal": "Test goal",
		},
	}
	if result.Sections["Goal"] != "Test goal" {
		t.Errorf("Sections[Goal] = %q, want %q", result.Sections["Goal"], "Test goal")
	}
}

func TestTaskSummarizer_Constructor(t *testing.T) {
	// Test with nil tokenizer (should use HeuristicTokenizer)
	summarizer := NewTaskSummarizer(nil, nil)
	if summarizer == nil {
		t.Fatal("expected non-nil summarizer")
	}
	if summarizer.tokenizer == nil {
		t.Error("expected tokenizer to be set")
	}

	// Test with explicit tokenizer
	mockTokenizer := &HeuristicTokenizer{}
	summarizer2 := NewTaskSummarizer(nil, mockTokenizer)
	if summarizer2.tokenizer != mockTokenizer {
		t.Error("expected explicit tokenizer to be used")
	}
}

func TestSummarizeTaskTitle_ZeroMaxLen(t *testing.T) {
	// When maxLen is 0 or negative, default to 50
	summarizer := NewTaskSummarizer(nil, nil)

	// This test just verifies the method handles edge cases without crashing
	messages := []ChatMessage{
		{Role: RoleUser, Content: "Test"},
	}

	// Should not panic with zero maxLen
	result, err := summarizer.SummarizeTaskTitle(context.Background(), messages, 0)
	if err == nil {
		// If no error, verify default maxLen was used
		if result.Truncated && len(result.Title) > 53 { // 50 + 3 for "…"
			t.Errorf("title too long for default maxLen: %q", result.Title)
		}
	}
}

func TestSummarizeTaskTitle_EmptyMessages(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	result, err := summarizer.SummarizeTaskTitle(context.Background(), nil, 50)
	if err != nil {
		t.Fatalf("SummarizeTaskTitle failed: %v", err)
	}
	if result.Title != "Unknown task" {
		t.Errorf("Title = %q, want %q", result.Title, "Unknown task")
	}
}

func TestSummarizeSession_EmptyMessages(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	result, err := summarizer.SummarizeSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("SummarizeSession failed: %v", err)
	}
	if result.Summary != "Empty session" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Empty session")
	}
}

func TestSummarizeHandoff_EmptyMessages(t *testing.T) {
	summarizer := NewTaskSummarizer(nil, nil)

	result, err := summarizer.SummarizeHandoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("SummarizeHandoff failed: %v", err)
	}
	if result.Summary != "No context to hand off" {
		t.Errorf("Summary = %q, want %q", result.Summary, "No context to hand off")
	}
}

// recordingChatter captures the options passed to Chat so tests can assert
// which ChatOptions the summarizer applies.
type recordingChatter struct {
	resp     *Response
	err      error
	captured []*chatOptions // per-call resolved option state
}

func (r *recordingChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	resolved := &chatOptions{}
	for _, opt := range opts {
		opt(resolved)
	}
	r.captured = append(r.captured, resolved)
	return r.resp, r.err
}

func (r *recordingChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return r.Chat(ctx, messages, opts...)
}

func (r *recordingChatter) Config() *ModelConfig { return nil }

// assertDisableThinking verifies the captured option state carries an
// explicit thinking-disable (ReasoningConfig.Enabled = false).
func assertDisableThinking(t *testing.T, r *recordingChatter, wantCalls int) {
	t.Helper()
	if len(r.captured) != wantCalls {
		t.Fatalf("chatter called %d times, want %d", len(r.captured), wantCalls)
	}
	for i, resolved := range r.captured {
		if resolved.reasoning == nil {
			t.Errorf("call %d: no reasoning option passed (DisableThinking missing)", i)
			continue
		}
		if resolved.reasoning.Enabled == nil {
			t.Errorf("call %d: reasoning.Enabled is nil, want explicit false", i)
			continue
		}
		if *resolved.reasoning.Enabled {
			t.Errorf("call %d: reasoning.Enabled = true, want false", i)
		}
	}
}

func TestSummarizeTaskTitle_DisablesThinking(t *testing.T) {
	chatter := &recordingChatter{resp: &Response{Content: "Sidebar display - background image fix"}}
	summarizer := NewTaskSummarizer(chatter, nil)

	result, err := summarizer.SummarizeTaskTitle(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "fix the sidebar background"}}, 50)
	if err != nil {
		t.Fatalf("SummarizeTaskTitle failed: %v", err)
	}
	assertDisableThinking(t, chatter, 1)
	if result.Title != "Sidebar display - background image fix" {
		t.Errorf("Title = %q, want %q", result.Title, "Sidebar display - background image fix")
	}
}

func TestSummarizeSession_DisablesThinking(t *testing.T) {
	chatter := &recordingChatter{resp: &Response{Content: "Worked on the sidebar."}}
	summarizer := NewTaskSummarizer(chatter, nil)

	if _, err := summarizer.SummarizeSession(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "session work"}}); err != nil {
		t.Fatalf("SummarizeSession failed: %v", err)
	}
	assertDisableThinking(t, chatter, 1)
}

func TestSummarizeHandoff_DisablesThinking(t *testing.T) {
	chatter := &recordingChatter{resp: &Response{Content: "## Goal\nfix it"}}
	summarizer := NewTaskSummarizer(chatter, nil)

	if _, err := summarizer.SummarizeHandoff(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "handoff work"}}); err != nil {
		t.Fatalf("SummarizeHandoff failed: %v", err)
	}
	assertDisableThinking(t, chatter, 1)
}

func TestStripThinking(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no block", "Just a plain summary", "Just a plain summary"},
		{
			"inline block",
			"<think>Let me reason about this</think>Sidebar display - image fix",
			"Sidebar display - image fix",
		},
		{
			"trailing block",
			"Sidebar display - image fix<think>reasoning here</think>",
			"Sidebar display - image fix",
		},
		{
			"multiple blocks",
			"<think>first</think>Start<think>second</think> middle <think>third</think>End",
			"Start middle End",
		},
		{
			"multiline block",
			"<think>line one\nline two\nline three</think>\nThe actual summary",
			"The actual summary",
		},
		{
			"case insensitive tags",
			"<THINK>uppercase reasoning</THINK>The summary",
			"The summary",
		},
		{
			"leading unclosed block",
			"<think>I started reasoning but got cut off",
			"",
		},
		{
			"leading whitespace before unclosed block",
			"\n\n  <think>cut off reasoning",
			"",
		},
		{
			"leading reasoning_content fragment",
			"reasoning_content: \"I should summarize this\"\nThe actual summary",
			"The actual summary",
		},
		{
			"quoted reasoning_content key",
			"\"reasoning_content\": \"thinking...\"\nThe actual summary",
			"The actual summary",
		},
		{
			"reasoning_content fragment alone",
			`reasoning_content: "just reasoning"`,
			"",
		},
		{
			"empty input",
			"",
			"",
		},
		{
			"only a closed block",
			"<think>nothing else</think>",
			"",
		},
		{
			"whitespace trimmed",
			"  \n Summary text \n ",
			"Summary text",
		},
		{
			"mid-string think tag without close is preserved",
			"The summary mentions <think> as a literal string",
			"The summary mentions <think> as a literal string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinking(tt.input)
			if got != tt.want {
				t.Errorf("stripThinking(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSummarizeSession_StripsThinkLeakage(t *testing.T) {
	// Defense in depth: even when the server-side disable fails (e.g. the
	// capability gate suppresses enable_thinking), inline <think> output
	// must never reach the summary.
	chatter := &recordingChatter{resp: &Response{
		Content:   "<think>The user wants a session summary.</think>Built the summarizer. Fixed tests.",
		Reasoning: "separate channel reasoning",
	}}
	summarizer := NewTaskSummarizer(chatter, nil)

	result, err := summarizer.SummarizeSession(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "session work"}})
	if err != nil {
		t.Fatalf("SummarizeSession failed: %v", err)
	}
	if result.Summary != "Built the summarizer. Fixed tests." {
		t.Errorf("Summary = %q, want stripped summary", result.Summary)
	}
}

func TestSummarizeHandoff_StripsThinkLeakage(t *testing.T) {
	chatter := &recordingChatter{resp: &Response{
		Content: "<think>need handoff format</think>\n## Goal\nShip the fix",
	}}
	summarizer := NewTaskSummarizer(chatter, nil)

	result, err := summarizer.SummarizeHandoff(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "handoff work"}})
	if err != nil {
		t.Fatalf("SummarizeHandoff failed: %v", err)
	}
	if result.Summary != "## Goal\nShip the fix" {
		t.Errorf("Summary = %q, want stripped summary", result.Summary)
	}
	if got := result.Sections["Goal"]; got != "Ship the fix" {
		t.Errorf("Sections[Goal] = %q, want %q", got, "Ship the fix")
	}
}
