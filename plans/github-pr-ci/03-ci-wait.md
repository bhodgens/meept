# CI Status + Wait/Park Tool — Implementation Leaf (03)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Gate tests EXIST and are committed — make them pass; do NOT weaken them.
> Do NOT commit. Explore with search_files or terminal cat, not read_file.

## Meta

- **Parent:** ../master.md
- **Scope:** github_pr_status tool + github_ci_wait with park/re-arm.
- **Dependencies:** 01-client-pool.md
- **Estimated Context:** 70K
- **Concurrency Group:** B

## Goal

The read-side of the loop: one call summarizes every check run and workflow
run on a PR (Contract 3), and a second polls to terminal state with
bounded backoff, parking the turn and re-arming via the scheduler on
timeout instead of blocking the agent loop.

## Context

Meept turns can park and resume (internal/agent/budget_resume.go —
`Park(turn ParkedTurn)`; parked turns re-enter the loop on a watcher
signal). Cron re-arming rides the instruction scheduler
(internal/scheduler/instructions.go `SyncCronInstructions` — trigger
prefix `cron:`). This leaf defines its own small re-arm sink interface so
it does not import the daemon wiring; leaf 05 supplies the real sink.

Key files:
- internal/agent/budget_resume.go — ParkedTurn shape
- internal/scheduler/instructions.go:24-70 — instruction -> job shape
- internal/githubsvc/status.go — SummarizePR (from 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/github_ci.go
func NewGitHubPRStatusTool(pool *githubsvc.ClientPool, defaults OwnerRepo) *GitHubPRStatusTool
func NewGitHubCIWaitTool(pool *githubsvc.ClientPool, defaults OwnerRepo,
    sink ReArmSink) *GitHubCIWaitTool

// ReArmSink schedules the cron re-arm for a parked CI wait.
type ReArmSink interface {
    // ScheduleReArm(pr Ref, at time.Time, waitID string) error
    // — writes a `cron:` instruction row; leaf 05 wires the real impl.
    ScheduleReArm(ref CIRef, at time.Time, waitID string) error
}

type CIRef struct {
    Owner  string `json:"owner"`
    Repo   string `json:"repo"`
    Number int    `json:"number"`
}
```

Polling constants (this leaf, exported for tests):
`pollInterval = 15s` (doubling, cap 60s), secondary-rate-limit
`Retry-After` honored via response header on 403/429.

### What This Leaf Consumes

```
githubsvc.ClientPool.For, SummarizePR, PRStatus   (from 01)
githubsvc.OwnerRepo.Resolve                        (from 02)
```

## Tasks

### Task 1: github_pr_status tool

**Objective:** F4 — aggregate checks + workflows into PRStatus.

**Files:**
- Create: `internal/tools/builtin/github_ci.go` (status tool)
- Test: gate `TestPRStatusAggregation` (tool-level), e2e smoke

Fetch: PR (`PullRequests.Get`) + `Checks.ListCheckRunsForRef` (head SHA,
list all pages up to 100) + `Actions.ListWorkflowRunsForRepo` filtered to
`head_sha == pr head` (single page, 50). Override `IsReadOnly` -> true.

Run: `go test ./internal/tools/builtin/ -run TestPRStatus -v` -> PASS.

### Task 2: github_ci_wait — terminal polling

**Objective:** F5 terminal half.

**Files:**
- Modify: `internal/tools/builtin/github_ci.go`
- Test: gate `TestCIWaitTerminal`

Loop: SummarizePR every pollInterval; return PRStatus the moment AllGreen
OR any conclusion in {failure, cancelled, timed_out, action_required}
(fast-fail beats waiting out the timeout). Respect
`timeout_seconds` (default 600, cap 3600) via ctx deadline.

### Task 3: timeout -> park + re-arm

**Objective:** F5 park half.

**Files:**
- Modify: `internal/tools/builtin/github_ci.go`
- Create: `internal/githubsvc/rearm.go` (CIRef + sink plumbing helpers if
  needed)
- Test: gate `TestCIWaitTimeoutParks`

On deadline: build `waitID = pkg/id.Generate("ciwait_")`, call
`sink.ScheduleReArm(ref, now+backoff, waitID)`, return an error wrapping
`ErrCIWaitParked` (new sentinel, this leaf) with the PRStatus snapshot
embedded in the tool result so the resuming turn sees current state.
The re-armed instruction re-invokes `github_ci_wait` with the same args.

## Self-Verification Checklist

- [ ] Gate tests F4 + F5 PASS with `-race`
- [ ] Fast-fail set implemented; not waiting out failures
- [ ] Backoff caps at 60s; Retry-After honored
- [ ] waitID via pkg/id.Generate (never time/rand — predid)
- [ ] Read-only: status + wait both true
- [ ] gofmt/vet clean; no unused code

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified
- [ ] PRStatus matches Contract 3 byte-for-byte (json tags)
- [ ] Park path cannot busy-loop (deadline always set)
- [ ] No API hammering: interval schedule verified in tests

## Notes

- Checks API: `client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, opts)`.
- Workflows API pagination is deliberately capped at one page — a PR's
  relevant runs share the head SHA; deeper paging is noise for this tool.
