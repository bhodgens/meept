# Handoff Document — GitHub-Intake-Factory Tree (Leaf 06) — CLOSED

**Status: COMPLETE.** Leaf 06 (project-projection) and its deferred daemon
wiring seam both landed. The github-intake-factory tree (00T, 01-06) is
closed. Do not resume this handoff; it is kept as a record.

## What Was Done

### Leaf 06 implementation — `internal/intake/project.go` (commit 77a52be6)

- `NewProjector(pool, ledger, projectNumber, repo, slog.Handler)` — the
  logger param is a `slog.Handler` (the sibling `NewWriteBack` convention);
  nil degrades to `slog.DiscardHandler`.
- `EnsureProjection(ctx, ticket, planID, phases)` and
  `EnsureProjectionWithSteps(ctx, ticket, planID, phases, stepsByPhase)` —
  idempotent ticket issue -> phase sub-issue -> step sub-issue tree. One
  effects key per phase
  (`effects.EffectKey("intake_projection", planID, phaseID)`); the receipt
  (`phase_issue`, `phase_node_id`, `phase_item_id`, `steps[]`) is the
  correlation store — no new column, no new table.
- `MirrorState(ctx, planID, phaseID, state)` — GraphQL
  `updateProjectV2ItemFieldValue` on the board's `State` single-select
  field. Fail-soft: every error logs Warn and returns nil, so a mirror
  failure never fails the meept-side transition that triggered it.
- Transport: sub-issue creation uses the raw
  `client.NewRequest(ctx, method, url, body)` / `client.Do(req, v)` escape
  hatch on `POST /repos/{o}/{r}/issues/{parent}/sub_issues` (go-github v93
  has no typed sub-issue create). The two board writes (add item, set
  State) use `github.com/shurcooL/githubv4` on the pool's OWN authenticated
  `*http.Client` (`client.Client()`), with the endpoint derived from the
  pool's base URL — one token, no second auth surface, no `internal/secrets`
  import.
- Deps: `github.com/shurcooL/githubv4` (direct) +
  `github.com/shurcooL/graphql` (indirect). `go mod tidy` clean.

### Leaf 06 integration line — daemon wiring (commit 7cf4ac73)

- `Components.IntakeProjector` (internal/daemon/components.go).
- `wireIntakePoller` builds it when `[intake].project_number > 0` AND the
  effects ledger is wired; `project_number` 0 (the shipped default) skips
  silently.
- `wireIntakeProjectionSubscriptions` arms two bus subscribers:
  - `plan.executing` -> `projectPlanFromEvent`: resolve
    plan -> goal -> `TriggerRef intake:<repo>#<ticket>`, then
    `EnsureProjectionWithSteps` (real `task.TaskStep` lists from
    `StepStore` when reachable, synthesized steps from each phase's
    `TotalSteps` counter otherwise). Panic-recovered + fail-soft, same
    posture as the poll tick.
  - `plan.phase_state` -> `MirrorState`.
- `PlanManager` publishes the new `plan.phase_state` topic at its three
  phase-state transitions (in_progress on synthesize, completed on
  phase-completion detection and on phase task completion) and gains a
  `GetPhases` read-through.
- Docs: `docs/workflows/intake.md` projection section + operator notes on
  the board and its `State` field; AGENTS.md Intake row;
  `make graphs` regenerated (the `plan.phase_state` edge is in
  `docs/generated/bus-topology.md`).

## Gate Results (all verified by execution)

- `go test -p 2 -race ./internal/intake/ -run TestProjector -v` — 4/4 PASS
  (`TestProjector_EnsureProjection_Creates`,
  `TestProjector_ReceiptReuse_Idempotent`, `TestProjector_MirrorState`,
  `TestProjector_MirrorFailSoft`).
- `go test -p 2 -race ./internal/intake/ ./internal/plan/ ./internal/effects/`
  — all ok.
- `go test -tags intake_gate -p 2 -count=1 -race ./internal/daemon/ -run TestIntakeWiring`
  — ok.
- `make e2e-fast-area AREA=github-intake` — ok (35.8s; 31.9s inside the
  pre-commit gate run).
- `go build ./...`, `go vet`, `gofmt -l`, `make graphs-check`,
  `mutexio` + `predid` analyzers — all clean.

## Deviations From The Plan Doc (recorded)

1. **Auth transport.** The plan suggested an `oauth2` static token source
   for GraphQL. The implementation rides the pool's already-authenticated
   `*http.Client` instead, so `go.mod` carries only the two shurcooL deps
   and no new auth path.
2. **Step list source.** `plan.PlanPhase` carries only `TotalSteps`, no
   step list, so `EnsureProjection` synthesizes step sub-issues from the
   counter. `EnsureProjectionWithSteps` accepts the real `task.TaskStep`
   lists when the caller can reach `StepStore` (the daemon wiring does).
3. **Sub-issue create shape.** `POST .../sub_issues` is called with
   `{title, body}` (create-and-parent in one request, the shape the gate
   fake speaks). Real GitHub's endpoint takes `{issue_number}` to parent
   an existing issue; the two-step form is the follow-up if the hosted
   API rejects the combined body.

## Known Pre-Existing Failures (NOT from this tree)

- `e2e/suites/turn-journal` `TestKill9NoDuplicateCompletion` —
  `query jobs: SQL logic error: no such table: jobs`. Reproduced with this
  diff stashed. Needs its own investigation.
- `e2e/suites/step-pipeline`
  `TestNeedsInfoReviewVerdictPausesForHuman` — "task never finalized after
  the needs_info verdict". Passed standalone (20.1s) on the same tree;
  flakes under the gate's parallel suite load.

## Commits

`2f2c45c1` (00T gate) · `d4e2d1b2` (01) · `d58d5cbc` (02+03) ·
`9edc4c85`/`0d081d84` (04, sibling wave) · `5dc732e5` (05) ·
`77a52be6` (06 implementation) · `7cf4ac73` (06 integration line).
