# leaf 03 — Frontier-Aware Hooks, Per-Phase Worktrees, Single-Slot Phase-State Survey

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task in this document
using TDD. **Do NOT commit. Do NOT run `git add`.** Write changes, run
verification, report results. The orchestrator handles all git
operations.

- Parent: `docs/plans/phase-frontier-parallel/master.md` — Interface
  Contracts D and E are frozen.
- Scope (≤3 files of change): `internal/agent/orchestrator.go`
  (provisioner field + setter + worktree cache),
  `internal/agent/orchestrator_phases.go` (provisioner invocation inside
  startPhase), `internal/agent/budget_hierarchy.go` (single-slot fix
  from the Task-3 survey — ONLY if the survey confirms the fork below;
  otherwise this file is untouched and the survey findings are reported
  instead). Tests:
  `internal/agent/orchestrator_frontier_test.go` (extend) and
  `internal/agent/budget_hierarchy_test.go` (extend).
- Dependencies: leaf 02 (startPhase exists with fromPhase threading;
  parallelPhases flag exists).
- Estimated context: ~60-90K.
- Test command: `go test -p 2 ./internal/agent/... ./internal/plan/...`
  (ALWAYS `-p 2`: macOS ephemeral-port rule).

## Interface Contract (Contracts D + E, frozen)

### Contract D: per-phase worktree provisioner

```
Field:       phaseWorktreeProvisioner func(ctx context.Context, taskID, phaseID, phaseName string) (string, error)
             phaseWorktrees sync.Map   // key taskID+"\x00"+phaseName -> string

Setter:      func (o *Orchestrator) SetPhaseWorktreeProvisioner(
                 fn func(ctx context.Context, taskID, phaseID, phaseName string) (string, error))
             Nil-guarded per AGENTS.md setter convention (fn != nil).

Invocation:  inside startPhase (orchestrator_phases.go), AFTER the
             checkPhaseReady gate and BEFORE the step-stamping loop;
             ONLY when o.parallelPhases is true (serial mode: identical
             behavior to today — provisioner never invoked).

             wt, err := o.phaseWorktreeProvisioner(ctx, taskID, p.ID, p.Name)
             switch {
             case err != nil:
                 o.logger.Warn("phase worktree provisioning failed; phase continues without isolation",
                     "task_id", taskID, "phase", p.Name, "error", err)
             case wt == "":
                 // provisioner decided no worktree is needed (e.g. plan
                 // performs no file writes) — nothing to cache
             default:
                 o.phaseWorktrees.Store(taskID+"\x00"+p.Name, wt)
             }
             The WHOLE invocation is wrapped in the same panic-safe
             subscriber wrapper as onPhaseTransition
             (orchestrator_phases.go:80-85 pattern):
                 func() { defer func() { _ = recover() }(); ... }()
             A provisioner panic therefore degrades to "no worktree",
             logged at Warn, never fails the phase start.

Accessor:    func (o *Orchestrator) PhaseWorktree(taskID, phaseName string) string
             Returns "" when absent. Exported: the daemon-side job
             processor resolves the step working dir
             (resolveStepWorkingDir precedence: WorktreePath >
             ProjectPath > session CWD > "", internal/daemon/
             components.go:7240) and leaf 04 wires the consumption.

Skip conditions (ALL documented in code comment at the invocation site):
  - flag off (serial mode)
  - provisioner not wired (nil) — no log spam; single Debug on first
    frontier phase start is acceptable but optional
  - provisioner error → Warn, continue WITHOUT isolation
  - provisioner returns ("", nil) → no worktree needed (provisioner's
    decision, e.g. no-file-write plans)
```

### Contract E: per-phase-start hook (already exists — VERIFY, don't rebuild)

```
onPhaseTransition (orchestrator.go:53-56) + SetPhaseTransitionHook
(orchestrator.go:89-96, nil-guarded) already fire once per startPhase
invocation after leaf 02. This leaf's hook work is:

1. VERIFY per-phase-start firing: extend the leaf-02 hook-capture test —
   a 3-phase frontier activation (A→B, A→C in one advance, D later)
   must produce exactly 3 hook calls total, one per phase start,
   frontier starts carrying fromPhase == "".
2. VERIFY conversationID scoping: "phase-<phaseID>-<stepID>"
   (orchestrator_phases.go:70 stamping) is already phase-scoped and
   collision-free under parallel starts — assert two phases activated
   in the same advance produce disjoint conversationID sets. No code
   change expected here; a regression test only.
```

## Tasks

### Task 1: RED — worktree provisioner tests

Extend `internal/agent/orchestrator_frontier_test.go` (table-driven,
same fixture helpers as leaf 02):

| case | setup | expect |
|------|-------|--------|
| serial mode skips provisioner | flag off; provisioner sets a flag var | provisioner NOT invoked; phase starts normally |
| flag on + provisioner wired | frontier activation; stub provisioner returns `/tmp/wt-<phase>` | PhaseWorktree(taskID, phase) returns the path; phase steps stamped normally |
| flag on + provisioner nil | frontier activation | no panic; phase starts; PhaseWorktree returns "" |
| provisioner error | returns error | Warn logged (capture via slog handler test or assert via behavior), phase STILL starts, cache empty |
| provisioner returns ("", nil) | no-file-write plan | no cache entry; phase starts |
| provisioner panics | panics inside provisioner | phase STILL starts (panic-safe wrapper); Warn-able condition |
| idempotent re-invocation | same phase started once (re-entrancy guard from leaf 02 prevents double-start) | provisioner invoked exactly once per phase start |

Run RED (setter + accessor don't exist yet → compile failure).

### Task 2: GREEN — implement provisioner

1. Add field, cache (`sync.Map`), nil-guarded setter (orchestrator.go,
   near SetPhaseTransitionHook), accessor `PhaseWorktree`.
2. Wire invocation into startPhase per Contract D (position: after
   checkPhaseReady, before stamping; flag-gated; panic-wrapped).
3. Verify: `go test -p 2 ./internal/agent/ -run 'Worktree|AdvancePhases'`
   green; full package green.

### Task 3: single-slot phase-state survey + fix

Survey EVERY cross-component phase-state slot. Known suspects (verified
during plan authoring — re-verify line numbers before editing):

- **CONFIRMED single-slot: `BudgetHierarchy.currentPhase`**
  (internal/agent/budget_hierarchy.go:306, `// ID of the
  currently-selected phase`). Written by `SelectPhaseBudget` (:477) and
  `AdvancePhase` (carryover logic :552-588); read by `RecordUsage`
  (:520) and the borrow path (:525). Under parallel phases, usage from
  phase B records against whichever phase was selected LAST —
  misattribution.
  - **Fix (this leaf):** replace the single slot with a per-phase
    map-based selection: `phaseTurnBudgets map[string]*BudgetAllocation`
    keyed by phaseID (turn budget per phase), plus a method
    `SelectPhaseBudget(phaseID)` that becomes idempotent-per-phase
    (re-selecting an already-selected phase is a no-op, preserving
    turn-budget continuity while both run). `RecordUsage` gains a
    `phaseID` routing parameter — CALLERS updated:
    internal/agent/loop.go call sites (:2011, :7023 pass "default";
    grep `RecordUsage(` + `SelectPhaseBudget(` for the full set).
    Keep `GetTurnBudget()` semantics: sum across selected phases' turn
    budgets (document the change in the method comment). Carryover
    (:552) applies per-phase on its own transition, not a global swap.
  - Serial-mode equivalence: with exactly one phase selected at a time,
    behavior is identical to today (map with one entry). Table test
    proves it: 2-phase serial sequence → same allocations/usage
    attribution as a fresh single-slot implementation would give.
  - Parallel test: select p1+p2, record usage with each phaseID,
    assert per-phase pools and task root are attributed independently.
- **Checked, NOT single-slot (no change; record in report):**
  - `o.artifacts` (orchestrator.go:50) — per-task store, keyed by
    artifact name; concurrent phases WRITE distinct names; frontier
    gate reads snapshots. Phase-name collision would be a plan-authoring
    bug, not scheduler state.
  - conversationID stamping (orchestrator_phases.go:70) — phase-scoped
    by construction (Contract E verification above).
  - `plan.PlanPhase.State` (internal/plan/plan.go:73) — per-phase
    column, not a slot.
  - `phaseSpecOverride` map (orchestrator.go:51) — test-only, map not
    slot.
  - `PlanManager.phaseTaskMap` / `taskPlanMap` (internal/plan/
    manager.go:678) — keyed maps, not slots.
  - `StrategicPlanner` phase fields — planning-time only, no runtime
    mutation during execution.
- If the survey finds an ADDITIONAL single-slot state this plan missed,
  STOP and report it in the results — do NOT expand this leaf's scope;
  the orchestrator files a follow-up.

Test additions (budget_hierarchy_test.go, table-driven):
- `TestBudgetHierarchy_ParallelPhaseSelection` — two phases selected,
  independent usage attribution, carryover per-phase.
- `TestBudgetHierarchy_SerialEquivalence` — single-phase-at-a-time
  sequence matches legacy expectations (existing tests in this file
  must pass UNMODIFIED wherever semantics are preserved; where
  GetTurnBudget() semantics changed, update ONLY that assertion with a
  comment citing this plan).

### Task 4: verify

```bash
go build ./...
go vet ./internal/agent/
go test -p 2 ./internal/agent/... ./internal/plan/...
go test -p 2 -race ./internal/agent/ -run 'Worktree|BudgetHierarchy'
```

## Self-Verification Checklist

- [ ] Provisioner: nil-guarded setter, panic-wrapped invocation,
      flag-gated (serial mode untouched), cached + accessor, all 7
      table cases green
- [ ] Contract E: 3 hook calls for 3 phase starts verified; frontier
      starts carry fromPhase == ""; conversationIDs disjoint across
      parallel starts
- [ ] BudgetHierarchy single slot replaced; serial equivalence test
      proves flag-off/serial behavior unchanged; parallel attribution
      correct; callers updated (grep RecordUsage/SelectPhaseBudget for
      stragglers)
- [ ] Survey findings recorded (in this leaf's completion report):
      confirmed slots, verified non-slots, any NEW findings flagged
      separately not silently fixed
- [ ] No mutex across I/O (sync.Map for cache; budget hierarchy keeps
      its existing mu pattern with I/O-free sections)
- [ ] No debug artifacts, no TODOs, no placeholder values

## Review Checklist (for orchestrator)

- [ ] Contract D verbatim: setter signature, invocation position
      (after gate, before stamping), panic wrapper, skip conditions
      commented at the site
- [ ] PhaseWorktree exported accessor present
- [ ] BudgetHierarchy: map-based phase selection; existing
      budget_hierarchy_test.go suite green (only GetTurnBudget-related
      assertions updated, with citations)
- [ ] No line-number corruption: re-check context with search_files /
      `terminal cat` before writing; NEVER pipe read_file's `NNN|`
      line-numbered output into write_file. Verify:
      `grep -rcE '^\s*[0-9]+\|' internal/agent/orchestrator*.go internal/agent/budget_hierarchy.go` → all 0.
- [ ] `go test -p 2 ./internal/agent/... ./internal/plan/...` green;
      -race clean on Worktree|BudgetHierarchy
- [ ] No debug artifacts, no TODOs, no placeholder values

Do NOT commit. Do NOT run git add.
