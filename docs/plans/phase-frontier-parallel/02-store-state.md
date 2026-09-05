# leaf 02 — Multiple ACTIVE Phases (frontier dispatch behind plans.parallel_phases)

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task in this document
using TDD. **Do NOT commit. Do NOT run `git add`.** Write changes, run
verification, report results. The orchestrator handles all git
operations.

- Parent: `docs/plans/phase-frontier-parallel/master.md` — read its
  Interface Contracts section first; Contract B is frozen.
- Scope (≤3 files): `internal/agent/orchestrator_frontier.go` (NEW),
  `internal/agent/orchestrator.go` (flag field + setter + dispatch
  branch), `internal/agent/orchestrator_phases.go` (startPhase
  extraction). Test file:
  `internal/agent/orchestrator_frontier_test.go` (NEW, tests don't
  count against the 3-file budget).
- Dependencies: leaf 01 (`phaseNode`, `computePhaseFrontier` exist in
  `package agent`, unexported).
- Estimated context: ~60-90K (realistically ~90K; this is the big leaf).
- Test command: `go test -p 2 ./internal/agent/... ./internal/plan/...`
  (ALWAYS `-p 2`: macOS ephemeral-port rule).

## Interface Contract (Contract B, frozen)

```
Setter:      func (o *Orchestrator) SetParallelPhases(enabled bool)
Field:       parallelPhases bool on Orchestrator (zero value false —
             serial default; NOT settable via constructor to keep
             OrchestratorDeps untouched)

Dispatch:    maybeTransitionPhase (orchestrator.go:367) gains ONE branch
             at the point where it currently calls startNextPhase:
               if !o.parallelPhases {
                   // legacy serial path — body byte-identical to today
                   if err := o.startNextPhase(ctx, taskID, step.Phase); err != nil {
                       o.logger.Warn("phase transition failed",
                           "task_id", taskID, "from_phase", step.Phase, "error", err)
                   }
                   return
               }
               o.advancePhasesFrontier(ctx, taskID)
             Everything above that point (nil guards, step lookup,
             step.Phase == "" short-circuit, IsPhaseComplete check that
             detects "the finished step's phase may now be done") stays
             shared — it is the trigger, not the strategy.

New file:    internal/agent/orchestrator_frontier.go
             func (o *Orchestrator) advancePhasesFrontier(ctx context.Context, taskID string)
             - in-flight guard: `inflight sync.Map` (taskID -> *atomic.Bool);
               CompareAndSwap(false, true); on CAS failure return (another
               terminal event for the same task is already advancing).
               defer Store(false). NEVER hold a mutex across store I/O
               (mutexio).
             - non-plan task: GetPhasesByTask returns (nil, nil) → return.
               GetPhasesByTask error → Warn + return (matches
               maybeTransitionPhase's best-effort posture).
             - classify phases: for each phase p,
                 steps := stepStore.GetPhaseSteps(taskID, p.Name)
                 busy  := any step.State not IsSuccessfullyTerminal()
                 done  := all steps IsSuccessfullyTerminal() OR no steps
               (IsPhaseComplete predicate parity: task/step.go:1463-1479,
               zero-steps-counts-complete.)
             - nodes := []phaseNode for phases with !done
             - availableArtifacts := snapshot of o.artifacts (names -> true).
               Use artifactStore.Has per name over the union of all nodes'
               consume names (read-only; add NO mutating accessor). If
               o.artifacts == nil treat as empty map (flag-off behavior
               never reaches here, but nil-safety is free).
             - busyPhases := set of !done phase names
             - ready, cycleDetected := computePhaseFrontier(nodes,
               availableArtifacts, busyPhases)
             - cycleDetected: Warn "phase frontier stalled (cycle or
               unmet dependency)" + fallback: legacy list-order pick of
               the first !done phase → startPhase. Anti-cycle escape,
               never a busy loop (the guard + !done filter bound it).
             - for each ready node in returned order: resolve *plan.PlanPhase
               by Name from the loaded phase list → o.startPhase(ctx,
               taskID, phase, "")  (fromPhase == "" = frontier activation).
             - log Info "Phase frontier advanced" with
               ready names + counts.

Extraction:  internal/agent/orchestrator_phases.go
             startNextPhase KEEPS signature
             `func (o *Orchestrator) startNextPhase(ctx, taskID, completedPhaseName string) error`
             and its list-order selection loop (lines 27-41). Steps 2-5
             (spec load via getPlanPhaseSpec, checkPhaseReady gate,
             renderPhaseStartup, fresh conversationIDs +
             AccumulatedContext stamping over GetPhaseSteps/
             UpdatePhaseSteps, panic-safe onPhaseTransition, Info log)
             move verbatim into:
               func (o *Orchestrator) startPhase(ctx context.Context,
                   taskID string, p *plan.PlanPhase, fromPhase string) error
             - startNextPhase = select-next-in-list + startPhase(..., from)
             - frontier path  = ready set + startPhase(..., "")
             - onPhaseTransition invocation keeps the panic-safe wrapper
               (orchestrator_phases.go:80-85 pattern) and passes
               fromPhase through (serial: completed name; frontier: "").
             - Re-entrancy guard inside startPhase: if every step of p is
               already past StepPending (state != task.StepPending after
               stamping would be a no-op anyway), skip stamping and log
               Debug "phase already started; skipping double-start".
               Compare with task.StepReady/StepScheduled/StepRunning —
               do NOT reimplement state checks, import internal/task.
```

Flag-off equivalence is REQUIRED: identical seeds, flag off ⇒ exactly
today's transition sequence. The existing
`orchestrator_phases_test.go` suite must pass UNMODIFIED after your
extraction refactor.

## Tasks

### Task 1: RED — flag-off equivalence harness (proves the refactor is safe)

Create `internal/agent/orchestrator_frontier_test.go`:

1. `TestAdvancePhases_SerialEquivalence` — table-driven, reusing the
   fixture patterns from `orchestrator_phases_test.go`
   (stubPlanStore + newTestOrchestrator + real StepStore as in
   TestMaybeTransitionPhase_AdvancesToNextPhase, :336). For each case:
   seed a task + 2-4 phases + steps, fire handleJobCompleted-equivalent
   (call maybeTransitionPhase directly, as existing tests do) with
   `SetParallelPhases(false)`, assert: active-phase count == 1 at every
   observation point, next-phase selection == list order, and step
   conversationIDs == `phase-<phaseID>-<stepID>` (unchanged format).
   Cases: (a) linear 3-phase; (b) phase with multiple steps; (c)
   single-phase plan (no-op); (d) task with no plan (no-op).
2. Run it GREEN against the untouched code (this is the safety net that
   must stay green through Tasks 2-3).

### Task 2: extraction refactor (no behavior change)

1. Move startNextPhase's steps 2-5 into `startPhase` verbatim (same
   file, orchestrator_phases.go). startNextPhase keeps selection +
   delegates. fromPhase threading per contract.
2. Add the `parallelPhases` field + `SetParallelPhases` setter to
   orchestrator.go (near SetPhaseTransitionHook :89-96; plain-bool
   setter, nil-guard convention does not apply to bools).
3. Add the dispatch branch in maybeTransitionPhase (contract above).
   `advancePhasesFrontier` may be a stub that just calls
   startNextPhase for now — Task 3 replaces it.
4. Verify: `go test -p 2 ./internal/agent/...` — ALL green including
   the pre-existing orchestrator_phases_test.go suite UNMODIFIED, and
   your Task-1 table still green. If any existing test needed editing,
   STOP — the extraction is wrong; fix the refactor, not the test.

### Task 3: RED→GREEN — frontier dispatch

1. RED: `TestAdvancePhases_ParallelFrontier` (same file, table-driven):
   - fixture: A produces artifact `x`; B consumes `x` (required);
     C consumes `x` (required); D DependsOnPhase=[B]. Seed steps so
     only A is busy. Stamp A's artifact into a real artifactStore
     (use the store's public API as artifact-producing steps do —
     inspect internal/agent/artifacts.go for the record method; do NOT
     touch struct fields directly).
   - case "frontier starts both": SetParallelPhases(true); complete
     A's last step; assert B's AND C's steps BOTH got fresh
     conversationIDs + AccumulatedContext (startup context) after ONE
     maybeTransitionPhase call, and D's steps did not.
   - case "fromPhase empty on frontier starts": wire
     SetPhaseTransitionHook capturing calls; assert one call per
     started phase with fromPhase == "".
   - case "artifact gate still applies": B consumes `y` (required,
     never produced): B must NOT start even though the graph allows;
     assert steps untouched + Warn-able condition (no fatal).
   - case "cycle fallback": B requires x produced by A, A requires y
     produced by B, both busy, fire an unrelated phase's completion →
     Warn path exercised, no panic, no double-start.
   - case "concurrent terminal events": launch 2 goroutines both
     calling advancePhasesFrontier(taskID) (expose it for test via
     same-package call); assert exactly one phase-start per phase
     (in-flight guard works). Run with `-race` locally in your
     verification step.
2. GREEN: implement `advancePhasesFrontier` fully per Contract B.
3. Verify:

```bash
go build ./...
go vet ./internal/agent/
go test -p 2 ./internal/agent/... ./internal/plan/...
go test -p 2 -race ./internal/agent/ -run 'AdvancePhases'
go test -p 2 ./internal/agent/ -run 'PhaseTransition|MaybeTransition'   # legacy suite intact
```

## Self-Verification Checklist

- [ ] Files touched: orchestrator_frontier.go (new), orchestrator.go,
      orchestrator_phases.go (+ 2 new test files) — nothing else
- [ ] Flag default false; SetParallelPhases exists; OrchestratorDeps
      untouched
- [ ] Legacy suite `orchestrator_phases_test.go` passes UNMODIFIED
- [ ] Flag-off table test reproduces today's exact behavior (1 active
      phase, list order, same conversationID format)
- [ ] Flag-on: two sibling phases start from ONE trigger; dependent
      phase does not; artifact gate enforced; fromPhase == "" on
      frontier activations
- [ ] In-flight guard: no double-start under concurrent triggers
      (-race clean)
- [ ] No mutex held across store calls (mutexio-safe: sync.Map +
      atomic.Bool CAS, snapshot-then-operate)
- [ ] onPhaseTransition keeps panic-safe wrapper
- [ ] No new bus topics; no changes to tactical.go / step.go /
      ScheduleReadySteps / PromoteReadySteps / acquireSlots
- [ ] No debug artifacts, no TODOs, no placeholder values

## Review Checklist (for orchestrator)

- [ ] Contract B verbatim: setter name, dispatch branch shape,
      startPhase signature `(ctx, taskID string, p *plan.PlanPhase,
      fromPhase string) error`
- [ ] startNextPhase still present with original signature and
      list-order selection (serial callers/behavior intact)
- [ ] conversationID stamping still exactly
      `fmt.Sprintf("phase-%s-%s", phaseID, stepID)`
- [ ] Cycle fallback = Warn + list-order startPhase, no busy loop
- [ ] No line-number corruption: any file re-authored from observed
      context must be written from clean source — use search_files /
      `terminal cat` for context, NEVER pipe read_file's `NNN|`
      line-numbered output into write_file. Verify:
      `grep -rcE '^\s*[0-9]+\|' internal/agent/orchestrator*.go` → all 0.
- [ ] `go test -p 2 ./internal/agent/... ./internal/plan/...` green
- [ ] No debug artifacts, no TODOs, no placeholder values

Do NOT commit. Do NOT run git add.
