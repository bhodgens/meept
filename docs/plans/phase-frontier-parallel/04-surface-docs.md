# leaf 04 — Config Key, Daemon Wiring, Surface Docs, AGENTS.md Invariant

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task in this document
using TDD. **Do NOT commit. Do NOT run `git add`.** Write changes, run
verification, report results. The orchestrator handles all git
operations.

- Parent: `docs/plans/phase-frontier-parallel/master.md` — Contract C is
  frozen. OPEN-QUESTIONS.md forks flagged `leaf 04` in their Rec must be
  RESOLVED by the orchestrator BEFORE you dispatch; if any remain open
  when you start, implement the Rec side and note it in your report.
- Scope (≤3 files of change): `internal/config/schema.go` (one field),
  `internal/daemon/components.go` (one wiring call + worktree
  consumption point), plus docs
  (`docs/workflows/agent-orchestration.md`, `AGENTS.md`) — docs don't
  count against the file budget. Tests:
  `internal/config/schema_test.go` (or the package's existing config
  test file — discover it) and `internal/daemon/components_test.go`
  (or nearest existing test site for the wiring).
- Dependencies: leaf 02 (SetParallelPhases), leaf 03
  (SetPhaseWorktreeProvisioner, PhaseWorktree accessor).
- Estimated context: ~60-90K.
- Test command: `go test -p 2 ./internal/agent/... ./internal/plan/...
  ./internal/config/... ./internal/daemon/...` (ALWAYS `-p 2`: macOS
  ephemeral-port rule).

## Interface Contract (Contract C, frozen)

```
File:        internal/config/schema.go — PlansConfig (struct at line ~339)

Field:       ParallelPhases bool
Tags:        json:"parallel_phases"  toml:"parallel_phases"
Key:         plans.parallel_phases
Default:     false (Go zero value; absent key = false; serial preserved)
Validate():  unchanged (bool needs no validation)
Doc comment on the field:
     // ParallelPhases enables frontier-based phase dispatch: multiple
     // plan phases run concurrently when their Produces/Consumes
     // dependencies allow. Default false = strict serial phases
     // (legacy behavior). Meaningful parallelism requires plans whose
     // phases declare Produces/Consumes artifacts. See
     // docs/workflows/agent-orchestration.md (phase frontier section).

Daemon wiring (internal/daemon/components.go, where the Orchestrator is
constructed — locate via search_files for "NewOrchestrator" and the
existing SetPhaseTransitionHook call site; wire NEXT to it, BEFORE
Start):
     orch.SetParallelPhases(cfg.Plans.ParallelPhases)

Worktree consumption (same file, resolveStepWorkingDir area):
     when a step's phase has a provisioned worktree, the per-phase
     worktree participates in the resolution precedence
     (WorktreePath > ProjectPath > session CWD > ""). Minimal wiring:
     at the AgentJobProcessor step-dispatch site (components.go:7362
     "wd := p.resolveStepWorkingDir(job)"), prefer
     orchestrator.PhaseWorktree(job.TaskID, stepPhase) when non-empty
     (stepPhase from the step record; "" phase = no worktree). The
     provisioner itself is wired by whoever constructs the
     Orchestrator with a real worktree factory — this leaf wires the
     FLAG and the CONSUMPTION hook; a production provisioner
     implementation is out of scope (test stubs prove the plumbing).
     If the consumption point proves to be a different function than
     resolveStepWorkingDir's caller, adapt to the actual site and note
     the deviation in your report.
```

## Tasks

### Task 1: RED — config schema tests

In the config package's existing test file (discover with
search_files for `PlansConfig` in `internal/config/*_test.go`; if none
exists, create `internal/config/plans_config_test.go`), table-driven:

- absent key → `ParallelPhases == false`
- `plans.parallel_phases = true` parses (both the JSON tag via config
  load path and the TOML tag if the package tests TOML — match
  however sibling fields like `require_signoff` are tested)
- key does not break `Validate()` (true and false both valid)

Run RED → add the field to PlansConfig with exact tags + doc comment →
GREEN.

### Task 2: RED→GREEN — daemon wiring test

In the daemon package's existing orchestrator-construction test (or a
new `internal/daemon/orchestrator_wiring_test.go` if none):

- construct the daemon component graph the way components.go does (or
  call the extracted wiring function if components.go has one; else
  assert via a small test-only construction helper): flag false by
  default → `orchestrator.parallelPhases == false` (same-package
  access not available — assert observable behavior: phase transitions
  stay serial in a seeded scenario, mirroring the leaf-02 serial table
  with the daemon-configured orchestrator, or assert via a config
  getter if one exists).
- config true → parallelPhases observed true (via behavior: seeded
  frontier scenario advances two phases at once, reusing leaf-02 test
  patterns).

Keep this test LIGHT: the deep behavior matrix already lives in leaf
02; here you prove the config VALUE actually reaches the orchestrator.

### Task 3: worktree consumption wiring

1. Locate the step working-dir resolution call site (components.go
   ~:7362). Add the per-phase worktree preference per Contract C,
   nil-safe on orchestrator reference and empty-safe on phase name.
2. Table test in the daemon package (extend
   `agent_job_processor_test.go` patterns — it already tests
   resolveStepWorkingDir precedence with seeded tasks/sessions):
   - phase worktree set → that path wins over ProjectPath/CWD
   - phase worktree empty → existing precedence unchanged (all three
     existing precedence assertions stay green)
   - serial mode (provisioner never ran) → precedence unchanged

### Task 4: docs + AGENTS.md

1. `docs/workflows/agent-orchestration.md`: add a "Phase frontier
   (parallel phases)" subsection to the phase/orchestration section:
   - the frontier rule (artifact readiness + no busy transitive deps;
     list order = tiebreak/anti-cycle only)
   - opt-in flag `plans.parallel_phases` (default false; how to enable;
     what a parallel-friendly plan looks like — phases declaring
     Produces/Consumes)
   - per-phase worktrees (when provisioned, precedence in step working
     dir, when skipped: serial mode / no provisioner / no-file-write
     plans / provisioner error)
   - budget note: per-phase budget selection under parallel phases
     (leaf 03)
   - observability: per-phase-start via the existing phase-transition
     hook; `plan.phase_completed` and `task.progress` topics UNCHANGED
     — no new bus topics.
2. `AGENTS.md`: add a short invariant under Critical Invariants —
   **"Phase dispatch mode is per-config opt-in"**: `plans.parallel_phases`
   default false preserves strict serial phases; when true, phase
   starts are frontier-driven (artifact + dependency gating, list order
   tiebreak only), conversationIDs stay phase-scoped
   (`phase-<phaseID>-<stepID>`), per-phase worktrees are provisioned
   via the orchestrator hook and win in step working-dir resolution,
   and BudgetHierarchy phase selection is per-phase (multi-select).
   Same-commit rule: this edit lands in the same commit as the wiring
   code.
3. If OPEN-QUESTIONS.md forks were resolved differently than their Rec,
   reflect the DECIDED behavior in the docs (not the Rec).

### Task 5: verify

```bash
go build ./...
go vet ./internal/config/ ./internal/daemon/ ./internal/agent/
go test -p 2 ./internal/agent/... ./internal/plan/... ./internal/config/... ./internal/daemon/...
make lint-ci     # mutexio + predid + golangci-lint on touched packages
```

## Self-Verification Checklist

- [ ] Field `ParallelPhases` with EXACT tags `json:"parallel_phases"
      toml:"parallel_phases"` on PlansConfig; doc comment cites default
      + docs path
- [ ] Daemon wiring: SetParallelPhases(cfg.Plans.ParallelPhases) next
      to the existing SetPhaseTransitionHook site, before Start
- [ ] Per-phase worktree wins in working-dir precedence when set;
      existing precedence assertions untouched and green
- [ ] Config-value-reaches-orchestrator test green both ways
- [ ] agent-orchestration.md phase-frontier section complete (rule,
      flag, worktrees, budget, observability)
- [ ] AGENTS.md invariant added (same-commit pairing noted)
- [ ] No new bus topics anywhere in the diff
- [ ] No debug artifacts, no TODOs, no placeholder values

## Review Checklist (for orchestrator)

- [ ] Contract C verbatim (field, tags, key, default, daemon call)
- [ ] `./bin/meept config get plans.parallel_phases` returns false on a
      default config (manual smoke, optional)
- [ ] Worktree consumption adapts to the REAL call site (deviation
      from resolveStepWorkingDir's caller documented in the report if
      any)
- [ ] Docs match IMPLEMENTED behavior (incl. any OPEN-QUESTIONS
      decisions), not aspirational behavior
- [ ] No line-number corruption: re-check context with search_files /
      `terminal cat` before writing; NEVER pipe read_file's `NNN|`
      line-numbered output into write_file. Verify:
      `grep -rcE '^\s*[0-9]+\|' internal/config/schema.go internal/daemon/components.go` → 0.
- [ ] Full four-package test command green; `make lint-ci` clean
- [ ] No debug artifacts, no TODOs, no placeholder values

Do NOT commit. Do NOT run git add.
