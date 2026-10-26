# PR Lifecycle Tools — Implementation Leaf (02)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT use
> read_file on existing source files — explore with search_files or
> terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ../master.md
- **Scope:** github_pr_create, github_pr_comment, github_pr_review,
  github_pr_merge tools, all effects-wrapped.
- **Dependencies:** 01-client-pool.md (ClientPool exists and is committed)
- **Estimated Context:** 75K
- **Concurrency Group:** B

## Goal

The write-side of the PR loop: create, comment, review, merge. Each
irreversible call claims an effects key first (Contract 4), executes only
when granted, records the API outcome as a receipt, and completes.

## Context

Tool pattern: `internal/tools/builtin/git_commit.go` (Name/Description/
Parameters/Execute + `tools.ToolDefaults` embedded). Effects protocol:
`internal/effects/effects.go` `Run(ctx, ledger, key, meta, fn)` — read the
doc comment at effects.go:110 before wiring. Registration into the daemon
registry is leaf 05's job; this leaf only defines constructors.

Key files:
- internal/tools/builtin/git_commit.go — constructor + JSON-schema pattern
- internal/effects/effects.go — Run + EffectMeta + ProviderIdempotent=false
- internal/githubsvc/pool.go — ClientPool.For(ctx, owner, repo)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/github_pr.go
func NewGitHubPRCreateTool(pool *githubsvc.ClientPool, ledger effects.Ledger,
    defaults OwnerRepo) *GitHubPRCreateTool
func NewGitHubPRCommentTool(pool *githubsvc.ClientPool, ledger effects.Ledger,
    defaults OwnerRepo) *GitHubPRCommentTool
func NewGitHubPRReviewTool(pool *githubsvc.ClientPool, ledger effects.Ledger,
    defaults OwnerRepo) *GitHubPRReviewTool
func NewGitHubPRMergeTool(pool *githubsvc.ClientPool, ledger effects.Ledger,
    defaults OwnerRepo) *GitHubPRMergeTool

// internal/githubsvc/owner_repo.go (type owned here, lives in githubsvc)
type OwnerRepo struct{ Owner, Repo string }
// Resolve(ownerArg, repoArg string) (owner, repo string, err error)
// — falls back to defaults; error when neither present.
```

Params per Contract 2 (C2). Output of create: `{"number": N, "url": ...}`.

### What This Leaf Consumes

```
githubsvc.ClientPool.For / ErrNoToken          (from 01)
effects.Run / EffectKey / EffectMeta           (existing package)
```

## Tasks

### Task 1: OwnerRepo + github_pr_create

**Objective:** create tool with effects guard per F3.

**Files:**
- Create: `internal/githubsvc/owner_repo.go`
- Create: `internal/tools/builtin/github_pr.go` (create tool)
- Test: gate `TestPRCreateTool`, `TestPRCreateEffectsKey`

Effects key `EffectKey("github_pr_create", owner, repo, head, title)`;
`ProviderIdempotent: false`; receipt = `{"number","html_url"}` marshaled.
Fail-closed: `ErrNoToken` and resolution errors happen BEFORE the claim.

Run: `go test ./internal/tools/builtin/ -run TestPRCreate -v` -> PASS.

### Task 2: comment + review tools

**Objective:** F6's comment/review half.

**Files:**
- Modify: `internal/tools/builtin/github_pr.go`
- Test: gate `TestPRCommentEffects`

Body sha256 via `hex.EncodeToString(sha256.Sum256(...)[:])` in the key per
C4. Review `event` param defaults to "COMMENT"; validate against the
allowed set before claiming.

### Task 3: merge tool

**Objective:** merge with strategy default "merge", effects-guarded.

**Files:**
- Modify: `internal/tools/builtin/github_pr.go`
- Test: gate `TestPRMergeEffects`

Key includes `head` (C4) so a re-merge after new commits derives a fresh
key. Receipt = `{"merged": bool, "sha": ..., "message": ...}`. On
405/409 (not mergeable) record the receipt, Complete, and return the
provider error as a normal tool error — do NOT Abandon (the attempt
happened; the ledger should reflect it).

## Self-Verification Checklist

- [ ] Gate tests F3 + F6's comment/merge tests PASS
- [ ] All four tools effects-wrapped with C4 keys exactly
- [ ] ErrNoToken/resolution errors precede any effects claim
- [ ] JSON schemas valid (Parameters()); names per C2
- [ ] gofmt + go vet clean; no unused code; no token in any log/error text

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified (`git diff` empty on test files)
- [ ] Effects keys byte-identical to C4
- [ ] ProviderIdempotent=false on every effect
- [ ] Read-only flags NOT overridden on these tools (they mutate)

## Notes

- go-github merge: `client.PullRequests.Merge(ctx, owner, repo, number, msg)`.
- Never log request bodies containing tokens; go-github auth is
  transport-level so bodies are safe — do not add token logging anywhere.
