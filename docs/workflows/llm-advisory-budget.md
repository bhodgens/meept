# llm advisory budget mode (`llm.budget.mode`) — Contract C7

Sovereign-router-parity tree, leaf 08. Gives the existing token/cost budget a
non-refusing posture alongside the default enforcing one.

## Values

| `mode` | Behavior |
|---|---|
| (absent) / `""` / unknown | **enforce** — backward compatible; byte-identical to earlier releases |
| `"enforce"` | A breached budget returns `*llm.BudgetExceededError` (`NonRetryable`, unchanged message table). DEFAULT. |
| `"steer"` | A breach NEVER refuses a turn. `CheckBudget`/`CheckBudgetWithScope` report "allowed"; `(*Budget).Advisory()` reports the breach; exactly one `BudgetAdvisorySignal` per breach episode is emitted. |

Config surface: `llm.budget.mode` in `meept.json5` (JSON5+TOML twin tags,
absent by default). `.Mode` is read by the llm-side `llm.BudgetConfig.Mode`
— mapping happens in the daemon's `llmBudgetConfigFromConfig` and therefore
must be wired there (outside the leaf-08 write scope; see "Wiring" below).
No Go code invents a mode: zero value is enforce.

## Steer semantics (frozen)

1. `BudgetExceededError` is never constructed by the five budget return sites
   (they all call `CheckBudgetWithScope`, whose steer branch returns
   `Exceeded:false`).
2. `Advisory()` reports the breach WITHOUT mutating counters:
   `AdvisoryVerdict{OverTarget, Limit, Used, Cap}` (first global breach,
   same precedence as `CheckBudget`).
3. One `BudgetAdvisorySignal{Class:"budget_advisory", Limit, Used, Cap, Scope}`
   per breach episode `(limit | taskID | sessionID)` — deduplicated by an
   edge-triggered set cleared when a check observes no breach. Signals are
   collected under `b.mu` and published AFTER unlock (no mutex across I/O).
4. The signal rides the EXISTING `agent.quota_wait` bus topic with
   `class:"budget_advisory"` — no new topic, no new WS type (it classifies
   as `agent_progress` like every park event).
5. The tier gate prefers a cheaper alias when one exists — see "Cheaper-alias
   steer" below.
6. The turn COMPLETES.

`BudgetExceededError.NonRetryable()` and `UserMessage()` are untouched. No
limit was removed, renamed, or weakened; all eight limits still fire in
enforce mode. There is no third state.

## Wiring (what this leaf owns vs what the orchestrator must wire)

Owned by leaf 08 (committed separately):

- `internal/llm/budget.go` — `BudgetMode`, `ParseBudgetMode`,
  `(*Budget) Advisory()`, `(*Budget) Mode()`, `(*Budget) SetAdvisoryPublisher`,
  `BudgetAdvisorySignal`, `BudgetAdvisoryClass`, the mode gate on
  `CheckBudget`/`CheckBudgetWithScope` (the five client-side budget return
  sites therefore honor steer with zero edits).
- `internal/config/schema.go` — `BudgetConfig.Mode string json:"mode"`.

NOT owned by leaf 08 (write scope); orchestrator/leaf-02 wiring needed in
`internal/daemon/` (+ `internal/llm/provider_manager.go`) before the feature
is live end-to-end:

1. `internal/daemon/daemon.go` `llmBudgetConfigFromConfig` — map
   `bc.Mode → llm.BudgetConfig{Mode: llm.ParseBudgetMode(bc.Mode)}`, and pass
   `Mode` through the disabled (`!bc.Enabled`) early return as well
   (all-zero = unlimited = today's posture; mode is inert there).
2. `internal/daemon/components.go` — after `budgetTracker = llm.NewBudget(...)`,
   `budgetTracker.SetAdvisoryPublisher(func(sig llm.BudgetAdvisorySignal){ ...
   publish on "agent.quota_wait" with models.NewBusMessage ... })` (mirror
   the existing budget/throttle `SetParkEventBus` pattern; class
   `budget_advisory`).
3. Cheaper-alias steer: WIRED (forward lane, commit 2f34d16d).
   `steerReorderedTail(providers)` reorders the FAILOVER TAIL (index 1+)
   by ascending `TotalCost()` with `sort.SliceStable` when
   `pm.config.Budget.Mode() == BudgetModeSteer` AND
   `budget.Advisory().OverTarget`; the primary candidate is never moved,
   so tier depth semantics are untouched. Applied at all three rotation
   loops as `pm.steerReorderedTail(pm.orderedProvidersForRequest(opts))`
   (Chat, ChatWithProgress, chatWithAttemptTaggedDeltas). Enforce mode,
   nil budget, and no-breach states return the input slice unchanged —
   byte-identical rotation. Steering never refuses: the reorder can only
   change WHICH healthy candidate serves next, never whether the turn
   dispatches.

## Enforce-mode invariance

With `mode` absent/enforce, `applyModeGateLocked` returns the pre-leaf result
unchanged and the five return sites construct the identical
`*BudgetExceededError` with unchanged fields and message. `go test ./internal/llm/ -run
'TestBudgetExceeded'` stays green with zero edits.

## Docs to update when wired (not in this leaf's scope)

- `docs/configuration/llm.md` and `docs/configuration/token-budgets.md` — add
  the `mode` row (documented here instead; templates are not edited by leaf 08).
- `internal/llm/AGENTS.md` — extend the quota/park `class` contract sentence
  with `budget_advisory`.
