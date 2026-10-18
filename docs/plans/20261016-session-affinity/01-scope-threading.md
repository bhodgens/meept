# Scope Threading - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Guarantee a non-empty session scope on every agent-loop LLM
  call, including secondary call sites that bypass `chatWithFailoverRaw`.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** Wave 1

## Goal

Affinity headers can only work if the session ID survives to the client.
Today `chatWithFailoverRaw` (internal/agent/loop.go:6092-6106) prepends
`llm.WithTaskScope(taskID, sessionID)` from `l.turnAccountingSessionID`
under `l.mu.RLock`, but several call sites call `Chat(ctx, messages)` with
no scope: reflection.go:259, rlm_analyzer.go:333, trace_tools.go:669
(`llmClient.Chat(ctx, messages)` — zero opts). Those calls produce empty
`chatOptions.sessionID`, so clients that would send an affinity header
omit it.

## Context

The scope fields live on `AgentLoop` under the mutex (loop.go:6097-6102):
`l.currentTaskID` and `l.turnAccountingSessionID`. `currentSessionID` is a
session-identity field — NEVER use it for accounting or headers on
interactive turns (AUDIT FIX H11, loop.go:6092-6098 comment). Secondary
clients (`ia.client`, `rc.classifier`, `re.llmClient`, etc.) are
`llm.Chatter` values; some are the loop's own `l.llm`, some are
specialist/classifier clients.

Call sites to fix (verify each with search_files before editing — line
numbers drift):

- internal/agent/reflection.go:259 — `re.llmClient.Chat(ctx, messages)`
- internal/agent/rlm_analyzer.go:333 — `a.llmClient.Chat(ctx, messages)`
- internal/agent/trace_tools.go:669 — `t.llmClient.Chat(ctx, messages)`
- internal/agent/intent_analyzer.go:254 — has `opts...`; verify the opts
  actually carry WithTaskScope (they may come from a config path that
  never sets it)
- internal/agent/llm_classifier.go:529 — same check
- internal/agent/reflection_collector.go:99 — same check
- internal/agent/skill_state.go:506,520 — same check

## Interface Contracts (From Parent)

```
// No new public API. The invariant:
// Every code path reaching llm.Client.Chat / ChatWithProgress /
// ChatWithDeltaCallback / ProviderManager chatter carries a ChatOption set
// containing WithTaskScope when the owning loop knows a session:
//   if taskID != "" || sessionID != "" { opts = prepend(llm.WithTaskScope(taskID, sessionID)) }
// Source of taskID/sessionID: the owning loop's fields under l.mu.RLock
// (l.currentTaskID, l.turnAccountingSessionID) or an added explicit
// setter/parameter. NEVER l.currentSessionID for interactive turns.
// When both IDs are empty the header is omitted downstream (clients skip
// empty values) — do NOT invent placeholder IDs.
```

### What This Leaf Exposes

A single helper so the seven call sites share one implementation:

```go
// File: internal/agent/loop_scope.go (new)
package agent

// scopeOptions returns the loop's current task/session scope as prepended
// ChatOptions. Mirrors chatWithFailoverRaw's prepending (loop.go:6092):
// same fields, same lock, same AUDIT FIX H11 restriction. Returns nil when
// both IDs are empty.
func (l *AgentLoop) scopeOptions() []llm.ChatOption {
    l.mu.RLock()
    taskID, sessionID := l.currentTaskID, l.turnAccountingSessionID
    l.mu.RUnlock()
    if taskID == "" && sessionID == "" {
        return nil
    }
    return []llm.ChatOption{llm.WithTaskScope(taskID, sessionID)}
}
```

## Tasks

1. **RED: write the scope test in an EXISTING test file**
   (`internal/agent/loop_scope_invariant_test.go` is a NEW file — the
   e2e-first policy bans new unit test files for features; BUT this leaf's
   helper is trivial glue and the repo makes an exception only for bug
   fixes. Resolution: put the test in the existing
   `internal/agent/loop_test.go`, function
   `TestScopeOptionsPrependsTaskScope`: construct a loop with
   `currentTaskID="task-1"`, `turnAccountingSessionID="session-1"` (same
   way existing loop tests set them), assert `scopeOptions()` returns one
   option whose application sets both values on chatOptions; assert nil
   when both empty. Run it — it must FAIL to compile (helper missing).
2. **GREEN: add `scopeOptions()` in `internal/agent/loop_scope.go`** per
   the contract above. `go test ./internal/agent/ -run TestScopeOptions`.
3. **Thread the helper through the seven call sites.** Pattern for a
   zero-opts site:
   `opts := l.scopeOptions()` (or `owner.scopeOptions()` when the call
   site is not on the loop itself — pass the loop reference or add a
   narrow setter; prefer restructuring the struct field to hold the loop
   pointer the call sites already have), then
   `..., append(opts, existingOpts...)` preserving existing options and
   append order (caller opts first, scope prepended — mirror
   chatWithFailoverRaw: scope FIRST in the slice).
   For sites that already build opts (intent_analyzer, llm_classifier,
   reflection_collector, skill_state): verify whether WithTaskScope is
   already present (grep the opts construction); if present, leave the
   site alone and note it in your report; if absent, prepend scopeOptions.
4. **Verify no regression in accounting:** `go test -p 2 ./internal/agent/...`
   must pass. The AUDIT FIX H11 tests must stay green — scopeOptions reads
   the same fields chatWithFailoverRaw reads, so behavior is identical for
   the main path.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test -p 2 ./internal/agent/...` passes
- [ ] `grep -n "Chat(ctx, messages)" internal/agent/*.go` returns no
      zero-opt call sites inside the agent loop package that originate
      from an owning session loop
- [ ] No use of `currentSessionID` in the new helper
- [ ] No line-number corruption, no TODOs, no debug prints
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Scope options prepend BEFORE caller opts in the slice (append order
  matches chatWithFailoverRaw).
- All seven call sites verified, not just the three obvious ones.
- No change to park/quota/accounting behavior (H11 tests green).
