# 05: Memory Ceiling via Groups - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Feed the group MemoryMaxScope ceiling into the recall filter:
  effective ceiling = narrower of agent MemoryMaxScope and group ceiling.
- **Dependencies:** 02-groups-model (REVIEWED) AND issue #70 tree branch 01
  (docs/plans/20261030-memory-second-brain/01-scoping/) COMPLETE. If #70
  branch 01 has not landed: this leaf is BLOCKED - do not fork scoping
  primitives; report BLOCKED to the master orchestrator.
- **Estimated Context:** 40K
- **Concurrency Group:** C

## Goal

A grouped agent with ceiling "project" cannot recall user- or global-scope
rows, even when its own AGENT.md declares a wider ceiling - groups NEVER
widen (C7).

## Interface Contracts (From Parent)

C7 (verbatim): effective = min(agent MemoryMaxScope, group
Resolved.MemoryMaxScope); scope order project < user < global. Fed into
WithScopeFilter at buildMemoryContext + the four memory tools. The memory
scoping primitives (Scope, WithScopeFilter) come from #70 - REUSE, never
parallel-mint.

## Verified Anchors (verify post-#70-landing; line numbers will drift)

- Agent MemoryMaxScope resolution: #70 leaf 01-scoping/03 deliverable
  (resolveScopeInputs in internal/agent).
- Recall filter sites: buildMemoryContext (internal/agent/loop.go) + the four
  memory tools (internal/tools/builtin/memory.go).
- The loop's group Resolved record arrives via leaf 04's wiring (dependency:
  04 REVIEWED, or at minimum its T1 resolution-record seam merged).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Narrowing function: EffectiveMemoryScope(agentDecl, groupCeiling string) memory.Scope with project<user<global order; empty = unrestricted only if BOTH empty | internal/groups/ (or internal/agent per #70 layout - same package as resolveScopeInputs), +test | `go test -p 2 ./internal/groups/ -run TestMemoryScopeNarrow` | fn absent | matrix: agent global + group project => project; agent "" + group user => user; both "" => unrestricted |
| T2 | Wire effective ceiling into resolveScopeInputs path (loop + tools) | internal/agent/ (recall sites), +test | `go test -p 2 ./internal/agent/ -run TestScope` (+#70 gate suite green) | ceiling not consulted | grouped agent recall filtered at effective ceiling; ungrouped unchanged |
| T3 | Pin: groups cannot WIDEN (agent declares project, group declares global => project) | test only | same as T2 | n/a | pin green |

## Self-Verification Checklist

- [ ] #70 scoping primitives REUSED (grep: no new Scope/WithScopeFilter defs)
- [ ] Ungrouped agents: identical recall behavior to post-#70 baseline
- [ ] Gate tests (both trees) GREEN and unmodified

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. #70 interplay notes: [any drift from the pinned contract]

## Review Checklist (For Review Agent)

- [ ] Narrowing direction correct (never widens) - T3 pin exists and RED-verified
- [ ] Both recall surfaces (loop injection + memory tools) consult the ceiling
