# Caller Migration (Adaptive) - Implementation Leaf

> **For the implementing agent:** TDD. Do NOT commit. No read_file. NEVER edit gates.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/signalleg/voiceattempt.go (contact-chain caller) → CallLeg; internal/daemon wiring comments. NO builtin voice_call tool migration (Q-B1 default: leave on Dialer).
- **Dependencies:** 02-event-unification.md
- **Estimated Context:** 50K

## Goal

ONE real caller moves onto CallLeg to prove the interface fits production:
the signal contact-chain attempt uses signalAdapter.Place + Events()
instead of raw leg + per-attempt bookkeeping, with identical observable
behavior (stable effect keys from 20261109 leaf 02 must not regress).

## Contracts (From Parent)

Consumes C-B1 fully (leaf 02 state).

## Tasks

| Task ID | Objective | Files | Verify | PASS |
|---------|-----------|-------|--------|------|
| T1 | migrate chain caller | internal/signalleg/voiceattempt.go (+ small constructor plumbing) | existing chain/signalleg tests GREEN unchanged + a new pin that chain uses the adapter (grep/behavior) | identical outcomes |
| T2 | DESIGN.md status update: item 3 survey answered, interface validated by production caller | docs/plans/20261110-voice-callleg/DESIGN.md | - | marked |

## Self-Verification

- [ ] full signalleg/voice/daemon(voice) suites green; all CallLeg gates green; frozen gates zero-diff
- [ ] build green

**DO NOT COMMIT.**

## Review Checklist

- [ ] no behavior deltas beyond the indirection (diff-read against pre-migration call sequence)

## Notes

If plumbing the adapter into the chain reaches outside scope, implement
leaf-01-style accessor in signalleg and note; orchestrator decides merge
order.
