# CallLeg Interface + Adapters - Implementation Leaf

> **For the implementing agent:** Implement with TDD. Gate file internal/voice/callleg_gate_test.go is frozen (read first; it pins names). Do NOT commit. No read_file on existing sources. Write once. NEVER edit any *_gate_test.go.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/voice: new callleg.go; adapters may live in callleg_adapters.go or per-leg files; minimal hooks added to phone (dial/manager), local (local.go), signal (internal/signalleg — cross-pkg OK, keep small).
- **Dependencies:** 00T gate
- **Estimated Context:** 60K

## Goal

Make TestCallLegAdaptersExist/PlacePin/HangupIdempotentPin GREEN:
CallLeg interface + three behavior-preserving wrapper adapters. NO caller
migration in this leaf (03 does that); NO releaseOnce/fan-out changes
(frozen).

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
C-B1 (DESIGN.md verbatim):
type CallLeg interface { Place(ctx, PlaceRequest) (CallHandle, error); Accept(ctx, id string) (CallHandle, error); Hangup(ctx, h CallHandle, reason string) error; Events() <-chan CallEvent }
CallEvent kinds: Ringing/Connected/Reconnecting/Ended/Errored (+Reason string).
phoneAdapter{d *Dialer or mgr}, localAdapter{*LocalLeg}, signalAdapter{*signalleg.Leg or Deps seam}.
```

### What This Leaf Consumes
- Existing per-leg public APIs only.

## Tasks

| Task ID | Objective | Files | Verify | PASS criteria | Given/When/Then |
|---------|-----------|-------|--------|---------------|-----------------|
| T1 | interface + event enum + CallHandle | Create: internal/voice/callleg.go | vet | compiles | - |
| T2 | adapters (thin; behavior passthrough) | Create/modify: internal/voice/callleg_phone.go, callleg_local.go, callleg_signal.go (signal adapter may import internal/signalleg) | CallLeg gates 1-3 GREEN (test 4 may stay RED until 02 if events not unified) | per gate | Given each fake rig; When Place/Accept/Hangup via adapter; Then same outcome as direct leg call |
| T3 | Events channel: per-leg adapter emits today's natural signals; parity across legs REQUIRED here or in 02 — follow gate test 4 | same | T4 gate GREEN | Ended parity |

## Self-Verification Checklist

- [ ] gates 1-4 status reported honestly
- [ ] full ./internal/voice/ + ./internal/signalleg/ suites green; frozen gates zero-diff
- [ ] go build ./... green; -race sample

**DO NOT COMMIT.**

## Completion Report Format

Files; scoreboard incl. which gates stay RED for 02; adapter-vs-rewire notes; deviations.

## Review Checklist (For Review Agent)

- [ ] adapters ADD NO behavior (each method a call-through; event mapping is the only new logic)

## Notes

Accept() on legs with no inbound concept (local): return ErrNoInbound —
pin in report; gate test 2 covers per-leg.
