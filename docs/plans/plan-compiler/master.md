# master.md — Plan Compiler: Brainstorm Dialect → Sealed Plan → Deterministic PlanPhaseSpec

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 leaves (01–04)
- **Scope:** Replace LLM-generated strict-JSON phase planning with a
  human-in-the-loop markdown brainstorm dialect that compiles
  deterministically into the orchestrator's PlanPhaseSpec.

> **For the executing agent:** You are the orchestrator for this tree.
> Dispatch leaf agents per the Dispatch Protocol, review their work
> in-session, commit per-leaf after review, track completion. Do NOT
> implement code yourself.

## Goal

Today `spec_plan` mode asks an LLM to emit strict JSON
(`config/prompts/planner/decompose_spec.md`: "Output ONLY valid JSON") and
compensates with a 4-layer defense: `ExtractJSON` → unmarshal → repair
(drop empty phases, fix invalid kinds, remap depends_on indices) →
validate (internal/agent/strategic.go:1527 `parsePhaseOutput`). Live runs
(2026-09-06) still fail: `cannot unmarshal string into depends_on.0`,
`no JSON found in phase planner output`, multi-turn budget exhaustion on
8B-class models, and Agnes 400s on malformed continuation messages.

The user wants a different pipeline:

1. **Brainstorm (human-readable markdown).** The planner agent and the
   user iterate on ONE living plan document — goals, decisions, open
   questions, phases described in prose with named artifacts — until the
   user seals it. This replaces the one-shot interview, which is
   currently a dead end (`task.interview` at strategic.go:613 has no
   external responder; `PlanningContext.InterviewAnswers` exists in
   internal/plan/plan.go:122 but nothing writes it).
2. **Seal.** The user marks the draft final. The sealed document becomes
   immutable input (hash recorded).
3. **Compile (zero LLM).** A deterministic Go compiler parses the sealed
   markdown into `[]plan.PlanPhaseSpec` — named produces/consumes
   artifacts (NOT numeric indices), cross-phase step deps, steps with
   tool hints. Compile errors are readable plan problems that feed
   another brainstorm round instead of an LLM retry burn.

Dialect inputs (researched 2026-09-06): brainstorm stage follows the
OpenAI Codex ExecPlan/PLANS.md living-document pattern (Progress /
Decision Log / Open Questions sections, prose-first); orchestrator stage
keeps the existing PlanPhaseSpec shape; dependency structure inside the
markdown uses GSD-style explicit waves where needed. The compilation
itself has no external standard — it is the novel piece.

## Relationship to the existing plan tree format (hierarchical-planning)

The seal/compile output lands in `[]plan.PlanPhaseSpec` which the
orchestrator executes as phases with named artifacts. When a phase's
scope exceeds one leaf agent's 128K context (per the hierarchical-planning
skill), the compile step is ALSO the natural point to emit a hierarchical
plan tree (master.md + leaves) instead of flat steps: the dialect's
per-phase "contracts" sections map directly to the master's frozen
Interface Contracts, and per-phase work items map to leaves. Leaf 03
defines this mapping and the gate that decides flat-steps vs tree
emission. This keeps agent scope-of-work constrained per implementation
agent and gives clean segregation lines — the user explicitly wants the
compiled plan to be expressible in the same tree/branch/leaf format used
by the hierarchical-planning skill.

## Architecture Overview

```
 user ◄──── brainstorm turns ────► planner agent
   │            (markdown draft on task metadata, edit/refine)
   │
   │  meept plan seal <task-id>
   ▼
 sealed draft (immutable, hash recorded on task)
   │
   ▼
 plan.CompileSealed(markdown) → []plan.PlanPhaseSpec   ← NEW, pure Go
   │        errors = readable plan problems (unknown artifact,
   │        unsatisfiable consume, cycle) → back to brainstorm
   ▼
 existing orchestrator: PersistPlan → phases → frontier dispatch
   │                     (plans.parallel_phases / worktrees / budget)
   └── when phase too big for one leaf → hierarchical tree emission
       (leaf 03) consumed by the orchestrator hook path
```

## Interface Contracts (frozen)

### Contract A: plan dialect v1 (leaf 01, docs only)

```
File:    docs/workflows/plan-dialect.md (NEW) — normative spec of the
         brainstorm dialect v1. Also mirrored as the agent-facing
         template at config/prompts/planner/plan_draft.md (NEW).
Document shape (all sections required unless marked optional):
  # Plan: <title>
  ## Meta            — YAML block: task_id, version, status
                       (draft|sealed), updated
  ## Goal            — 1-3 sentences, user-visible outcome
  ## Decisions       — bullet list: "Decision: X — Rationale: Y"
  ## Open Questions  — bullet list; MUST be empty to seal
  ## Phases          — one "### Phase N: <name>" per phase; each has:
    - prose intent paragraph (1-3 sentences)
    - "**Produces:**" bullet list of named artifacts
      `- <name> (<kind>) — <one-line description>` where kind ∈
      {file, interface, schema, decision, test_suite}
    - "**Consumes:**" same shape (may be "none")
    - optional "**Depends on:** Phases N, M" (omit when derivable
      from consumes; compiler fills it)
    - "**Steps:**" numbered list `1. <description> [tool_hint]
      (needs: <artifact-or-step-ref>)`; needs references artifacts
      BY NAME or prior steps as "PhaseN.S<step#>"
  ## Notes (optional)
Rules:
  - Artifact names are kebab-case, unique across the whole plan;
    consume names MUST match a produce name of an earlier phase.
  - Open Questions non-empty ⇒ seal refuses (exit code 2).
  - tool_hint ∈ existing hints {code, refactor, debug, fix, analyze,
    research, git, plan, chat, bash}.
  - No JSON anywhere. No numeric cross-phase indices.
```

### Contract B: compiler (leaf 02)

```
File:     internal/plan/compiler.go (NEW; package plan) +
          internal/plan/compiler_test.go
Pure:     no I/O beyond input string; no LLM; no clocks.
API:
  // CompileSealed parses dialect-v1 markdown into phase specs.
  // Returns all problems (not just the first) so one brainstorm round
  // can fix everything. problems have line numbers + human text.
  func CompileSealed(markdown string, maxPhases int) (*CompiledPlan, error)
  type CompiledPlan struct {
      Phases  []PlanPhaseSpec   // existing struct, agent package type alias
      Hash    string            // sha256 of the sealed markdown
      Warnings []string         // non-fatal notes (e.g. depends_on inferred)
  }
  // CompileProblem carries line + message for the CLI to print.
  type CompileProblem struct { Line int; Message string }
Validation (hard errors):
  - Open Questions section present and non-empty
  - unknown artifact kind; unknown consume name; consume before produce
  - step needs: reference to nonexistent artifact/step
  - phase count > maxPhases; duplicate artifact name; empty phase
  - dependency cycle among phases (via consumed artifacts)
Behavior:
  - fills DependsOn from consumes when omitted
  - keeps steps' tool hints verbatim; unknown hint ⇒ error
  - artifact Required=true when a later phase consumes it
Error type: *CompileError { Problems []CompileProblem } so callers
  render "plan compile failed: N problems" + list (feeds brainstorm).
```

### Contract C: seal + compile wiring (leaf 04)

```
Files:   internal/agent/plan_draft.go (NEW, package agent) —
           SetPlanDraftEnabled / brainstorm session helpers:
           draft lives on task.Metadata under "plan_draft" key
           {markdown, version, updated_at}; brainstorm turns append
           via the planner agent loop using the leaf-01 template;
         internal/rpc/plan_seal.go (NEW) — direct handler
           "plan.seal" {task_id} → seals draft (hash), runs
           CompileSealed, on success persists phases via the existing
           PersistPlan path and transitions task → executing (reuses
           the task.approve plumbing shape from 286e2dda);
           on CompileError returns problems to the caller (draft
           stays draft; reply carries problems for the next round).
         cmd/meept/plan.go — subcommands:
           meept plan draft <task-id>            status of draft
           meept plan show <task-id>             print draft markdown
           meept plan edit <task-id> [file]      replace draft content
           meept plan seal <task-id>             seal + compile + execute
         Config: plans.plan_compiler_enabled (default false; JSON5 tag
           parallel to plans.parallel_phases; wiring in daemon.go next
           to the SetParallelPhases site).
Constraints:
  - When disabled, existing spec_plan JSON path is untouched.
  - plan.seal is a NEW RPC; it must NOT collide with plan.approve
    (existing, plan-lifecycle) — different object (task draft).
  - task.interview dead end: leaf 04 disables the interview prompt
    path when plan_compiler_enabled is true (draft IS the interview).
```

### Contract D: tree emission (leaf 03)

```
File:     internal/plan/treeemit.go (NEW; package plan) +
          internal/plan/treeemit_test.go
Pure function over CompiledPlan:
  func EmitTree(cp *CompiledPlan, opts TreeEmitOptions) (*EmittedTree, error)
  type TreeEmitOptions struct {
      MaxLeavesPerPhase int     // default 3
      LeafCharBudget    int     // default 128_000-token equivalent (~14KB text)
  }
  type EmittedTree struct {
      Root   string   // master.md content (hierarchical-planning template)
      Leaves []LeafFile { Path, Content string }
  }
Decision gate (flat vs tree):
  - total steps ≤ 6 AND single phase ⇒ flat steps (existing path)
  - any phase with steps > MaxLeavesPerPhase*steps-per-leaf, or any
    phase whose prose intent exceeds LeafCharBudget ⇒ tree
Tree shape (mirrors hierarchical-planning skill exactly):
  - master.md: Meta/Goal/Architecture/Interface Contracts (frozen from
    each phase's produces/consumes blocks)/Child Index (leaves numbered,
    concurrency groups derived from consumes)/Dispatch Protocol/Review
    Checklist/Coding Conventions/Completion Tracking Table/Integration
    Test Plan — ALL required sections per the skill's compliance scan
  - leaf per work item: TDD task skeleton, contracts-from-parent
    section inlined, "Do NOT commit" rule
Guarantees:
  - emitted tree passes check_template_compliance.py --strict-leaves
    (leaf 03 test asserts section presence programmatically)
  - artifact produce/consume map 1:1 onto master Interface Contracts
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-dialect-spec.md | leaf | none | ~40K | A |
| 02 | 02-compiler.md | leaf | 01 (dialect spec) | ~70K | B |
| 03 | 03-tree-emission.md | leaf | 02 (CompiledPlan type) | ~60K | B |
| 04 | 04-seal-wiring.md | leaf | 01, 02 | ~80K | C |

## Dispatch Protocol

### Phase 1: Dispatch leaf 01 (concurrency group A)

1. **Read** 01-dialect-spec.md and dispatch via `delegate_task`:
   - Goal: "Author the plan-dialect v1 specification and template per the leaf"
   - Context: full leaf text + Contract A + the Codex ExecPlan reference
     summary + existing prompts (config/prompts/planner/*.md) INLINED
   - Include: "Do NOT commit. Do NOT run git add."
   - Docs leaf: no Go code; two markdown files only.

### Phase 2: Dispatch leaves 02 + 03 in parallel (concurrency group B)

After leaf 01 review passes, dispatch 02 and 03 together (03 pins
CompiledPlan from Contract B, so it does not need 02's code):

1. **Read** 02-compiler.md → dispatch: "Implement CompileSealed per the
   leaf. TDD. Do NOT commit." Context: leaf text + Contract B + dialect
   spec (leaf 01 output) + internal/plan/parser.go, artifacts.go INLINED.
2. **Read** 03-tree-emission.md → dispatch: "Implement EmitTree per the
   leaf. TDD. Do NOT commit." Context: leaf text + Contract D + Contract
   B's CompiledPlan/EmitTree signatures + the hierarchical-planning
   master/leaf template section list INLINED.

### Phase 3: Review and commit each child

Per the standard protocol: review in-session, re-dispatch with findings
(max 3), commit per-leaf with explicit paths
(`git add internal/plan/compiler.go internal/plan/compiler_test.go docs/workflows/plan-dialect.md ...`),
update the tracking table.

### Phase 4: Dispatch leaf 04 (concurrency group C)

After 02 + 03 commit: **Read** 04-seal-wiring.md → dispatch: "Wire seal +
compile end to end per the leaf. Do NOT commit." Context: leaf text +
Contract C + Committed compiler API + daemon.go SetParallelPhases wiring
site + task.approve RPC pattern (internal/rpc/task_approval.go) INLINED.

### Phase 5: Integration review

1. End-to-end test: draft → seal → compile → phases persisted →
   task executing (leaf 04's integration test must exist; run it).
2. `go test ./internal/plan/ ./internal/agent/ ./internal/rpc/ -p 2`.
3. Emitted-tree compliance: run leaf 03's section-presence test.
4. Flag-off equivalence: with plans.plan_compiler_enabled false, run one
   spec_plan task — JSON path byte-identical behavior.
5. Docs: update docs/workflows/agent-orchestration.md (new section:
   "Plan compiler pipeline") + AGENTS.md invariant note (draft/seal
   state machine + plan_compiler_enabled default).

## Review Checklist (root)

- [ ] Leaf 01: dialect has NO JSON, NO numeric cross-phase indices; all
      rules from Contract A present; Open-Questions gate documented
- [ ] Leaf 02: CompileSealed returns ALL problems not first-only;
      depends_on inference; cycle detection; sha256 of sealed input
- [ ] Leaf 03: emitted master.md contains ALL hierarchical-planning
      required sections; flat-vs-tree gate implemented
- [ ] Leaf 04: plan.seal RPC + 4 CLI subcommands; config default false;
      JSON path untouched when disabled; interview path disabled when
      enabled
- [ ] All leaves: tests green under `-p 2`, race on compiler tests
- [ ] No changes to parsePhaseOutput / existing JSON planner code paths
- [ ] No new bus topics (plan.seal is RPC only)

## Coding Conventions

- **Language:** Go (repo module github.com/caimlas/meept), Go 1.22+
- **Style:** gofmt; errors wrapped `fmt.Errorf("context: %w", err)`;
  no ignored errors in non-test code; nil-guarded setters
- **Testing:** stdlib tests + testify where already in use; table-driven;
  `-p 2` ALWAYS (macOS ephemeral-port exhaustion)
- **Pure functions:** compiler and emitter take strings/structs in,
  return structs out; no I/O, no clocks, no globals
- **JSON5 config:** follow schema.go patterns for new config keys
- **Docs:** lowercase UI strings; ASCII diagrams only

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-dialect-spec.md | COMPLETE | 1 | Committed 64682470; judgment calls: same-phase step refs allowed (persisted intra-phase), cycle class documented unreachable-but-implemented |
| 02-compiler.md | PENDING | 0 | |
| 03-tree-emission.md | PENDING | 0 | |
| 04-seal-wiring.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go test -p 2 ./internal/plan/ ./internal/agent/ ./internal/rpc/ ./internal/config/ ./internal/daemon/`
2. `go test -race -p 2 ./internal/plan/ -run CompileSealed`
3. Leaf 03 compliance assertion: `go test ./internal/plan/ -run EmitTree`
4. E2E (manual, flag on): create task with brainstorm request → draft
   appears → `meept plan show` → `meept plan seal` → task executing with
   phases matching draft → frontier log lines appear.
5. E2E (flag off): existing spec_plan task still uses JSON path.
6. `make graphs` regenerates with plan.seal RPC entry.

## Notes

- The existing one-shot interview (strategic.go ConductInterview /
  awaitInterviewAnswers) stays for the JSON path; leaf 04 only gates it
  off under plan_compiler_enabled. Do NOT delete it in this tree.
- parser.go's markdown plan format (## Phase N: name [state], `N. step
  [status] (depends: M)`) is the PERSISTED format — the brainstorm
  dialect is a SUPERSET designed to round-trip into it; leaf 01 must
  define the mapping and leaf 04 persists via existing writer.go.
- CompiledPlan.Hash seeds the audit trail (sealed-at hash on task
  metadata) — feed the tamper-evident audit chain, not a new log.
- Scratch: check_template_compliance.py lives in the Hermes skill dir,
  NOT this repo — leaf 03 must implement its own section-presence check
  in Go for tests.
