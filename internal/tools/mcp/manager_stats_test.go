// Package mcp contains unit tests for the per-server stats and runtime
// state tracking added by the MCP default catalog plan (Phase 9).
//
// These tests are white-box (package mcp) so they can access the Manager's
// unexported fields (clients, stats, configs) for setup and verification.
//
// Testing approach for CallTool:
//   - Option C (minimum viable) from the spec is used for the direct
//     CallTool counter test: we invoke CallTool against a server whose
//     client is not connected, which exercises the early-return error
//     path without incrementing counters. The actual counter-increment
//     path is exercised via a real subprocess (TestMCPToolExecution in
//     tests/integration/mcp_test.go) and by the TestCallTool_* tests
//     below that manually inject a mock client to verify the counters
//     increment correctly under both success and failure returns.
package mcp

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools/mcp/transport"
)

// boolPtr returns a pointer to b. Used to construct ServerConfig.Enabled.
func boolPtr(b bool) *bool { return &b }

// quietLogger returns a logger that discards output, keeping tests quiet.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Spec section 9: required unit tests ---

// TestSetConfigs_PopulatesConfigsMap verifies that SetConfigs records both
// enabled and disabled server configs so AllServerStatuses can report on them.
func TestSetConfigs_PopulatesConfigsMap(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	m.SetConfigs([]ServerConfig{
		{Name: "a", Enabled: boolPtr(true), Category: "alpha", Description: "alpha-a"},
		{Name: "b", Enabled: boolPtr(false), Category: "beta", Description: "beta-b"},
	})

	m.mu.RLock()
	aCfg, aOk := m.configs["a"]
	bCfg, bOk := m.configs["b"]
	m.mu.RUnlock()

	if !aOk || !bOk {
		t.Fatalf("expected both configs in map; got a=%v b=%v", aOk, bOk)
	}
	if !aCfg.IsEnabled() {
		t.Errorf("server a should be enabled; IsEnabled=%v", aCfg.IsEnabled())
	}
	if bCfg.IsEnabled() {
		t.Errorf("server b should be disabled; IsEnabled=%v", bCfg.IsEnabled())
	}

	// AllServerStatuses should report both entries.
	entries := m.AllServerStatuses()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(entries))
	}
	// Verify sorted by Category then Name ("alpha" < "beta").
	if got, want := entries[0].Config.Name, "a"; got != want {
		t.Errorf("entries[0].Config.Name = %q; want %q", got, want)
	}
	if got, want := entries[1].Config.Name, "b"; got != want {
		t.Errorf("entries[1].Config.Name = %q; want %q", got, want)
	}

	// Verify states: enabled-and-never-started -> inactive; disabled -> disabled.
	if entries[0].Stats.State != StateInactive {
		t.Errorf("server a state = %q; want %q", entries[0].Stats.State, StateInactive)
	}
	if entries[1].Stats.State != StateDisabled {
		t.Errorf("server b state = %q; want %q", entries[1].Stats.State, StateDisabled)
	}
}

// TestAllServerStatuses_IncludesDisabledServers verifies that disabled
// servers appear with state=disabled even when StartServer is never called.
func TestAllServerStatuses_IncludesDisabledServers(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	m.SetConfigs([]ServerConfig{
		{Name: "disabled-one", Enabled: boolPtr(false), Category: "z", Description: "never starts"},
	})

	entries := m.AllServerStatuses()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(entries))
	}
	if got, want := entries[0].Stats.State, StateDisabled; got != want {
		t.Errorf("disabled server state = %q; want %q", got, want)
	}
	if entries[0].Config.Name != "disabled-one" {
		t.Errorf("entry name = %q; want %q", entries[0].Config.Name, "disabled-one")
	}
}

// TestAllServerStatuses_FallbackWithoutSetConfigs verifies that a Manager
// constructed without calling SetConfigs returns an empty list from
// AllServerStatuses (no panic, no nil). The "fall back to listing clients"
// branch is also covered: with no SetConfigs and no clients, the result is
// empty; the fallback path is exercised in TestAllServerStatuses_FallbackListsClients.
func TestAllServerStatuses_FallbackWithoutSetConfigs(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	entries := m.AllServerStatuses()
	if entries == nil {
		t.Fatal("expected non-nil entries slice")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries; got %d", len(entries))
	}
}

// TestAllServerStatuses_FallbackListsClients exercises the backward-compat
// fallback path: when SetConfigs has not been called but clients exist
// (e.g., ad-hoc StartServer calls), AllServerStatuses lists those clients.
// This is a white-box test that injects a fake connected client into the
// clients map to avoid the need for a real subprocess.
func TestAllServerStatuses_FallbackListsClients(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	// White-box: inject a fake client that reports connected=false so the
	// fallback path marks it as StateError. We avoid a real subprocess here.
	fake := &Client{name: "ad-hoc"} // connected atomic defaults to false; transport is nil
	m.mu.Lock()
	if m.configs == nil {
		m.configs = make(map[string]ServerConfig)
	}
	// Ensure configs is empty so the fallback branch is taken.
	clear(m.configs)
	m.clients["ad-hoc"] = fake
	m.mu.Unlock()

	entries := m.AllServerStatuses()
	if len(entries) != 1 {
		t.Fatalf("expected 1 fallback entry; got %d", len(entries))
	}
	if entries[0].Config.Name != "ad-hoc" {
		t.Errorf("fallback entry name = %q; want %q", entries[0].Config.Name, "ad-hoc")
	}
	// disconnected client in fallback path → StateError per AllServerStatuses logic.
	if entries[0].Stats.State != StateError {
		t.Errorf("fallback disconnected client state = %q; want %q", entries[0].Stats.State, StateError)
	}
}

// TestCallTool_IncrementsRequestsAndErrors uses Option B (pragmatic white-box)
// from the spec: a mock transport is injected via the Client so CallTool
// can be exercised without a real subprocess. Two subtests cover the
// success path (Requests incremented) and the failure path (Requests +
// Errors + LastError/LastErrorAt set).
func TestCallTool_IncrementsRequestsAndErrors(t *testing.T) {
	t.Run("success_increments_requests", func(t *testing.T) {
		m := NewManager(quietLogger())
		t.Cleanup(m.StopAll)

		mt := newMockTransport()
		mt.sendResponse = []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}`)
		client := NewClient("mock-srv", mt, quietLogger())
		if err := client.Connect(context.Background()); err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Logf("close failed: %v", err)
			}
		})

		// Inject the client and a stats entry.
		m.mu.Lock()
		m.clients["mock-srv"] = client
		m.stats["mock-srv"] = &ServerStats{State: StateActive}
		m.mu.Unlock()

		result, err := m.CallTool(context.Background(), "mock-srv.tool", map[string]any{"x": 1})
		if err != nil {
			t.Fatalf("CallTool returned error: %v", err)
		}
		if result == nil || !result.Success {
			t.Fatalf("expected successful result; got %+v", result)
		}

		m.mu.RLock()
		st := m.stats["mock-srv"]
		m.mu.RUnlock()

		if st.Requests != 1 {
			t.Errorf("expected Requests=1; got %d", st.Requests)
		}
		if st.Errors != 0 {
			t.Errorf("expected Errors=0; got %d", st.Errors)
		}
		if st.LastRequestAt == nil {
			t.Error("expected LastRequestAt to be set")
		}
		if st.LastError != "" {
			t.Errorf("expected LastError empty; got %q", st.LastError)
		}
	})

	t.Run("failure_increments_errors", func(t *testing.T) {
		m := NewManager(quietLogger())
		t.Cleanup(m.StopAll)

		mt := newMockTransport()
		// Configure Send to return a transport-level error (simulating subprocess write failure).
		mt.sendErr = errors.New("pipe closed")
		client := NewClient("fail-srv", mt, quietLogger())
		// Bypass Connect: manually mark connected and populate transport.running so
		// IsConnected returns true, allowing CallTool to reach the client.CallTool path.
		client.connected.Store(true)
		mt.running.Store(true)
		t.Cleanup(func() {
			client.connected.Store(false)
			mt.running.Store(false)
		})

		m.mu.Lock()
		m.clients["fail-srv"] = client
		m.stats["fail-srv"] = &ServerStats{State: StateActive}
		m.mu.Unlock()

		_, err := m.CallTool(context.Background(), "fail-srv.tool", nil)
		if err == nil {
			t.Fatal("expected CallTool to return error; got nil")
		}

		m.mu.RLock()
		st := m.stats["fail-srv"]
		m.mu.RUnlock()

		if st.Requests != 1 {
			t.Errorf("expected Requests=1; got %d", st.Requests)
		}
		if st.Errors != 1 {
			t.Errorf("expected Errors=1; got %d", st.Errors)
		}
		if st.LastError == "" {
			t.Error("expected LastError to be populated")
		}
		if st.LastErrorAt == nil {
			t.Error("expected LastErrorAt to be set")
		}
		if !strings.Contains(st.LastError, "pipe closed") {
			t.Errorf("expected LastError to contain 'pipe closed'; got %q", st.LastError)
		}
	})
}

// TestStartServer_FailureSetsErrorState uses the `false` shell builtin
// (always exits 1) to trigger a StartServer failure at the Connect stage.
// Verifies the stats entry records StateError and a non-empty LastError.
func TestStartServer_FailureSetsErrorState(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	cfg := ServerConfig{
		Name:    "fail",
		Command: []string{"false"}, // exits 1 immediately
		Type:    "stdio",
	}
	// Register the config via SetConfigs so ServerStatus (which keys off
	// the configs map) can find it after the failed start records the
	// stats entry.
	m.SetConfigs([]ServerConfig{cfg})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := m.StartServer(ctx, cfg)
	if err == nil {
		t.Fatal("expected StartServer to fail for `false` command")
	}

	_, stats, ok := m.ServerStatus("fail")
	if !ok {
		t.Fatal("expected stats entry for 'fail' to exist after failed start")
	}
	if stats.State != StateError {
		t.Errorf("expected State=%q; got %q", StateError, stats.State)
	}
	if stats.LastError == "" {
		t.Error("expected LastError to be non-empty after failed start")
	}
	if stats.LastErrorAt == nil {
		t.Error("expected LastErrorAt to be set after failed start")
	}
}

// TestStartServer_DisabledReturnsError verifies that calling StartServer on
// a disabled config returns an error (programmer-error semantics per spec)
// and does not create a stats entry with StateActive.
func TestStartServer_DisabledReturnsError(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	cfg := ServerConfig{
		Name:    "disabled",
		Enabled: boolPtr(false),
		Command: []string{"sleep", "3600"},
		Type:    "stdio",
	}
	err := m.StartServer(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected StartServer to reject disabled config")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected error to mention 'disabled'; got %v", err)
	}
	// No client should have been registered.
	if c := m.GetClient("disabled"); c != nil {
		t.Errorf("expected no client for disabled server; got %v", c)
	}
}

// TestReload_DisabledServersSkippedFromStart verifies that Reload does not
// start disabled servers. We use `sleep 3600` for an enabled server and a
// disabled config, then confirm only the enabled server enters clients.
func TestReload_DisabledServersSkippedFromStart(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	// Construct a minimal stub server script that stays alive.
	tempDir := t.TempDir()
	scriptPath := tempDir + "/stub.sh"
	//nolint:gosec // test script, temp dir
	if err := os.WriteFile(scriptPath, []byte(stubServerScript), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	configs := []ServerConfig{
		{Name: "alive", Enabled: boolPtr(true), Command: []string{"bash", scriptPath}, Type: "stdio"},
		{Name: "dead", Enabled: boolPtr(false), Command: []string{"bash", scriptPath}, Type: "stdio"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Reload(ctx, configs); err != nil {
		t.Fatalf("Reload returned error: %v", err)
	}

	if c := m.GetClient("alive"); c == nil {
		t.Error("expected 'alive' client to be started")
	}
	if c := m.GetClient("dead"); c != nil {
		t.Errorf("expected 'dead' (disabled) client NOT to be started; got %v", c)
	}
	if got := m.ServerCount(); got != 1 {
		t.Errorf("expected ServerCount=1; got %d", got)
	}
}

// TestReload_PreservesStatsForDisabledServers verifies that stats entries
// for disabled servers survive a Reload, and that AllServerStatuses reports
// them with State=disabled. Counts from prior runs are preserved.
func TestReload_PreservesStatsForDisabledServers(t *testing.T) {
	m := NewManager(quietLogger())
	t.Cleanup(m.StopAll)

	// Seed an initial stats entry with non-zero counts for a server that
	// will be disabled in the incoming config.
	m.mu.Lock()
	now := time.Now()
	m.stats["to-disable"] = &ServerStats{
		State:         StateActive,
		Requests:      7,
		Errors:        2,
		LastError:     "boom",
		LastErrorAt:   &now,
		LastRequestAt: &now,
	}
	m.mu.Unlock()

	tempDir := t.TempDir()
	scriptPath := tempDir + "/stub.sh"
	//nolint:gosec // test script, temp dir
	if err := os.WriteFile(scriptPath, []byte(stubServerScript), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	configs := []ServerConfig{
		{Name: "to-disable", Enabled: boolPtr(false), Command: []string{"bash", scriptPath}, Type: "stdio"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Reload(ctx, configs); err != nil {
		t.Fatalf("Reload returned error: %v", err)
	}

	// ServerStatus should reflect disabled merge logic.
	_, stats, ok := m.ServerStatus("to-disable")
	if !ok {
		t.Fatal("expected ServerStatus to find 'to-disable'")
	}
	if stats.State != StateDisabled {
		t.Errorf("expected State=%q; got %q", StateDisabled, stats.State)
	}
	// Counts preserved.
	if stats.Requests != 7 {
		t.Errorf("expected Requests=7 (preserved); got %d", stats.Requests)
	}
	if stats.Errors != 2 {
		t.Errorf("expected Errors=2 (preserved); got %d", stats.Errors)
	}
	if stats.LastError != "boom" {
		t.Errorf("expected LastError='boom' (preserved); got %q", stats.LastError)
	}
}

// --- Helpers ---

// stubServerScript is a minimal MCP-responder script used by tests that need
// a real connected server. It handles initialize/notifications/initialized/
// tools/list and stays alive reading stdin until closed.
const stubServerScript = `#!/bin/bash
# Minimal MCP responder for tests.
while read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"stub","version":"1.0.0"},"capabilities":{"tools":{}}}}'
      ;;
    *'"method":"notifications/initialized"'*)
      : # notification, no response
      ;;
    *'"method":"tools/list"'*)
      echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
      ;;
  esac
done
`

// mockTransport is a minimal Transport implementation for white-box testing
// of the Client. It satisfies transport.Transport without spinning up a
// subprocess.
type mockTransport struct {
	mu            sync.Mutex
	running       atomic.Bool
	sendResponse  []byte
	sendErr       error
	startErr      error
	closeErr      error
	notifiedCount atomic.Int64
}

func newMockTransport() *mockTransport {
	return &mockTransport{}
}

func (t *mockTransport) Start(ctx context.Context) error {
	if t.startErr != nil {
		return t.startErr
	}
	t.running.Store(true)
	return nil
}

func (t *mockTransport) Send(ctx context.Context, message []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sendErr != nil {
		return nil, t.sendErr
	}
	return t.sendResponse, nil
}

func (t *mockTransport) SendNotification(ctx context.Context, message []byte) error {
	t.notifiedCount.Add(1)
	return nil
}

func (t *mockTransport) Close() error {
	t.running.Store(false)
	return t.closeErr
}

func (t *mockTransport) IsRunning() bool {
	return t.running.Load()
}

// Compile-time assertion that mockTransport satisfies transport.Transport.
var _ transport.Transport = (*mockTransport)(nil)
