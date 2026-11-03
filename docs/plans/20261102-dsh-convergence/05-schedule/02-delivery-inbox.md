# Session Delivery Inbox + Receipts (02) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests (from `00T-tests.md`)
> ALREADY EXIST and are committed - your job is to make them pass. Do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong gate
> tests to the orchestrator instead). Do NOT commit - the orchestrator handles
> all git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file, do
> NOT read it back - write once and stop. Report what you built, files touched,
> and any deviations.

## Meta

- **Parent:** [`orchestrator.md`](orchestrator.md)
- **Scope:** Deliver a scheduled agent job to a target session's inbox (not
  fire-and-forget), record a durable `ScheduleDeliveryReceipt` + bounded
  history, and recover in-flight deliveries across restart via the existing
  claim store (at-least-once).
- **Dependencies:** `00T-tests.md` (TESTS_VALIDATED); none on 01.
- **Estimated Context:** 90K
- **Concurrency Group:** A (parallel with 01-timing-kinds.md)
- **Audit references:** `/tmp/dsh-audit/skills-schedule-storage.md` FEATURE 2b;
  dsh `runtime.ts:309-367` (send + recoverOutstandingExecution),
  `storage.ts:31-128` (receipt/history schema + retention).

## Goal

Today `AgentJob.Execute` (jobs.go:212-248) publishes a fire-and-forget
`chat.request` bus message and returns - no target session, no ack, no record.
This leaf pins a scheduled delivery to a session: when `AgentConfig.TargetSessionID`
is set, Execute delivers a message pinned to that session's inbox (the `chat.request`
payload carries `session_id`, which the ChatRequest struct already supports), and
the scheduler records a durable receipt whose status transitions
`pending -> in_flight -> delivered` only after the target session acknowledges.
A crash in the middle leaves a `pending`/`in_flight` receipt that
`recoverInFlightDeliveries()` re-delivers after Start with the same `MessageID`
(idempotent via the `chat.request` `turn_id` dedupe), giving at-least-once
without duplicating the pre-existing tick claim.

## Context

- `internal/scheduler/jobs.go` - `AgentJobConfig` (:70-77, add `TargetSessionID`);
  `AgentJob.Execute` (:212-248) - the fire-and-forget gap to close.
- `internal/scheduler/scheduler.go` - `executeJob` (:612-713) already does
  claim-before-deliver via `s.ClaimTick` (:627) then calls `job.Execute(ctx)`
  (:683) and `s.store.UpdateLastRun` (:687). This is where to hook receipt
  recording around Execute and where `recordDelivery` / `recoverInFlightDeliveries`
  live. `Start` (:209-241) runs `loadPersistedJobs` then `pruneExpiredClaims` +
  `ProcessMissedTicks` - add `recoverInFlightDeliveries()` there.
- `internal/scheduler/persistence.go` - the `Store` (jobs.json). `JobConfig`
  gains `LastDelivery` + `DeliveryHistory` fields persisted here.
- `internal/scheduler/claims.go` - the claim store (`ClaimTick` :96). It is the
  at-least-once backstop: a claimed tick that crashed before session-ack will
  NOT re-fire, so recovery scans receipts, not ticks.
- `internal/agent/handler.go:227` - `ChatRequest.SessionID` (the inbox-pin key);
  `handler.go:804-814` - TurnID re-registration is idempotent (re-delivery safe).
- dsh source-of-truth to mirror: `runtime.ts` `send()` (:352-367) enqueues a
  user message then `ctx.sessions.flush()` waits for persistence before
  concluding the delivery; `recoverOutstandingExecution` (:309-346) reconstructs
  an in-flight delivery by re-reading the target inbox and requeueing the same
  message id. `storage.ts` `deliveryReceiptSchema` = {scheduledAt, deliveredAt,
  messageId, targetSessionId} (:31-36), `deliveryHistorySchema` with
  `earlierRecordsUnavailable`/`earlierRecordsPruned` (:38-44), retention
  30 days / 200 records (audit FEATURE 2b).

## Verified Anchors

- `internal/scheduler/jobs.go:70-77` - `AgentJobConfig` (add `TargetSessionID string`).
- `internal/scheduler/jobs.go:212-248` - `AgentJob.Execute` (publish with
  `session_id` when TargetSessionID set).
- `internal/scheduler/scheduler.go:612-713` - `executeJob` (hook receipt around
  `job.Execute`).
- `internal/scheduler/scheduler.go:209-241` - `Start` (add `recoverInFlightDeliveries()`).
- `internal/scheduler/claims.go:96-120` - `ClaimTick` (claim store; at-least-once backstop).
- `internal/agent/handler.go:227, 804-814` - `ChatRequest.SessionID` + idempotent TurnID.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/scheduler/receipts.go (NEW)
type DeliveryStatus string
const (
    DeliveryPending   DeliveryStatus = "pending"
    DeliveryInFlight  DeliveryStatus = "in_flight"
    DeliveryDelivered DeliveryStatus = "delivered"
)
type ScheduleDeliveryReceipt struct {
    ScheduleID      string         `json:"schedule_id"`
    ScheduledAt     time.Time      `json:"scheduled_at"`
    OccurrenceAt    time.Time      `json:"occurrence_at,omitempty"`
    DeliveredAt     time.Time      `json:"delivered_at,omitempty"`
    MessageID       string         `json:"message_id"`
    TargetSessionID string         `json:"target_session_id"`
    Prompt          string         `json:"prompt,omitempty"`
    Status          DeliveryStatus `json:"status"`
}
type DeliveryHistory struct {
    Records              []ScheduleDeliveryReceipt `json:"records"`
    EarlierRecordsPruned bool                     `json:"earlier_records_pruned"`
}
const MaxDeliveryHistoryRecords = 200
const DeliveryHistoryRetentionDays = 30
func NewDeliveryHistory() *DeliveryHistory
func (h *DeliveryHistory) Append(r ScheduleDeliveryReceipt)   // prune >200 records / >30 days, set EarlierRecordsPruned
// Existing types extended:
//   AgentJobConfig.TargetSessionID string `json:"target_session_id,omitempty"`
//   JobConfig.LastDelivery *ScheduleDeliveryReceipt `json:"last_delivery,omitempty"`
//   JobConfig.DeliveryHistory *DeliveryHistory       `json:"delivery_history,omitempty"`
//   Scheduler.recordDelivery(jobID string, r ScheduleDeliveryReceipt) error
//   Scheduler.recoverInFlightDeliveries() error
```

### What This Leaf Consumes

- L2 (self-owned, from `orchestrator.md`) - the full receipt contract above.
- 01-timing-kinds.md exposes none of L1's types to this leaf; delivery is
  kind-agnostic and both dispatch in parallel (Group A).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement the receipt types + bounded history in a new `receipts.go`: `DeliveryStatus`, `ScheduleDeliveryReceipt`, `DeliveryHistory.Append` (prune >200 records or >30 days RFC3339 age; set `EarlierRecordsPruned`) | Create: `internal/scheduler/receipts.go`; Test: `internal/scheduler/receipts_test.go` (gate) | `go test -p 2 ./internal/scheduler/ -run 'TestReceipts_BoundedHistory'` | RED - `receipts.go` absent | GREEN - Append stays <=200 and prunes >30-day rows, flagging `EarlierRecordsPruned` | Given many receipts for one job; When `Append` is called past the bounds; Then `Records` length <=200, old rows dropped, `EarlierRecordsPruned=true` |
| T2 | Pin delivery to a session inbox: `AgentJobConfig` gains `TargetSessionID`; `AgentJob.Execute` sets `payload["session_id"]=TargetSessionID` (plus a stable `MessageID` as `turn_id`) when set, retaining prompt/source/job_id/model | Modify: `internal/scheduler/jobs.go:70-77, 212-248`; Test: gate | `go test -p 2 ./internal/scheduler/ -run 'TestAgentJob_DeliversToSession'` | RED - no session pin | GREEN - Execute publishes `chat.request` carrying `session_id == TargetSessionID` and a stable `turn_id` == MessageID | Given an AgentJob with `TargetSessionID` set; When `Execute` runs; Then the bus payload includes `session_id` and a stable `turn_id` (idempotent redelivery key) |
| T3 | Record + acknowledge: `executeJob` writes an `in_flight` receipt (claimed tick + OccurrenceAt) BEFORE `job.Execute`, then flips to `delivered` (DeliveredAt=now) AFTER Execute returns success; `recordDelivery` persists `LastDelivery` + `DeliveryHistory` on the JobConfig via the store | Modify: `internal/scheduler/scheduler.go:612-713, 659-713`; Test: gate | `go test -p 2 ./internal/scheduler/ -run 'TestReceiptStatus_Transitions'` | RED - no receipt written/transitioned | GREEN - a delivered agent job leaves a `delivered` receipt; an Execute error leaves `in_flight` (for recovery) | Given an agent job with TargetSessionID that succeeds; When it fires through `executeJob`; Then a receipt transitions `in_flight`->`delivered` and is durable on the JobConfig; on error it stays re-dispatchable |
| T4 | In-flight recovery = at-least-once: `recoverInFlightDeliveries()` scans JobConfigs for receipts in `pending`/`in_flight` and re-delivers the same `MessageID` to the same `TargetSessionID` (idempotent via the `chat.request` `turn_id`); called in `Start` after `loadPersistedJobs` | Modify: `internal/scheduler/scheduler.go:209-241` + new method; Test: gate | `go test -p 2 ./internal/scheduler/ -run 'TestRecoverInFlight_Delivery'` | RED - no recovery | GREEN - a receipt left `in_flight` by a simulated crash is re-delivered with the identical MessageID after Start | Given a persisted job whose last receipt is `in_flight`; When the daemon restarts; Then `recoverInFlightDeliveries` re-publishes the same MessageID to that session (at-least-once, no duplicate logical delivery) |

T1 is the substantive new file; T2-T4 are thin wiring into the existing
`executeJob`/`Start` flow. Implement TDD: step 1 confirm each gate test fails
RED, step 3 write the minimal implementation, step 4 confirm GREEN. Where a gate
test lacks a needed wire assertion (e.g. `recordDelivery` durability), ADD it in
the same modify - additions extend the gate upward only, never weaken it.

**At-least-once contract (do not re-add the tick claim):**

- The tick claim in `executeJob` (:627, pre-existing) is NOT reimplemented. It
  is the reason a crash between claim and session-ack loses the delivery (the
  tick will not re-fire) - so recovery is driven by RECEIPT rows, never ticks.
- `recoverInFlightDeliveries()` iterates the persisted JobConfigs (via
  `s.store.List()`), matches receipts with `Status == pending || Status ==
  in_flight`, and re-delivers the same `MessageID`/`TargetSessionID`. Re-delivery
  is safe because the handler treats a duplicate `chat.request` for the same
  `turn_id` idempotently (internal/agent/handler.go:804-814).
- Only transition `in_flight` -> `delivered` after the publish succeeds
  (`delivered := bus.Publish("chat.request", msg)`; `delivered == 0` is an
  error, jobs.go:243-245). A failed publish leaves the receipt `in_flight` so a
  restart re-delivers it.

**Bounded history:** `Append` enforces `MaxDeliveryHistoryRecords == 200` and
`DeliveryHistoryRetentionDays == 30` on `DeliveredAt`/`ScheduledAt` age; when
either prunes, set `EarlierRecordsPruned=true`. `LastDelivery` mirrors the latest
record. No new DB file - this Leaf persists on the existing `JobConfig` via the
existing `Store` (jobs.json), so no schema migration is needed.

## Self-Verification Checklist

- [ ] All tasks implemented; gate suite GREEN
- [ ] L2 interface contracts satisfied exactly (types, method names, JSON tags)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only delivery-inbox + receipts; nothing re-adding the
      tick claim, run-now, pause/resume/delete, or timing kinds

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path (expect `receipts.go` NEW,
   `jobs.go`, `scheduler.go`)
2. **Verify command + output:** the unit + e2e commands above; last 20 lines
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent - the orchestrator, in-session)

- [ ] Every task row implemented; tests passing; gate tests untouched
- [ ] L2 contracts match exactly (signatures, types, JSON paths)
- [ ] At-least-once is correct: recovery scans receipts (not ticks); re-delivery
      is idempotent by `MessageID`/`turn_id`; the pre-existing tick claim is not
      duplicated
- [ ] History is bounded (<=200 / <=30d) with `EarlierRecordsPruned` honored
- [ ] Conventions followed; no bugs, no security issues; predictable-ID rule (use
      `pkg/id.Generate` for MessageID - never `time.Now().UnixNano()`)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- No new package is introduced, so AGENTS.md package table is unchanged; the
  existing `e2e/suites/scheduler` + manifest `internal/scheduler/` path_map cover
  the feature e2e (gate T3 of 00T extends that suite).
- Delivery is a bus publish pinned by `session_id`; the target session's inbox
  persistence ack is the `delivered` transition point. Do not change the
  ChatHandler loop semantics - only the scheduler's delivery + ack bookkeeping.
  (A GUI task-manager delivery-history page is out of scope - master Open
  Questions / branch 04.)
- ASCII only in new .md edits; gofmt before reporting.
