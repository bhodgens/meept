# Tier Floor Doc Fix - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Write the doc fix, run the verify commands, report results.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Correct the TierFloorError field doc in errors_tier.go.
  Code is CORRECT as-is; the pinned e2e observes next-hop semantics.
- **Dependencies:** none. Do-first leaf (S, ~15 min).
- **Estimated Context:** 15K

## Goal

errors_tier.go:26-27 documents ProviderID/ModelID as "the LAST member
that was dispatched (not the refused next hop)". The call site
(provider_manager.go:533) passes the hop ABOUT to dispatch (the refused
next hop). The pinned e2e (e2e/suites/llm-serving-tiers/tier_policy_test.go:47-54,
"floor error does not name the refused hop" is the OBSERVED contract)
pins next-hop. Doc is wrong, not code.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Rewrite the errors_tier.go:25-28 comment to next-hop semantics: ProviderID/ModelID identify the member ABOUT to dispatch (the refused next hop; it was never touched). Keep the Cause/lastErr sentence - that describes the dispatch failure that TRIGGERED the floor, which is separate and correct. | Modify: internal/llm/errors_tier.go (comment only, zero code change) | `go doc ./internal/llm TierFloorError` shows next-hop wording; `go build ./internal/llm/` | none (doc-only) | doc names next-hop; no code diff | Given the comment; When read against provider_manager.go:533 and the pinned e2e; Then semantics match |
| T2 | Grep for any OTHER doc repeating the last-dispatched claim (docs/workflows, docs/reference, comments in tier_policy.go) and correct in the same commit. | Modify: any files with the stale claim | `grep -rn "LAST member" internal/ docs/ | grep -i tier` returns only corrected text | stale copies exist | zero stale copies | Given repo; When grepped; Then only next-hop wording remains |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] go test -p 2 ./internal/llm/ -run TestTier green (pinned e2e untouched)
- [ ] gofmt clean
- [ ] ASCII only
- [ ] zero code changes (git diff shows comment lines only)

## Review Checklist (orchestrator)

- [ ] Comment now says next-hop
- [ ] e2e tier_policy_test.go:47-54 unmodified
- [ ] No code change
- [ ] Stale copies swept (T2 grep output in report)

## Do NOT commit. The orchestrator stages and commits after review.
