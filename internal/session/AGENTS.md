# internal/session/AGENTS.md

Guidance for AI agents working in `internal/session/`. Referenced from the
root AGENTS.md; update both in the same commit per the root maintenance rule.

## Session lifecycle hooks and teardown

The store exposes a `SessionLifecycleHook`
(`lifecycle.go`: `OnSessionEvent(ctx, sessionID, SessionEvent)`) fired on
archive, delete, and idle events. `SetLifecycleHook` wires ONE hook (the
daemon's teardown coordinator adapter, `internal/daemon/components.go`
`teardownStoreHook`); replace — never stack — hooks (the field is an
atomic pointer: the daemon wires it externally after store
construction), and keep firing best-effort outside the store lock
(`collectLocked` under the lock, `fireLifecycle` after unlock): a hook
failure must never fail the archive or delete caller.

On the DELETE path the row is gone before the hook fires, so identity
resolution via a post-delete store Get structurally cannot work. Both
stores therefore capture the session's ALTERNATE identity (its
conversation id) UNDER THE STORE LOCK before deletion
(`collectLockedWithAlternate`) and hand it to hooks implementing the
richer `SessionLifecycleHookWithAlternate`
(`OnSessionEventWithAlternate(ctx, sessionID, alternateID, event)`);
plain hooks keep the primary-id-only call. The daemon's
`teardownStoreHook` dispatches under both identities so the
accounting-id-keyed teardown snapshot is reachable on delete.

The `ActivityTracker` is evicted on delete (same hook path) and the
idle reaper prunes `idleFired` entries for sessions the tracker no
longer knows — both maps stay bounded instead of growing one entry per
session ever served.

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

## GetMostRecent is the aligned restore policy

`Store.GetMostRecent` (both `SQLiteStore` and `MemoryStore`) returns the
AUTO-RESUME restore target under the policy the GUI ships (commit b7a20820,
`findMostRecentSessionWithHistory` → `findReusableEmptySession`): the most
recent NON-ARCHIVED session WITH history (`leaf_message_id` set), falling
back to the most recent non-archived session when none has history, nil
when only archived sessions exist (callers then create a session). It is
NOT a raw `ORDER BY last_activity DESC LIMIT 1` — a soft-archived or empty
leftover session must never win (bughunt wave2 finding 4; the TUI
auto-resume in `internal/tui/app.go` consumes `session.get_most_recent`
and previously bound exactly those). Pinned by
`TestGetMostRecentRestorePolicy` / `TestMemoryStoreGetMostRecentRestorePolicy`.

## User renames are durable (description_user_set)

`Session.DescriptionUserSet` (column `description_user_set`, additive
migration, legacy rows default 0) marks a USER-authored description.
`Store.UpdateDescription` is the USER rename lane (RPC
`session.update_description`, services/HTTP PATCH description): it stamps
the marker. `Store.UpdateDescriptionAuto` is the AUTO-NAMER lane
(`session.generate_description`, `session.refresh_title`): the SQLite
guard is ONE statement (`WHERE description_user_set = 0`), so a rename
landing between an auto-namer's read and write can never be clobbered;
it returns `(false, nil)` when the rename is preserved and still errors
on a missing session. Auto namers keep updating `Name` on renamed
sessions, and when the description write is skipped they report/publish
the USER's description (never the generated text) so live title events
and RPC responses cannot visually revert a rename. The agent loop's
periodic `triggerTitleRefresh` publishes only (no store write) and
applies the same user-description substitution to its event payload.
Clients additionally re-check for an intervening rename before painting
an async auto-name result (TUI generation counter + placeholder guard,
GUI description-baseline guard).
