# 08 Advisory Budget Mode - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Add `llm.budget.mode: enforce | steer` (Contract C7). In `steer` mode a breached budget NEVER returns `BudgetExceededError`; instead the turn completes, the tier gate prefers cheaper aliases, and one advisory signal carries the warning. `enforce` is the DEFAULT and stays byte-identical.
- **Dependencies:** 00T gate tests committed (RED); 02-admission-context.md COMPLETE (serializes behind 02; both touch `provider_manager.go`)
- **Estimated Context:** 50K
- **Concurrency Group:** B2 (wave 2 - serialized behind 02)
- **Source reference:** Sovereign Router `src/governor.py:87-207` (`Steering`, `EmergencyConfig`) and the owner ruling in `docs/ROUTING-PRINCIPLES.md` rule 4: "No spending caps, ever... Cost is controlled by routing and by per-client daily targets that steer and alert, never block."

## Goal

meept's `BudgetConfig` (`internal/config/schema.go:1809-1821`) has eight limits - hourly/daily tokens and cost, per-task and per-session tokens and cost. Exceeding any one returns `*BudgetExceededError` (`internal/llm/budget.go:867-914`), which is `NonRetryable`, so **the turn is refused**. For a user mid-task that is a hard failure with no recourse except editing config and restarting the turn.

Sovereign Router takes the opposite position: a budget must never refuse a request. Its per-client daily targets steer routing (above target, cheap levels preferred) and alert, and never block. The one time it hard-cut, it 429-walled two fleets in one move (`docs/incidents/2026-11-09-rate-limit-cascade.md`).

meept should support both, with `enforce` as the default so nothing changes for existing installs. This is proposal P6 from the comparison, and it is a **product decision surfaced as a config flag** (Q2), not a change to the default.

## Context

- `internal/llm/budget.go:70-90` - `BudgetConfig` (the llm-side mirror). Gains `Mode`.
- `internal/config/schema.go:1809-1821` - the canonical `BudgetConfig` JSON schema. Gains `mode`.
- `internal/llm/budget.go:867-914` - `BudgetExceededError` with `NonRetryable()` and a per-reason `UserMessage()`. **This type and its message table stay exactly as they are** - `enforce` mode returns it unchanged.
- `internal/llm/budget.go:225-262` - `RecordCost` / `RecordCostWithScope`: where spend accrues. The advisory check reads the same counters.
- `internal/llm/client.go:801`, `:1063`, `:2552` and `internal/llm/anthropic.go:431`, `:679` - the FIVE `BudgetExceededError` return sites (grep-verified; an earlier draft said three and was wrong). **All five must consult the mode**, or the mode is a lie on some clients.
- `internal/llm/provider_manager.go` - `chatWithFailoverRaw`: the rotation loop. In steer mode this is where "prefer a cheaper alias" has to take effect.
- `internal/llm/AGENTS.md` - "Deferral parks at the handler, mirroring budget" and the park-event contract: park/resume/give-up ride the existing `agent.quota_wait` topic with a `class` key, NEVER a new topic prefix. The advisory follows that: class `budget_advisory` on the existing topic.
- `internal/llm/tier_policy.go:19-24` - `TierPolicy` + `TurnCeilings`. The "prefer cheaper" steer can reuse the tier concept, but this leaf must not change tier semantics.

## Verified Anchors

- `internal/llm/budget.go:70-90` - llm-side `BudgetConfig`
- `internal/config/schema.go:1809-1821` - canonical `BudgetConfig` (8 limits)
- `internal/llm/budget.go:867-914` - `BudgetExceededError` + `NonRetryable` + `UserMessage` table
- `internal/llm/budget.go:225-262` - `RecordCost` / `RecordCostWithScope`
- `internal/llm/client.go:801` - a `BudgetExceededError` return site
- `internal/llm/anthropic.go:431` and `:679` - the other two return sites
- `internal/llm/provider_manager.go:525-600` - `chatWithFailoverRaw`
- `internal/llm/tier_policy.go:42-47` - metadata signaling keys (the no-new-topic precedent)
- `internal/llm/AGENTS.md` - park-event `class` key contract
- `e2e/suites/sovereign-parity/budget_test.go` - gate tests T15, T16

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/budget.go
type BudgetMode string
const (
    BudgetModeEnforce BudgetMode = "enforce"  // DEFAULT - today's behavior
    BudgetModeSteer   BudgetMode = "steer"    // never refuse; warn + steer
)

// ParseBudgetMode maps a config string to a BudgetMode.
// "" or unknown -> BudgetModeEnforce (backward compatible, Q2).
func ParseBudgetMode(s string) BudgetMode

// Advisory reports the breach state WITHOUT blocking. It reads the same
// counters RecordCost maintains; it never mutates them.
type AdvisoryVerdict struct {
    OverTarget bool
    Limit      BudgetLimit   // the existing BudgetLimit enum
    Used       float64       // tokens or USD, matching Limit's unit
    Cap        float64       // the configured limit after aggressiveness
}
func (b *Budget) Advisory() AdvisoryVerdict
func (b *Budget) Mode() BudgetMode

// budgetAdvisorySignal is the advisory payload. It rides the EXISTING
// agent.quota_wait topic with class "budget_advisory" - never a new topic.
type BudgetAdvisorySignal struct {
    Class  string  // "budget_advisory"
    Limit  string  // the BudgetLimit reason string
    Used   float64
    Cap    float64
    Scope  string  // "hourly" | "daily" | "per_task" | "per_session"
}
```

### Frozen semantics

```
mode absent / unknown / empty   -> BudgetModeEnforce (byte-identical to today)
mode = enforce                  -> every existing BudgetExceededError site is
                                   UNCHANGED. This is the default and the
                                   flag-off control.
mode = steer                    -> the budget check NEVER returns
                                   BudgetExceededError. Instead:
                                     1. Advisory() reports the breach
                                     2. one BudgetAdvisorySignal is emitted
                                        on the existing quota_wait topic
                                     3. the tier gate prefers a cheaper alias
                                        when one exists
                                     4. the turn COMPLETES
```

`NonRetryable` is untouched: in `enforce` mode the error still refuses; in `steer` mode the error is never created. There is no third state.

### Non-goals

- **Do NOT remove or weaken any limit.** All eight limits still exist and still fire in `enforce`.
- **Do NOT change `BudgetExceededError.UserMessage()` text.** It is asserted by existing tests and is an operator-facing string.
- **Do NOT add a per-client daily-target concept.** meept has no multi-tenant cost target; Sovereign Router's per-client targets are its own product surface. The mode flag is the portable part.
- **Do NOT add a killswitch.** Out of scope.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `BudgetMode`, the constants, and `ParseBudgetMode` (`""`/unknown -> Enforce) | Modify: `internal/llm/budget.go` | `go test -p 2 ./internal/llm/ -run TestParseBudgetMode` | RED (undefined) | GREEN | Given "", "enforce", "steer", "garbage"; When parsed; Then Enforce, Enforce, Steer, Enforce |
| T2 | Add `Mode BudgetMode` to the llm-side `BudgetConfig` and `mode` to the canonical schema (`internal/config/schema.go:1809`) | Modify: `internal/llm/budget.go:70-90`, `internal/config/schema.go:1809-1821` | `go build ./...` | RED (compile) | GREEN | Given `"mode": "steer"`; When loaded; Then `cfg.Mode == BudgetModeSteer` |
| T3 | Implement `(*Budget).Advisory()` reading the existing counters without mutation | Modify: `internal/llm/budget.go` | `go test -p 2 ./internal/llm/ -run 'TestAdvisory_'` | RED (undefined) | GREEN | Given an exceeded daily cost cap; When advisory; Then `OverTarget=true` with the right `Limit`/`Used`/`Cap` |
| T4 | Enforce-mode invariance: with mode Enforce, all existing `BudgetExceededError` sites return the SAME error with the SAME message | Modify: none (assert) | `go test -p 2 ./internal/llm/ -run 'TestBudgetExceeded'` | n/a | GREEN, zero edits | Given an exceeded cap and no mode; When a turn runs; Then `*BudgetExceededError` and the message matches the existing table |
| T5 | Steer-mode gate: the budget check returns "allowed" in steer mode; no `BudgetExceededError` is constructed | Modify: all FIVE sites (`client.go:801`, `:1063`, `:2552`, `anthropic.go:431`, `:679`) | `go test -p 2 ./internal/llm/ -run 'TestBudgetSteer_'` | RED | GREEN | Given an exceeded cap and mode steer; When a turn runs; Then no error, turn completes |
| T6 | Emit exactly ONE advisory signal per breach per turn (dedup by limit+scope), on the existing `agent.quota_wait` topic with `class: "budget_advisory"` | Modify: `internal/llm/budget.go` (signal emission seam), the daemon wiring that owns the topic publish | `-run TestBudgetSteer_SingleSignal'` | RED | GREEN | Given an exceeded cap; When a turn runs; Then one signal, not one per budget check |
| T7 | Steer the tier gate: in steer mode, when a cheaper alias exists, prefer it (do not change tier depth semantics) | Modify: `internal/llm/provider_manager.go` (the tier-gate consult site) | `-run TestBudgetSteer_PrefersCheaper'` | RED | GREEN | Given a mid-rotation turn over target; When resolved; Then a cheaper alias serves when available |
| T8 | Client coverage: the mode is honored on ALL clients - OpenAI-compatible, Anthropic (both sites), Codex | Modify: verify all sites; add a Codex check if it has its own budget path | `go test -p 2 ./internal/llm/ -run 'TestBudgetSteer_Clients'` | RED | GREEN | Given each client; When over cap in steer mode; Then no refusal |
| T9 | E2E: steer mode completes a turn over the daily cost cap and surfaces `budget_advisory` | - | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestBudgetSteer_'` | RED | GREEN | Given a tiny cap already exceeded; When a turn runs in steer mode; Then the turn completes and the advisory is visible |
| T10 | E2E flag-off: enforce is the default and is byte-identical to `main` | - | `-run TestBudgetSteer_EnforceIsDefault'` | RED | GREEN | Given no mode key; When over cap; Then `*llm.BudgetExceededError`, turn refused |
| T11 | Document `mode` in `config/models.json5` or `meept.json5` with the two values and the default | Modify: the config template | config-drift-check | n/a | documented | Given the template; When read; Then the key is documented and defaults to enforce |

## Self-Verification Checklist

- [ ] T1-T11 complete; gate tests T15, T16 GREEN.
- [ ] `BudgetExceededError`, its `NonRetryable()`, and its `UserMessage()` table are UNCHANGED.
- [ ] All FIVE existing return sites (`client.go:801`, `:1063`, `:2552`, `anthropic.go:431`, `:679`) consult the mode; no site was missed.
- [ ] Enforce mode is byte-identical: T4 and T10 pass with zero modified assertions.
- [ ] Steer mode emits exactly one signal per breach, on the EXISTING topic with a `class` key.
- [ ] No new bus topic, no new WS type.
- [ ] The cheaper-alias steer does not change tier depth semantics.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen table matches master.md C7 (absent/unknown -> enforce).
- [ ] No limit was removed, renamed, or weakened.
- [ ] The default is enforce everywhere; steer is opt-in per `[llm.budget]`.
- [ ] Every client honors the mode; a missed site is the most likely defect in this leaf.
- [ ] The advisory signal rides the existing `agent.quota_wait` topic with `class: budget_advisory` and creates no new topic prefix.
- [ ] T4/T10 prove byte-identical enforce behavior.

## Do NOT commit

The orchestrator commits this leaf after review.