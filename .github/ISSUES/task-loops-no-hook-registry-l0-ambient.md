# Task-scoped agent loops do not inherit the hook registry — L0 recording and ambient extraction silently skipped for dispatched turns

Found: 2026-10-13 live scratch-daemon gate (memory capability forest follow-up).
Status: OPEN — product decision needed before fixing.

## The gap

`internal/agent`'s loop factory (`createLoop` path in registry.go) does not
propagate the primary loop's `HookRegistry` to task-scoped agent loops. The
memory forest's L0 recorder and the epistemic ambient extractor are both
session-end hook registrations on that registry
(`internal/daemon/l0_wiring.go`: `RegisterSessionEndHook("l0_record", ...)`).

Consequence: with `multiagent.enabled=true` (the default), every turn that
routes through a dispatched task loop records NO L0 lines and runs NO ambient
extraction. Live observation 2026-10-13: a direct-mode turn (multiagent
disabled) recorded L0 + claims correctly; the subagent could not observe the
same for dispatched turns because the hook registry simply is not there.

## Why it went unnoticed

- The forest's e2e suites drive direct-mode loops (the harness has no
  dispatcher), so hook coverage looked green.
- The gap predates the forest: ambient extraction had the same hole — it
  would also not fire on dispatched turns. The forest did not create it,
  but L0 makes it visible (raw lines are directly inspectable on disk).

## Options

(a) Share the registry: createLoop clones the primary registry (hooks are
    read-mostly after boot; concurrent firing needs a lock audit first).
(b) Per-loop registries wired at dispatch: task loops get their own registry
    with the same daemon-installed hooks (keeps isolation; duplication risk).
(c) Move L0/ambient out of hooks: fire from a turn-terminal bus subscriber
    instead (loop-agnostic, matches the async-turn architecture; biggest
    change).

Recommendation: (a) with a concurrency audit of HookRegistry firing under
parallel loops; (c) is the architecturally clean long-term answer given the
turn-terminal topic already exists.

## Repro sketch

multiagent.enabled=true → dispatch a task turn whose conversation contains an
extractable fact → observe: no new lines in <memory.data_dir>/l0/*.jsonl and
no status=auto claim, while the same turn in direct-mode records both.
