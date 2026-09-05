# OPEN-QUESTIONS.md — phase-frontier-parallel

Format: Q (question) / Rec (recommendation) / Impact (what changes
depending on the answer). Forks marked **leaf 04** must be resolved by
the orchestrator before dispatching that leaf. Others resolve during
review of their owning leaf.

---

## Q1. Flag default after validation — flip to true later?

**Q:** `plans.parallel_phases` defaults false. After the integration
window proves the frontier in production-like runs, should the default
flip to true (parallel-by-default), or stay opt-in indefinitely?

**Rec:** Stay opt-in for at least one full release cycle after leaf 04
lands, then decide with data: run meept-bench workloads with the flag
on and compare (a) wall-clock per task, (b) per-agent semaphore
starvation, (c) artifact-gate misses (Warn log rate from
advancePhasesFrontier). Flip ONLY if (c) stays ~zero — a high miss rate
means plans in the wild don't declare Produces/Consumes well enough for
frontier gating to be trustworthy. Flipping the default is a product
decision (AGENTS.md: "Flipping these defaults is a product decision,
not a code cleanup") — same bar as the skills.wiki defaults.

**Impact:** flip ⇒ docs + schema doc comment change, serial-equivalence
tests stay (they cover explicit false). No code change.

---

## Q2. Cycle policy — reject at synthesize vs runtime skip

**Q:** When a plan's phase graph has a dependency cycle (or a fully
stalled frontier), what should happen: (a) reject at plan
synthesize/approve time, or (b) runtime skip with the Warn +
list-order fallback (current Contract B design)?

**Rec:** Keep BOTH, in this order of preference: (1) BEST —
synthesize-time validation: when the strategic planner produces phases,
run `computePhaseFrontier` with everything busy over the complete
phase set; a non-empty `cycleDetected` fails plan synthesis and forces
replan (planner gets structured feedback: "phase dependency cycle
among [A, B]"). Add this in a FOLLOW-UP leaf, not this tree — it
touches the planner path and deserves its own tests. (2) The runtime
fallback stays forever as defense-in-depth (hand-edited plans, store
corruption).

**Impact:** (1) adds a leaf touching strategic.go validation path;
(2) is already specced. If rejected entirely, delete the runtime
Warn-fallback and fail the task instead — harsher, but catches plan
bugs earlier at the cost of killing in-flight work.

---

## Q3. Planner prompts taught to emit parallel-friendly phases — now or later?

**Q:** Meaningful parallelism requires the planner to declare
Produces/Consumes on phases and to group independent work into sibling
phases. Should the planner templates/prompts be updated in this tree so
NEW plans are frontier-friendly from day one?

**Rec:** LATER, as its own plan. Reasons: (a) prompt changes shift
planner output distribution globally and need their own eval loop
(harness-eval infrastructure), (b) the runtime is flag-off by default,
so parallel-unfriendly plans lose nothing until the flag flips (Q1),
(c) bundling prompt surgery into a scheduler tree risks conflating
scheduler regressions with prompt regressions. When it happens: teach
the multi-phase template to (i) declare artifacts on every phase, (ii)
split unconstrained phases into siblings, (iii) avoid optional consumes
where the producer is guaranteed (they create frontier edges without
gating).

**Impact:** now ⇒ new leaf editing planner templates + evals, serial
work stacked after leaf 04. Later ⇒ zero code impact in this tree;
docs/workflows/agent-orchestration.md (leaf 04) includes a
"writing parallel-friendly plans" note so human authors get the
benefit immediately.

---

## Q4. Provisioner implementation source (leaf 03/04 boundary)

**Q:** The per-phase worktree provisioner HOOK is wired in leaf 03 and
consumed in leaf 04, but no production provisioner is implemented. Who
builds the real one — the project manager's existing worktree machinery
(`projects.worktree_per_plan`, `MaxWorktreesPerProject` in
internal/config/schema.go:392-396) or the session worktree path
(session.WorktreePath)?

**Rec:** Follow-up leaf after this tree: a provisioner backed by the
project worktree manager (`project.Manager`), one worktree per
(taskID, phaseID), registered in `phaseWorktrees`, cleaned up on task
completion (hook the existing task-terminal artifact-store reset path).
Until then, parallel phases on a shared project share one working
directory — safe ONLY for read-only/non-conflicting plans; the skip
condition ("", nil) exists precisely so the operator can opt plans out.

**Impact:** follow-up leaf touches internal/project + daemon wiring;
until it lands, parallel-mode operators must either run
non-file-writing phases concurrently or accept shared-dir semantics.

---

## Q5. `IsPhaseComplete` zero-steps quirk under the frontier

**Q:** `IsPhaseComplete` (internal/task/step.go:1469-1471) treats a
ZERO-STEP phase as complete. Under the frontier, a phase whose steps
haven't been materialized yet (plan approved, steps pending
generation) would classify as done and could gate its dependents open.

**Rec:** Preserve the predicate verbatim (Contract B pins parity) and
handle it structurally: the frontier only considers phases present in
`GetPhasesByTask` WITH their steps already seeded by the normal task
materialization flow — by the time terminal events fire, steps exist.
Add one table case in leaf 02's tests asserting a zero-step phase is
`done` (never busy, never in nodes) and does not stall the graph. If
real-world plans hit the un-materialized window (observe via the
frontier Info log), revisit with a `totalSteps == 0` special case.

**Impact:** special case ⇒ small change in advancePhasesFrontier
classification + tests. None if observation never fires (likely).

---

## Q6. Bus topic for frontier-activated phase starts

**Q:** Frontier phase starts carry `fromPhase == ""` through the
onPhaseTransition hook. Should there be a dedicated bus topic (e.g.
`plan.phase_started`) so UIs can render concurrent phase progress?

**Rec:** NO new topic (pinned by the task context; reconfirmed).
Per-phase-start observability already flows through the
phase-transition hook; per-step progress already flows on
`task.progress`; completion on `plan.phase_completed`
(internal/plan/manager.go:587). If the GUI later wants phase-level
cards, it can subscribe via the existing hook → its own WS relay
decision — that's a UI-plan concern, not this scheduler's.

**Impact:** a future `plan.phase_started` topic ⇒ WS classification
work (AGENTS.md bucket rules) + topology regen + AGENTS.md invariant
edit. Deliberately out of scope here.

---

## Q7. Per-task frontier vs per-plan frontier

**Q:** The frontier is computed per TASK (advancePhasesFrontier takes
taskID, phases loaded via GetPhasesByTask). Plan records can in
principle outlive/rebind tasks — should the frontier key on planID
instead?

**Rec:** Per-task (current design). `GetPhasesByTask` is the existing,
test-covered lookup (manager.go:676; taskPlanMap), step state — the
busy signal — is inherently per-task, and `RestoreMappings`
(manager.go:701) already rebuilds task→plan on restart. Per-plan would
add a task-resolution step for zero behavioral gain.

**Impact:** per-plan ⇒ leaf 02 signature change + new lookup path;
rejected unless multi-task-per-plan execution becomes real.
