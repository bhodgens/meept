# Throttle Parking in the Agent Loop - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The agent loop parks the turn on ThrottleBackoffError using
  BackoffPlan scheduling; resume re-enters the loop; give-up surfaces
  the D8 error.
- **Dependencies:** 01-generalize-parker.md; tree 02 COMPLETE (wired
  ThrottleBackoffError + BackoffPlan)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Decision references:** D8, D9

## Goal

Tree 02 leaf 04 left the loop's ThrottleBackoffError branch as a clean
pass-through ("returns it to the caller, no rotation"). This leaf
replaces that pass-through with park-or-give-up:

- Park succeeds → the turn's agent state moves to a parked state (leaf
  04 finalizes naming; this leaf uses StateQuotaWait-style transition via
  SafeTransition with reason "throttle_wait"), the slot releases, and
  the TurnParker schedules resume at `plan.NextAttempt(now, err.Attempt,
  err.RetryAt)`.
- Park returns false (wait exceeds MaxWait) → the D8 give-up error
  surfaces to the caller: a wrapped error whose UserMessage() names the
  provider, the waited duration, and the next step ("provider throttled
  for Xh — turn abandoned; queue/goal policy applies"). The chat surface
  already has error rendering; match the QuotaResetError.UserMessage
  pattern (errors_quota.go:41).
- Resume: the TurnParker callback re-dispatches the ORIGINAL turn
  payload through the same entry path it arrived on (chat → ChatHandler
  path; specialist loop → its dispatch entry). Resume success/failure
  follows the loop's normal error handling — no special second path.

## Context

Key files:
- `internal/agent/loop.go:~4298` — the rate-limit branch; tree 02's
  ThrottleBackoffError branch sits just before it.
- `internal/agent/loop.go:4375` — the quota branch. ACCURACY NOTE: this
  branch does NOT itself park or transition state — it enters the
  QuotaEpisodeTracker (which drives StateQuotaWait via
  QuotaEpisodeTracker.setState, quota_episode.go:220), blocks the
  resolver entry, and returns the error; ChatHandler (handler.go) then
  builds the parked record via QuotaResumeWatcher.Park. Mirror the
  SPLIT pattern (loop signals, handler parks, tracker moves state) or
  deliberately deviate by parking inside the loop branch — either way
  document the choice; do not assume parked-record code exists at 4375.
- `internal/agent/quota_resume.go` / `parked_turn.go` — the parker API.
- The chat-side resume plumbing for parked quota turns (how a resumed
  chat turn re-enters ChatHandler) — mirror for the throttle class.
- `internal/llm/failure_policy.go` — DefaultBackoffPlan.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 2 (loop branch behavior) plus:

```go
// internal/agent/loop.go (or a small loop_park.go helper file)
// parkThrottledTurn is the branch body: builds ParkedTurnRecord from the loop
// state + error, calls the parker, transitions state, returns the
// give-up error when Park returns false.
func parkThrottledTurn(loop *AgentLoop, terr *llm.ThrottleBackoffError) error
// Give-up error type (in internal/llm/errors.go):
// ThrottleGiveUpError{ProviderID, ModelID string, Waited time.Duration}
//   - implements UserMessage() string (QuotaResetError pattern)
//   - NonRetryable() true (never re-enters any retry loop)
```

### What This Leaf Consumes

```go
// From 01: TurnParker.Park/ParkedTurnRecord
// From tree 02: ThrottleBackoffError{RetryAt, Attempt}, DefaultBackoffPlan
```

## Tasks

### Task 1: ThrottleGiveUpError

**Objective:** The D8 give-up error type.

**Files:**
- Modify: `internal/llm/errors.go`
- Test: `internal/llm/errors_test.go`

**Step 1:** Failing tests: Error()/UserMessage() content (provider,
model, waited, next step); NonRetryable() true; errors.As works.
**Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 2: Branch body + state transition

**Objective:** Park-or-give-up in the loop.

**Files:**
- Modify: `internal/agent/loop.go` (throttle branch)
- Test: `internal/agent/loop_test.go` (scripted provider returning bare
  429s; assert on parker state + returned error)

**Step 1:** Failing tests: (a) ThrottleBackoffError with future RetryAt
within MaxWait → ParkedTurnRecord recorded (Class=throttle, ResumeAt in
[RetryAt-clamped, now+MaxWait]), agent state transitioned with reason
"throttle_wait", loop returns no error to ITS caller (the turn is
parked, not failed); (b) RetryAt beyond MaxWait → ThrottleGiveUpError
returned, nothing parked; (c) the parked turn's Attempt count feeds
plan.NextAttempt (schedule honors backoff growth across attempts).
**Step 2:** FAIL. **Step 3:** Implement parkThrottledTurn; ensure no
model-rotation call, no RecordAliasFailure on this path (comment citing
D4: throttle is load-shedding, not model death). **Step 4:** PASS.

### Task 3: Resume path

**Objective:** The parked turn resumes through its original entry.

**Files:**
- Modify: the parker construction site (components wiring) — its resume
  callback routes by TurnPayload/entry metadata.
- Test: `internal/agent/parked_turn_test.go` extension (or the loop
  integration test file) — a scripted parked throttle turn resumes and
  completes when the provider recovers.

**Step 1:** Failing test: park a chat-class turn → advance the clock →
watcher fires → the captured resume payload re-enters the loop → success
recorded. (Chat routing exists for quota; REUSE the router — this leaf
adds the class dispatch, not a new router.) **Step 2:** FAIL.
**Step 3:** Implement. **Step 4:** PASS.

## Self-Verification Checklist

- [ ] Parked turns hold NO agent slot/goroutine while waiting (verify with a goroutine-count assertion in tests if the loop structure allows; otherwise state the check performed)
- [ ] Give-up error matches D8 wording requirements; NonRetryable
- [ ] No rotation/failure-recording on the throttle path
- [ ] Existing quota-park tests green; no chat behavior change
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master Contract 2 exactly
- [ ] State transition goes through SafeTransition (reason "throttle_wait")
- [ ] Attempt-based schedule growth asserted

Output: APPROVED or specific gaps with file + line references.

## Notes

- `now` for BackoffPlan: use the loop's injected clock; BackoffPlan math
  is pure (tree 02 leaf 02) — compose, don't recompute.
- If specialist-agent loops share this AgentLoop implementation, they
  inherit parking for free — verify with one dispatch-level test and
  note it (it shrinks leaf 03's scope).
