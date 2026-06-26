package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

func TestBuildConversationExcerpt_Nil(t *testing.T) {
	if got := buildConversationExcerpt(nil); got != "" {
		t.Errorf("nil messages should yield empty excerpt; got %q", got)
	}
	if got := buildConversationExcerpt([]llm.ChatMessage{}); got != "" {
		t.Errorf("empty messages should yield empty excerpt; got %q", got)
	}
}

func TestBuildConversationExcerpt_AssistantAndTool(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleAssistant, Content: "I will edit the file."},
		{Role: llm.RoleTool, Name: "shell_edit", Content: "edited successfully"},
		{Role: llm.RoleUser, Content: "should not appear in excerpt"},
		{Role: llm.RoleSystem, Content: "system prompt should not appear"},
	}

	got := buildConversationExcerpt(messages)

	if !strings.Contains(got, "ASSISTANT:") {
		t.Errorf("missing ASSISTANT prefix; got %q", got)
	}
	if !strings.Contains(got, "I will edit the file.") {
		t.Errorf("missing assistant content; got %q", got)
	}
	if !strings.Contains(got, "TOOL[") {
		t.Errorf("missing TOOL prefix; got %q", got)
	}
	if !strings.Contains(got, "shell_edit") {
		t.Errorf("missing tool name; got %q", got)
	}
	if !strings.Contains(got, "edited successfully") {
		t.Errorf("missing tool content; got %q", got)
	}
	if strings.Contains(got, "should not appear in excerpt") {
		t.Errorf("user message leaked into excerpt; got %q", got)
	}
	if strings.Contains(got, "system prompt should not appear") {
		t.Errorf("system message leaked into excerpt; got %q", got)
	}
}

func TestBuildConversationExcerpt_Truncates(t *testing.T) {
	long := strings.Repeat("z", 1000)
	messages := []llm.ChatMessage{
		{Role: llm.RoleAssistant, Content: long},
	}

	got := buildConversationExcerpt(messages)

	if strings.Contains(got, long) {
		t.Errorf("excerpt not truncated; contains full 1000-char message; len=%d", len(got))
	}
	// 500 chars + "..." = 503 chars for the content portion.
	// The line prefix "ASSISTANT: " adds 11 chars + newline.
	if !strings.Contains(got, "...") {
		t.Errorf("expected truncation marker '...' in excerpt; got %q", got)
	}
}

func TestBuildConversationExcerpt_AssistantWithToolCalls(t *testing.T) {
	messages := []llm.ChatMessage{
		{
			Role:    llm.RoleAssistant,
			Content: "Let me read the file.",
			ToolCalls: []llm.ToolCall{
				{ID: "tc1", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"/foo"}`}},
			},
		},
	}

	got := buildConversationExcerpt(messages)

	if !strings.Contains(got, "ASSISTANT:") {
		t.Errorf("missing ASSISTANT prefix; got %q", got)
	}
	if !strings.Contains(got, "Let me read the file.") {
		t.Errorf("missing assistant content; got %q", got)
	}
}

func TestGenerateHandoff_NilTemplateReg(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	// o.templateReg is nil from newTestOrchestrator
	step := &task.TaskStep{ID: "s1", TaskID: "t1", Description: "do thing", ToolHint: "code"}

	_, err := o.generateHandoff(context.Background(), step, nil)
	if err == nil {
		t.Fatal("expected error when templateReg is nil")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("expected template-related error; got %v", err)
	}
}

func TestGenerateHandoff_NilRegistry(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	// Wire a loader with fallback so templateReg is non-nil and render succeeds,
	// but leave registry nil to test the nil-registry guard.
	o.templateReg = newPlannerTemplateLoader()
	o.templateReg.fallbacks["orchestrator/handoff.md"] = defaultHandoffFallback()
	step := &task.TaskStep{ID: "s1", TaskID: "t1", Description: "do thing", ToolHint: "code"}

	_, err := o.generateHandoff(context.Background(), step, nil)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "registry") && !strings.Contains(err.Error(), "classifier") {
		t.Errorf("expected registry/classifier error; got %v", err)
	}
}
