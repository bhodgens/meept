# 02 Admission Control (Context Fit) - Implementation Leaf

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
- **Scope:** Add a pre-dispatch admission gate (Contract C3) that rejects a request whose estimated input plus a 64-token output reserve exceeds the model's declared context. Register it as the third filter in the selection chain.
- **Dependencies:** 00T gate tests committed (RED); 01-quality-dimension.md COMPLETE (it creates `selection.go` and the `Filter` interface)
- **Estimated Context:** 55K
- **Concurrency Group:** B (wave 1 - file-disjoint from 01/03/04)
- **Source reference:** Sovereign Router `src/gateway.py:706` (`context_too_small`), `MIN_OUTPUT_TOKENS = 64`

## Goal

meept detects context overflow **reactively**: `DetectContextOverflowFromBody` (`internal/llm/errors_overflow.go:10`) classifies a provider's 400/overflow response *after* the request was sent and rejected. That wastes a round trip and, on a paid provider, may bill the rejected request. Sovereign Router rejects **before dispatch** with a distinct 413 (`src/gateway.py:706`).

This leaf adds the cheap, deterministic version: a pre-dispatch check against the *declared* `ContextLimit` on `ModelConfig`. No token counting library, no provider call — an estimate plus a fixed reserve.

The gate rule, verbatim from `gateway.py:706`:

```
est_tokens + MIN_OUTPUT_TOKENS > entry.context_tokens   ->  skip("context_too_small")
MIN_OUTPUT_TOKENS = 64
```

## Context

- `internal/llm/errors_overflow.go:10` - `DetectContextOverflowFromBody`. **Keep it.** This leaf adds a *pre*-dispatch gate; the post-hoc classifier stays as the fallback for models that overflow for reasons other than raw size (e.g. a provider-side system-prompt accounting difference).
- `internal/llm/models.go` - `ModelConfig.ContextLimit int`, `ModelConfig.MaxTokens int`. Both already exist.
- `internal/llm/resolver.go` - `ResolveForAlias`, `ResolveForSkill`. Both call `runSelectionFilters` (added by leaf 01).
- `internal/llm/provider_manager.go:525-600` - the head of `chatWithFailoverRaw`: ordered-provider snapshot and health snapshot. The shadow path must NOT go through here (it would risk a dispatch); shadow resolves in `internal/llm/shadow.go` and returns.
- `internal/llm/errors_tier.go:15-45` - the typed-error + sentinel pattern.
- `internal/config/schema.go:1291-1310` - `ContextDiscoveryConfig` (opt-in, default off). **Do not depend on it** (Q5): discovery is async and off by default; a gate that depends on a background syncer flaps.

## Verified Anchors

- `internal/llm/errors_overflow.go:10` - `DetectContextOverflowFromBody`
- `internal/llm/models.go:233-241` - `CostPerMillionInput` (:233), `CostPerMillionOutput` (:234), `MaxTokens` (:235), `ContextLimit` (:241). Both already exist.
- `internal/llm/selection.go` - created by leaf 01; the `Filter` interface and `Verdict` constants
- `internal/llm/resolver.go:113` - `NewResolver(cfg *ProvidersConfig, logger *slog.Logger) *Resolver`, where `registerSelectionFilters()` is called
- `internal/llm/provider_manager.go:525` - the ordered-provider snapshot at the head of `chatWithFailoverRaw` (the rotation loop; do not gate here)
- `internal/llm/errors_tier.go:15-45` - sentinel + `NonRetryable()` pattern
- `e2e/suites/sovereign-parity/admission_test.go` - gate tests T5, T6

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/admission.go
var ErrContextTooSmall = errors.New("llm: request does not fit the model context")

type ContextTooSmallError struct {
    ModelID      string
    ProviderID   string
    EstInput     int
    Reserve      int
    ContextLimit int
}
func (e *ContextTooSmallError) Error() string
func (e *ContextTooSmallError) NonRetryable() bool { return true }
func (e *ContextTooSmallError) Unwrap() error { return ErrContextTooSmall }

// MinOutputReserve is the frozen output reserve, quoted from
// SovereignRouter src/gateway.py MIN_OUTPUT_TOKENS = 64.
const MinOutputReserve = 64

// EstimateInputTokens returns a conservative token estimate for a request.
// v1 is a character heuristic (runes/4, rounded up) - deterministic, no
// dependency, and deliberately an OVER-estimate so the gate fails safe.
func EstimateInputTokens(messages []Message, tools []ToolDef) int

type admissionFilter struct{}
func (admissionFilter) Name() string { return "admission" }
func (admissionFilter) Filter(c *Candidate, fc *FilterCtx) Verdict
func (admissionFilter) Reason() string   // "context_too_small"
```

### What This Leaf Consumes

- `selection.go` from leaf 01 - `Candidate`, `FilterCtx`, `Verdict`, `Filter`.
- `ModelConfig.ContextLimit` and `.MaxTokens`.
- The message/tool types the client already builds. **Survey first**: find the request struct the OpenAI-compatible client marshals (`internal/llm/client.go`) and estimate from that exact type, so the estimate cannot drift from what is actually sent.

### Frozen contracts

```
reserve(c) = min(c.Model.MaxTokens, MinOutputReserve)   // MaxTokens<=0 -> MinOutputReserve
gate(c, fc) = fc.EstInput + reserve(c.Model) > c.Model.ContextLimit

ContextLimit <= 0  ->  ALLOW (unknown limit; never fabricate a bound)
EstInput <= 0      ->  ALLOW (nothing to estimate; do not guess)
```

`Reason()` returns exactly `"context_too_small"` - the routing-log skip reason is a cross-surface contract (it appears in `routes.jsonl` on the Sovereign side and in the e2e assertions here).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `ErrContextTooSmall`, `ContextTooSmallError` (+ `NonRetryable`, `Unwrap`), and `MinOutputReserve = 64` | Create: `internal/llm/admission.go`; Create: `internal/llm/errors_admission.go` (or fold into admission.go) | `go test -p 2 ./internal/llm/ -run TestContextTooSmall_Error` | RED (undefined) | GREEN | Given the error wrapped once; When inspected; Then `errors.Is(err, ErrContextTooSmall)` and `errors.As` yields `*ContextTooSmallError` |
| T2 | Implement `EstimateInputTokens(messages, tools)`: rune count / 4, rounded up, plus a per-tool schema allowance | Modify: `internal/llm/admission.go` | `go test -p 2 ./internal/llm/ -run TestEstimateInputTokens` | RED (undefined) | GREEN | Given a known message set; When estimated; Then the result is deterministic and never below the true rune/4 count |
| T3 | Implement `admissionFilter` satisfying the `Filter` interface; `Reason()` returns `"context_too_small"` | Modify: `internal/llm/admission.go` | `go test -p 2 ./internal/llm/ -run 'TestAdmissionFilter_'` | RED | GREEN | Given an over-context candidate; When filtered; Then `(VerdictSkip, "context_too_small")` |
| T4 | Boundary cases: exactly-at-limit allows; one over rejects; `ContextLimit == 0` allows; `MaxTokens == 0` uses the 64 reserve | same | `-run 'TestAdmissionFilter_Boundary'` | RED | GREEN | Given each boundary; When filtered; Then the frozen table holds |
| T5 | Register the filter in the chain (third position, after posture and quality) | Modify: `internal/llm/resolver.go:113` (`NewResolver` -> `registerSelectionFilters`) | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestAdmission_'` | RED | GREEN | Given the registered chain; When an over-context request resolves; Then `ErrContextTooSmall` surfaces |
| T6 | No-dispatch proof: an over-context turn makes ZERO upstream requests (fake-upstream counter) | - | `-run TestAdmission_NoUpstreamCall'` | RED | GREEN | Given a fake upstream counter; When the over-context turn runs; Then the counter is 0 |
| T7 | Unknown-limit pass-through: a model with no `context_limit` serves the request | - | `-run TestAdmission_UnknownContextAllows'` | RED | GREEN | Given `context_limit` absent; When the turn runs; Then it reaches upstream |
| T8 | Post-hoc path preserved: `DetectContextOverflowFromBody` still classifies a provider-side overflow | Modify: none (verify only) | `go test -p 2 ./internal/llm/ -run TestDetectContextOverflow` | n/a | GREEN, unmodified | Given a provider 400 overflow body; When classified; Then it still yields the overflow class |

## Self-Verification Checklist

- [ ] T1-T8 complete; gate tests T5, T6, T7 GREEN.
- [ ] `errors_overflow.go` is UNCHANGED.
- [ ] The gate fires inside the resolver, never in `chatWithFailoverRaw` (shadow routing inherits it).
- [ ] `MinOutputReserve == 64` exactly, with a comment quoting `gateway.py`.
- [ ] `ContextLimit <= 0` allows; no fabricated bound.
- [ ] The estimate is a rune/4 over-estimate; comment says why (fail safe).
- [ ] `Reason()` returns exactly `"context_too_small"`.
- [ ] No provider call, no token-counting dependency, no `internal/config` import.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen reserve and gate rule match master.md C3 and `gateway.py:706`.
- [ ] The filter is registered third, after posture and quality, and the order is commented.
- [ ] `ContextTooSmallError` is `NonRetryable` and reachable via `errors.Is` through a wrap.
- [ ] The estimate cannot under-count (rune/4 rounds up; tools add an allowance).
- [ ] No existing test was edited; only new ones added.
- [ ] The post-hoc overflow classifier is still wired and tested.

## Do NOT commit

The orchestrator commits this leaf after review.