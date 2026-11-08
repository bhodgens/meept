package voice

// Gate tests for CL6 (master.md, quoted verbatim): Instance gains Leg
// ("phone"|"local", zero value = "phone"); Acquire normalizes the target
// ONLY for the phone leg (local targets contain colons; NormalizeE164 must
// never mangle "local:..."); employee-one-live and cap rules are shared
// across legs. The 3-arg Acquire remains as the phone-leg compatibility
// wrapper so Registry implementors do not all break at once.

import (
	"errors"
	"testing"
)

// TestRegistryLeg pins the local-leg semantics: the target string is
// stored VERBATIM (no normalization), a second live call to the same local
// target is ErrDuplicateTarget, and the shared cap/employee rules fire for
// local legs exactly as for phone legs.
func TestRegistryLeg(t *testing.T) {
	reg := NewRegistry(4)

	inst, err := reg.AcquireLeg("cc-local-1", "emp-1", "local:macbook", LegLocal)
	if err != nil {
		t.Fatalf("local AcquireLeg: %v", err)
	}
	if inst.Target != "local:macbook" {
		t.Errorf("local instance Target = %q, want verbatim %q (NormalizeE164 must never touch local targets)", inst.Target, "local:macbook")
	}
	if inst.Leg != LegLocal {
		t.Errorf("local instance Leg = %q, want %q", inst.Leg, LegLocal)
	}
	if inst.CallControlID != "cc-local-1" || inst.EmployeeID != "emp-1" {
		t.Errorf("local instance = %+v, want (cc-local-1, emp-1, local:macbook, local)", inst)
	}

	// Second live call to the same local target: ErrDuplicateTarget.
	if _, err := reg.AcquireLeg("cc-local-2", "emp-2", "local:macbook", LegLocal); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("same local-target AcquireLeg = %v, want ErrDuplicateTarget", err)
	}

	// A phone leg to a DIFFERENT target is refused for the employee even
	// though the legs differ: one live call per employee is shared.
	if _, err := reg.AcquireLeg("cc-phone-x", "emp-1", "5551230001", LegPhone); !errors.Is(err, ErrBusy) {
		t.Errorf("phone AcquireLeg for already-live employee = %v, want ErrBusy (employee rule shared across legs)", err)
	}

	// A second local leg for a different employee at a different target
	// succeeds (cap 4, distinct target).
	if _, err := reg.AcquireLeg("cc-local-3", "emp-2", "local:imac", LegLocal); err != nil {
		t.Errorf("distinct local AcquireLeg = %v, want nil", err)
	}

	// Release frees the local target: reacquire succeeds.
	reg.Release("cc-local-1")
	if _, err := reg.AcquireLeg("cc-local-4", "emp-3", "local:macbook", LegLocal); err != nil {
		t.Errorf("local AcquireLeg after Release = %v, want nil (release frees the local target)", err)
	}
}

// TestRegistryLegSharedCap pins the cap rule across legs: a local registry
// at cap 1 refuses the second acquire (either leg) with ErrBusy.
func TestRegistryLegSharedCap(t *testing.T) {
	reg := NewRegistry(1)
	if _, err := reg.AcquireLeg("cc-c1", "emp-1", "local:macbook", LegLocal); err != nil {
		t.Fatalf("seed local AcquireLeg: %v", err)
	}
	if _, err := reg.AcquireLeg("cc-c2", "emp-2", "+155****0002", LegPhone); !errors.Is(err, ErrBusy) {
		t.Errorf("phone AcquireLeg over cap 1 = %v, want ErrBusy", err)
	}
	if _, err := reg.AcquireLeg("cc-c3", "emp-2", "local:imac", LegLocal); !errors.Is(err, ErrBusy) {
		t.Errorf("local AcquireLeg over cap 1 = %v, want ErrBusy", err)
	}
}

// TestRegistryPhoneLegUnchanged pins the phone path: the target still
// normalizes, the stored Leg is "phone", and the 3-arg Acquire wrapper
// behaves byte-identically with the 4-arg phone-leg call.
func TestRegistryPhoneLegUnchanged(t *testing.T) {
	reg := NewRegistry(4)

	// Bare 10-digit target normalizes to +1 form at the registry boundary.
	inst, err := reg.Acquire("cc-ph-1", "emp-1", "555 123 0001")
	if err != nil {
		t.Fatalf("3-arg Acquire: %v", err)
	}
	if inst.Target != "+15551230001" {
		t.Errorf("3-arg Acquire Target = %q, want normalized %q", inst.Target, "+15551230001")
	}
	if inst.Leg != LegPhone {
		t.Errorf("3-arg Acquire Leg = %q, want %q (zero value = phone)", inst.Leg, LegPhone)
	}

	// Normalized shape collides: ErrDuplicateTarget by canonical number.
	if _, err := reg.AcquireLeg("cc-ph-2", "emp-2", "+15551230001", LegPhone); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("phone AcquireLeg same canonical target = %v, want ErrDuplicateTarget", err)
	}

	// The 4-arg phone-leg call matches the 3-arg wrapper: same
	// normalization, same Leg.
	reg2 := NewRegistry(4)
	inst4, err := reg2.AcquireLeg("cc-ph-3", "emp-1", "555 123 0001", LegPhone)
	if err != nil {
		t.Fatalf("4-arg phone AcquireLeg: %v", err)
	}
	if inst4.Target != inst.Target || inst4.Leg != inst.Leg {
		t.Errorf("4-arg phone AcquireLeg = %+v, want identical to 3-arg wrapper %+v", inst4, inst)
	}

	// An empty leg on the 4-arg call means phone (zero value), so it
	// normalizes too.
	reg3 := NewRegistry(4)
	instZero, err := reg3.AcquireLeg("cc-ph-4", "emp-1", "555 123 0001", "")
	if err != nil {
		t.Fatalf("zero-leg AcquireLeg: %v", err)
	}
	if instZero.Target != "+15551230001" || instZero.Leg != LegPhone {
		t.Errorf("zero-leg AcquireLeg = %+v, want normalized phone instance", instZero)
	}

	// A local target on a PHONE leg is not exempted: normalization applies
	// (this documents the gating is by leg, not by target shape).
	reg4 := NewRegistry(4)
	instM, err := reg4.AcquireLeg("cc-ph-5", "emp-1", "local:macbook", LegPhone)
	if err != nil {
		t.Fatalf("phone-leg local-shaped AcquireLeg: %v", err)
	}
	if instM.Target != "local:macbook" {
		t.Errorf("phone-leg local-shaped Target = %q; NormalizeE164 passes non-numerics through unchanged - got mangled shape", instM.Target)
	}
}
