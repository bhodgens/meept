# Live-Tail Event Ring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A bounded in-memory ring buffer in internal/activity that
  subscribes to live bus topics and serves the newest N activity events
  (activity.recent backing store).
- **Dependencies:** 01-aggregator.md (ActivityRecord type)
- **Estimated Context:** 55K
- **Concurrency Group:** A (parallel with 01 and 07)

## Goal

Stored history shows what happened; the live tail shows what is happening.
This leaf buffers recent bus events (queue jobs, task completions, agent
progress, turn terminals) in a fixed-capacity ring inside the daemon, so
`activity.recent` can answer "the last 50 events, plus how many were
dropped" with zero I/O.

## Context

The daemon publishes progress on bus topics (raw string publishes for most;
one typed topic for turn terminals). The RPC layer has NO server push, so
clients poll `activity.recent`; the ring is the poll's backing store. The
daemon-side ring is NET-NEW (only a client-side TUI buffer exists today).
Subscriptions must be best-effort: a slow consumer must never block a bus
publisher.

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- internal/queue/queue.go:336,447,511,580,609,643,701 - queue.job.claimed/completed/failed/retry/requeue/recovered publishers - topics to subscribe.
- internal/agent/topics.go:14 - turn.terminal as typed bus.Topic[agent.TurnTerminalEvent] (SubscribeT).
- docs/generated/bus-topology.md:20-21 - agent.progress + agent.progress.synthesized payload keys (agent_id, conversation_id, stage, text_so_far, token_count).
- internal/comm/wsclass/wsclass.go:17-41 - WS event classification - shows which topics classify as agent_progress (useful for payload shaping).
- internal/daemon/components.go:2126,2156 - existing progress-synthesizer bus subscriptions - the pattern for daemon-side subscribers.
- internal/activity/types.go (leaf 01) - ActivityRecord is the ring's element type.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/activity/ring.go - C3 (byte-identical to master)
func NewRing(capacity int) *Ring
func (r *Ring) Append(rec ActivityRecord)          // drops oldest; mutex-guarded
func (r *Ring) Tail(n int) ([]ActivityRecord, int) // newest-last, dropped total
// internal/activity/busbridge.go
func WireBusBridge(bus busLike, r *Ring) // subscribes; every handler converts
// its payload to ActivityRecord (Kind "bus_event") and Append()s in a
// goroutine-safe, non-blocking way; panics in handlers recovered+logged.
// busLike is a minimal local interface (Subscribe/SubscribeT-compatible)
// so tests inject fakes - keep internal/activity free of internal/bus import
// IF wiring allows; otherwise take concrete *bus.Bus and use fakes at the
// daemon-wiring level instead (report which path you took).
```

### What This Leaf Consumes

- C1 ActivityRecord (leaf 01)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Ring: Append/Tail/dropped-counter, capacity eviction, mutex safety | Create: internal/activity/ring.go; Test: internal/activity/ring_test.go | `go test -p 2 ./internal/activity/ -run TestRing -race` | Ring undefined | eviction drops oldest; Tail(n) returns newest-last; dropped counts every evicted+overflowed event; -race clean with concurrent Append+Tail | Given capacity 3; When Append 5; Then Tail(10)=3 newest, dropped=2 |
| T2 | Bus bridge: subscribe queue.job.*/task.*/agent.progress/turn.terminal, map payloads to ActivityRecord | Create: internal/activity/busbridge.go; Test: internal/activity/busbridge_test.go | `go test -p 2 ./internal/activity/ -run TestBusBridge -race` | bridge undefined | each topic maps to the right Kind/AgentID/TaskID/Summary; handler panic recovered (ring survives); summary <=200 runes | Given fake bus with a published agent.progress; When wired; Then ring contains one bus_event record with agent_id set |
| T3 | Daemon wiring: construct ring with capacity, call WireBusBridge after bus exists | Modify: internal/daemon/components.go (or new activity_wiring helper called from it); Test: internal/daemon/activity_wiring_test.go (shared with leaf 02 - create only if absent) | `go test -p 2 ./internal/daemon/ -run TestActivityWiring` | wiring absent | ring exists on Components; bridge subscribed; capacity configurable (default 1000) | Given daemon components test double; When built; Then Ring non-nil and Tail(1) works |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C3 signatures)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] All handlers non-blocking: no publisher can stall on the ring
- [ ] `-race` clean on ring + bridge tests

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C3 signatures byte-identical to master
- [ ] Panic recovery in every bus handler (one bad payload cannot kill the
      subscription)
- [ ] Bounded memory: capacity respected, dropped counter truthful
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep summaries SHORT at capture time (first line of text_so_far / detail,
  200-rune cap) - the ring holds at most 1000 entries but they should stay
  small regardless.
- Typed turn.terminal subscription uses bus.SubscribeT; raw string topics
  use Subscribe. The connectivity-graph generator does NOT see SubscribeT
  sites - expect the regenerated graph to under-report; that is known, not
  a defect (note it in the report).
- Memory ceiling: 1000 records x ~1KB = ~1MB - fine; do not "optimize" with
  a channel-based design that can block publishers.
