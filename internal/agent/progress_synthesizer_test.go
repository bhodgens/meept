package agent

import (
	"strings"
	"testing"
	"time"
)

func TestSynthesizeToolEndQuiet(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventToolExecutionEnd,
		Timestamp: time.Now(),
		AgentID:   "coder",
		Data: ToolExecutionEndData{
			ToolCallID: "tc-1",
			ToolName:   "shell_execute",
			Success:    true,
			Result:     "build succeeded with 0 errors",
			Duration:   2 * time.Second,
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for ToolExecutionEnd")
	}
	if got.Message == "" {
		t.Error("expected non-empty message")
	}
	if got.Tier != VerbosityNormal {
		t.Errorf("expected tier %v, got %v", VerbosityNormal, got.Tier)
	}
	if got.SourceEvent != AgentEventToolExecutionEnd {
		t.Errorf("expected source event %v, got %v", AgentEventToolExecutionEnd, got.SourceEvent)
	}
	if !strings.Contains(got.Message, "coder") {
		t.Errorf("expected message to contain agent id 'coder', got %q", got.Message)
	}
	if !strings.Contains(got.Message, "shell_execute") {
		t.Errorf("expected message to contain tool name 'shell_execute', got %q", got.Message)
	}
}

func TestSynthesizeAgentEndQuiet(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventAgentEnd,
		Timestamp: time.Now(),
		AgentID:   "chat",
		Data: AgentEndData{
			AgentID:  "chat",
			Reason:   "completed",
			Duration: 5 * time.Second,
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for AgentEnd")
	}
	if got.Tier != VerbosityQuiet {
		t.Errorf("expected tier %v, got %v", VerbosityQuiet, got.Tier)
	}
	if !strings.Contains(got.Message, "chat") {
		t.Errorf("expected message to contain 'chat', got %q", got.Message)
	}
	if !strings.Contains(got.Message, "completed") {
		t.Errorf("expected message to contain 'completed', got %q", got.Message)
	}
}

func TestSynthesizeToolStartVerbose(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventToolExecutionStart,
		Timestamp: time.Now(),
		AgentID:   "coder",
		Data: ToolExecutionStartData{
			ToolCallID: "tc-2",
			ToolName:   "file_write",
			Arguments:  `{"path": "/tmp/test.go"}`,
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for ToolExecutionStart")
	}
	if got.Tier != VerbosityVerbose {
		t.Errorf("expected tier %v, got %v", VerbosityVerbose, got.Tier)
	}
	if !strings.Contains(got.Message, "coder") {
		t.Errorf("expected message to contain 'coder', got %q", got.Message)
	}
	if !strings.Contains(got.Message, "file_write") {
		t.Errorf("expected message to contain 'file_write', got %q", got.Message)
	}
}

func TestSynthesizeTurnEndVerbose(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventTurnEnd,
		Timestamp: time.Now(),
		AgentID:   "coder",
		Data: TurnEndData{
			TurnNumber:     3,
			HadToolCalls:   true,
			ToolCallCount:  2,
			ResponseTokens: 150,
			StoppedBy:      "tool_use",
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for TurnEnd")
	}
	if got.Tier != VerbosityVerbose {
		t.Errorf("expected tier %v, got %v", VerbosityVerbose, got.Tier)
	}
	if !strings.Contains(got.Message, "turn 3") {
		t.Errorf("expected message to contain 'turn 3', got %q", got.Message)
	}
	if !strings.Contains(got.Message, "2 tool calls") {
		t.Errorf("expected message to contain '2 tool calls', got %q", got.Message)
	}
	if !strings.Contains(got.Message, "150 tokens") {
		t.Errorf("expected message to contain '150 tokens', got %q", got.Message)
	}
}

func TestSynthesizeUnknownEventReturnsNil(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventSessionStart,
		Timestamp: time.Now(),
		AgentID:   "chat",
		Data:      SessionStartData{SessionID: "s-1"},
	}

	got := ps.Synthesize(event)
	if got != nil {
		t.Errorf("expected nil for unhandled event type, got %+v", got)
	}
}

func TestVerbosityLevelString(t *testing.T) {
	tests := []struct {
		level    VerbosityLevel
		expected string
	}{
		{VerbosityQuiet, "quiet"},
		{VerbosityNormal, "normal"},
		{VerbosityVerbose, "verbose"},
		{VerbosityLevel(99), "unknown"},
	}
	for _, tc := range tests {
		got := tc.level.String()
		if got != tc.expected {
			t.Errorf("VerbosityLevel(%d).String() = %q, want %q", tc.level, got, tc.expected)
		}
	}
}

func TestParseVerbosityLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected VerbosityLevel
	}{
		{"quiet", VerbosityQuiet},
		{"QUIET", VerbosityQuiet},
		{"normal", VerbosityNormal},
		{"Normal", VerbosityNormal},
		{"verbose", VerbosityVerbose},
		{"VERBOSE", VerbosityVerbose},
		{"unknown", VerbosityNormal},
		{"", VerbosityNormal},
	}
	for _, tc := range tests {
		got := ParseVerbosityLevel(tc.input)
		if got != tc.expected {
			t.Errorf("ParseVerbosityLevel(%q) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestSynthesizeToolEndWithError(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventToolExecutionEnd,
		Timestamp: time.Now(),
		AgentID:   "coder",
		Data: ToolExecutionEndData{
			ToolCallID: "tc-3",
			ToolName:   "shell_execute",
			Success:    false,
			Error:      "exit code 1",
			Duration:   500 * time.Millisecond,
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for ToolExecutionEnd with error")
	}
	if !strings.Contains(got.Message, "failed") || !strings.Contains(got.Message, "shell_execute") {
		t.Errorf("expected message to mention failure and tool name, got %q", got.Message)
	}
}

func TestSynthesizeToolEndBlocked(t *testing.T) {
	ps := NewProgressSynthesizer(nil, nil, nil)

	event := AgentEvent{
		Type:      AgentEventToolExecutionEnd,
		Timestamp: time.Now(),
		AgentID:   "coder",
		Data: ToolExecutionEndData{
			ToolCallID:  "tc-4",
			ToolName:    "shell_execute",
			Blocked:     true,
			BlockReason: "dangerous command",
			Duration:    10 * time.Millisecond,
		},
	}

	got := ps.Synthesize(event)
	if got == nil {
		t.Fatal("expected non-nil result for blocked tool execution")
	}
	if !strings.Contains(got.Message, "blocked") {
		t.Errorf("expected message to mention 'blocked', got %q", got.Message)
	}
}
