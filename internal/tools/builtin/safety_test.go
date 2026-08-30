package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadOnlyTools(t *testing.T) {
	tests := []struct {
		name string
		tool interface {
			IsReadOnly(map[string]any) bool
			IsConcurrencySafe(map[string]any) bool
		}
	}{
		{"ReadFileTool", &ReadFileTool{}},
		{"ListDirectoryTool", &ListDirectoryTool{}},
		{"FileGrepTool", &FileGrepTool{}},
		{"FileFindTool", &FileFindTool{}},
		{"WebSearchTool", &WebSearchTool{}},
		{"WebFetchTool", &WebFetchTool{}},
		{"ScheduleListTool", &ScheduleListTool{}},
		{"ScheduleGetTool", &ScheduleGetTool{}},
		{"RecallTool", &RecallTool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.tool.IsReadOnly(nil), "%s should be read-only", tt.name)
			assert.True(t, tt.tool.IsConcurrencySafe(nil), "%s should be concurrency-safe", tt.name)
		})
	}
}

func TestWriteTools(t *testing.T) {
	tests := []struct {
		name string
		tool interface {
			IsReadOnly(map[string]any) bool
			IsConcurrencySafe(map[string]any) bool
		}
		wantReadOnly    bool
		wantConcurrency bool
	}{
		{"WriteFileTool", &WriteFileTool{}, false, true},
		{"FileEditTool", &FileEditTool{}, false, true},
		{"DeleteFileTool", &DeleteFileTool{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantReadOnly, tt.tool.IsReadOnly(nil), "%s IsReadOnly", tt.name)
			assert.Equal(t, tt.wantConcurrency, tt.tool.IsConcurrencySafe(nil), "%s IsConcurrencySafe", tt.name)
		})
	}
}

func TestShellReadOnly(t *testing.T) {
	shell := NewShellExecuteTool("/tmp", 0, nil)
	tests := []struct {
		name         string
		command      string
		wantReadOnly bool
	}{
		{"cat is read-only", "cat /etc/hosts", true},
		{"ls is read-only", "ls -la /tmp", true},
		{"grep is read-only", "grep -r foo .", true},
		{"rm is not read-only", "rm -rf /tmp/foo", false},
		{"write redirect is not read-only", "echo hi > /tmp/file", false},
		{"empty command is not read-only", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]any{"command": tt.command}
			got := shell.IsReadOnly(input)
			assert.Equal(t, tt.wantReadOnly, got, "IsReadOnly(%q)", tt.command)
			// IsConcurrencySafe mirrors IsReadOnly for shell
			assert.Equal(t, got, shell.IsConcurrencySafe(input), "IsConcurrencySafe(%q)", tt.command)
		})
	}
}

func TestDefaultToolsNotReadOnly(t *testing.T) {
	// Tools that embed ToolDefaults without overriding should be conservative.
	tests := []struct {
		name string
		tool interface {
			IsReadOnly(map[string]any) bool
			IsConcurrencySafe(map[string]any) bool
		}
	}{
		{"TaskCreateTool", &TaskCreateTool{}},
		{"RememberTool", &RememberTool{}},
		{"RetainTool", &RetainTool{}},
		{"ReflectTool", &ReflectTool{}},
		{"GitCommitTool", &GitCommitTool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, tt.tool.IsReadOnly(nil), "%s should not be read-only by default", tt.name)
			assert.False(t, tt.tool.IsConcurrencySafe(nil), "%s should not be concurrency-safe by default", tt.name)
		})
	}
}
