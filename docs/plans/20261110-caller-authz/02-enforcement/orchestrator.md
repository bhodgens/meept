# 02-enforcement — Branch Orchestrator

## Goal

Enforce the per-caller action gate at execution time (PreExecChecker step
1c, caller_not_approved deny, no auto-pause, one audit row per deny) and
correct the docs + pin shipped behavior in an integration test.

## Architecture Overview

Consumes branch 01's callerid ctx seam + the agentID fix. Three leaves:
gate tests, enforcement step, docs+proof. The allowed-tool list comes
from [voice] config threaded to checker construction.

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts — this branch owns the step-1c
behavior block: reason string caller_not_approved, model-facing deny text
verbatim ("This caller is not approved for that action. Take a message
instead."), audit type voice.unapproved_action {employee_id, tool, ccid},
no-pause rule, executor details injection (caller_approved/caller_known
present ONLY when caller in ctx), docs/workflows/voice-sidecar.md rewrite.

## Child Index

| Child | Est. ctx | Deps | Order |
|---|---|---|---|
| 00T-tests.md | 45K | branch 01 | 1 |
| 01-step1c.md | 40K | 00T | 2 |
| 02-docs-proof.md | 35K | 01 | 3 |

## Dispatch Protocol

1. Dispatch 00T (tests only, RED for missing step-1c symbols/behavior
   only, Do NOT commit); validate IN-SESSION; commit gate tests.
2. 01 then 02 serially (shared file: enforcement.go).
3. Orchestrator-only commits, per-file pathspec; tracking updates.

## Coding Conventions

Per master.md. enforcement.go step order comment must be updated in the
same commit as the step (540s block). Existing PreExec table tests must
stay byte-green (no-caller ctx => 1c inert).

## Test Gate

| Feature | Unit tests | e2e | Status |
|---|---|---|---|
| deny for !approved non-allowed tool | 00T §1 | matrix | PENDING |
| allow for approved / allowed list | 00T §1 | — | PENDING |
| no-caller ctx inert (golden) | 00T §2 | — | PENDING |
| no auto-pause on caller deny | 00T §3 | — | PENDING |
| audit row once per deny / nil-safe | 00T §4 | — | PENDING |
| details injection presence-only | 00T §5 | — | PENDING |
| deny text reaches model | 00T §6 | — | PENDING |
| docs match code | 02 review | — | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 00T | PENDING | | |
| 01 | PENDING | | |
| 02 | PENDING | | |

## Integration Test Plan

Daemon-level behavior matrix (leaf 02): approved caller -> constitution
gating applies (O5 proof); unknown caller -> message tool allowed,
shell_execute denied with pinned text, no pause, one audit row; ledger
state append by unknown caller -> ALLOWED today (O4 residual) — pinned in
the test with a comment flagging owner decision Q1.

## Review Checklist

- [ ] Reason/deny/audit strings verbatim.
- [ ] No answering-path or outbound-path behavior change.
- [ ] O4 residual risk surfaced in the leaf report AND the test comment.

## Open Questions

Master Q1 (narrow ledger_append for unapproved) — deferred, flagged.
