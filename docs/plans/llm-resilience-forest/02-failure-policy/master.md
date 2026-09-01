# Unified Failure Policy (429/402/timeout) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root of tree 02 in the llm-resilience-forest)
- **Children:** 5 leaf documents under this node
- **Scope:** ONE failure-policy engine owning all 429/402 classification
  and long-horizon retry/park decisions; endpoint-level timeout cooldowns
  with alias-level `timeout:` config; adaptive pacing for spurious-429
  providers.

Read `../SHARED-CONVENTIONS.md` (§4.1, §4.3 are THIS tree's contracts)
and `../DECISIONS.md` (D4-D8, D10, D15) first.

## Goal

Today the same error handling is copy-pasted across FIVE client retry
loops (openai non-streaming client.go:426, openai streaming client.go:586,
streaming-delta, anthropic non-streaming anthropic.go:281, anthropic
streaming anthropic.go:448) with hardcoded `maxRetries = 3` and a 30s
backoff cap. A provider that throttles for an hour kills every turn in
under 2 minutes. Meanwhile quota-shaped errors already have a proper
hours-scale path (QuotaResetError) — but "service limit" 429s and spurious
provider-load 429s do not, and users cannot tell them apart.

This tree delivers (D4: one handler, one behavior):

1. **Classification** — `llm.Classify(statusCode, header, body, now)`
   returns a PolicyVerdict bucketing every non-2xx into
   FailureThrottle / FailureQuota / FailureServerError / FailureFatal /
   FailureNone. 402 rides the quota class with a minutes-longer initial
   timer (D5). 429s WITHOUT quota-indicating signals are throttle, so
   spurious 429s never inherit quota-length waits (D7).
2. **Full Retry-After** — RFC7231 HTTP-date (IMF-fixdate, RFC850,
   asctime) + delta-seconds, per D6.
3. **Long-horizon schedule** — exponential growth → 1h polling floor →
   24h cap (configurable), for BOTH throttle and (when reset unknown)
   quota classes (D8).
4. **Endpoint-level timeouts** — timeouts/cooldowns key on the base
   endpoint so openai/model-1 (medium) and openai/model-2 (thinkhard)
   share fate; alias-level cooldown applies only when the alias config
   declares `timeout:` explicitly, firing on CONSISTENT member-model
   failure (D10). Documented.
5. **Adaptive pacing** — for providers that emit spurious 429s without
   Retry-After, learn the effective ceiling from the rate-limit metrics
   store and pace outbound requests below it; config-gated, default off
   (D15).
6. **Client unification** — all five retry loops delegate to the policy
   engine; no sixth divergent copy.

Out of scope: WHO parks the turn (tree 03 consumes `PolicyVerdict.Park`).

## Architecture

New file `internal/llm/failure_policy.go` owns classification + schedule
math (pure functions, injected clock, fully unit-testable). The existing
`classifyQuotaDecision` (errors_quota.go:131) GENERALIZES into it — its
existing observable classifications stay green. The five client loops
shrink to: call Classify on each failure → obey the verdict
(retry-now / sleep-until / return-for-parking). Retry-After parsing moves
to one function used by both classification and the schedule. State
(endpoint cooldowns, pacing ledgers) lives on the Resolver next to
AliasHealth under the single Resolver.mu regime (SHARED-CONVENTIONS §2).

## Interface Contracts

### Contract 1: PolicyVerdict + Classify (frozen; SHARED-CONVENTIONS §4.1)

```go
// File: internal/llm/failure_policy.go
type FailureClass int
const (
    FailureNone FailureClass = iota
    FailureThrottle
    FailureQuota
    FailureServerError
    FailureFatal
)
type PolicyVerdict struct {
    Class   FailureClass
    RetryAt time.Time
    Park    bool
    GiveUp  bool
    Reason  string
}
func Classify(statusCode int, header http.Header, body []byte, now time.Time) PolicyVerdict
```

- Owner: 01-classification.md
- Consumers: 02-retryafter-schedule.md, 03-client-unification.md,
  04-endpoint-timeouts.md, tree 03 (parking)

### Contract 2: Backoff schedule

```go
// File: internal/llm/failure_policy.go
// BackoffPlan converts a class + attempt history into attempt times.
type BackoffPlan struct {
    Base       time.Duration // first retry delay (throttle default 30s; 402 = +5m, D5)
    Max        time.Duration // polling floor once reached (default 1h)
    GiveUpAt   time.Time     // now + Horizon (default 24h, config "llm.failure_policy.horizon")
}
func (p BackoffPlan) NextAttempt(now time.Time, attempt int, prior time.Time) time.Time
func (p BackoffPlan) ShouldGiveUp(now time.Time) bool
```

- Owner: 02-retryafter-schedule.md
- Consumers: 03-client-unification.md, tree 03

### Contract 3: Endpoint cooldown state

```go
// File: internal/llm/resolver.go (add ON THE RESOLVER STRUCT, next to the
// health map; Resolver.mu regime — amended per ARCH audit B3: AliasHealth
// is PER-ALIAS (getOrCreateHealth), but D10 shared fate is CROSS-ALIAS
// (openai/model-1 medium-alias and openai/model-2 thinkhard-alias are
// different aliases), so per-alias state cannot deliver it.
// endpointBlocks: EndpointKey -> until. Key derivation (audit R2):
//   EndpointKey(cfg) = endpoint URL host + credential fingerprint
//   (QuotaCredentialKey provider portion, models.go:356-358 precedent) —
//   host-only is wrong here: gala-mlx/gala-llama share host "gala" and
//   xai/xai-oauth share api.x.ai with unrelated credentials.
// Set on FailureThrottle/timeouts; consulted by ResolveForAlias rotation:
// an endpoint-blocked model is skipped like a quota-blocked one, and if
// every candidate is blocked → ErrAllModelsQuotaBlocked-style distinct
// error ErrAllEndpointsBlocked.
func EndpointKey(cfg *ModelConfig) string
// Alias-level: honored ONLY when the alias entry declares timeout:
// (AliasEntry.Timeout > 0). Then N consecutive member failures (same
// MaxFails counter) block the ALIAS for that duration. Consistent-member
// rule: the failing model must be the same member on consecutive fails.
```

- Owner: 04-endpoint-timeouts.md
- Consumers: 03-client-unification.md (indirect via rotation), docs leaf

### Contract 4: Pacing gate

```go
// File: internal/llm/pacing.go
// AdaptivePacer paces outbound requests per provider using rate-limit
// history (internal/llm/metrics GetRateLimitSummary) when the provider
// emits 429s without Retry-After. Config: llm.failure_policy.pacing
// { enabled: false, target_429_per_hour: 1, min_interval: 1s }.
type AdaptivePacer struct{ ... }
func (p *AdaptivePacer) Wait(ctx context.Context, providerID string) error // nil = clear to send
func (p *AdaptivePacer) Observe(v PolicyVerdict, providerID string)        // feedback loop
```

- Owner: 05-adaptive-pacing.md
- Consumers: 03-client-unification.md

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-classification.md | leaf | none | 65K | A |
| 02 | 02-retryafter-schedule.md | leaf | 01 | 55K | B |
| 03 | 03-client-unification.md | leaf | 01, 02 | 85K | C |
| 04 | 04-endpoint-timeouts.md | leaf | 01 | 65K | C |
| 05 | 05-adaptive-pacing.md | leaf | 01, 02 | 55K | C |

**Concurrency groups:** A first; B after 01; C group (03, 04, 05) runs
after B. Within C, 03 and 04 touch overlapping resolver/client files —
dispatch 03 → 04 serially; 05 may run parallel to 04.

## Dispatch Protocol

Standard protocol (see ../SHARED-CONVENTIONS.md §6 commit policy; master
template Phases 1-3 apply to each leaf):

- Leaf 01: verify `go test ./internal/llm/ -run 'TestClassify|TestQuota' -v`
  — BOTH new and pre-existing quota classification tests green. Commit:
  `feat(llm): unified failure-policy classification (tree 02 leaf 01)`.
- Leaf 02: verify schedule tests + RFC7231 parser table tests. Commit:
  `feat(llm): RFC7231 Retry-After + long-horizon backoff plan (tree 02 leaf 02)`.
- Leaf 03: verify all five loops delegate; run the full
  `go test ./internal/llm/... ./internal/agent/... -count=1`. Commit:
  `refactor(llm): unify client retry loops on failure policy (tree 02 leaf 03)`.
- Leaf 04: verify rotation skips endpoint-blocked models; docs updated
  (alias timeout semantics). Commit:
  `feat(llm): endpoint-level timeout cooldowns + alias timeout semantics (tree 02 leaf 04)`.
- Leaf 05: verify pacer math with injected clock; config default OFF.
  Commit: `feat(llm): adaptive 429 pacing behind config gate (tree 02 leaf 05)`.

In-session review each leaf against its Review Checklist BEFORE commit;
max 3 re-dispatch cycles per leaf.

## Review Checklist

- [ ] Leaf tasks complete; tests pass; contracts satisfied exactly
- [ ] Quota-reset-resilience invariants intact (SHARED-CONVENTIONS §2):
      QuotaResetError never short-retries; ErrAllModelsQuotaBlocked distinct
- [ ] classifyQuotaDecision's existing test expectations preserved
- [ ] No sixth divergent retry loop; the per-client retry constants
      (`maxRetries` client.go:25, `streamMaxRetries` client.go:28,
      `anthropicMaxRetries` anthropic.go:25 — ALL =3) are GONE from the
      five loops (grep all THREE names, not just maxRetries)
- [ ] Config fields: schema tags + defaults + docs (repo config rules)
- [ ] gofmt/vet clean; analyzers clean on touched packages
- [ ] No artifacts, no line-number corruption

Output: APPROVED or specific gaps.

## Coding Conventions

Pass `../SHARED-CONVENTIONS.md` §1, §2, §3 in every dispatch context.
Time math uses injected `now` — no `time.Sleep` over 100ms in tests.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-classification.md | PENDING | 0 | |
| 02-retryafter-schedule.md | PENDING | 0 | |
| 03-client-unification.md | PENDING | 0 | |
| 04-endpoint-timeouts.md | PENDING | 0 | |
| 05-adaptive-pacing.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./... && go test ./internal/llm/... ./internal/agent/... -count=1`.
2. Cross-checks:
   - quota-shaped 429 with reset header → FailureQuota, Park-capable.
   - bare 429 with `Retry-After: Fri, 31 Dec 2027 23:59:59 GMT` (IMF-fixdate) → FailureThrottle honoring the date.
   - bare 429, no headers, keyword-free body → FailureThrottle, no quota wait.
   - 402 → FailureQuota with +5m initial timer.
   - 503 → FailureServerError, bounded.
   - model-1 timeout then resolve → model-2 (same endpoint) skipped.
3. `make analyzers`; `make graphs` (no new topics expected); AGENTS.md
   review in final commit (config fields, conventions).

## Structural Completeness Check (Before Dispatch)

`python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep 02-failure-policy`

## Notes

- This tree is the highest-risk integration point in the forest (five
  loops, live provider traffic). Sequence 01 → 02 → 03 strictly; do NOT
  parallelize 03 with 04.
- The policy engine NEVER sleeps inside Classify; it returns verdicts.
  Sleeping/parking belongs to callers (loops short-sleep only per
  verdict; long waits → tree 03's park path).
- Keep `DefaultQuotaMaxWait` (24h) as the horizon default; the new
  config knob REPLACES the ad-hoc constant only after leaf 03 wires it.
