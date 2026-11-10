# 01-identity — Branch Orchestrator

## Goal

Resolve WHO is calling at answer time (contacts.db voice_approved), carry
that identity in the neutral callerid ctx seam, stamp it on every voice
turn, and fix the voice executor agentID so per-employee constitution
gating applies to voice turns.

## Architecture Overview

Three steps, one gate: callerid package + contact lookup + config key
(leaf 01), then voice/daemon plumbing: Instance.Caller, resolve-once at
answer, ctx stamp, agentID fix (leaf 02). Enforcement consumes this
branch's seam in branch 02-enforcement.

## Interface Contracts (FROZEN)

The full frozen contracts are in ../master.md §Interface Contracts.
This branch owns: internal/callerid (Info/With/From), contact
LookupByPhone + voice_approved + phone index, config
unapproved_caller_allowed_tools (default ["ledger_append", <real message
tool>]), voice Instance.Caller + CallerResolver seam, daemon resolve-once
+ callerid.With stamp in bindingTurnRunner.Run + voice-loop executor
agentID = employee id (VOICE clones only).

## Child Index

| Child | Est. ctx | Deps | Order |
|---|---|---|---|
| 00T-tests.md | 45K | — | 1 |
| 01-callerid-contact.md | 40K | 00T | 2 |
| 02-stamping.md | 50K | 01 | 3 |

## Dispatch Protocol

1. Dispatch 00T ("tests ONLY, RED for missing symbols in its own files
   only, Do NOT commit"); validate IN-SESSION vs the Test Gate matrix;
   commit gate tests; TESTS_VALIDATED.
2. Dispatch 01, review, commit; then 02 (depends on 01), review, commit.
3. Orchestrator-only commits, per-file pathspec.

## Coding Conventions

Per master.md. contact store edits keep the guarded-ALTER idiom and its
duplicate-column tolerance test. callerid imports ONLY stdlib+context.

## Test Gate

| Feature | Unit tests | e2e | Status |
|---|---|---|---|
| callerid With/From | 00T §1 | — | PENDING |
| LookupByPhone + migrate + index | 00T §2 | — | PENDING |
| resolver fail-soft | 00T §3 | voice-lifecycle | PENDING |
| ctx stamp + agentID fix | 00T §4 | — | PENDING |
| config default normalization | 00T §5 | — | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 00T | PENDING | | |
| 01 | PENDING | | |
| 02 | PENDING | | |

## Integration Test Plan

`go test ./internal/callerid/ ./internal/contact/ ./internal/config/
./internal/voice/ -count=1` + daemon `-run 'Voice|Caller'` suite +
`go build ./...`; the §4 stamp assertions are the branch acceptance core.

## Review Checklist

- [ ] Contracts verbatim; message-tool name choice recorded from leaf 01.
- [ ] Answering path NOT gated anywhere (fail-soft only).
- [ ] agentID change scoped to voice clones; global defaults untouched.
- [ ] Branch suite GREEN before reporting COMPLETE.

## Open Questions

Master Q2 (signal leg stamping) resolved here if the stamp site is shared.
