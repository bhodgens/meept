# Failure Classification Engine - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** `internal/llm/failure_policy.go` — FailureClass,
  PolicyVerdict, Classify(); quota-keyword bucketing; generalization of
  classifyQuotaDecision WITHOUT breaking its existing tests.
- **Dependencies:** none
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Decision references:** D4, D5, D7

## Goal

One function owns the question "what KIND of failure is this?" for every
provider response. Buckets (D7):

- **FailureQuota** — 429/402 WITH a quota-indicating signal: structured
  body codes (existing parseQuotaBody results: usage_limit_reached,
  insufficient_quota, quota_exceeded, payment_required, …), a reset
  horizon (body resets_at / reset headers), OR header names / body text
  matching the frozen keyword list (Task 2). 402 ALWAYS quota (D5).
- **FailureThrottle** — 429 (or 503 with Retry-After) WITHOUT quota
  signals. Spurious provider-load 429s land here and never wait
  quota-length horizons.
- **FailureServerError** — 5xx (bounded retry, not park).
- **FailureFatal** — other 4xx.
- **FailureNone** — not a failure (2xx) or transport-class transient the
  caller already handles.

This leaf does NOT compute schedules (leaf 02) and does NOT touch the
five client loops (leaf 03). Classify returns RetryAt=zero; only Class
and Reason are meaningful from this leaf.

## Context

Key files:
- `internal/llm/errors_quota.go` — classifyQuotaDecision (line 131),
  parseQuotaBody (line 235), parseQuotaResetHeader (line 290),
  shortCycleRetryAfter (line 129). REUSE these helpers; Classify is the
  generalization layer above them, not a rewrite. Their existing tests
  (errors_quota_test.go) must stay green untouched.
- `internal/llm/errors.go` — APIError, RateLimitError shapes.
- `internal/llm/client.go:1442-1461` — the current inline 429/402
  classification at the OpenAI error site (leaf 03 moves ITS calls onto
  Classify; this leaf leaves the call sites alone).

Keyword list (FROZEN — cite D7 in the comment; extend only via
DECISIONS.md amendment): header names containing "quota" or "usage"
(case-insensitive substring); body strings "quota", "usage limit",
"usage_limit", "plan limit", "rate plan", "subscription limit". Reasons
returned: "structured_quota_body", "quota_reset_header",
"quota_keyword_header", "quota_keyword_body", "status_402",
"throttle_no_quota_signal", "server_error", "client_error".

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly SHARED-CONVENTIONS §4.1 / master Contract 1:

```go
func Classify(statusCode int, header http.Header, body []byte, now time.Time) PolicyVerdict
// PolicyVerdict{Class, RetryAt: zero, Park: false, GiveUp: false, Reason}
```

### What This Leaf Consumes

```go
// Existing (unchanged):
parseQuotaBody(body []byte) *quotaBody            // errors_quota.go
parseQuotaResetHeader(header http.Header) time.Time
```

## Tasks

### Task 1: Types + Classify skeleton

**Objective:** The contract types exist; pure status-code bucketing works.

**Files:**
- Create: `internal/llm/failure_policy.go`
- Test: `internal/llm/failure_policy_test.go`

**Step 1:** Failing table tests: 200→None; 402→Quota(Reason
"status_402"); 429→Throttle("throttle_no_quota_signal") when no quota
signal; 500/502/503/504→ServerError; 400/401/403→Fatal. Injected `now`
parameter accepted (unused yet).

**Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 2: Quota-signal detection (D7)

**Objective:** Quota-shaped inputs flip 429→Quota; bare 429 stays Throttle.

**Files:**
- Modify: `internal/llm/failure_policy.go`
- Test: `internal/llm/failure_policy_test.go`

**Step 1:** Failing table tests:
- body `{"error":{"type":"usage_limit_reached","resets_at":123}}` →
  Quota("structured_quota_body").
- header `Retry-After` as reset horizon ≥ short-cycle → Quota only when
  a quota keyword ALSO matches (retry-after alone is NOT a quota signal —
  it is the throttle schedule input; this preserves D7's intent). Document
  this rule with a comment citing D7.
- header `x-quota-reset` → Quota("quota_keyword_header").
- body "you have exceeded your usage limit" → Quota("quota_keyword_body").
- same tests through classifyQuotaDecision parity: for every input where
  the OLD classifyQuotaDecision returned true, Classify returns Quota;
  where it returned false with a short retry-after, Classify returns
  Throttle.

**Step 2:** FAIL. **Step 3:** Implement by composing the existing
helpers + the frozen keyword scan. **Step 4:** PASS; run
`go test ./internal/llm/ -run 'TestQuota' -v` — all pre-existing green
(errors_quota_test.go and quota_client_test.go both stay untouched).

### Task 3: Parity guard test

**Objective:** A guard test pins the old classifier to the new one.

**Files:**
- Test: `internal/llm/failure_policy_test.go`

**Step 1:** Test iterating a corpus of (status, header, body) fixtures
covering every case in the existing classifier tests (quota_client_test.go's
TestQuotaClient_ClassifyQuotaDecision table — there is no
classifyQuotaDecision_test.go file; the cases live there): assert
`classifyQuotaDecision(...) == (Classify(...).Class == FailureQuota)` for
every row. **Step 2:** FAIL where buckets diverge (fix Classify, never
the old function). **Step 3/4:** PASS.

## Self-Verification Checklist

- [ ] Contract signature matches master Contract 1 exactly
- [ ] All pre-existing errors_quota_test.go cases green, unmodified
- [ ] Bare 429 never classifies Quota (D7 core assertion)
- [ ] 402 always Quota (D5)
- [ ] Keyword list frozen in code with D7 comment
- [ ] gofmt/vet clean; `go test ./internal/llm/ -count=1` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] No call-site changes (client.go/anthropic.go untouched)
- [ ] Classify never sleeps, never errors
- [ ] Reasons exactly match the frozen Reason list

Output: APPROVED or specific gaps with file + line references.

## Notes

- `now` is in the signature for schedule symmetry (leaf 02) and future
  clock injection; classification itself is time-independent today
  except retry-after horizon math delegated to leaf 02 — keep it so.
- Anthropic client's structured rate-limit headers
  (anthropic-ratelimit-*-reset) already route through
  parseQuotaResetHeader — verify in the parity corpus that Anthropic
  short-cycle limits still land Throttle, not Quota.
