# Repeat-Identical-Error Tool Breaker - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. After
> writing a file, do NOT read it back to verify. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A turn-scoped circuit breaker that terminalizes a step/plan turn when the same tool with the same args fails with the same error 3 times.
- **Dependencies:** none (works with or without leaf 01; different package)
- **Estimated Context:** ~60K
- **Concurrency Group:** A
- **Audit references:** 2026-09-18 e2e (planner made 45+ identical `task_create{}` calls across 11+ abort→replan→fresh-turn rounds; 66 planner LLM calls; cycle detector reset each generation)

## Goal

The cycle detector aborts after 4 identical calls, but escalation replan and
replan-fallback start FRESH turn generations that call `resetTurnGuards` - so
the next generation repeats the same doomed calls. This leaf adds a breaker
keyed on (tool, canonical-args-hash, error-text) with a per-generation
memory that SURVIVES the abort/replan cycle: once the identical triple has
failed 3 times in a turn generation, subsequent identical attempts are
refused WITHOUT invoking the tool, with an honest terminal message. A
generation boundary that carries genuinely different args or a different tool
passes freely - only the identical triple is dead.

Design decision (from parent): the breaker keys its memory on the step/plan
EXECUTION SCOPE, not on the raw turn generation. Concretely: it lives on the
step-job / plan-conversation loop instance (which survives across the abort→
escalation→replan attempts of one logical unit of work), and resets only when
a NEW step or plan conversation starts. Verify where loop instances are
reused vs recreated (internal/agent/loop.go GetOrCreateWired / executor job
loop construction) and place the breaker accordingly; document the placement
decision in the report.

## Context

- `internal/agent/loop.go` is the agent loop; tool calls flow through
  `ExecuteToolCall` (find the exact name/seam - search `Executing tool` log
  line producer) or the executor in `internal/daemon/tool_invocations.go`.
- `resetTurnGuards` (loop.go:3885) resets per-turn guard state at ~:2503,
  :3187, :6358.
- The cycle detector (find `cycleDetector`) already counts identical SUCCESS
  or error calls within one turn; this breaker is cross-generation.
- Prior art for shape: `nudgeBudgetExhausted` / nudge caps (loop.go:3866,
  landed in 4394be7d).

Key files:
- `internal/agent/loop.go` - guard set, resetTurnGuards, tool-call seam
- `internal/agent/executor.go` - step-job execution (where loop instances are
  created per job - check lifetime)
- `internal/agent/escalation.go`, `strategic.go` - the replan cycle that
  resets generations

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/repeat_error_breaker.go
package agent

const maxIdenticalToolErrors = 3

// repeatErrorBreaker: per logical-work-scope breaker. Key:
// tool + "|" + sha256(canonicalJSON(args)) + "|" + firstLine(error).
type repeatErrorBreaker struct{ mu sync.Mutex; counts map[string]int; firstErr map[string]string }

func newRepeatErrorBreaker() *repeatErrorBreaker

// Observe records one failed call. Returns exhausted=true when this exact
// key has now failed maxIdenticalToolErrors times. summary is the honest
// terminal message for the step/plan turn:
//   "tool <name> rejected the identical input N times (<firstErr>); giving up"
func (b *repeatErrorBreaker) Observe(tool string, argsHash, errMsg string) (exhausted bool, summary string)

// Allow is the pre-call check: false when the key is already exhausted
// (the call must NOT reach the tool).
func (b *repeatErrorBreaker) Allow(tool, argsHash string) bool

// Reset clears all state (new step / new plan conversation).
func (b *repeatErrorBreaker) Reset()
```

Integration: on every tool-call failure path, `Observe`; when exhausted,
terminalize the current step (set step failed with summary; for plan
conversations, fail the planning attempt with the summary) instead of
returning the error to the model for another round.

### What This Leaf Consumes

- Existing loop guard-set patterns (nudge budget, cycle detector)
- The tool-call error seam (find where ToolResult.Error / err is set)

## Tasks

### Task 1: Breaker primitive

**Objective:** The breaker struct with Allow/Observe/Reset semantics.

**Files:**
- Create: `internal/agent/repeat_error_breaker.go`
- Test: `internal/agent/repeat_error_breaker_test.go`

**Step 1: Failing tests** (table-driven):

```go
func TestBreaker_ExhaustsAfterThreeIdenticalFailures(t *testing.T) { /* 3x Observe same key -> exhausted on 3rd; Allow false after */ }
func TestBreaker_DifferentArgsAllowed(t *testing.T) { /* same tool diff args -> independent counts */ }
func TestBreaker_DifferentErrorResetsCount(t *testing.T) { /* same args but DIFFERENT error text -> new key (the model may have fixed the arg) */ }
func TestBreaker_ResetClears(t *testing.T) { }
func TestBreaker_Concurrent(t *testing.T) { /* goroutine hammer: exactly one summary, no race (run under -race) */ }
func TestBreaker_CanonicalArgsHash(t *testing.T) { /* {"a":1,"b":2} and {"b":2,"a":1} hash equal - canonical JSON (sorted keys) */ }
```

**Step 2:** FAIL. **Step 3:** Implement with `encoding/json` canonical
marshaling (sort keys - write a tiny canonicalizer or use json.Marshal on a
sorted reconstruction; maps in Go marshal with sorted keys already - verify
and note it) + `crypto/sha256`. Mutex-protected. **Step 4:** PASS under -race.

### Task 2: Wire the breaker into the tool-call error path

**Objective:** Failed tool calls observe; exhausted keys refuse without executing.

**Files:**
- Modify: `internal/agent/loop.go` (guard set init + tool-call seam)
- Test: `internal/agent/repeat_error_breaker_loop_test.go`

**Step 1: Failing tests:**

```go
func TestLoop_IdenticalToolErrorTerminalizesStep(t *testing.T) {
    // loop with a fake tool that always errors "name is required"
    // run a turn whose model (fake chatter) calls it 4 times with identical args
    // assert: the 4th call NEVER reaches the tool (tool call counter == 3)
    // assert: the step/turn ends with the honest terminal message
}
func TestLoop_BreakerSurvivesGuardResetWithinScope(t *testing.T) {
    // call resetTurnGuards mid-scope (simulating the replan-generation reset)
    // breaker counts CONTINUE (it is NOT in resetTurnGuards' reset list)
}
func TestLoop_DifferentArgsGetFreshBudget(t *testing.T) { }
```

**Step 2:** FAIL. **Step 3:** Add `repeatErr *repeatErrorBreaker` to the loop
struct (constructed with the loop, NOT reset in resetTurnGuards - document
why in a comment at resetTurnGuards). In the tool-call failure seam: if
`!b.Allow(...)` return the summary as a terminal ToolResult error without
executing; else run, and on failure `Observe`. When exhausted, terminalize:
return the summary wrapped so the step/plan attempt ends (mirror how the
cycle detector's abort terminalizes - find `agent detected a cycle` producer
and use the same mechanism). **Step 4:** PASS.

### Task 3: Placement verification + honest-failure copy

**Objective:** Prove the breaker's scope survives the real abort→replan cycle.

**Files:**
- Modify: `internal/agent/repeat_error_breaker.go` (comments only if wiring already right)
- Test: extend `repeat_error_breaker_loop_test.go`

**Step 1:** Write `TestBreaker_ScopeSurvivesReplanCycle`: simulate
(1) plan attempt with failing tool x3 → breaker hot; (2) the escalation/
replan path re-enters the SAME loop instance (verify: does
`handlePlanRequest` → `strategic.Plan` reuse the planner loop? Trace it. If
replan creates a NEW loop instance per attempt, the breaker must instead live
on the shared executor/step-job scope - move it and document). Assert the
4th identical call is refused at the new attempt. **Step 2/3:** adjust
placement per findings; **Step 4:** PASS. Report the actual lifetime chain
you found (file:line) - this is the load-bearing design fact.

## Self-Verification Checklist

- [ ] Breaker primitive green under -race
- [ ] 4th identical call provably never reaches the tool (counter pin)
- [ ] Honest terminal message shape: `tool <name> rejected the identical input N times (<firstErr>); giving up`
- [ ] Placement decision documented with file:line evidence
- [ ] go build ./... clean; gofmt clean; `go test -p 2 -count=1 ./internal/agent` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Breaker NOT reset by resetTurnGuards (comment + test prove)
- [ ] Different-args and different-error keys get fresh budgets (model can recover)
- [ ] Canonical hash order-insensitive
- [ ] Terminal message reaches the user-visible reply path, not just logs
- [ ] No new goroutines/leaks; mutex scope clean (no I/O under lock)

Output: APPROVED or specific gaps with file:line.

## Notes

- The 45-call loop cost 66 LLM calls at ~6K tokens: containment has real cost
  value even though the model is the root cause.
- Do NOT touch nudge-cap or overflow machinery (4394be7d) - complementary.
- If you find the tool-call seam is in internal/daemon/tool_invocations.go
  rather than the loop, wire there and note the deviation - contract stays.
