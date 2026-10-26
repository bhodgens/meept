# GitHub Issue Tools — Implementation Leaf (04)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Gate tests EXIST and are committed — make them pass; do NOT weaken them.
> Do NOT commit. Explore with search_files or terminal cat, not read_file.

## Meta

- **Parent:** ../master.md
- **Scope:** github_issue_get + github_issue_comment (effects-wrapped).
- **Dependencies:** 01-client-pool.md
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

The intake read-side and its write-back half. Issue get feeds ticket
context into agent turns (issue #67's poller will reuse the same client
pool); issue comment lets the agent report outcomes back to the ticket.

## Context

Smallest leaf. Same tool pattern as 02. The comment tool is the only
effects-wrapped tool here (get is read-only).

Key files:
- internal/tools/builtin/git_commit.go — tool pattern
- internal/githubsvc/pool.go — ClientPool

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/github_issue.go
func NewGitHubIssueGetTool(pool *githubsvc.ClientPool,
    defaults OwnerRepo) *GitHubIssueGetTool
func NewGitHubIssueCommentTool(pool *githubsvc.ClientPool,
    ledger effects.Ledger, defaults OwnerRepo) *GitHubIssueCommentTool
```

Params per C2: `{owner?, repo?, number}` / `{owner?, repo?, number, body}`.
Issue get output: `{number, title, state, author, labels, body, url}`.

### What This Leaf Consumes

```
githubsvc.ClientPool.For          (from 01)
githubsvc.OwnerRepo.Resolve       (from 02)
effects.Run / EffectKey           (existing)
```

## Tasks

### Task 1: github_issue_get

**Objective:** F7 read half.

**Files:**
- Create: `internal/tools/builtin/github_issue.go`
- Test: gate `TestIssueGetTool`

`client.Issues.Get`; map fields via getters (`issue.GetTitle()` etc.);
labels -> []string via GetNames. `IsReadOnly` -> true. 404 -> clean
"issue not found" error (no stack spam).

### Task 2: github_issue_comment

**Objective:** F7 write half with effects.

**Files:**
- Modify: `internal/tools/builtin/github_issue.go`
- Test: gate `TestIssueCommentEffects`

Key `EffectKey("github_issue_comment", owner, repo, number, sha256(body))`
per C4; receipt = `{"id", "html_url"}` from the created comment.

## Self-Verification Checklist

- [ ] Gate tests F7 PASS
- [ ] Key derivation byte-identical to C4
- [ ] Read-only flag true ONLY on issue_get
- [ ] gofmt/vet clean; no unused code

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified
- [ ] Output shape matches contract
- [ ] No scope creep (no label/edit/close tools)

## Notes

- Do NOT add issue creation or state changes — intake direction is one-way
  in this tree (write-back = comments only). #67 owns the poller.
