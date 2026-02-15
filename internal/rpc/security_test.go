package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/pkg/security"
)

func TestSecurityHandler_CheckPermission(t *testing.T) {
	h := NewSecurityHandler(security.Config{
		AllowedPaths:   []string{"/tmp", "/home/user"},
		BlockedPaths:   []string{"/etc", "/root"},
		BlockFinancial: true,
	})

	tests := []struct {
		name    string
		action  string
		details map[string]string
		allowed bool
	}{
		{
			name:    "allowed file read in tmp",
			action:  "file_read",
			details: map[string]string{"path": "/tmp/test.txt"},
			allowed: true,
		},
		{
			name:    "blocked file read in etc",
			action:  "file_read",
			details: map[string]string{"path": "/etc/passwd"},
			allowed: false,
		},
		{
			name:    "safe shell command",
			action:  "shell_execute",
			details: map[string]string{"command": "ls -la"},
			allowed: true,
		},
		{
			name:    "financial operation blocked",
			action:  "shell_execute",
			details: map[string]string{"command": "transfer funds to account"},
			allowed: false,
		},
		{
			name:    "unknown action",
			action:  "unknown_action",
			details: map[string]string{},
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CheckPermissionRequest{
				Action:  tt.action,
				Details: tt.details,
			}
			params, _ := json.Marshal(req)

			result, err := h.handleCheckPermission(context.Background(), params)
			if err != nil {
				t.Fatalf("handleCheckPermission failed: %v", err)
			}

			resp := result.(CheckPermissionResponse)
			if resp.Allowed != tt.allowed {
				t.Errorf("Allowed = %v, want %v (reason: %s)", resp.Allowed, tt.allowed, resp.Reason)
			}
		})
	}
}

func TestSecurityHandler_EvaluateShellRisk(t *testing.T) {
	h := NewSecurityHandler(security.Config{})

	tests := []struct {
		command  string
		wantRisk string
	}{
		{"ls -la", "MEDIUM"},
		{"rm -rf /", "HIGH"},
		{"git status", "MEDIUM"},
		{"shutdown -h now", "HIGH"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			req := EvaluateShellRiskRequest{Command: tt.command}
			params, _ := json.Marshal(req)

			result, err := h.handleEvaluateShellRisk(context.Background(), params)
			if err != nil {
				t.Fatalf("handleEvaluateShellRisk failed: %v", err)
			}

			resp := result.(EvaluateShellRiskResponse)
			if resp.RiskLevel != tt.wantRisk {
				t.Errorf("RiskLevel = %q, want %q", resp.RiskLevel, tt.wantRisk)
			}
		})
	}
}

func TestSecurityHandler_IsFinancial(t *testing.T) {
	h := NewSecurityHandler(security.Config{})

	tests := []struct {
		text string
		want bool
	}{
		{"transfer funds to account", true},
		{"send payment", true},
		{"read the file", false},
		{"git commit", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			req := IsFinancialRequest{Text: tt.text}
			params, _ := json.Marshal(req)

			result, err := h.handleIsFinancial(context.Background(), params)
			if err != nil {
				t.Fatalf("handleIsFinancial failed: %v", err)
			}

			resp := result.(IsFinancialResponse)
			if resp.IsFinancial != tt.want {
				t.Errorf("IsFinancial = %v, want %v", resp.IsFinancial, tt.want)
			}
		})
	}
}

func TestSecurityHandler_CheckPath(t *testing.T) {
	h := NewSecurityHandler(security.Config{
		AllowedPaths: []string{"/tmp", "/home/user"},
		BlockedPaths: []string{"/etc"},
	})

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/tmp/test.txt", true},
		{"/home/user/file.txt", true},
		{"/etc/passwd", false},
		{"/var/log/syslog", false}, // Not in allowed paths
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := CheckPathRequest{Path: tt.path}
			params, _ := json.Marshal(req)

			result, err := h.handleCheckPath(context.Background(), params)
			if err != nil {
				t.Fatalf("handleCheckPath failed: %v", err)
			}

			resp := result.(CheckPathResponse)
			if resp.Allowed != tt.allowed {
				t.Errorf("Allowed = %v, want %v", resp.Allowed, tt.allowed)
			}
		})
	}
}

func TestSecurityHandler_CheckBatch(t *testing.T) {
	h := NewSecurityHandler(security.Config{
		AllowedPaths: []string{"/tmp"},
		BlockedPaths: []string{"/etc"},
	})

	req := BatchCheckRequest{
		Checks: []CheckPermissionRequest{
			{Action: "file_read", Details: map[string]string{"path": "/tmp/test.txt"}},
			{Action: "file_read", Details: map[string]string{"path": "/etc/passwd"}},
			{Action: "shell_execute", Details: map[string]string{"command": "ls -la"}},
		},
	}
	params, _ := json.Marshal(req)

	result, err := h.handleCheckBatch(context.Background(), params)
	if err != nil {
		t.Fatalf("handleCheckBatch failed: %v", err)
	}

	resp := result.(BatchCheckResponse)
	if len(resp.Results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(resp.Results))
	}

	// Check expected results
	if !resp.Results[0].Allowed {
		t.Errorf("Result[0] should be allowed")
	}
	if resp.Results[1].Allowed {
		t.Errorf("Result[1] should be blocked")
	}
	if !resp.Results[2].Allowed {
		t.Errorf("Result[2] should be allowed")
	}
}

// Benchmarks

func BenchmarkSecurityHandler_CheckPermission(b *testing.B) {
	h := NewSecurityHandler(security.Config{
		AllowedPaths:   []string{"/tmp", "/home/user"},
		BlockedPaths:   []string{"/etc", "/root"},
		BlockFinancial: true,
	})

	req := CheckPermissionRequest{
		Action:  "file_read",
		Details: map[string]string{"path": "/tmp/test.txt"},
	}
	params, _ := json.Marshal(req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.handleCheckPermission(context.Background(), params)
	}
}

func BenchmarkSecurityHandler_CheckBatch(b *testing.B) {
	h := NewSecurityHandler(security.Config{
		AllowedPaths: []string{"/tmp", "/home/user"},
		BlockedPaths: []string{"/etc", "/root"},
	})

	req := BatchCheckRequest{
		Checks: []CheckPermissionRequest{
			{Action: "file_read", Details: map[string]string{"path": "/tmp/test.txt"}},
			{Action: "file_write", Details: map[string]string{"path": "/tmp/out.txt"}},
			{Action: "shell_execute", Details: map[string]string{"command": "ls -la"}},
			{Action: "network_request", Details: map[string]string{"url": "https://api.example.com"}},
		},
	}
	params, _ := json.Marshal(req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.handleCheckBatch(context.Background(), params)
	}
}
