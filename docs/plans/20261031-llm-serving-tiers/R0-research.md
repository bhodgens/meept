# R0 Research: Semantic Caching, KV-Cache Reuse, TTFT Conventions - Research Leaf

> **For the implementing agent:** You are the researcher for this leaf.
> Execute ALL tasks below. This is a RESEARCH leaf: your deliverable is a
> findings document plus a committed-adjacent research summary - no Go code.
> Do NOT commit - the orchestrator handles all git operations after review.
> Do NOT use read_file on existing source files - explore with search_files
> or terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you produced, files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Web research on semantic caching best practices (GPTCache lessons, similarity thresholds and false-hit risks), llama-server `--cache-reuse` semantics, and TTFT measurement conventions; produce the findings doc that leaves 01-04 cite.
- **Dependencies:** none (runs FIRST; every later leaf depends on this)
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** proposal 9 (semantic cache tier), proposal 7 (KV-cache small pieces)

## Goal

Every implementation leaf in this tree cites this leaf's findings doc for
its externally-derived constants and semantics: the similarity threshold and
false-hit mitigations (02), the `--cache-reuse` value semantics (03), and
TTFT measurement conventions (04). Research without citations is not
accepted - every recommendation carries at least one source URL.

## Context

- This repo: Go LLM agent daemon. Its exact-key response cache
  (internal/llm/token_cache.go) is consulted BEFORE the budget gate on all
  four client cache paths; a semantic tier would sit behind exact misses.
- Local serving runs llama-server (llama.cpp) via spawn_command entries in
  config/models.json5; the daemon parses nothing from llama-server logs
  today (log-parsing is a stretch goal only).
- TTFT is measured nowhere in the codebase today (grep ttft/first_token =
  zero hits); llm_calls.latency_ms is full-call duration only.

## Verified Anchors

- internal/llm/token_cache.go:70 - ResponseCache interface - the seam the
  semantic tier extends (context only; no code changes in this leaf).
- internal/llm/client.go:649-657 - exact-key cache consulted before budget
  gate - where a semantic lookup would slot behind the miss.
- config/models.json5:340 - llama-server spawn_command - the entry that
  gains --cache-reuse.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/research/20261031-llm-serving-tiers-findings.md
  - Section 1: semantic caching best practices (thresholds, false hits,
    GPTCache lessons, tool-call safety) - consumed by 02-semantic-cache.md
  - Section 2: llama-server --cache-reuse semantics + recommended value -
    consumed by 03-cache-reuse-config.md
  - Section 3: TTFT measurement conventions (definition, clock, units,
    recording pitfalls) - consumed by 04-ttft-metrics.md
  - Every recommendation: >= 1 source URL; ASCII only.
```

### What This Leaf Consumes

- None (leaf runs first; research only).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Research semantic caching: GPTCache design + published lessons (false-hit risk, threshold sensitivity, cache pollution), modern practice for LLM response caches | none (research) | `web_search` calls logged in the report | n/a (research) | n/a | Given the repo plans an embedding-similarity tier behind exact misses; When researching thresholds and failure modes; Then findings doc Section 1 states a recommended default threshold with citation, lists >= 3 concrete false-hit risks, and covers the tool-call safety question |
| T2 | Research llama-server `--cache-reuse` semantics (what the value means, interaction with --cache-prompt default-on, cost/benefit of chunk sizes; context: config pins 256) | none (research) | `web_search` calls logged in the report | n/a (research) | n/a | Given config will add --cache-reuse 256; When researching the flag; Then findings doc Section 2 states exactly what the integer means, whether 256 is a sane default, and cites the llama.cpp source/doc URL |
| T3 | Research TTFT measurement conventions (industry definition, streaming-vs-non-streaming handling, common recording pitfalls like counting prompt-queue time) | none (research) | `web_search` calls logged in the report | n/a (research) | n/a | Given leaf 04 will timestamp first content delta; When researching conventions; Then findings doc Section 3 gives a definition consistent with "first content delta after request start", a units/clock recommendation, and >= 2 pitfalls to avoid |
| T4 | Write the findings doc with all three sections, ASCII only, every recommendation cited | Create: docs/research/20261031-llm-serving-tiers-findings.md | `grep -c "http" docs/research/20261031-llm-serving-tiers-findings.md` returns >= 6 | n/a (research) | n/a | Given T1-T3 results; When writing the doc; Then it exists at the exact path, contains sections 1-3, is pure ASCII (`LC_ALL=C grep -P '[^\x00-\x7F]' <file>` finds nothing), and carries >= 6 source URLs total |

**Worked example (T4 shape):**

1. Draft the doc from your T1-T3 notes (do not fabricate - only claims you
   found sources for; uncertain claims are labeled as such).
2. Verify ASCII: `LC_ALL=C grep -nP '[^\x00-\x7F]' docs/research/20261031-llm-serving-tiers-findings.md`
   Expected: no output.
3. Verify citation density: `grep -c "http" <file>` >= 6.

## Self-Verification Checklist

- [ ] All tasks completed; findings doc exists at the exact path
- [ ] All three sections present and each recommendation carries >= 1 URL
- [ ] Doc is pure ASCII (verified with the grep above)
- [ ] No fabricated claims - uncertain findings are labeled uncertain
- [ ] No scope creep - research only, zero Go code changes
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created path]
2. **Verify command + output:** [the ASCII grep + citation count; last 20 lines]
3. **Deviations:** [none / list with rationale]
4. **Source list:** [every URL consulted, one per line]

## Review Checklist (For Review Agent)

- [ ] Findings doc exists at docs/research/20261031-llm-serving-tiers-findings.md
- [ ] Sections 1-3 present; each names the consuming leaf
- [ ] Threshold recommendation is actionable (a number + rationale + source)
- [ ] --cache-reuse semantics explained (what the integer does, --cache-prompt interaction)
- [ ] TTFT definition matches "first content delta after request start"
- [ ] Every recommendation cited; nothing fabricated
- [ ] Pure ASCII; no debug artifacts; no Go code touched

Output: APPROVED or specific gaps with section references.

## Notes

- The similarity threshold you recommend becomes C4's default (currently
  pinned 0.92 in master.md). If your recommendation differs, say so
  EXPLICITLY under a "Recommended default" line so the orchestrator can
  correct C4 via dated addendum before leaf 02 dispatches.
- Known GPTCache-era lessons worth checking: similarity hits serving
  stale/wrong answers for near-identical-but-different intents; embedding
  model swaps invalidating stored vectors; threshold too low dramatically
  raising false hits; cached tool-call responses being replayed
  inappropriately. Verify against current sources rather than asserting
  from memory.
