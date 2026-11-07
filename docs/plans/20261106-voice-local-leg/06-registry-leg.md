# Registry leg awareness - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.

## Meta

- **Parent:** master.md
- **Scope:** Add a Leg field to the C8 registry Instance, normalize target
  only for phone legs, and make duplicate/cap semantics leg-correct.
- **Dependencies:** none (wave A)
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voice/dial.go (Instance/Acquire
  region), internal/voice/registry_test.go additions, internal/daemon/
  voice_wiring_test.go (fake registry call sites if they compile-break)

## Goal

A local-leg call and a phone call to the same employee contend for the
ONE-live-call-per-employee slot (that is intended: one employee, one
conversation at a time), but the local target string "local:host" must
never be mangled by NormalizeE164, and telemetry must know which leg an
instance is.

## Context

Key file: internal/voice/dial.go - Instance struct (~line 556), Registry
interface, CallRegistry.Acquire (normalizes target with NormalizeE164
today; audit 2026-11-06 M7 comment explains why). The C8 rules: cap, one
per employee, one per target.

## Interface Contracts (From Parent)

CL6 (verbatim): `Instance struct { CallControlID, EmployeeID, Target, Leg string }`
with Leg values "phone" | "local"; the zero value of Leg means "phone" (so
existing composite literals stay valid — if vet flags composite literals,
add Leg explicitly in test fakes rather than changing semantics).
`Acquire(ccid, employeeID, target, leg string) (Instance, error)` - add a
method on CallRegistry with this signature; KEEP the old 3-arg Acquire as a
wrapper that calls the 4-arg with leg="phone" so Registry interface
implementors (voice.Registry consumers, test fakes) do not all break at
once. Target normalization runs only when leg == "phone".

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: Acquire leg="local" with target "local:macbook" stores it verbatim; a second local acquire to the same target is ErrDuplicateTarget | internal/voice/registry_leg_test.go | go test -p 2 ./internal/voice/ -run TestRegistryLeg | no Leg field / 4-arg Acquire | PASS |
| T2 | Test: phone acquire of "+15551234567" still normalizes; 3-arg Acquire still behaves as today | same | -run TestRegistryPhoneLegUnchanged (adapt existing) | - | PASS |
| T3 | Implement per CL6 (Instance.Leg, 4-arg Acquire, 3-arg wrapper, leg-gated normalize) | dial.go | T1/T2 | - | PASS |
| T4 | Registry.Live() consumers tolerate the new field (compile sweep) | - | go build ./... && go test -p 2 ./internal/voice/ ./internal/daemon/ -run 'Voice|Registry' | - | PASS |

## Interface Contract (What This Leaf Exposes)

Instance.Leg, CallRegistry.AcquireLeg (or 4-arg name per CL6), the
3-arg compatibility wrapper, leg constants LegPhone="phone",
LegLocal="local".

## Self-Verification Checklist

- [ ] All existing registry tests pass with zero semantic edits
- [ ] Employee-one-live and cap rules fire for local legs too
- [ ] ASCII only

## Review Checklist

- [ ] NormalizeE164 never runs on local targets
- [ ] 3-arg Acquire wrapper keeps old tests byte-identical in behavior
- [ ] Live() docs updated to mention Leg
