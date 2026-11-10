# Employee Situational Awareness (issue #128) — Master Plan

Date: 2026-11-10. Repo: meept. Tree: docs/plans/20261110-situational/
Worktree: .hermes/worktrees/situational (branch plan/situational)

## Goal

Give every employee a basic surface of knowledge, three layers: (1) the
moment - local time/date/weekday in the employee's own timezone, in
every prompt for chat AND voice turns; (2) the place - per-employee
location + optional weather awareness with domain thresholds for
outdoor trades (concrete/roofing: cold and rain matter); (3) the
day-state - a COMPUTED state (clear/busy/behind/overloaded) from real
signals (ledger open+overdue commitments, goal health, today's call
outcomes, activity volume) with a persona tone line per state.

## Architecture Overview

Grounding (survey 2026-11-10, file:line verified):
- Chat prompts funnel through assembleSystemPrompt
  (internal/agent/loop.go:9140) with the volatile StatusBar precedent
  (:9142); voice turns BYPASS the builder via SystemPromptOveride
  (voice_employee.go:261-275) so the override path (loop.go:9125) must
  append the same block. Two choke points, one renderer.
- BotDefinition is a JSON blob (internal/bot/store.go:31-45): new fields
  Timezone/LocationLabel/Weather/PersonaDayState need ZERO ALTER
  (ExecutorAgent precedent bot/types.go:96-103).
- Signals: ledger QueryOpen (ops.go:26; needs the additive due_at column
  for overdue), goal ListActive (goal.go:887; states :41-51), calls.db
  (NEW per-employee today-counts method), activity Aggregator.Query
  AgentID filter (sources.go:16).
- Weather: keyless open-meteo; client copies the SSRF-guard pattern
  (web_fetch.go:158, ssrf.go); refresh via simpleIntervalJob
  (employee_scheduler_adapter.go:49-85).
- Persona: SOUL stable slot untouched (soul.go; prompt.go:604); tone
  lines ride the VOLATILE moment block only.

Owner-ratified principles: state is COMPUTED, never LLM-assessed;
modulation changes pacing/priorities, never facts; per-employee persona
override allowed.

## Decisions (do NOT re-litigate)

- D1 [awareness] config table, `enabled` DEFAULT FALSE: a daemon that
  does not opt in renders byte-identical prompts to today.
- D2 moment block is volatile-only; prompt cache stability preserved
  (StatusBar pattern, never a stable section).
- D3 day-state ladder thresholds frozen in contracts (owner may tune
  later via code const PR, not config, v1).
- D4 ledger due_at is the ONE additive schema change in this tree
  (guarded-ALTER idiom; existing 26 ledger tests untouched+green).
- D5 weather alerts are rule-based per trade only; unknown trades yield
  nothing; no LLM interpretation of weather.
- D6 voice lane appends the moment block to the override prompt per turn
  (not user-message prefix; daybrief's first-turn-only idiom does not
  extend to per-turn moment data).

## Interface Contracts (FROZEN — every leaf quotes these verbatim)

Config (BotDefinition JSON blob, internal/bot/types.go:83 — the blob
round-trips with ZERO ALTER, executor-agent precedent at :96-103):
```go
Timezone      string // IANA name ("America/Denver"); '' = scheduler tz; bad name -> UTC + WARN
LocationLabel string // free-form place ("Rapid City shop")
Weather       BotWeather // optional; nil/zero = no weather awareness
type BotWeather struct {
    Lat, Lon float64
    Trades   []string // "concrete" | "roofing" | "earthwork" | ...
}
PersonaDayState map[string]string // state -> tone line override ("behind": "..."); nil = built-in tones
```

internal/moment package:
```go
type Snapshot struct {
    LocalNow  time.Time // employee-local wall clock
    Weekday   string
    Label     string    // LocationLabel ('' ok)
    Weather   *weather.Outlook // nil when absent/disabled
    WeatherAlerts []string     // computed thresholds, may be empty
    DayState  string  // "clear|busy|behind|overloaded" ('' = daystate unavailable)
    Tone      string  // persona tone line for DayState ('' when none)
}
type Provider struct{ ... } // clock seam + per-employee render; nil-safe
func (p *Provider) Render(def *moment.EmployeeDef) string
// EmployeeDef mirrors the BotDefinition fields above (avoid bot import;
// daemon passes a small struct). Output block:
//   # Moment
//   It is 14:02 MST, Monday, at Rapid City shop.
//   Weather: 3C falling to -2C, rain 60%. RISK: cold — concrete pour at risk.
//   Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first.
// (sections omitted when their data is absent — stable byte-identical
// block WITHOUT weather/daystate equals today's absent behavior)
```

Wiring choke points (survey-verified): chat lane = builder volatile
section inside `assembleSystemPrompt` (internal/agent/loop.go:9140,
StatusBar pattern at :9142, stability=false via
AddSectionWithStability — internal/agent/prompt.go:191-200 precedent);
voice lane = `buildSystemPromptWithOverride` (loop.go:9125) APPENDS the
same rendered block (one branch), because voice turns short-circuit the
whole PromptBuilder (voice_employee.go:261->275 override). AgentLoop
gains `WithMomentProvider(*moment.Provider)` +
`SetMomentDef(fn func() EmployeeDef)` style lane-neutral seams (impl
leaf picks exact shape; MUST keep prompt-cache stability: the moment
section is volatile, NEVER stable).

internal/daystate package:
```go
type Signals struct {
    OpenCommitments, Overdue, ActiveGoals, BlockedGoals int
    CallsToday, CallsFailedToday, ActivityItemsToday    int
}
type State string
const (StateClear State="clear"; StateBusy="busy"; StateBehind="behind"; StateOverloaded="overloaded")
// Deterministic ladder (owner-tunable constants, FROZEN here):
// overloaded: Overdue>=5 || BlockedGoals>=2 || OpenCommitments>=12
// behind:     !(state below busy) — i.e. Overdue>=1 || OpenCommitments>6 (residual case)
// busy:       OpenCommitments in 3..6 && Overdue==0 && BlockedGoals==0
// clear:      OpenCommitments<=2 && Overdue==0 && BlockedGoals==0
func Compute(s Signals) State
func Tone(st State, persona map[string]string) string // persona override wins; built-in:
// clear: "Steady day. Proactively offer next steps."
// busy: "Loaded but clean. Prioritize; batch small asks."
// behind: "Keep it terse; surface blockers first."
// overloaded: "Escalate: name the top 3 overdue items and who owes them."
type Source interface { Signals(ctx context.Context, employeeID string) (Signals, error) }
```

Ledger extension (plan 20261109-employee-ledger is MERGED; this is an
additive guarded-ALTER follow-on, house idiom contact/store.go:58 +
callstore.go:126):
```sql
ALTER TABLE ledger_state ADD COLUMN due_at TEXT NOT NULL DEFAULT ''
CREATE INDEX IF NOT EXISTS idx_ledger_state_due ON ledger_state(due_at) WHERE due_at <> ''
```
Go: DraftRecord gains `Due time.Time` (zero = none; only meaningful when
SetStatus != ""); envelope gains `"due"` (RFC3339 or ""); state UPSERT
writes due_at (overwritten on each state-setting append; '' clears);
State gains `Due time.Time` (zero when none); QueryOpen returns Due;
Overdue predicate: status IN (open,waiting_on_me,waiting_on_them) AND
due_at != '' AND due_at < now-RFC3339 comparison. VerifyState fold
compares due as part of Want (same envelope source). ledger_test.go
existing 26 tests MUST stay green; add new due-section cases in NEW test
file only.

internal/weather package:
```go
type Outlook struct {
    NowC, TodayHighC, TodayLowC, WindKph float64
    RainProbToday                        int // 0-100
    Fetched                              time.Time
}
type Fetcher interface { Fetch(ctx context.Context, lat, lon float64) (Outlook, error) }
// open-meteo impl, keyless: https://api.open-meteo.com/v1/forecast?...
// 10s timeout; reuse the SSRF-guarded client CONSTRUCTION pattern from
// internal/tools/builtin/web_fetch.go:158 + ssrf.go (not the tool
// itself). Cache: package-level per (lat,lon) 30 min TTL, singleflight,
// stale-on-error allowed.
func AlertsFor(trades []string, o Outlook) []string
// FROZEN thresholds (concrete/roofing domain, owner-tunable later):
// "concrete": NowC<5 || (TodayHighC-TodayLowC drop) -> TodayLowC<0 => "cold — concrete pour at risk"
//             RainProbToday>=40 => "rain — exposed concrete work at risk"
// "roofing":  WindKph>=50 => "high wind — roofing work at risk"
//             RainProbToday>=40 => "rain — roofing work at risk"
// "earthwork": RainProbToday>=60 => "rain — excavation at risk"
// unknown trades -> no alerts (never fabricate).
```

Signals source (daemon composite over existing APIs, survey-verified):
ledger Store.QueryOpen (open+overdue counts, per employee);
employee.GoalStore.ListActive(ctx, employeeID) internal/employee/goal.go:887
(Active + Blocked via GoalState enum :41-51); NEW
voice.CallStore method `TodayCountsByEmployee(ctx, employeeID, loc)
(counts struct{Today, Failed int}, error)` — `WHERE employee_id=? AND
started_at >= local-midnight` over calls.db (TodayTranscripts :266 is
number-scoped and must NOT be repurposed); activity Aggregator.Query
with Query.AgentID internal/activity/sources.go:16, limit 1000.
DAYSTATE COMPUTE IS READ-ONLY + BOUNDED (2s ctx per source; any source
error -> zero contribution + DEBUG log; NEVER blocks a turn — if all
sources fail, Render omits the day-state line entirely).

Scheduler: weather-refresh + daystate warm job via simpleIntervalJob
pattern (internal/daemon/employee_scheduler_adapter.go:49-85; trace
example :154-176; ledger-integrity precedent bb1f4343). Config keys
under new [awareness] table (schema.go sibling pattern):
enabled bool default true? NO — default FALSE (opt-in; no behavior
change for existing daemons), refresh_minutes int default 30.

No phone digits in weather/daystate lines (daystate carries counts only).
Soul/persona STABLE sections untouched (survey Q6: personality slot
prompt.go:604 stays stable; moment is volatile).

## Child Index

| Child | Type | Deps |
|---|---|---|
| 01-moment/00T | test-gate | — |
| 01-moment/01 | leaf (moment pkg) | 00T |
| 01-moment/02 | leaf (chat+voice wiring) | 01 |
| 02-daystate/00T | test-gate | — |
| 02-daystate/01 | leaf (ledger due_at) | 00T |
| 02-daystate/02 | leaf (daystate pkg + signals source) | 01 |
| 02-daystate/03 | leaf (calls today-counts method) | 00T |
| 03-weather/00T | test-gate | — |
| 03-weather/01 | leaf (weather pkg) | 00T |
| 03-weather/02 | leaf (refresh job + moment feed) | 03-01, 01-02 |

Parallelism: all three 00T gates dispatch together. 02-daystate/01
(ledger) and 03-weather/01 (new pkg) are independent of 01-moment. The
moment RENDERER takes optional Weather+DayState inputs (nil today);
03-02/02-02 feed them via the daemon wiring in
internal/daemon/awareness_wiring.go (OWNED serially: 01-02 creates,
02-02 edits, 03-02 edits last).

## Dispatch Protocol

1. Worktree created (plan/situational). Per branch: dispatch its 00T
   gate first (tests only, RED-for-missing only, Do NOT commit);
   validate IN-SESSION against the Test Gate matrix; commit gate tests;
   TESTS_VALIDATED.
2. Implementation leaves per branch order; cross-branch parallel EXCEPT
   awareness_wiring.go (serialized 01-02 -> 02-02 -> 03-02).
3. Review IN-SESSION (main model); orchestrator-only commits, per-file
   pathspec. Max 3 loops; update tracking tables at every transition.
4. Gate tests reference symbols that may not compile yet — RED posture
   per branch, same discipline as 20261110-caller-authz.

## Coding Conventions

As branch orchestrator docs; additionally: time comparisons in day-state
use UTC RFC3339 strings like due_at (lexicographic-safe); clock seams for
all time-dependent tests.

## Test Gate

| Branch | Gate | Status |
|---|---|---|
| 01-moment | 00T | PENDING |
| 02-daystate | 00T | PENDING |
| 03-weather | 00T | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 01/00T | PENDING | | |
| 01/01 | PENDING | | |
| 01/02 | PENDING | | |
| 02/00T | PENDING | | |
| 02/01 | PENDING | | |
| 02/02 | PENDING | | |
| 02/03 | PENDING | | |
| 03/00T | PENDING | | |
| 03/01 | PENDING | | |
| 03/02 | PENDING | | |

## Integration Test Plan

1. `go build ./... && go vet` clean.
2. `go test ./internal/moment/ ./internal/daystate/ ./internal/weather/
   ./internal/ledger/ ./internal/voice/ ./internal/agent/
   ./internal/daemon/ -count=1` — existing ledger 26/26 + agent/daemon
   suites UNCHANGED green; [awareness] default-off golden proves prompt
   byte-identity (a daemon-level test renders a voice override and a
   chat prompt with awareness disabled and compares to pre-tree shape).
3. Live sanity (daemon on temp DataDir, enabled=true, one employee w/
   timezone+location+weather, seeded ledger commitments incl. one
   overdue): assert the moment block appears in the voice prompt path
   (log/debug capture) with correct local time, weather line, and
   `behind` state; assert chat lane volatile section present and NOT
   cached (two consecutive turns, block present in both, prompt-cache
   prefix stable).
4. `-race` on moment/weather/daystate packages.
5. Merge order: this tree lands after plan/stranger-lane (already
   merged b29f9ab0) — ledger due_at Append edit must not conflict with
   the Q1 gate lines; rebase-check ledger.go tools file untouched by
   this tree (only store/ops change).

## Review Checklist

- [ ] D1 no-regression golden green (disabled daemon byte-identical).
- [ ] Moment/volatile discipline: grep builder additions use
      AddSectionWithStability(..., false); stable slots untouched.
- [ ] Voice override append at loop.go:9125 path verified against
      voice_employee override site (survey refs).
- [ ] due_at guarded-ALTER idiom + fold/VerifyState due handling.
- [ ] Weather: no secrets, no new transport deps, SSRF guard present.
- [ ] Day-state is deterministic function of signals — grep for any
      LLM call inside daystate (there must be none).
- [ ] awareness_wiring.go serialized edits; tracking tables honest.

## Open Questions

- Q1: day-state refresh cadence per turn (compute inline w/ 2s budget vs
      cached warm by job) — impl default inline+cheap, warm job optional
      later; pin choice in 02-02 report.
- Q2: weather units — contract pins metric (C, kph); imperial display
      deferred (owner is US; counts in prompts, units can flip later).
- Q3: multiple employees sharing one shop location -> same weather fetch
      (cache keyed by lat/lon handles it; note in 03-01 doc).
