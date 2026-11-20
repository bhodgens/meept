# 03-critical-delivery -- COMPLETION REPORT

- Plan: 20261119-usability-remediation / 01-safety / leaf 03
- Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
- Status: complete (NOT committed, per leaf instruction; orchestrator commits)

## 1. Files touched (this leaf)

- internal/bus/critical.go (NEW): IsCriticalTopic + CriticalTopics (exact C6
  set: turn.terminal, task.completed, task.failed, task.rejected,
  plan.completed, plan.rejected, plan.failed), PublishBlockingErr +
  ErrBusClosed, TurnOutbox store (bounded 256, drop-oldest with explicit
  accounting, ErrLoopClosed sentinel shared with leaf 05),
  MessageBus.TurnOutbox() lazy accessor + publish-side capture
  (captureTerminalOutbox runs BEFORE delivery attempts so a full-buffer or
  subscriber-less publish still leaves a recoverable terminal).
- internal/bus/bus.go: turnOutbox/outboxMu fields on MessageBus; capture
  wired into publish(); PublishBlocking (5s bounded wait, Error-logged on
  timeout); SubscribeWithBuffer(id, topic, bufferSize) -- explicit
  per-subscriber channel capacity (<= 0 falls back to bus default), the
  seam the rpc collector uses to stop default-100 bus-side overflow.
- internal/rpc/proxy.go: C6 critical ring in busSubscription
  (CriticalEvents / MaxCriticalEvents 256 / criticalTrimmed); collector
  routes bus.IsCriticalTopic records into the ring, exempt from the
  MaxEvents drop-oldest trim; single nextSeq feeds BOTH rings so merged
  poll pages stay strictly seq-increasing and a terminal keeps its
  original assignment seq; poll merges + sorts rings; dropped =
  trimmed + criticalTrimmed + bus channel drops (critical delivered is
  never counted dropped). Collector channels sized
  MaxEvents+MaxCriticalEvents+64 via SubscribeWithBuffer (root cause of
  the next_seq=247 stall: default-100 channel dropped flood events on the
  BUS side before seq assignment; oversized channel moves floods into the
  counted ring path, identity buffered+dropped==published preserved).
- internal/agent/strategic.go, internal/agent/tactical.go: publishEvent
  routes critical topics through PublishBlocking (per-task-once
  terminals: completed/failed/rejected); all other events unchanged.
- internal/agent/ralph_loop.go: task.failed publish -> PublishBlocking.
- internal/plan/manager.go: publishEvent routes critical topics
  (plan.completed/rejected/failed) through PublishBlocking.
- internal/tui/events.go: poll "subscription not found" -> re-run
  subscribe() (cursor reset to 0 -- fresh subscription restarts daemon
  seq), pendingResubscribe flag surfaced as
  EventStreamDataMsg.Resubscribed; poll decodes `dropped` and surfaces it
  on EventStreamDataMsg.Dropped (loss never silent end-to-end).
- internal/tui/app.go: EventStreamDataMsg handler renders one-per-episode
  system notices for reconnect and for NEW drop increases
  (lastDroppedNotice tracks the cumulative counter).

## 2. Verify output (all actually run)

- go test ./internal/rpc/ -run 'TestCriticalRing|TestBusStream|TestBusPoll|TestSubscriber'
  -count=2 -> ok (TestCriticalRingNoTrim, TestCriticalRingDroppedCountsProgressOnly,
  TestBusPollSeqCursorNeverSkips, TestBusPollDropAccounting,
  TestBusPollStressFastProducerSlowPoller, TestBusPollStressPacedProducerZeroDrops,
  TestSubscriberDropCounter all GREEN).
- go test ./internal/bus/ -count=1 -> ok (incl. TestBlockingPublishers*,
  TestTurnOutbox*, TestCritical*).
- go test ./internal/tui/ -run TestResubscribe -count=2 -> ok
  (TestResubscribeOnLostSubscription GREEN -- was the T6 RED gate).
- go test ./internal/rpc/ -run ... -race -> ok.
- go vet ./internal/bus/ ./internal/rpc/ -> clean.
- gofmt: all leaf files clean. ASCII-only comments in the new code.

## 3. Gate T5/T6 adjudication

- T5 rpc half (TestCriticalRingNoTrim / DroppedCountsProgressOnly): GREEN.
- T5 bus half (TestBlockingPublishers*): GREEN (prior child + this fix).
- T6 (TestResubscribeOnLostSubscription): GREEN.
- proxy_bus_stream identity contract (buffered+dropped==published,
  zero-drop paced, seq cursor never skips): GREEN -- not weakened; the
  channel sizing deliberately keeps bus-level DroppedCount in the poll
  `dropped` sum so identity holds even beyond the 420-slot channel.

## 4. Deviations / notes

- Turn outbox store lives in internal/bus/critical.go (not outbox.go as
  the gate report guessed; the file never existed on disk).
- ErrLoopClosed guard (closed-loop rebuild) stays leaf 05 T4; this leaf
  only declares the sentinel + outbox store seam (per leaf doc Notes:
  turn.result query RPC is 05 T5).
- Live-gate (rig under 3 concurrent tasks, kill daemon mid-turn): NOT run
  -- needs a real rig session; offline evidence is the T6 reconnect pin
  driving a real unix-socket RPC client against a mock daemon.
- Worktree is SHARED with sibling leaves: during this leaf's run the tree
  was rewound by a sibling commit + stash (bf3918e6) and my bus.go,
  proxy.go and agent edits were re-applied from it; ralph_loop.go lost
  its blocking publish and was re-applied. internal/agent was briefly
  unbuildable mid-flight on a SIBLING's edit (loop_park.go referencing
  llm.DefaultFailurePolicyConfig before its llm half landed); the
  sibling's half landed and the FINAL `go build ./...` is clean.
  internal/agent/ralph_storm_test.go is a sibling untracked file
  (not gofmt-clean; not leaf-03 surface, left alone).
- queue_recovery.go sibling contract: PublishBlocking returns int and the
  survivor-fixed call sites compile; final `go build ./...` is clean.
