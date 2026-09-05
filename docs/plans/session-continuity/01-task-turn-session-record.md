# Task-Turn Session Record - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Task-path chat turns (sync_dispatch/route_to_agent) record the user message and final reply into the SESSION conversation, so follow-up turns share context.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** A5 (e2e continuity FAIL, chat-dispatch-ux leaf 10); root-cause chain item 1

## Goal

When the dispatcher routes a turn to a task (intent=code → sync dispatch),
the exchange is recorded only in the task-scoped step conversation. The
session conversation never sees it, so the next turn's model context is
empty of what was just done. This leaf writes both sides of the exchange
into the session conversation after the reply resolves, best-effort.

## Context

ChatHandler.HandleChatMessage: on route_to_agent/sync dispatch the reply
arrives via waitForTaskCompletion (handler.go ~765). persistExchange
(handler.go:924) already records the exchange to the DB (session_messages)
but the in-memory conversation cache — what the model actually sees next
turn — is only populated by RunOnceWithParts on the direct path.

The session conversation is reachable via the same session-scoped loop the
direct path uses: `h.sessionLoop(conversationID)` (handler.go:1875) → its
`conversations` store (`l.conversations.Get(id)`; Conversation has
AddUserMessage / AddAssistantMessage — check internal/agent/conversation.go
for exact method names).

Key files to understand before implementing:
- internal/agent/handler.go - the sync-dispatch branch (~756-765), persistExchange (924), sessionLoop (1875)
- internal/agent/conversation.go - Conversation mutators (AddUserMessage etc.), ConversationStore.Get
- internal/agent/loop.go - conversations field (517), accessors

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/handler.go
// func (h *ChatHandler) recordExchangeInSessionConv(conversationID,
//   userMsg string, reply string) — best-effort:
//   loop := h.sessionLoop(conversationID); conv := loop's conversation
//   (via a new small accessor on AgentLoop, e.g. SessionConversation(id)
//   *Conversation, mutex-safe) — nil-safe every step; Warn on failure.
//   Appends user entry + assistant entry (reply text) when non-empty.
// Called on the route_to_agent/sync_dispatch path after reply resolves,
// next to persistExchange. NOT called on the direct path (RunOnce already
// records it — double-append must be avoided).
// Owner: A. Consumers: C (e2e A5).
```

### What This Leaf Consumes

```
// AgentLoop.conversations (unexported) — expose a narrow accessor:
//   func (l *AgentLoop) SessionConversation(id string) *Conversation
//   (loop.go; returns conversations.Get(id) — creating via Get is fine,
//   it auto-vivifies; check ConversationStore.Get semantics)
// Conversation mutators from internal/agent/conversation.go
```

## Tasks

### Task 1: SessionConversation accessor on AgentLoop

**Objective:** Mutex-safe read access to a conversation from the loop.

**Files:**
- Modify: `internal/agent/loop.go` (near GetConversation, line ~6457)
- Test: `internal/agent/loop_test.go`

**Step 1: Write failing test** — construct a loop (existing helpers), call
SessionConversation("conv-x"), assert non-nil and that a second call
returns the SAME pointer (cache identity). Mirror existing conversation
tests for setup.

**Step 2: verify failure → implement (delegate to l.conversations.Get) →
verify pass.** `go test -p 2 ./internal/agent/ -run SessionConversation -count=1`

### Task 2: recordExchangeInSessionConv + call site

**Objective:** Best-effort recording on the task path.

**Files:**
- Modify: `internal/agent/handler.go` (new method + one call next to the
  sync-dispatch reply resolution, before persistExchange at ~914)
- Test: `internal/agent/handler_test.go`

**Step 1: Write failing test** — handler with stores; simulate a
sync-dispatch reply; assert the session conversation now contains a user
entry with the request text and an assistant entry with the reply text;
assert a second call does NOT duplicate (guard: record only when the
last user entry differs, or record exactly once per request — prefer
explicit call-site placement so it runs once).

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run TestChatHandler_RecordExchange -count=1 -v`

**Step 3: full package** — `go test -p 2 ./internal/agent/ -count=1` green.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Direct path NOT double-recording (test or clear reasoning documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Accessor is mutex-safe and nil-safe
- [ ] Recording happens ONLY on the task path (direct path untouched)
- [ ] Empty reply does not append an empty assistant entry
- [ ] Best-effort: no error propagation to the reply path
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- handler.go is shared with sibling sessions — work around foreign hunks,
  keep your diff minimal.
- The direct path records via RunOnceWithParts internally (AddUserMessage
  etc. in RunOnce) — verify that before assuming; if it does NOT, note it
  as a deviation rather than expanding scope.
