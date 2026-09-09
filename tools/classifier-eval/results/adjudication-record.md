# Silver/Gold Adjudication Record — 2026-09-09

48 Hermes-transcript cases adjudicated by the user. Final labels applied
and promoted to `replay-gold.local.json5` (UNTRACKED — verbatim text).

## Decision rules (now normative for the taxonomy)

1. **review-and-CORRECT (autonomous) = quickplan.** "review X for bugs
   and correct them as you find them" is plan+clarify+execute, not a
   verdict request.
2. **review (verdict-only) = review.** Findings to the user, guaranteed
   no changes ("review the json files for completeness").
3. **execute-existing-plan**: bare execution verb → `plan`; execution
   WITH orchestration hints (subagents/waves/leaves) → `quickplan`.
   (#15 vs #45 distinction, user-confirmed.)
4. **research vs analyze**: "do research" = `research` when surveying
   without testable criteria; `analyze` when judging against testable
   criteria ("compare A vs B for our stack, give tradeoffs").
5. **search-verification** ("search and make sure it works") = `review`
   (verify-a-claim, no changes).
6. **document editing producing a tracked artifact = `code`** (#32).

## Final label distribution (48 cases)

quickplan 20 · review 10 · git 5 · plan 3+1(quickplan-rule 45→actually
3 plan) · code 4 · analyze 3 · debug 2 · platform 1

quickplan is the largest real-traffic class (42%), supporting both the
new-intent decision and quickplan-as-fall-through.

## Unresolved taxonomy notes

- #24 rule refined: research-vs-analyze hinges on testable criteria.
- #45/#15 distinction (bare verb vs orchestration hint) needs to be
  encoded in the quickplan detector's lexical triggers.
