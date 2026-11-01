# Token Audit - Measurement Leaf

> **For the implementing agent:** MEASUREMENT ONLY. Scratch programs in /tmp;
> the repo may be read freely; write nothing into the repo except your report
> file location named below (the orchestrator archives it). Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** measure where meept's per-turn input tokens go today; produce the
  numbers that decide Open Questions Q1/Q2
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** solo

## Goal

Answer with numbers: (1) cold-start system prompt tokens by section;
(2) tool-definition tokens: full mode vs indexed mode, all ~75 tools;
(3) per-turn overhead: what a bare "hello" turn costs before any work;
(4) stub-list cost breakdown: which stubs are never plausibly used together
(co-occurrence evidence from any existing logs/eval data if cheap, else skip);
(5) comparison anchor: pi's ~18.2K cold "Hello" (agents.cocode.dk) vs measured
meept.

## Context

- Counting helpers exist: llm.CountToolDefinitionsTokens
  (internal/llm/models.go:541), estimateTokens-style heuristics in
  internal/llm/context_compactor.go.
- Prompt assembly: internal/agent/prompt.go BuildSystemPromptOrdered (:518);
  ContextInjector.BuildSystemPrompt (context_injector.go:54).
- Tool defs: registry.ToLLMDefinitions (internal/tools/registry.go:249) in
  both SchemaModeFull and SchemaModeIndexed.
- A scratch Go program in /tmp CAN import meept packages via a go.mod replace
  directive pointing at /Users/caimlas/git/meept (module name from its go.mod).
  That is the cheapest path to real numbers without touching the repo.

## Verified Anchors

- internal/llm/models.go:541 - CountToolDefinitionsTokens
- internal/agent/prompt.go:518 - BuildSystemPromptOrdered
- internal/tools/registry.go:249 - ToLLMDefinitions
- internal/config/schema.go:1954 - DefaultAlwaysFullTools (10 names)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Report at /tmp/pi-eval/meept-token-audit.md with tables:
1. System prompt by section (constitution, restrictions, purpose, tools
   section, project context, memory, skills listing, custom) - chars + est tokens.
2. Tool defs: full vs indexed, total + top-10 largest stubs.
3. Bare-turn total (prompt + tools) vs pi anchor 18.2K.
4. Candidate savings per shape A/B/C/D from ../orchestrator.md, computed from
   the measured numbers (e.g. B saves the stub list minus a 2-meta-tool +
   category-digest estimate).

### What This Leaf Consumes

- none

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Scratch measurement program | /tmp/pi-eval/token-audit/ (go.mod replace -> repo) | `go run .` in scratch | n/a | prints all four tables | Given the real registry + prompt builder; When run; Then numbers come from production code paths, not hand-estimates |
| T2 | Section breakdown | same | same | n/a | per-section chars+tokens | Given BuildSystemPromptOrdered output; When split on section markers; Then each section measured |
| T3 | Full vs indexed tool defs | same | same | n/a | both totals + delta | Given ToLLMDefinitions in both modes; When counted; Then delta quantified |
| T4 | Savings model per candidate | report | n/a | n/a | A/B/C/D savings rows with arithmetic shown | Given measured stub cost; When modeling shape B; Then savings = stub list - (2 meta-tool schemas + category digest estimate) |

## Self-Verification Checklist

- [ ] Numbers from production code paths (imports, not copy-pasted prompt text)
- [ ] Token estimates name their method (heuristic chars/N or tokenizer)
- [ ] Repo untouched (git status shows no new files in /Users/caimlas/git/meept)

**DO NOT COMMIT.**

## Completion Report Format

1. Report path; 2. the four tables verbatim; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Spot-check one table cell by re-running the scratch program
- [ ] Savings model arithmetic correct (recompute one row)
- [ ] pi anchor cited with source (docs/research/token-efficiency-2026-10/pi-evidence.md)
