# Bughunt 2026-11-17 — two-week work review (2026-11-03..2026-11-17)

Wave anchor: d69a1ad8 (last audited point, 2026-11-15 two-run report).
HEAD at audit: 11a00045. Scope = d69a1ad8..11a00045 (162 commits).
Prior reports: 2026-11-15-bughunt-two-run.md, 2026-11-14-bughunt-week.md.
Baseline: `go build ./...` clean at HEAD; main checkout clean; sibling
sessions active in separate worktrees (lint-debt, antipythony sweeps) — do NOT
touch other worktrees or stash/checkout on the shared tree.

Method: read-only auditors over disjoint scopes -> parent source-verifies HIGHs
at file:line -> isolated fixers (disjoint file ownership) -> parent gate matrix ->
concern-split commits with explicit pathspecs. Fixing authorized by request
("fix all issues you encounter, validate them, commit them").

## Baseline findings (parent, independent of auditors)

- B1 (production, pre-existing since ~2026-09-27, file external to the 2-week
  window): `internal/project/store.go:389` `formatTime` uses RFC3339Nano whose
  fractional part is variable-width; `ListProjects` ORDER BY created_at DESC
  (store.go:166) compares those strings lexicographically. Probe proved
  `...20Z` sorts as newer than `...20.4Z` and `...20.1Z` newer than `...20.12Z`.
  Symptom: `TestListProjects` failed in full suite (projects[0].ID=p2 want p3)
  but passed 10/10 in isolation. Clean fix: emit fixed-width 9-digit fractional
  timestamps (parseTime with RFC3339Nano accepts them). Note existing rows may
  need a normalize pass; new rows correct.
- B2 (test-side race, pre-existing): `internal/plan/handler_stepfailed_test.go:114`
  polls until `FailedSteps==3` then checks phase state, but `OnStepFailed`
  (manager.go:1345) increments then calls SetPhaseState in a second step — the
  test can observe the intermediate state. Symptom: state=in_progress in full
  suite, passed 10/10 in isolation. Fix: poll until state==PhaseFailed too.

Both were encountered in baseline; will be batched with window fixes. Auditors
spawned focused sub-auditors; tree has a sibling editing
internal/agent/turnstate.go, internal/llm/posture.go, internal/tui/models/turn.go
(left untouched).

## Status

- [x] Scope + baseline
- [ ] Auditor wave 1 (6 auditors + sub-auditors running)
- [ ] Parent HIGH verification
- [ ] Fix wave
- [ ] Gate matrix
- [ ] Report / disposition table
