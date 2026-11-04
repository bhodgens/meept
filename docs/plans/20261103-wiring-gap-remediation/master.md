# Wiring-Gap Remediation - 20261031 Forest Follow-ups

## Goal

Close the 8 wiring gaps the 20261031 forest's e2e suites and surveys
flagged. Every gap is verified live at HEAD 72c24930 (survey
deleg_786d8798, 2026-11-03). Nothing is a stub: every leaf wires to a
real surface and carries a wiring-failure proof test (delete the wiring,
test fails).

## Traceability (Req -> leaf)

| Req | Source (suite docblock / commit body, verbatim) | Leaf | Verified by |
|---|---|---|---|
| R1 | "ConfigSnapshot omits contextAssemblerCfg/tierProvider/piiRestore" | 01 | seam-propagation clone test |
| R2 | "RunTransformContext has ZERO production callers" | 02 | obfuscated-content reaches provider test |
| R3 | "managerCandidates passes time.Time{} ... memory.tiers schema knob absent ... no producer" | 03 | cutoff/demote/producer/wiring tests |
| R4 | "ScoreGateHook never constructed" | 04 | daemon wiring e2e case |
| R5 | "session_tool_calls has NO runtime writer" | 05 | rows keyed to assistant message_id test |
| R6 | "hop spans export with empty ParentSpanID" | 06 | ParentSpanID == turnSpanID wiring unit |
| R7 | "Deobfuscate is process-wide not session-scoped ... collision" | 07 | collision + cross-session tests |
| R8 | "tier floor doc-vs-code" | 00 | existing pinned e2e tier_policy_test.go:47-54 |

## Dispatch Protocol

Per-survey dependency map (survey deleg_786d8798, verified at HEAD 72c24930). Dispatch waves in this order; re-grep sibling state before each wave.

- Wave A (parallel, disjoint files): 00 (doc-only), 04 (components.go +
  schema.go jev keys), 05 (handler.go + session store).
- Wave B: 02 (loop.go transform call; needs nothing) then 07 (depends on
  02 for end-to-end proof).
- Wave C: 01 (ConfigSnapshot closures; pairs with 02/03 seams being
  load-bearing) + 06 (TurnTerminalEvent additive field).
- Wave D: 03 (largest; schema knob -> adapter -> call site; SEQUENCE
  against memory second-brain leaf 03-agent-recall-wiring, which touches
  loop recall seams - do not merge blindly on top).
- Final: integration gates + completion report.

Concurrent-sibling rule: memory second-brain leaf 03 is queued by another
session (commit 72c24930). Before dispatching wave D, re-grep its
dispatch state; if it has landed, reconcile item 3 against what it wired.

## Interface Contracts

- C1 (leaf 01): snapshot closure set = {contextAssemblerCfg, tierProvider,
  contextSeam, piiRestore}; propagation sites = template loop (site 1),
  Manager.GetOrCreateWired clones (site 2, manager.go:153 ->
  template.ConfigSnapshot()), verifier children (site 3,
  verification_hook.go - audit required). Mirrors the 7bffceb9 group-gates
  three-site rule.
- C2 (leaf 02): RunTransformContext fixpoint per reasoningCycle iteration,
  beside RunPrepareNextTurn (loop.go ~:4663-4698), Modified==false cap 5,
  messages cloned before transform (history never mutated).
- C3 (leaf 05): persistence keys tool calls to the assistant message_id;
  SaveMessages path returns IDs (or new combined call). Restores the
  golden-capture harness first-choice path (92fa3350) and enables
  restore-with-tool-calls (loop.go:9328-9377 read side flagged, follow-up).
- C6 (leaf 06): TurnTerminalEvent gains turn_span_id omitempty; all 9
  construction sites audited; receiver sets lastTurn.turnSpan from the
  event (trace->turnspan map, NOT last-seen-terminal - mid-turn hops
  arrive before their terminal). Consumer audit: additive, decode unchanged.

## Review Checklist

Per-leaf in-session review by the orchestrator (never delegated):
wiring-failure proof demonstrated, scoped gates green, staged-set audit,
sibling-sweep attribution in commit bodies.

## Coding Conventions

Inherited from repo AGENTS.md + 20261031 forest conventions: ASCII only,
no stubs (every leaf wires to a real surface), hermetic e2e tier for new
feature tests, -p 2 test sweeps, opt-in defaults for new knobs, three-site
rule for loop-carried seams, best-effort persistence never fails a turn.

## Test Gate

No separate 00T phase: each leaf carries its own wiring-failure proof
test (delete the wiring -> test fails), authored in-leaf per the 20261031
forest precedent. Coverage matrix lives in each leaf's task table.

## Invariants

- Three-site rule (7bffceb9) for every loop-carried seam.
- SanitizeInput per-turn lane stays; transform lane is additive.
- Messages cloned before any transform mutation.
- CLOSED contracts gain fields only additively with same-commit contract
  doc notes (TurnTerminalEvent precedent).
- OPT-IN defaults: memory.tiers and jev_command_gate keys default false.
- Do NOT "fix" tier floor code to last-dispatched - the pinned e2e
  observes next-hop; doc-only (leaf 00).

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00-tier-floor-doc | PENDING | | doc-only, do-first |
| 01-snapshot-seams | PENDING | | wave C |
| 02-transform-lane | PENDING | | wave B; 07 depends |
| 03-tierpolicy-daemon | PENDING | | wave D; coordinate w/ second-brain |
| 04-jev-gate-wiring | PENDING | | wave A |
| 05-toolcall-persistence | PENDING | | wave A |
| 06-turnspan-linkage | PENDING | | wave C |
| 07-secret-restore-scoping | PENDING | | wave B; depends on 02 |

## Integration Test Plan

1. go build ./... green.
2. go test -p 2 on all touched packages green.
3. Wiring-failure property per leaf: each proof test named in the leaf.
4. make lint-ci green (gosec G202 gate: parameterize or #nosec with
   justification, shadow-store convention).
5. e2e suites for affected areas: jev-command-gate (04), boundary-hardening
   (07 PII/secret scenarios), context-memory-engine (01/03), llm-serving-tiers
   (00 pinned e2e untouched).
