# Client Retry-Loop Unification - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Rewire all five client retry loops onto Classify +
  BackoffPlan; long-horizon throttle handling returns a park-capable
  signal instead of burning out; delete the hardcoded constants.
- **Dependencies:** 01-classification.md, 02-retryafter-schedule.md
- **Estimated Context:** 85K
- **Concurrency Group:** C (dispatch FIRST in C, serially before leaf 04)
- **Decision references:** D4, D6, D8

## Goal

The five loops (openai non-streaming client.go:426, openai streaming
client.go:586, openai streaming-delta client.go:1269 — NOTE: this loop's
count constant is `streamMaxRetries`, NOT `maxRetries` (audit
2026-09-01); anthropic non-streaming anthropic.go:281, anthropic
streaming anthropic.go:448) share copy-pasted backoff and burn out in
~90s on sustained throttle. After this leaf:

1. Every loop's failure branch calls `llm.Classify` FIRST. Quota class →
   existing QuotaResetError early-exit (unchanged contract). Throttle →
   short-horizon retries (a small bounded count, e.g. 3) obeying
   Retry-After from ParseRetryAfter; if the plan says keep waiting past
   the short budget, the loop returns a NEW error type
   `ThrottleBackoffError{RetryAt time.Time, Attempt int, Class FailureClass}`
   instead of failing — the CALLER (agent loop / tree 03) parks.
   ServerError → existing bounded behavior via the plan. Fatal →
   immediate return.
2. `maxRetries = 3`, `streamMaxRetries`, and the 30s cap constants leave
   the five loops (client.go:25-27 and anthropic equivalents); short-loop
   counts come from config `failure_policy.short_retries` (default 3).
3. NO behavior change for quota: QuotaResetError paths keep their exact
   early-exit placement and tests (SHARED-CONVENTIONS §2).

## Context

Key files:
- The five loops (client.go:426 non-streaming, :586 streaming, :1269
  streaming-delta `for attempt := range streamMaxRetries`; anthropic.go
  :281 and :448). Grep BOTH `maxRetries` AND `streamMaxRetries` plus
  `for attempt` to catch every loop.
- `internal/llm/client.go:1349` — existing "use Retry-After from rate
  limit response" logic (subsumed by ParseRetryAfter).
- `internal/llm/errors.go` — add ThrottleBackoffError beside
  RateLimitError; implement `RetryAt()`-style accessor if the repo's
  error conventions expect one (check APIError/RateLimitError patterns).
- `internal/agent/loop.go:~4298` — the agent-loop RateLimitError branch.
  This leaf ADDS a ThrottleBackoffError branch that returns the error to
  the caller WITHOUT rotating (rotation is for dead models, not
  load-shedding — record this rationale in a comment citing D4/D8);
  tree 03 replaces the branch body with parking.

Streaming subtlety: the streaming loops cannot retry mid-stream after
tokens flowed; only failures BEFORE first token retry — the loops
already gate on this; preserve each loop's existing gating exactly and
note where a mid-stream throttle surfaces as ThrottleBackoffError.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/llm/errors.go (add)
// ThrottleBackoffError: sustained provider throttling exceeded the
// short in-loop budget. Carries the earliest future attempt time so the
// caller can park. Implements error + Unwrap(cause).
type ThrottleBackoffError struct {
    ProviderID string
    ModelID    string
    RetryAt    time.Time
    Attempt    int
    Cause      error
}
func (e *ThrottleBackoffError) Error() string
func (e *ThrottleBackoffError) Unwrap() error
// NOTE: the plan docs also reference a classification accessor named
// AsThrottleBackoffError (tree 03 leaf 03 consumes it). It does not exist
// today (only IsQuotaResetError exists, errors_quota.go:87); leaf 03 of this
// tree adds it here alongside the type:
func AsThrottleBackoffError(err error) (*ThrottleBackoffError, bool)
```

```go
// Each loop's contract: classify → obey → escalate:
//   FailureQuota   → return QuotaResetError immediately (existing)
//   FailureThrottle→ retry while attempt < cfg.short_retries honoring
//                    ParseRetryAfter/BackoffPlan; then return
//                    ThrottleBackoffError{RetryAt: plan-derived}
//   FailureServerError → bounded via plan; give up → ClientError (existing)
//   FailureFatal   → return immediately (existing)
```

### What This Leaf Consumes

```go
// From 01/02:
Classify(statusCode, header, body, now) PolicyVerdict
ParseRetryAfter(header) (time.Time, time.Duration, bool)
DefaultBackoffPlan(class, now, cfg) BackoffPlan
FailurePolicyConfig (config leaf 02)
```

## Tasks

### Task 1: ThrottleBackoffError

**Objective:** The park-capable error type exists.

**Files:**
- Modify: `internal/llm/errors.go`
- Test: `internal/llm/errors_test.go`

**Step 1:** Failing tests: Error() mentions provider, model, retry time;
Unwrap returns cause; errors.As works. **Step 2:** FAIL. **Step 3:**
Implement. **Step 4:** PASS.

### Task 2: Rewire the OpenAI loops (non-streaming, streaming, delta)

**Objective:** All three openai loops classify-then-obey.

**Files:**
- Modify: `internal/llm/client.go` (three loops + constants block)
- Test: `internal/llm/client_test.go` / existing retry tests extended

**Step 1:** Failing tests per loop (httptest servers returning scripted
sequences):
- 429 with `Retry-After: 1` then success → 1 retry, success.
- 429 bare ×(short_retries+1) → ThrottleBackoffError with RetryAt from
  the plan (assert ≥ now+30s, ≤ now+plan horizon), NOT ClientError.
- 402 scripted → QuotaResetError surfaces immediately (attempt count 1).
- 500 then success → retry path intact.
- quota body 429 → QuotaResetError (existing test corpus stays green).

**Step 2:** FAIL. **Step 3:** Rewire one loop at a time; run that
loop's tests between loops; delete the old constants when the LAST
openai loop moves. **Step 4:** PASS; `go test ./internal/llm/ -count=1`.

### Task 3: Rewire the Anthropic loops

**Objective:** Same for anthropic non-streaming + streaming.

**Files:**
- Modify: `internal/llm/anthropic.go` (two loops + constants)
- Test: `internal/llm/anthropic_ratelimit_test.go` extended

Same scripted-sequence tests; SSE-encoding for the streaming server.
Preserve the anthropic 402/quota semantics (the quota section near
anthropic.go:1051 — "Quota payment-required (402): treat as
retry-with-estimate" — and its tests) unchanged.

### Task 4: Agent-loop branch

**Objective:** ThrottleBackoffError stops at the agent loop cleanly.

**Files:**
- Modify: `internal/agent/loop.go` (RateLimitError branch area ~4298)
- Test: `internal/agent/loop_test.go` (or the loop's existing error-path
  test file)

**Step 1:** Failing test: loop receives ThrottleBackoffError → returns
it to the caller unchanged (no rotation, no RecordAliasFailure — with a
comment citing D4/D8 rationale). **Step 2:** FAIL. **Step 3:** Add the
branch BEFORE the RateLimitError branch. **Step 4:** PASS;
`go test ./internal/agent/ -count=1`.

### Task 5: Config plumbing

**Objective:** short_retries knob reaches the loops.

**Files:**
- Modify: `internal/llm/client.go` + `anthropic.go` (loop counts from
  FailurePolicyConfig.ShortRetries, injected via client config; nil-safe
  default 3)
- Test: config default + one loop honoring ShortRetries=1.

## Self-Verification Checklist

- [ ] All five loops classify-first; no loop keeps local backoff constants
- [ ] Quota paths byte-identical (existing quota tests untouched + green)
- [ ] Sustained bare 429 → ThrottleBackoffError (not ClientError) on every loop
- [ ] Agent loop returns ThrottleBackoffError without rotating
- [ ] gofmt/vet clean; `go test ./internal/llm/... ./internal/agent/... -count=1` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; scripted-sequence tests present per loop
- [ ] `grep -n "maxRetries\|streamMaxRetries\|retryBackoffMaxDelay" internal/llm/client.go internal/llm/anthropic.go` shows only policy-config references
- [ ] No new divergence between loops (diff the five failure branches — same shape)
- [ ] Streaming pre-first-token gating preserved

Output: APPROVED or specific gaps with file + line references.

## Notes

- This is the forest's risk hotspot. Land loop-by-loop; never rewire two
  loops in one edit. If a loop's tests are absent, write them BEFORE
  rewiring it.
- The ClientError "All N attempts failed" shape remains for
  server-error exhaustion — only THROTTLE exhaustion changes type.
