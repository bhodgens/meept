# Plan -> GitHub Project Projection — Implementation Leaf (06)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> weaken them. Do NOT commit. Explore with search_files or terminal cat,
> not read_file. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** `internal/intake/project.go`: mirror a synthesized plan's
  phase hierarchy into a GitHub Projects V2 project as sub-issues of
  the ticket issue, with state mirroring.
- **Dependencies:** 02 (effects patterns), 04 (config exists)
- **Estimated Context:** 60K
- **Concurrency Group:** D (alone — owns the githubv4 dependency and the
  go.mod/go.sum changes; no sibling leaf edits manifests or go.mod)

## Goal

Make the plan hierarchy a GitHub-native artifact: when an intake goal's
plan synthesizes, its phases (and their steps) appear as sub-issues of
the ticket issue inside a GitHub Project, with a State field mirroring
meept's phase/step states. The effects receipts are the correlation
store — no new columns, no new tables.

## Context

Meept's hierarchy to project (verified shapes):
- `Plan` (internal/plan/plan.go:52) carries `GoalID`.
- `PlanPhase` (internal/plan/plan.go:85) carries `PlanID`, `Name`,
  `Sequence`, step counters, `State PhaseState`.
- `TaskStep` (internal/task/step.go:202) carries `TaskID`,
  `Description`, `DependsOn`, `State StepState`, `Sequence`.

GitHub platform facts (binding for this leaf):
- Projects V2 is GraphQL-ONLY. go-github (REST) cannot create projects
  or items. This leaf adds `github.com/shurcooL/githubv4` +
  `github.com/shurcooL/graphql` (MIT) to go.mod — the ONLY leaf allowed
  to touch go.mod.
- The graphql client authenticates with an oauth2 static token source
  fed from the same resolution path the pool uses: construct with
  `oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
  AccessToken: tok}))` where tok comes from the pool's TokenSource
  (expose it via a small accessor on ClientPool if needed — one method,
  `TokenSource() TokenSource`, noted as a deviation if added). Never
  import internal/secrets.
- Sub-issues: REST `POST /repos/{o}/{r}/issues/{n}/sub_issues` (the
  v93 client may lack this endpoint — use the raw
  `client.NewRequest`/`client.Do` escape hatch with the exact REST
  path; record the deviation). Max hierarchy depth 8 (ticket=1,
  phase=2, step=3 — well under).
- Project item add: GraphQL `addProjectV2ItemById`. Project field
  update: `updateProjectV2ItemFieldValue` with a single-select field
  (pre-create the "State" field + options once at wiring; the projector
  looks up its field/option node IDs lazily and caches them on the
  struct).
- Draft items CANNOT parent — everything is a real sub-issue.

Key files:
- internal/intake/writeback.go — effects + 4xx recorded-outcome pattern
- internal/intake/linkage.go — TriggerRef conventions, IntakeKey shape
- internal/plan/plan.go — PlanPhase/PhaseState
- internal/task/step.go — TaskStep/StepState

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 5 verbatim: `Projector`, `NewProjector`, `EnsureProjection`,
`MirrorState`. Frozen details:

- Phase sub-issue title: `phase:<sequence> <phase name>`.
- Phase sub-issue body MUST contain `meept-plan:<planID> phase:<phaseID>`
  (the GitHub-side reverse-lookup anchor) plus a one-line summary.
- Step sub-issue title: `step:<sequence> <description truncated to 72
  runes>`; body carries `meept-plan:<planID> phase:<phaseID>
  step:<stepID>` and, when present, a `Depends on: <step titles>` list.
- State field options map 1:1 from PhaseState/StepState strings
  (pending/ready/scheduled/running/ + completed/failed per the real
  enums — read the actual PhaseState/StepState constants and mirror
  them exactly; do not invent states).
- Effects keys: `EffectKey("intake_projection", planID, phaseID)` per
  sub-issue-create batch (one effect per phase; step sub-issues are
  part of the phase effect — created in the same claimed region, so a
  crash replays the whole phase idempotently via receipt reuse).
  Receipt: `{"phase_issue": N, "phase_node_id": "...", "steps": [{"id":
  "...", "issue_number": N}]}`.

### What This Leaf Consumes

```
github.com/shurcooL/githubv4          (ADD to go.mod — this leaf only)
github.com/caimlas/meept/internal/effects
github.com/caimlas/meept/internal/plan  (PhaseState — read-only types)
github.com/caimlas/meept/internal/task   (StepState — read-only types)
```

## Tasks

### Task 1: githubv4 wiring + State field bootstrap (F8)

**Objective:** GraphQL client on the pool's token; State field exists.

**Files:**
- Create: `internal/intake/project.go`
- Modify: `go.mod`/`go.sum` via `go get github.com/shurcooL/githubv4`
- Test: gate `TestProjector_EnsureProjection_Creates`

Bootstrap: lazily resolve (and create if missing) the "State"
single-select field on the configured project, caching node IDs on the
Projector. All mutations are effects-wrapped.

Run: `go test ./internal/intake/ -run TestProjector -v` -> PASS.

### Task 2: EnsureProjection (F8)

**Objective:** idempotent ticket -> phase -> step sub-issue tree.

**Files:**
- Modify: `internal/intake/project.go`
- Test: gate `TestProjector_ReceiptReuse_Idempotent`

Order: create phase sub-issue (REST sub-issues endpoint via
NewRequest/Do) -> add to project (GraphQL) -> create step sub-issues
(children of the phase sub-issue) -> add each to project. Then
Complete the phase effect with the full receipt. Receipt reuse short-
circuits everything.

### Task 3: MirrorState (F9)

**Objective:** state transitions mirror soft.

**Files:**
- Modify: `internal/intake/project.go`
- Test: gate `TestProjector_MirrorState`, `TestProjector_MirrorFailSoft`

Look up the receipt for (planID, phaseID) -> GraphQL field-value update
to the matching State option. Any GraphQL error: log Warn with the item
and state (no secret material), return nil — a mirror failure must
never fail the meept-side transition that triggered it.
`TestProjector_MirrorFailSoft` asserts exactly that.

## Self-Verification Checklist

- [ ] Gate tests F8, F9 PASS with `-race`
- [ ] go.mod/go.sum changes are ONLY the two shurcooL deps
- [ ] Receipt reuse makes EnsureProjection fully idempotent (double-call
      produces zero new issues — counter-asserted)
- [ ] No internal/secrets import (audit script clean)
- [ ] GraphQL mutations never panic; errors are effects-claimed or soft
- [ ] gofmt/vet clean; no unused code; no body content in logs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified
- [ ] Effects keys byte-identical to Contract 5; receipts carry node IDs
- [ ] Step sub-issues parented to the PHASE issue (depth check)
- [ ] Bodies carry the meept-plan/phase/step anchors (grep them)
- [ ] State options mirror the REAL PhaseState/StepState constants
- [ ] MirrorState fail-soft verified by test
- [ ] Only this leaf touched go.mod

## Notes

- GraphQL in tests: shurcooL/graphql accepts an arbitrary HTTP client —
  point it at an httptest server speaking minimal JSON for the few
  mutations used. No live network.
- If the sub-issues REST endpoint is absent from v93 AND NewRequest/Do
  proves awkward, the fallback is `shurcooL/githubv4`'s
  `addSubIssue`-equivalent mutation (verify the exact name in the v4
  schema docs before writing) — record whichever was used.
- This leaf does NOT wire the projection trigger into the daemon
  (leaf 05's wiring gains one call when the plan synthesizes — a
  follow-up integration line in 05's file, patched by the orchestrator
  at integration review, or a 2-line addition here if 05 has already
  landed its wiring seam; coordinate through the committed seam, not by
  re-editing 05's files).
