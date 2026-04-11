package lite

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommandHandler_Execute(t *testing.T) {
	// Test that Execute returns a command that produces a CommandResultMsg
	h := NewCommandHandler()
	cmd := &SlashCommand{Name: "help"}

	teaCmd := h.Execute(cmd)
	if teaCmd == nil {
		t.Fatal("Execute() returned nil")
	}

	// Execute the tea.Cmd and check result
	msg := teaCmd()
	resultMsg, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("Execute() returned wrong message type: %T", msg)
	}
	if resultMsg.Result == nil {
		t.Fatal("Execute() returned nil result")
	}
}

func TestCommandHandler_Help(t *testing.T) {
	h := NewCommandHandler()

	// Test general help
	result := h.executeSync(&SlashCommand{Name: "help"})
	if result.IsError {
		t.Errorf("help command returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "/help") {
		t.Error("help output should contain /help")
	}
	if !strings.Contains(result.Output, "/model") {
		t.Error("help output should contain /model")
	}

	// Test help for specific command
	result = h.executeSync(&SlashCommand{Name: "help", Args: []string{"model"}})
	if result.IsError {
		t.Errorf("help model returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "model") {
		t.Error("help model output should contain 'model'")
	}

	// Test help for unknown command
	result = h.executeSync(&SlashCommand{Name: "help", Args: []string{"unknown"}})
	if !result.IsError {
		t.Error("help for unknown command should return error")
	}
}

func TestCommandHandler_NewClear(t *testing.T) {
	h := NewCommandHandler()

	// Test /new
	result := h.executeSync(&SlashCommand{Name: "new"})
	if result.IsError {
		t.Errorf("new command returned error: %s", result.Output)
	}
	if !result.ClearConversation {
		t.Error("new command should set ClearConversation")
	}

	// Test /clear (alias)
	result = h.executeSync(&SlashCommand{Name: "clear"})
	if result.IsError {
		t.Errorf("clear command returned error: %s", result.Output)
	}
	if !result.ClearConversation {
		t.Error("clear command should set ClearConversation")
	}
}

func TestCommandHandler_Model(t *testing.T) {
	// Test without model getter/setter
	h := NewCommandHandler()

	result := h.executeSync(&SlashCommand{Name: "model"})
	if !result.IsError {
		t.Error("model without getter should return error")
	}

	result = h.executeSync(&SlashCommand{Name: "model", Args: []string{"gpt-4"}})
	if !result.IsError {
		t.Error("model set without setter should return error")
	}

	// Test with model getter/setter
	currentModel := "claude-3"
	h = NewCommandHandler(
		WithModelGetter(func() string { return currentModel }),
		WithModelSetter(func(m string) error {
			currentModel = m
			return nil
		}),
	)

	// Get model
	result = h.executeSync(&SlashCommand{Name: "model"})
	if result.IsError {
		t.Errorf("model get returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "claude-3") {
		t.Errorf("model get should show current model, got: %s", result.Output)
	}

	// Set model
	result = h.executeSync(&SlashCommand{Name: "model", Args: []string{"gpt-4"}})
	if result.IsError {
		t.Errorf("model set returned error: %s", result.Output)
	}
	if currentModel != "gpt-4" {
		t.Errorf("model should be set to gpt-4, got: %s", currentModel)
	}

	// Test model setter error
	h = NewCommandHandler(
		WithModelSetter(func(m string) error {
			return errors.New("invalid model")
		}),
	)
	result = h.executeSync(&SlashCommand{Name: "model", Args: []string{"bad-model"}})
	if !result.IsError {
		t.Error("model set with error should return error")
	}
}

func TestCommandHandler_RetryUndo(t *testing.T) {
	h := NewCommandHandler()

	// Test /retry
	result := h.executeSync(&SlashCommand{Name: "retry"})
	if result.IsError {
		t.Errorf("retry command returned error: %s", result.Output)
	}
	if !result.RetryLast {
		t.Error("retry command should set RetryLast")
	}

	// Test /undo
	result = h.executeSync(&SlashCommand{Name: "undo"})
	if result.IsError {
		t.Errorf("undo command returned error: %s", result.Output)
	}
	if !result.UndoLast {
		t.Error("undo command should set UndoLast")
	}
}

func TestCommandHandler_Usage(t *testing.T) {
	// Test without getter
	h := NewCommandHandler()
	result := h.executeSync(&SlashCommand{Name: "usage"})
	if !result.IsError {
		t.Error("usage without getter should return error")
	}

	// Test with nil stats
	h = NewCommandHandler(
		WithUsageGetter(func() *UsageStats { return nil }),
	)
	result = h.executeSync(&SlashCommand{Name: "usage"})
	if result.IsError {
		t.Errorf("usage with nil stats returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "no usage data") {
		t.Errorf("usage with nil stats should show no data message, got: %s", result.Output)
	}

	// Test with actual stats
	h = NewCommandHandler(
		WithUsageGetter(func() *UsageStats {
			return &UsageStats{
				TotalTokens:      1000,
				PromptTokens:     600,
				CompletionTokens: 400,
				TotalCost:        0.05,
				SessionTokens:    500,
				SessionCost:      0.025,
			}
		}),
	)
	result = h.executeSync(&SlashCommand{Name: "usage"})
	if result.IsError {
		t.Errorf("usage returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "1000") {
		t.Error("usage output should contain total tokens")
	}
	if !strings.Contains(result.Output, "session") {
		t.Error("usage output should contain session info")
	}
}

func TestCommandHandler_Session(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "s1", Name: "main", IsCurrent: true, MessageCount: 10},
		{ID: "s2", Name: "test", IsCurrent: false, MessageCount: 5},
	}
	switchedTo := ""

	h := NewCommandHandler(
		WithSessionLister(func() ([]SessionInfo, error) {
			return sessions, nil
		}),
		WithSessionSwitcher(func(id string) error {
			switchedTo = id
			return nil
		}),
	)

	// Test list sessions
	result := h.executeSync(&SlashCommand{Name: "session"})
	if result.IsError {
		t.Errorf("session list returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "main") {
		t.Error("session list should contain main")
	}
	if !strings.Contains(result.Output, "test") {
		t.Error("session list should contain test")
	}
	if !strings.Contains(result.Output, "*") {
		t.Error("session list should mark current session")
	}

	// Test switch by name
	result = h.executeSync(&SlashCommand{Name: "session", Args: []string{"test"}})
	if result.IsError {
		t.Errorf("session switch returned error: %s", result.Output)
	}
	if switchedTo != "s2" {
		t.Errorf("session should switch to s2, got: %s", switchedTo)
	}
	if result.SwitchSession != "s2" {
		t.Error("session switch should set SwitchSession")
	}

	// Test switch by ID
	switchedTo = ""
	result = h.executeSync(&SlashCommand{Name: "session", Args: []string{"s1"}})
	if result.IsError {
		t.Errorf("session switch by ID returned error: %s", result.Output)
	}
	if switchedTo != "s1" {
		t.Errorf("session should switch to s1, got: %s", switchedTo)
	}

	// Test session not found
	result = h.executeSync(&SlashCommand{Name: "session", Args: []string{"nonexistent"}})
	if !result.IsError {
		t.Error("session switch to nonexistent should return error")
	}
}

func TestCommandHandler_Task(t *testing.T) {
	tasks := []TaskInfo{
		{ID: "t1", Name: "analyze code", Status: "running", Progress: 0.5},
		{ID: "t2", Name: "generate docs", Status: "pending", Progress: 0},
	}

	h := NewCommandHandler(
		WithTaskLister(func() ([]TaskInfo, error) {
			return tasks, nil
		}),
		WithTaskGetter(func(id string) (*TaskInfo, error) {
			for _, t := range tasks {
				if t.ID == id {
					return &t, nil
				}
			}
			return nil, errors.New("task not found")
		}),
	)

	// Test list tasks
	result := h.executeSync(&SlashCommand{Name: "task"})
	if result.IsError {
		t.Errorf("task list returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "analyze code") {
		t.Error("task list should contain analyze code")
	}
	if !strings.Contains(result.Output, "50%") {
		t.Error("task list should show progress percentage")
	}

	// Test get specific task
	result = h.executeSync(&SlashCommand{Name: "task", Args: []string{"t1"}})
	if result.IsError {
		t.Errorf("task get returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "analyze code") {
		t.Error("task get should contain task name")
	}
	if !strings.Contains(result.Output, "running") {
		t.Error("task get should contain status")
	}

	// Test task not found
	result = h.executeSync(&SlashCommand{Name: "task", Args: []string{"t999"}})
	if !result.IsError {
		t.Error("task get for nonexistent should return error")
	}
}

func TestCommandHandler_SkillInvocation(t *testing.T) {
	h := NewCommandHandler()

	// Non-builtin command should be treated as skill
	result := h.executeSync(&SlashCommand{Name: "code-review", Args: []string{"file.go"}})
	if result.IsError {
		t.Errorf("skill invocation returned error: %s", result.Output)
	}
	if result.SkillInvoke != "code-review" {
		t.Errorf("skill invocation should set SkillInvoke, got: %s", result.SkillInvoke)
	}
	if !reflect.DeepEqual(result.SkillArgs, []string{"file.go"}) {
		t.Errorf("skill invocation should set SkillArgs, got: %v", result.SkillArgs)
	}
}

func TestCommandHandler_GetCompletions(t *testing.T) {
	h := NewCommandHandler(
		WithSkillLister(func() []string {
			return []string{"code-review", "summarize", "help-me"}
		}),
	)

	tests := []struct {
		name     string
		prefix   string
		expected []string
	}{
		{
			name:     "no slash",
			prefix:   "hel",
			expected: nil,
		},
		{
			name:     "empty after slash",
			prefix:   "/",
			expected: []string{"/clear", "/code-review", "/help", "/help-me", "/model", "/new", "/retry", "/session", "/summarize", "/task", "/undo", "/usage"},
		},
		{
			name:     "h prefix",
			prefix:   "/h",
			expected: []string{"/help", "/help-me"},
		},
		{
			name:     "help exact",
			prefix:   "/help",
			expected: []string{"/help", "/help-me"},
		},
		{
			name:     "s prefix",
			prefix:   "/s",
			expected: []string{"/session", "/summarize"},
		},
		{
			name:     "no match",
			prefix:   "/xyz",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.GetCompletions(tt.prefix)

			// Handle nil vs empty slice
			if tt.expected == nil {
				if result != nil {
					t.Errorf("GetCompletions(%q) = %v, want nil", tt.prefix, result)
				}
				return
			}

			// Sort both for comparison since order doesn't matter
			sortStrings(result)
			sortStrings(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetCompletions(%q) = %v, want %v", tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestCommandHandler_HelpForSkill(t *testing.T) {
	h := NewCommandHandler(
		WithSkillLister(func() []string {
			return []string{"code-review", "summarize"}
		}),
	)

	// Help for known skill
	result := h.executeSync(&SlashCommand{Name: "help", Args: []string{"code-review"}})
	if result.IsError {
		t.Errorf("help for skill returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "code-review") {
		t.Error("help for skill should mention skill name")
	}
	if !strings.Contains(result.Output, "skill") {
		t.Error("help for skill should mention it's a skill")
	}

	// Help for unknown (neither builtin nor skill)
	result = h.executeSync(&SlashCommand{Name: "help", Args: []string{"unknown-thing"}})
	if !result.IsError {
		t.Error("help for unknown should return error")
	}
}

func TestCommandHandler_NilCommand(t *testing.T) {
	h := NewCommandHandler()
	result := h.executeSync(nil)
	if !result.IsError {
		t.Error("executeSync(nil) should return error")
	}
}

func TestCommandHandler_EmptySessions(t *testing.T) {
	h := NewCommandHandler(
		WithSessionLister(func() ([]SessionInfo, error) {
			return []SessionInfo{}, nil
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "session"})
	if result.IsError {
		t.Errorf("session list returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "no sessions") {
		t.Errorf("empty sessions should show no sessions message, got: %s", result.Output)
	}
}

func TestCommandHandler_EmptyTasks(t *testing.T) {
	h := NewCommandHandler(
		WithTaskLister(func() ([]TaskInfo, error) {
			return []TaskInfo{}, nil
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "task"})
	if result.IsError {
		t.Errorf("task list returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "no tasks") {
		t.Errorf("empty tasks should show no tasks message, got: %s", result.Output)
	}
}

func TestCommandHandler_SessionWithDescription(t *testing.T) {
	h := NewCommandHandler(
		WithSessionLister(func() ([]SessionInfo, error) {
			return []SessionInfo{
				{ID: "s1", Name: "main", Description: "my main session", IsCurrent: true, MessageCount: 10},
			}, nil
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "session"})
	if result.IsError {
		t.Errorf("session list returned error: %s", result.Output)
	}
	// Should show description instead of name when available
	if !strings.Contains(result.Output, "my main session") {
		t.Errorf("session list should show description, got: %s", result.Output)
	}
}

func TestCommandHandler_TaskWithDescription(t *testing.T) {
	h := NewCommandHandler(
		WithTaskGetter(func(id string) (*TaskInfo, error) {
			return &TaskInfo{
				ID:          "t1",
				Name:        "analyze",
				Status:      "running",
				Description: "analyzing the codebase for issues",
			}, nil
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "task", Args: []string{"t1"}})
	if result.IsError {
		t.Errorf("task get returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "analyzing the codebase") {
		t.Errorf("task detail should show description, got: %s", result.Output)
	}
}

func TestCommandHandler_SessionListError(t *testing.T) {
	h := NewCommandHandler(
		WithSessionLister(func() ([]SessionInfo, error) {
			return nil, errors.New("database error")
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "session"})
	if !result.IsError {
		t.Error("session list with error should return error")
	}
	if !strings.Contains(result.Output, "database error") {
		t.Errorf("session list error should contain original error, got: %s", result.Output)
	}
}

func TestCommandHandler_TaskListError(t *testing.T) {
	h := NewCommandHandler(
		WithTaskLister(func() ([]TaskInfo, error) {
			return nil, errors.New("network error")
		}),
	)

	result := h.executeSync(&SlashCommand{Name: "task"})
	if !result.IsError {
		t.Error("task list with error should return error")
	}
	if !strings.Contains(result.Output, "network error") {
		t.Errorf("task list error should contain original error, got: %s", result.Output)
	}
}

func TestCommandHandler_EmptyModel(t *testing.T) {
	h := NewCommandHandler(
		WithModelGetter(func() string { return "" }),
	)

	result := h.executeSync(&SlashCommand{Name: "model"})
	if result.IsError {
		t.Errorf("model get returned error: %s", result.Output)
	}
	if !strings.Contains(result.Output, "(default)") {
		t.Errorf("empty model should show default, got: %s", result.Output)
	}
}
