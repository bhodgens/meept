# Adaptive 429 Pacing (config-gated) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** AdaptivePacer — per-provider outbound pacing learned from
  rate-limit history, for providers emitting spurious 429s without
  Retry-After. Config-gated, default OFF.
- **Dependencies:** 01-classification.md, 02-retryafter-schedule.md
- **Estimated Context:** 55K
- **Concurrency Group:** C (parallel with leaf 04 is acceptable; different files)
- **Decision references:** D7, D15

## Goal

Some providers shed load with bare 429s (no Retry-After, no quota
signals) — the HTTP spec's intent is "back off", but they do not say for
how long. After leaf 03, each bare 429 costs a short-retry slot and
eventually a ThrottleBackoffError park. Pacing makes meept PROACTIVE:
watch per-provider rate-limit events in the metrics store
(internal/llm/metrics/store.go GetRateLimitSummary, error_type
'rate_limit'); when a provider's recent 429 rate crosses the target,
stretch the minimum interval between outbound requests to that provider
so meept stays under the observed ceiling. The pacer feeds back on every
verdict: 429s raise the interval, clean traffic decays it toward zero.

Config (from leaf 02's schema):
```json5
"failure_policy": {
  "pacing": {
    "enabled": false,              // D15: default OFF
    "target_429_per_hour": 1,      // tolerate at most 1 throttle 429/hour/provider
    "min_interval": "1s",          // smallest enforced gap
    "max_interval": "30s"          // pacing never exceeds this
  }
}
```

Scope guard: pacing NEVER blocks a request outright — `Wait` returns a
sleep duration (bounded by max_interval), or zero. It composes with, and
never replaces, the retry loops.

## Context

Key files:
- `internal/llm/metrics/store.go` — GetRateLimitSummary (~line 725,
  error_type 'rate_limit' query), RateLimitEntry/Summary shapes.
- `internal/llm/metrics/adaptive.go` — the existing adaptive TIMEOUT
  calculator: struct + config + NewCalculator pattern to mirror (it is
  the closest sibling; copy its construction/wiring style, not its math).
- `internal/llm/client.go` — where requests egress per provider (the
  doRequest site); the pacer's Wait call goes there when enabled.
- Provider identity: `cfg.ProviderID` on ModelConfig.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 4:

```go
// internal/llm/pacing.go
type PacingConfig struct {
    Enabled         bool
    Target429PerHour int
    MinInterval     time.Duration
    MaxInterval     time.Duration
}
type AdaptivePacer struct { /* store, cfg, per-provider interval state (mutex-guarded), now func */ }
func NewAdaptivePacer(store *metrics.Store, cfg PacingConfig) *AdaptivePacer
func (p *AdaptivePacer) Wait(ctx context.Context, providerID string) error // nil = clear
func (p *AdaptivePacer) Observe(v PolicyVerdict, providerID string)
```

### What This Leaf Consumes

```go
// From 01: PolicyVerdict / FailureClass (Observe input)
// From 02: FailurePolicyConfig.Pacing (config shape)
// Existing: metrics.Store.GetRateLimitSummary
```

## Tasks

### Task 1: Interval state machine

**Objective:** Pure pacing math, clock-injected, fully unit-tested.

**Files:**
- Create: `internal/llm/pacing.go`
- Test: `internal/llm/pacing_test.go`

**Step 1:** Failing tests (injected `now`): disabled pacer → Wait
returns immediately; first request per provider → no wait; Observe
(FailureThrottle) → interval grows (multiplicative, e.g. ×2 from
MinInterval, clamped to MaxInterval); Observe(clean) after quiet period
→ interval decays (×0.5 per quiet window, floored at 0); 429-rate query
above target holds the interval even without fresh Observe calls.
**Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 2: Metrics integration

**Objective:** The pacer reads REAL rate-limit history.

**Files:**
- Modify: `internal/llm/pacing.go`
- Test: `internal/llm/pacing_test.go` (in-memory metrics store fixture —
  follow metrics/store_test.go fixture pattern)

**Step 1:** Failing test: seed the store with N rate_limit rows for a
provider in the last hour → Wait enforces the stretched interval; a
provider with zero rows → no wait. **Step 2:** FAIL. **Step 3:**
Implement the hourly-rate query (collect-under-lock; the store API is
already context-based). **Step 4:** PASS.

### Task 3: Client wiring (gated)

**Objective:** Wait/Observe calls exist but are inert when disabled.

**Files:**
- Modify: `internal/llm/client.go` (egress site: `pacer.Wait(ctx, cfg.ProviderID)`
  before doRequest; `pacer.Observe(verdict, ...)` after Classify —
  ONLY when the client was constructed with a non-nil pacer)
- Modify: daemon components wiring — construct AdaptivePacer from config
  and hand it to the client (typed-nil guard per repo rules: nil pacer =
  feature off, zero overhead).
- Test: `internal/llm/client_test.go` — with pacer enabled, two rapid
  requests after a throttle verdict show the second waiting (inject a
  clock/pacer stub); disabled → identical to today.

**Verify:** `go test ./internal/llm/ -count=1` green.

### Task 4: Docs

**Objective:** The knob is discoverable.

**Files:**
- Modify: `docs/configuration/` LLM page (the failure_policy section from
  leaf 02) — add the pacing sub-block with defaults and a one-paragraph
  "when to enable" note (providers that 429 without Retry-After).
- Modify: `docs/workflows/llm-management.md` (the LLM workflow page) —
  one paragraph on the pacing loop (observe → interval → decay).

## Self-Verification Checklist

- [ ] Default OFF; disabled path byte-identical to today (tested)
- [ ] Interval growth/decay deterministic under injected clock
- [ ] Wait never blocks longer than max_interval; honors ctx cancel
- [ ] Metrics read is mutex-safe (collect-under-lock)
- [ ] gofmt/vet/analyzers clean; package tests green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Config keys match leaf 02's schema exactly
- [ ] No pacing on quota-class failures (Observe gates on Class)
- [ ] Typed-nil guard on the client's pacer field

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep interval math in the pacer, NOT in the retry loops — leaf 03's
  unified loops must not grow a second copy of this logic.
- If GetRateLimitSummary lacks per-provider hourly resolution it needs,
  extend the store with a narrow query (COUNT by provider WHERE
  error_type='rate_limit' AND ts > now-1h) rather than filtering the
  summary in Go — note the addition for the metrics doc.
