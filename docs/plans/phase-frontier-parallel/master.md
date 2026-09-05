# master.md — Phase-Frontier Parallel Dispatch (plans.parallel_phases)

## Goal

Replace strictly-serial plan-phase execution with full ready-frontier
parallel dispatch across phases. Today the within-task step layer already
runs a parallel ready frontier — `ScheduleReadySteps`
(internal/agent/tactical.go:291), `PromoteReadySteps`
(internal/task/step.go:714), per-agent semaphores via `acquireSlots`
(internal/agent/tactical.go:478), limits `MaxConcurrentJobs=10` /
`MaxConcurrentPerAgent=3` (tactical.go:236-243). **Phases are the serial
bottleneck**: `maybeTransitionPhase` (internal/agent/orchestrator.go:367)
waits for `IsPhaseComplete` on the WHOLE phase, then `startNextPhase`
(internal/agent/orchestrator_phases.go:17) picks the NEXT PHASE IN LIST
ORDER (lines 27-41), never a computed frontier.

Meanwhile the data for frontier edges already exists: `PlanPhase` records
carry `Produces`/`Consumes` (internal/plan/plan.go:78-79, JSON columns via
internal/plan/store_sqlite.go:349-436) and `checkPhaseReady`
(orchestrator_phases.go:52-56, implemented at strategic.go:1617) already
gates phase start on artifact readiness. The scheduler just ignores it.

Everything behind an opt-in flag: `plans.parallel_phases` (default false —
serial behavior preserved byte-for-byte until validated).

## Architecture Overview

```
                       ┌──────────────────────────────────────────────┐
 step terminal event   │ handleJobCompleted (orchestrator.go:316)     │
 ─────────────────────►│   maybeTransitionPhase (orchestrator.go:367) │
                       │     ├─ flag OFF → legacy path (unchanged):   │
                       │     │    IsPhaseComplete → startNextPhase    │
                       │     │    (next-in-list-order)                │
                       │     └─ flag ON  → advancePhasesFrontier      │
                       │          (new, orchestrator_frontier.go)     │
                       └──────────────┬───────────────────────────────┘
                                      │ loads phases + steps + specs
                                      ▼
                       ┌──────────────────────────────────────────────┐
                       │ ComputePhaseFrontier (NEW pure function,     │
                       │ internal/agent/phase_frontier.go)            │
                       │  inputs: []PhaseNode (Produces/Consumes/     │
                       │  cross-phase step deps), artifact store      │
                       │  snapshot, busyPhases set                    │
                       │  output: ready phases (Sequence-ordered),    │
                       │  cycleDetected flag                          │
                       └──────────────┬───────────────────────────────┘
                                      │ for each ready phase
                                      ▼
                       ┌──────────────────────────────────────────────┐
                       │ startPhase (extracted from startNextPhase,   │
                       │ orchestrator_phases.go): checkPhaseReady     │
                       │ gate → renderPhaseStartup → fresh            │
                       │ conversationIDs (phase-<phaseID>-<stepID>,   │
                       │ already phase-scoped) → worktree provisioner │
                       │ hook → onPhaseTransition hook (panic-safe)   │
                       └──────────────────────────────────────────────┘
```

Frontier rule (pinned): a phase is READY when

- **(a)** every Required consume is present in the artifact store snapshot
  (exact `checkPhaseReady` parity — strategic.go:1617; optional consumes
  are best-effort and never block), AND
- **(b)** no phase it transitively depends on still has non-terminal
  steps. Dependency edges are: every consume (required OR optional) whose
  producing phase exists in the graph, plus explicit step `DependsOn`
  edges that cross phase boundaries (step in phase X `DependsOn` step in
  phase Y ⇒ edge X→Y).

List order is a tiebreak/anti-cycle guard ONLY — never a hard barrier.
Multiple phases are ACTIVE simultaneously. Step-level scheduling safety is
untouched (existing per-agent semaphores + queue; no new executor
machinery). Workspace safety: concurrent phases sharing one project get a
per-phase worktree via a new orchestrator provisioning hook; the step
working-dir precedence (`resolveStepWorkingDir`, internal/daemon/
components.go:7240: WorktreePath > ProjectPath > session CWD > "") is the
existing consumption surface.

Four leaves, strictly sequential (each edits files the previous one
touched):

1. **01-frontier-compute** — pure frontier function, new file, table-driven
   tested, NO wiring.
2. **02-store-state** — multiple ACTIVE phases: rewire
   maybeTransitionPhase/startNextPhase to the frontier behind the flag;
   serial preserved when flag=false (table test proves flag-off ==
   today's list-order behavior).
3. **03-hooks-budget** — frontier-aware per-phase-start hook wiring,
   per-phase worktree provisioning hook, single-slot phase-state survey
   + fix (BudgetHierarchy.currentPhase).
4. **04-surface-docs** — config key in internal/config schema, daemon
   wiring, docs/workflows/agent-orchestration.md phase section, AGENTS.md
   invariant note.

Bus events: REUSE existing `plan.phase_completed` (internal/plan/
manager.go:587) and `task.progress` topics. Phase-START observability
rides the existing `onPhaseTransition` hook (orchestrator.go:53-56,
SetPhaseTransitionHook :89-96) — **no new bus topics**.

## Interface Contracts (frozen)

### Contract A: frontier function (leaf 01)

```
File:        internal/agent/phase_frontier.go (NEW; package agent)
Pure:        no I/O, no locks, no clocks — fully table-testable

type PhaseNode struct {
    Name           string     // plan.PlanPhase.Name — the join key
    Sequence       int        // list order; output tiebreak + anti-cycle escape ONLY
    Produces       []string   // artifact names this phase declares
    Consumes       []Artifact // full declarations (agent.Artifact = plan.Artifact alias)
    DependsOnPhase []string   // resolved cross-phase step DependsOn (phase names)
}

func ComputePhaseFrontier(
    nodes []PhaseNode,              // the INCOMPLETE phase set only
    availableArtifacts map[string]bool, // artifact-store snapshot, by name
    busyPhases map[string]bool,     // phases with >=1 non-successfully-terminal step
) (ready []PhaseNode, cycleDetected bool)
```

Semantics:
- ready = (a) every `Required` consume present in `availableArtifacts`
  (optional consumes never block — checkPhaseReady parity) AND (b) no
  dependency edge into a `busyPhases` member (edges from ALL consumes —
  required and optional — that some in-graph phase `Produces`, plus every
  `DependsOnPhase` entry; a phase consuming its own artifact
  (`producer == Name`) creates no self-edge).
- `ready` is sorted by `Sequence` ascending (the list-order tiebreak).
- `cycleDetected == true` iff `len(ready) == 0 && len(nodes) > 0`
  (nodes is the incomplete set, so an empty ready set means the remaining
  graph cannot advance). Policy for that case is the CALLER's (Contract B),
  not this function's.
- Declared-but-never-produced Required consumes create no edge; the store
  check in (a) still gates them at start time (matches today's
  startNextPhase behavior — no new deadlock mode).

### Contract B: frontier dispatch (leaf 02)

```
File:        internal/agent/orchestrator_frontier.go (NEW)
             internal/agent/orchestrator.go (flag field + setter + dispatch)
             internal/agent/orchestrator_phases.go (startPhase extraction)

Setter:      func (o *Orchestrator) SetParallelPhases(enabled bool)
Field:       parallelPhases bool (zero value false — serial default)

Dispatch:    maybeTransitionPhase (orchestrator.go:367) becomes:
               if !o.parallelPhases { ...existing body, byte-identical... }
               o.advancePhasesFrontier(ctx, taskID)

New:         func (o *Orchestrator) advancePhasesFrontier(ctx, taskID string)
             - non-plan task (GetPhasesByTask -> nil, nil): return
             - nodes: phases where !IsPhaseComplete (task/step.go:1463
               predicate: all steps IsSuccessfullyTerminal)
             - busyPhases: >=1 step not IsSuccessfullyTerminal
             - availableArtifacts: snapshot of o.artifacts
               (artifactStore.Get/Has; add tiny snapshotNames() accessor
               to internal/agent/artifacts.go if needed — keep it read-only)
             - DependsOnPhase: for each step.DependsOn entry resolving to a
               step whose Phase differs from the depending step's Phase
             - per-task single-flight WITHOUT holding a lock across I/O:
               `inflight sync.Map` taskID -> *atomic.Bool;
               CompareAndSwap(false, true) guards re-entry; defer Store(false).
               (mutexio: never hold o's mutex across store calls.)
             - for each ready node (Sequence order): find phase by name ->
               startPhase(...). cycleDetected -> Warn log + legacy serial
               fallback (list-order next incomplete phase) = anti-cycle
               escape; never busy-loop.

Extraction:  startNextPhase keeps its signature and serial selection
             (lines 27-41); its steps 2-5 (spec load, checkPhaseReady gate,
             renderPhaseStartup, fresh conversationIDs via
             "phase-<phaseID>-<stepID>", onPhaseTransition, log) move to
             new `func (o *Orchestrator) startPhase(ctx, taskID string,
             p *plan.PlanPhase, fromPhase string) error`.
             startNextPhase = list-order pick + startPhase.
             Frontier path = ready set + startPhase (fromPhase = "").
             startPhase defensively skips a phase whose steps are already
             past StepPending (re-entrancy guard for double events).
```

Flag-off equivalence is a REQUIRED table-driven test: identical task/step
seeds, flag off reproduces exactly today's transition sequence (one phase
active at a time, next-in-list order).

### Contract C: config key (leaf 04)

```
File:        internal/config/schema.go — PlansConfig (line ~339)

Field:       ParallelPhases bool
Tags:        json:"parallel_phases"  toml:"parallel_phases"
Key:         plans.parallel_phases
Default:     false (Go zero value; absent key = false; serial preserved)
Validate():  no change (bool needs no validation); doc comment states
             default and that enabling implies plans declare
             Produces/Consumes for meaningful parallelism.

Daemon wiring (leaf 04): internal/daemon/components.go — after the
orchestrator is constructed (and BEFORE Start), call
orchestrator.SetParallelPhases(cfg.Plans.ParallelPhases).
```

### Contract D: worktree provisioner hook (leaf 03)

```
File:        internal/agent/orchestrator.go (field + setter)
             internal/agent/orchestrator_phases.go (invocation in startPhase)

Setter:      func (o *Orchestrator) SetPhaseWorktreeProvisioner(
                 fn func(ctx context.Context, taskID, phaseID, phaseName string) (string, error))
             Nil-guarded per AGENTS.md setter convention (fn != nil).

Invocation:  inside startPhase, AFTER the checkPhaseReady gate and BEFORE
             step stamping; ONLY when o.parallelPhases is true.
             Result cached in `phaseWorktrees sync.Map`
             (key taskID + "\x00" + phaseName -> string) with accessor
             func (o *Orchestrator) PhaseWorktree(taskID, phaseName string) string.

Skip conditions (documented, never fail the phase start):
  - flag off (serial mode: behavior identical to today)
  - provisioner not wired (nil) — log Debug once
  - provisioner returns error — log Warn ("phase continues without
    worktree isolation"), proceed with existing resolution
  - plan/phase performs no file writes — the provisioner itself decides
    (it may return ("", nil) meaning "no worktree needed")

Panic safety: the invocation is wrapped in the SAME panic-safe subscriber
wrapper used by onPhaseTransition (orchestrator_phases.go:80-85):
  func() { defer func() { _ = recover() }(); ... }()
```

### Contract E: per-phase-start hook (leaf 03, already exists — verified)

```
onPhaseTransition func(taskID, fromPhase, toPhase string)  // orchestrator.go:53-56
SetPhaseTransitionHook                                    // orchestrator.go:89-96, nil-guarded

Under frontier mode the hook fires ONCE PER PHASE START inside startPhase.
Frontier-activated phases pass fromPhase == "" (no completed predecessor);
serial mode keeps fromPhase == completed phase name. Subscribers must
tolerate "" — pinned in the leaf-03 daemon-subscriber check.
conversationID stamping "phase-<phaseID>-<stepID>"
(orchestrator_phases.go:70) is already phase-scoped and collision-free
under parallel starts — verified; no change.
```

## Dispatch Protocol

For each child document, in order (01 → 02 → 03 → 04; each leaf edits
files the previous one touched, so no parallel batching):

1. **Dispatch implementation agent** via `delegate_task` with the leaf
   document as context, plus this master's Interface Contracts section,
   plus: "Do NOT commit. Do NOT run git add. Write code, run tests,
   report results only. The orchestrator handles all git operations."
2. **Review in-session** (main model, NOT a delegated reviewer): build,
   run `go test -p 2 ./internal/agent/... ./internal/plan/...`, verify
   Contracts A-E verbatim, check for debug artifacts and stray TODOs,
   verify flag-off equivalence test exists and passes. Re-dispatch with
   specific feedback on gaps (max 3 iterations).
3. **Commit** only after review passes: `git add` the leaf's exact file
   list, commit `feat(agent): <leaf summary> (plan phase-frontier-parallel)`.
4. **Update the Completion Tracking Table** in this file.

AGENTS.md obligations for every leaf: no `_ = fn()` ignored errors, no
bare `panic(err)`, two-value type assertions on `map[string]any`, typed-nil
guards in Set* methods, no I/O under mutex (`mutexio` runs in
`make lint-ci`), no `os.Getwd()` in daemon code, no `time.Now().UnixNano()`
IDs, `-p 2` on any multi-package test run, AGENTS.md updated same-commit
when a new cross-boundary convention lands (leaf 04 owns that edit).

## Child Index

| Doc | Type | Scope | Est. context | Dependencies |
|-----|------|-------|--------------|--------------|
| 01-frontier-compute.md | leaf | pure ComputePhaseFrontier + PhaseNode, table-driven tests, NO wiring | ~60K | none |
| 02-store-state.md | leaf | frontier dispatch behind flag, startPhase extraction, flag-off equivalence tests | ~90K | 01 |
| 03-hooks-budget.md | leaf | worktree provisioner hook, per-phase-start hook verification, single-slot phase-state survey + fix | ~90K | 02 |
| 04-surface-docs.md | leaf | plans.parallel_phases in config schema, daemon wiring, docs + AGENTS.md | ~60K | 02, 03 |
| OPEN-QUESTIONS.md | doc | Q/Rec/Impact forks | — | — |

## Review Checklist (root)

- [ ] Contract A: pure function, no wiring, table-driven tests cover all
      pinned cases (chain, diamond, optional-consume edge, missing
      required consume, self-produce, Sequence tiebreak, cycleDetected)
- [ ] Contract B: flag-off path byte-identical (table test: flag off ==
      today's list-order transitions); flag-on starts multiple phases;
      re-entrancy guard proven under concurrent terminal events
- [ ] Contract C: exact key `plans.parallel_phases`, default false,
      daemon wiring present
- [ ] Contract D: nil-guarded setter, panic-safe invocation, documented
      skip conditions, PhaseWorktree accessor
- [ ] Contract E: hook fires per phase-start; fromPhase == "" on frontier
      activations; conversationIDs remain phase-scoped
- [ ] Step-level scheduling untouched: no changes to ScheduleReadySteps /
      PromoteReadySteps / acquireSlots / semaphore limits
- [ ] No new bus topics (grep: only plan.phase_completed, task.progress)
- [ ] `go build ./...` clean; `go vet ./internal/agent/...` clean;
      `make lint-ci` clean on touched packages (mutexio, predid)
- [ ] `go test -p 2 ./internal/agent/... ./internal/plan/...` green after
      every leaf
- [ ] No debug artifacts, no TODOs, no placeholder values
- [ ] No line-number corruption (files authored via search_files/terminal
      reads, never by piping `read_file` line-numbered output into
      write_file): `grep -rcE '^\s*[0-9]+\|' --include='*.go'
      internal/agent/ | grep -v ':0'` = empty
- [ ] AGENTS.md invariant added in leaf 04's commit (same-commit rule)

## Coding Conventions

Match `internal/agent` house style:

- **Logging**: `slog` structured key-values (`o.logger.Warn("msg",
  "task_id", id, "error", err)`); Debug for hot-path noise.
- **Errors**: `fmt.Errorf("context: %w", err)` wrapping; every error
  checked (pre-commit blocks new ignored-error sites).
- **Tests**: table-driven (`tests := []struct{ name string; ... }` with
  `t.Run(tt.name, ...)`), `_test.go` beside source, no network, no
  sleeps where a channel/atomic suffices.
- **Setters**: nil-guard for interface/callback params (AGENTS.md
  "Setter methods"); plain-bool setters exempt.
- **Mutexes**: never across I/O — collect-under-lock/release-then-operate,
  or atomic flags (`sync.Map` + `atomic.Bool` CAS for the per-task
  in-flight guard, Contract B).
- **IDs**: never `time.Now().UnixNano()`; phase/step IDs already come
  from the plan store — do not generate new ones.
- **Type assertions**: two-value form on `map[string]any` payloads.
- **Comments** explain WHY; cite contract letters (A-E) at definition
  sites.
- **Test command**: `go test -p 2 ./internal/agent/... ./internal/plan/...`
  (macOS ephemeral-port rule: ALWAYS `-p 2`; leaf 04 additionally runs
  `./internal/config/... ./internal/daemon/...`).

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-frontier-compute.md | PENDING | |
| 02-store-state.md | PENDING | |
| 03-hooks-budget.md | PENDING | |
| 04-surface-docs.md | PENDING | |
| OPEN-QUESTIONS.md | PENDING | resolve before/with leaf 04 |

## Integration Test Plan

1. `go build ./...` after every leaf.
2. `go test -p 2 ./internal/agent/... ./internal/plan/...` after every
   leaf; leaf 04 adds `go test -p 2 ./internal/config/...
   ./internal/daemon/...`.
3. **Behavior matrix** (leaf 02 test artifacts, re-run at integration):
   - Serial plan (flag off): existing orchestrator_phases_test.go suite
     passes UNCHANGED — proves byte-identical legacy behavior.
   - Parallel plan (flag on): seed 3 phases A→(B,C) where B and C depend
     only on A's artifacts; completing A's last step asserts BOTH B and C
     get stamped conversationIDs and startup context in the same advance;
     D (depends on B) stays pending until B completes.
4. **Race check**: `go test -p 2 -race ./internal/agent/ -run Frontier`
   (leaf 02/03 tests) — concurrent terminal events on one task must not
   double-start a phase.
5. `make lint-ci` clean on touched packages (mutexio + predid analyzers).
6. Manual smoke (orchestrator, in-session, after leaf 04): set
   `plans.parallel_phases = true` in `~/.meept/meept.json5`, submit a
   plan with two independent phases, observe both phase-start logs and
   per-phase conversationIDs; flip back to false and observe strict
   serial behavior restored.
