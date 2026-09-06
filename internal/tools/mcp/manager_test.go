package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	m := NewManager(logger)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}

	if m.ServerCount() != 0 {
		t.Errorf("expected 0 servers, got %d", m.ServerCount())
	}
}

func TestManagerStartServerValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)
	ctx := context.Background()

	// Test empty name
	err := m.StartServer(ctx, ServerConfig{})
	if err == nil {
		t.Error("expected error for empty server name")
	}

	// Test missing command and URL
	err = m.StartServer(ctx, ServerConfig{Name: "test"})
	if err == nil {
		t.Error("expected error when both command and url are missing")
	}

	// Test unknown transport type
	err = m.StartServer(ctx, ServerConfig{
		Name: "test",
		Type: "invalid",
		URL:  "http://localhost:8080",
	})
	if err == nil {
		t.Error("expected error for unknown transport type")
	}
}

func TestManagerGetClientNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	client := m.GetClient("nonexistent")
	if client != nil {
		t.Error("expected nil for nonexistent client")
	}
}

func TestManagerStopServerNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	err := m.StopServer("nonexistent")
	if err == nil {
		t.Error("expected error for stopping nonexistent server")
	}
}

func TestManagerIsServerConnected(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	if m.IsServerConnected("nonexistent") {
		t.Error("expected false for nonexistent server")
	}
}

func TestManagerAllToolsEmpty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	tools := m.AllTools()
	if len(tools) != 0 {
		t.Errorf("expected empty tools list, got %d", len(tools))
	}
}

func TestManagerAllLLMDefinitionsEmpty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	defs := m.AllLLMDefinitions()
	if len(defs) != 0 {
		t.Errorf("expected empty definitions list, got %d", len(defs))
	}
}

func TestManagerCallToolInvalidFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)
	ctx := context.Background()

	// Test invalid tool name format (missing dot)
	_, err := m.CallTool(ctx, "toolwithoutserver", nil)
	if err == nil {
		t.Error("expected error for invalid tool name format")
	}

	// Test server not found
	_, err = m.CallTool(ctx, "nonexistent.tool", nil)
	if err == nil {
		t.Error("expected error for nonexistent server")
	}
}

func TestManagerListServers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	servers := m.ListServers()
	if len(servers) != 0 {
		t.Errorf("expected empty server list, got %d", len(servers))
	}
}

func TestManagerStopAll(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	// Should not panic with empty manager
	m.StopAll()

	if m.ServerCount() != 0 {
		t.Errorf("expected 0 servers after StopAll, got %d", m.ServerCount())
	}
}

func TestGetRateLimiter_Defaults(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	// First call should create a new limiter with default values
	limiter := m.getRateLimiter("test-server")
	if limiter == nil {
		t.Fatal("getRateLimiter returned nil")
	}

	// Default: 10 RPS, burst 20
	// Limit() should return 10
	if float64(limiter.Limit()) != 10.0 {
		t.Errorf("expected default limit 10.0, got %f", limiter.Limit())
	}
}

func TestGetRateLimiter_FromConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	// Set config with custom rate limits
	m.SetConfigs([]ServerConfig{
		{
			Name:           "custom-server",
			RateLimitRPS:   5.0,
			RateLimitBurst: 10,
		},
	})

	limiter := m.getRateLimiter("custom-server")
	if limiter == nil {
		t.Fatal("getRateLimiter returned nil")
	}

	if float64(limiter.Limit()) != 5.0 {
		t.Errorf("expected custom limit 5.0, got %f", limiter.Limit())
	}
}

func TestCallTool_RateLimitEnforced(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	// Set config with very low rate limit: 1 RPS, burst of 1
	m.SetConfigs([]ServerConfig{
		{
			Name:           "rate-limited-server",
			RateLimitRPS:   1.0,
			RateLimitBurst: 1,
		},
	})

	// Get the limiter directly
	limiter := m.getRateLimiter("rate-limited-server")

	// First Allow() should succeed (uses the burst)
	if !limiter.Allow() {
		t.Error("expected first Allow() to succeed")
	}

	// Second immediate Allow() should fail (rate limited)
	if limiter.Allow() {
		t.Error("expected second Allow() to be rate limited")
	}
}

// TestStartServer_LaunchFailureWarnsDaemonPath verifies the issue #32
// diagnosability warning: a stdio server whose binary cannot exist must
// emit a single Warn line carrying the daemon's runtime PATH (what a
// subprocess would see via cmd.Env = os.Environ() in transport/stdio.go),
// so "works in shell, fails under launchd" is diagnosable from the log
// alone. Seam: the Manager's injectable *slog.Logger (captured via a
// buffer-backed slog handler) — no code refactor needed.
func TestStartServer_LaunchFailureWarnsDaemonPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	m := NewManager(logger)

	err := m.StartServer(context.Background(), ServerConfig{
		Name:    "no-such-server",
		Command: []string{"/nonexistent-binary-xyz"},
		Type:    "stdio",
	})
	if err == nil {
		t.Fatal("expected StartServer to fail for a nonexistent binary")
	}

	out := buf.String()
	if !strings.Contains(out, "mcp server launch failed") {
		t.Fatalf("expected warning line %q in log output, got:\n%s", "mcp server launch failed", out)
	}
	for _, want := range []string{`server=no-such-server`, "daemon_path=" + os.Getenv("PATH")} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in log output, got:\n%s", want, out)
		}
	}

	// Failure must also be recorded in stats (StateError), matching the
	// contract of the surrounding error path.
	m.mu.RLock()
	st := m.stats["no-such-server"]
	m.mu.RUnlock()
	if st == nil || st.State != StateError {
		t.Fatalf("expected StateError stats entry for failed launch, got %+v", st)
	}
}

// TestServerConfigInstallHintRoundTrip verifies the install_hint field
// round-trips through JSON when set and is omitted entirely when empty
// (omitempty keeps absent entries clean for existing configs).
func TestServerConfigInstallHintRoundTrip(t *testing.T) {
	in := `{"name":"github","type":"stdio","command":["npx","-y","@modelcontextprotocol/server-github"],"install_hint":"npm install -g @modelcontextprotocol/server-github"}`
	var sc ServerConfig
	if err := json.Unmarshal([]byte(in), &sc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if sc.InstallHint != "npm install -g @modelcontextprotocol/server-github" {
		t.Errorf("InstallHint = %q, want the npm hint", sc.InstallHint)
	}

	out, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back ServerConfig
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-unmarshal failed: %v", err)
	}
	if back.InstallHint != sc.InstallHint {
		t.Errorf("round-trip InstallHint = %q, want %q", back.InstallHint, sc.InstallHint)
	}
	if !strings.Contains(string(out), `"install_hint"`) {
		t.Errorf("marshaled JSON missing install_hint key: %s", out)
	}

	var empty ServerConfig
	emptyOut, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty failed: %v", err)
	}
	if strings.Contains(string(emptyOut), "install_hint") {
		t.Errorf("empty InstallHint should be omitted (omitempty), got: %s", emptyOut)
	}
}
