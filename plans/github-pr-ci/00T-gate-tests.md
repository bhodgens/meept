# GitHub PR+CI Gate Tests — Test-Gate Leaf (00T)

> **For the implementing agent:** You are the test-gate implementer.
> Write the tests listed below ONLY. Do NOT write or modify implementation
> files. Tests must fail RED for missing-implementation reasons — run them
> once to confirm. Do NOT commit. Do NOT run git add. Do NOT use read_file
> on existing source files — explore with search_files or terminal cat.

## Meta

- **Parent:** ../master.md
- **Scope:** Full unit + gate test suite for contracts F1-F8, ahead of any
  implementation.
- **Dependencies:** none (tests-first)
- **Estimated Context:** 45K
- **Concurrency Group:** gate

## Goal

Write every gate test for the github-pr-ci tree so that RED state proves
which contract is unimplemented. Tests live in three packages:
`internal/githubsvc/`, `internal/tools/builtin/`, `e2e/suites/github-pr-ci/`.
The e2e suite follows `e2e/suites/` conventions (build tag `e2e`, hermetic,
fake GitHub via httptest started inside the suite).

## Interface Contracts (From Parent)

Copy all five contracts from master.md verbatim: ClientPool (C1), tool
names/params (C2), PRStatus shape (C3), effects keys (C4), `[github]`
config block (C5). Tests assert these EXACT shapes.

## Tasks

### Task 1: githubsvc unit tests (F1, F2)

**Files:** Create `internal/githubsvc/pool_test.go`, `internal/githubsvc/status_test.go`

Tests (table-driven where inputs vary):
- `TestClientPool_For_BuildsAndReuses` — two `For(ctx,"owner","repo")`
  calls return the same `*github.Client` pointer.
- `TestClientPool_BaseURLOverride` — `BaseURL` pointing at an
  `httptest.NewServer`; a probe request hits that server (assert via a
  handler flag), NOT api.github.com.
- `TestClientPool_HTTPClientPlumbed` — custom `*http.Client` transport
  records the request; pool uses it.
- `TestSecretsTokenSource_Resolves` — TokenSource returns the secret value.
- `TestClientPool_ErrNoToken` — empty token -> `errors.Is(err, ErrNoToken)`.
- `TestPRStatusAggregation` (status_test.go, against C3 struct + a
  summarizer func the leaf 03 will expose as
  `SummarizePR(*github.PullRequest, []*github.CheckRun, []*github.WorkflowRun) PRStatus`)
  — mixed check states -> `AllGreen=false`; all success -> `AllGreen=true`.

### Task 2: builtin tool tests (F3-F7)

**Files:** Create `internal/tools/builtin/github_pr_test.go`,
`github_ci_wait_test.go`, `github_issue_test.go`

Construct tools with a pool whose BaseURL points at an httptest fake
implementing the exercised endpoints. Assert:
- `TestPRCreateTool` — C2 params; fake returns 201 with number 7; tool
  returns `{"number":7,"url":...}`.
- `TestPRCreateEffectsKey` — run twice with identical args against a fake
  effects ledger (in-memory `effects.Ledger` stub): second call returns the
  prior receipt, provider called once (handler counter == 1).
- `TestPRStatusAggregation` via the `github_pr_status` tool end-to-end.
- `TestCIWaitTerminal` — fake returns completed/success immediately.
- `TestCIWaitTimeoutParks` — fake returns in_progress forever with
  timeout_seconds=1; assert error wraps a park/re-arm sentinel and a
  re-arm instruction row is created (stub the scheduler sink).
- `TestPRCommentEffects`, `TestPRMergeEffects` — effects key derivation
  per C4 (assert ledger keys, not just outcomes); merge strategy default
  "merge".
- `TestIssueGetTool`, `TestIssueCommentEffects` — same pattern.
- `TestReadOnlyFlags` — C2's read-only set is exactly
  {github_pr_status, github_ci_wait, github_issue_get}.

### Task 3: registration + e2e gate tests (F5, F8)

**Files:** Create `internal/tools/builtin/github_register_test.go`,
`e2e/suites/github-pr-ci/main_test.go`, update `e2e/manifest.json` (path_map
entry `internal/githubsvc/` + `internal/tools/builtin/` -> suite
`github-pr-ci`).

- `TestGitHubToolRegistration` — registry with `[github].enabled=true`
  contains all 8 tools; `enabled=false` contains none. (Config struct C5
  may not exist yet — write the test against a `RegistrationOptions`
  signature pinned here:
  `RegisterGitHubTools(r *tools.Registry, opts githubsvc.RegistrationOptions) error`.)
- `TestE2ECreateWaitGreen` (e2e tag) — suite spins its own fake GitHub
  (httptest in the harness process), daemon config `[github].base_url`
  pointed at it; chat turn "open a PR for branch X and wait for CI" ->
  PR created + wait returns green.

## Self-Verification Checklist

Before reporting completion, verify:
- [ ] All listed tests exist at exact paths with exact names
- [ ] `go test ./internal/githubsvc/ ./internal/tools/builtin/` compiles
      and fails RED with "undefined: githubsvc.*" / tool-not-registered
      errors only — no test-logic failures
- [ ] e2e suite + manifest entry written; suite compiles under `-tags e2e`
- [ ] No implementation files created or modified
- [ ] Tests assert CONTRACT shapes (C1-C5), not invented fields

**DO NOT COMMIT.** The orchestrator handles all git operations after
control-agent validation.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Control Agent)

- [ ] Every Test Gate matrix row (F1-F8) has >= 1 named test
- [ ] RED reasons are missing-implementation only
- [ ] No implementation files touched (`git status` clean of non-test paths)
- [ ] Contracts asserted verbatim

## Notes

- Effects ledger stub: implement a minimal in-memory `effects.Ledger` in the
  test files only (production Ledger is SQLite-backed elsewhere).
- If `tools.Registry` import shape differs from expectation, adapt the test
  to the real signature and note the deviation — do not invent APIs.
