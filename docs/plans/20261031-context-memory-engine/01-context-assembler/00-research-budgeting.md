# Research: LLM Context-Window Budget Allocation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Execute the research tasks below and write the pinned findings doc. This
> is a RESEARCH leaf: use web_search extensively; produce a findings
> document, not code. Do NOT commit - the orchestrator handles all git
> operations after review.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Research best practices for per-request context-window budget
  allocation and pin them in a findings doc that sibling leaves cite.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** R11

## Goal

Produce `docs/plans/20261031-context-memory-engine/01-context-assembler/
findings-context-budgeting.md`: a pinned, sourced summary of how
production systems allocate a resolved LLM context window across prompt
sections, with concrete recommendations this tree's allocator implements.
Siblings (01, 02, 03) MUST cite this doc in their Notes sections.

## Context

The allocator being built (leaf 01) splits a resolved window into
sections: stable system prompt, dynamic docs, memory tiers, tools,
history, output reserve. Research should inform: fraction choices and
their adaptivity, per-section floors/caps, stable-prefix preservation for
provider KV/prompt caches, and compaction/summarization triggers. Current
meept statics for contrast: internal/agent/loop.go:4169-4184
(IterationTokenBudget=30000, ToolResultMaxTokens=3000,
DefaultConversationTokenBudget=50000, warn 0.80),
internal/agent/conversation.go:22,33 (DefaultMaxMessages=200,
MaxMemoryContextTokens=2000), internal/llm/context_firewall.go:548-584
(ratio-derived budgets), internal/llm/tokenizer.go (NewTokenizerForModel,
HeuristicTokenizer 3 chars/token).

## Verified Anchors

- internal/agent/loop.go:4169-4184 - budget statics - the constants the
  allocator supersedes when a real window is discovered
- internal/llm/context_discovery.go - resolves real context_limit - the
  "resolved window" input the research informs
- internal/agent/prompt.go:461-525 - AssembleOrdered + stablePrefixHash -
  the cache-preservation constraint research must cover

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
findings-context-budgeting.md with REQUIRED sections (exact headings):
  ## Budget Allocation Practices
  ## Stable-Prefix and KV-Cache Preservation
  ## Compaction and Summarization Policies
  ## Recommendations for the Allocator
Each recommendation numbered R-A1, R-A2, ... so siblings cite by ID.
```

### What This Leaf Consumes

- none (research leaf)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey budget-allocation practice (per-section budgeting, window fractions, floors/caps) via web_search: include Anthropic/OpenAI prompt-caching docs, cBTC/LLMLingua-style compaction literature, MemGPT/Letta paper, and at least 2 production engineering posts | none (notes) | manual review | n/a (research) | findings has >=6 distinct cited sources | Given search results; When synthesized; Then each claim carries a URL |
| T2 | Research stable-prefix preservation: provider KV/prompt cache behavior (Anthropic cache_control, OpenAI automatic prefix caching), why byte-identical prefixes matter, drift failure modes | none (notes) | manual review | n/a | Stable-Prefix section covers >=3 sources + failure modes | Given cache docs; When summarized; Then byte-drift consequence stated explicitly |
| T3 | Research compaction policies: when to summarize vs drop (thresholds, ratios), reserve budgets for output, small-context model adjustments | none (notes) | manual review | n/a | Compaction section has >=3 policies with source URLs | Given policies; When recorded; Then each has an applicability note for meept |
| T4 | Write findings-context-budgeting.md with the four required headings + numbered Recommendations R-A1..N mapping to allocator design (fractions, floors, output reserve, compaction hook) | Create: docs/plans/20261031-context-memory-engine/01-context-assembler/findings-context-budgeting.md | `grep -c '^## ' docs/plans/20261031-context-memory-engine/01-context-assembler/findings-context-budgeting.md` | file absent | >=4 headings, >=5 recommendations, all ASCII | Given research notes; When written; Then recommendations are concrete (numbers, not vibes) and ASCII-only |

Given/When/Then acceptance: the doc is self-contained -- a leaf-01 agent
reading only this file plus the branch orchestrator can justify every
fraction/floor it implements by citing an R-Ax ID.

## Self-Verification Checklist

- [ ] All tasks completed; findings doc exists at the exact path
- [ ] All four required headings present, exactly as specified
- [ ] Every factual claim carries a source URL
- [ ] At least 5 numbered recommendations (R-A1..)
- [ ] ASCII only throughout
- [ ] No scope creep - no code changes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [grep heading count + source count]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Findings doc exists with exact required headings
- [ ] Sources cited per claim; >=6 distinct URLs
- [ ] Recommendations concrete and implementable; cited by-ID scheme
- [ ] ASCII only; no code changes anywhere

Output: APPROVED or specific gaps.

## Notes

Search at least: "prompt caching anthropic", "openai prompt caching
prefix", "context window budget allocation LLM", "MemGPT Letta memory
tiers", "LLMLingua prompt compression", "context compaction summarization
agent". Cite the allocator-relevant numbers (e.g. recommended output
reserve fraction ranges) verbatim in the findings doc.
