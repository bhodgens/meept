# GitHub PR + CI Tools (Issue #64) — Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) run the test gate, (2) dispatch implementation agents,
> (3) review their work in-session, (4) re-dispatch if incomplete,
> (5) track completion. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 7 (1 test-gate leaf, 6 implementation leaves)
- **Scope:** Close the L2 work loop: meept can open a PR, read CI status,
  wait for it to go green, comment/review, merge on authorization, and read
  GitHub issues — via `google/go-github` v93, with irreversible calls wrapped
  in the effects ledger.
- **Tracking issue:** bhodgens/meept#64

## Goal

Meept's git tooling stops at commit (`internal/tools/builtin/git_commit.go`
and siblings). There is no PR lifecycle and no CI read-back, so plan gates
are local commands only. This tree adds a GitHub service layer
(`internal/githubsvc/`) plus seven builtin tools so that "ticket -> spec ->
tested code -> PR -> CI green -> merge" becomes one agent-executable loop,
and "CI green" becomes a plan gate command.

## Architecture

- One new package `internal/githubsvc/` owns the authenticated client pool,
  token resolution, and the base-URL seam used by tests. It depends on
  `google/go-github/v93` (BSD-3-Clause, requires Go 1.26 — matches go.mod).
- Tools live in `internal/tools/builtin/` following the existing
  `GitCommitTool` pattern: Name/Description/Parameters/Execute +
  `tools.ToolDefaults`.
- Irreversible effects (PR create, comment, review, merge, issue comment)
  wrap their call in `internal/effects.Run` with content-derived keys.
- `github_ci_wait` polls to terminal state; on timeout the turn parks and a
  scheduler cron instruction re-arms the poll (no busy loop).
- No new auth machinery: the token comes from the secrets broker or an
  OAuth provider slot; requests go through the standard HTTP client seam so
  the egress policy keeps enforcing.

## Interface Contracts

All contracts below are FROZEN. Leaves implement to these exact shapes.

### Contract 1: Client pool — `internal/githubsvc/pool.go`

```go
package githubsvc

// TokenSource supplies the GitHub token. Implemented by the secrets-backed
// source in this package; tests pass a static source.
type TokenSource interface {
    Token(ctx context.Context) (string, error)
}

type ClientPoolOptions struct {
    Tokens  TokenSource
    // BaseURL overrides https://api.github.com (GitHub Enterprise + tests).
    BaseURL string
    // HTTPClient plugs the daemon's egress-policy transport in. nil = default.
    HTTPClient *http.Client
}

type ClientPool struct { /* lazy per-identity clients, mutex-guarded */ }

func NewClientPool(opts ClientPoolOptions) (*ClientPool, error)
// For resolves the token, builds (or reuses) a *github.Client for the
// identity implied by owner, and returns it.
func (p *ClientPool) For(ctx context.Context, owner, repo string) (*github.Client, error)
func (p *ClientPool) Close()
```

Owner: 01-client-pool.md. Consumers: 02, 03, 04, 05.

### Contract 2: Tool names and registration

```
github_pr_create    {owner?, repo?, base, head, title, body, draft?}
github_pr_status    {owner?, repo?, pr}         -> PRStatus (JSON, below)
github_ci_wait      {owner?, repo?, pr, timeout_seconds?}
github_pr_comment   {owner?, repo?, pr, body}
github_pr_review    {owner?, repo?, pr, body, event?}  // event: APPROVE|REQUEST_CHANGES|COMMENT
github_pr_merge     {owner?, repo?, pr, strategy?}     // merge|squash|rebase
github_issue_get    {owner?, repo?, number}
github_issue_comment{owner?, repo?, number, body}
```

`owner`/`repo` optional: default from config (`[github] default_owner`,
`default_repo`). All tools embed `tools.ToolDefaults`; only
`github_pr_status`, `github_ci_wait`, `github_issue_get` override
`IsReadOnly` to true. Owner: 02 (pr tools), 03 (ci_wait), 04 (issue tools).
Consumer: 05-wiring-registration.md.

### Contract 3: PRStatus summary shape — `internal/githubsvc/status.go`

```go
type CheckRun struct {
    Name       string `json:"name"`
    Status     string `json:"status"`     // queued|in_progress|completed
    Conclusion string `json:"conclusion"` // success|failure|neutral|...
}
type WorkflowRun struct {
    Name       string `json:"name"`
    Status     string `json:"status"`
    Conclusion string `json:"conclusion"`
}
type PRStatus struct {
    Number     int           `json:"number"`
    State      string        `json:"state"`       // open|closed
    Draft      bool          `json:"draft"`
    Mergeable  string        `json:"mergeable"`   // mergeable|conflicting|unknown
    Head       string        `json:"head"`        // head SHA
    Checks     []CheckRun    `json:"checks"`
    Workflows  []WorkflowRun `json:"workflows"`
    AllGreen   bool          `json:"all_green"`   // every check+workflow concluded success
}
```

Owner: 03-ci-wait.md (github_pr_status tool consumes it; 02's create
returns the created PR number in `{"number": N, "url": ...}`).

### Contract 4: Effects keys (content-derived, `effects.EffectKey`)

```
github_pr_create:     EffectKey("github_pr_create", owner, repo, head, title)
github_pr_comment:    EffectKey("github_pr_comment", owner, repo, pr, sha256(body))
github_pr_review:     EffectKey("github_pr_review", owner, repo, pr, event, sha256(body))
github_pr_merge:      EffectKey("github_pr_merge", owner, repo, pr, head)
github_issue_comment: EffectKey("github_issue_comment", owner, repo, number, sha256(body))
```

`EffectMeta{Tool: <tool name>, SessionID: <from ctx if available>,
ProviderIdempotent: false}` — GitHub accepts no idempotency key, so stuck
records surface for human reconciliation. Receipt = the API response's
relevant fields (number/html_url/merged) marshaled. Owner: 02, 04 apply it.

### Contract 5: Config — `[github]` block in `internal/config/schema.go`

```go
type GitHubConfig struct {
    Enabled      bool   `json:"enabled"`        // default false (opt-in)
    TokenSecret  string `json:"token_secret"`   // name in the secrets broker
    DefaultOwner string `json:"default_owner"`
    DefaultRepo  string `json:"default_repo"`
    BaseURL      string `json:"base_url,omitempty"` // Enterprise / test override
}
```

Owner: 05-wiring-registration.md. Consumers: 01 (reads via constructor args,
not config directly).

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (test gate) | none | 45K | gate |
| 01 | 01-client-pool.md | leaf | none (after gate) | 55K | A |
| 02 | 02-pr-tools.md | leaf | 01 | 75K | B |
| 03 | 03-ci-wait.md | leaf | 01 | 70K | B |
| 04 | 04-issue-tools.md | leaf | 01 | 50K | B |
| 05 | 05-wiring-registration.md | leaf | 01,02,03,04 | 60K | C |
| 06 | 06-e2e-suite.md | leaf | 01,02,03 | 55K | C |

**Concurrency groups:** gate first; A alone; B = {02,03,04} in parallel;
C = {05,06} in parallel after B.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 0: Test Gate (MANDATORY — before ANY implementation dispatch)

1. Dispatch `00T-gate-tests.md` via `delegate_task`:
   - Goal: "Write ALL unit + gate tests from 00T-gate-tests.md — tests only,
     no implementation."
   - Context: full 00T text + all Interface Contracts INLINED + coding
     conventions below.
   - Include: "Write tests ONLY. Do NOT write or modify implementation files.
     Tests must fail RED for missing-implementation reasons — run once to
     confirm. Do NOT commit. Do NOT run git add."
2. Control-agent validation (IN-SESSION, main model — never delegated):
   run the tests, confirm compile + RED-for-the-right-reason, map every test
   to a Test Gate matrix row, apply the delta protocol (update the LESSER
   artifact to reflect the GREATER).
3. Commit gate tests:
   `git add internal/githubsvc/ internal/tools/builtin/ e2e/ && git commit -m "test(github): gate tests for github-pr-ci tree (F1-F8)"`
   (stage only paths the gate touched — explicit paths, never `git add .`).
4. Tracking table: 00T -> TESTS_VALIDATED. Only then dispatch Phase 1.

### Phase 1: Dispatch Concurrency Group A

1. Read 01-client-pool.md, dispatch via `delegate_task`:
   - Context: full leaf text + Contracts 1, 3, 5 + conventions + "Gate tests
     already exist and are committed. Make them pass. Do NOT delete, skip,
     weaken, or rewrite any gate test; report suspected-wrong tests instead."
   - Include: "Do NOT commit. Do NOT run git add. Do NOT use read_file on
     existing source files — use search_files or terminal cat. After writing
     a file, do NOT read it back."

### Phase 2: Dispatch Concurrency Group B (parallel batch)

Dispatch 02-pr-tools.md, 03-ci-wait.md, 04-issue-tools.md simultaneously
(same dispatch-context boilerplate as Phase 1, with Contracts 1-4 inlined).
Each dispatch context adds: "01-client-pool.md is implemented and committed;
internal/githubsvc exists with NewClientPool/For per Contract 1."

### Phase 3: Dispatch Concurrency Group C (parallel batch)

Dispatch 05-wiring-registration.md and 06-e2e-suite.md with the same
boilerplate. 05 gets the note: "all tools exist; wire them, do not
re-implement."

### Phase 4: Review and Commit Each Child

- Review IN-SESSION (main model, never a delegated reviewer — children
  inherit delegation.model). Read changed files, check against leaf spec +
  contracts + Review Checklist below, run `go build ./... &&
  go test -p 2 ./internal/githubsvc/... ./internal/tools/builtin/... -race`.
- Gaps -> re-dispatch with specific feedback (max 3 cycles).
- Pass -> stage the leaf's EXACT file list, commit
  `feat(github): implement <leaf name>`, update tracking table -> REVIEWED.

### Phase 5: Integration Review

After all children REVIEWED:
1. `go build -o bin/meept-daemon ./cmd/meept-daemon && go vet ./internal/githubsvc/... ./internal/tools/builtin/...`
2. `go test -p 2 ./internal/githubsvc/... ./internal/tools/builtin/... -race`
3. `make e2e-fast-area AREA=github-pr-ci` (after 06 lands; manifest entry required)
4. gofmt all changed files; corruption check:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/githubsvc/ internal/tools/builtin/` returns zero
5. Commit integration fixes (if any); all children -> COMPLETE; report COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied exactly (signatures, types, keys)
- [ ] All specified files at exact paths
- [ ] Committed gate tests pass GREEN; `git diff` shows no gate test
      modified, weakened, skipped, or deleted (any such diff = NEEDS_FIXES)
- [ ] Tests written and passing (TDD)
- [ ] Irreversible tools wrap in effects.Run with Contract 4 keys
- [ ] No secrets logged at info level (token never printed)
- [ ] No scope creep; no debug artifacts (no fmt.Println, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes)
- [ ] Coding conventions followed (below)

## Coding Conventions

- **Language:** Go 1.26 (toolchain pinned in go.mod; do not bump).
- **Naming:** exported PascalCase, unexported camelCase; tool structs
  `<Name>Tool` mirroring `GitCommitTool`.
- **Imports:** stdlib / third-party / module-local groups; no unused imports.
- **Error handling:** wrap with `%w`; sentinels exported where callers must
  `errors.Is` (e.g. `ErrNoToken`); never panic in library code; no ignored
  errors (pre-commit enforced).
- **Testing:** stdlib + repo's existing test style in neighboring files
  (`internal/tools/builtin/*_test.go`); table-driven where inputs vary.
- **Formatting:** gofmt before reporting completion.
- **No `time.Now().UnixNano()`/`math/rand` for IDs — `pkg/id.Generate()`
  (predid analyzer enforces).**
- **Tests hermetic:** no network in unit tests; the fake GitHub server is
  `httptest.NewServer` behind `ClientPoolOptions.BaseURL`.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests.md | COMPLETE | 2 | Gate tests authored by 2 subagents (unit + e2e), validated RED-for-right-reasons in-session; landed with their leaves (one-gate deviation). |
| 01-client-pool.md | COMPLETE | 1 | 2acdcc4f. Pool/PRStatus/token/rearm types; go-github v93 API break (NewClient error-return, Mergeable *bool, Merge PUT) found here and forwarded to all later leaves. |
| 02-pr-tools.md | COMPLETE | 1 | 302f643d. 4 PR tools effects-wrapped; 2 gate-test mechanical v93 fixes (mergeable bool, PUT merge verb), no assertion weakened. |
| 03-ci-wait.md | COMPLETE | 1 | 18b57b24. pr_status + ci_wait (poll 2-15s doubling, fast-fail, park via ReArmSink, pkg/id waitID); channil-class close-then-nil caught by hook in leaf 05's ticker, fixed there. |
| 04-issue-tools.md | COMPLETE | 1 | f69bb034. issue get (read-only) + comment (effects-wrapped, body-digest key part). Test restored unmodified. |
| 05-wiring-registration.md | COMPLETE | 1 | ed1ecabd. [github] config block, RegisterGitHubTools behind init-populated ToolFactories seam (breaks builtin<->githubsvc cycle), daemon wiring fail-closed, InstructionReArmSink + 30s ticker, CLI `meept github status`, docs/workflows/github.md + AGENTS.md row. |
| 06-e2e-suite.md | COMPLETE | 1+1 | 1d89fb4a + ce291e9a (fence fix, see delta log). Hermetic suite GREEN (3/3 scenarios) against the real scratch daemon. |

Tree COMPLETE 2026-10-26. All gates green at close: `make test` 11026/0 fail,
`make e2e-fast-area AREA=github-pr-ci` 3/3 PASS, `make graphs-check` +
`make docs-check` green (5cb8d806, 46e585f3, 96a53fed).

Adversarial execution review 2026-10-26 (001a5d5f): five plan-omission
defects fixed post-COMPLETE (see Delta log); `go build ./...` clean,
`go test -race` on both packages green, e2e suite green under the
pre-commit e2e-affected gate (22 suites) at commit time.

## Integration Test Plan

1. Cross-contract: `github_pr_create` output `{"number","url"}` feeds
   `github_ci_wait {pr}` in one hermetic e2e scenario (06).
2. Effects: kill-and-restart simulation — claim a `github_pr_create` key,
   crash before execute, restart, verify reconcile surfaces (not re-executes)
   because ProviderIdempotent=false.
3. Park/re-arm: unit-level in 03; e2e verifies a timed-out wait leaves a
   resumable turn and a scheduler instruction row.
4. Full suite: `go test -p 2 ./... -short` green; `make e2e-fast-area
   AREA=github-pr-ci` green.
5. `make graphs` after RPC/HTTP/WS surface changes (registration adds HTTP
   routes only if 05 adds any — run it if so; graphs-check is a CI gate).

## Test Gate

Test leaf: 00T-gate-tests.md — dispatched in Phase 0, validated by the
control agent in-session. One gate leaf for the whole tree (deviation from
one-per-implementation-leaf, justified: contracts are shared and all gate
tests live in three packages; splitting would create cross-gate import
cycles in RED state).

| # | Feature (from plan) | Source (leaf/spec §) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------|-----------|-----------|--------|
| F1 | ClientPool builds/reuses clients; BaseURL + HTTPClient honored | 01 Tasks 1-2 | TestClientPool_BuildsAndReuses, TestClientPool_BaseURLOverride, TestClientPool_HTTPClientPlumbed | e2e fake-server smoke (scn 01) | VALIDATED |
| F2 | Token resolution from secrets broker; ErrNoToken when missing | 01 Task 3 | TestClientPool_ErrNoToken (unit); broker/file source wiring in leaf 05 daemon test + scn 02 | n/a (secret plumbing) | VALIDATED |
| F3 | github_pr_create + effects claim (no dup on same key) | 02 Tasks 1-2 | TestGitHubPRCreateTool, TestGitHubPRCreateTool_EffectsKey | e2e create-then-recreate = scn 03 (reconcile) | VALIDATED |
| F4 | github_pr_status aggregates checks + workflows, AllGreen | 03 Task 1 | TestSummarizePR_AllGreen/NotGreenWhenCheckFails/NotGreenWhenWorkflowCancelled + TestGitHubPRStatusTool | e2e status-after-create (scn 01) | VALIDATED |
| F5 | github_ci_wait polls to terminal; timeout parks turn + re-arm | 03 Tasks 2-3 | TestGitHubCIWaitTool_TerminalGreen/_ParkOnTimeout/_FastFail | e2e wait-green (scn 01: 2nd poll) | VALIDATED |
| F6 | github_pr_comment/review/merge wrapped in effects.Run | 02 Task 3 | TestGitHubPRCommentTool, TestGitHubPRReviewTool, TestGitHubPRMergeTool | n/a (covered by F3 pattern) | VALIDATED |
| F7 | github_issue_get + github_issue_comment (effects on comment) | 04 Tasks 1-2 | TestGitHubIssueGetTool, TestGitHubIssueCommentTool | n/a | VALIDATED |
| F8 | Tools registered when [github].enabled; absent when disabled | 05 Task 1 | TestGitHubToolRegistration (+TestInstructionReArmSink) | e2e disabled-by-default (scn 02) | VALIDATED |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED
Delta log:
- 2026-10-26, F8/F1-F7, plan->impl: go-github v93 API break (NewClient
  returns error + functional options; Mergeable *bool; Merge is PUT; checks
  path /commits/{sha}/check-runs). All leaves built against the real v93
  surface; gate-test edits were mechanical, no assertion weakened. No
  contract shape changed.
- 2026-10-26, F1-F7, plan->impl: e2e revealed a security-fence gap the plan
  did not anticipate: github_* tools are fence-denied "Unknown action"
  because ToolActionMap (internal/agent/executor.go) has no entry and
  BuiltinRules has no rule. ce291e9a registers all eight as
  network_request (RiskLow, external-API class, transcript_fetch/json_extract
  precedent) and the completeness guard now pins them. Suite flipped 2/3->3/3
  green.
- 2026-10-26, F8, impl->test: e2e scn 01 terminal-text defect - a
  SetPostToolText claiming a side effect with zero tool executions in that
  iteration trips the review step's unbacked-claim nudge, the fake LLM
  repeats the text, the convergence guard aborts the turn (empty reply).
  Fixed by removing the claiming terminal text (assertions read tool
  results, not the terminal text). Test-only, no assertion weakened.
- 2026-10-26 (adversarial execution review), F3/F6/F7 impl->plan: five
  defects found and fixed post-COMPLETE (001a5d5f): (1) C4 SessionID
  stamping omitted on all five effects claims; (2) C4 effect payloads
  omitted (reconcile had no input to re-drive a stuck claim); (3)
  github_pr_review reported success for reviews that did not land (4xx
  rejections now recorded-and-surfaced like merge; 2xx non-terminal
  review states no longer read as success); (4) leaf-03's Retry-After
  requirement was never implemented (secondary rate limits now honored
  in the poll loop); (5) parked-wait re-arm rows leaked (cron rows fired
  daily forever after resume) — ReArmSink gains CancelReArm, wired via
  the new resume_wait_id arg on terminal green/fast-fail. Also: the
  deadline park now embeds the last observed PRStatus (leaf 03 Task 3
  said to; the zero-value was shipped instead).
- 2026-10-26 (gap-closure wave), residuals from the review above, all
  landed and verified: (6) e2e scenario 1 gained the leaf-06 <= 6
  poll-ceiling assertion and a new github-pr-ci-04 park scenario asserts
  parked:true + wait_id AND the persisted github_ci_wait-<waitID>
  instruction row end-to-end (6de0db27 — Integration Test Plan item 3
  is now actually covered); (7) the [secrets]-broker token path was
  dead — Broker.ChildValue returns only the placeholder, so
  SecretsTokenSource could never authenticate; fixed with an in-process
  Broker.Value accessor and a token test pinning all four leaf-00T
  cases including a RESOLVING real value (903ce3b1); (8) the re-arm
  prompt now instructs the resuming agent to pass resume_wait_id so the
  CancelReArm lifecycle engages on real resumes; pinned in
  TestInstructionReArmSink (07575ae6). The two 00T gate tests that were
  never written (TestSecretsTokenSource_Resolves, TestReadOnlyFlags)
  now exist.

## Structural Completeness Check (Before Dispatch)

After writing every document in this tree, run:

    python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py plans/github-pr-ci --strict-leaves

Re-run until `ALL TREES COMPLIANT: True`, THEN commit the tree.

## Open Questions

All were resolved at issue-authoring time (bhodgens/meept#64) and are
frozen into the contracts. Defaults applied without a user decision, listed
for review before dispatch:

1. go-github v93 pinned (latest, Go 1.26-compatible). Impact if reversed:
   v92 also works; contract shapes unchanged.
2. Token via secrets broker first; OAuth provider slot deferred. Impact:
   OAuth-only installs need a follow-up to add an oauth-backed TokenSource.
3. One gate-test leaf for the whole tree instead of one per implementation
   leaf (deviation from the default, justified in ## Test Gate).
4. Workflow-runs paging capped at one page filtered by head SHA. Impact:
   repos with >50 concurrent workflow runs on one SHA may miss rows.
5. Merge-tool not-mergeable responses Complete the effect (attempt
   happened) rather than Abandon. Human reconciliation never auto-retries
   merges.

## Notes

- e2e policy (AGENTS.md): new-feature tests go in the hermetic e2e tier —
  06 adds `e2e/suites/github-pr-ci/` + `e2e/manifest.json` path_map entry in
  the same leaf. Pre-commit check [18/18] blocks the package without it.
- `internal/secrets` Broker.ChildValue(name) resolves the token secret; do
  not add new secret storage.
- The merge tool rides the existing PreExecChecker/enforcement path — no new
  authority machinery in this tree (that is issue #65's broker).
- Known gotcha: go-github v93 requires Go >= 1.26 — go.mod already pins
  1.26.5; do not touch GOTOOLCHAIN.
- Rate limits: honor `Retry-After` on secondary limits via
  `client.Rate` checks + backoff in 03's poll loop; do not hammer the API.
- macOS full-sweep test runs MUST use `-p 2` (port exhaustion — AGENTS.md).
