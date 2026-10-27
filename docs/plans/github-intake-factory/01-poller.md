# GitHub Issues Poller — Implementation Leaf (01)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass. Do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> tests to the orchestrator instead). Do NOT commit. Do NOT use read_file
> on existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** `internal/intake/` package skeleton: Ticket/Filter types,
  the Poller interface, and the GitHubPoller adapter on the #64
  githubsvc client pool.
- **Dependencies:** none (first implementation leaf)
- **Estimated Context:** 50K
- **Concurrency Group:** A

## Goal

The provider-agnostic intake seam: anything that can list "tickets
changed since last time" can feed the factory. This leaf delivers the
interface and its first implementation (GitHub Issues, label-gated) on
the existing client pool.

## Context

Meept is a Go 1.26 agent daemon. The #64 tree shipped
`internal/githubsvc` (client pool, token sources, OwnerRepo). This leaf
consumes that pool — it must NEVER import `internal/secrets` or touch
`secrets.Broker` (scripts/audit-secrets-value-callers.py enforces; the
pool resolves tokens itself).

Key files to understand before implementing:
- internal/githubsvc/pool.go — ClientPool.For(ctx, owner, repo)
- internal/tools/builtin/github_issue.go — go-github Issues API usage
- go.mod — google/go-github/v93 already pinned

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 1 verbatim: `Ticket`, `Filter`, `Poller`, plus:

```go
// internal/intake/github.go
func NewGitHubPoller(pool *githubsvc.ClientPool, repo githubsvc.OwnerRepo,
    filter Filter, logger *slog.Logger) *GitHubPoller

// Poll implements Poller. Lists repo issues updated since the watermark
// (state=all, sorted=updated, direction=desc), filters by label gate,
// maps to Ticket. First poll (zero watermark) returns all matching.
// Watermark = max UpdatedAt seen (persisted in-memory on the struct;
// daemon restarts re-scan once — acceptable for v1, the dedup layer
// makes it a no-op).
func (p *GitHubPoller) Poll(ctx context.Context) ([]Ticket, error)
```

### What This Leaf Consumes

```
github.com/google/go-github/v93/github   (already in go.mod)
githubsvc.ClientPool.For                 (from #64)
```

## Tasks

### Task 1: Package skeleton + types

**Objective:** Contract 1 types compile; Filter matching is table-tested.

**Files:**
- Create: `internal/intake/poller.go` (Ticket, Filter, Poller, Filter.Matches)
- Test: gate `TestFilter_Matches`

Run: `go test ./internal/intake/ -run TestFilter -v` -> PASS.

### Task 2: GitHubPoller

**Objective:** label-gated listing with watermark.

**Files:**
- Create: `internal/intake/github.go` (GitHubPoller + Poll)
- Test: gate `TestGitHubPoller_LabelGate`, `TestGitHubPoller_Watermark`

Use `client.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{
    State: "all", Sort: "updated", Direction: "desc", Labels: ...})`.
Map `*github.Issue` -> Ticket via getters (GetNumber -> strconv.Itoa,
GetTitle, GetBody, GetLabels->names, GetHTMLURL, GetUser().GetLogin(),
GetUpdatedAt()).
Skip entries where `issue.IsPullRequest()` — the issues API returns PRs
too; intake tickets are issues only.
Watermark: keep the max UpdatedAt; second poll passes
`Since: watermark` to the list options.

Run: `go test ./internal/intake/ -run TestGitHubPoller -v` -> PASS.

### Task 3: Fake server shared helper

**Objective:** the package-local fake the gate tests use stays package-local.

**Files:**
- Modify: `internal/intake/poller_test.go` ONLY if the gate-test fake
  needs a route the poller legitimately requires. Do NOT change
  assertions.

## Self-Verification Checklist

- [ ] Gate tests F1 `go test ./internal/intake/ -race` PASS
- [ ] Contract 1 implemented exactly (types, signatures)
- [ ] No `internal/secrets` import (audit script clean)
- [ ] go.mod untouched (go-github already pinned)
- [ ] gofmt clean; go vet clean; no unused code (U1000 blocks pre-commit)
- [ ] No network in unit tests (httptest only)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests F1 green; no gate test modified
- [ ] Contract 1 verbatim
- [ ] PRs excluded from poll results
- [ ] Watermark advances only after a successful, non-empty scan
- [ ] No token logging; no secrets import

## Notes

- Pagination: one page (PerPage: 50) is fine for v1; a repo with >50
  updated-since matches loses rows — same accepted trade as #64's
  workflow paging cap. Note it in the type doc.
