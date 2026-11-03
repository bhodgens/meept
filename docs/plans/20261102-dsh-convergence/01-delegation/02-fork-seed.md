# 02-fork-seed - History-Seeded Fork

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests for this branch
> (00T-tests.md) ALREADY EXIST and are committed — your job is to make them
> pass; do NOT delete, skip, weaken, or rewrite any gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back — write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Child creation mode `fork`: a child seeded with the parent's
  completed-turn prefix, so it "continues this conversation's analysis"
  without reloading the thread. Implements B2 (`completedTurnPrefix`).
- **Dependencies:** 01-spawn-async (B1 core: `DelegateManager.SetForkSeed`),
  00T-tests (gate F2)
- **Estimated Context:** 55K
- **Concurrency Group:** C (parallel with 03, after 01 REVIEWED)
- **Audit references:** dsh-audit GAP 1b (in-process fork), fork index.ts

## Goal

meept's `delegate_task` explicitly does NOT inherit the parent transcript
(platform.go:359-363: "the child's conversation is fresh ... no parent
transcript is inherited"). dsh's `fork` provider seeds the child with the
recorded parent log through its last completed `turn/end`
(subagent-fork-in-process/index.ts:46-53): an in-flight tool-call turn is
unbalanced and cannot be replayed. This leaf ports that logic: a pure
`completedTurnPrefix` that slices a parent Conversation's messages up to and
including the last completed turn, plus wiring it into the `DelegateManager`
`SetForkSeed` hook so `Mode:"fork"` spawns a child whose session begins with
that seed. The seed is captured ONCE at spawn (not re-forked from the parents
later history) — leaf 06 persists it in the descriptor for durable replay.

## Context

- `internal/agent/conversation.go:518` — `(*Conversation).GetMessages() []llm.ChatMessage` (the message log to slice).
- `internal/agent/isolation.go:65-106` — `BuildSpawnContext(iso, brief, artifacts, memoryIDs, parent)`. `IsolationSharedTranscript` materializes the inherited parent transcript into the child's spawn brief.
- `internal/agent/delegate.go` (01) — the `SetForkSeed` seam hook `func(*Conversation) []llm.ChatMessage` that `Spawn` calls for `Mode:"fork"`.
- dsh reference: `/tmp/deepseek-harness/packages/subagent/subagent-fork-in-process/src/index.ts:46-53,72-78`.
- meept role/message shapes: `llm.Role` (user/assistant/tool), `llm.ChatMessage`. Tool reveals: `llm.ToolCall` on assistant messages.

## Verified Anchors (grep-confirmed at authoring time)

- internal/agent/conversation.go:518 — `GetMessages() []llm.ChatMessage`
- internal/agent/conversation.go:475,485 — `AddAssistantMessage` / `AddAssistantMessageWithToolCalls` (the child seed materialization)
- internal/agent/isolation.go:24-46 — `ContextIsolation` enum; `IsolationSharedTranscript`
- internal/agent/isolation.go:65-106 — `BuildSpawnContext` / `RenderSpawnContext`
- internal/tools/builtin/platform.go:359-363 — the existing "no parent transcript is inherited" comment (what fork overrides)
- internal/agent/registry.go:940-947 — `RunAgent` (the child runs on `fullMessage` built from `RenderSpawnContext`)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/delegate_fork.go  (NEW)
// completedTurnPrefix returns the parent's messages up to AND INCLUDING its
// last completed turn; the in-flight (unbalanced) turn is excluded. Empty
// when no turn has completed, so the child starts fresh.
func completedTurnPrefix(conv *Conversation) []llm.ChatMessage
// Wired by 01 via DelegateManager.SetForkSeed(completedTurnPrefix).
// Fork constraint (B2): the seed is captured ONCE at spawn and rides the
// child's descriptor (B6) for cold resume.
```

### What This Leaf Consumes

- B1 (01-spawn-async) — `DelegateManager.SetForkSeed(func(*Conversation) []llm.ChatMessage)`; `BuildSpawnContext(IsolationSharedTranscript, ...)`
- M-C1 DelegateRequest — `Mode == "fork"` triggers the seed path

## Tasks

### T1: completedTurnPrefix slices to last completed turn

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Implement `completedTurnPrefix` per dsh fork semantics | Create: internal/agent/delegate_fork.go; Test: internal/agent/delegate_fork_test.go (gate) | `go test -p 2 ./internal/agent/ -run TestCompletedTurnPrefix -count=1` | symbol undefined | seed ends at last completed turn; in-flight turn excluded; empty when none | Given a Conversation with 3 completed assistant turns followed by an in-flight tool-call turn; When `completedTurnPrefix` runs; Then the seed includes the 3 turns and excludes the trailing unbalanced assistant/tool messages |

T1 (expand):
1. Confirm the gate test REDs.
2. Implement a pure function over `[]llm.ChatMessage` + a thin `*Conversation` wrapper:

```go
// turnCompleted reports whether messages[..i] ends on a balanced turn: a
// user message followed by an assistant non-tool response, OR a user message
// whose following assistant turn has all tool calls settled by their results.
func lastCompletedTurn(msgs []llm.ChatMessage) int

func completedTurnPrefix(conv *Conversation) []llm.ChatMessage {
    if conv == nil { return nil }
    msgs := conv.GetMessages()
    idx := lastCompletedTurn(msgs)
    if idx < 0 { return nil }
    return msgs[:idx]
}
```

Meept has no explicit `turn/end` event in the conversation's message list
(the `turn_end` bus event is separate, events.go:35); a completed turn is an
assistant message with NO unsettled tool calls (every `ToolCall` matched by a
`tool` role result that follows it). `lastCompletedTurn` walks from the end,
returns the index one past the last message where the conversation is
balanced. Rules:
- A trailing assistant message with pending tool calls => unbalanced, drop it (and everything after the last good turn).
- A trailing `tool`-role message with no following assistant => unbalanced, drop.
- The last index whose prefix is a complete user→assistant exchange (no dangling tool call) is the cut.
3. Verify pass via the gate test.

### T2: Wire fork seeding into DelegateManager

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | `Mode:"fork"` spawns a child seeded with the completed-turn prefix | Modify: internal/agent/delegate.go (register the hook); Test: e2e delegate-seam (`TestDelegate_ForkSeed`) | `make e2e-fast-area AREA=delegate-seam` | fork seeded child has no inherited context (gate e2e RED) | a forked child's conversation begins with the parent's completed-turn prefix | Given a parent with completed turns in its session; When `DelegateRequest{Mode:"fork"}` spawns a child; Then the child's `fullMessage` (via RenderSpawnContext) includes the seed and the child continues the analysis |

T2 (expand):
1. In `NewDelegateManager` (01) the `SetForkSeed` hook exists; this leaf
   calls `mgr.SetForkSeed(completedTurnPrefix)` where the manager is
   constructed (in the tool wiring AND the daemon, so every spawn path uses
   it). If 01 shipped `SetForkSeed` with a nil default, stub it here — do NOT
   build a second registry.
2. In the manager's fork routing: when `Mode=="fork"` and the hook is set,
   `seed := m.forkSeed(parentConv)`; build the spawn brief with
   `BuildSpawnContext(IsolationSharedTranscript, message, nil, nil, seed)`
   (isolation.go:65) and `RenderSpawnContext(sc)`, then `RunAgent` with that
   brief. When the hook is nil OR `IsolationSharedTranscript` is not yet
   wired into this path, fall back to the fresh `IsolationArtifactOnly` spawn
   (default) and log a Debug — the seed is best-effort-available, never a
   hard requirement for a running daemon.
3. The parent's `*Conversation` must exist at spawn: pull it from the
   registry's active loop for the parent session (the same handle 01 uses to
   stamp `ConversationID` on the record). If the parent loop is not
   reachable as a `*Conversation` (only a message list), pass the seed as
   `[]llm.ChatMessage` to an alternative `SetForkSeed(func([]llm.ChatMessage) []llm.ChatMessage)`
   that 01 may have declared — see 01's T1 note; reconcile at 01/02 review:
   the orchestrator holds both contracts.
4. Verify via `make e2e-fast-area AREA=delegate-seam` — `TestDelegate_ForkSeed` GREEN.

`./bin/meept chat` smoke: `delegate_task` with `mode:"fork"` shows a child
whose first turn references the parent's prior analysis.

### T3: Seed capture-once + freshness

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | The seed is captured ONCE at spawn (not re-computed from the parent's newer history on resume); the child's descriptor carries it (B6) | Modify: internal/agent/delegate_fork.go; Test: internal/agent/delegate_fork_test.go (gate, if present) / delegate-descriptor_test.go | `go test -p 2 ./internal/agent/ -run 'TestCompletedTurnPrefix|TestChildDescriptor' -count=1` | seed recomputes on resume | seed is a snapshot stored with the child, not re-derived | Given a child forked at time T when the parent had N turns; When the parent continues (M>N turns) and the child cold-resumes; Then the child replays the T-time seed, not the parent's M-turn history |

The dsh contract (fork index.ts:73-76): the fork prefix is captured ONCE at
creation and becomes part of the child's durable transcript; cold resume
replays that prefix instead of re-forking the parent's newer history. Make
`completedTurnPrefix` pure (no closure over a live parent) and return a copy
of the slice (`append([]llm.ChatMessage(nil), msgs[:idx]...)`) so later
parent-appends cannot mutate the child's seed. Leaf 06's descriptor carries
the seed payload; this leaf guarantees it is a stable snapshot, not a pointer.

## Self-Verification Checklist

- [ ] `completedTurnPrefix` implemented pure; gate T1 GREEN
- [ ] Fork wiring: `Mode:"fork"` seeds via IsolationSharedTranscript; gate T2 e2e GREEN
- [ ] Seed copied (capture-once, no parent mutation), not re-derived on resume
- [ ] No scope creep — no control tools, no workflow engine (leaves 03/05)
- [ ] All files at exact paths (delegate_fork.go); the only other change is the `SetForkSeed` wiring in 01's manager construction

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path.
2. **Verify command + output:** `go test -p 2 ./internal/agent/ -run TestCompletedTurnPrefix -count=1`; `make e2e-fast-area AREA=delegate-seam` (fork scenario); last ~20 lines.
3. **Deviations:** none / list (esp. conversation-handle vs message-list seed seam if 01 declared the other signature).

## Review Checklist (For Review Agent)

- [ ] completedTurnPrefix matches dsh fork semantics (balanced completed-turn prefix, in-flight turn dropped, empty when none)
- [ ] Seed is captured once and copied (no live-parent closure, no mutation by later appends)
- [ ] Fork route actually seeds the child (IsolationSharedTranscript) and GREEN e2e
- [ ] SetForkSeed wiring is nil-safe and does not change spawn/fork behavior when unset
- [ ] Conventions; no debug artifacts / line-number corruption

Output: APPROVED or specific gaps (file + line).

## Notes

- meept conversation messages carry no explicit `turn/end` marker in the
  message list — the cut is structural (balanced user→assistant, no dangling
  tool call). This is the one deliberate deviation from dsh's event-based
  `turn/end`; document it in a comment so a future versioned-session-log
  (branch 03) can tighten the boundary.
- The fork seam chose `SetForkSeed` over inlining so leaf 01 can ship without
  waiting for this leaf; keep the hook nil-safe and default-to-fresh.
- Root AGENTS.md predicts `llm.StripThinking` on every assistant message —
  reasoning never persists, so the seed inherently excludes thinking blocks,
  which is correct for a fresh child LLM context.
