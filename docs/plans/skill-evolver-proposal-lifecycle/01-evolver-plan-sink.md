# Evolver plan sink + provenance - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** A user-scoped sink for machine-originated (evolver) plans with
  origin provenance stamped into each plan; human-authored plans keep
  landing in the repo's `docs/plans/`.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A

## Goal

The daemon's CWD is arbitrary — often the meept repo itself — so
`PlanManager.resolvePlanDir` (internal/plan/manager.go:787-800 =
`projectPath + docs/plans`) drops evolver-generated plans into git: **17
uncommitted `docs/plans/skill-evolution-*.md` files exist right now.**
Machine-originated plans belong in a user-scoped sink
(`~/.meept/plans/evolver/` by default, configurable), and each one must
identify its evolver origin so the approval actuator (leaf 03) can find and
dispatch them.

## Context

Verified facts (2026-08-31, do not re-derive):

- `internal/plan/manager.go:787-800` — `resolvePlanDir` returns
  `projectPath + docs/plans`. `:789` already honors a `Storage.ExternalPath`
  override; it just defaults to the repo-relative path.
- `internal/skills/lifecycle/evolver.go:604` — the evolver parks each
  proposal via `planMgr.CreatePlan` when `auto_apply=false`.
- The daemon constructs the evolver's dependencies in
  `internal/daemon/components.go` (Writer/Versioner at :1039-1040; the
  evolver/PlanManager construction site is nearby — locate it).
- User config today: `skills.evolver` enabled, interval 1h,
  `run_on_start` true, `auto_apply` false.
- Evidence of the pollution: real plan files matching
  `docs/plans/skill-evolution-*.md` exist in the repo working tree. Read ONE
  with `terminal cat` (never read_file → write_file) to learn the exact plan
  file format before designing the provenance encoding.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Config: internal/config, skills.evolver section (JSON5 quoted keys):
"plan_dir": ""   // default "" -> ~/.meept/plans/evolver (user-scoped, ~-expanded)

// PlanManager construction: the evolver's PlanManager is created with
// Storage.ExternalPath set to the resolved plan_dir. resolvePlanDir's
// repo-relative default remains ONLY for human-authored plan usage.

// Provenance: every evolver-created plan records:
//   origin: "skill-evolver"
//   proposal_id: <evolver proposal id>
//   action: "archive" | "refine" | "create"
// Encoding follows the EXISTING plan file format — adapt field style to
// what CreatePlan actually writes (frontmatter keys, metadata struct, or
// filename convention). Do not invent a parallel format.

// Provenance readers/helpers (package internal/skills/lifecycle or wherever
// the encoding lands — match CreatePlan's writer):
//   IsEvolverPlan(...) / evolver plan origin+proposal+action accessors
//   (exported if leaf 03 needs them cross-package; keep minimal)
```

### What This Leaf Consumes

```
// internal/plan/manager.go:787-800  resolvePlanDir + ExternalPath override
// internal/skills/lifecycle/evolver.go:604  CreatePlan park site
// internal/config skills.evolver section + normalization/defaults path
// internal/daemon/components.go  evolver/PlanManager construction
// pkg/path or similar ~-expansion helper (FIND it with search_files; do not
// hand-roll tilde expansion)
```

## Tasks

### Task 1: Config knob `skills.evolver.plan_dir`

**Objective:** Add the config key with a user-scoped default and normalize
it (empty → default, `~` expansion).

**Files:**
- Modify: internal/config (skills evolver struct + defaults/normalize)
- Test: matching `_test.go`

**Step 1: Write failing test**

- Empty `plan_dir` → resolves to `<home>/.meept/plans/evolver`
  (expand `~`/`$HOME` from the test fixture, not the developer's home).
- Absolute configured path passes through expanded; relative path is an
  error or rejected by normalization (pick one, document it).
- Existing evolver config fields unchanged.

**Step 2: Run test to verify failure**
Run: `go test ./internal/config/ -run TestEvolverPlanDir -v`
Expected: FAIL (undefined field)

**Step 3: Write minimal implementation**

Follow the existing config struct + normalize patterns (mirrors how
`llm.quota_retry` was normalized — see docs/workflows/quota-resilience.md if
helpful).

**Step 4: Run test to verify pass**
Run: `go test ./internal/config/ -run TestEvolverPlanDir -v`
Expected: PASS

### Task 2: Provenance on evolver plans

**Objective:** Stamp origin/proposal_id/action into every evolver-created
plan, with typed accessors.

**Files:**
- Modify: the evolver's CreatePlan call site (internal/skills/lifecycle,
  evolver.go:604 area) and/or the plan metadata type it populates
- Test: `internal/skills/lifecycle/` test file (new or existing)

**Step 1: Write failing test**

- CreatePlan from the evolver with auto_apply=false produces a plan whose
  stored payload carries origin "skill-evolver", the proposal id, and the
  action — asserted via the accessors, not string-matching the file.
- Accessors on a non-evolver plan return false/empty (no panic).
- Round-trip: write via the real writer path, read via the accessors.

**Step 2: Run test to verify failure**

**Step 3: Write minimal implementation**

Match the existing plan file format exactly (learn it from a real
skill-evolution-*.md file). Extend the metadata type if one exists; do not
add a sidecar file.

**Step 4: Run test to verify pass**
Run: `go test ./internal/skills/lifecycle/ -run TestEvolverPlanProvenance -v`
Expected: PASS

### Task 3: Sink wiring in the daemon

**Objective:** The evolver's PlanManager lands plans in the sink, not the
repo.

**Files:**
- Modify: internal/daemon/components.go (evolver/PlanManager construction)
- Test: daemon or lifecycle test covering construction

**Step 1: Write failing test**

- Construct the evolver via the daemon component path with a fixture HOME
  and a CWD pointed at a temp "repo": creating a plan writes under the
  sink (fixture `~/.meept/plans/evolver/`), and the temp repo's
  `docs/plans/` gains NOTHING.
- Sink directory is created on demand.

**Step 2: Run test to verify failure**

**Step 3: Write minimal implementation**

Set `Storage.ExternalPath` on the evolver's PlanManager storage to the
resolved config value. Do NOT change resolvePlanDir's default — human plans
keep their behavior. Create the sink dir lazily at first write.

**Step 4: Run test to verify pass**
Run: `go test ./internal/daemon/ -run TestEvolverPlanSink -v`
Expected: PASS

### Task 4: Docs

**Objective:** The knob is documented (AGENTS.md wiring requirement).

**Files:**
- Modify: the config docs file covering skills/evolver settings (locate the
  canonical one — docs/configuration/ or similar)

**Step 1: Write the entry**

`skills.evolver.plan_dir`: default, what lands there, that repo
docs/plans is for human-authored plans only. 3-6 lines.

**Step 2: Verify**

Cross-check the documented default matches Task 1's normalization.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] resolvePlanDir's default behavior for human plans is UNCHANGED
      (existing plan manager tests green, unmodified)
- [ ] No tilde expansion hand-rolled; existing helper used
- [ ] Fixture tests use temp HOMEs — never the developer's real home

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (config key, default, provenance
      fields)
- [ ] Human-authored plan flow untouched (regression tests green)
- [ ] The daemon construction path uses the sink; no other PlanManager
      construction site silently changed
- [ ] Provenance encoding matches the existing plan file format (no
      sidecar, no parallel format)
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The 17 existing uncommitted `docs/plans/skill-evolution-*.md` files are
  EVIDENCE, not cleanup work. Do not move or delete them.
- This repo's own plan trees under `docs/plans/` (including this one) are
  human-authored and MUST keep working exactly as before.
- If the evolver constructs its own PlanManager rather than receiving one
  from the daemon, wire the sink at that construction site instead — the
  invariant is "evolver plans land in the sink," not "components.go edits."
