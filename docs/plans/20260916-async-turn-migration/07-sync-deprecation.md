# Sync Chat Deprecation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Flip the default: new sessions use the async path; the 110s sync task-wait and its stub are retired for migrated clients while the sync RPC stays available behind a config flag for the migration tail. Rewrite the AGENTS.md invariant in the same commit.
- **Dependencies:** 02, 03, 04, 05, 06 all REVIEWED (every client migrated; watchdog live)
- **Estimated Context:** ~40K
- **Concurrency Group:** D (last)

## Goal

All four clients now speak submit-ack. This leaf makes async the default
and demotes the old path: `waitForTaskCompletion`'s 110s ceiling and the
"Task ... is still running" stub stop existing on the default path; the
blocking `chat` RPC remains behind `[orchestrator] sync_chat_enabled=false`
(default) so un-migrated external consumers keep working during the tail.

## Context

The sync machinery: `syncMode` (handler.go:111) forced true for
`source_client=meept-bench` (handler.go:798); the bench no longer sends
that (leaf 02). `waitForTaskCompletion` (handler.go:1890, 110s ceiling at
:1904, stub at :1939) only runs when syncMode. The proxy cap stays
(proxy.go:54) — it is a normal fast-RPC timeout under the ack model and is
NOT touched (per the master plan's evaluation). AGENTS.md carries the
invariant "Sync replies carry the real step result" — repo rules require
rewriting it in the same commit as the behavior change.

Key files:
- internal/agent/handler.go — syncMode, waitForTaskCompletion, the bench
  special-case at :798
- internal/config/schema.go — new flag + defaults
- config/meept.json5 — template block
- AGENTS.md — Chat-reply invariants section
- docs/workflows/ — whichever workflow doc describes chat dispatch (locate
  via search for the stub string or sync dispatch)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// 1. internal/config/schema.go OrchestratorConfig gains:
//    SyncChatEnabled bool `json:"sync_chat_enabled" toml:"sync_chat_enabled"`
//    Default FALSE. Comment: legacy blocking chat for un-migrated
//    integrations; when true, task-dispatched turns block the chat RPC up
//    to the 110s sync-wait ceiling and may return a still-running stub.
//
// 2. handler.go: syncMode resolution becomes
//    h.syncMode = cfg.SyncChatEnabled || strings.HasPrefix(req.SourceClient, "meept-bench") && cfg.SyncChatEnabled
//    — i.e. the bench special-case ALSO honors the flag (default config:
//    bench turns are async; the bench no longer wants sync per leaf 02).
//    waitForTaskCompletion itself is UNCHANGED (it still exists for
//    sync_chat_enabled=true installs) — only its reachability changes.
//
// 3. AGENTS.md rewrite (same commit): replace the "Sync replies carry the
//    real step result" bullet with the async contract: chat.submit acks
//    immediately; results arrive via turn.terminal; sync blocking is a
//    legacy opt-in (sync_chat_enabled=true) with the 110s ceiling + stub
//    documented as legacy-only behavior.
//
// 4. docs: the stub string and 110s ceiling get a "legacy" note wherever
//    documented (grep "is still running" and "110" in docs/).
```

### What This Leaf Consumes

```
// Leaves 02-06 complete: bench/CLI/TUI/GUI on submit+await; watchdog live.
```

## Tasks

### Task 1: Config flag + syncMode resolution

**Objective:** The flag exists, defaults false, and gates syncMode including
the bench special-case.

**Files:**
- Modify: `internal/config/schema.go` (+ defaults fn + template)
- Modify: `internal/agent/handler.go` (the :798 special-case + wherever
  syncMode is initialized from config — find the SetSyncMode caller; if
  none exists in daemon composition, wire the config through the existing
  handler-construction seam)
- Test: config defaults/template test; handler test

**Step 1: Failing tests**

- Default config → syncMode false even for source_client=meept-bench.
- sync_chat_enabled=true → syncMode true (bench and otherwise).
- Template parses with the new key (extend the template-parse test).

**Steps 2-4:** standard cycle.

### Task 2: Default-path stub elimination proof

**Objective:** Under default config, NO path can return the stub.

**Files:**
- Test: `internal/agent/handler_sync_deprecation_test.go` (create)

**Step 1: Failing tests**

- Task-dispatched turn with sync OFF: the chat.submit path returns the ack
  (leaf 01) and handleRequest never enters the sync wait (assert: the
  waitForTaskCompletion wait ticker never starts — via a test seam or by
  asserting the reply is the async ack, never stub-shaped).
- sync ON (legacy): the old behavior still works (existing sync tests keep
  passing unchanged — they may need the flag set in their fixtures).

**Steps 2-4:** standard cycle. Existing tests that assumed sync-by-default
get the flag explicitly — that is a test-fixture change, not a behavior
change; list each in Deviations.

### Task 3: AGENTS.md + docs rewrite

**Objective:** The invariant matches reality in the same commit.

**Files:**
- Modify: `AGENTS.md` (the chat-reply invariants section)
- Modify: docs mentioning the stub/110s (grep-driven; also
  docs/plans/20260916-async-turn-migration/master.md gets a completion note)

**Step 1:** Draft the replacement bullet:

```
- **Turns are asynchronous; acks are immediate.** chat.submit acks a turn
  in milliseconds (turn_id + conversation_id); the result arrives via the
  turn.terminal bus event (Plan: docs/plans/20260916-async-turn-migration).
  The blocking `chat` RPC is a legacy opt-in (sync_chat_enabled=true):
  task-dispatched turns then block up to the 110s sync-wait ceiling and may
  return the "still running" stub. Stalled async turns are reaped by the
  turn watchdog and surface as failed terminal events.
```

**Step 2:** Apply, plus the docs greps. **Step 3:** verify no doc still
describes the stub as default behavior (grep "is still running" docs/ —
hits only under legacy notes). **Step 4:** this is docs — cross-reference
check instead of tests.

### Task 4: The bench special-case audit

**Objective:** No hidden sync dependency survives in-tree.

**Files:**
- Modify: any in-tree caller relying on the bench-forced syncMode (grep
  `meept-bench` across internal/ — the special-case at :798 is the known
  one; fix per Task 1).

**Step 1:** grep + fix + test (covered by Task 1's tests). **Steps 2-4:** verify
`go test -race ./internal/... -count=1 -p 2` green.

## Self-Verification Checklist

- [ ] Flag defaults false; template + defaults + parse test
- [ ] Default path provably cannot return the stub (test)
- [ ] Legacy path (flag on) fully functional (existing sync tests pass with the flag)
- [ ] AGENTS.md rewritten in the same change; no stale doc claims
- [ ] Bench special-case honored only under the flag
- [ ] Full suite -race green (-p 2)

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale — especially list
every test fixture that needed the flag set]

## Review Checklist (For Review Agent)

- [ ] Tasks + tests present/passing (-race, -p 2)
- [ ] Default config: end-to-end async (submit ack, terminal event, reaper armed)
- [ ] Legacy opt-in works and is documented as legacy
- [ ] AGENTS.md rewrite present and accurate; docs grep clean
- [ ] waitForTaskCompletion code UNCHANGED (reachability-only deprecation —
      deleting it is explicitly out of scope until the migration tail ends)
- [ ] Proxy 120s cap untouched

Output: APPROVED or specific gaps with file+line.

## Notes

- Non-goals (documented): turn cancellation, cross-restart turn resume,
  deleting the legacy sync code. These are follow-ups after the migration
  tail.
- The reaper (leaf 06) covers stalls; this leaf only changes reachability
  of the legacy wait. The two together give the guarantee: every submitted
  turn reaches a terminal event (result, failure, park-notice, or reap).
