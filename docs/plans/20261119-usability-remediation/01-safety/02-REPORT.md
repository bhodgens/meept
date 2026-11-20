# 01-safety/02-queue-lifecycle -- Implementation Report (orchestrator-completed)

Status: PARTIALLY GREEN + GATE-DEFECT documented. Child sa-0-3bb321ed wrote the
core C3 code then hung (2h, no 429); orchestrator stopped it, verified the on-disk
work, completed the remaining unambiguous T4 items, and documented a gate-design
contradiction requiring adjudication (see GATE-ADJUDICATION-T3.md).

## Files touched
- internal/agent/loop.go (child): RunOnceWithParts ~3490 registers a FRESH
  MessageQueue per turn (inherits config/bus/agent-id/logger), re-points l.queue
  at the live turn queue; turn-end defer UnregisterActiveQueue closes it.
  ConfigSnapshot ~10123: clone isolation closure replaces WithMessageQueue(l.queue)
  -- each session clone gets its own queue instance.
- internal/agent/registry.go (child): GetActiveQueue + GetActiveQueueLoop use
  resolveActiveQueueLocked (exact -> thread-unwrapped base -> session-owned
  thread keys; ambiguous = ErrAmbiguousQueue, never silent pick).
- internal/agent/queue_resolve.go (child, NEW): ResolveActiveQueue error-carrying
  twin + ErrAmbiguousQueue + unwrapThreadConversationID/isThreadKeyFor helpers.
- internal/agent/dispatcher.go (orchestrator): RouteToAgent steer/follow-up
  injection now uses result.OriginalInput (full text), falling back to
  Intent.Summary only when OriginalInput empty. Fixes 100-char truncation (W2).
- internal/comm/http/api_handlers.go (orchestrator): handleServiceError maps
  agent.ErrQueueNotFound -> 404 (was default 500). Client-correctable idle-steer
  state, actionable reason. Added agent import (no cycle; http already imports agent).

## Verification (orchestrator independent runs)
- go build ./... : OK
- T4 thread-remap: TestThreadRemapLookup/AmbiguousErrors/ExactWins/NotFound all PASS.
- TestSteerFullText: PASS (full 423-char OriginalInput injected, not 100-char Summary).
- Regression: TestSteer|TestFollowUp|TestRouteToAgent|TestQueue ok; http
  TestWSSchema|TestSteer|TestQueue ok.
- gofmt clean; added-lines ASCII clean.
- Empirical C3 proof (debug tests, removed): fresh-distinct=true,
  openDuringTurn1=true, openDuringTurn2=true, t1ClosedAfter=true, t2ClosedAfter=true.

## GATE DEFECT (RESOLVED 2026-11-20 - option A approved; GATE-ADJUDICATION-T3.md)
TestFreshQueuePerTurn:84-87 + TestCloneQueueIsolation:159 asserted turn2Queue is
OPEN and Steer-able AFTER both RunOnce calls return. But the turn-end defer
legitimately closes the queue, and committed TestRegistry_UnregisterActiveQueue_ClosesQueue
REQUIRES close-on-unregister. Gate now asserts the open/steerable contract
MID-TURN (the F-NEW-11 window), keeps distinct-pointer + old-queue-stays-closed
pins, adds a turn-2 close-on-unregister pin. Mutation-verified: reverts to the
original defects are caught. Impl was and stays correct.

## Remaining (leaf-doc extras, NOT gated -- no gate test files exist)
- DrainFollowUp persisted-row delete (queue.go:348 + persister INSERT-only).
  No TestDrainDeletes gate file exists; deferred to a follow-up leaf.

## Deviations
- Child hung post-tool-result (not 429); orchestrator stopped + completed.
- 2 of 3 T3 gate tests fail due to gate-design contradiction, NOT impl defect.