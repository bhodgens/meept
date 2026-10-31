# Agent Recall Wiring (MemoryMaxScope) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The branch gate tests (00T-tests.md)
> ALREADY EXIST and are committed - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add MemoryMaxScope to agent specs; resolve scope filter inputs from the session at recall time; wire loop recall + memory tools to filter and stamp.
- **Dependencies:** 01-scope-schema.md (COMPLETE)
- **Estimated Context:** 65K
- **Concurrency Group:** B
- **Audit references:** S3 of branch Test Gate

## Goal

Every recall in the agent loop passes through WithScopeFilter with inputs
resolved from the session, and every memory tool write stamps the session's
scope. An agent definition declares the widest scope it may recall
(MemoryMaxScope, default project) - the chief-of-staff pattern: one agent
spans projects by explicit declaration, everyone else is isolated.

## Context

- internal/agent/loop.go memory recall path: ~L7160-7195 region assembles
  `<memory-context>` (grep memory-context / recallMemory for current lines -
  line numbers drift; anchor by symbol).
- Agent specs: internal/agent/registry.go (RegistryConfig + spec resolution)
  and the AGENT.md frontmatter discovery path; employee definitions add
  fields via their definition struct (grep memory_max_scope after gate lands).
- memory tools: internal/tools/builtin/memory.go (memory_store,
  memory_retain stamp on write; memory_search/memory_recall filter on read).
- Session identity: project binding per session (DetectionContext.CWD
  resolution); multiuser identity via the auth store when enabled.

## Verified Anchors

- internal/agent/loop.go (recall assembly; anchor by `memory-context` fence
  symbol) - recall call site to thread the filter through.
- internal/tools/builtin/memory.go:28 (MemoryStoreTool) + :643
  (MemoryRetainTool) - write-stamping sites.
- internal/tools/builtin/memory.go:115 (MemorySearchTool) + :740
  (MemoryRecallTool) - read-filter sites.
- internal/agent/registry.go (modelRefForAgent area) - precedent for spec
  field resolution precedence chains.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// AgentSpec/employee definition:
MemoryMaxScope string `json:"memory_max_scope,omitempty"` // "", "project", "user", "global"
// Resolution: empty -> ScopeProject. Invalid value -> Warn log + ScopeProject.
func resolveScopeInputs(...) -> (maxScope memory.Scope, projectID, userID string)
// Precedence per contract B1 (../orchestrator.md): agent MemoryMaxScope;
// projectID = session's bound project; userID = multiuser identity or "".
// Unbound session + maxScope=project => project-scoped rows match NOTHING;
// global + own-user rows still recallable. Never os.Getwd() fallback.
```

### What This Leaf Consumes

- C1/C2 (01-scope-schema.md, COMPLETE): Scope, WithScopeFilter.
- C3 from ../../master.md.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | MemoryMaxScope field + resolution on agent specs | Modify: internal/agent/registry.go (+ employee definition struct site); Test: gate pins | `go test -p 2 ./internal/agent/ -run TestResolveScopeInputs` | field/func undefined | empty=project; "global" honored; invalid warns + project | Given specs with "", "user", "bogus"; When resolved; Then project, user, project+Warn |
| T2 | Loop recall threads WithScopeFilter | Modify: internal/agent/loop.go (recall site); Test: gate pins | `go test -p 2 ./internal/agent/ -run TestScope` | filter absent | every recall passes resolveScopeInputs results; unbound+project recalls only global | Given session bound to project A with maxScope=project; When recall runs; Then filter carries (project, A, "") |
| T3 | Memory tools stamp + filter | Modify: internal/tools/builtin/memory.go | `go test -p 2 ./internal/tools/builtin/ -run TestMemoryScope` | tools scope-blind | memory_store/memory_retain stamp session scope (explicit arg can widen to maxScope); search/recall filter | Given memory_store in project A session; When stored; Then row scope=project; Given explicit scope=global arg within maxScope; Then stored global |
| T4 | memory_get_context path filtered | Modify: internal/tools/builtin/memory.go (MemoryGetContextTool) | `go test -p 2 ./internal/tools/builtin/ -run TestMemoryScope` | unfiltered | same filter as search | Given project filter; When get_context; Then only matching rows |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] Default behavior change is EXACTLY the isolation: default agents stop
      seeing other projects' facts; global rows still visible everywhere
- [ ] ScopedMemoryManager (bot_id) untouched
- [ ] No os.Getwd() fallback anywhere in the resolution chain
- [ ] All files at exact specified paths; gofmt clean; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify command]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Gate tests GREEN, untouched
- [ ] B1 precedence implemented exactly; invalid values fail SAFE (project,
      not global)
- [ ] Write-stamp + read-filter on ALL four memory tools (store/retain/search/
      recall) + get_context
- [ ] Existing dispatched-loop recall paths covered (registry loops use the
      same resolution - three-site rule: template loop, registry loops,
      verifier children)
- [ ] No debug artifacts; gofmt clean

Output: APPROVED or specific gaps with file + line references.

## Notes

- Registry-loop rule (wave-proven): any recall wiring on the template loop
  must also reach RegistryConfig-driven loops or dispatched turns stay
  unfiltered while appearing wired. Grep the registry construction path.
- The e2e memory-scoping suite (parent gate) flips GREEN when this leaf
  lands - the orchestrator runs it as this leaf's live verification.
- Bench-harness note: report (do not implement) whether e2e/bench harnesses
  plant markers that should become project-scoped after this lands.
