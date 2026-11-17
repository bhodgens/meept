package llm

// Pre-dispatch admission control (Contract C3, sovereign-router-parity
// leaf 02; source reference: Sovereign Router src/gateway.py:706
// `context_too_small`, MIN_OUTPUT_TOKENS = 64 at src/gateway.py:246).
//
// meept detects context overflow REACTIVELY: DetectContextOverflowFromBody
// (errors_overflow.go — UNCHANGED by this leaf) classifies a provider's
// 400/overflow body AFTER the request was sent and rejected. That wastes a
// round trip and, on a paid provider, may bill the rejected request. This
// file adds the cheap, deterministic PRE-dispatch check against the
// DECLARED ContextLimit on ModelConfig: an estimate plus a fixed output
// reserve. No token-counting library, no provider call.
//
// Gate rule, verbatim from gateway.py:706:
//
//	est_tokens + MIN_OUTPUT_TOKENS > entry.context_tokens -> skip("context_too_small")
//
// Opt-in by construction: a model with NO context_limit (ContextLimit <= 0)
// has an UNKNOWN limit — the gate allows (never fabricates a bound), so a
// config with no context_limit anywhere is byte-identical to pre-leaf.
// The post-hoc classifier stays as the fallback for models that overflow
// for reasons other than raw size (e.g. provider-side system-prompt
// accounting differences).

import (
	"errors"
	"fmt"
)

// ErrContextTooSmall is the admission sentinel (Contract C3). It is a
// CLASSIFIED terminal error in the ErrTierFloor / ErrQualityFloor family —
// check with errors.Is, never string matching. It is never retried, never
// parked, and never surfaces as a completed turn.
//
// ErrProviderLimit was DROPPED from the contract (drift-audit ruling #5,
// 2026-11-16): rate-window handling stays with the existing cooldown
// machinery; no leaf owns this sentinel and none may declare it.
var ErrContextTooSmall = errors.New("llm: request does not fit the model context")

// ContextTooSmallError is the typed admission verdict: the pre-dispatch
// gate measured the request against the candidate's DECLARED context limit
// and the request cannot fit. Like TierFloorError (errors_tier.go) it is
// NonRetryable — a size verdict is not a transient provider condition, so
// every client short-retry loop must exit immediately (the NonRetryable
// lane), and the verdict is never retried, never parked.
type ContextTooSmallError struct {
	// ModelID/ProviderID identify the candidate the gate refused (it was
	// never dispatched; no request reached it).
	ModelID    string
	ProviderID string
	// EstInput is the rune/4 input estimate that failed the gate.
	EstInput int
	// Reserve is the output reserve applied (min(MaxTokens,
	// MinOutputReserve)).
	Reserve int
	// ContextLimit is the model's declared context limit.
	ContextLimit int
}

func (e *ContextTooSmallError) Error() string {
	return fmt.Sprintf("%s: provider=%s model=%s est_input=%d reserve=%d context_limit=%d",
		ErrContextTooSmall.Error(), e.ProviderID, e.ModelID, e.EstInput, e.Reserve, e.ContextLimit)
}

// Unwrap to the sentinel: errors.Is(err, ErrContextTooSmall) must hold
// through any wrap, and errors.As recovers the typed verdict.
func (e *ContextTooSmallError) Unwrap() error {
	return ErrContextTooSmall
}

// NonRetryable returns true so the client short-retry loops exit
// immediately: the gate fired BEFORE dispatch — there is nothing to retry
// against this candidate, and the same payload cannot fit on a retry.
func (e *ContextTooSmallError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*ContextTooSmallError)(nil)

// IsContextTooSmall reports whether err is (or wraps) a *ContextTooSmallError.
func IsContextTooSmall(err error) bool {
	if err == nil {
		return false
	}
	var te *ContextTooSmallError
	return errors.As(err, &te)
}

// AsContextTooSmall returns the *ContextTooSmallError in err's chain,
// mirroring AsTierFloorError (errors_tier.go).
func AsContextTooSmall(err error) (*ContextTooSmallError, bool) {
	if err == nil {
		return nil, false
	}
	return errors.AsType[*ContextTooSmallError](err)
}

// MinOutputReserve is the frozen output reserve, quoted from Sovereign
// Router src/gateway.py MIN_OUTPUT_TOKENS = 64 (src/gateway.py:246). The
// gate always holds this much of the context back for the reply; a model
// with a smaller configured max_output reserves THAT instead (the smaller
// of the two — reserve(c) = min(c.Model.MaxTokens, 64), master Contract
// C3).
const MinOutputReserve = 64

// EstimateInputTokens returns a conservative token estimate for a request:
// a rune/4 character heuristic over every message's content (plus names,
// tool-call envelopes, and assistant prefills) plus a fixed per-tool
// schema allowance, rounded UP.
//
// v1 is deliberately an OVER-estimate so the gate fails safe: a round trip
// wasted on a false REJECT costs one local check the caller can retry with
// a smaller context, but a false PASS wastes a provider round trip that
// may bill. The heuristic is deterministic (no dependency, no provider
// call) and must never under-count the true rune/4 total.
//
// The tool allowance covers each tool definition's name + description +
// serialized parameter schema on the wire; parameters vary widely, so the
// allowance is a flat conservative add per tool rather than a second
// character pass over the schema JSON.
func EstimateInputTokens(messages []ChatMessage, tools []ToolDefinition) int {
	total := 0
	for i := range messages {
		m := &messages[i]
		total += runeTokenEstimate(m.Content)
		if len(m.Parts) > 0 {
			// Multimodal parts synthesize to flat text for estimation
			// (image parts estimate from their description/URL — the
			// wire form the model sees is at least that large).
			total += runeTokenEstimate(ContentFromParts(m.Parts, true))
		}
		if m.Name != "" {
			total += runeTokenEstimate(m.Name)
		}
		if m.ToolCallID != "" {
			total += runeTokenEstimate(m.ToolCallID)
		}
		for _, tc := range m.ToolCalls {
			total += runeTokenEstimate(tc.ID)
			total += runeTokenEstimate(tc.Type)
			total += runeTokenEstimate(tc.Function.Name)
			total += runeTokenEstimate(tc.Function.Arguments)
		}
	}
	for _, t := range tools {
		total += runeTokenEstimate(t.Function.Name)
		total += runeTokenEstimate(t.Function.Description)
		// Flat per-tool schema allowance (conservative; deterministic).
		total += toolSchemaAllowance
	}
	return total
}

// toolSchemaAllowance is the flat per-tool token add in EstimateInputTokens.
// 128 tokens covers a typical function name + description + a compact
// parameter schema; an under-estimate here risks a false pass, so the
// value errs high for small schemas and low only for very large ones —
// the rune/4 pass over content dominates for real workloads.
const toolSchemaAllowance = 128

// runeTokenEstimate estimates tokens as runes/4, rounded UP (the frozen
// C3 heuristic; gateway.py's estimate_tokens shape). Round-up is the
// fail-safe direction: the gate must never under-count.
func runeTokenEstimate(s string) int {
	if s == "" {
		return 0
	}
	n := len([]rune(s))
	return (n + 3) / 4
}

// admissionFilter is the R2 context-fit filter (position 3 in the C1
// chain, after posture and quality — the registration order is frozen in
// selection.go). Stateless (empty struct): the gate is a pure function of
// the candidate's declared limits and the request estimate.
type admissionFilter struct{}

func (admissionFilter) Name() string { return "admission" }

// Filter applies the frozen gate rule (master Contract C3, gateway.py:706):
//
//	reserve(c) = min(c.Model.MaxTokens, MinOutputReserve)   // MaxTokens<=0 -> MinOutputReserve
//	gate(c, fc) = fc.EstInput + reserve(c.Model) > c.Model.ContextLimit
//
//	ContextLimit <= 0  ->  ALLOW (unknown limit; never fabricate a bound)
//	EstInput <= 0      ->  ALLOW (nothing to estimate; do not guess)
//
// fc.EstInput is the authoritative estimate channel (FilterCtx.EstInput —
// the resolver's callers thread the per-request estimate there); when a
// caller leaves it zero the filter has nothing to judge and allows, exactly
// like an unknown limit: the gate never invents an input size.
func (admissionFilter) Filter(c *Candidate, fc *FilterCtx) Verdict {
	if c == nil || c.Model == nil || fc == nil {
		return VerdictAllow
	}
	// Unknown limit: ContextLimit <= 0 means the config declares no
	// context_limit — allow (never fabricate a bound; opt-in default).
	if c.Model.ContextLimit <= 0 {
		return VerdictAllow
	}
	// Nothing to estimate: do not guess an input size from absence.
	if fc.EstInput <= 0 {
		return VerdictAllow
	}
	reserve := outputReserve(c.Model.MaxTokens)
	if fc.EstInput+reserve > c.Model.ContextLimit {
		return VerdictSkip
	}
	return VerdictAllow
}

// Reason is the skip reason recorded when the admission filter rejects a
// candidate. EXACT string — it is a cross-surface contract (it appears in
// routes.jsonl on the Sovereign side and in the e2e assertions here;
// gateway.py:706 skip("context_too_small")).
func (admissionFilter) Reason() string { return "context_too_small" }

// outputReserve is the frozen reserve function: min(maxTokens,
// MinOutputReserve), with MaxTokens <= 0 meaning the model declares no
// output cap — the full 64-token reserve applies.
func outputReserve(maxTokens int) int {
	if maxTokens <= 0 {
		return MinOutputReserve
	}
	if maxTokens < MinOutputReserve {
		return maxTokens
	}
	return MinOutputReserve
}
