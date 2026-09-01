# Retry-After Parsing + Long-Horizon Backoff - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Full RFC7231/RFC3339 Retry-After parsing; BackoffPlan with
  exponential → 1h floor → 24h cap; config knobs.
- **Dependencies:** 01-classification.md
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Decision references:** D5, D6, D8

## Goal

1. **Parse Retry-After COMPLETELY (D6).** The spec (RFC 7231 §7.1.3)
   allows two forms; senders use both and buggy senders mix them:
   - delta-seconds: `Retry-After: 120`
   - HTTP-date: IMF-fixdate `Fri, 31 Dec 2027 23:59:59 GMT` (REQUIRED),
     obsolete RFC850 `Friday, 31-Dec-27 23:59:59 GMT`, obsolete
     asctime `Fri Dec 31 23:59:59 2027`.
   Today's parseRetryAfterSeconds handles delta-seconds and
   parseQuotaResetHeader handles two date attempts (RFC1123 + RFC3339 —
   NOT "RFC3339 attempts"; HTTP-date's required IMF-fixdate form IS
   covered, RFC850 and asctime are the gaps) plus provider
   headers — RFC850/asctime are MISSING. Consolidate into one
   `ParseRetryAfter(header http.Header) (time.Time, time.Duration, bool)`
   returning (absolute-date, delta, present) trying all four forms in
   spec order, with provider-specific headers consulted afterward
   (preserve the existing anthropic-*/codex-* lookups verbatim).
2. **BackoffPlan (D8).** Exponential from Base; once a step exceeds Max
   (1h default), poll at Max hourly; never past GiveUpAt (now+Horizon;
   24h default). 402's Base is the 429 Base + 5 minutes (D5). Honor an
   earlier server-provided RetryAt when it is later than the computed
   step (never retry BEFORE the server says, never wait longer than the
   horizon).

## Context

Key files:
- `internal/llm/errors_quota.go:290-331` — parseQuotaResetHeader (the
  provider-header section to preserve: anthropic-ratelimit-*-reset,
  X-Codex-*).
- `internal/llm/client.go` — parseRetryAfterSeconds (grep it), the
  retryBackoff* constants (lines 25-27), BackoffWithJitter.
- `internal/config/schema.go` — add `failure_policy` config block (Task 3).
- `internal/llm/errors_quota.go:129` — shortCycleRetryAfter stays the
  quota/throttle boundary.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 2 plus the parser:

```go
func ParseRetryAfter(header http.Header) (date time.Time, delta time.Duration, present bool)
type BackoffPlan struct {
    Base     time.Duration
    Max      time.Duration
    GiveUpAt time.Time
}
func (p BackoffPlan) NextAttempt(now time.Time, attempt int, prior time.Time) time.Time
func (p BackoffPlan) ShouldGiveUp(now time.Time) bool
func DefaultBackoffPlan(class FailureClass, now time.Time, cfg FailurePolicyConfig) BackoffPlan
```

`NextAttempt(attempt=0)` returns the first retry; `prior` is a
server-provided retry time (zero = none) — when prior is after the
computed step, prior wins but is capped at GiveUpAt.

### What This Leaf Consumes

```go
// From 01-classification.md
FailureClass, PolicyVerdict
```

## Tasks

### Task 1: ParseRetryAfter (D6)

**Objective:** All four time forms + provider headers, one function.

**Files:**
- Create: `internal/llm/retry_after.go` (or extend errors_quota.go —
  prefer the new file; the old helpers stay for one leaf of transition)
- Test: `internal/llm/retry_after_test.go`

**Step 1:** Failing table tests — delta-seconds; IMF-fixdate; RFC850;
asctime; invalid garbage → not present; date in the past → present with
zero/negative (caller clamps); anthropic-ratelimit-tokens-reset;
X-Codex-Primary-Reset-At; precedence (Retry-After beats provider
headers). Build dates with explicit time.FixedZone("GMT", 0) — never
time.Now-relative fixtures.

**Step 2:** FAIL. **Step 3:** Implement (time.Parse with
time.RFC1123 covers IMF-fixdate; add RFC850 `time.RFC850` and asctime
layout `Mon Jan _2 15:04:05 2006`). **Step 4:** PASS.

### Task 2: BackoffPlan math (D8)

**Objective:** Deterministic schedule, no real time.

**Files:**
- Modify: `internal/llm/failure_policy.go`
- Test: `internal/llm/failure_policy_test.go`

**Step 1:** Failing table tests (fixed now): attempt 0..N produce 30s,
60s, 120s, 240s, … until exceeding 1h → all subsequent exactly 1h;
ShouldGiveUp false before GiveUpAt, true at/after; 402 Base = throttle
Base+5m; prior-date respected but capped at GiveUpAt; jitter —
BackoffWithJitter is NOT used in the schedule (hourly polling must be
exact; short steps MAY add the existing jitter helper if a test asserts
bounds — keep it simple: no jitter in BackoffPlan, jitter stays at the
call sites' short sleeps).

**Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 3: Config knobs

**Objective:** `llm.failure_policy` config block; defaults per D8.

**Files:**
- Modify: `internal/config/schema.go` (FailurePolicyConfig struct +
  wiring into the llm config block; json+toml tags)
- Modify: config defaults site (locate NewDefaultConfig or the llm
  defaults; follow the pattern used by existing llm fields)
- Modify: `docs/configuration/` LLM page — document:
  `horizon` (default "24h"), `base_throttle` ("30s"),
  `base_quota_402_extra` ("5m"), `poll_floor` ("1h"),
  `short_retries` (3 — tree 02 leaf 03's loops consume this; add the
  schema field in THIS leaf so leaf 03 only reads it),
  `pacing.enabled` (false — leaf 05 consumes), `pacing.min_interval`,
  `pacing.max_interval` ("30s" — leaf 05's ceiling; add the field here
  so leaf 05's PacingConfig maps onto the schema 1:1).
- Test: `internal/config/schema_test.go` (defaults + tag round-trip).

**Verify:** defaults test PASS; docs name every knob and its default;
`go build ./...` clean.

## Self-Verification Checklist

- [ ] All four RFC7231/RFC3339 forms parse (table-tested, GMT-fixed)
- [ ] Provider header lookups preserved verbatim
- [ ] Schedule math deterministic under injected now; 402 +5m (D5); 1h floor; 24h cap (D8)
- [ ] Config block + defaults + docs complete
- [ ] gofmt/vet clean; package tests green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Parser consolidation does not break errors_quota_test.go cases
- [ ] No time.Sleep anywhere in this leaf's implementation
- [ ] Config tags follow repo json5/toml conventions

Output: APPROVED or specific gaps with file + line references.

## Notes

- Leap seconds in IMF-fixdate (":60") are out of scope; a parse failure
  falls through to the next candidate form by design.
- GiveUp semantics: ShouldGiveUp answers the SCHEDULE question only;
  the decision to surface a user error belongs to callers (leaf 03
  short loops, tree 03 parks).
