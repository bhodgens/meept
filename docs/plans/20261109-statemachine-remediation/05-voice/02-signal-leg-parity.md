# Signal Leg Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Voice findings 5 (fresh_session_per_call + day brief ignored on signal leg) + 6 (random-ccid effect key defeats retry idempotency).
- **Dependencies:** 00T-voice-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** findings 5, 6

## Goal

The signal leg honors the same session, brief, and idempotency contracts as
the phone/local legs.

## Context

- internal/signalleg/leg.go:302-312 bindCall calls OnCallStart WITHOUT freshSession variadic and with empty DayBrief{}
- Correct patterns: voice_wiring.go:298/385 (FreshSessionFor), :454-459 (local leg brief)
- internal/signalleg/leg.go:412 effectKey(employeeID, target, ccid) with per-attempt ccid; phone pattern: (employee, target, request_id) dial.go:231-260

## Verified Anchors

Re-grep at dispatch (BLOCKED-START: sibling voice wave may already touch
leg.go — check for SUPERSEDED first, per branch Notes).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-V2:
// bindCall resolves voice.FreshSessionFor(cfg, employeeID) and passes it; DayBrief flows from the same source as local leg (wiring passes brief into Place/Accept call chain).
// effectKey(employeeID, target, requestID) — requestID carried through Place; retries of the same request reuse the ledger key (rings once).
```

### What This Leaf Consumes

- Existing OnCallStart variadic + effects ledger Claim semantics.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Fresh session + brief | Modify: leg.go bindCall + Place/Accept signatures (or leg config struct) | gate TestSignalLegFreshSession GREEN | absent | GREEN: one call one session; brief injected | Given fresh_session_per_call=true; When signal call binds; Then OnCallStart receives fresh session + non-empty brief (same source as local) |
| T2 | Stable effect key | Modify: leg.go :412 + Place chain (requestID plumbed) | gate TestSignalLegEffectKeyStable GREEN | random ccid in key | GREEN: same request_id retry -> same key -> effects ledger dedup (no second ring) | Given Place(request R) twice; Then second claims existing receipt, no new ring |
| T3 | AGENTS.md seam note | Modify: internal/voice/AGENTS.md signal-leg section | grep | - | parity documented (contract now: signal leg honors fresh-session + brief + stable keys) | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] No signature break for other callers (grep Place/Accept call sites)
- [ ] internal/voice tests still green (wiring passes new args correctly)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Brief source identical to local leg (no second brief implementation)
- [ ] requestID plumbing preserves per-attempt ccid for call identity (only the LEDGER key changes)

## Notes

Careful separation: ccid stays the call/session identity; requestID is the
idempotency identity. Do not conflate them.
