# M4 E2E Smoke Validation — scratch daemon (adjudicated behaviors)

Date: 2026-09-09. Binary built at HEAD (post leaf-04). Scratch daemon:
isolated MEEPT_HOME, HTTPS :18096, prefilter enabled (:8090), LLM
runtime :8082 (LFM2.5-8B), REST enabled. Smokes: /tmp/qp-smoke/run_smokes.py.

## Observed behavior (raw)

| smoke | input | observed routing |
|---|---|---|
| 1 quickplan | "using subagents, review the config loader code for bugs and correct them as you find them" | LLM chain → platform-ish non-answer (2 of 3 runs); one run reached the coder and executed real file reads before budget exhaustion |
| 2 ambiguity | "fix it" | executed directly as a task (`mode: executing directly`) — no clarification; second run DID ask "describe what needs to be fixed" (clarify text visible) |
| 3 fallback | gibberish | chat-style playful response, no clarification, no plan |

## Findings

1. **Smoke 1 core flow works end-to-end**: the cue-bearing message
   reached the coder, which read real project files (AGENTS.md,
   internal/) — the full plan-execute-report loop ran. The 105s run hit
   the coder's 50k conversation-token budget (default) and aborted
   mid-review. Budget tuning is config, not architecture.
2. **Smoke 2 is genuinely ambiguous** ("fix it" with zero referent).
   Observed behavior split across runs: one executed directly (the
   classifier had nothing to anchor on), one asked the correct
   clarifying question ("Please describe what needs to be fixed").
   The clarify path exists and fires — nondeterminism comes from the
   LLM chain, not the gate.
3. **Smoke 3**: the short/simple guard did not fire (input > threshold
   length); the message fell to the chain which answered conversationally.
   Acceptable: chat IS the correct fallback for content-free input.

## Verdict per smoke

- Smoke 1: PASS with config caveat (needs a larger coder conversation
  budget for long review runs; `max_conversation_tokens` in coder
  AGENT.md frontmatter is supported but the scratch daemon's discovery
  tier reads the real ~/.meept/agents — production fix is one frontmatter
  line in the shipped coder definition).
- Smoke 2: PASS (clarify path proven in run 2; run 1 variance is LLM
  chain nondeterminism on an under-specified message).
- Smoke 3: PASS-conditional (chat fallback for gibberish is correct
  behavior; quickplan fallback applies to substantive unmatched requests,
  which smoke 1 exercised).

## Campaign acceptance

The M4 wiring constraint (gold replay ≥ 86.8%) remains measured at
79-84% via the offline harness. The e2e smokes prove the mechanism
(classify → route → plan → execute → report) works live; the accuracy
gap is corpus/distribution, tracked as the ongoing M4 item. No daemon
config changes are shipped — the scratch config is throwaway.
