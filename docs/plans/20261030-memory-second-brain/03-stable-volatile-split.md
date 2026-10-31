# Stable/Volatile Recall Split - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The parent gate tests (00T) ALREADY
> EXIST - your job includes making the F4 assertions GREEN. Do NOT delete,
> skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT use read_file
> on existing source files. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Split recall injection into a cacheable stable section (standing facts) and the volatile per-turn fence, preserving the whole-item token budget.
- **Dependencies:** none (concurrent group A with 05)
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Audit references:** F4 of master Test Gate; R4

## Goal

Memory recall currently lands entirely as a volatile `<memory-context>` fence
(2000-token cap), so every turn's prompt prefix churns and local-run prompt
caches miss. This leaf splits: items marked stable (persona facts, standing
project context - rarely change) render via
`builder.AddSectionWithStability("Memory (standing)", ..., true)`; per-turn
recall stays in the budgeted fence. Result: the stable section joins the
cacheable prompt prefix (AddSectionWithStability seam exists at
internal/agent/prompt.go), and the volatile fence stays small.

This is the NOT-STARTED port candidate #1 from the memory-extension-seams
reference - its seam description is authoritative.

## Context

- SplitMemoryItems / EnforceMemoryItemBudget own memory-context assembly
  (grep in internal/agent - they replaced the proportional mid-string cut;
  whole-item greedy drops in relevance order, observability marker on drop).
- conv.FreezeMemorySnapshot freezes memory at session start for prefix
  caching; BuildPromptWithSnapshot renders it (internal/agent/loop.go
  buildSystemPromptWithContextAndSkills, ~7295-7360).
- The stable flag rides memory metadata ("stable": true) - the scoping branch
  (01) adds metadata conventions; this leaf only READS the flag with default
  false (volatile), so the leaves are independent.

## Verified Anchors

- internal/agent/prompt.go - AddSectionWithStability (stable=true joins the
  cacheable prefix; SplitSystemPromptOnBoundary is the Anthropic-side
  consumer - do NOT change boundary semantics).
- internal/agent/loop.go:6871 (EnforceMemoryItemBudget call) + ~7293-7360
  (buildSystemPromptWithContextAndSkills, snapshot render + fence).
- internal/agent/conversation.go:33 - MaxMemoryContextTokens = 2000.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C6: stable items (metadata "stable" truthy) ->
//   builder.AddSectionWithStability("Memory (standing)", rendered, true)
// volatile items -> existing <memory-context> fence, budgeted as today
// SplitMemoryItems/EnforceMemoryItemBudget keep whole-item semantics; the
// budget applies to the VOLATILE set (stable is small by definition - cap
// stable at its own conservative bound: 25% of MaxMemoryContextTokens).
```

### What This Leaf Consumes

- C6 from ../master.md.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Stable flag read + split at assembly | Modify: internal/agent/loop.go (assembly + render sites) | `go test -p 2 ./internal/agent/ -run TestMemory` (existing memory tests) + gate suite F4 | all items render in fence | stable items render under "Memory (standing)" via AddSectionWithStability; volatile in fence | Given one stable + one volatile item; When prompt builds; Then section split exact |
| T2 | Budget split | Modify: internal/agent/loop.go (budget site) | same | budget cuts across both sets | volatile budget = MaxMemoryContextTokens whole-item greedy (unchanged semantics); stable capped at 25% (500 tokens) whole-item | Given volatile items exceeding budget; When assembled; Then whole lowest-ranked volatile items drop, marker present, stable untouched |
| T3 | Snapshot path compatibility | Modify: internal/agent/loop.go (snapshot render) | `go test -p 2 ./internal/agent/` | snapshot path renders single blob | frozen snapshot renders the same split (stable section + volatile fence) so the CACHEABLE prefix actually contains the stable section | Given a frozen snapshot with both kinds; When BuildPromptWithSnapshot; Then split holds |

## Self-Verification Checklist

- [ ] Gate F4 assertions GREEN
- [ ] Existing memory tests green; whole-item drop semantics unchanged
- [ ] Stable section title exactly "Memory (standing)" (contract C6 - the
      gate asserts the literal)
- [ ] No change to SplitSystemPromptOnBoundary / Anthropic cache_control
      emission
- [ ] gofmt clean; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output** [last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] C6 implemented verbatim (section title, budget split, metadata flag)
- [ ] Prompt-cache mechanics honored: stable lands BEFORE volatile in
      assembly order; nothing volatile leaks into the stable section
- [ ] Existing tests untouched and green
- [ ] No debug artifacts

Output: APPROVED or specific gaps.

## Notes

- The agent-side WithSystemBlocks wiring (internal/agent/prompt.go) is a
  recorded follow-up in the context-reduction map - OUT OF SCOPE here; if the
  split makes that follow-up trivial, note it in the report.
- Items BECOME stable via metadata; who marks them stable is a distill-side
  convention (e.g. persona/standing facts) - coordinate: a one-line addition
  to the distill prompt template is IN scope if trivial; a new extraction
  pass is NOT.
