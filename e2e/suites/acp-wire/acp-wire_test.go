//go:build e2e

// Suite acp-wire: the ACP (Agent Client Protocol) wire surface — the
// disabled-by-default posture (no subprocess on call) and the full wire
// against a REAL stub agent subprocess: catalog load, handshake, send,
// response, plus the not-found/disabled error paths.
package acpwire

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
)

// stubAgentBin builds the fake ACP agent from internal/acp testdata —
// the same subprocess the package's own tests use, exercised here from
// OUTSIDE the package over its exported surface.
func stubAgentBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	cmd.Dir = filepath.Join(repoRoot(), "internal", "acp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

// repoRoot resolves the module root relative to this file's location.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// TestACPWire_DisabledByDefault covers acp-wire-01: with [acp] enabled
// unset (the default), GetOrCreate returns the disabled sentinel and —
// the core safety property — NO subprocess is spawned even when a
// command is configured in the catalog.
func TestACPWire_DisabledByDefault(t *testing.T) {
	// Build a stub agent we can PROVE never ran: its argv points at a
	// sentinel file that would be created on spawn.
	sentinel := filepath.Join(t.TempDir(), "spawned.marker")
	stub := filepath.Join(t.TempDir(), "noop-agent.sh")
	script := "#!/bin/sh\ntouch " + sentinel + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{{
		ID:      "e2e-agent",
		Command: []string{stub},
		Enabled: true,
	}}}
	// ACPConfig zero value: Enabled=false — the default posture.
	mgr := acp.NewManager(config.ACPConfig{}, catalog)

	if mgr.Enabled() {
		t.Fatal("ACP must be disabled by default")
	}
	// The catalog snapshot still lists entries (surface introspection),
	// but GetOrCreate refuses before any lookup happens.

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := mgr.GetOrCreate(ctx, "e2e-agent", t.TempDir())
	if sess != nil {
		_ = sess.Close()
		t.Fatal("disabled manager returned a live session")
	}
	if !errors.Is(err, acp.ErrDisabled) {
		t.Fatalf("GetOrCreate error = %v, want ErrDisabled", err)
	}
	// The subprocess proof: the sentinel file was never created.
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("a subprocess WAS spawned while ACP is disabled (sentinel: %v)", statErr)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("disabled manager reports live sessions: %+v", live)
	}
	// Stop/StopAll are no-ops while disabled (no panic, no error surface).
	if err := mgr.Stop("e2e-agent"); err != nil {
		t.Fatalf("disabled Stop errored: %v", err)
	}
	mgr.StopAll()
}

// TestACPWire_CatalogStubAgentHandshakeSendErrors covers acp-wire-02:
// with ACP enabled, the catalog loads, the stub agent handshakes to
// ready, send round-trips the response, and the not-found/disabled agent
// errors are distinct.
func TestACPWire_CatalogStubAgentHandshakeSendErrors(t *testing.T) {
	bin := stubAgentBin(t)
	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
		{ID: "echo-agent", Command: []string{bin, "-mode", "echo"}, Enabled: true},
		{ID: "off-agent", Command: []string{bin, "-mode", "echo"}, Enabled: false},
	}}
	mgr := acp.NewManager(config.ACPConfig{
		Enabled:        true,
		DialTimeout:    30,
		CallTimeout:    20,
		MaxAgents:      8,
		PermissionMode: "permissive",
	}, catalog)

	if !mgr.Enabled() {
		t.Fatal("manager must report enabled")
	}
	agents := mgr.Agents()
	if len(agents) != 2 {
		t.Fatalf("catalog snapshot = %d agents, want 2", len(agents))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	workdir := t.TempDir()

	// Distinct error paths first: unknown id vs disabled entry.
	if _, err := mgr.GetOrCreate(ctx, "no-such-agent", workdir); !errors.Is(err, acp.ErrAgentNotFound) {
		t.Fatalf("unknown agent = %v, want ErrAgentNotFound", err)
	}
	if _, err := mgr.GetOrCreate(ctx, "off-agent", workdir); !errors.Is(err, acp.ErrAgentDisabled) {
		t.Fatalf("disabled agent = %v, want ErrAgentDisabled", err)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("failed lookups must not leave sessions: %+v", live)
	}

	// The happy wire: handshake → ready → send → response.
	sess, err := mgr.GetOrCreate(ctx, "echo-agent", workdir)
	if err != nil {
		t.Fatalf("GetOrCreate echo-agent: %v", err)
	}
	if sess.State() != acp.StateReady {
		t.Fatalf("post-handshake state = %v, want ready", sess.State())
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sendCancel()
	reply, err := sess.Send(sendCtx, "hello over the acp wire")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "hello over the acp wire" {
		t.Fatalf("echo reply = %q", reply)
	}

	// GetOrCreate returns the SAME live session (no second subprocess).
	again, err := mgr.GetOrCreate(ctx, "echo-agent", workdir)
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if again != sess {
		t.Fatal("GetOrCreate minted a second session for a live agent")
	}
	if live := mgr.LiveSessions(); len(live) != 1 || live["echo-agent"] != acp.StateReady {
		t.Fatalf("LiveSessions = %+v, want echo-agent ready", live)
	}

	// Stop closes the session and drops it from the registry.
	if err := mgr.Stop("echo-agent"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("sessions after Stop: %+v", live)
	}
	// StopAll idempotent.
	mgr.StopAll()
}
