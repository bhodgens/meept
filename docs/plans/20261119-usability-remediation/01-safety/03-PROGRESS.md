# 03-critical-delivery -- PROGRESS

- Plan: 20261119-usability-remediation / 01-safety / leaf 03
- Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation (HEAD f326aef8)
- Status: COMPLETE (gates GREEN; no commits, per leaf instruction)

## Checkpoints

- [x] Read leaf doc 03-critical-delivery.md
- [x] Read gate tests ONCE: internal/bus/critical_test.go (T5),
      internal/rpc/proxy_critical_test.go (T5/T6, C6 critical ring)
- [x] Locate TurnOutbox / ErrLoopClosed gate references: 00T-GATE-REPORT.md
      lists T6 internal/bus/outbox_test.go, but that file DOES NOT EXIST on
      disk (find/git ls-files: zero hits). ErrLoopClosed guard is owned by
      leaf 05 T4 (05-failure-terminalization.md). Turn outbox/replay RPC is
      leaf 05 T5 per leaf doc Notes. This leaf implements the outbox STORE
      (internal/bus/outbox.go) as the C6 recovery seam; loop guard stays 05.
- [x] bus: IsCriticalTopic + CriticalTopics declared contract (C6 set) in
      internal/bus/critical.go; PublishBlocking already existed (bus.go);
      added PublishBlockingErr + ErrBusClosed for honest closed-bus failure.
      T5 GREEN: go test ./internal/bus/ -run TestBlockingPublishers ok.
- [x] T1: critical ring (cap 256) in rpc poll buffer (busSubscription
      CriticalEvents/MaxCriticalEvents/criticalTrimmed; collector routes
      bus.IsCriticalTopic into the ring; single nextSeq across rings; poll
      merges rings sorted by seq; dropped = trimmed + criticalTrimmed +
      bus channel drops). TestCriticalRingNoTrim GREEN.
- [x] T1b: TestCriticalRingDroppedCountsProgressOnly + NoTrim stall root
      cause: the per-topic collector channel used the bus default buffer
      (100), so a 300-event flood dropped on the BUS side before seq
      assignment (next_seq stuck 247 < 301). Fix: bus.SubscribeWithBuffer
      (explicit per-subscriber channel capacity, falls back to bus
      default, drop accounting unchanged) + rpc collector channel sized
      MaxEvents+MaxCriticalEvents+slack (420). Flood now lands in the
      bounded rings where the trim counts it in `dropped`; identity
      buffered+dropped==published preserved (TestBusPoll* GREEN, -race
      GREEN). Prior PROGRESS note's "assign seq at publish boundary" idea
      NOT needed - channel sizing keeps seq assignment = collector order.
- [x] T2: blocking publishers (task.completed/failed/rejected, plan terminals)
      via publishEvent funnels in strategic.go/tactical.go/plan manager.go +
      direct ralph_loop.go task.failed -> PublishBlocking. T5 bus half GREEN.
- [x] Turn outbox store + wiring (C6 recovery seam: bus/critical.go
      TurnOutbox + publish-side captureTerminalOutbox; ErrLoopClosed
      sentinel declared here, guard itself stays leaf 05).
- [x] T3: TUI reconnect + dropped surface (events.go: re-subscribe on
      'subscription not found' poll error with cursor reset,
      Resubscribed/Dropped on EventStreamDataMsg; app.go: one-per-episode
      system notices). TestResubscribeOnLostSubscription GREEN (x2).
- [x] Gates T5/T6 GREEN; package builds (bus/rpc/plan/tui compile+vet
      clean; internal/agent transiently broken by SIBLING WIP
      loop_park.go/llm.DefaultFailurePolicyConfig, not leaf-03 surface);
      gofmt clean on all leaf files; ASCII; no '_ = '; %w on wrapped
      errors.
- [x] 03-REPORT.md written
- [x] NOT committed (per leaf instruction)

## Gate contract summary (from tests on disk)

- bus.IsCriticalTopic: true for turn.terminal, task.completed, task.failed,
  task.rejected, plan.completed, plan.rejected, plan.failed; false for
  agent.progress, task.progress, step.started, llm.call.
- bus.PublishBlocking onto FULL subscriber buffer delivers after drain
  (bounded wait); plain Publish drops and bumps DroppedCount.
- rpc critical ring: terminal buffered first survives 300-event progress
  flood; merged poll page strictly seq-increasing; terminal keeps original
  seq (1); dropped>0 for progress overflow; critical never counted dropped.
- Existing stream tests (proxy_bus_stream_test.go) pin collection identity
  buffered+dropped==published under bursts incl. bus-level channel drops;
  the seq fix must preserve that identity and zero-drop paced behavior.
