package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func TestBuildTrajectory_Truncates(t *testing.T) {
	conv := NewConversation()
	longContent := strings.Repeat("x", 2000)
	// Use AddMessage directly so we can set Name (AddToolResult doesn't expose it).
	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleTool,
		Content: longContent,
		Name:    "file_read",
	})

	traj := buildTrajectory(conv, "session-1", "coder", "user input", "success", 0)
	if len(traj.Steps) != 1 {
		t.Fatalf("got %d steps; want 1", len(traj.Steps))
	}
	if len(traj.Steps[0].ToolResult) > 500 {
		t.Errorf("tool result not truncated: %d chars", len(traj.Steps[0].ToolResult))
	}
	if traj.Steps[0].ToolName != "file_read" {
		t.Errorf("tool name = %q; want %q", traj.Steps[0].ToolName, "file_read")
	}
}

func TestBuildTrajectory_ErrorStep(t *testing.T) {
	conv := NewConversation()
	conv.AddMessage(llm.ChatMessage{
		Role:        llm.RoleTool,
		Content:     "panic: nil pointer dereference",
		Name:        "go_test",
		IsToolError: true,
	})

	traj := buildTrajectory(conv, "s1", "coder", "run tests", "partial", 0)
	if len(traj.Steps) != 1 {
		t.Fatalf("got %d steps; want 1", len(traj.Steps))
	}
	if traj.Steps[0].Kind != "error" {
		t.Errorf("Kind = %q; want %q", traj.Steps[0].Kind, "error")
	}
	if traj.Steps[0].ErrorCode == "" {
		t.Error("ErrorCode empty for error step")
	}
}

func TestBuildTrajectory_Caps50Steps(t *testing.T) {
	conv := NewConversation()
	for i := 0; i < 100; i++ {
		conv.AddAssistantMessage("x")
	}
	traj := buildTrajectory(conv, "s1", "coder", "in", "success", 0)
	if len(traj.Steps) > 50 {
		t.Errorf("steps = %d; want <= 50", len(traj.Steps))
	}
	if len(traj.Steps) != 50 {
		t.Errorf("steps = %d; want exactly 50", len(traj.Steps))
	}
}

func TestBuildTrajectory_NilConv(t *testing.T) {
	traj := buildTrajectory(nil, "s1", "coder", "in", "success", 0)
	if len(traj.Steps) != 0 {
		t.Errorf("nil conv yielded %d steps; want 0", len(traj.Steps))
	}
	if traj.UserInput != "in" {
		t.Errorf("UserInput = %q; want %q", traj.UserInput, "in")
	}
}

func TestBuildTrajectory_AssistantTruncation(t *testing.T) {
	conv := NewConversation()
	long := strings.Repeat("a", 2000)
	conv.AddAssistantMessage(long)

	traj := buildTrajectory(conv, "s1", "coder", "in", "success", 0)
	if len(traj.Steps) != 1 {
		t.Fatalf("got %d steps; want 1", len(traj.Steps))
	}
	if len(traj.Steps[0].Content) > 1000 {
		t.Errorf("assistant content not truncated: %d chars", len(traj.Steps[0].Content))
	}
}

func TestBuildTrajectory_SkipsSystemAndUser(t *testing.T) {
	conv := NewConversation()
	conv.AddSystemMessage("sys")
	conv.AddUserMessage("hi")
	conv.AddAssistantMessage("hello")

	traj := buildTrajectory(conv, "s1", "coder", "hi", "success", time.Second)
	// Only the assistant message should become a step.
	if len(traj.Steps) != 1 {
		t.Fatalf("got %d steps; want 1 (assistant only)", len(traj.Steps))
	}
	if traj.Steps[0].Kind != "assistant_message" {
		t.Errorf("Kind = %q; want %q", traj.Steps[0].Kind, "assistant_message")
	}
}

func TestTrajectory_JSON(t *testing.T) {
	traj := ReflectionTrajectory{
		UserInput: "fix bug",
		Steps:     []ReflectionTrajectoryStep{{Kind: "tool_call", ToolName: "file_edit"}},
	}
	j, err := traj.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(j), "fix bug") {
		t.Errorf("JSON missing input")
	}
}
