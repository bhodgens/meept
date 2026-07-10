package agent

import (
	"reflect"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestToolNamesSinceLastUser(t *testing.T) {
	t.Parallel()

	mkTC := func(name string) llm.ToolCall {
		return llm.ToolCall{Function: llm.ToolCallFunction{Name: name}}
	}

	tests := []struct {
		name string
		msgs []llm.ChatMessage
		want []string
	}{
		{
			name: "empty",
			msgs: nil,
			want: nil,
		},
		{
			name: "no user message",
			msgs: []llm.ChatMessage{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{mkTC("grep")}},
			},
			want: nil,
		},
		{
			name: "pure chat no tools",
			msgs: []llm.ChatMessage{
				{Role: llm.RoleUser, Content: "hello"},
				{Role: llm.RoleAssistant, Content: "hi"},
			},
			want: nil,
		},
		{
			name: "current turn tools only",
			msgs: []llm.ChatMessage{
				{Role: llm.RoleUser, Content: "old question"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{mkTC("memory_search"), mkTC("file_read")}},
				{Role: llm.RoleTool, Content: "old result"},
				{Role: llm.RoleAssistant, Content: "old answer"},
				{Role: llm.RoleUser, Content: "new question"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{mkTC("web_search"), mkTC("grep")}},
				{Role: llm.RoleTool, Content: "new result"},
			},
			want: []string{"web_search", "grep"},
		},
		{
			name: "multi-hop same turn",
			msgs: []llm.ChatMessage{
				{Role: llm.RoleUser, Content: "debug panic"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{mkTC("grep")}},
				{Role: llm.RoleTool, Content: "hit"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{mkTC("file_read")}},
				{Role: llm.RoleTool, Content: "src"},
			},
			want: []string{"grep", "file_read"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toolNamesSinceLastUser(tt.msgs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toolNamesSinceLastUser() = %v, want %v", got, tt.want)
			}
		})
	}
}
