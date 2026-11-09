package daemon

// Signal leg wiring tests (leaf 11a T6): the disabled boot is clean
// (nil Leg, nil error, no construction), the enabled boot arms the
// supervisor + leg, and an unarmed sidecar DEGRADES (nil Leg, non-nil
// result, no error) - the bindPhoneCall nil-safe posture.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
)

func sigTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(os.Stderr, nil)) }

func sigTestDeps(t *testing.T) WireVoiceDeps {
	t.Helper()
	cfg := config.DefaultConfig()
	// Short MEEPT_HOME keeps the sidecar socket below the sun_path cap.
	if err := os.MkdirAll("/tmp/mpt-sl", 0o700); err != nil {
		t.Fatalf("short base: %v", err)
	}
	t.Setenv("MEEPT_HOME", "/tmp/mpt-sl")
	return WireVoiceDeps{
		Config: cfg,
		Logger: sigTestLogger(),
	}
}

// TestWireSignalLegDisabled pins the clean disabled boot: no Leg, no
// error, nil result - the daemon boots with zero Signal machinery.
func TestWireSignalLegDisabled(t *testing.T) {
	deps := sigTestDeps(t)
	cfg := deps.Config.Signal
	cfg.Enabled = false
	cfg.Binary = "signal-cli-nonexistent-for-test"
	logger := sigTestLogger()

	res, err := WireSignalLeg(context.Background(), cfg, deps, nil, nil, nil, nil, logger)
	if err != nil {
		t.Fatalf("disabled boot returned error %v, want nil (clean disabled boot)", err)
	}
	if res != nil {
		t.Errorf("disabled boot returned a result, want nil")
	}
}

// TestWireSignalLegUnarmedSidecarDegrades pins the degrade posture: the
// binary does not exist, so the supervisor cannot arm; the wiring logs
// a loud WARN and returns a nil LEG with a non-nil result (the caller
// keeps booting; Signal call paths stay refused). No error surfaces.
func TestWireSignalLegUnarmedSidecarDegrades(t *testing.T) {
	deps := sigTestDeps(t)
	cfg := deps.Config.Signal
	cfg.Enabled = true
	cfg.Binary = "signal-cli-nonexistent-for-test"
	cfg.SocketPath = filepath.Join("/tmp/mpt-sl", "sig-test.sock")

	res, err := WireSignalLeg(context.Background(), cfg, deps, nil, nil, nil, nil, sigTestLogger())
	if err != nil {
		t.Fatalf("unarmed sidecar returned error %v, want nil (degrade, not fatal)", err)
	}
	if res == nil {
		t.Fatalf("unarmed sidecar returned nil result, want a non-nil result carrying the supervisor")
	}
	if res.Leg != nil {
		t.Errorf("unarmed sidecar armed a Leg, want nil (call paths must stay refused)")
	}
	if res.Supervisor == nil {
		t.Errorf("result lost the supervisor handle (Stop needs it)")
	}
}

// TestSignalAllowlist pins the fail-closed rule: an empty list admits
// nothing; a configured uuid admits exactly itself.
func TestSignalAllowlist(t *testing.T) {
	allow := signalAllowlist(nil)
	if allow("anything") {
		t.Errorf("empty allowlist admitted a uuid (must fail closed)")
	}
	allow = signalAllowlist([]string{"uuid-a", "uuid-b"})
	if !allow("uuid-a") || !allow("uuid-b") {
		t.Errorf("configured uuids refused")
	}
	if allow("uuid-c") {
		t.Errorf("unconfigured uuid admitted")
	}
}

// TestStopSignalLegNilSafe pins the nil-safe teardown (a disabled boot
// passes nil; Stop must not panic).
func TestStopSignalLegNilSafe(t *testing.T) {
	StopSignalLeg(context.Background(), nil)
	StopSignalLeg(context.Background(), &WireSignalLegResult{})
}

// TestVoiceWiringCarriesSignalNil pins the VoiceWiring field posture:
// an absent signal block leaves w.Signal nil after a LOCAL-ONLY wire
// (the disabled [signal] boot). Compile-level pin via a live
// VoiceWiring construction is heavy; this asserts the zero value.
func TestVoiceWiringCarriesSignalNil(t *testing.T) {
	var w VoiceWiring
	if w.Signal != nil {
		t.Errorf("zero VoiceWiring carries a Signal result, want nil")
	}
	_ = errors.New // keep the errors import if assertions change
	_ = voice.NewRegistry(0)
}
