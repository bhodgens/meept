# Gate-Design Adjudication: 01-safety T3 TestFreshQueuePerTurn / TestCloneQueueIsolation

Status: RESOLVED - OPTION A APPROVED by user 2026-11-20 and applied.
The two gate tests now assert the open/steerable contract MID-TURN inside the
spy hook (where F-NEW-11 applies), keep the distinct-pointer and
old-queue-stays-closed pins, and ADD a close-on-unregister pin for turn 2
(harmonized with TestRegistry_UnregisterActiveQueue_ClosesQueue).
Mutation proof (in-memory overlay, originals untouched):
- Reverting to shared-l.queue registration -> TestFreshQueuePerTurn FAILS
  with 'CLOSED mid-turn' + 'SAME queue object'.
- Re-adding WithMessageQueue(l.queue) to ConfigSnapshot ->
  TestCloneQueueIsolation FAILS with 'share ONE queue pointer'.
- Real tree: 10/10 gate battery PASS (fresh/clone/steer/remap x4/
  unregister x3).
Below is the original finding, kept for the record.

## The contradiction (empirically verified)
The C3 implementation in loop.go RunOnceWithParts (~3490) is FUNCTIONALLY CORRECT:
- Fresh DISTINCT MessageQueue per RunOnce (turn1 != turn2 pointer).
- Queue is OPEN during the turn (spy hook captures closed=false mid-turn).
- Turn-end defer UnregisterActiveQueue CLOSES the queue (one-way CAS).
- Clone isolation: ConfigSnapshot no longer carries WithMessageQueue(l.queue);
  each clone gets its own instance (loop.go ~10123).
- T4 thread-remap tests all PASS (TestThreadRemapLookup/AmbiguousErrors/ExactWins/NotFound).

Empirical proof (orchestrator debug tests, since removed):
  fresh-distinct=true openDuringTurn1=true openDuringTurn2=true
  t1ClosedAfter=true t2ClosedAfter=true
  post-turn Steer(t2) err=queue is closed

## Why the gate fails
TestFreshQueuePerTurn (queue_lifecycle_test.go:84-87) captures turn2Queue via the
spy hook DURING turn 2, but asserts AFTER both RunOnce calls return:
  - turn2Queue.IsClosed() == false   (line 84)
  - turn2Queue.Steer(ctx,...) == nil (line 87)
By that point turn 2's defer has CLOSED turn2Queue. So the assertion is
unsatisfiable GIVEN close-on-unregister.

TestCloneQueueIsolation:159 same shape (queueB.IsClosed() after both turns).

## The conflicting committed contract
internal/agent/registry_queue_test.go:85 TestRegistry_UnregisterActiveQueue_ClosesQueue
REQUIRES: after UnregisterActiveQueue, q.IsClosed()==true. This is a pre-existing
committed test (HEAD). Close-on-unregister is load-bearing (rejects in-flight
injection after turn end, S1-15).

## Leaf doc INTENT (02-queue-lifecycle.md T1 Given/When/Then)
"Given cached loop turn 2; When steer; Then open queue accepts; old queue object
stays closed (no reopen-in-place)."
The INTENT is mid-turn steering on turn 2 ACCEPTS (F-NEW-11: steer to ACTIVE turn
rejected 'queue not found'). It does NOT require post-turn steering into a dead
turn's queue. The gate test encoded the intent incorrectly (asserts post-turn).

## Resolution options (pick one)
A. FIX THE GATE (recommended): move the IsClosed/Steer assertions INTO the spy
   hook (mid-turn), where the queue is live. Keep "old queue object stays closed"
   as a post-turn assertion on turn1Queue only. This matches the leaf intent and
   preserves close-on-unregister. Gate author error, not impl error.
B. Change lifecycle so the registered queue is NOT closed on unregister (only
   removed from map). BREAKS TestRegistry_UnregisterActiveQueue_ClosesQueue and
   S1-15 in-flight rejection. NOT recommended.
C. Keep a separate "live turn queue" open after turn end for late steering.
   Adds complexity + risks steering into a finished turn (wrong work stream).

## Orchestrator decision
PENDING. The implementation should NOT be weakened to satisfy a contradictory
gate. Recommend option A (gate fix) with user sign-off, since gate edits are
normally forbidden mid-impl. The mid-turn steering path is provably correct.

## What IS done and GREEN (01-safety/02)
- T4 thread-remap: 4/4 PASS.
- Fresh-per-turn + clone isolation: functionally correct (empirically proven).
- Remaining impl work: T4 full-text injection (dispatcher.go:3180/3194 uses
  Intent.Summary, must use OriginalInput), ErrQueueNotFound->HTTP 404,
  DrainFollowUp persisted-row delete.