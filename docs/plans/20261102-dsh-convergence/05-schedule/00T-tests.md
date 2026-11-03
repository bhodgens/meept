# 05-schedule Gate Tests (00T) - Test Leaf

> **For the implementing agent:** You are the TEST implementer for this gate leaf.
> Write tests ONLY. Do NOT write or modify any implementation files under
> `internal/scheduler/` (other than the test/target fixtures you author). The
> tests MUST fail RED for missing-implementation reasons (they reference types
> and methods that do not exist yet) - that is the expected end state. Run them
> once to confirm they fail for the RIGHT reason (missing implementation, not a
> syntax error). Do NOT commit. Do NOT git add. Write once and stop - never read
> a file back after writing it.

## Meta

- **Parent:** [`orchestrator.md`](orchestrator.md)
- **Scope:** The complete unit + hermetic e2e suite for F13a (RFC5545 timing
  kinds) and F13b (session delivery-inbox + receipts), per the branch `## Test
  Gate` matrix.
- **Dependencies:** none (gates both impl leaves)
- **Estimated Context:** 35K
- **Concurrency Group:** phase 0 (dispatched before Group A)
- **Audit references:** `/tmp/dsh-audit/skills-schedule-storage.md` PART 2
  FEATURE 2a (timing kinds) + FEATURE 2b (delivery contract)

## Goal

Author the tests that the branch's two implementation leaves must make pass.
You build no implementation: F13a targets the `internal/scheduler/timing.go`
L1 surface; F13b targets `internal/scheduler/receipts.go` + delivery behavior
per L2. When the impl leaves make the suite GREEN, RFC5545 timing kinds and
session-delivery receipts are verified end-to-end.

## Context

- `internal/scheduler/scheduler.go` - the robfig/cron-backed scheduler
  (`cron.New` at :183-194 with `cron.WithLocation` + `WithSeconds`; `executeJob`
  at :612-713 does claim-before-deliver via `s.ClaimTick`; `ScheduleConfig` at
  :357; `loadPersistedJobs` at :750; `parseSchedule` at :830).
- `internal/scheduler/jobs.go` - `JobConfig` (:47-67), `AgentJobConfig`
  (:70-77, no TargetSessionID yet), `AgentJob.Execute` (:212-248,
  fire-and-forget `chat.request` with no session pin / ack).
- `internal/scheduler/claims.go` - the claim store (`newClaimStore` :58,
  `ClaimTick` :96, `migrateClaimStore` :77; table `scheduler_claimed_ticks`).
- `internal/scheduler/persistence.go` - the `Store` (jobs.json: Load/Save/Add/
  Get/Remove/Update/List/UpdateLastRun/SetEnabled, :68-293).
- `internal/scheduler/rpc.go` - `AddJobParams` (:42-54, `Schedule string`),
  `RegisterRPCHandlers` (:27). `internal/tools/builtin/tool_schedule_create.go`
  - the `schedule_create` tool surface (Parameters :46-90).
- `internal/agent/handler.go:804-814` - `chat.request` `TurnID` re-registration
  is idempotent (the re-delivery safety hook for at-least-once).
- e2e manifest: `e2e/manifest.json` already lists `"scheduler"` suite
  (dir `e2e/suites/scheduler`, mapped from `internal/scheduler/`). Extend the
  existing suite + manifest entry; do not add a new package-based suite.
- dsh source-of-truth for the bounded RFC5545 profile: dsh
  `packages/schedule/schedule/src/domain.ts` (:1209-1273 parse/normalize
  RRULE; bounded subset: reject ineffective SetPositions, COUNT<=1000, no
  SECONDLY/single BYSECOND) and `/tmp/dsh-audit/skills-schedule-storage.md`
  FEATURE 2a.

## Verified Anchors

- `internal/scheduler/scheduler.go:183-194` - `cron.WithLocation(loc)`, `WithSeconds`,
  parser `cron.SecondOptional|Minute|...|Descriptor` - the engine + DST seam.
- `internal/scheduler/jobs.go:212-248` - `AgentJob.Execute` publishes
  `chat.request` (fire-and-forget; the F13b gap).
- `internal/scheduler/claims.go:96-120` - `(cs *claimStore) ClaimTick(jobID, tick) (bool, error)`.
- `internal/scheduler/persistence.go:14-68` - `Store` type; persist jobs.json.
- `internal/agent/handler.go:804-814` - idempotent TurnID registration.

## Interface Contracts (From Parent)

### What This Leaf Exposes

The validate-able test suite (names in the branch Test Gate matrix):

```
Unit (internal/scheduler): TestParseTiming_Kinds, TestTiming_CronExpr_PerKind,
  TestTiming_NextOccurrence_DST, TestTiming_Validate_RFC5545Boundary,
  TestAgentJob_DeliversToSession, TestReceipts_BoundedHistory,
  TestRecoverInFlight_Delivery, TestReceiptStatus_Transitions.
E2E (e2e/suites/scheduler, add to manifest): table rows F13a-schedule_timing_kind_fires,
  F13b-schedule_session_delivery_receipt, F13b-schedule_restart_redelivers_inflight.
```

### What This Leaf Consumes

- L1 (owner 01-timing-kinds.md) - `ParseTiming`, `TimingSpec{CronExpr, NextOccurrence, Validate}`,
  `JobConfig.Timing`, `Scheduler.RegisterTiming`, `AddJobParams.Timing`. Test against
  these EXACT signatures only; full definitions live in the branch orchestrator.
- L2 (owner 02-delivery-inbox.md) - `AgentJobConfig.TargetSessionID`,
  `ScheduleDeliveryReceipt`, `DeliveryHistory`, `Scheduler.recordDelivery`,
  `Scheduler.recoverInFlightDeliveries`. Full definitions in the orchestrator.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Author F13a unit tests for the L1 timing surface: parse the five kinds, translate each to a robfig cron expression, DST-safe next occurrence, and the RFC5545 profile boundary | Create: `internal/scheduler/timing_test.go`; Test: `internal/scheduler/` | `go test -p 2 ./internal/scheduler/ -run 'TestParseTiming_Kinds\|TestTiming_'` | FAIL - `TimingSpec`/`ParseTiming` undefined (missing impl) | PASS - all timing unit tests GREEN | Given the L1 signatures; When the tests compile+run against a full timing impl; Then each kind parses/translates/next-occurrence DST-correct and the RFC5545 boundary rejects/rejects-typed the unsupported constructs |
| T2 | Author F13b unit tests: AgentJob delivers a pinned chat.request to TargetSessionID, receipt status transitions (pending->in_flight->delivered), bounded history (prune >200 or >30d), and recoverInFlightDeliveries re-delivers stuck receipts | Create: `internal/scheduler/receipts_test.go`; Test: `internal/scheduler/` | `go test -p 2 ./internal/scheduler/ -run 'TestAgentJob_DeliversToSession\|TestReceipts_BoundedHistory\|TestRecoverInFlight_Delivery\|TestReceiptStatus_Transitions'` | FAIL - `ScheduleDeliveryReceipt`/`recordDelivery`/`recoverInFlightDeliveries` undefined | PASS - all delivery unit tests GREEN | Given a job with `AgentConfig.TargetSessionID` set; When Execute runs and receipt helpers are invoked; Then the payload carries `session_id`, receipts transition to `delivered` after ack, history stays <=200 / prunes >30d, and a stuck `pending` receipt is re-delivered with the same MessageID |
| T3 | Author the hermetic e2e rows for F13a/F13b in the existing scheduler suite: a timing-kind job fires within its window; a target-session job delivers a receipt; a restart re-delivers an in-flight receipt (idempotent) | Modify: `e2e/suites/scheduler/scheduler_test.go`, `e2e/manifest.json` | `make e2e-fast-area AREA=scheduler` | FAIL - impl `timing.go`/`receipts.go` missing or engine still fire-and-forget | PASS - e2e GREEN | Given the live RPC surface (`scheduler.add_job`); When a `timing` job schedules and a `target_session_id` job runs + daemon restarts; Then the timing job fires in its window, a receipt is recorded/delivered, and the in-flight receipt is re-delivered at-least-once |

T1-T2 are unit tests in `internal/scheduler` (extend existing test files or add
NEW `timing_test.go`/`receipts_test.go` - the two new files are the L1/L2 unit
homes). T3 is the hermetic e2e tier per root AGENTS.md e2e policy (extend the
existing suite + manifest path_map list `internal/scheduler/` already present).

Mandatory assertions per kind (encode exactly the L1 profile):

- `ParseTiming("daily 09:00:00 America/Denver")` -> Kind=TimingDaily,
  Time="09:00:00", TimeZone="America/Denver"; `CronExpr()` parses as a robfig
  expression (`parser.Parse` succeeds) and fires at 09:00 wall-clock.
- `ParseTiming("weekly MON,THU 09:00:00 America/Denver")` -> Weekdays
  normalized to ascending-unique ISO [1,4]; `CronExpr()` maps to a DOW set.
- `ParseTiming("at 2026-12-01T09:00:00Z")` -> a one-shot fired once at the
  absolute instant.
- `ParseTiming("every 1h30m")` -> Every=90m; rejects < 60s with
  `frequency_too_high` (MinTimingEverySeconds=60).
- `ParseTiming("rrule:\nDTSTART;TZID=America/Denver:20261201T090000\nRRULE:FREQ=WEEKLY;BYDAY=MO,TH")`
  -> Kind=TimingRRule, RValue normalized; `CronExpr()` produces a weekly DOW expr.
- Boundary (must REJECT with a typed error, never silently pass):
  `FREQ=MONTHLY;BYDAY=-1FR`, `FREQ=YEARLY`, `COUNT=5`, `UNTIL=...`,
  `FREQ=DAILY;INTERVAL=2`, `BYSECOND=0` and any >28 `BYMONTHDAY`. Assert the
  returned error is one of `invalid_rule`/`invalid_time_zone`/`frequency_too_high`.

The F13b unit/e2e tests must assert behavior (payload `session_id` set, receipt
status transitions, bounded length, re-delivery with the same MessageID), not
implementation details.

## Self-Verification Checklist

- [ ] All test tasks written; the suite compiles and runs
- [ ] Tests fail RED for missing-implementation reasons ONLY (no syntax errors;
      unknown-identifier failures are the correct RED shape in Go)
- [ ] F13a and F13b matrix rows each have >=1 unit test AND e2e coverage
- [ ] No implementation written - only test files + the e2e suite + manifest edit
- [ ] Every F13a assertion maps to the L1 RFC5545 profile and every F13b
      assertion to the L2 receipt contract

**DO NOT COMMIT.** The orchestrator handles all git operations after validation.

## Completion Report Format

1. **Files touched:** every created/modified path (test files only)
2. **Verify command + output:** the unit + e2e commands above; last 20 lines of
   output confirming RED for the right reason
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent - the control agent, in-session)

- [ ] Every Test Gate matrix row has a corresponding test (unit + e2e); no row
      left VALIDATE-less
- [ ] Tests fail for missing-implementation reasons, not syntax errors
- [ ] Assertions target behavior/state (payload, status, bounds), not internal
      details; no tautologies
- [ ] No implementation logic smuggled into the test files
- [ ] No line-number corruption; ASCII only

Output: VALIDATED or a list of specific gaps (delta protocol - update the
LESSER of plan/tests to the GREATER; test deletion needs written rationale).

## Notes

- Do not invent signatures: write the tests AGAINST the L1/L2 contracts in the
  orchestrator. If a contract field name is wrong, STOP and report via the
  report rather than silently testing a different name.
- The reserved RFC5545 profile is a defined subset over robfig/cron (see
  orchestrator L1). The RED reason for unsupported constructs is the typed
  error vocabulary, not a silent OK.
- Feasibility: a single e2e test process runs in the hermetic harness with a
  scratch `MEEPT_HOME`; `scheduler.add_job` fires on second granularity.
