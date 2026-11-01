# R0 Research Findings: Trajectory Grading in CI

> Pinned findings for docs/plans/20261031-trajectory-golden-ci (leaf R0).
> ASCII only. Research digest + C2 verdict. No code, no git operations.

## Topic 1: tau-bench / tau2-bench outcome + process reward structure

- tau2-bench scores a task as the PRODUCT of the components listed in
  `evaluation_criteria.reward_basis` (default `["DB", "COMMUNICATE"]`).
  Each component is gated separately; one failed component zeroes the
  reward.
  https://github.com/sierra-research/tau2-bench/blob/main/docs/evaluation.md
- The reference `actions` list is ONE solving trajectory replayed on a
  fresh gold environment to derive a target DB end state. The agent is
  NOT required to take that path: any tool sequence producing an
  equivalent DB end state passes. Direct action-sequence gating
  (`RewardType.ACTION`) is opt-in and deliberately rare (used in a small
  subset of banking_knowledge tasks; never in airline/retail/telecom)
  because it assumes the reference is the ONLY correct trajectory.
  https://github.com/sierra-research/tau2-bench/blob/main/docs/evaluation.md
- tau2-bench paper: success criteria are DB check, status/env
  assertions, NL assertions, communication check, and action matching;
  each task picks a subset. Action matching = every expected call exists
  somewhere in the actual trajectory (order-insensitive presence).
  https://arxiv.org/html/2506.07982
- Implication for C2: even the benchmark famous for trajectory rigor
  avoids hard sequence gating for live agents, reserving it for cases
  where the path is itself the contract. Meept's goldens are captured
  from FakeLLM-scripted hermetic runs, so the reference IS the contract
  by construction (R4) - the tau2 caveat applies to the live tier,
  which the master policy already excludes from sequence gating (R5).

## Topic 2: Anthropic-style outcome-vs-trajectory grading

- Anthropic's eval guidance: code-based graders (string match, binary
  tests, tool-call verification, transcript analysis) are fast, cheap,
  objective, reproducible, and easy to debug; their weakness is
  brittleness to valid variation. Regression evals should run at
  nearly 100% pass rate and exist to catch backsliding.
  https://www.anthropic.com/research/demystifying-evals-for-ai-agents
- Anthropic explicitly warns against rigid step-sequence checks on
  live agents: "too rigid and results in overly brittle tests ... it's
  often better to grade what the agent produced, not the path it took,"
  and recommends partial credit for multi-component tasks.
  https://www.anthropic.com/research/demystifying-evals-for-ai-agents
- Trajectory checks are still mandated for process guarantees that
  outcomes cannot see: verify-before-acting, ask-before-delete,
  forbidden intermediate actions, stubbed tools where the trajectory is
  the only evidence, and debugging why an outcome failed. Exact
  reference matching is ranked "almost never; too brittle"; in-order
  subsequence is the recommended strictness for real ordering rules
  ("lookup before refund").
  https://www.anthropic.com/research/demystifying-evals-for-ai-agents
  (framing expanded in https://www.compilenrun.com/docs/ai/agents/agents-evaluation-and-testing/agents-trajectory-evaluation/ )
- Implication for C2: the sequence gate is justified precisely for the
  dropped-verify-step / reordered-read-after-write class, and the
  in-order subsequence mode is the community-recommended strictness.
  The brittleness warning is neutralized here because the substrate is
  deterministic (scripted FakeLLM replay), not a live model.

## Topic 3: Google ADK match modes (EXACT / IN_ORDER / ANY_ORDER)

- ADK `tool_trajectory_avg_score` compares actual vs expected tool
  calls per invocation with match_type EXACT, IN_ORDER, or ANY_ORDER,
  awarding 1.0 or 0.0 per invocation and averaging. Threshold default
  is 1.0; a score below 1.0 means at least one invocation deviated.
  https://adk.dev/evaluate/criteria/
  https://github.com/google/adk-docs/blob/main/docs/evaluate/criteria.md
- Implementations in
  https://github.com/google/adk-python/blob/main/src/google/adk/evaluation/trajectory_evaluator.py :
  - EXACT: same length, position-wise equal (name + args).
  - IN_ORDER: greedy subsequence scan - every expected call appears in
    actual in order; extra interleaved calls allowed.
  - ANY_ORDER: multiset containment - each expected call matched
    against a copy of actual (consumed on match); extras allowed.
- ADK defaults to EXACT; IN_ORDER/ANY_ORDER were added later
  (commit e2d3b2d, 2025-11-12).
  https://github.com/google/adk-python/commit/e2d3b2d862f7fc93807d16089307d4df25367a24
- ADK also supports `ignore_args` (name-only comparison) at the
  criterion level.
  https://github.com/google/adk-python/blob/main/src/google/adk/evaluation/eval_metrics.py
- Implication for C2: the C2 mode set and semantics mirror ADK
  one-to-one (MatchExact == EXACT, MatchInOrder == IN_ORDER,
  MatchAnyOrder == multiset ANY_ORDER), with IgnoreCallSet playing the
  role of ignore_args at coarser granularity. This is the mainstream
  scheme.

## Topic 4: LangChain agentevals match modes

- `create_trajectory_match_evaluator` modes: `strict` (same messages
  and tool calls in the same order; message CONTENT may differ),
  `unordered` (same tool calls in any order), `subset` (agent calls
  only reference tools - no extras), `superset` (agent calls at least
  the reference tools - extras allowed).
  https://github.com/langchain-ai/agentevals
  https://docs.langchain.com/langsmith/trajectory-evals
- Per-tool argument equality is separately configurable via
  `tool_args_match_mode` (`exact` | `ignore` | `subset` | `superset`)
  plus `tool_args_match_overrides` keyed by tool name (mode, required
  field list, or arbitrary comparator). Overrides take precedence over
  the global mode. Use case: tolerate case/format drift in LLM
  generated arguments for specific tools only.
  https://github.com/langchain-ai/agentevals
- Implication for C2: C2's ArgRules map[string]ArgRule (exact | subset
  | ignore, per tool name) is exactly the agentevals
  overrides-by-tool-name pattern. Two deltas, neither blocking:
  (a) agentevals has a "subset" TRAJECTORY mode (extras forbidden,
  order-free) that C2 lacks - C2 approximates it with MatchExact;
  (b) agentevals `strict` tolerates free-text content differences,
  which maps to C2's normalizer + IgnoreCallSet for brittle text tools.

## Topic 5 (T2): LCS / ordered-subsequence tolerance scoring

- UiPath `ToolCallOrderEvaluator`: non-strict mode scores
  `len(LCS(expected, actual)) / len(expected)`; e.g. expected
  [A,B,C,D] vs actual [A,X,B,D] -> 3/4 = 0.75. Strict mode is 1.0/0.0.
  https://uipath.github.io/uipath-python/eval/tool_call_order/
- gemini-adk-rs uses the same LCS idea but divides by
  max(len(actual), len(expected)), penalizing extra steps; set-based
  (Jaccard) mode for unordered.
  https://docs.rs/gemini-adk-rs/latest/src/gemini_adk_rs/evaluation/trajectory_evaluator.rs.html
- nanoevals and other small harnesses compute a greedy in-order
  match count over expected names and score matches/len(expected).
  https://github.com/trifiasco/nanoevals/blob/main/nanoevals/metrics.py
- Denominator choice is the load-bearing decision: /len(golden)
  treats extras as free (superset-friendly, UiPath-style) while
  /max(len) penalizes extras. For a CI gate whose purpose is "the
  required steps happened in the required order" and whose extra-call
  tolerance is handled explicitly (retries, interleaved reads,
  IgnoreCallSet), the golden-denominator form is correct. C2 pins
  len(golden). KEEP.
- C2's Score >= 1.0 default threshold under in_order is exactly
  boolean ADK IN_ORDER (LCS == len(golden) iff golden is an ordered
  subsequence of actual); partial scores become meaningful only when a
  scenario deliberately lowers the threshold. This layered design
  (boolean-by-default, partial-credit available) matches practice.

## Topic 6 (T2): flake-resistant CI gating of nondeterministic agents

- Lane separation: deterministic stubbed/replay tests gate every PR
  (hard gate, no retries); live-model smoke runs on merge/nightly with
  pass@k-style thresholds; eval suites trend on nightly. Red in a
  deterministic lane means "your change broke it" - never "probably
  the model."
  https://buildpulse.io/blog/ci-for-ai-agents-testing-non-deterministic-systems
  https://changegamer.ai/resources/testing-ai-agents
- Retry policy: CI may retry INFRASTRUCTURE failures (runner died,
  provider 5xx before first token); CI must NOT silently retry
  assertion failures - if retries exist, record attempts/successes and
  fail when a flake budget is exceeded. Best-of-N laundering makes a
  40% agent look 100% in a PR check.
  https://pnakhat.com/writing/flaky-agent-evals-ci.html
- Flip-source attribution: classify a verdict flip as agent-variance,
  judge-flake, infra-error, or mixed BEFORE acting on it; deterministic
  checks cannot flip on identical output by construction. Suite-level
  (not comparator-level) ownership of retry/attribution.
  https://github.com/suniel12/ciagent/blob/main/docs/stability.md
- Quarantine, not blind rerun: unstable cases move out of the merge
  gate into nightly soak until fixed; quarantined tests must keep
  reporting. Never quarantine an eval signal itself - a declining pass
  rate is the product getting worse, not noise.
  https://buildpulse.io/blog/ci-for-ai-agents-testing-non-deterministic-systems
  https://pnakhat.com/writing/flaky-agent-evals-ci.html
- pass^k vs pass@1: pass@k rises with k (any of k tries) and is the
  capability metric; pass^k falls with k (all k tries) and is the
  reliability metric users feel. Under independence pass^k = p^k
  (p=0.95, k=10 -> ~0.60), and measured pass^k is typically WORSE than
  p^k because failures correlate. Gate on the lower confidence bound,
  never the point estimate.
  https://agentreliability.dev/k/choosing-k-for-pass-hat-k
  https://latenteval.ai/research/how-to-measure-agent-reliability
  https://agent-evals-explained.vercel.app/learn/03-pass-at-k-and-pass-hat-k

## Recommendations (concrete)

1. Default `Options.Mode = MatchInOrder`, `Threshold = 1.0` as frozen.
   At threshold 1.0, in_order score reduces to the ADK boolean
   subsequence check; only deliberately-loosened scenarios should
   lower the threshold, and a lowered threshold must be visible in the
   golden fixture's `match` block (C4 already stores it).
2. Do NOT switch the score denominator to max(len(golden),
   len(actual)): the len(golden) denominator keeps extra/retry calls
   score-neutral, which is the intended tolerance. If extra-call
   hygiene ever needs gating, add a separate diagnostic count in
   Result (e.g. ExtraSteps) rather than changing Score semantics.
3. Keep ArgRules per-tool-name overrides (exact|subset|ignore) - it is
   the pattern agentevals validated. The C3 normalizer's variable
   scrubbing (`<var>` placeholder for id/uuid/timestamp/path/token)
   covers the remaining arg-shape drift agentevals handles with
   comparators; prefer normalizer rules over comparator callbacks to
   stay deterministic.
4. Suite-level flake attribution (already pinned in master policy):
   the trajectory-golden suite owns retry/attribution; the comparator
   must stay a pure function. Retry only infrastructure-class failures;
   an assertion-class failure is RED immediately. A golden diff
   failure is investigate-then-regenerate (R8) - never auto-retry into
   green.
5. Replays are deterministic, so pass^k compounding is NOT needed for
   the scripted gate (k=1 is honest). If any live-tier trajectory
   reporting is ever added, report pass^k-style reliability with a CI,
   never pass@k.
6. Risk flag (policy-level, no contract change): a golden fixture pins
   ONE scripted path. Its diagnostic value degrades silently if the
   scenario script and the fixture drift apart; the C5
   investigate-then-regenerate flow plus the deliberate-regression
   drill in the Integration Test Plan is the mitigation. Keep the
   drill in CI-adjacent review practice.

## C2 VERDICT: KEEP

Rationale: C2's mode set and semantics are a faithful superset of the
two mainstream reference implementations (Google ADK EXACT/IN_ORDER/
ANY_ORDER; agentevals strict/unordered/subset/superset reduced to the
scripted-replay case), the default threshold 1.0 makes in_order behave
as the community-standard boolean subsequence gate, the LCS score with
len(golden) denominator matches the UiPath-style partial-credit
precedent while keeping retries/extras score-neutral, and ArgRules
mirror agentevals' per-tool override pattern. No amendment required
before leaf 01 dispatches. Recommendations 2 and 3 above are
implementation guidance within C2's existing contract, not contract
changes.

## Source index

1. https://github.com/sierra-research/tau2-bench/blob/main/docs/evaluation.md
2. https://arxiv.org/html/2506.07982
3. https://www.anthropic.com/research/demystifying-evals-for-ai-agents
4. https://adk.dev/evaluate/criteria/
5. https://github.com/google/adk-python/blob/main/src/google/adk/evaluation/trajectory_evaluator.py
6. https://github.com/google/adk-python/blob/main/src/google/adk/evaluation/eval_metrics.py
7. https://github.com/langchain-ai/agentevals
8. https://docs.langchain.com/langsmith/trajectory-evals
9. https://uipath.github.io/uipath-python/eval/tool_call_order/
10. https://docs.rs/gemini-adk-rs/latest/src/gemini_adk_rs/evaluation/trajectory_evaluator.rs.html
11. https://github.com/trifiasco/nanoevals/blob/main/nanoevals/metrics.py
12. https://buildpulse.io/blog/ci-for-ai-agents-testing-non-deterministic-systems
13. https://pnakhat.com/writing/flaky-agent-evals-ci.html
14. https://github.com/suniel12/ciagent/blob/main/docs/stability.md
15. https://changegamer.ai/resources/testing-ai-agents
16. https://agentreliability.dev/k/choosing-k-for-pass-hat-k
17. https://latenteval.ai/research/how-to-measure-agent-reliability
18. https://agent-evals-explained.vercel.app/learn/03-pass-at-k-and-pass-hat-k
19. https://www.compilenrun.com/docs/ai/agents/agents-evaluation-and-testing/agents-trajectory-evaluation/
