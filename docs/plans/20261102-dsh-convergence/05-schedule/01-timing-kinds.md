# Timing Kinds over robfig/cron (01) - Implementation Leaf

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
- **Scope:** Add the L1 semantic timing kinds (rrule/daily/weekly/at/every)
  normalized to the existing robfig/cron engine, with a defined RFC5545 profile
  and DST-safe wall-clock resolution.
- **Dependencies:** `00T-tests.md` (TESTS_VALIDATED)
- **Estimated Context:** 85K
- **Concurrency Group:** A (parallel with 02-delivery-inbox.md)
- **Audit references:** `/tmp/dsh-audit/skills-schedule-storage.md` FEATURE 2a;
  dsh `domain.ts:839-1306` (the bounded RRULE subset).

## Goal

Give the model-facing scheduler real recurrence semantics. Today `JobConfig.Schedule`
is a bare cron expression (or `@every 1h` descriptor). This leaf adds a
`TimingSpec` layer that parses the five semantic kinds, normalizes them to a
robfig/cron expression registered on the existing engine, resolves DST-safe
wall-clock occurrences, and wires the new spec through the RPC + `schedule_create`
tool surfaces - while keeping persisted jobs fully restart-compatible (the cron
`Schedule` string stays for the engine; the semantic `Timing` spec is persisted
for re-registration).

## Context

- `internal/scheduler/scheduler.go` - the engine + registration path. `cron.New`
  with `cron.WithLocation(s.location)` (:183-194) is already DST-aware; the
  parser accepts 6-field cron, names, and descriptors. `JobConfig` registers via
  cron.AddFunc in `loadPersistedJobs` (:798) and `ScheduleConfig` (:357).
- `internal/scheduler/jobs.go` - `JobConfig` (:47-67) with `Schedule string`
  (:51); `ValidateJobConfig` (:714-767) REQUIRES a non-empty `Schedule` (:721-723)
  - that must become "Schedule OR Timing".
- `internal/scheduler/rpc.go` - `AddJobParams` (:42-54, `Schedule string` :46);
  `AddJob` (:63-109) maps params -> JobConfig -> `ScheduleConfig`. The model
  surface (`scheduler.add_job`) therefore needs a `timing` accept path.
- `internal/tools/builtin/tool_schedule_create.go` - the `schedule_create` model
  tool: `Parameters()` (:46-90) currently exposes only a cron `"schedule"`
  string (Required [:88]). Add an optional `"timing"` param, exclusive with
  `schedule`.
- dsh source-of-truth (bounded RFC5545 subset to mirror): dsh
  `packages/schedule/schedule/src/domain.ts` `createRRuleScheduleRecord`
  (:1222-1236) + `normalizeRRule` (:1209-1211); rejection vocabulary
  `invalid_rule`/`invalid_time_zone`/`frequency_too_high` (:1116-1120);
  `MIN_EVERY_INTERVAL_SECONDS` floor (createEveryScheduleRecord :951-956);
  `normalizeWeekdays` ISO 1=Mon..7=Sun (:490-509); DST gap-skip/repeat-earlier
  local-instant semantics (:689,:709-713,:752).

## Verified Anchors

- `internal/scheduler/scheduler.go:183-194` - engine options (WithLocation,
  WithSeconds, parser, chain Recuperar+SkipIfStillRunning).
- `internal/scheduler/jobs.go:47-67` - `JobConfig` (add `Timing` here).
- `internal/scheduler/jobs.go:714-767` - `ValidateJobConfig` (:721-723 forces
  Schedule); relax to Schedule OR Timing.
- `internal/scheduler/rpc.go:42-109` - `AddJobParams` + `AddJob`.
- `internal/tools/builtin/tool_schedule_create.go:46-90` - `Parameters()` + the
  Required list.
- dsh `domain.ts:1209-1236, 951-956, 490-509` - RRULE parse/normalize, every
  floor, ISO weekdays.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/scheduler/timing.go (NEW) - all L1 types + helpers
type TimingKind string
const (TimingRRule, TimingDaily, TimingWeekly, TimingAt, TimingEvery TimingKind = "rrule","daily","weekly","at","every")
type TimingSpec struct {
    Kind     TimingKind    `json:"kind"`
    Time     string        `json:"time,omitempty"`      // HH:mm:ss (daily/weekly)
    TimeZone string        `json:"time_zone,omitempty"` // IANA (daily/weekly/rrule)
    Weekdays []int         `json:"weekdays,omitempty"`   // ISO 1=Mon..7=Sun (weekly)
    At       *time.Time    `json:"at,omitempty"`          // (at)
    Every    time.Duration `json:"every,omitempty"`       // (every)
    RRule    string        `json:"rrule,omitempty"`       // two-line DTSTART\nRRULE (rrule)
}
func ParseTiming(spec string) (*TimingSpec, error)
func (t *TimingSpec) Validate() error
func (t *TimingSpec) CronExpr() (string, error)
func (t *TimingSpec) NextOccurrence(from time.Time) (time.Time, bool)
func RegisterTiming(c *cron.Cron, cfg JobConfig, job Job) (cron.EntryID, error)
const MinTimingEverySeconds = 60
// Existing types extended:
//   JobConfig.Timing *TimingSpec `json:"timing,omitempty"`
//   Scheduler: method RegisterTimingVia(cfg JobConfig, job Job) (cron.EntryID, error)
//               wrapper used from ScheduleConfig + loadPersistedJobs
//   RPC AddJobParams.Timing string `json:"timing,omitempty"`  (model-facing spec string)
//   tool schedule_create optional "timing" string param
```

### What This Leaf Consumes

- L1 (self-owned, from `orchestrator.md`) - the full TimingSpec contract above.
- 02-delivery-inbox.md consumes none of L1's types (delivery is kind-agnostic);
  no cross-leaf coupling.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement the L1 `TimingSpec` core in a new `timing.go`: `ParseTiming`, `Validate`, `CronExpr`, `NextOccurrence`, `RegisterTiming`, `MinTimingEverySeconds`, and the bounded RFC5545 profile | Create: `internal/scheduler/timing.go`; Test: `internal/scheduler/timing_test.go` (gate) | `go test -p 2 ./internal/scheduler/ -run 'TestParseTiming_Kinds\|TestTiming_'` | RED - `timing.go` absent | GREEN - five kinds parse + translate; DST next-occurrence correct; RFC5545 boundary rejects typed | Given the L1 signatures; When the gate unit tests run; Then each kind parses to a `TimingSpec`, `CronExpr()` yields a robfig-parseable expression, `NextOccurrence` is DST-safe, and out-of-profile RRULE constructs return `invalid_rule`/`frequency_too_high`/`invalid_time_zone` |
| T2 | Extend `JobConfig` with `Timing *TimingSpec` and relax `ValidateJobConfig` to accept Schedule OR Timing | Modify: `internal/scheduler/jobs.go:47-67, 714-767`; Test: gate | `go test -p 2 ./internal/scheduler/` | RED - validation still demands Schedule | GREEN - a job with only `Timing` validates; one with only `Schedule` still validates | Given a JobConfig with `Timing` set and empty `Schedule`; When `ValidateJobConfig` runs; Then it passes (and the inverse still passes) |
| T3 | Wire registration: `Scheduler` gains a `RegisterTimingVia(cfg, job)` wrapper used from `ScheduleConfig` (:357) and `loadPersistedJobs` (:750) so a persisted `Timing` re-registers on restart via `RegisterTiming` | Modify: `internal/scheduler/scheduler.go:357-370, 798`; Test: gate | `go test -p 2 ./internal/scheduler/` + F13a e2e | RED - timing job not scheduled | GREEN - a Timing job registers a robfig entry whose `Next` is the expected occurrence; restart re-registers it | Given a config with `Timing`; When registered through `ScheduleConfig` and reloaded by `loadPersistedJobs`; Then `NextRun` reports the DST-correct occurrence and a restart re-schedules it |
| T4 | Surface wiring: `scheduler.add_job` RPC accepts a `Timing` spec string (parse via `ParseTiming`, set `cfg.Timing` when `schedule` absent) | Modify: `internal/scheduler/rpc.go:42-109`; Test: gate + e2e | `go test -p 2 ./internal/scheduler/ -run TestAddJobTiming` (author in T-physically as part of this change if gate lacks it) | RED - `timing` param ignored | GREEN - creating a job with `timing` returns a JobID with a correct NextRun | Given `scheduler.add_job` with `timing:"daily 09:00:00 America/Denver"` (no `schedule`); When called; Then RPC accepts it and the job lands in the scheduler |
| T5 | Surface wiring: the `schedule_create` model tool gains an optional `"timing"` string param (exclusive with `"schedule"`; Required keeps `name`+`job_type`) | Modify: `internal/tools/builtin/tool_schedule_create.go:46-90` + Execute path | `go test -p 2 ./internal/tools/builtin/...` | RED - no `timing` param | GREEN - the tool doc exposes `timing`, and passing it (without `schedule`) creates a job | Given the tool with a `timing` arg; When invoked; Then it builds a JobConfig from the parsed TimingSpec and schedules it |

T1 is the substantive core; T2-T5 are thin wiring. Implement with TDD:
step 1 confirm each gate test fails RED, step 3 write the minimal
implementation, step 4 confirm GREEN. Where a gate test does not exist for a
wire (e.g. an explicit `TestAddJobTiming`), ADD it as a new test in the same
modify - additions flow through review like any code (gate tests already created
in 00T may be extended upward only, never weakened).

**RFC5545 supported profile (the boundary `Validate` + `CronExpr` must enforce):**

- ACCEPT: `FREQ=DAILY` (with optional `BYHOUR`/`BYMINUTE`); `FREQ=WEEKLY`
  with `BYDAY` (comma list maps to DOW); `FREQ=MONTHLY` with a single
  `BYMONTHDAY` 1-28. `DTSTART` must carry `TZID=<IANA>`; wall-clock resolved
  via `time.LoadLocation`.
- REJECT with a typed error (`invalid_rule` / `invalid_time_zone` /
  `frequency_too_high`), never a silent wrong schedule: `FREQ=YEARLY`,
  `FREQ=MONTHLY;BYDAY=-1FR` and any relative/nth weekday, `COUNT=`,
  `UNTIL=`, `FREQ=DAILY;INTERVAL=2`, `BYSECOND`/high-density, and any
  `BYMONTHDAY` outside 1..28. `Normalize` trims and collapses whitespace;
  an un-parsable rule returns `invalid_rule`.

**Kind-to-cron mapping (register in the scheduler location so `cron.WithLocation`
DST handling applies):**

- daily `HH:mm:ss TZ` -> 6-field `ss mm HH * * *` (Vixie DOW semantics; in s.location).
- weekly `DOW1,DOW2 HH:mm:ss TZ` -> `ss mm HH * * DOW1,DOW2`; Weekdays
  normalized ISO ascending-unique.
- at `RFC3339` -> a one-shot entry that unregisters after firing
  (cron.EntryID removed in the first run; if a plain entry can't self-remove,
  wrap with a chain that calls `cron.Remove` after the first fire - the trigger
  must fire exactly once).
- every `Dur` -> `@every <dur>` (assert `>= MinTimingEverySeconds` first).
- rrule -> a weekly/daily/monthly expression per the profile; the normalized
  `RRule` text stored; unsupported constructs rejected at parse/validate.

`NextOccurrence` uses `time.LoadLocation(t.TimeZone)` and returns
`(time.Time, bool)` (bool=false when no future occurrence) - the DST-safe
wall-clock seam the gate tests assert.

## Self-Verification Checklist

- [ ] All tasks implemented; gate suite GREEN
- [ ] L1 interface contracts satisfied exactly (types, method names, JSON tags)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only timing kinds; no delivery/receipt code here, and
      nothing re-adding run-now/pause/resume/delete

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path (expect `timing.go` NEW,
   `jobs.go`, `scheduler.go`, `rpc.go`, `tool_schedule_create.go`)
2. **Verify command + output:** the unit + e2e commands above; last 20 lines
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent - the orchestrator, in-session)

- [ ] Every task row implemented; tests passing; gate tests untouched
- [ ] L1 contracts match exactly (signatures, types, JSON paths)
- [ ] The RFC5545 boundary rejects exactly what it must and accepts what it may
- [ ] Conventions followed; no bugs, no security issues; predictable-ID rule
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] RPC + tool surface wired (model-facing rule)

Output: APPROVED or specific gaps with file + line references.

## Notes

- File count: this leaf touches 5 files (`timing.go` NEW + 4 thin wiring edits).
  The skill's "<=3 files" sizing rule is relaxed here because the root AGENTS.md
  wiring requirement obliges RPC (`scheduler.add_job`) AND tool (`schedule_create`)
  parity, and the task fixes this branch at exactly 2 impl leaves; only `timing.go`
  is a substantive new file - the other four are one-field/one-call edits.
- Do NOT touch `02-delivery-inbox.md` obligations: `AgentJobConfig.TargetSessionID`,
  receipts, `recoverInFlightDeliveries` belong to leaf 02.
- ASCII only in new .md edits; gofmt before reporting.
