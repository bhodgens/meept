# 03 — calls TodayCountsByEmployee

## DISPATCH INSTRUCTION

Implement this leaf end-to-end in the isolation worktree given in your
dispatch context (`.hermes/worktrees/situational`, absolute path). TDD:
the branch test-gate tests exist and are committed; make them GREEN.
**Do NOT commit** — no `git commit`, no `git add`. If a pinned contract
is impossible as written, leave tests RED and REPORT the conflict —
never silently change a frozen name or signature. Read the gate test
file FIRST; it is the executable spec.

- Parent: 02-daystate/orchestrator.md
- Scope: internal/voice/callstore.go new method (+ its test in existing NEW-file posture — add to callstore_test.go file if one exists else new file named per package norm).
- Dependencies: 00T §D
- Estimated context: 25K

## Interface Contracts (FROZEN)
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



## Tasks

1. TodayCountsByEmployee(ctx, employeeID, loc *time.Location)
   (counts voice.CallDayCounts{Today, Failed int}, error): local-midnight
   window like TodayTranscripts (:266 pattern — do NOT touch it), WHERE
   employee_id AND started_at >= midnight; Failed = outcome LIKE 'failed'
   exact outcome vocab (check OutcomeRegistry strings first; pin in doc).
2. RFC3339 string comparison safe (same as callstore existing).

## What This Leaf Exposes to Siblings

`CallStore.TodayCountsByEmployee`, `CallDayCounts`.

## Self-Verification Checklist

- [ ] §D GREEN; existing callstore tests untouched green.

## Review Checklist

- [ ] Pinned signatures/strings present exactly; no contract drift.
- [ ] Tests GREEN, `go build ./...` clean, gofmt, no artifacts.
- [ ] Nothing committed by the implementer.
