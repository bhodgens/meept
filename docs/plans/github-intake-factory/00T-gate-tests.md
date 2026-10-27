# Intake Gate Tests — Test-Gate Leaf (00T)

> **For the implementing agent:** You are the test-gate implementer.
> Write the tests listed below ONLY. Do NOT write or modify implementation
> files. Tests must fail RED for missing-implementation reasons — run them
> once to confirm. Do NOT commit. Do NOT run git add. Do NOT use read_file
> on existing source files — explore with search_files or terminal cat.

## Meta

- **Parent:** master.md
- **Scope:** Full unit + gate test suite for contracts F1-F7, ahead of any
  implementation.
- **Dependencies:** none (tests-first)
- **Estimated Context:** 45K
- **Concurrency Group:** gate

## Goal

Write every gate test for the github-intake-factory tree so RED state
proves which contract is unimplemented. Tests live in two packages plus
the e2e tier: `internal/intake/`, `internal/config/`, and
`e2e/suites/github-intake/`.

## Interface Contracts (From Parent)

Copy all four contracts from master.md verbatim: Poller/Ticket/Filter
(C1), Linker/IntakeKey (C2), WriteBack/Outcome (C3), `[intake]` config
(C4). Tests assert these EXACT shapes.

## Tasks

### Task 1: intake package tests (F1, F2, F3, F4, F5)

**Files:** Create `internal/intake/poller_test.go`, `internal/intake/linkage_test.go`, `internal/intake/writeback_test.go`

Construct components with a githubsvc pool whose BaseURL points at an
httptest fake implementing the exercised endpoints (reuse the fake
shapes from `internal/tools/builtin/github_*_test.go` as style
reference; package-local fake is fine). Assert:

- `TestGitHubPoller_LabelGate` — fake serves issues with mixed labels;
  only issues carrying ALL required labels are returned (F1).
- `TestGitHubPoller_Watermark` — second Poll returns only issues
  updated after the first poll's watermark (F1).
- `TestFilter_Matches` — table-driven: empty filter matches all;
  case-insensitive label match; missing one of several labels fails.
- `TestLinker_EnsureGoal_Creates` — with a stubbed employee manager
  surface (see Note on the manager seam), a ticket creates a goal with
  Source="trigger" and TriggerRef `intake:<repo>#<id>` (F2).
- `TestIntakeKey_Stable` — `IntakeKey(repo, id, body)` is equal for
  equal inputs and differs when any part differs; the body component is
  sha256 (F3).
- `TestLinker_Dedup_IdenticalBody` — second EnsureGoal with the same
  body returns the same goal, makes no second CreateGoal call (F3).
- `TestLinker_Redispatch_OnBodyChange` — changed body updates the
  goal's mandate and resets state/health; no second goal (F4).
- `TestWriteBack_Report_Comment` — posts a comment via the fake;
  effects key = `intake_writeback` + (repo, ticketID, state-goalID)
  per Contract 3; receipt id/url surfaced (F5).
- `TestWriteBack_HealthyAddsLabel` — State "healthy" adds the done
  label; other states do not (F5).
- `TestWriteBack_NoStorm` — second Report with the same
  (goalID, state) makes no second POST (effects key collision) (F5).

### Task 2: config tests (F6)

**Files:** Create `internal/config/intake_test.go`

- `TestIntakeConfig_Parse` — a full `[intake]` block parses into
  `Config.Intake` with every field of Contract 4.
- `TestIntakeConfig_DurationRoundTrip` — CRITICAL (master Contract 4
  note): save a config with `poll_interval = "5m"`, reload it, assert
  the field is still the string "5m" (the json5 duration-rewrite pass
  must not corrupt it), AND a real duration-typed field elsewhere in
  the config still converts. Follow the two-sided regression pattern of
  the `queue.interactive_window` precedent (grep `quotedDurationToNanos`
  in the loader for the exemption mechanism).

### Task 3: e2e gate tests (F2, F3, F4, F5, F7)

**Files:** Create `e2e/suites/github-intake/main_test.go`, update `e2e/manifest.json`
(path_map entry `internal/intake/` + `internal/config/` -> suite
`github-intake`; suite registration).

Scenarios (skeleton + assertions only; the harness fake GitHub pattern
lives in `e2e/suites/github-pr-ci/fake` — replicate the needed routes):
- intake-create-wait: daemon with `[intake]` enabled pointed at the
  fake; a labeled issue exists; the poll fires (interval short enough
  for the suite budget); assert the goal exists with Source=trigger
  (via the employee API surface the harness exposes) and the fake saw
  the issues GET.
- intake-dedup: second poll cycle with unchanged issue -> goal count
  unchanged.
- intake-redispatch: issue body changes between polls -> goal mandate
  updated, still exactly one goal.
- intake-writeback: goal reaches healthy -> fake sees a comments POST +
  labels POST; second cycle -> no second comments POST.
- intake-disabled: no [intake] block -> no poll activity (fake counter
  stays at zero).

## Self-Verification Checklist

Before reporting completion, verify:
- [ ] All listed tests exist at exact paths with exact names
- [ ] `go test ./internal/intake/ ./internal/config/` compiles and fails
      RED with missing-implementation errors only — no test-logic failures
- [ ] e2e suite + manifest entry written; suite compiles under `-tags e2e`
- [ ] No implementation files created or modified
- [ ] Tests assert CONTRACT shapes (C1-C4), not invented fields

**DO NOT COMMIT.** The orchestrator handles all git operations after
control-agent validation.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Control Agent)

- [ ] Every Test Gate matrix row (F1-F7) has >= 1 named test
- [ ] RED reasons are missing-implementation only
- [ ] No implementation files touched (`git status` clean of non-test paths)
- [ ] Contracts asserted verbatim

## Notes

- The employee.Manager seam: EnsureGoal needs CreateGoal + goal
  lookup-by-TriggerRef. If constructing a full Manager in unit tests is
  heavy, define a package-local minimal interface in the TEST files'
  expectations matching Contract 2's usage (CreateGoal, ListGoals or a
  TriggerRef scan) — the implementation leaf then satisfies it. Do NOT
  invent a production interface in this leaf; write tests against the
  Contract 2 shape and note the seam adaptation as a deviation.
- Effects ledger stub: in-memory `effects.MemoryLedger` if it exists in
  internal/effects, else a minimal stub in the test files.
