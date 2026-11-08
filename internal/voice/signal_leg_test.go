package voice

// Gate tests for contract C4 (master.md, quoted verbatim): the registry
// gains the "signal" leg. Written BEFORE implementation (00T-gate-tests.md,
// T4). RED until leaf 11 lands LegSignal.
//
// Pinned rules (mirror of the CL6 local-leg pins in registry_leg_test.go):
//   - LegSignal constant = "signal"; zero value of Leg STAYS "phone".
//   - Target shape "signal:<uuid>"; NormalizeE164 NEVER runs on signal
//     targets (same rule as local:<label>, dial.go:624-633).
//   - ccid prefix "signal-" + pkg/id.Generate().
//   - One-live-per-target, one-live-per-employee, and the shared cap are
//     SHARED across legs (signal competes with phone/local for capacity).

import (
	"errors"
	"strings"
	"testing"
)

// TestLegSignalConstant pins the leg string exactly (the config matrix in
// docs/workflows/voice-sidecar.md extends per leg on this value).
func TestLegSignalConstant(t *testing.T) {
	if LegSignal != "signal" {
		t.Errorf("LegSignal = %q, want %q", LegSignal, "signal")
	}
	// Zero value stays phone: an Instance without an explicit leg must
	// never become a signal leg by accident.
	if LegPhone != "phone" {
		t.Errorf("LegPhone = %q, want %q (zero value contract)", LegPhone, "phone")
	}
}

// TestRegistrySignalLegTargetVerbatim pins the leaf's canonical Given/
// When/Then: a live signal call for uuid U; a second AcquireLeg on the
// SAME target is ErrDuplicateTarget; the stored target is VERBATIM
// "signal:<uuid>" (never E.164-mangled).
func TestRegistrySignalLegTargetVerbatim(t *testing.T) {
	reg := NewRegistry(4)

	const uuid = "99999999-8888-7777-6666-555555555555"
	target := "signal:" + uuid

	inst, err := reg.AcquireLeg("signal-test-1", "emp-1", target, LegSignal)
	if err != nil {
		t.Fatalf("signal AcquireLeg: %v", err)
	}
	if inst.Target != target {
		t.Errorf("signal instance Target = %q, want verbatim %q (NormalizeE164 must never touch signal targets, C4)",
			inst.Target, target)
	}
	if inst.Leg != LegSignal {
		t.Errorf("signal instance Leg = %q, want %q", inst.Leg, LegSignal)
	}
	if !strings.HasPrefix(inst.CallControlID, "signal-") {
		t.Errorf("signal ccid = %q, want \"signal-\" prefix (C4: ccid = signal- + id.Generate())", inst.CallControlID)
	}

	// The canonical duplicate-target refusal.
	if _, err := reg.AcquireLeg("signal-test-2", "emp-2", target, LegSignal); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("same signal-target AcquireLeg = %v, want ErrDuplicateTarget", err)
	}
}

// TestRegistrySignalLegNoMangling pins the no-E.164 rule negatively: a
// uuid-shaped target must round-trip byte-identically even though phone
// legs normalize. (A uuid is digits+letters; an E.164 pass-through would
// strip/mangle it.)
func TestRegistrySignalLegNoMangling(t *testing.T) {
	reg := NewRegistry(4)

	const target = "signal:abcdefab-cdef-abcd-efab-cdefabcdefab"
	inst, err := reg.AcquireLeg("signal-test-3", "emp-3", target, LegSignal)
	if err != nil {
		t.Fatalf("signal AcquireLeg: %v", err)
	}
	if inst.Target != target {
		t.Errorf("signal target mangled: got %q, want %q", inst.Target, target)
	}
}

// TestRegistrySignalSharesCapacity pins the shared rules: a signal leg
// consumes the same global cap and the same one-live-per-employee rule as
// phone/local (C4: "shared capacity rules").
func TestRegistrySignalSharesCapacity(t *testing.T) {
	// Cap 1: a live signal call blocks a phone acquire for another employee.
	reg := NewRegistry(1)
	if _, err := reg.AcquireLeg("signal-cap-1", "emp-1", "signal:aaaa1111-2222-3333-4444-555555555555", LegSignal); err != nil {
		t.Fatalf("seed signal AcquireLeg: %v", err)
	}
	if _, err := reg.AcquireLeg("phone-cap-2", "emp-2", "+15550000012", LegPhone); !errors.Is(err, ErrBusy) {
		t.Errorf("phone AcquireLeg over cap 1 with live signal call = %v, want ErrBusy", err)
	}

	// One live call per employee across legs: emp-1 busy on signal cannot
	// also acquire a phone leg (same target or not).
	reg2 := NewRegistry(4)
	if _, err := reg2.AcquireLeg("signal-emp-1", "emp-9", "signal:bbbb2222-2222-3333-4444-555555555555", LegSignal); err != nil {
		t.Fatalf("seed emp-9 signal leg: %v", err)
	}
	if _, err := reg2.AcquireLeg("phone-emp-1", "emp-9", "+15550000013", LegPhone); !errors.Is(err, ErrBusy) {
		t.Errorf("phone AcquireLeg for employee live on signal = %v, want ErrBusy (one live call per employee shared across legs)", err)
	}
}

// TestRegistrySignalReleaseFrees pins the release fan-out pre-condition:
// releasing the signal ccid frees the signal target for reacquire (the
// hangup path empties the registry slot exactly once).
func TestRegistrySignalReleaseFrees(t *testing.T) {
	reg := NewRegistry(4)

	const target = "signal:cccc3333-2222-3333-4444-555555555555"
	inst, err := reg.AcquireLeg("signal-rel-1", "emp-4", target, LegSignal)
	if err != nil {
		t.Fatalf("seed signal AcquireLeg: %v", err)
	}
	reg.Release(inst.CallControlID)

	if _, err := reg.AcquireLeg("signal-rel-2", "emp-5", target, LegSignal); err != nil {
		t.Errorf("signal AcquireLeg after Release = %v, want nil (release frees the target)", err)
	}
}
