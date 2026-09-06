# 04-seal-wiring.md — Seal + compile wiring: brainstorm loop end to end (TDD leaf)

## Meta

- **Parent:** ../master.md
- **Scope:** Wire the brainstorm draft lifecycle (draft on task metadata,
  interview-path gating), the plan.seal RPC, the meept plan CLI
  subcommands, the config key, and daemon wiring — connecting the
  compiler and emitter to the live orchestrator.
- **Dependencies:** 01-dialect-spec.md (template + grammar),
  02-compiler.md (CompileSealed), 03-tree-emission.md (EmitTree +
  ShouldEmitTree) — all committed before this leaf dispatches
- **Estimated Context:** ~80K
- **Concurrency Group:** C

## Goal

Turn the pieces into the user-facing pipeline: a task in brainstorm mode
carries a markdown draft; the user and planner agent iterate; `meept
plan seal <task-id>` seals + compiles; on success the plan persists
through the EXISTING persistence path (writer.go / PersistPlan) and the
task transitions to executing; on compile problems the draft stays
draft and the problems return to the user for the next round. The whole
path sits behind `plans.plan_compiler_enabled` (default false) — the
existing JSON spec_plan path stays byte-identical when disabled.

This leaf also retires the interview dead end FOR THE NEW PATH: when
plan_compiler_enabled is true, the one-shot interview
(ConductInterview/awaitInterviewAnswers, strategic.go:397-406) does not
run — the brainstorm draft IS the interview. The JSON path keeps the
interview unchanged.

## Context

Wiring precedents to mirror:
- internal/rpc/task_approval.go (commit 286e2dda) — the direct-RPC
  handler shape with an injected func field; mirror its structure
- internal/daemon/daemon.go SetParallelPhases site — where the config
  flag reaches components, before Start
- internal/config/schema.go PlansConfig — ParallelPhases bool with
  json+toml tags; add the sibling key
- internal/plan/writer.go WritePlanMarkdown / ParsePlanContent — the
  persisted-format round trip; the seal path persists via these
- internal/agent/strategic.go:397-406 — the interview branch to gate
- cmd/meept/plan.go — existing plans CLI (list/show/approve/...); the
  new subcommands join this command tree
- internal/rpc/plan.go — existing plan RPC methods (plan.approve etc.);
  plan.seal must NOT collide (different object: task draft)

Key constraint (AGENTS.md wiring rule): every feature ships with an
interface. This leaf IS the interface leaf — no data-structure-only
delivery.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
1. Config (internal/config/schema.go):
   PlansConfig.PlanCompilerEnabled bool
     json:"plan_compiler_enabled" toml:"plan_compiler_enabled"
     doc comment: "Draft→seal→compile planning pipeline (default false;
     legacy JSON spec_plan path used when false)."
   Validate(): no new constraints.

2. Draft store (internal/agent/plan_draft.go, package agent):
   func (sp *StrategicPlanner) SetPlanCompilerEnabled(enabled bool)
       // nil-guarded setter convention
   func (sp *StrategicPlanner) DraftFor(taskID string) (*PlanDraft, bool)
   func (sp *StrategicPlanner) SaveDraft(taskID, markdown string) error
       // stamps version + updated_at; refuses when task not in
       // planning state
   type PlanDraft struct {
       Markdown  string    `json:"markdown"`
       Version   int       `json:"version"`
       UpdatedAt time.Time `json:"updated_at"`
       SealedHash string   `json:"sealed_hash,omitempty"`
   }
   Storage: task.Metadata["plan_draft"] (JSON) — same metadata-bag
   pattern as PlanningContext.
   Interview gate: in the Plan() mode=="plan" branch
   (strategic.go:397-406), when sp.planCompilerEnabled: seed the draft
   (scaffold via config/prompts/planner/plan_draft.md rendered with
   the request) and return WITHOUT calling ConductInterview. The
   brainstorm turns themselves ride the existing chat/planner-agent
   conversation; SaveDraft is invoked by the planner agent loop
   through a new tool OR by chat turn hook — MINIMAL PATH: expose
   SaveDraft/DraftFor through the existing memory_rpc-free direct
   handler "plan.draft" {task_id, markdown?} (get or save) so the CLI
   and TUI can drive turns without new agent-loop plumbing. Note the
   deviation if you choose a different draft-edit transport.

3. Seal RPC (internal/rpc/plan_seal.go, package rpc):
   type PlanSealHandler struct — injected funcs (nil-guarded):
       DraftFor func(taskID) (*agent-shaped-draft, bool)   — or a
         narrow interface; rpc CANNOT import agent (cycle). Define:
         type DraftSource interface {
             DraftFor(taskID string) (markdown string, sealedHash string, ok bool)
             SealDraft(taskID string, hash string) error  // stamps hash
         }
       Compile func(markdown string, maxPhases int) (phases any, hash string, warnings []string, problems []CompileProblemView, err error)
       Persist func(taskID string, phases any, tree *EmittedTreeView) error
       Execute  func(taskID string) error  // existing schedule path
   Registered methods:
     "plan.seal"  {task_id} →
       draft,ok := DraftFor; !ok ⇒ error "no draft for task"
       problems := Compile(draft.Markdown, maxPhases)
       len(problems)>0 ⇒ return {status:"problems", problems} (HTTP
         200-shaped RPC result, NOT an error — the CLI prints them)
       tree? := ShouldEmitTree gate; if tree: EmitTree → Persist(tree)
         else Persist(flat phases)
       SealDraft(hash); Execute(taskID)
       → {status:"sealed", hash, mode:"tree"|"flat"}
     "plan.draft" {task_id, markdown?} →
       markdown present ⇒ SaveDraft → {status:"saved", version}
       absent ⇒ {status:"draft", markdown, version}
   Wiring (internal/daemon/daemon.go, next to SetParallelPhases site):
     when fullCfg.Plans.PlanCompilerEnabled:
       handler funcs closed over strategicPlanner + compile/persist;
       RegisterPlanSealMethods(rpcServer); log line
       "Plan compiler pipeline enabled".

4. CLI (cmd/meept/plan.go, new subcommands on the existing plan cmd):
   meept plan draft <task-id>            → plan.draft get; prints status
   meept plan show <task-id>             → prints draft markdown
   meept plan edit <task-id> [file]      → file or "-" stdin → plan.draft save
   meept plan seal <task-id>             → plan.seal; prints sealed hash
                                            + mode, or the problem list
   Transport: client.Call (low-level extensibility call, existing).

5. Persist path (inside handler closure or a small agent helper):
   flat mode: PhaseSpec[] → agent.PlanPhaseSpec[] (field-parity map,
     round-trip pinned by leaf 02's test) → existing PersistPlan/
     strategic persist steps path (mirror what ApprovePlan does with
     pending steps — persist steps + set executing)
   tree mode: write root + leaves under
     <data_dir>/plan-trees/<task-id>/ via existing writer.go where
     applicable; persist phases as ABOVE (the orchestrator still runs
     phases; the tree is the human/leaf-agent artifact) — the tree
     leaves' paths go into each phase's metadata for the worktree/
     dispatch layer to pick up later (integration point documented,
     consumed-by later tree; do NOT build the leaf-agent dispatcher).

6. Tests (new files alongside each):
   - config: key round-trip (json5 + toml), default false
   - rpc: seal happy path (flat), problems path (stays draft),
     no-draft error, tree mode gate → EmitTree called
   - agent: interview gate — planCompilerEnabled ⇒ ConductInterview
     not called; disabled ⇒ unchanged (existing tests prove)
   - CLI: cobra wiring smoke (command exists, args enforced)
   - Integration: end-to-end in daemon test — draft → seal(flat) →
     task executing, phases persisted (mirror
     internal/daemon/orchestrator_wiring_test.go fixture style)

7. Docs:
   - docs/workflows/agent-orchestration.md: new section "Plan compiler
     pipeline (plans.plan_compiler_enabled)" — draft lifecycle, seal,
     compile problems, tree vs flat, interview gating.
   - AGENTS.md: one bullet under Critical Invariants —
     "Plan compiler pipeline is per-config opt-in (default false);
     when false the JSON spec_plan path is byte-identical; plan.seal
     is RPC-only, never a bus topic." + package table unchanged.
```

### What This Leaf Consumes

```
From 02-compiler.md: CompileSealed, CompiledPlan, CompileProblem,
  CompileError (package plan)
From 03-tree-emission.md: EmitTree, ShouldEmitTree, EmittedTree,
  TreeEmitOptions (package plan)
From 01-dialect-spec.md: config/prompts/planner/plan_draft.md template
  path (render scaffold at draft creation)
From repo: all wiring precedents listed under Context
```

## Tasks

### Task 1: Config key + PlansConfig test

**Objective:** plans.plan_compiler_enabled lands with tags + tests.

**Files:**
- Modify: `internal/config/schema.go` (PlansConfig)
- Test: `internal/config/plans_config_test.go` (exists — extend)

**Step 1: Write failing test** — parse `plan_compiler_enabled = true`
(toml) + json5 key; default false.
**Step 2: verify fail** → **Step 3: implement** → **Step 4: verify pass**

Run: `go test -p 2 ./internal/config/ -run Plans -v`

### Task 2: Draft store + interview gate

**Objective:** PlanDraft metadata bag + SetPlanCompilerEnabled + the
Plan() branch gating; planner conversation untouched otherwise.

**Files:**
- Create: `internal/agent/plan_draft.go`
- Modify: `internal/agent/strategic.go` (setter field + gate at the
  interview branch ONLY — surgical, no other changes)
- Test: `internal/agent/plan_draft_test.go`

**Step 1: failing tests** — gate on/off; SaveDraft version stamping;
DraftFor miss; SaveDraft refuses non-planning state.
**Step 2: verify fail** → **Step 3: implement** → **Step 4: verify pass**

Run: `go test -p 2 -race ./internal/agent/ -run 'PlanDraft|PlanCompiler' -v`

### Task 3: plan.seal + plan.draft RPC

**Objective:** Handler with injected interfaces (no rpc→agent import),
both methods registered, daemon wiring behind the flag.

**Files:**
- Create: `internal/rpc/plan_seal.go`, `internal/rpc/plan_seal_test.go`
- Modify: `internal/daemon/daemon.go` (wiring block beside
  SetParallelPhases; closure adapters rpc-interface → plan/agent funcs)

**Step 1: failing tests** — happy flat seal; problems reply (not error);
no-draft; tree gate calls EmitTree; SealDraft stamps hash.
**Step 2: verify fail** → **Step 3: implement** → **Step 4: verify pass**

Run: `go test -p 2 ./internal/rpc/ -run 'PlanSeal|PlanDraft' -v`

### Task 4: CLI subcommands + integration test + docs

**Objective:** The four `meept plan` subcommands; end-to-end daemon test;
docs + AGENTS.md.

**Files:**
- Modify: `cmd/meept/plan.go`
- Test: `internal/daemon/plan_seal_wiring_test.go` (new; fixture style
  of orchestrator_wiring_test.go: real orchestrator + bus + queue;
  flag on ⇒ draft→seal→executing; flag off ⇒ interview/JSON path)
- Modify: `docs/workflows/agent-orchestration.md`, `AGENTS.md`

**Step 1: failing integration test** — the flag-on path persists 2
phases and reaches executing from a sealed 2-phase draft; the flag-off
path leaves the interview branch intact (assert via existing
behavior, no regression).
**Step 2: verify fail** → **Step 3: implement CLI + docs** →
**Step 4: verify pass**

Run:
`go test -p 2 ./internal/daemon/ -run PlanSeal -v`
`go test -p 2 ./internal/config/ ./internal/agent/ ./internal/rpc/ ./internal/plan/`
`go test -race -p 2 ./internal/plan/`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Config key default false; flag-off path untouched (existing JSON
      tests still green, interview branch intact when disabled)
- [ ] rpc imports neither agent nor daemon (interface injection only)
- [ ] plan.seal problems are a RESULT, not an error (CLI prints list)
- [ ] Four CLI subcommands wired through client.Call
- [ ] Integration test: flag on → draft→seal→executing; flag off → old path
- [ ] Docs section + AGENTS.md invariant bullet present
- [ ] No new bus topics; `make graphs` output gains plan.seal/plan.draft
- [ ] `go build ./internal/... ./cmd/meept` green (repo-wide cmd build
      may fail on the KNOWN stray cmd/skillparse_main.go — ignore that
      file only)

**DO NOT COMMIT.** The orchestrator handles git after review.

**Deviations from spec:** [none / list with rationale — especially any
draft-edit transport choice different from plan.draft RPC]

## Review Checklist (For Review Agent)

- [ ] Wiring site matches the SetParallelPhases precedent (before Start)
- [ ] Handler funcs nil-guarded; no typed-nil interface hazards
- [ ] Seal path persists via EXISTING writer/persist code — no new
      persistence format
- [ ] Interview gate is surgical: one branch, flag-guarded
- [ ] Docs + AGENTS.md updated in the same change (AGENTS.md rule)
- [ ] `-p 2` everywhere; race on plan package

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The rpc→agent boundary: PlanSealHandler takes interfaces; daemon.go
  closures adapt. Do NOT move PlanDraft into package rpc.
- Tree mode writes the tree files but does NOT wire a leaf-agent
  dispatcher — dispatching tree leaves through employee/delegation is
  a FOLLOW-UP tree. The integration point (phase metadata carrying
  leaf paths) is the contract for that future work; document it in the
  orchestration doc's new section.
- maxPhases for CompileSealed comes from the strategic planner's
  existing MaxPlanSteps config sibling (default 10) — reuse, do not
  add a second knob.
- The draft scaffold render: minimal — fill Goal + Open Questions from
  the task description; do not invoke an LLM for the scaffold.
