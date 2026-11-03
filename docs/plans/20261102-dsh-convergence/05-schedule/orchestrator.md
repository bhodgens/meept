# 05-schedule - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch

- **Parent:** [`../../master.md`](../master.md) (root orchestrator, control)
- **Children:** `00T-tests.md` (test gate), `01-timing-kinds.md` (impl),
  `02-delivery-inbox.md` (impl)
- **Scope:** Add RFC5545 timing kinds (rrule/daily/weekly/at/every) over the
  existing robfig/cron engine and a session-delivery-inbox contract with
  durable receipts + bounded history + in-flight recovery.

## Goal

Meept's scheduler already wraps robfig/cron behind a `JobConfig`/`Store`
surface and ships run-now, pause/resume/delete, restart persistence, and a
crash-safe claim store (this premise was corrected by subagent 4's audit; see
`/tmp/dsh-audit/skills-schedule-storage.md` Part 2). Two dsh-meept gaps remain,
and this branch closes exactly those two and nothing else:

1. **Timing kinds.** dsh models reminders as typed recurrence kinds (`rrule`,
   `daily`, `weekly`, `at`, `every`, `after`, `cron`); meept is bare
   cron-expression-only. "Last Friday of every month at 9am" is impossible in
   one meept cron row, and there is no DST-safe wall-clock modeling. This
   branch adds the five kinds the contract names, normalized to the robfig/cron
   engine meept already uses, with a defined RFC5545 profile and DST-safe
   wall-clock resolution.
2. **Delivery-to-session inbox.** dsh pins a scheduled delivery to a target
   session's inbox, persists a `ScheduleDeliveryReceipt` + bounded history, and
   recovers an in-flight delivery across restart (at-least-once). meept's
   `AgentJob.Execute` publishes a fire-and-forget `chat.request` bus message
   with no target session, no receipt, no bounded history, and reclaims nothing
   after the (pre-existing) tick claim. This branch pins delivery to a target
   session inbox and closes the acknowledgment/recovery gap by reusing the
   existing claim store.

Non-goals (already present in meept): run-now, pause/resume/delete, restart
survival of jobs, DST handling via `cron.WithLocation`, and the delivery-*tick*
claim (which already exists). Do NOT re-add or duplicate them; the branch
extends only the two deltas above.

## Architecture

Both deltas layer onto the existing scheduler with no engine swap:

```
model schedule spec (rrule|daily|weekly|at|every)
   -> 01-timing-kinds: TimingSpec normalize+validate -> robfig cron expression
                           (DST-safe wall-clock via TimeZone + NextOccurrence)
   -> scheduler.cron (unchanged engine, cron.WithLocation already DST-aware)
        -> executeJob (existing claim-before-deliver tick dedup)
             -> 02-delivery-inbox: AgentJob pins chat.request to TargetSessionID
                     as an inbox follow-up; record ScheduleDeliveryReceipt
                     (pending -> in_flight -> delivered); bounded history;
                     recoverInFlightDeliveries() on Start = at-least-once.
```

Leaves 01 and 02 are independent: 01 changes how a *schedule* is expressed and
registered; 02 changes what a tick *delivers* and how it is acknowledged. Both
touch `internal/scheduler` but disjoint files (01: `timing.go` + `rpc.go` +
`tool_schedule_create.go`; 02: `receipts.go` + `jobs.go` + `scheduler.go`), so
they dispatch in parallel (Group A) after the test gate validates.

## Interface Contracts

Branch-owned contracts live here once. Leaves consume exact signatures only and
never restate full definitions.

### L1: Timing kinds (Owner: 01-timing-kinds.md)

```go
// internal/scheduler/timing.go (NEW)
type TimingKind string
const (
	TimingRRule  TimingKind = "rrule"
	TimingDaily  TimingKind = "daily"
	TimingWeekly TimingKind = "weekly"
	TimingAt     TimingKind = "at"
	TimingEvery  TimingKind = "every"
)
type TimingSpec struct {
	Kind     TimingKind    `json:"kind"`
	Time     string        `json:"time,omitempty"`      // HH:mm:ss (daily/weekly)
	TimeZone string        `json:"time_zone,omitempty"` // IANA, e.g. "America/Denver" (daily/weekly/rrule DTSTART TZID)
	Weekdays []int         `json:"weekdays,omitempty"`  // ISO 1=Mon..7=Sun, ascending unique (weekly)
	At       *time.Time    `json:"at,omitempty"`        // absolute instant (at)
	Every    time.Duration `json:"every,omitempty"`     // >= 60s (every)
	RRule    string        `json:"rrule,omitempty"`     // frozen two-line "DTSTART:...\nRRULE:..." (rrule)
}
func ParseTiming(spec string) (*TimingSpec, error)          // model-facing string -> spec
func (t *TimingSpec) Validate() error                        // kind-specific input rules + RFC5545 profile gate
func (t *TimingSpec) CronExpr() (string, error)              // robfig expression for s.cron (scheduler location)
func (t *TimingSpec) NextOccurrence(from time.Time) (time.Time, bool) // DST-safe via time.LoadLocation(t.TimeZone)
const MinTimingEverySeconds = 60   // mirrors dsh MIN_EVERY_INTERVAL_SECONDS floor
// JobConfig gains:      Timing *TimingSpec `json:"timing,omitempty"`
// scheduler.go gains:   func (s *Scheduler) RegisterTiming(cfg JobConfig, job Job) (cron.EntryID, error)
// rpc.go gains:         AddJobParams.Timing string `json:"timing,omitempty"` (model-authored spec string)
// tool_schedule_create.go: schedule_create tool gains optional "timing" string param (exclusive with "schedule")
// Owners: 01-timing-kinds. Consumers: 02 uses none of L1's types (delivery is kind-agnostic).
```

Supported RFC5545 profile (the "RFC5545 satisfied in meept" bound, enumerated
in 01-timing-kinds.md): `FREQ=DAILY|WEEKLY|MONTHLY` with only the constructs a
single robfig/cron expression can express (DAILY with at most `BYHOUR/BYMINUTE`;
WEEKLY with `BYDAY`; MONTHLY with a single `BYMONTHDAY` 1-28). Everything else
(`BYDAY`-relative last/nth weekday, `FREQ=YEARLY`, `COUNT`, `UNTIL`,
`BYSECOND`/high-density, `INTERVAL`) is REJECTED with a typed error
(`invalid_rule`/`frequency_too_high`/`invalid_time_zone`) so an unsupported
rule fails loudly rather than silently expanding to a wrong schedule. This
mirrors dsh's bounded-subset RRULE parser (rejectIneffectiveSetPositions,
COUNT<=1000 ceiling, no SECONDLY/single BYSECOND).

### L2: Session delivery inbox + receipts (Owner: 02-delivery-inbox.md)

```go
// internal/scheduler/receipts.go (NEW)
type DeliveryStatus string
const (
	DeliveryPending   DeliveryStatus = "pending"   // claim taken, enqueue started, session ack not yet confirmed
	DeliveryInFlight  DeliveryStatus = "in_flight" // chat.request published, awaiting session-store ack
	DeliveryDelivered DeliveryStatus = "delivered" // target session inbox acknowledged the message
)
type ScheduleDeliveryReceipt struct {
	ScheduleID      string        `json:"schedule_id"`
	ScheduledAt     time.Time     `json:"scheduled_at"`
	OccurrenceAt    time.Time     `json:"occurrence_at,omitempty"`
	DeliveredAt     time.Time     `json:"delivered_at,omitempty"`
	MessageID       string        `json:"message_id"`        // stable turn_id; idempotent across re-delivery
	TargetSessionID string        `json:"target_session_id"`
	Prompt          string        `json:"prompt,omitempty"`
	Status          DeliveryStatus `json:"status"`
}
type DeliveryHistory struct {
	Records              []ScheduleDeliveryReceipt `json:"records"`
	EarlierRecordsPruned bool                     `json:"earlier_records_pruned"`
}
const MaxDeliveryHistoryRecords = 200
const DeliveryHistoryRetentionDays = 30
// AgentJobConfig gains: TargetSessionID string `json:"target_session_id,omitempty"`
// JobConfig gains:       LastDelivery *ScheduleDeliveryReceipt `json:"last_delivery,omitempty"`
//                        DeliveryHistory *DeliveryHistory       `json:"delivery_history,omitempty"`
// jobs.go: AgentJob.Execute pins payload["session_id"]=TargetSessionID when set
// scheduler.go gains: func (s *Scheduler) recordDelivery(jobID string, r ScheduleDeliveryReceipt) error
//                     func (s *Scheduler) recoverInFlightDeliveries() error  // called in Start, after loadPersistedJobs
// Owners: 02-delivery-inbox. Consumers: none downstream (GUI/task-manager parity deferred to branch 04/Open Questions).
```

At-least-once across restart: the existing claim store already dedupes *ticks*
(`executeJob` calls `s.ClaimTick` before dispatch). That claim is the reason a
crash after claim+deliver but before the session ack loses the reminder
(fire-and-forget) - the tick will not re-fire. Recovery therefore scans *receipt
rows*, not ticks: any receipt stuck in `pending`/`in_flight` is re-delivered
with the same `MessageID`/`target_session_id` after Start; the `chat.request`
`turn_id` dedupe (internal/agent/handler.go:804-814, idempotent re-registration)
makes that re-delivery safe. Contract consumers read `LastDelivery` +
`DeliveryHistory` for the bounded history (retention 30 days / <=200 records).

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | `00T-tests.md` | test leaf | none | 35K | phase 0 |
| 01 | `01-timing-kinds.md` | leaf | 00T | 85K | A |
| 02 | `02-delivery-inbox.md` | leaf | 00T | 90K | A |

01 and 02 have no inter-dependency and dispatch together (Group A), only after
00T reaches TESTS_VALIDATED.

## Dispatch Protocol

Full recipe: skill `hierarchical-plan-execution`. This branch runs in the
20261102-dsh-convergence isolation worktree.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch `00T-tests.md` first (subagent, tests-only): full unit + e2e suite for
F13a and F13b. Validate IN-SESSION (control agent, never delegated) against the
`## Test Gate` matrix; the tests must compile and fail RED for
missing-implementation reasons. Apply the delta protocol (update the LESSER
artifact), commit gate tests, set the branch TESTS_VALIDATED. Then 01 and 02 may
dispatch.

### Phase 1: Dispatch Concurrency Group A

Dispatch `01-timing-kinds.md` and `02-delivery-inbox.md` simultaneously via
`delegate_task`. Context per leaf: the full leaf text + the L1/L2 contracts
INLINED + the dsh + meept source anchors verbatim + the do-not-commit,
no-git-add, no-read_file-on-source, write-once-and-stop rules. Both leaves
depend only on 00T (TESTS_VALIDATED), never on each other.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check each
leaf spec, the L1/L2 contracts, the Review Checklist; run the verify commands
(`go test -p 2 ./internal/scheduler/...`, affected e2e suite); the gate-test
diff must be empty (any gate-test edit = automatic NEEDS_FIXES). Pass: commit
(explicit pathspecs, single-line message) + status REVIEWED. Gaps: re-dispatch
with findings, max 3 cycles, then escalate to parent (master).

### Phase 3: Integration Review

After both leaves REVIEWED: verify L1/L2 hold together (a `timing`-based job
whose `AgentJobConfig.TargetSessionID` is set delivers to the session inbox and
records a bounded receipt history); run the full branch gate suite GREEN;
`go build ./...`; confirm no import cycles and no gate-test diff. Pass: run
gofmt, apply the ASCII + line-number corruption checks, set both leaves
COMPLETE, report COMPLETE to parent.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] L1/L2 interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (no ignored errors, no mutex across I/O,
      typed-nil guards on wiring seams, predictable-ID prevention via
      `pkg/id.Generate` - never UnixNano for MessageID)
- [ ] No scope creep - only RFC5545 kinds + delivery-inbox + receipts, nothing
      re-added from the already-present run-now/pause/resume/delete persistence
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing surface ships RPC + tool wiring or a documented
      deviation (root AGENTS.md wiring requirement)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN); robfig/cron v3 already imported
- **Naming:** exported/unexported per repo; timing kinds in snake_case JSON
  (`"rrule"`, `"daily"`, `"weekly"`, `"at"`, `"every"`)
- **Imports:** stdlib first, then third-party; no unused (U1000-strict)
- **Error handling:** wrap with context; no ignored errors; typed sentinel errors
  for validation (`invalid_rule`, `invalid_time_zone`, `frequency_too_high`)
- **Security:** predictable-ID prevention (`pkg/id.Generate`); never UnixNano;
      typed-nil guards on wiring seams
- **Testing:** NEW feature tests go in the hermetic e2e tier (root AGENTS.md
      e2e policy) - extend the existing `e2e/suites/scheduler` suite + its
      `e2e/manifest.json` entry in the SAME commit; do NOT add new unit-test
      files for green-field features (gate/update existing unit tests only)
- **Formatting:** gofmt before reporting; ASCII only in new .md

## Requirement Traceability

| Req | Source (user words, verbatim) | Plan element (leaf/task/contract) | Verified by |
|-----|-------------------------------|----------------------------------|-------------|
| R3 | "Make sure RFC5545 is satisfied in meept" | 05-schedule: L1 (01-timing-kinds.md, the supported RFC5545 profile) + L2 (02-delivery-inbox.md) | 05 Test Gate matrix F13a/F13b + e2e/suites/scheduler |
| R4 | "END TO END and unit tests for all features as a prerequisite" | 05 Phase 0 gate (00T-tests.md) | 05 Test Gate matrix VALIDATED |
| D5 | user: RFC5545 satisfied in meept (frozen 2026-11-02) | L1 01-timing-kinds.md | 05 Test Gate |

Every user requirement maps to a plan element. DEFERRED: a GUI task-manager
recurrence-editor + delivery-history page (item 2e of the audit) is deferred to
branch 04 / master Open Questions - the TUI + RPC surfaces already exist and
this branch ships the model-facing seams.

## Completion Tracking Table

Single source of truth for subtree status; parent reads this.

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests.md | PENDING | 0 | gate |
| 01-timing-kinds.md | PENDING | 0 | gated on 00T |
| 02-delivery-inbox.md | PENDING | 0 | gated on 00T |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches
TESTS_VALIDATED.

## Integration Test Plan

- `go test -p 2 ./internal/scheduler/...` - unit gate suite GREEN (additive).
- `make e2e-fast` (or `make e2e-fast-area AREA=scheduler`) - the extended
  `e2e/suites/scheduler` suite: a timing-kind job registered through
  `scheduler.add_job` with `timing=...` fires within its window; an agent job
  with `target_session_id` delivers into that session's inbox and records a
  `ScheduleDeliveryReceipt`; a restarted daemon re-delivers a `pending`/`in_flight`
  receipt at-least-once (idempotent `turn_id`), and history is bounded.
- Cross-boundary from master: branch 02-goals round driver may reuse the
  scheduler's deferred-turn; assert the round driver's queued goal turn still
  lands when the scheduler delivers to a target session.
- `go build ./...` GREEN; no import cycles.

## Test Gate

Test leaves: `00T-tests.md` (single test leaf covering both impl leaves) -
dispatched in Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F13a | RFC5545 timing kinds (rrule/daily/weekly/at/every) over robfig/cron, DST-safe wall-clock, defined RFC5545 profile + typed rejection | 01-timing-kinds.md Tasks 1-4 / L1 | `TestParseTiming_Kinds`, `TestTiming_CronExpr_PerKind`, `TestTiming_NextOccurrence_DST`, `TestTiming_Validate_RFC5545Boundary` (unit names authored in 00T) | e2e suite table, F13a: `schedule_timing_kind_fires` | PENDING |
| F13b | Session delivery-inbox (TargetSessionID), durable receipt + bounded history, at-least-once + in-flight recovery via claim store | 02-delivery-inbox.md Tasks 1-4 / L2 | `TestAgentJob_DeliversToSession`, `TestReceipts_BoundedHistory`, `TestRecoverInFlight_Delivery`, `TestReceiptStatus_Transitions` | e2e suite table, F13b: `schedule_session_delivery_receipt` + `schedule_restart_redelivers_inflight` | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log (control agent fills): [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents (`00T-tests.md`, `01-timing-kinds.md`, `02-delivery-inbox.md`)
freeze at dispatch. Corrections ride dispatch context as dated addenda (dated,
appended to the dispatch brief). Never edit a dispatched leaf document
mid-execution.

## Open Questions

1. GUI task-manager page with a recurrence editor + delivery-history view (audit
   FEATURE 2e) is NOT a leaf in this branch - the TUI + RPC surfaces already
   exist and this branch ships the model-facing seams. The GUI page is deferred
   to branch 04-tool-surface / master Open Questions as a parity note (C10
   consumers). Owner: orchestrator at master Phase 2.
2. dsh also has `after` and `cron` timing kinds; meept's bare `cron` expression
   (via the existing `Schedule` field + `ValidateSchedule`) already covers
   `cron`; `after` (a delay-relative one-shot) is out of this branch's stated
   scope (master C10 names rrule/daily/weekly/at/every). If `after` is later
   wanted, it maps to an absolute `at` at creation time.
3. Receipt-row schema names (`ScheduleDeliveryReceipt`, JSON tags) are pinned in
   L2. If a future storage-record-change event stream (branch 03 C6) wants to
   source delivery-history, keep these JSON tags stable so the event wiring can
   key off them.

## Notes

- The premise that meept's scheduler lacked run-now/pause/resume/delete/restart
  persistence was already corrected by subagent 4. This branch only closes the
  timing-kind + delivery-inbox gaps per master C10. Do NOT re-plan already-shipped
  primitives.
- RFC5545 is satisfied as a *defined supported profile* over the robfig/cron
  engine, not a full RFC5545 engine. Unsupported constructs fail loudly with the
  typed error vocabulary inherited from dsh (`invalid_rule`/`invalid_time_zone`/
  `frequency_too_high`). The gate tests encode exactly which constructs are in and
  out of the profile.
- The delivery *tick* claim (claim-before-deliver in `executeJob`) already exists
  and is NOT re-added. It is the reason receivers scan receipts (not ticks) for
  in-flight recovery: a claimed tick that crashed before session-ack will not
  re-fire, so the receipt row is the at-least-once hook.
- DST: `cron.WithLocation(s.location)` (scheduler.go:183-194) already makes the
  engine DST-aware; timing specs resolve wall-clock through
  `time.LoadLocation(TimeZone)` for `NextOccurrence` and register the cron
  expression in the scheduler location.
- Wiring: both leaves land RPC + tool surfaces (scheduler.add_job + schedule_create)
  on the model-facing path per root AGENTS.md; no new package is introduced, so the
  existing `e2e/suites/scheduler` + `e2e/manifest.json` entry cover e2e.
