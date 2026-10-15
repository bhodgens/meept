# Leaf 02 — DecisionClient seam + confidence-driven tier routing

## Goal

A thin typed-decision client (`internal/llm/decision_client.go`) plus the
confidence escalation method on Resolver, wired so legacy behavior is
byte-identical when the decision model is unset.

## Depends on

Leaf 01 (slot + resolver decisionModel field) landed.

## Tasks

1. `internal/llm/decision_client.go` — implement exactly Contract B from
   master.md: `DecisionQuestion`, `DecisionResult`, `DecisionClient`,
   `Ask`, `Available`, `ErrDecisionUnavailable`. Construction takes the
   Resolver; `Ask` resolves the decision model via the resolver (so quota/
   cooldown handling comes free — use the same resolution path skills use,
   e.g. ResolveForAlias-style; read how classifier calls resolve first and
   mirror it).
2. Serving shape: `Ask` renders `State + Prompt + numbered Options` into a
   chat prompt and requests a ONE-token completion at temperature 0; parse
   the answer token back to the label (trim, case-insensitive, exact match
   else empty answer → error `ErrDecisionUnavailable`-style
   `ErrDecisionParse`). If the provider's completion response carries
   logprobs, build `Raw` from them; otherwise Raw holds only the argmax
   with confidence 1.0-minus-parse-distance = 1.0 and document the
   degradation inline. Confidence: when logprobs absent, derive from the
   returned token's logprob if present, else 1.0 (caller-side thresholds
   still apply; a 1.0-conf parse-answer is treated as "decided").
3. `internal/llm/resolver.go` — add `ResolveModelTierWithConfidence(tier,
   fallback string, confidence float64) string` per Contract C:
   - tier != "fast" → identical to ResolveModelTier(tier, fallback).
   - tier == "fast" && confidence >= DecisionEscalationThreshold (read the
     knob from config via resolver's ProvidersConfig; 0 = off) → return
     defaultModel's id (nil-safe: fall back to legacy result).
   - else → identical to ResolveModelTier.
   ResolveModelTier itself is NOT modified.
4. Tests:
   - decision_client_test.go: unconfigured → Available()==false,
     Ask→ErrDecisionUnavailable; configured with a stub Chatter-style
     client (read how context_compressor tests stub the summarizer
     Chatter and reuse that pattern) → parse happy path, parse failure
     error, option-mismatch error.
   - resolver tier test: table-driven — legacy path unchanged at
     confidence 0, escalation at 0.95 with threshold 0.85, off with
     threshold 0, unknown tier falls to fallback in both methods.
5. Dispatcher pass-through: find where the agent loop / handler picks the
   model via tier for a turn (grep `ResolveModelTier` callers in
   internal/agent + internal/daemon). At the call site where the Intent's
   confidence is available (Intent has Confidence — verify field name via
   grep `type Intent struct` in internal/agent/intent.go), switch that ONE
   call site to ResolveModelTierWithConfidence, passing intent confidence
   when non-zero, else 0 (never fabricate). Other call sites untouched.
6. Sweep: `go test -p 2 ./internal/llm/... ./internal/agent/...` green;
   `go build` both binaries.

## Constraints

- No changes to dispatcher classification itself — routing signal only.
- Explicit-path commit.

## Report back

RED/GREEN evidence, the modified call site (file:line), and whether the
Intent confidence field name matched (if the cascade does not populate
confidence at that call site, report the actual available signal — do not
invent one).
