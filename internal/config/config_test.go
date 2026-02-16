package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("ANOTHER_VAR", "another")
	defer os.Unsetenv("TEST_VAR")
	defer os.Unsetenv("ANOTHER_VAR")

	tests := []struct {
		input    string
		expected string
	}{
		{"${TEST_VAR}", "test_value"},
		{"$TEST_VAR", "test_value"},
		{"prefix_${TEST_VAR}_suffix", "prefix_test_value_suffix"},
		{"${TEST_VAR}/${ANOTHER_VAR}", "test_value/another"},
		{"${UNDEFINED_VAR}", ""},
		{"no_vars_here", "no_vars_here"},
	}

	for _, tt := range tests {
		result := expandEnvVars(tt.input)
		if result != tt.expected {
			t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test", filepath.Join(homeDir, "test")},
		{"~", homeDir},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Daemon.LogLevel != "INFO" {
		t.Errorf("Default log level = %q, want INFO", cfg.Daemon.LogLevel)
	}

	if cfg.LLM.Budget.HourlyTokenLimit != 100000 {
		t.Errorf("Default hourly token limit = %d, want 100000", cfg.LLM.Budget.HourlyTokenLimit)
	}

	if cfg.Security.BlockFinancial != true {
		t.Error("Default block_financial should be true")
	}

	if cfg.Memory.Episodic.Enabled != true {
		t.Error("Default episodic memory should be enabled")
	}
}

func TestStripJSON5Comments(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "no comments",
			input: `{"key": "value"}`,
		},
		{
			name:  "single line comment",
			input: "{\n  // comment\n  \"key\": \"value\"\n}",
		},
		{
			name:  "multi line comment",
			input: "{\n  /* comment */\n  \"key\": \"value\"\n}",
		},
		{
			name:  "url not stripped",
			input: `{"url": "http://example.com"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripJSON5Comments(tt.input)
			// Verify the result is valid JSON-like (no comment markers remain)
			if strings.Contains(result, "//") && !strings.Contains(result, "http://") {
				t.Errorf("Result still contains single-line comment: %q", result)
			}
			if strings.Contains(result, "/*") || strings.Contains(result, "*/") {
				t.Errorf("Result still contains multi-line comment markers: %q", result)
			}
		})
	}
}

func TestLoadConfigNonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load should not error for non-existent file: %v", err)
	}

	// Should return defaults
	if cfg.Daemon.LogLevel != "INFO" {
		t.Errorf("Default log level = %q, want INFO", cfg.Daemon.LogLevel)
	}
}

func TestLoadConfigValid(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.toml")

	content := `
[daemon]
log_level = "DEBUG"
socket_path = "/tmp/test.sock"

[llm.budget]
hourly_token_limit = 50000

[security]
block_financial = false
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Daemon.LogLevel != "DEBUG" {
		t.Errorf("Log level = %q, want DEBUG", cfg.Daemon.LogLevel)
	}

	if cfg.Daemon.SocketPath != "/tmp/test.sock" {
		t.Errorf("Socket path = %q, want /tmp/test.sock", cfg.Daemon.SocketPath)
	}

	if cfg.LLM.Budget.HourlyTokenLimit != 50000 {
		t.Errorf("Hourly token limit = %d, want 50000", cfg.LLM.Budget.HourlyTokenLimit)
	}

	if cfg.Security.BlockFinancial != false {
		t.Error("BlockFinancial should be false")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"DEBUG", "DEBUG"},
		{"INFO", "INFO"},
		{"WARN", "WARN"},
		{"WARNING", "WARN"},
		{"ERROR", "ERROR"},
		{"UNKNOWN", "INFO"}, // Default to INFO
	}

	for _, tt := range tests {
		level := ParseLogLevel(tt.input)
		if level.String() != tt.expected {
			t.Errorf("ParseLogLevel(%q) = %q, want %q", tt.input, level.String(), tt.expected)
		}
	}
}
