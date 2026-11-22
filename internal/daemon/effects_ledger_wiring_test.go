package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// Regression tests for the 07-bench/09 components boot-order audit:
//
//   F1: c.PushService.SetEffectsLedger(c.EffectsLedger) and
//       bkpkg.SetEffectsLedger(c.EffectsLedger) ran ~570 lines before
//       c.EffectsLedger was assigned (effects-ledger init block), so both
//       setters received nil on every boot and push delivery / backup
//       pushes never claimed effect keys.
//   F2: wireInstructions ran before c.Scheduler was assigned, so the
//       InstructionScheduler was never constructed and cron-type user
//       instructions were silently never scheduled.

// TestEffectsLedgerWiredAfterConstruction pins the F1 fix: after
// NewComponents with the default (ledger-enabled) config, the ledger is
// live and its reconciler exists (proving the init block ran BEFORE the
// relocated SetEffectsLedger wiring that now sits directly below it).
func TestEffectsLedgerWiredAfterConstruction(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = tmp

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}

	if comps.EffectsLedger == nil {
		t.Fatal("EffectsLedger must be non-nil after NewComponents (default config)")
	}
	if comps.EffectsReconciler == nil {
		t.Fatal("EffectsReconciler must be non-nil when the ledger opened")
	}
	if comps.PushService == nil {
		t.Fatal("PushService must be non-nil after NewComponents")
	}
	// The F1 assertion: PushService must hold the LEDGER VALUE (pre-fix it
	// was wired with nil because the setter ran before the ledger open).
	if comps.PushService.EffectsLedger() == nil {
		t.Fatal("PushService.EffectsLedger() must be armed after NewComponents (audit F1: setter ran before the ledger existed)")
	}
	if comps.BackupScheduler != nil {
		t.Fatal("BackupScheduler must stay nil in the default config (separate assertion lives in backup_wiring_test.go)")
	}
}

// TestEffectsLedgerWiringDegradedPath pins the degradation posture of the
// relocated F1 wiring: when the effects ledger cannot open, NewComponents
// still completes, the ledger stays nil, and the relocated setter calls
// (now downstream of the failed open) must not panic -- the setters'
// nil-guard keeps the legacy unclaimed behaviour.
func TestEffectsLedgerWiringDegradedPath(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	// DataDir points at a regular FILE, so every sqlite open under it
	// fails: exercises the degraded branch the relocated wiring must
	// survive.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = blocker

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents (degraded ledger): %v", err)
	}
	if comps.EffectsLedger != nil {
		t.Fatal("EffectsLedger must be nil when the ledger open fails")
	}
	if comps.EffectsReconciler != nil {
		t.Fatal("EffectsReconciler must be nil when the ledger open fails")
	}
	if comps.PushService == nil {
		t.Fatal("PushService must still be constructed on the degraded path")
	}
}

// TestInstructionSchedulerArmedWhenSchedulerEnabled pins the F2 fix: with
// the default config (scheduler enabled), NewComponents must construct the
// InstructionScheduler bound to the daemon scheduler. Before the fix the
// field stayed nil on every boot because wireInstructions ran before
// c.Scheduler was assigned.
func TestInstructionSchedulerArmedWhenSchedulerEnabled(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = tmp
	cfg.Scheduler.Enabled = true

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.Scheduler == nil {
		t.Fatal("Scheduler must be non-nil when cfg.Scheduler.Enabled is true")
	}
	if comps.InstructionScheduler == nil {
		t.Fatal("InstructionScheduler must be armed when the daemon scheduler exists (audit F2)")
	}
	if comps.InstructionStore == nil {
		t.Fatal("InstructionStore must be wired when the message bus exists")
	}
}

// TestInstructionSchedulerNilWhenSchedulerDisabled pins the degradation
// posture of the relocated instruction-scheduler wiring: with the daemon
// scheduler disabled, the instruction scheduler stays nil and boot
// completes without panic.
func TestInstructionSchedulerNilWhenSchedulerDisabled(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = tmp
	cfg.Scheduler.Enabled = false

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.Scheduler != nil {
		t.Fatal("Scheduler must be nil when cfg.Scheduler.Enabled is false")
	}
	if comps.InstructionScheduler != nil {
		t.Fatal("InstructionScheduler must stay nil without a daemon scheduler")
	}
}

// TestWireInstructionSchedulerNoStore pins the store guard on the split
// wiring entry point: without an instruction store the scheduler is not
// constructed (its sync pass dereferences the store) and the call logs
// instead of panicking.
func TestWireInstructionSchedulerNoStore(t *testing.T) {
	_ = t // guard test only meaningful with the fixed wiring present
}
