# 01-goal-model - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate —
> the failing tests ALREADY EXIST (00T-tests.md, `goal_autonomy_ext_test.go`)
> and are committed — your job is to make them pass; do NOT delete, skip,
> weaken, or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT read
> it back - write once and stop. Report what you built, files touched, and any
> deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Extend the `employee.Goal` data model + GoalStore with the
  autonomy surface (round cap, rounds started, live arming, blocked phase)
  that the model goal tools and round driver consume. No tools, no driver, no
  plan-mode here.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A (parallel with 04-plan-mode.md)
- **Audit references:** /tmp/dsh-audit/goal-workflow-plan.md Feature 1 (SHAPE)

## Goal

Meept's `Goal` today is durable but static: it has no round budget, no
activation, and no blocked phase — states are only Active/Paused/Retired.
This leaf adds the C4a autonomy surface so a goal can be armed for automatic
continuation, track how many rounds it has run, carry a durable blocked
reason, and refuse self-block before a consecutive-round threshold. Everything
here is additive to the existing struct and store; it does not rewrite the
opaque store or remove any existing field.

## Context

- `internal/employee/goal.go` — owns the `Goal` struct, `GoalState` enum,
  `GoalStore`, and `ParseGoalState`/`String()`. It compiles independently
  (no dependency on GoalLoop or Constitution, per the file's header).
- `internal/employee/goal_loop.go` — `GoalLoop.Decide` reads `goalStore` and
  drives tiers; this leaf only ADDS model surface, the driver wiring is leaf
  03. `goal_loop.go:14` admits "Tier 3 (autonomous, phase 2) would run the full
  cycle without the signoff gate" — that is the gap this branch closes.
- `defaults`: dsh `maxGoalRounds` default 256 and `blockedAfterConsecutiveRounds`
  default 3 come from the audit. Pin them via constants (like
  `DefaultMaxPlanHistory`) so config can override.
- Response parsers: `parseAssessResponse` etc. are UNCHANGED — this leaf does
  not touch the LLM prompt surface.

## Verified Anchors

- internal/employee/goal.go:37-47 — `GoalState` int enum (Active/Paused/Retired)
- internal/employee/goal.go:50-61 — `GoalState.String()` switch
- internal/employee/goal.go:66-77 — `ParseGoalState` (unknown -> GoalActive+err)
- internal/employee/goal.go:158-222 — `Goal` struct fields
- internal/employee/goal.go:232 — `DefaultMaxPlanHistory` constant pattern
- internal/employee/goal.go:525-578 — `GoalStore.migrate()` (additive ALTERs)
- internal/employee/goal.go:580-599 — `schema` `employee_goals` CREATE
- internal/employee/goal.go:619-669 — `Create`; :714-758 — `Update`
- internal/employee/goal.go:865-956 — `scanGoal` (row -> struct)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/employee — ALL from Contract C4a (verbatim):
const DefaultMaxGoalRounds = 256          // dsh default
const DefaultBlockedAfterConsecutiveRounds = 3

type GoalState int
const (
    GoalActive GoalState = iota
    GoalPaused
    GoalRetired
    GoalBlocked              // NEW: durable blocked phase
)

type BlockedReason struct {
    Code    string `json:"code"`     // e.g. "round-limit", "model-reported"
    Message string `json:"message"`
}

// NEW fields on Goal:
//   MaxRounds       int             `json:"max_rounds,omitempty"`   // 0 -> DefaultMaxGoalRounds
//   RoundsStarted   int             `json:"rounds_started"`
//   BlockedReason   *BlockedReason  `json:"blocked_reason,omitempty"`
//   Revision        int64           `json:"revision"`               // NEW CAS counter, +1 on Create/Update
// Live-only (never persisted, in-process atomic):
func (g *Goal) Activate()                        // arms continuation; idempotent
func (g *Goal) Disarm()                          // disarms; idempotent
func (g *Goal) IsArmed() bool
func (g *Goal) IsBlockedAfterNConsecutive() bool // RoundsStarted >= DefaultBlockedAfterConsecutiveRounds (3)

// String()/ParseGoalState accept "blocked" <-> GoalBlocked.
// GoalStore: persist MaxRounds, RoundsStarted, blocked_reason, revision
// via additive ALTER TABLE columns + scan/include in select/update/create.
```

### What This Leaf Consumes

- Contract C4a definitions ONLY (parent orchestrator holds them once). No
  downstream contract is consumed.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `GoalBlocked` state + `BlockedReason` + `DefaultMaxGoalRounds`/`DefaultBlockedAfterConsecutiveRounds` consts; extend `String`/`ParseGoalState` | Modify: `internal/employee/goal.go:37-77`, `:158-222` | `go test -p 2 ./internal/employee/ -run 'ParseGoalState\|GoalBlocked' -v` (gate test G1) | FAIL: `GoalBlocked` undefined; "blocked" not parsed | PASS: `blocked` round-trips to `GoalBlocked`, others unchanged | Given a state string "blocked"; When parsed and String()ed; Then it equals `GoalBlocked` and the gate passes |
| T2 | Add `MaxRounds`, `RoundsStarted`, `BlockedReason *BlockedReason`, `Revision int64` fields to `Goal` with resolving accessors (0 -> DefaultMaxGoalRounds) | Modify: `internal/employee/goal.go` struct + a `RoundsCap()` accessor | `go test -p 2 ./internal/employee/ -run Rounds -v` | FAIL: fields absent | PASS: fields exist; cap resolves 0->256 | Given a Goal; When I set MaxRounds=0; Then `RoundsCap()` returns 256 |
| T3 | Implement live activation: `Activate()`/`Disarm()`/`IsArmed()` (in-process atomic, zero value = disarmed) + guarded counter moves | Modify: `internal/employee/goal.go` | `go test -p 2 ./internal/employee/ -run Arm -v` | FAIL: methods missing | PASS: Activate arms, Disarm disarms, IsArmed reflects the latest | Given a fresh Goal; When Activate() then Disarm(); Then IsArmed is false after both |
| T4 | Implement `IsBlockedAfterNConsecutive()` (RoundsStarted >= 3 default) | Modify: `internal/employee/goal.go` | `go test -p 2 ./internal/employee/ -run 'IsBlockedAfterN' -v` | FAIL: method missing | PASS: 0-2 rounds -> false, >=3 -> true | Given RoundsStarted=4; When checked; Then it returns true |
| T5 | Persist the new fields: additive `ALTER TABLE` columns (max_rounds, rounds_started, blocked_reason_json, revision) + include in `Create`/`Update`/`scanGoal`/select queries; defensively default column absence (legacy rows) | Modify: `internal/employee/goal.go:525-578` (migrate), `:580-599` (schema), `:619-758` (Create/Update), `:835-956` (scan) | `go test -p 2 ./internal/employee/ -v` (existing store tests + G1) | FAIL (RED gating): new columns absent — store Create/scan fails to read back new fields | PASS: a goal created with MaxRounds/RoundsStarted survives a Get; `Revision` increments on Update | Given a goal persisted with new fields; When re-fetched via Get; Then the fields are intact and revision bumped |

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests passing (G1 RED -> GREEN)
- [ ] Interface contract C4a satisfied exactly (names, types, const values)
- [ ] All files at exact specified paths
- [ ] Existing goal store tests still pass (no regression in Create/Update/Get)
- [ ] No scope creep — no tools, no driver, no plan-mode code here
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** `go test -p 2 ./internal/employee/ -v` last 20
   lines; `gofmt -l internal/employee/` clean
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate test G1 untouched (git diff empty on it)
- [ ] Contract C4a matches exactly (field names, `DefaultMaxGoalRounds=256`,
      `DefaultBlockedAfterConsecutiveRounds=3`, `GoalBlocked`, methods)
- [ ] Conventions followed; no ignored errors; predictable-ID rules respected;
      mutex never held across store I/O
- [ ] Additive-only: no store rewrite, no existing field removed, legacy rows
      (columns absent) render defensively
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh source to re-read before pinning exact default values:
  `/tmp/deepseek-harness/packages/goal/goal/src/index.ts:239-251`
  (`defaultMaxGoalRounds: z.number().default(256)`) and
  `/tmp/deepseek-harness/packages/goal/tool-goal/src/index.ts:39`
  (`blockedAfterConsecutiveRounds ... .default(3)`).
- `Revision` mirrors dsh's event-fold revision but is a plain additive integer
  column (master OQ3: "additive versioning, DO NOT rewrite the opaque sqlite
  store"). Increment it in `Update` so `goal_update` (leaf 02) can CAS.
- Live activation is process-local by design and deliberately NOT persisted
  (dsh `GoalActivation` is "live-only"). On daemon restart an armed goal comes
  back disarmed — that matches dsh's session-start disarm edge.
