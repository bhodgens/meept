# Provider Teardown Adapters + Coordinator - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** `internal/llm` teardown adapters (llama-server erase, cloud
  no-op), the coordinator subscribing to the session lifecycle hook, and
  the daemon-shutdown synchronous sweep.
- **Dependencies:** leaf 05 (lifecycle hook interface), leaf 02 (config:
  `IdleRelease`, `TeardownTimeout`), leaf 04 (sticky endpoint map tells
  the coordinator which endpoint a session used)
- **Estimated Context:** 65K
- **Concurrency Group:** Wave 2 (dispatch after 05 merges; may start
  against frozen contracts if 02/04 are late — the shapes are pinned in
  this leaf)

## Goal

When a session ends, release its upstream resources. For local
llama.cpp/llama-server endpoints that means the KV-cache erase route.
Cloud providers have no session API — their no-op exists as explicit
code so the decision is visible.

## Context

- Local runtime endpoints: `internal/llm/runtime_manager.go` owns
  llama-server lifecycle (autostop at :417, `StopAll`). The HTTP route
  for KV erase lives in the pinned llama.cpp build under
  `$MEEPT_HOME/deps/llama.cpp` — VERIFY the exact route + method against
  that checkout at implementation time (`grep -rn "cache/erase\|slots" `
  there) and put the route in ONE constant.
- Reference counting: leaf 04's sticky map (and the runtime manager's own
  session knowledge, if present) tells whether OTHER active sessions
  still map to the same runtime. Only erase when the count drops to zero;
  never erase a shared KV cache out from under a live session.
- Daemon wiring: `internal/daemon/components.go` — construction order is
  store → runtime manager → clients; wire the coordinator after all
  three; register it as the store's `SetLifecycleHook`.
- Shutdown: hook into the existing `StopAll` path
  (runtime_manager.go:417) or the daemon shutdown sequence — the sweep
  is synchronous with a bounded total budget (sum of per-call
  TeardownTimeout, capped).

## Interface Contracts (From Parent)

### Contract 6

```go
// File: internal/llm/teardown.go (new)
package llm

type TeardownAdapter interface {
    Handles(endpointKey string) bool
    // Best-effort: bounded by TeardownTimeout, returns error, never
    // panics. Callers log and continue.
    Teardown(ctx context.Context, sessionID, endpointKey string) error
}

// LlamaServerTeardown: POST {base}<eraseRoute> for endpoints it handles.
//   Skips (returns nil, logged) when other active sessions still map to
//   the same runtime endpoint.
// CloudTeardown: no-op returning nil — explicit type for the "cloud has
//   nothing to release" decision.
// TeardownCoordinator: implements session.SessionLifecycleHook.
//   OnSessionEvent fans out async (per-event goroutine, bounded by
//   TeardownTimeout) to every adapter whose Handles(endpointKey) is true,
//   where endpointKey comes from the sticky/session-endpoint map.
//   ShutdownSweep(ctx) runs synchronously for all known sessions.
```

### What This Leaf Exposes

- `TeardownAdapter`, `LlamaServerTeardown`, `CloudTeardown`,
  `TeardownCoordinator`, and daemon wiring that makes the whole chain
  live: session archive → hook → coordinator → adapter → erase.

## Tasks

1. **RED:** in an existing internal/llm test file (or a new
   `teardown_test.go` ONLY if no file is a natural home — prefer
   extending `runtime_manager_test.go`):
   - `TestLlamaTeardownErases`: httptest stub server recording the erase
     request; adapter Teardown hits it with the right route/method;
     timeout path returns error without panic.
   - `TestCoordinatorFansOut`: fake hook source + recording adapters;
     fire archive for a session mapped to endpoint A; assert only
     A-matching adapters called; assert cloud adapter called with the
     cloud endpoint key and returns nil.
   - `TestSharedRuntimeSkip`: two sessions mapped to one endpoint;
     archive one; assert NO erase (refcount > 0); archive the second;
     assert erase.
   Fails first.
2. **GREEN:** implement adapters + coordinator. The session→endpoint map
   source: consume leaf 04's `PreferredEndpoint`/map access (add a narrow
   resolver accessor if needed, e.g. `SessionEndpoints() map[string]string`
   snapshot, mutex-safe copy) — do not duplicate session bookkeeping.
3. **Shutdown sweep:** on `StopAll` (or the daemon shutdown hook),
   run `ShutdownSweep` synchronously with total budget
   `TeardownTimeout * min(8, len(sessions))` — bound it, log overruns.
4. **Daemon wiring:** components.go — construct coordinator with
   adapters + resolver ref + config; `sessionStore.SetLifecycleHook(coordinator)`;
   `coordinator.SetIdleRelease(cfg...)` from `llm.session_affinity`;
   shutdown registration. Follow existing component wiring order rules.
5. **Verify:** `go build ./...`;
   `go test -p 2 ./internal/llm/... ./internal/daemon/...`;
   `go vet ./internal/llm/... ./internal/daemon/...`.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test -p 2 ./internal/llm/... ./internal/daemon/...` passes
- [ ] Erase route verified against the pinned llama.cpp checkout (report
      the route string + file you verified it in)
- [ ] Shared-runtime refcount prevents erasing a live session's cache
- [ ] Cloud adapter is an explicit no-op type
- [ ] Teardown errors never fail a turn, an archive, or shutdown
      (shutdown logs overruns, still exits cleanly)
- [ ] No goroutine leak: per-event goroutines bounded by timeout, sweep
      synchronous
- [ ] No line-number corruption, no TODOs
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Route constant verified against the real llama.cpp source, single
  definition.
- No teardown call holds a lock that could deadlock with the resolver or
  store locks (check lock ordering: coordinator takes snapshots, never
  calls back into locked methods).
- Shutdown budget is actually bounded (trace the worst-case path).
