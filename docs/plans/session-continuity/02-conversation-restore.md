# Conversation Restore From DB - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Cache-miss conversation lookups restore history from the session store (session_messages) via the existing GetOrRestore seam.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** A5; root-cause chain items 2 and 3

## Goal

The daemon persists every chat exchange to `session_messages`
(persistExchange, handler.go:924 — verified: rows exist), and
ConversationStore.GetOrRestore (internal/agent/conversation.go:1726)
exists to hydrate a conversation from storage on cache miss — but has
zero callers. After a daemon restart, conversation eviction, or any
cache miss, the model sees an empty history. This leaf wires a restore
function into the loop's conversation lookups.

## Context

RunOnce (internal/agent/loop.go:2303) and the other conversation lookups
(2719, 4999) use `l.conversations.Get(conversationID)` — cache-only.
GetOrRestore(id, restoreFn) does fast-path cache hit, slow-path restoreFn
I/O outside the lock, and falls back to a fresh conversation on error.

The session store interface in the loop (loop.go:767 sessionStore:
Get/SaveMessages/UpdateDesignation/ClearDesignation) does not expose
reading messages. The real store (internal/session store.go:87) has
`GetMessages(sessionID string, offset, limit int) ([]Message, error)`.
Messages carry Role (user/assistant), Content, Timestamp, SessionID.

Config: `session.restore_message_limit` (schema.go:494, default 0 = all)
— respect it: restore the MOST RECENT N messages when N > 0.

Key files to understand before implementing:
- internal/agent/loop.go:767 (sessionStore interface), 517 (conversations field), 2303/2719/4999 (Get call sites), SetSessionStore (6736)
- internal/agent/conversation.go:1726 (GetOrRestore), Conversation population helpers (how RunOnce adds user/assistant entries — reuse the same mutators)
- internal/session/store.go:87 (GetMessages), Message struct (Role/Content/Timestamp)
- internal/config/schema.go:494 (RestoreMessageLimit)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/loop.go
//   - sessionStore interface (767) gains:
//       GetMessages(sessionID string, offset, limit int) ([]sessionMessage, error)
//     via a narrow structural view type (map session.Message fields — the
//     loop must not import internal/session directly if it doesn't already;
//     CHECK current imports: SetSessionStore already type-asserts the real
//     store, so match the existing import situation).
//   - AgentLoop gains restoreFn wiring: SetSessionStore also stores the
//     config (already receives sessionCfg any — extract limit from it).
//     restoreFn(id): GetMessages(id, 0, 0) then apply limit (tail N),
//     map to []llm.ChatMessage{Role, Content} in ascending timestamp order;
//     unknown id / empty list → return empty slice, nil error (fresh conv).
//   - RunOnce (2303) and RunOnceWithParts conversation acquisition switch
//     to conversations.GetOrRestore(id, l.restoreFn) — the OTHER two call
//     sites (2719, 4999) stay cache-only unless they are the same turn
//     path (verify; document choice).
// Owner: B. Consumers: C (e2e A5).
```

### What This Leaf Consumes

```
// ConversationStore.GetOrRestore (existing, unmodified)
// session store GetMessages (existing)
// cfg.Session.RestoreMessageLimit (existing)
```

## Tasks

### Task 1: restoreFn construction

**Objective:** Build and wire the restore function.

**Files:**
- Modify: `internal/agent/loop.go` (sessionStore interface, SetSessionStore, new restoreFn method)
- Test: `internal/agent/loop_test.go`

**Step 1: Write failing test** — fake session store returning 3 known
messages for id "conv-1"; loop with SetSessionStore(fake, cfg{Limit:0});
call the restore path (drive RunOnce with an unrelated new conversation,
or test restoreFn directly if exported); assert the conversation
contains the 3 messages in order. Second case: Limit=2 → only last 2.

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run Restore -count=1 -v`

### Task 2: switch RunOnce/RunOnceWithParts acquisition to GetOrRestore

**Objective:** Cache misses hydrate instead of starting empty.

**Files:**
- Modify: `internal/agent/loop.go:2303` (and RunOnceWithParts' equivalent
  acquisition — locate it; RunOnce may delegate)
- Test: extend Task 1 test — first RunOnce call persists fake history;
  simulate cache miss by constructing a NEW loop with the same fake store;
  RunOnce with a follow-up message; assert the model-visible conversation
  contains restored history BEFORE the new user message (check via
  conversation contents or a chatter fake capturing messages).

**Step 1: failing test → Step 2: implement (Get → GetOrRestore(id,
l.conversationRestoreFn)) → Step 3: package green.**
`go test -p 2 ./internal/agent/ -count=1`

### Task 3: unknown-ID behavior

**Objective:** Restore errors/empty never break the turn.

**Files:**
- Test: `internal/agent/loop_test.go`

**Step 1: test** — store returns error / empty for the id; RunOnce still
succeeds with a fresh conversation; restore failure logged once at Debug/Warn.

**Step 2: run** scoped + package green.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] GetOrRestore I/O stays OUTSIDE the store lock (it already does — do
      not move it inside)
- [ ] Restore limit respected (0 = all, N = most recent N)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] GetOrRestore used for turn-path conversation acquisition
- [ ] RestoreFn maps DB rows to llm.ChatMessage ascending, tail-capped
- [ ] Error/empty → fresh conversation, logged, turn unaffected
- [ ] sessionStore interface extension is minimal and structurally asserted
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Message-count sanity: session_messages rows for a session include
  role user/assistant with EntryType "message" (persistExchange) —
  filter to those two roles to avoid restoring system/anchor noise.
- The 12-rows-in-e2e observation confirms the data is there; the restore
  just needs to read it.
- If sessionStore's existing methods make the interface extension awkward,
  a separate narrow interface + type assertion at SetSessionStore time is
  acceptable (document as deviation) — do not break existing fakes.
