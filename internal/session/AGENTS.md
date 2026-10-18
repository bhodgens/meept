# internal/session/AGENTS.md

Guidance for AI agents working in `internal/session/`. Referenced from the
root AGENTS.md; update both in the same commit per the root maintenance rule.

## Session lifecycle hooks and teardown

The store exposes a `SessionLifecycleHook`
(`lifecycle.go`: `OnSessionEvent(ctx, sessionID, SessionEvent)`) fired on
archive, delete, and idle events. `SetLifecycleHook` wires ONE hook (the
daemon's teardown coordinator adapter, `internal/daemon/components.go`
`teardownStoreHook`); replace — never stack — hooks, and keep firing
best-effort outside the store lock (`collectLocked` under the lock,
`fireLifecycle` after unlock): a hook failure must never fail the archive
or delete caller.

The `IdleReaper` (also `lifecycle.go`) reuses the shared `ActivityTracker`
— the same data the LLM call path's activity callback feeds; a tracker fed
by nothing makes idle reaping dormant. It fires `SessionEventIdle` exactly
once per idle episode and re-arms on fresh activity. Its ticker is fixed at
1 minute (`defaultIdleCheckInterval`) — `idle_release` (the
`llm.session_affinity.idle_release` knob) is the quiet period, not the tick
period, so a short `idle_release` still lands within about a minute. Idle
coverage lives in this package's unit tests; the daemon-level erase proofs
live in `e2e/suites/session-teardown/` (green — the leaf-07 key-shape and
wiring gaps they documented are fixed; see `internal/llm/AGENTS.md`).

The accounting session id (`session_id` vs `conversation_id` duality, root
AGENTS.md) is the key every downstream consumer — sticky endpoint map,
teardown snapshot, budget scopes — shares with the LLM layer; never mint a
second identity for lifecycle events.
