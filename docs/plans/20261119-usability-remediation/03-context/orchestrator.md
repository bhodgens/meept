# 03-CONTEXT - Session Identity, History Durability, Recall Fidelity

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 2 implementation leaves
- **Scope:** Clear context between messages: validated session identity,
  durable history, faithful recall of the session's own work.

## Goal

Close W10/W11 + C1-C8 (report axis 1): F-ORCH-16/22/24/29, session-steering
A1/A7, recall_answer.go defects, GUI restore policy divergence (client-side
restore is 05-navigation; this branch owns the SERVER resume contract).

## Architecture

Two leaves: session validation + history durability (01, chat_submit.go +
handler persistExchange + session store), recall digest fidelity (02,
recall_answer.go + dispatcher digest). File-disjoint (group A both).
Depends on 01-safety/02 (C3) only for queue-aware continuation context.

## Interface Contracts

Produces C4 (session identity + history + include_tool_calls). Consumes
C3 (queue resolution), C1 (reply_source key for recall answers).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 60K | A |
| 01 | 01-session-history.md | leaf | 00T | 80K | A |
| 02 | 02-recall-fidelity.md | leaf | 00T | 70K | A |

## Dispatch Protocol

Phase 0: 00T gate RED, validate in-session, commit, TESTS_VALIDATED.
Phase 1: 01 + 02 together (file-disjoint). Review in-session per leaf;
commit exact paths; max 3 cycles then BLOCKED. Briefs: worktree path,
consumed contracts, no-commit/no-git-add/no-read_file/write-once rules.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] C4 exact; include_tool_calls additive (legacy clients unaffected)
- [ ] Gate tests GREEN and unmodified
- [ ] Live-gate: conversation-only submit -> resume -> messages present;
      unknown id rejected; recall answers name the session's actual artifacts
- [ ] No scope creep / artifacts / corruption; gofmt clean

## Coding Conventions

Per ../master.md. Branch specifics: session-row upsert must be idempotent
and race-safe (two concurrent submits, one conv id); recall digest bounded
(task-set cap, never unbounded history dump); RFC3339 second-precision
roundtrip pins for any new timestamp column.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-session-history | PENDING | 0 | |
| 02-recall-fidelity | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/rpc/... ./internal/agent/... ./internal/session/...`.
2. e2e session-storage + naive-user-chat suites (extended by 00T).
3. Live rig: p1 naive continuity probe - T3/T4 answers reference hello.txt
   and the session's files; p2 isolation - session A recalls ZEBRA-4417,
   session B does NOT see A's private notes (scope check, I5).

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F9-F10):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F9 | session validation + history | 01 T1-T4 | TestUnknownIDRejected, TestBothIDMismatch, TestConvOnlyHistoryRoundtrip, TestIncludeToolCalls | e2e/suites/session-storage (extend) | PENDING |
| F10 | recall fidelity | 02 T1-T3 | TestRecallFailedState, TestRecallTaskSet, TestRecallFactToMemory | e2e/suites/naive-user-chat (extend) | PENDING |

Delta log: (none yet)

## Open Questions

- Q: cross-session bleed (p2 B saw A's notes) may be memory SCOPE, not
  recall - 02 includes an I5 scope verification task; if the leak is in
  EffectiveMemoryScope, report and open a follow-up leaf (do not widen scope).

## Notes

- GUI/TUI restore-policy divergence (C6/C8): server resume contract here;
  client policy alignment is 05-navigation/03.
