package builtin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellExecuteTool_Execute(t *testing.T) {
	tool := NewShellExecuteTool("", time.Second*10, nil)
	ctx := context.Background()

	// Test simple echo
	t.Run("simple echo", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"command": "echo hello",
		})
		require.NoError(t, err, "unexpected error")

		shellResult := unwrapShellResult(t, result)
		assert.Equal(t, 0, shellResult.ReturnCode)
		if !strings.Contains(shellResult.Stdout, "hello") {
			t.Errorf("expected stdout to contain 'hello', got %q", shellResult.Stdout)
		}
	})

	// Test command with stderr
	t.Run("with stderr", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"command": "echo error >&2",
		})
		require.NoError(t, err, "unexpected error")

		shellResult := unwrapShellResult(t, result)
		if !strings.Contains(shellResult.Stderr, "error") {
			t.Errorf("expected stderr to contain 'error', got %q", shellResult.Stderr)
		}
	})

	// Test non-zero exit code
	t.Run("non-zero exit", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"command": "exit 42",
		})
		require.NoError(t, err, "unexpected error")

		shellResult := unwrapShellResult(t, result)
		assert.Equal(t, 42, shellResult.ReturnCode)
	})

	// Test timeout
	t.Run("timeout", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"command": "sleep 10",
			"timeout": float64(0.1),
		})
		assert.Error(t, err, "expected timeout error")
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("expected timeout error, got: %v", err)
		}
	})

	// Test empty command
	t.Run("empty command", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"command": "",
		})
		assert.Error(t, err, "expected error for empty command")
	})

	// Test blocked command
	t.Run("blocked command", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"command": "rm -rf /",
		})
		assert.Error(t, err, "expected error for blocked command")
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("expected blocked error, got: %v", err)
		}
	})

	// Test sudo blocked
	t.Run("sudo blocked", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"command": "sudo ls",
		})
		assert.Error(t, err, "expected error for sudo command")
	})
}

func TestShellExecuteTool_ClassifyRisk(t *testing.T) {
	tool := NewShellExecuteTool("", 0, nil)

	tests := []struct {
		command string
		want    ShellCommandRisk
	}{
		// Read-only commands (MEDIUM)
		{"ls", RiskMedium},
		{"cat file.txt", RiskMedium},
		{"grep pattern file", RiskMedium},
		{"git status", RiskMedium},
		{"python3 script.py", RiskMedium},

		// Blocked commands (CRITICAL)
		{"rm file", RiskCritical},
		{"rm -rf /", RiskCritical},
		{"sudo ls", RiskCritical},
		{"kill 1234", RiskCritical},
		{"chmod 777 file", RiskCritical},

		// Unknown commands (HIGH)
		{"unknown_command", RiskHigh},
		{"./custom_script.sh", RiskHigh},

		// Pipes - evaluated segment by segment
		{"cat file | grep pattern", RiskMedium},
		{"cat file | rm -rf /", RiskCritical},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := tool.classifyRisk(tt.command)
			if got != tt.want {
				t.Errorf("classifyRisk(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"ls", "ls"},
		{"ls -la", "ls"},
		{"/usr/bin/ls", "ls"},
		{"FOO=bar ls", "ls"},
		{"FOO=bar BAR=baz ls -la", "ls"},
		{"", ""},
		{"   ", ""},
		// Quoted strings
		{"cmd 'arg with space'", "cmd"},
		{"cmd \"double quoted\"", "cmd"},
		{"FOO='bar baz' make build", "make"},
		{"echo 'nested \"quotes\"'", "echo"},
		{"ENV_VAR=value ./my-tool --flag", "my-tool"},
		{"/usr/bin/python3 script.py", "python3"},
		{"sudo apt-get install", "sudo"}, // classifyRisk handles sudo specially anyway
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := extractBaseCommand(tt.command)
			if got != tt.want {
				t.Errorf("extractBaseCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestShellExecuteTool_WorkingDir(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellExecuteTool(dir, 0, nil)
	ctx := context.Background()

	// Test default working directory
	t.Run("default working dir", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"command": "pwd",
		})
		require.NoError(t, err, "unexpected error")

		shellResult := unwrapShellResult(t, result)
		if !strings.Contains(shellResult.Stdout, dir) {
			t.Errorf("expected working dir %q in output, got %q", dir, shellResult.Stdout)
		}
	})

	// Test custom working directory
	t.Run("custom working dir", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"command":     "pwd",
			"working_dir": "/tmp",
		})
		require.NoError(t, err, "unexpected error")

		shellResult := unwrapShellResult(t, result)
		// /tmp might be a symlink to /private/tmp on macOS
		if !strings.Contains(shellResult.Stdout, "tmp") {
			t.Errorf("expected '/tmp' or '/private/tmp' in output, got %q", shellResult.Stdout)
		}
	})
}

func unwrapShellResult(t *testing.T, result any) ShellResult {
	t.Helper()
	toolResult, ok := result.(tools.ToolResult)
	if !ok {
		t.Fatalf("expected tools.ToolResult, got %T", result)
	}
	shellResult, ok := toolResult.Result.(ShellResult)
	if !ok {
		t.Fatalf("expected ShellResult in ToolResult.Result, got %T", toolResult.Result)
	}
	return shellResult
}

// TestShellRisk_ConfigurableAllowlist verifies that an unknown command is
// classified RiskHigh by default but drops to RiskMedium when added via
// SetKnownSafeCommands.
func TestShellRisk_ConfigurableAllowlist(t *testing.T) {
	tool := NewShellExecuteTool("", time.Second*10, nil)

	// Default: unknown command is RiskHigh.
	if got := tool.classifyRisk("mytool --flag"); got != RiskHigh {
		t.Errorf("classifyRisk(mytool) = %v, want RiskHigh before allowlist", got)
	}

	tool.SetKnownSafeCommands([]string{"mytool"})

	if got := tool.classifyRisk("mytool --flag"); got != RiskMedium {
		t.Errorf("classifyRisk(mytool) = %v, want RiskMedium after allowlist", got)
	}

	// Blocked commands remain RiskCritical even if in the allowlist.
	tool.SetKnownSafeCommands([]string{"rm"})
	if got := tool.classifyRisk("rm -rf /"); got != RiskCritical {
		t.Errorf("classifyRisk(rm) = %v, want RiskCritical (blocked list wins)", got)
	}
}

// mockFenceChecker is a test double for FenceChecker.
type mockFenceChecker struct {
	rejectCmd string
	rejectDir string
}

func (m *mockFenceChecker) CheckPath(path string, op string) error {
	return nil
}

func (m *mockFenceChecker) CheckCommand(cmd string, workDir string) error {
	if m.rejectDir != "" && workDir == m.rejectDir {
		return fmt.Errorf("fence rejected: dir %s is outside allowed boundaries", workDir)
	}
	return nil
}

// TestShellExecuteTool_CreateSession_FenceCheck verifies that CreateSession
// honors the fence checker for the working directory (SEC-H4 fix).
func TestShellExecuteTool_CreateSession_FenceCheck(t *testing.T) {
	fence := &mockFenceChecker{rejectDir: "/forbidden"}

	tool := NewShellExecuteTool("", time.Second*10, nil)
	tool.SetFenceChecker(fence)

	_, err := tool.CreateSession("test-session", tools.PTYSessionConfig{
		Cmd:  "bash",
		Args: []string{},
		Dir:  "/forbidden",
		Rows: 24,
		Cols: 80,
	})

	if err == nil {
		t.Fatal("expected fence rejection error, got nil")
	}
	if !strings.Contains(err.Error(), "fence") {
		t.Errorf("expected fence-related error, got: %v", err)
	}
}

