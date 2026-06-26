package security

import (
	"testing"
)

func TestCheckPath(t *testing.T) {
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/home/user/projects", "/tmp"},
		BlockedPaths: []string{"/etc", "/root"},
	})

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/home/user/projects/foo.txt", true},
		{"/home/user/projects", true},
		{"/tmp/scratch", true},
		{"/etc/passwd", false},
		{"/root/.ssh/id_rsa", false},
		{"/var/log/syslog", false}, // Not in allowed paths
	}

	for _, tt := range tests {
		got := pc.CheckPath(tt.path)
		if got != tt.allowed {
			t.Errorf("CheckPath(%q) = %v, want %v", tt.path, got, tt.allowed)
		}
	}
}

func TestCheckPathNoRestrictions(t *testing.T) {
	pc := NewPermissionChecker(Config{})

	// With no restrictions, all paths should be allowed
	paths := []string{"/etc/passwd", "/root/.ssh", "/home/user/file.txt"}
	for _, path := range paths {
		if !pc.CheckPath(path) {
			t.Errorf("CheckPath(%q) = false, want true (no restrictions)", path)
		}
	}
}

func TestEvaluateShellRisk(t *testing.T) {
	tests := []struct {
		command string
		want    RiskLevel
	}{
		{"ls -la", RiskMedium},
		{"cat /etc/passwd", RiskMedium},
		{"rm -rf /", RiskHigh},
		{"rm -rf ~/*", RiskHigh},
		{"chmod -R 777 /", RiskHigh},
		{"shutdown -h now", RiskHigh},
		{"kill -9 1234", RiskHigh},
		{"systemctl stop nginx", RiskHigh},
		{"echo hello", RiskMedium},
		{"git status", RiskMedium},
	}

	for _, tt := range tests {
		got := EvaluateShellRisk(tt.command)
		if got != tt.want {
			t.Errorf("EvaluateShellRisk(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestIsFinancial(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"transfer funds to account", true},
		{"send payment to vendor", true},
		{"purchase items from store", true},
		{"check my bank account balance", true},
		{"wire transfer $1000", true},
		{"buy 1 bitcoin", true},
		{"read the file", false},
		{"git commit -m message", false},
		{"hello world", false},
	}

	for _, tt := range tests {
		got := IsFinancial(tt.text)
		if got != tt.want {
			t.Errorf("IsFinancial(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestCheckPermission(t *testing.T) {
	pc := NewPermissionChecker(Config{
		AllowedPaths:   []string{"/tmp"},
		BlockedPaths:   []string{"/etc"},
		BlockFinancial: true,
	})

	tests := []struct {
		action  string
		details map[string]string
		allowed bool
	}{
		{"file_read", map[string]string{"path": "/tmp/test.txt"}, true},
		{"file_read", map[string]string{"path": "/etc/passwd"}, false},
		{"file_write", map[string]string{"path": "/tmp/out.txt"}, true},
		{"shell_execute", map[string]string{"command": "ls -la"}, true},
		{"shell_execute", map[string]string{"command": "transfer funds"}, false}, // Financial
		{"network_request", map[string]string{"url": "https://api.example.com"}, true},
		{"unknown_action", map[string]string{}, false},
	}

	for _, tt := range tests {
		result := pc.CheckPermission(tt.action, tt.details)
		if result.Allowed != tt.allowed {
			t.Errorf("CheckPermission(%q, %v) = %v, want %v (reason: %s)",
				tt.action, tt.details, result.Allowed, tt.allowed, result.Reason)
		}
	}
}

// stubPreExecChecker is a test double for PreExecChecker that denies when
// the tool name matches the forbidden entry.
type stubPreExecChecker struct {
	forbiddenTool string
}

func (s stubPreExecChecker) Check(action, toolName string, details map[string]string) PreExecDecision {
	if toolName == s.forbiddenTool {
		return PreExecDecision{
			Allowed: false,
			Reason:  "tool " + toolName + " is forbidden",
		}
	}
	return PreExecDecision{Allowed: true}
}

func TestCheckPermission_PreExecChecker_ToolName(t *testing.T) {
	pc := NewPermissionChecker(Config{})
	pc.SetPreExecChecker("emp-1", stubPreExecChecker{forbiddenTool: "git_push"})

	tests := []struct {
		name     string
		action   string
		details  map[string]string
		allowed  bool
	}{
		{
			name:    "forbidden by tool name — denied",
			action:  "shell_execute",
			details: map[string]string{"agent_id": "emp-1", "tool_name": "git_push", "command": "git push origin main"},
			allowed: false,
		},
		{
			name:    "different tool — allowed",
			action:  "shell_execute",
			details: map[string]string{"agent_id": "emp-1", "tool_name": "git_status", "command": "git status"},
			allowed: true,
		},
		{
			name:    "no agent_id — checker skipped",
			action:  "shell_execute",
			details: map[string]string{"tool_name": "git_push", "command": "git push"},
			allowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pc.CheckPermission(tt.action, tt.details)
			if result.Allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", result.Allowed, tt.allowed, result.Reason)
			}
		})
	}
}

// TestCheckPermissionForAgent_BackwardCompat verifies that CheckPermission
// delegates to CheckPermissionForAgent with empty agentID, preserving
// existing behavior (E6).
func TestCheckPermissionForAgent_BackwardCompat(t *testing.T) {
	pc := NewPermissionChecker(Config{})
	result := pc.CheckPermission("file_read", map[string]string{"path": "/tmp/test"})
	if !result.Allowed {
		t.Errorf("CheckPermission should allow file_read, got: %s", result.Reason)
	}
}

// TestCheckPermissionForAgent_WithAgentID verifies that CheckPermissionForAgent
// uses the agentID parameter for PreExecChecker lookup when details["agent_id"]
// is not set (E6).
func TestCheckPermissionForAgent_WithAgentID(t *testing.T) {
	pc := NewPermissionChecker(Config{})
	pc.SetPreExecChecker("emp-1", stubPreExecChecker{forbiddenTool: "git_push"})

	t.Run("agentID triggers pre-exec check", func(t *testing.T) {
		result := pc.CheckPermissionForAgent("shell_execute", map[string]string{
			"tool_name": "git_push",
		}, "emp-1")
		if result.Allowed {
			t.Error("expected denied for forbidden tool")
		}
	})

	t.Run("empty agentID skips pre-exec check", func(t *testing.T) {
		result := pc.CheckPermissionForAgent("shell_execute", map[string]string{
			"tool_name": "git_push",
		}, "")
		if !result.Allowed {
			t.Errorf("expected allowed with empty agentID, got: %s", result.Reason)
		}
	})

	t.Run("details agent_id takes precedence over parameter", func(t *testing.T) {
		// When details["agent_id"] is set, it takes precedence over the agentID parameter.
		// This preserves backward compat for callers that use the details-based path.
		result := pc.CheckPermissionForAgent("shell_execute", map[string]string{
			"tool_name": "git_push",
			"agent_id":  "emp-1",
		}, "") // empty agentID, but details has agent_id
		if result.Allowed {
			t.Error("expected denied when agent_id in details maps to registered checker")
		}
	})

	t.Run("different agentID skips unregistered checker", func(t *testing.T) {
		result := pc.CheckPermissionForAgent("shell_execute", map[string]string{
			"tool_name": "git_push",
		}, "emp-2") // no checker registered for emp-2
		if !result.Allowed {
			t.Errorf("expected allowed for unregistered agentID, got: %s", result.Reason)
		}
	})
}

// Benchmarks

func BenchmarkCheckPath(b *testing.B) {
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/home/user/projects", "/tmp", "/var/data"},
		BlockedPaths: []string{"/etc", "/root", "/usr/sbin"},
	})

	paths := []string{
		"/home/user/projects/src/main.go",
		"/tmp/scratch.txt",
		"/etc/passwd",
		"/var/log/syslog",
	}

	b.ResetTimer()
	for range b.N {
		for _, path := range paths {
			pc.CheckPath(path)
		}
	}
}

func BenchmarkEvaluateShellRisk(b *testing.B) {
	commands := []string{
		"ls -la",
		"git status",
		"rm -rf /tmp/test",
		"echo hello world",
		"chmod 755 file.sh",
	}

	b.ResetTimer()
	for range b.N {
		for _, cmd := range commands {
			EvaluateShellRisk(cmd)
		}
	}
}

func BenchmarkIsFinancial(b *testing.B) {
	texts := []string{
		"read the file and process data",
		"transfer funds to savings account",
		"run the test suite",
		"purchase order #12345",
	}

	b.ResetTimer()
	for range b.N {
		for _, text := range texts {
			IsFinancial(text)
		}
	}
}

func BenchmarkCheckPermission(b *testing.B) {
	pc := NewPermissionChecker(Config{
		AllowedPaths:   []string{"/home/user", "/tmp"},
		BlockedPaths:   []string{"/etc", "/root"},
		BlockFinancial: true,
	})

	actions := []struct {
		action  string
		details map[string]string
	}{
		{"file_read", map[string]string{"path": "/home/user/test.txt"}},
		{"file_write", map[string]string{"path": "/tmp/output.txt"}},
		{"shell_execute", map[string]string{"command": "ls -la"}},
		{"network_request", map[string]string{"url": "https://api.example.com"}},
	}

	b.ResetTimer()
	for range b.N {
		for _, a := range actions {
			pc.CheckPermission(a.action, a.details)
		}
	}
}
