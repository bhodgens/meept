package config

import (
	"encoding/json"
	"errors"
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

func TestExpandEnvVarsCycle(t *testing.T) {
	os.Setenv("CYCLE_A", "${CYCLE_B}")
	os.Setenv("CYCLE_B", "${CYCLE_A}")
	defer os.Unsetenv("CYCLE_A")
	defer os.Unsetenv("CYCLE_B")

	_, err := ExpandEnvVars("${CYCLE_A}")
	if err == nil {
		t.Fatal("expected ErrEnvVarCycle, got nil")
	}
	if !errors.Is(err, ErrEnvVarCycle{}) {
		t.Errorf("expected ErrEnvVarCycle, got %T: %v", err, err)
	}
}

func TestErrEnvVarCycleIs(t *testing.T) {
	err := ErrEnvVarCycle{Input: "test"}
	if !err.Is(err) {
		t.Error("ErrEnvVarCycle.Is should match itself")
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

func TestExpandConfigPathsLearning(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	cfg := DefaultConfig()
	cfg.Learning.DataDir = "~/.meept/learning"
	cfg.Learning.AdaptersDir = "~/.meept/adapters"

	expandConfigPaths(cfg)

	wantData := filepath.Join(homeDir, ".meept", "learning")
	wantAdapters := filepath.Join(homeDir, ".meept", "adapters")
	if cfg.Learning.DataDir != wantData {
		t.Errorf("Learning.DataDir = %q, want %q", cfg.Learning.DataDir, wantData)
	}
	if cfg.Learning.AdaptersDir != wantAdapters {
		t.Errorf("Learning.AdaptersDir = %q, want %q", cfg.Learning.AdaptersDir, wantAdapters)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Daemon.LogLevel != "INFO" {
		t.Errorf("Default log level = %q, want INFO", cfg.Daemon.LogLevel)
	}

	if cfg.LLM.Budget.HourlyTokenLimit != 0 {
		t.Errorf("Default hourly token limit = %d, want 0 (disabled)", cfg.LLM.Budget.HourlyTokenLimit)
	}

	if cfg.Security.BlockFinancial != true {
		t.Error("Default block_financial should be true")
	}

	if cfg.Memory.Episodic.Enabled != true {
		t.Error("Default episodic memory should be enabled")
	}

	if cfg.Agents.CapabilityMatchEnabled != false {
		t.Error("Default agents.capability_match_enabled should be false")
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
	//nolint:gosec // test directory/file
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
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

func TestLoadJSON5(t *testing.T) {
	f, err := os.CreateTemp("", "test*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	content := `{
		// This is a comment
		"name": "test",
		"value": 42,
		"nested": {
			/* block comment */
			"enabled": true
		}
	}`
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var result struct {
		Name   string `json:"name"`
		Value  int    `json:"value"`
		Nested struct {
			Enabled bool `json:"enabled"`
		} `json:"nested"`
	}

	if err := LoadJSON5(f.Name(), &result); err != nil {
		t.Fatalf("LoadJSON5 failed: %v", err)
	}

	if result.Name != "test" {
		t.Errorf("Name = %q, want test", result.Name)
	}
	if result.Value != 42 {
		t.Errorf("Value = %d, want 42", result.Value)
	}
	if !result.Nested.Enabled {
		t.Error("Nested.Enabled should be true")
	}
}

func TestLoadJSON5EnvVars(t *testing.T) {
	os.Setenv("TEST_JSON5_VAR", "hello")
	defer os.Unsetenv("TEST_JSON5_VAR")

	f, err := os.CreateTemp("", "test*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(`{"msg": "${TEST_JSON5_VAR}"}`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var result struct {
		Msg string `json:"msg"`
	}
	if err := LoadJSON5(f.Name(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Msg != "hello" {
		t.Errorf("Msg = %q, want hello", result.Msg)
	}
}

func TestLoadJSON5Config(t *testing.T) {
	f, err := os.CreateTemp("", "meept*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	content := `{
		"daemon": {
			"log_level": "DEBUG",
			"socket_path": "/tmp/test-json5.sock"
		},
		"llm": {
			"budget": {
				"hourly_token_limit": 50000
			}
		}
	}`
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := LoadJSON5Config(f.Name())
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}

	if cfg.Daemon.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG", cfg.Daemon.LogLevel)
	}
	if cfg.Daemon.SocketPath != "/tmp/test-json5.sock" {
		t.Errorf("SocketPath = %q, want /tmp/test-json5.sock", cfg.Daemon.SocketPath)
	}
	if cfg.LLM.Budget.HourlyTokenLimit != 50000 {
		t.Errorf("HourlyTokenLimit = %d, want 50000", cfg.LLM.Budget.HourlyTokenLimit)
	}
}

func TestDefaultConfigTransport(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Transport.RPC.Enabled {
		t.Error("RPC transport should be enabled by default")
	}
	if cfg.Transport.HTTP.Enabled {
		t.Error("HTTP transport should be disabled by default")
	}
	// Addr is deliberately NOT seeded in DefaultConfig: the HTTP server
	// supplies the same loopback default (127.0.0.1:8081 — NewServer in
	// internal/comm/http), and an empty Addr is what lets
	// `transport.http.port` take effect through ListenAddr.
	//
	// "" is NOT a bind address: net.Listen("tcp", "") binds all interfaces on
	// an ephemeral port, so every consumer MUST fall back to the server's
	// loopback default. The end-to-end "an enabled keyless transport is bound
	// to loopback" invariant is pinned where the binding is decided —
	// internal/daemon/http_listen_addr_test.go
	// (TestHTTPListenAddrEmptyConfigBindsLoopback) — because this config type
	// cannot express or observe a bind policy.
	if cfg.Transport.HTTP.Addr != "" {
		t.Errorf("expected HTTP addr to default to empty (server default applies), got %s", cfg.Transport.HTTP.Addr)
	}
	if got := cfg.Transport.HTTP.ListenAddr(); got != "" {
		t.Errorf("expected default ListenAddr() to be empty (no address configured; the loopback default belongs to the server, never to this value), got %s", got)
	}
	if !cfg.Transport.HTTP.RequireAuth {
		t.Error("HTTP RequireAuth should be true by default")
	}
	if cfg.Transport.HTTP.TLSCertFile == "" {
		t.Error("HTTP TLSCertFile should not be empty by default")
	}
	if cfg.Transport.HTTP.TLSKeyFile == "" {
		t.Error("HTTP TLSKeyFile should not be empty by default")
	}
}

// TestHTTPTransportListenAddr pins the precedence of the `port` convenience
// alias over the raw Addr field. Before Port existed, `transport.http.port`
// was an undeclared key and JSON5/TOML decoding dropped it silently; every
// consumer resolving through ListenAddr is what keeps it visible now.
func TestHTTPTransportListenAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		port int
		want string
	}{
		{name: "addr set wins over port", addr: "127.0.0.1:9999", port: 18095, want: "127.0.0.1:9999"},
		{name: "host-less addr wins over port", addr: ":9000", port: 18095, want: ":9000"},
		{name: "addr only", addr: ":8081", port: 0, want: ":8081"},
		{name: "port only resolves to loopback", addr: "", port: 18095, want: "127.0.0.1:18095"},
		{name: "neither set stays empty for the server default", addr: "", port: 0, want: ""},
		{name: "negative port ignored", addr: "", port: -1, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := HTTPTransportConfig{Addr: tt.addr, Port: tt.port}
			if got := cfg.ListenAddr(); got != tt.want {
				t.Errorf("HTTPTransportConfig{Addr: %q, Port: %d}.ListenAddr() = %q, want %q",
					tt.addr, tt.port, got, tt.want)
			}
		})
	}
}

// TestLoadJSON5ConfigHTTPPortAlias loads the exact config shape that was
// silently ignored during the port-collision incident: transport.http with
// only a `port` key.
func TestLoadJSON5ConfigHTTPPortAlias(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "meept.json5")
	content := `{
	  "transport": {
	    "http": {
	      "enabled": true,
	      "port": 18095,
	    },
	  },
	}`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadJSON5Config(configPath)
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}

	if cfg.Transport.HTTP.Port != 18095 {
		t.Errorf("Port = %d, want 18095 (the key must not be silently dropped)", cfg.Transport.HTTP.Port)
	}
	if got := cfg.Transport.HTTP.ListenAddr(); got != "127.0.0.1:18095" {
		t.Errorf("ListenAddr() = %q, want 127.0.0.1:18095", got)
	}
}

// TestLoadJSON5ConfigHTTPAddrBeatsPort asserts the documented precedence
// survives a real config load: `addr` wins when both keys are present.
func TestLoadJSON5ConfigHTTPAddrBeatsPort(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "meept.json5")
	content := `{
	  "transport": {
	    "http": {
	      "enabled": true,
	      "addr": "127.0.0.1:9999",
	      "port": 18095,
	    },
	  },
	}`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadJSON5Config(configPath)
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}
	if got := cfg.Transport.HTTP.ListenAddr(); got != "127.0.0.1:9999" {
		t.Errorf("ListenAddr() = %q, want 127.0.0.1:9999 (addr must win over port)", got)
	}
}

// TestLoadDefaultHTTPPortAlias exercises the daemon's real load path
// (LoadDefault -> $MEEPT_HOME/meept.json5) with the incident's config shape:
// transport.http declaring only `port`. This is the end-to-end guard that the
// key reaches the HTTP listener address instead of being dropped.
func TestLoadDefaultHTTPPortAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvMeeptHome, home)

	content := `{
	  "transport": {
	    "http": {
	      "enabled": true,
	      "port": 18095,
	    },
	  },
	}`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(home, "meept.json5"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}
	if cfg.Transport.HTTP.Port != 18095 {
		t.Errorf("Port = %d, want 18095", cfg.Transport.HTTP.Port)
	}
	if got := cfg.Transport.HTTP.ListenAddr(); got != "127.0.0.1:18095" {
		t.Errorf("ListenAddr() = %q, want 127.0.0.1:18095", got)
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

// TestDefaultConfigOrchestrator validates the Thread D complexity-routing
// thresholds and Thread C+F forward-compat fields are populated correctly.
func TestDefaultConfigOrchestrator(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Orchestrator.AmbiguityThreshold != 0.6 {
		t.Errorf("AmbiguityThreshold = %v, want 0.6", cfg.Orchestrator.AmbiguityThreshold)
	}
	if cfg.Orchestrator.InterviewAmbiguityThreshold != 0.6 {
		t.Errorf("InterviewAmbiguityThreshold = %v, want 0.6", cfg.Orchestrator.InterviewAmbiguityThreshold)
	}
	if cfg.Orchestrator.MaxStepsPerPhase != 8 {
		t.Errorf("MaxStepsPerPhase = %d, want 8", cfg.Orchestrator.MaxStepsPerPhase)
	}
	if cfg.Orchestrator.MaxPhases != 12 {
		t.Errorf("MaxPhases = %d, want 12", cfg.Orchestrator.MaxPhases)
	}
}

// TestOrchestratorConfigJSONRoundTrip verifies the new fields survive
// JSON marshal/unmarshal so config files with these keys load correctly.
func TestOrchestratorConfigJSONRoundTrip(t *testing.T) {
	original := OrchestratorConfig{
		MaxPlanSteps:                7,
		MaxResearchSteps:            2,
		PlannerTimeout:              90,
		TokenBudgetAlert:            4000,
		MaxHandoffSteps:             3,
		HandoffUseAmendment:         true,
		AmbiguityThreshold:          0.75,
		InterviewAmbiguityThreshold: 0.8,
		MaxStepsPerPhase:            10,
		MaxPhases:                   15,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded OrchestratorConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.AmbiguityThreshold != original.AmbiguityThreshold {
		t.Errorf("AmbiguityThreshold round-trip = %v, want %v",
			decoded.AmbiguityThreshold, original.AmbiguityThreshold)
	}
	if decoded.InterviewAmbiguityThreshold != original.InterviewAmbiguityThreshold {
		t.Errorf("InterviewAmbiguityThreshold round-trip = %v, want %v",
			decoded.InterviewAmbiguityThreshold, original.InterviewAmbiguityThreshold)
	}
	if decoded.MaxStepsPerPhase != original.MaxStepsPerPhase {
		t.Errorf("MaxStepsPerPhase round-trip = %d, want %d",
			decoded.MaxStepsPerPhase, original.MaxStepsPerPhase)
	}
	if decoded.MaxPhases != original.MaxPhases {
		t.Errorf("MaxPhases round-trip = %d, want %d",
			decoded.MaxPhases, original.MaxPhases)
	}
}

// TestOrchestratorConfigZeroValueFallback verifies the backward-compat
// behavior: an empty OrchestratorConfig (user config without the new keys)
// yields zero values, which the constructor layers guard with legacy defaults.
func TestOrchestratorConfigZeroValueFallback(t *testing.T) {
	empty := OrchestratorConfig{}

	if empty.AmbiguityThreshold != 0 {
		t.Errorf("zero-value AmbiguityThreshold = %v, want 0", empty.AmbiguityThreshold)
	}
	if empty.InterviewAmbiguityThreshold != 0 {
		t.Errorf("zero-value InterviewAmbiguityThreshold = %v, want 0", empty.InterviewAmbiguityThreshold)
	}
	if empty.MaxStepsPerPhase != 0 {
		t.Errorf("zero-value MaxStepsPerPhase = %d, want 0", empty.MaxStepsPerPhase)
	}
	if empty.MaxPhases != 0 {
		t.Errorf("zero-value MaxPhases = %d, want 0", empty.MaxPhases)
	}

	// Verify JSON with missing keys also yields zero values (real-world scenario:
	// existing user config files do not have the new keys).
	jsonStr := `{"max_plan_steps": 10}`
	var fromJSON OrchestratorConfig
	if err := json.Unmarshal([]byte(jsonStr), &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if fromJSON.AmbiguityThreshold != 0 {
		t.Errorf("missing-key AmbiguityThreshold = %v, want 0", fromJSON.AmbiguityThreshold)
	}
	if fromJSON.InterviewAmbiguityThreshold != 0 {
		t.Errorf("missing-key InterviewAmbiguityThreshold = %v, want 0", fromJSON.InterviewAmbiguityThreshold)
	}
}
