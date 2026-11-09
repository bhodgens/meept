# Audit Findings - Implicit State Machine Audit (2026-11-09)

Provenance: committed HEAD 63a7cf90. Tree dirty (~104 files, sibling voice/
employee wave). All CRITICAL/MAJOR-CONFIRMED findings re-verified at HEAD by
parent reads. This file is the traceability source for master.md's
Requirement table. Finding IDs below use the report's naming; branch leaves
cite them.

## Machines and verdicts

| # | Machine | Verdict |
|---|---------|---------|
| 1 | Job queue (internal/queue) | DEFECTIVE + WARRANTS-EXPLICIT |
| 2 | Worker FSM (internal/worker) | APPROPRIATE-IMPLICIT |
| 3 | Effects ledger (internal/effects) | CLEAN |
| 4 | Scheduler delivery receipts | DEFECTIVE |
| 5 | Parked-turn/resume (quota, throttle, budget, gate) | DEFECTIVE + WARRANTS-EXPLICIT |
| 6 | Plan lifecycle (PlanState, 10 states) | DEFECTIVE + WARRANTS-EXPLICIT |
| 7 | Plan phase (PhaseState) | DEFECTIVE (2 unreachable states) |
| 8 | Goal lifecycle (GoalState) | DEFECTIVE |
| 9 | Agent messages | CLEAN |
| 10 | Voice call lifecycle (3 legs) | DEFECTIVE + DUPLICATED |
| 11 | Session idle reaper | CLEAN (parent-verified) |

## CRITICAL

- C1. Restart double-execution via park-kind collision: two parkers persist
  under ParkKindChat (quota_resume.go:310-317, components.go:4298); store
  loads by kind only (park_store_sqlite.go:205-209); throttle payload
  unmarshals as quota payload; same message executes twice post-restart.

## MAJOR (queue)

- M-1. MarkProcessing token-less (store.go:541 UpdateState guards terminal
  only); requeue race strands rows processing/unowned.
- M-2. Determinate failures strand in 'failed' (worker.go:415->432; no
  sweeper).
- M-3. moveToDead failure log-only (store.go:907-911); 'dead' rows strand
  in jobs.
- M-4. Reset-subset drift: Requeue/Retry leave timeout_at/claimed_by_node;
  RecoverFromDeadLetter drops interactive (:1495-1515).

## MAJOR (scheduler)

- M-5. advanceLastWake before Execute (scheduler.go:671-676) + recovery
  reads LastDelivery only (receipts.go:126-132): crash loses occurrences.

## MAJOR (plans/goals)

- M-6. PhaseFailed/PhaseConfirmed zero writers (plan.go:43-44).
- M-7. Only approval CAS; submit/cancel/reject/confirm/complete
  read-check-unconditional-write; full-row UpdatePlan clobbers leases.
- M-8. Reclaim-to-approved undrivable (RecoverStrandedSynthesis
  startup-only daemon.go:1076; nothing re-invokes Synthesize for approved).
- M-9. Mid-run lease expiry never recovers (no periodic reclaimer).
- M-10. Goal blocked->active operator unblock not wired (only goal_update
  tool; UpdateGoal no validation).
- M-11. GoalStore.Retire dead code; tool retire never sets retired_at;
  employee delete hard-DELETEs goals (manager.go:1368).

## MAJOR (parked turns)

- M-12. Gate parks strand on rejection (resume producer plan.approved
  only; ResumeAt=+10y; no release event).
- M-13. Throttle resume discards result (loop_park.go:418-477 logs only;
  payload lacks TurnID).
- F4-park. Quota re-park unbounded (handler.go:2668-2689; no Attempt cap).
- F5-park. Budget parker memory-only, invisible on quota_wait topic.

## MAJOR (voice)

- V1(1). Webhook goroutine blocks on per-employee goal mutex
  (voice_wiring.go:294-299 -> voice_employee.go:388-405).
- V2(2). Dial acquire-failure leaks outbound/done maps (dial.go:326-343,
  no FailOutcome).
- V3(3). Signal leg no ringing timeout (leg.go:375-480).
- V4(4). Media-WS reconnect never re-splices (manager.go:311-321).
- V5(5). Signal leg ignores fresh_session_per_call + day brief
  (leg.go:302-312).
- V6(6). Signal effect key per-attempt random ccid (leg.go:412).

## Minors addressed

F6 BEGIN-IMMEDIATE comment drift; F7 missing jobs.state CHECK; F8 legacy
token-less Complete (decision Q1); F9 operator Retry budget bypass;
F7-plan failed-confirmable-not-revisable; F8-plan cancel leaves phases;
F9-plan phase_completed zero subscribers; F11/F13 mapping restore gap;
F16 paused-goal assessment writes; messages queued->read nit; no CHECK on
plans/plan_phases/agent_messages; voice 7 (calls.db stranded live), 8
(restart-mid-ring residual), 9 (answered race), 10 (no C1 end frame), 11
(redelivery storm doc).

## Verified clean (patterns reused by fixes)

Claim-token CAS on all six job writes; effects ledger exactly-once;
release fan-out exactly-once at 3 voice layers; one-live-call invariant;
messages CAS + exactly-once drain; idle reaper exactly-once; gate parks
cannot time-approve.

## Could-not-verify (assigned)

- scheduler_jobs.go as external Synthesize re-driver -> 03/01 T4
- splice runner non-backpressure error path -> 05/03 T3 test
- ActivityTracker feeder wiring (idle reaper dormancy) -> master
  integration plan check
- signal-cli ENDED delivery guarantees -> out of scope (external system)
