# Doc Budgets: Bounding CLAUDE.md and Doc Sections - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Budget the dynamic doc sections (CLAUDE.md verbatim hole,
  README, skills/agents refs) from the allocator's SystemDynamicDocs
  allocation; preserve stable-prefix hashing byte-exactly.
- **Dependencies:** 01-allocator-core.md
- **Estimated Context:** 75K
- **Concurrency Group:** C
- **Audit references:** R3, R4

## Goal

Close the DOCS HOLE: internal/agent/artifact_integration.go:293-296 injects
CLAUDE.md RawContent verbatim with NO size bound (README at :315-334 is
capped at 4KB chars only, not tokens). BuildFullArtifactContext gains a
token budget parameter derived from Allocation.SystemDynamicDocs; oversize
doc content is truncated deterministically (head of the file + truncation
notice carrying original/budgeted token counts). CRITICAL: doc content is
DYNAMIC per project - it must stay in the UNSTABLE region of the prompt.
Verify and, if needed, pin the section classification so AssembleOrdered's
stablePrefixHash (internal/agent/prompt.go:461-525, sha256 :468,496) never
changes across requests within a session when doc content changes; the
stable/unstable classification itself must remain byte-exact with the
pre-change baseline.

## Context

- internal/agent/artifact_integration.go:275-335 - BuildFullArtifactContext
  + summarizeREADME - the code this leaf budgets.
- internal/agent/prompt.go:451-525 - PromptSection{name, Stable},
  AssembleOrdered, BuildSystemPromptOrdered, emptyPrefixSHA256 -
  stable-first assembly + drift hash; DO NOT alter ordering semantics.
- internal/agent/prompt.go:556+ - classifySections - where sections get
  their Stable flags.
- internal/llm/tokenizer.go - llm.NewTokenizerForModel for token counts.
- ../01-allocator-core.md - Allocation.SystemDynamicDocs source of budget.
- findings-context-budgeting.md - R-Ax citations for truncation strategy.

## Verified Anchors

- internal/agent/artifact_integration.go:293-296 - CLAUDE.md verbatim injection - THE hole
- internal/agent/artifact_integration.go:315-334 - summarizeREADME 4KB char cap - char cap, not token budget
- internal/agent/prompt.go:461-525 - AssembleOrdered + stablePrefixHash - must not drift
- internal/agent/prompt.go:556-575 - classifySections stable flags - doc sections must be Stable=false

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/artifact_integration.go (modified)
func (am *ArtifactManager) BuildFullArtifactContextBudgeted(
    taskDesc, workingDir string, docBudgetTokens int, tk llm.Tokenizer,
) (section string, truncated bool)
// - docBudgetTokens <= 0 => today's unbounded behavior is REPLACED by a
//   documented default of 2000 tokens (matches MaxMemoryContextTokens
//   scale) ONLY at the new entry point; the OLD BuildFullArtifactContext
//   signature and behavior are UNCHANGED for existing callers in this
//   leaf (rewiring callers is leaf 03's scope).
// - Truncation: keep head content until budget, append notice
//   "\n\n[... CLAUDE.md truncated: NNNN/TTTT tokens ...]" (ASCII).

// internal/agent/prompt.go (test-only additions allowed)
// TestBaselineStablePrefixHash pins the current hash of a fixed section
// set to guard byte-exact classification.
```

### What This Leaf Consumes

- C2 Allocation (../../master.md) - SystemDynamicDocs field budget
- C3 Allocator (../01-allocator-core.md) - `Allocate` (used in tests only)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Baseline drift guard: pin today's stablePrefixHash for a fixed section list in a committed golden test | Test: internal/agent/prompt_stable_prefix_baseline_test.go | `go test -p 2 ./internal/agent/ -run TestBaselineStablePrefixHash` | test does not exist | passes against current code and FAILS LOUDLY if classification changes | Given fixed PromptSection list; When AssembleOrdered; Then hash equals pinned constant |
| T2 | Budgeted doc builder: BuildFullArtifactContextBudgeted with token budget + deterministic head-truncation + ASCII notice | Modify: internal/agent/artifact_integration.go; Test: internal/agent/artifact_budget_test.go | `go test -p 2 ./internal/agent/ -run TestBuildFullArtifactContextBudgeted` | new func undefined | 1MB CLAUDE.md fixture truncated to <= budget tokens; small files byte-identical to content; notice has original/budgeted counts | Given docBudgetTokens=2000 and 1MB doc; When Budgeted called; Then resulting section token count <= 2000 and notice present |
| T3 | Tokenizer choice: use llm.NewTokenizerForModel when a model is known; HeuristicTokenizer fallback; budget counts via the SAME tokenizer at build time | Modify: internal/agent/artifact_integration.go; Test: internal/agent/artifact_budget_test.go | `go test -p 2 ./internal/agent/ -run TestDocBudgetTokenizer` | no tokenizer param behavior | nil tk falls back to HeuristicTokenizer; counts consistent | Given tk=nil; When Budgeted; Then heuristic counting used and result deterministic |
| T4 | Stable classification pinning: verify doc/artifact sections are classified Stable=false in classifySections; if any artifact-derived section is Stable=true, reclassify (test proves byte-stability of the prefix hash while doc content varies) | Modify: internal/agent/prompt.go (only if misclassified); Test: internal/agent/prompt_stable_prefix_baseline_test.go | `go test -p 2 ./internal/agent/ -run 'TestBaselineStablePrefixHash|TestClassification'` | classification test absent | hash stable across varying doc content; all artifact sections Stable=false | Given two runs differing ONLY in CLAUDE.md content; When assembled; Then stablePrefixHash identical |
| T5 | Unit-test MaxMemoryContextTokens-adjacent edge: docBudgetTokens<=0 uses documented 2000 default; negative treated as 0->default | Modify: internal/agent/artifact_integration.go; Test: internal/agent/artifact_budget_test.go | `go test -p 2 ./internal/agent/ -run TestDocBudgetDefault` | default absent | default applied, doc comment updated | Given docBudgetTokens=0; When Budgeted; Then budget = 2000 |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/agent/`)
- [ ] BuildFullArtifactContext (old signature) behavior UNCHANGED
- [ ] stablePrefixHash golden test green; classification untouched or
      provably improved with rationale
- [ ] All files at exact specified paths; ASCII only; gofmt clean
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing; baseline hash test present
- [ ] Old entry point byte-identical behavior (diff of old call path)
- [ ] Truncation deterministic; notice ASCII with counts
- [ ] No debug artifacts, no scope creep, no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

The hole: any repo with a multi-megabyte CLAUDE.md currently ships it all
to the model every turn (artifact_integration.go:293-296). The fix must be
budget-driven (tokens, from Allocation.SystemDynamicDocs), not another
char cap. Keep old function untouched so leaf 03 owns caller rewiring;
this leaf owns the mechanism + proof. findings-context-budgeting.md R-Ax
citations in comments for head-truncation vs tail strategy.
