# Session Continuity - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 leaf documents under this node
- **Scope:** Make conversation history survive across turns and routes so follow-up questions like "did the change get made? where is the file?" get answered with the actual file, not a clarification request.

## Goal

The chat-dispatch-ux tree's e2e (leaf 10, final live run) showed the one
remaining FAIL: after a sync-dispatched task turn ("create hello.txt"),
follow-up turns ("did the change get made? where is the file?", "what files
did you make for me?") return clarification questions — the model has no
memory of what was just done. The user's bar (set by the 2026-09-04 hermes
comparison) is that follow-ups reference the artifacts of prior turns.

Root-cause chain (verified 2026-09-04 against HEAD):

1. **Task-path turns bypass the session conversation.** When the
   dispatcher routes to a task (intent=code → sync_dispatch), the user
   message and the task result are NEVER written into the session
   conversation cache — the exchange lives in the step conversation
   (`step-<taskID>-<stepID>`) of the task-scoped loop. Only direct-mode
   turns populate the session conversation (RunOnceWithParts).
2. **DB persistence is write-only.** `persistExchange`
   (internal/agent/handler.go:924) saves every exchange to
   `session_messages` (verified: 12 rows in the e2e scratch DB), but
   nothing ever loads them back: `ConversationStore.GetOrRestore`
   (internal/agent/conversation.go:1726) — built for exactly this — has
   ZERO callers, and `WithPersistence`/`SetPersistence` are never wired.
3. **RunOnce reads cache-only.** loop.go:2303 uses
   `l.conversations.Get(conversationID)` — a cache-only lookup that
   returns an empty conversation on any miss (restart, eviction, or
   first contact after task-path turns).

## Architecture

Wire the existing restore seam rather than inventing a new one. The
session conversation becomes the single source of truth for turn
history: direct-mode turns already write it; leaf A makes task-path
turns write it too; leaf B makes cache misses restore it from the DB;
leaf C pins the behavior in e2e so A5 stops being flaky.

## Interface Contracts

### Contract 1: Task-path turns record into the session conversation (SC1)

```
// internal/agent/handler.go — the route_to_agent/sync_dispatch path
// (after reply is resolved, alongside persistExchange):
//   - append user message + final reply text to the SESSION conversation
//     (the one keyed by conversationID) via the same session-scoped loop
//     the direct path uses (h.sessionLoop(conversationID) → its
//     conversations store), so subsequent turns share context.
//   - best-effort: errors logged at Warn, never fail the reply.
// Owner: A. Consumers: C (e2e A5), all follow-up turns.
```

### Contract 2: Conversation restore from DB (SC2)

```
// internal/agent/loop.go — RunOnce (and RunOnceWithParts) conversation
// lookup switches from conversations.Get(id) to
// conversations.GetOrRestore(id, l.restoreFn) where restoreFn loads the
// last N (config: session.restore_message_limit, 0=all) messages for the
// conversation from the session store and maps them to llm.ChatMessage
// (role user/assistant, content). restoreFn returns an error for
// unknown IDs → GetOrRestore already falls back to a fresh conversation.
// The restoreFn interface is added to the sessionStore interface
// (loop.go:767) or a narrow new interface asserted structurally.
// Owner: B. Consumers: C.
```

### Contract 3: E2E asserts continuity (SC3)

```
// scripts/e2e-naive-user-chat.sh — A5 tightens from
// "reply references hello.txt" to asserting the reply references the
// artifact AND the follow-up turn receives task context (no bare
// clarification request when the prior turn created an artifact).
// Owner: C. Consumers: A5 stops being flaky.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-task-turn-session-record.md | leaf | none | 30K | A |
| 02 | 02-conversation-restore.md | leaf | none | 35K | A |
| 03 | 03-e2e-continuity-assert.md | leaf | 01, 02 | 25K | B |

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

Dispatch 01 and 02 simultaneously via `delegate_task` with the standard
hierarchy-planning dispatch context (full leaf text + contracts +
conventions + anchor snippets inlined; no-commit; no read_file; TDD).
NOTE: 01 edits handler.go, 02 edits loop.go — different files, safe in
parallel; both files may carry sibling-session WIP, so leaves must
`git diff` their target file first and work around foreign hunks.

### Phase 2: Review and Commit Each Child

Orchestrator reviews in-session per leaf spec + contracts; commits exact
paths after scoped tests pass. Shared-tree hazards apply: hooks bypass
requires manual package verification + message note; check `git log`
before each commit (sibling sessions land commits continuously).

### Phase 3: Dispatch Group C, Integration

After 01 + 02 commit, dispatch 03 (e2e update), then run the Integration
Test Plan, commit integration, update AGENTS.md invariant block.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied exactly
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD)
- [ ] Code follows project conventions
- [ ] No scope creep
- [ ] No debug artifacts, no line-number corruption
- [ ] AGENTS.md touched if the leaf invalidates a statement in it

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+; wrap errors with %w; two-value assertions
- **No ignored errors; no panic; mutex scope: never hold across I/O**
- **Testing:** stdlib testing, table-driven where natural, scoped
  `go test -p 2 ./internal/<pkg>/ -run X -count=1`
- **Never commit; never git add; do not modify .md plan files**
- **Do NOT use read_file on existing sources — search_files/terminal**
- **After writing a file do NOT read it back**

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-task-turn-session-record | REVIEWED | 1 | commit 2a433d8e; recordOnTaskPath gating; Get auto-vivify verified; direct-path no-double-record documented |
| 02-conversation-restore | REVIEWED | 1 | commit 9a3e3f65; narrow reader interface; MaxInt32+tail-cap (limit=0 returns 0 rows); clone propagation added (dead-code save) |
| 03-e2e-continuity-assert | REVIEWED | 1 | commit 9bd2c6a4; A5 tightened, not weakened; 4 runs run: 01/02 hold (sqlite-verified) — REAL blocker is pre-existing intent-analyzer ambiguity short-circuit at dispatcher.go:667 firing before the chat agent sees restored history. Follow-up leaf required: session-aware ambiguity gate |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...`
2. `go test -p 2 ./internal/agent/ -count=1`
3. `bash scripts/e2e-naive-user-chat.sh` — A5 must now PASS reliably
   (run twice; both runs green).
4. Full chat-dispatch assertion set still passes (A1-A4, A6, F6).

## Open Questions

- Restore ordering: session_messages rows carry timestamps; restore in
  ascending timestamp, cap by restore_message_limit from the END (most
  recent N). Default config value 0 = all (schema.go:3076) — keep.

## Structural Completeness Check (Before Dispatch)

Run:
```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep session-continuity
```
Must print ALL TREES COMPLIANT contribution (tree OK, leaves OK).

## Notes

- This tree is the A5 follow-up from chat-dispatch-ux (leaf 10 final
  live run: 16 PASS / 1 FAIL / 0 SKIP). Do not re-litigate leaf 05's
  reply guard (separately fixed, commit 26f276fd).
- macOS: never unbounded `go test ./...`; use -p 2 scoped runs.
- Working tree may carry sibling-session WIP; stage only your paths.
