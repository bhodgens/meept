# WS Wire + Docs + Graphs - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Wire `agent.turn.state` into the WS relay; regenerate connectivity graphs; update the orphan baseline if needed; write the schema doc `docs/workflows/inter-agent-coordination.md`; add the e2e suite manifest entry; wire the ACP turn-state publisher in the daemon.
- **Dependencies:** 02 REVIEWED (turn-state emitters landed), 03 REVIEWED (structured inbox landed)
- **Estimated Context:** 50K
- **Concurrency Group:** A

## Goal

The new vocabulary becomes observable end-to-end: WS clients see `agent.turn.state`, the connectivity graph includes it, the schema is documented, and the e2e suite is registered.

## Context

- `internal/comm/http/server.go:718-756` — the WS relay subscription list (add "agent.turn.state"; the `pair.*` comment block at :716-718 explains the pattern).
- `internal/comm/http/server.go:993` — the `agent.delegate.result` WS discrimination (payload["kind"]=="workflow"); the new topic needs no discrimination (single shape).
- `internal/comm/wsclass` — WSProgress covers TurnStateEvent via its WSClass() marker; no new class needed.
- `scripts/orphan-baseline.txt` — if `agent.turn.state` is flagged (publisher without subscriber or vice versa), add the documented entry.
- `docs/generated/bus-topology.md` — regenerate via `make graphs`.
- `docs/workflows/` — new file `inter-agent-coordination.md` (the schema doc R5).
- `e2e/manifest.json` — add the new suite; `e2e/suites/inter-agent-coord/` — the root 00T leaf writes the test files; this leaf creates the dir + manifest entry if absent.
- `internal/daemon/` — the ACP turn-state publisher wiring (C8): find where acp sessions are created (manager.go / acp_register.go / components) and inject `SetTurnStatePublisher` per session, routing to `agent.publishTurnState` (the daemon imports agent, no cycle).

## Verified Anchors

- `internal/comm/http/server.go:722-756` — relay topics slice (insertion point).
- `internal/comm/http/server.go:320` (handler.go) — WSClass marker pattern (reference only).
- `internal/daemon/acp_register.go:39` — `acp.NewManagerFromFiles` (where sessions get built).
- `internal/acp/manager.go:281-390` — GetOrCreatePersona (session construction; the SetTurnStatePublisher call goes here or at the daemon adapter).
- `e2e/manifest.json` — suite + path_map structure (copy an existing entry's shape).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- WS relay subscription for `agent.turn.state`.
- `docs/workflows/inter-agent-coordination.md` (the R5 schema doc).
- e2e manifest entry `inter-agent-coord` + suite dir.
- Daemon wiring: every acp.Session the daemon creates gets `SetTurnStatePublisher` routed to the bus.

### What This Leaf Consumes

- C6 TopicTurnState (01-01).
- C8 acp.SetTurnStatePublisher (02-02).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | WS relay subscribes agent.turn.state | Modify: `internal/comm/http/server.go` | `go build ./internal/comm/...` | (compile) | PASS: topic in subscription list | Given the relay list; When searched; Then "agent.turn.state" present |
| T2 | Regenerate graphs + orphan baseline | Modify: `docs/generated/*`, `scripts/orphan-baseline.txt` | `make graphs && python3 scripts/orphan-check.py` | orphan-check flags the new topic | PASS: graphs include topic; orphan-check green | Given the new topic; When graphs+orphan-check run; Then topic present + check passes |
| T3 | Schema doc | Create: `docs/workflows/inter-agent-coordination.md` | (docs gate [16]) | n/a | PASS: doc exists with the C1-C5 vocabulary | Given the contracts; When doc written; Then it documents TurnState, StopReason, TurnStateEvent, AgentInboxBody, agent.turn.state |
| T4 | e2e suite manifest entry | Modify: `e2e/manifest.json`; Create: `e2e/suites/inter-agent-coord/` (dir only if root 00T has not created it) | `make e2e-fast` | suite missing | PASS: manifest lists inter-agent-coord; suite runs | Given the manifest; When parsed; Then suite present with path_map entry |
| T5 | ACP turn-state publisher wired in daemon | Modify: `internal/daemon/acp_register.go` or components wiring | `go build ./internal/daemon/...` | (compile) | PASS: sessions get the publisher; nil-safe | Given the daemon builds an acp.Manager; When a session is created; Then SetTurnStatePublisher called with a func routing to the bus |

**T2 expansion:** run `make graphs`; if orphan-check flags `agent.turn.state` (publisher without subscriber — expected until T1's relay lands, or subscriber without publisher until 02-01), add a baseline entry with a one-line reason (same policy as existing entries). If T1 landed first, the topic has both sides and no baseline entry is needed.

**T4 expansion:** copy the shape of an existing manifest entry (e.g. `delegate-seam`): `{"suite": "inter-agent-coord", "paths": ["internal/agent/...", "internal/acp/...", "internal/employee/..."], "build_tags": ["e2e"]}` — match the exact schema the manifest uses (read one entry via search_files, do not guess).

**T5 expansion:** the daemon-side adapter (new small file `internal/daemon/turnstate_wiring.go` or inline in acp_register.go):
```go
// after mgr built:
// sessions are created lazily per GetOrCreatePersona; the cleanest seam is
// a wrapper: when the daemon creates a session for an employee, call
// sess.SetTurnStatePublisher(func(id, state, reason, phase string) {
//     ev := agent.TurnStateEvent{AgentID: id, State: agent.TurnState(state), StopReason: agent.StopReason(reason), Phase: phase, Timestamp: time.Now()}
//     bus.PublishT(msgBus, agent.TopicTurnState, "daemon", ev)
// })
```
Guard msgBus nil.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `make graphs` run; `git status` shows docs/generated changes
- [ ] `python3 scripts/orphan-check.py` green
- [ ] `make e2e-fast` green (or the suite compiles + manifest valid)
- [ ] Schema doc exists
- [ ] `grep -c "agent.turn.state" docs/generated/bus-topology.md` >= 1
- [ ] `go fmt` applied

**DO NOT COMMIT.** (The orchestrator commits; `make graphs` output is committed by the orchestrator.)

## Completion Report Format

1. **Files touched:** [paths — including regenerated docs/generated/*]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list — especially manifest schema adjustments]

## Review Checklist (For Review Agent)

- [ ] WS relay includes agent.turn.state
- [ ] graphs regenerated; orphan-check green
- [ ] Schema doc complete (all vocabulary)
- [ ] e2e manifest entry valid
- [ ] Daemon wiring nil-safe; no import cycle
- [ ] No line-number corruption

Output: APPROVED or specific gaps.