# Leaf 02 — DecisionClient seam

## Goal

A thin typed-decision client (`internal/llm/decision_client.go`)
abstracting "ask one typed question against the decision model".
Substrate for leaves 03/04. NO tier-routing changes (Contract C is
deferred — do not touch ResolveModelTier).

## Depends on

Leaf 01 (slot + resolver decisionModel field + DecisionModelName).

## Tasks

1. `internal/llm/decision_client.go` per master.md Contract B:
   `DecisionQuestion`, `DecisionResult`, `DecisionClient`, `Ask`,
   `Available`, `ErrDecisionUnavailable`, plus `ErrDecisionParse` for
   answer-token mismatches. Construction takes the Resolver.
2. Serving shape: `Ask` renders State + Prompt + numbered Options into a
   chat prompt, requests a ONE-token completion at temperature 0 through
   the same client-resolution path the classifier uses (read how the
   classifier's Chatter/client resolves — mirror it; do not invent a new
   transport). Parse the answer token back to a label (trim,
   case-insensitive, exact match; mismatch → ErrDecisionParse). Raw map:
   logprobs when the provider returns them; else argmax-only with
   Confidence 1.0 and an inline comment documenting the degradation.
3. Tests (decision_client_test.go): unconfigured → Available()==false,
   Ask→ErrDecisionUnavailable; stubbed client (reuse the Chatter stub
   pattern from context_compressor tests) → happy path, parse failure,
   option mismatch.
4. `go test -p 2 ./internal/llm/...` green; both binaries build.

## Constraints

- Do NOT modify resolver.go beyond reading DecisionModelName (leaf 01 owns it).
- Do NOT touch dispatcher, tools, or compaction.
- Explicit-path commit.

## Report back

RED/GREEN evidence, the resolution path used (file:line), Chatter stub
pattern reused.
