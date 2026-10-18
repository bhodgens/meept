# Session Lifecycle Hook + Idle Reaper - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Direct Go lifecycle hook on the session store (archive /
  delete / idle) plus the idle reaper. NO bus topic, NO WS surface.
- **Dependencies:** none for the hook itself; consumes
  `SessionAffinityConfig.IdleRelease` from leaf 02 (thread the value in
  via setter — start with it as a setter parameter so this leaf can land
  in Wave 1 without 02)
- **Estimated Context:** 55K
- **Concurrency Group:** Wave 1

## Goal

Sessions end (archive, delete, idle, daemon stop) but nothing tells the
LLM layer. Upstream resources (llama-server KV slots) stay pinned. This
leaf provides the notification surface; leaf 06 subscribes to it.

## Context

- `internal/session/store.go` — the `Store` interface. Both
  `MemoryStore` (session.go) and `SQLiteStore` (store_sqlite.go) implement
  it. Existing archive/delete methods are the fire points.
- `internal/session/activity_tracker.go` already tracks last-activity per
  session (`state.SessionID`, `:37` context). Reuse its data for the idle
  reaper; do not build a second tracker.
- Repo invariants: every `Set*` needs a typed-nil guard; hook errors never
  propagate (teardown is best-effort); no mutex held across the hook call
  — collect the hook + session ID under lock, invoke outside.

## Interface Contracts (From Parent)

### Contract 5

```go
// File: internal/session/lifecycle.go (new)
package session

type SessionEvent string
const (
    SessionEventArchive SessionEvent = "archive"
    SessionEventDelete  SessionEvent = "delete"
    SessionEventIdle    SessionEvent = "idle"
)

// Direct Go callback, not a bus topic (WS surface must not change;
// make graphs stays green).
type SessionLifecycleHook interface {
    OnSessionEvent(ctx context.Context, sessionID string, event SessionEvent)
}
```

Store integration:

```go
// session.Store interface gains:
//   SetLifecycleHook(SessionLifecycleHook)   // typed-nil guard
// SQLiteStore + MemoryStore fire the hook on archive and delete paths
// (fire-and-forget semantics: log Warn on hook error, never propagate).
// The hook receives ONLY the id + event — loading the session for the
// hook is the subscriber's job.
```

Idle reaper:

```go
// Ticker (default 1m) compares now-lastActivity against the configured
// IdleRelease duration and fires SessionEventIdle once per idle episode.
// Re-arm on new activity (activity tracker already records it). Runs
// under the store's Start/Stop lifecycle (find how activity_tracker is
// started — mirror it).
```

### What This Leaf Exposes

- `SessionEvent`, `SessionLifecycleHook`, `SetLifecycleHook` on the
  Store interface + both implementations.
- Idle reaper firing `SessionEventIdle` after the configured quiet period.
- A small `SetIdleRelease(d time.Duration)` (typed-nil-guard style
  setter, zero value disables reaping) so daemon wiring configures it
  from `llm.session_affinity.idle_release` in leaf 06.

## Tasks

1. **RED:** add tests to an existing internal/session test file:
   `TestLifecycleHookFiresOnArchive` / `...OnDelete`: register a fake
   hook, archive + delete a session, assert one call each with the right
   id + event; assert a hook that returns/logs an error does not fail the
   Archive call; assert `SetLifecycleHook(nil)` is a no-op. Fails first.
2. **GREEN:** `internal/session/lifecycle.go` + interface method + both
   store implementations. Fire points: the existing Archive/Delete (and
   any batch-delete) methods — search for all of them; a delete path
   that skips the hook is a silent leak.
3. **Idle reaper (RED→GREEN):** test with a fake clock or a short
   `SetIdleRelease` + short ticker interval (make the interval a field
   for tests); assert exactly ONE idle event per idle episode and a
   re-armed event after activity resumes and lapses again.
4. **Interface ripple check:** `grep -rn "session.Store" --include="*.go"`
   for mock/fake stores implementing the interface — every one needs the
   new method or compilation breaks. Fix each (one-line no-op + hook
   field is fine for test doubles).
5. **Verify:** `go build ./...`; `go test -p 2 ./internal/session/...`;
   `go vet ./internal/session/...`.

## Self-Verification Checklist

- [ ] `go build ./...` clean (all mock stores updated)
- [ ] `go test -p 2 ./internal/session/...` passes
- [ ] Hook errors never propagate to Archive/Delete callers
- [ ] No mutex held across hook invocation
- [ ] `SetLifecycleHook(nil)` safe (typed-nil guard)
- [ ] Idle event fires once per episode, re-arms correctly
- [ ] No bus topic added (`git diff` shows no internal/bus or internal/comm
      changes — `make graphs-check` stays green)
- [ ] No line-number corruption, no TODOs
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Every archive/delete/batch-delete path fires the hook (enumerate them
  in the leaf report).
- No new bus/WS surface.
- Reaper lifecycle ties to the store's existing start/stop (no leaked
  ticker on shutdown).
