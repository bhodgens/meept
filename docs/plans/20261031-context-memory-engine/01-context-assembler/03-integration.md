# Integration: Tier Budgets into the Allocator + Loop Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Wire the allocator into the agent loop: resolve window from
  ContextDiscovery, adapt branch B's tier stores onto TierBudgetProvider,
  route memory injection through the seam, rewire BuildFullArtifactContext
  callers to the budgeted builder. Seam nil (incl. typed-nil) = today's
  behavior bit-for-bit.
- **Dependencies:** 01-allocator-core.md, 02-doc-budgets.md; branch B
  leaves 01-working-tier-store.md, 02-policy-automation.md,
  03-demotion.md REVIEWED (MUST compile WITHOUT them via nil seam)
- **Estimated Context:** 90K
- **Concurrency Group:** D
- **Audit references:** R8, R9

## Goal

Make the assembler real on a user-visible surface: a chat turn's prompt is
assembled with allocator-derived budgets. Resolve the per-model window
(internal/llm/context_discovery.go real context_limit; statics when
undiscovered), run Allocate once per request, then: memory injection uses
the C1 seam (nil -> MaxMemoryContextTokens=2000 path at loop.go:6884
unchanged); doc injection uses leaf 02's budgeted builder; iteration/
conversation budgets come from Allocation fields. The TierBudgetProvider
adapter maps branch B stores (internal/memory/working, episodic FTS5,
semantic claims) onto TierBudget{Ceiling, Recall} - authored HERE so
branch B never imports branch A.

## Context

- internal/agent/loop.go:6866-6920 - memory context build +
  EnforceMemoryItemBudget(bulleted, MaxMemoryContextTokens) at :6884 -
  THE injection path the seam routes.
- internal/agent/loop.go:4476 - per-iteration effectiveBudget math -
  consults Allocation.IterationBudget when assembler enabled.
- internal/llm/context_discovery.go - window resolution.
- internal/llm/context_firewall.go:548-584 - Derived budgets; keep as
  fallback path when firewall-only mode (no allocator) is active.
- internal/agent/prompt.go:518 - BuildSystemPromptOrdered - do not alter.
- internal/memory/working/ (branch B leaf 01), internal/memory/tierpolicy/
  (branch B leaf 02) - adapter sources.
- internal/daemon/AGENTS.md - typed-nil guards on Set* wiring; construct
  evolver-adjacent components after dependencies (wiring ordering rules).
- docs/agents/opt-in-defaults.md - the assembler's per-tier seam is
  opt-in: config `agent.context_assembler.enabled` default false; the
  memory-tier provider part activates only when branch B config is on.

## Verified Anchors

- internal/agent/loop.go:6884 - EnforceMemoryItemBudget wiring - seam insertion point
- internal/agent/loop.go:7230 - EnforceMemoryItemBudget definition - nil-path budget enforcement
- internal/agent/conversation.go:33 - MaxMemoryContextTokens=2000 - nil-path budget
- internal/agent/loop.go:4169-4193 - statics + conversationTokenBudget() - fallback derivation
- internal/agent/artifact_integration.go:293 - BuildFullArtifactContext callers - rewiring targets (grep to enumerate before edit)
- internal/llm/context_discovery.go:214-228 - override wins, resolver.SetContextLimits - window source

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/contextalloc/seam.go (new)
type LoopSeam struct { /* holds *Allocator, TierBudgetProvider, tokenizer */ }
func NewLoopSeam(a *Allocator, prov TierBudgetProvider, tk llm.Tokenizer) *LoopSeam
func (s *LoopSeam) MemoryContext(ctx context.Context, query string) (items []string, budgets map[TierName]int, ok bool)
// ok=false when seam disabled/nil-equivalent: caller takes today's path.

// internal/agent/loop.go (modified, minimal)
// - on AgentLoop construction: if cfg.ContextAssembler.Enabled, build
//   LoopSeam; SetTierBudgetProvider-style setter WITH typed-nil guard.
// - memory injection: seam.ok ? tier-budgeted injection : today's path.
// - iteration/conversation budgets: allocator-derived when enabled.

// config (internal/config/schema.go): agent.context_assembler.enabled
// (json/toml), default false, gendoc comments; NOT default-on.
```

### What This Leaf Consumes

- C1 TierBudgetProvider (../01-allocator-core.md) - adapter target
- C2 Allocation (../../master.md) - budgets consumed
- leaf 02: BuildFullArtifactContextBudgeted - doc path
- branch B: internal/memory/working store + tierpolicy Recall handles
  (adapter only; OPTIONAL AT COMPILE TIME via indirection)

CRITICAL seam rule: loop code references the provider ONLY through the
contextalloc.TierBudgetProvider interface + a registration function
invoked from daemon wiring. The adapter file lives in internal/agent/
contextalloc and imports internal/memory - NOT the reverse. If branch B
packages are absent the tree still builds (the adapter file is the only
importer; it is part of this leaf, present once branch B lands).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Config: agent.context_assembler.enabled default false + gendoc comments | Modify: internal/config/schema.go; Test: internal/config/schema_context_assembler_test.go | `go test -p 2 ./internal/config/ -run TestContextAssemblerDefault` | key absent | zero-value config => Enabled()==false; template comment ASCII | Given fresh Config struct; When read; Then Enabled false |
| T2 | LoopSeam + MemoryContext with typed-nil guard | Create: internal/agent/contextalloc/seam.go; Test: internal/agent/contextalloc/seam_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestLoopSeam` | seam.go absent | nil *Allocator => ok=false; typed-nil provider (var p *stubProv; NewLoopSeam(a, p, nil)) => ok=false; stub provider => budgets map with 3 tiers when provided | Given typed-nil provider; When MemoryContext; Then ok=false and zero allocations of recall |
| T3 | Loop wiring: enable path builds seam, routes memory injection, keeps nil path bit-identical | Modify: internal/agent/loop.go (minimal diffs at :6884 region + construction); Test: internal/agent/loop_seam_wiring_test.go | `go test -p 2 ./internal/agent/ -run TestLoopSeamWiring` | wiring absent | disabled config => EnforceMemoryItemBudget path with MaxMemoryContextTokens (golden: same output string as pre-change for fixed fixture); enabled + stub provider => items respect per-tier budgets | Given fixture memory items and disabled seam; When injection; Then output equals recorded golden exactly |
| T4 | Window resolution: allocator window from ContextDiscovery context_limit; fall back to statics when limit==0 | Modify: internal/agent/loop.go; Test: internal/agent/loop_seam_wiring_test.go | `go test -p 2 ./internal/agent/ -run TestWindowResolution` | no resolution logic | discovered 200k => window=200000-reserve math via Allocation; no model => IterationTokenBudget/DefaultConversationTokenBudget fallbacks unchanged | Given model.ContextLimit=200000; When assembler enabled; Then Allocation.WindowTokens==200000*usable factor per Config |
| T5 | Doc path rewiring: callers of BuildFullArtifactContext in the ENABLED path call BuildFullArtifactContextBudgeted with Allocation.SystemDynamicDocs; disabled path untouched | Modify: internal/agent/loop.go (+ any caller found by grep); Test: internal/agent/loop_seam_wiring_test.go | `go test -p 2 ./internal/agent/ -run TestDocPathRewiring` | enabled path ignores doc budget | enabled + 1MB CLAUDE.md fixture => section within doc budget; disabled => old unbounded path reproduced | Given enabled seam and huge doc; When turn assembled; Then doc section <= SystemDynamicDocs tokens |
| T6 | Daemon wiring + compile independence: construct seam AFTER dependencies in daemon startup; adapter maps branch B stores; tree builds with provider nil | Modify: internal/daemon/ (wiring site found by grep for memory manager construction), internal/agent/contextalloc/adapter.go; Test: internal/agent/contextalloc/adapter_test.go | `go build ./... && go test -p 2 ./internal/agent/contextalloc/ -run TestAdapter` | adapter absent | adapter with nil stores => provider returns empty slice (not panic); with stub stores => 3 tiers ordered working/episodic/semantic; `go build ./...` green | Given nil branch-B stores; When TierBudgets called; Then empty non-nil slice, no panic (typed-nil guard) |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (commands above + `go test -p 2 ./internal/agent/ ./internal/agent/contextalloc/ ./internal/config/`)
- [ ] Seam nil (and TYPED-nil) = today's behavior, golden-proven
- [ ] No import of internal/agent from internal/memory (direction check:
      `go list -deps ./internal/memory/ | grep contextalloc` must be empty)
- [ ] Consolidation untouched; wiki/trace stores absent from prompt path
- [ ] Config default false; ASCII only; gofmt clean
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Typed-nil guards on every Set*/registration (internal/daemon/AGENTS.md)
- [ ] Import direction correct; builds without branch B present
- [ ] Nil path golden-identical; enabled path budgets enforced
- [ ] No debug artifacts, no scope creep, no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

User-visible surface satisfied via the chat path itself (budgets shape the
prompt) plus config visibility (`meept config get agent.context_assembler.enabled`
works via existing generic config get). Do NOT touch SetCompactor
(loop.go:2486) or WrapToolOutput (loop.go:5362). If branch B leaves are
not REVIEWED yet, T6's adapter compiles against their pinned store
interfaces from branch B's orchestrator contracts; report BLOCKED instead
of improvising store APIs.
