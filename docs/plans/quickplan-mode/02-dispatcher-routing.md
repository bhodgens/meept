# Leaf 02 — Dispatcher Routing: mode plumbing, ambiguity gate, fallback

## DISPATCH INSTRUCTION

> **Implementing agent:** implement ALL tasks below via TDD. Do NOT
> commit. Do NOT run git add. Write code, run tests, report results only.
> Explore with search_files or `terminal cat` — never feed read_file
> output into write_file. After writing a file, do NOT read it back.

- **Parent:** docs/plans/quickplan-mode/master.md
- **Scope:** dispatcher mode plumbing for quick_plan, ambiguity-gate
  interaction (clarify first, resume as quickplan), final-fallback swap,
  session-state context block for the orchestrator
- **Dependencies:** 01-intent-type.md (IntentQuickPlan constant)
- **Estimated context:** ~85K

## Interface Contract (exposed)

```go
// validModes gains "quick_plan"
// suggestMode(IntentQuickPlan, ...) == "quick_plan" (never short-input
//   downgraded)
// Final fallback Intent: {Type: "quickplan", AgentType: "orchestrator",
//   Confidence: 0.3, Method: "fallback"}
// Ambiguity gate unchanged in behavior for non-quickplan intents;
// quickplan pending-clarification resumes with mode=quick_plan.
```

## Current-source pins (verified 2026-09-09; re-verify at dispatch)

- `validModes` map: dispatcher.go ~line 1157 (`var validModes = map[string]struct{}{ "direct", "plan", "spec_plan", ... }`)
- `suggestMode`: dispatcher.go:1241
- Final fallback Step 5: dispatcher.go ~1143-1153
- Ambiguity gate: dispatcher.go:830 (`buildClarificationResult` at :1380)
- Clarification resume: `isPendingClarification` (dispatcher.go:671),
  `ResumeAfterClarification`, `clearPendingClarification` (:1509)
- Short/simple guard (routes to chat — DO NOT change): dispatcher.go ~961

## Tasks

### Task 1: Register the mode

dispatcher.go `validModes`: add `"quick_plan": {}`.

`suggestMode()` (line 1241): add BEFORE the analysis short-input
downgrade logic:

```go
if intentType == IntentQuickPlan {
    return "quick_plan" // explicit execution phrasing IS the signal;
                        // never short-input downgraded
}
```

### Task 2: Final fallback swap

classifyIntent() Step 5 (~line 1143):

```go
// Step 5: Final fallback — quickplan: clarify-if-needed, plan, execute
d.recordFallback(input, "all_classifiers_failed", 0.0, "orchestrator")
d.recordClassificationMethod("fallback")
d.recordAgent("orchestrator")
d.recordIntentType(string(IntentQuickPlan))
return &Intent{
    Type:       string(IntentQuickPlan),
    Confidence: 0.3,
    AgentType:  "orchestrator",
    Summary:    "Could not determine intent; planning and executing with clarification as needed",
    Method:     "fallback",
}, nil
```

NOTE: the short/simple guard EARLIER in classifyIntent still routes
short inputs to chat — unchanged. Grep `AgentIDChat` usages you touch;
do not alter unrelated chat paths.

### Task 3: Ambiguity gate — clarify, then resume as quickplan

The ambiguity gate (line 830) fires before execution and must keep doing
so for quickplan. Work required:

1. `buildClarificationResult` (line 1380): accept the pending mode —
   when the caller's intent type is (or resolves to) quickplan, record
   the pending-clarification state with `PendingMode: "quick_plan"` (add
   the field to the pendingClarification struct at ~line 1426, default
   empty for existing flows).
2. `ResumeAfterClarification` path: when pending mode is `quick_plan`,
   the resumed dispatch must carry `SuggestedMode: "quick_plan"` (set on
   the synthesized intent) so the post-clarification turn executes
   without a new approval gate. The clarification ANSWER completes the
   plan; it never downgrades to plain chat.

### Task 4: Session-state context for the orchestrator

The orchestrator receives session context so it can upgrade code/git →
quickplan at execution time (campaign finding: the quickplan signal is
session state, invisible to per-message classification).

Locate where the orchestrator's planning prompt/context is assembled for
deferred intents (the `CategoryDefer` path — follow
`ShouldCreateTask()`/orchestrator dispatch in executor.go or handler.go;
grep `orchestrator`). Add to that context block a `Session execution
context` section containing, when present:

- Active plan: ID + title + state (from the session's plan tracker)
- Open tracked tasks: count + first N titles
- Prior waves/quickplan runs in this conversation (from sessionTracker
  intents)

Plus this instruction line in the orchestrator prompt:

```
If the session shows an active plan or open tracked tasks, treat
code/git-classified requests as continuations of that flow (quickplan
execution) rather than fresh single-intent work. One-way: session
evidence upgrades to quickplan; absence never downgrades an explicit
quickplan.
```

### Task 5: Tests

File: `internal/agent/dispatcher_quickplan_test.go`

Cover, table-driven where natural:

1. `suggestMode(IntentQuickPlan, nil, "x") == "quick_plan"` (short input
   NOT downgraded)
2. `"quick_plan"` accepted by `validModes`
3. Final fallback: input that reaches Step 5 produces
   `Intent.Type == "quickplan"`, `AgentType == "orchestrator"`,
   `Method == "fallback"`
4. Ambiguity: ambiguous quickplan input → `ClarificationNeeded == true`;
   simulated resume (pending mode quick_plan) → suggested mode stays
   quick_plan
5. Short/simple guard still routes "hi" to chat (regression guard)
6. Existing plan/spec_plan routing tests unchanged and passing

Use the existing test fakes in dispatcher_prefilter_test.go for
embed/registry stubs; do not introduce new mocking frameworks.

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` passes
- [ ] `go test ./internal/agent/ -short -count=1` passes
- [ ] New tests 1-6 pass; no pre-existing test regressed
- [ ] grep: `recordAgent("orchestrator")` in final fallback
- [ ] grep: `"quick_plan"` present in validModes
- [ ] Ambiguity gate code path untouched for non-quickplan intents
- [ ] gofmt clean; no TODOs/debug prints

## Review Checklist (orchestrator)

- [ ] All 5 tasks implemented; contract 2/4/5 satisfied
- [ ] Clarification resume preserves quick_plan (test 4 proves it)
- [ ] Chat floor for short inputs intact
- [ ] No unrelated dispatcher behavior changed (diff scope check)
- [ ] No stray artifacts
