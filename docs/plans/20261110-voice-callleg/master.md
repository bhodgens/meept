# Voice CallLeg Interface - Implementation Orchestrator

> **For the executing agent:** orchestrator. Dispatch, review, re-dispatch, track. Do NOT implement.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 leaves
- **Scope:** DESIGN.md (../20261110-voice-callleg-design.md, outside the tree dir for scanner reasons) is the accepted design, unblocked 2026-11-10 by the voice-deadair-link merge (all sibling trees now on main). Extract one CallLeg contract + per-leg adapters; Manager terminal fan-out (releaseOnce) stays UNTOUCHED — verified-clean 20261109.

## Goal

Channel parity by construction: phone/local/signal each become adapters
behind one Place/Accept/Hangup/Events interface with unified CallEvent
shape, so a fourth carrier inherits correct behavior instead of copying
whichever leg it likes. Adaptive: adapters wrap existing legs; callers
migrate to the interface only where already being touched; no big-bang
rewrite of HandleCallEvent paths.

## Architecture

callleg.go defines the contract; leg adapters hold their existing structs
(behavior-preserving wrappers); CallEvent maps each leg's observable
signals (webhook event types, signal-cli events, local frames) onto one
enum. Registry/cap/one-live rules stay shared in dial.go (unchanged).
End frame + release fan-out unchanged (20261109 leaves).

## Interface Contracts

### C-B1: CallLeg (exact shape in DESIGN.md; events enum:
ringing, connected, reconnecting, ended(reason), errored(reason))

## Child Index

| # | Document | Type | Deps | Est | Group |
|---|----------|------|------|-----|-------|
| 00T | 00T-gate-tests.md | leaf(gate) | none | 35K | A |
| 01 | 01-interface-adapters.md | leaf | 00T | 60K | B |
| 02 | 02-event-unification.md | leaf | 01 | 55K | B (serialize after 01: same files) |
| 03 | 03-caller-migration.md | leaf | 02 | 50K | B (serialize) |

## Dispatch Protocol

Tests-first; all leaves serialize on internal/voice (single-package
refactor); run one at a time in this worktree.

## Review Checklist

- [ ] releaseOnce fan-out zero-diff (frozen invariant)
- [ ] voice AGENTS.md frame/hold invariants unchanged except added lines
- [ ] all 20261109 voice gates (terminal_gate, leg_gate, webhook_gate) zero-diff + green
- [ ] adapters add no behavior (diff-read every wrapper)
- [ ] no new bus topics unless graphs regen says otherwise

## Coding Conventions

Repo AGENTS.md + internal/voice/AGENTS.md is contract source; -p 2;
same-commit doc updates.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | design amendment 5 (20261109 master) | 01-03 | parity gates + adapter pins |
| R2 | parity-by-construction (audit DUPLICATED) | 02 | TestCallLegEventsParityAcrossLegs |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests | TESTS_VALIDATED | 1 | committed b8cd81f6; 4 voice + 3 signal arms (cycle-forced: signalleg imports voice); parent-verified RED=symbols only, frozen gates zero-diff |
| 01-interface-adapters | IN_PROGRESS | 0 | dispatched (event mechanism constrained: must NOT steal single-occupancy release hook) |
| 02-event-unification | PENDING | 0 | |
| 03-caller-migration | PENDING | 0 | |

## Integration Test Plan

go test -p 2 ./internal/voice/... ./internal/signalleg/... ./internal/daemon/ -run 'CallLeg|Leg|Hold|EndFrame'; full pkg; -race voice; graphs check.

## Test Gate

| # | Feature | Source | Unit tests | Status |
|---|---------|--------|-----------|--------|
| F1 | CallLeg contract + adapters | 01 | TestCallLegAdaptersCompileShape + per-leg Place/Accept/Hangup pins on existing fakes | PENDING |
| F2 | event unification | 02 | TestCallLegEventsParityAcrossLegs | PENDING |
| F3 | caller migration | 03 | existing voice suites green via interface | PENDING |

## Open Questions

- Q-B1: migrate the builtin voice_call tool to CallLeg in this tree or
  leave on Dialer? Default: leave (tool already parity-fixed 20261109);
  migration = follow-on when a 4th carrier actually lands.

## Frozen Documents

Freeze at dispatch; addenda dated.

## Notes

Pre-flight surveys DONE 2026-11-10: deadair link coordinator = adapter-
internal (phone adapter wraps it, no leak — link.go is phone-leg detail);
calls.db schema unchanged; C1 frame protocol untouched (end frame seam
20261109 stays in releaseCall, OUTSIDE the adapter).
