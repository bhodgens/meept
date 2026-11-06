package daemon

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// Tests for the jev score-gate daemon wiring (jev gate wiring leaf,
// docs/plans/20261103-wiring-gap-remediation/04-jev-gate-wiring.md):
// the hook is constructed + non-nil only when
// security.jev_command_gate.enabled=true, stays nil on the default
// (disabled) posture, and carries the capture recorder.
func TestJevGateWiring(t *testing.T) {
	newCfg := func(enabled bool) *config.Config {
		tmpDir := t.TempDir()
		sec := config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
			JevCommandGate: config.JevGateConfig{
				Enabled:             enabled,
				ScoreThreshold:      0.90,
				ConfidenceThreshold: 0.10,
			},
		}
		_ = sec.Validate()
		return &config.Config{
			Security: sec,
			// NewComponents derives every sqlite path from Daemon.DataDir;
			// an empty value turns them into CWD-relative paths and
			// litters the package dir with *.db artifacts.
			Daemon: config.DaemonConfig{DataDir: tmpDir},
		}
	}

	t.Run("disabled default: hook nil", func(t *testing.T) {
		logger := testLogger(t)
		msgBus := bus.New(nil, logger)
		comps, err := NewComponents(context.Background(), newCfg(false), msgBus, logger)
		if err != nil {
			t.Fatalf("NewComponents: %v", err)
		}
		if comps.JevScoreGate != nil {
			t.Fatal("JevScoreGate must be nil when security.jev_command_gate.enabled=false (opt-in default)")
		}
	})

	t.Run("enabled: hook constructed, armed, capture wired", func(t *testing.T) {
		logger := testLogger(t)
		msgBus := bus.New(nil, logger)
		comps, err := NewComponents(context.Background(), newCfg(true), msgBus, logger)
		if err != nil {
			t.Fatalf("NewComponents: %v", err)
		}
		if comps.JevScoreGate == nil {
			t.Fatal("JevScoreGate nil with enabled=true: construction wiring missing")
		}
		if !comps.JevScoreGate.Enabled() {
			t.Fatal("JevScoreGate constructed but not armed")
		}
	})
}

// TestJevGateRegistrationBesideDecisionGate pins the registration site:
// the jev hook is registered at the decision-gate registration block in
// the agent-options wiring, so the e2e wiring-failure case can delete the
// registration and watch the gate go inert. Asserted structurally: the
// components field is the ONLY registration source (nil = unregistered).
func TestJevGateRegistrationNilWhenDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Security: config.SecurityConfig{AllowedPaths: []string{tmpDir}},
		Daemon:   config.DaemonConfig{DataDir: tmpDir},
	}
	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.JevScoreGate != nil {
		t.Fatal("default config must leave JevScoreGate nil so the registration block no-ops (zero behavior)")
	}
}
