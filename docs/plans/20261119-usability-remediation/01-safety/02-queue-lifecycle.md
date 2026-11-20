# Steering Queue Lifecycle - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T3/T4) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Steering works on every turn, per-session, thread-resolved, full-text
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 85K
- **Concurrency Group:** A
- **Audit references:** W2, W23; F-ORCH-23/27; F-NEW-11 (steer to ACTIVE
  turn rejected "queue not found" on both ids - mid-run steering unusable);
  F-NEW-12 (queue died mid-work, partial files, no terminal);
  session-steering B2/B3/B4/B8; audit-routing top-10 #3/#4/#10

## Goal

Fix the four steering root causes: (a) queue closed after turn 1 never
reopened, (b) template-clone sessions share one queue, (c) lookups never
unwrap thread ids, (d) injection truncates to 100-char Summary. Plus map
ErrQueueNotFound to HTTP 404 and delete consumed follow-ups.

## Context

- internal/agent/queue.go:400-403 - Close one-way CAS; Steer/FollowUp return
  ErrQueueClosed after close.
- internal/agent/loop.go:3480-3528 - RunOnce registers l.queue under thread
  conv id, defer unregisters+closes. loop.go:10107 - ConfigSnapshot includes
  WithMessageQueue(l.queue) (shared pointer).
- internal/agent/manager.go:153 - clones built from ConfigSnapshot.
- internal/agent/registry.go:2078-2088 - GetActiveQueue exact map lookup.
- internal/session/thread_resolve.go:95 - ResolveThreadConversationID helper.
- internal/agent/dispatcher.go:3180/3194 - queue.Steer(ctx, result.Intent.Summary,...)
  injects truncated summary; extractSummary at :4206.
- internal/rpc/queue.go:61-67 - steer handler; internal/services/chat_service.go:333-353.
- internal/comm/http/api_handlers.go:222-224 - handleServiceError default 500.
- internal/agent/queue.go:348-371 - DrainFollowUp never deletes persisted rows;
  queue_persister.go:222-247 INSERT-only.

## Verified Anchors

- internal/agent/queue.go:400 - Close CAS
- internal/agent/loop.go:3513 - register same l.queue per turn
- internal/agent/loop.go:10107 - WithMessageQueue(l.queue) in ConfigSnapshot
- internal/agent/registry.go:2078 - GetActiveQueue
- internal/agent/dispatcher.go:3180 - queue.Steer(ctx, result.Intent.Summary, ...)
- internal/comm/http/api_handlers.go:222 - default 500 branch

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C3: fresh queue per RunOnce; clone isolation; resolution order exact ->
  unwrapped base -> session-owned thread keys (ambiguous = error);
  ErrQueueNotFound -> HTTP 404; full OriginalInput injection.

### What This Leaf Consumes
- none (foundation).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Fresh queue per turn | Modify: internal/agent/loop.go (RunOnce registration region ~3480-3528) | `go test ./internal/agent/ -run TestFreshQueuePerTurn -count=1` | gate T3 RED (closed queue reused) | GREEN | Given cached loop turn 2; When steer; Then open queue accepts; old queue object stays closed (no reopen-in-place) |
| T2 | Clone queue isolation | Modify: internal/agent/loop.go ConfigSnapshot (~10107); internal/agent/manager.go clone path | `go test ./internal/agent/ -run TestCloneQueueIsolation -count=1` | gate T3 RED (shared pointer) | GREEN | Given two session clones of one template; Then distinct queue instances; A's turn-end close does not close B's queue |
| T3 | Thread-remap lookup | Modify: internal/agent/registry.go GetActiveQueue (+ GetActiveQueueLoop twin) | `go test ./internal/agent/ -run TestThreadRemap -count=1` | gate T4 RED (exact-only) | GREEN | Given queue under conv-X-thread-work-ab12; When lookup conv-X; Then found; two live threads -> explicit ambiguous error |
| T4 | Full-text injection + 404 + drain delete | Modify: internal/agent/dispatcher.go:3180/3194 region; internal/comm/http/api_handlers.go handleServiceError; internal/agent/queue.go DrainFollowUp + persister | `go test ./internal/agent/ ./internal/comm/http/ -run 'TestSteerFullText|TestQueueNotFound404|TestDrainDeletes' -count=1` | gate T3 RED (Summary injected); 500 today; rows linger | GREEN | Given 300-char steer; Then loop receives full text; idle steer HTTP 404 with reason; consumed follow-up rows deleted |

## Self-Verification Checklist

- [ ] Gate T3/T4 GREEN; gate diff empty
- [ ] C3 resolution order exact; no cross-session delivery (two-session steer test)
- [ ] Live-gate: rig, 5-step primary, steer at +8s on turn 2 of cached loop -> correction acted on, receipt event present
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] No reopen-in-place race (fresh instance, old stays closed)
- [ ] ConfigSnapshot no longer carries queue pointer; registry-built loops still get queues
- [ ] Direct-mode (no dispatcher) loops also get a queue (B3b gap)
- [ ] Ambiguous multi-thread match errors explicitly (never silent pick)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Boot follow-up RECOVERY (re-enqueue instead of delete) is 02-routing/03
  (W13) - this leaf only deletes on drain; do not touch queue_recovery.go.
- latest-wins steer overwrite (B8): keep semantics but emit a negative event
  when an undrained steer is replaced (agent.queue.steer.replaced).
