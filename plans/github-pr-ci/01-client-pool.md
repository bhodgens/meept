# GitHub Service Client Pool — Implementation Leaf (01)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass. Do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> tests to the orchestrator instead). Do NOT commit. Do NOT use read_file
> on existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** `internal/githubsvc/` package: token sources, client pool,
  ErrNoToken, PRStatus types + SummarizePR.
- **Dependencies:** none (first implementation leaf)
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

Create the GitHub service layer every other leaf consumes: an authenticated
`go-github` client pool with token resolution from the secrets broker, a
base-URL seam for tests, and the `PRStatus` aggregation from Contract 3.

## Context

Meept is a Go 1.26 agent daemon. Built-in agent tools follow the
`tools.Tool` interface (internal/tools/interface.go) — this leaf does NOT
write tools, only the service package. The secrets broker
(internal/secrets/broker.go) exposes `ChildValue(name)` for resolving named
secrets; the daemon wires brokers in internal/daemon/components.go.

Key files to understand before implementing:
- internal/tools/builtin/git_commit.go — the tool-struct pattern siblings use
- internal/secrets/broker.go — ChildValue + IsPlaceholder
- go.mod — add `github.com/google/go-github/v93`

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 1 verbatim (ClientPool, TokenSource, ClientPoolOptions,
NewClientPool, For, Close) plus:

```go
// internal/githubsvc/token.go
type SecretsTokenSource struct{ Broker *secrets.Broker; SecretName string }
func (s *SecretsTokenSource) Token(ctx context.Context) (string, error)
// returns ErrNoToken when the secret is missing or a placeholder

var ErrNoToken = errors.New("githubsvc: no GitHub token resolved")

// internal/githubsvc/status.go — PRStatus/CheckRun/WorkflowRun structs
// per Contract 3, plus:
func SummarizePR(pr *github.PullRequest, checks []*github.CheckRun,
    workflows []*github.WorkflowRun) PRStatus
```

### What This Leaf Consumes

```
github.com/google/go-github/v93/github   (add to go.mod via go get)
internal/secrets.Broker.ChildValue(name) (string, error)
```

## Tasks

### Task 1: Package skeleton + pool

**Objective:** ClientPool per Contract 1 with lazy per-owner client caching.

**Files:**
- Create: `internal/githubsvc/pool.go`
- Test: existing `pool_test.go` gate tests

Steps: go get go-github/v93; implement pool (sync.Mutex map keyed by
owner; `For` resolves token each call via TokenSource, reuses the client
when the token is unchanged; honor BaseURL via `github.NewClient(&http.Client{...})
+ client.BaseURL = *url.Parse(BaseURL)`; honor HTTPClient). Close() empties
the map.

Run: `go test ./internal/githubsvc/ -run TestClientPool -v` -> PASS.

### Task 2: SecretsTokenSource + ErrNoToken

**Objective:** token resolution with fail-closed error.

**Files:**
- Create: `internal/githubsvc/token.go`
- Test: `TestSecretsTokenSource_Resolves`, `TestClientPool_ErrNoToken`

Placeholder/empty -> `fmt.Errorf("%w: secret %q missing", ErrNoToken, name)`.

### Task 3: PRStatus + SummarizePR

**Objective:** Contract 3 structs and aggregation.

**Files:**
- Create: `internal/githubsvc/status.go`
- Test: `TestPRStatusAggregation`

AllGreen = every check AND workflow has Conclusion == "success" (missing
conclusion = not green). Head = `pr.GetHead().GetSHA()`.

### Task 4: RegistrationOptions type (stub for leaf 05)

**Objective:** the type the registration gate test references.

**Files:**
- Create: `internal/githubsvc/register.go` (type only this leaf)

```go
type RegistrationOptions struct {
    Pool         *ClientPool
    DefaultOwner string
    DefaultRepo  string
    WorkingDir   string
}
```

## Self-Verification Checklist

- [ ] Gate tests `go test ./internal/githubsvc/ -race` PASS
- [ ] Contract 1 + extensions implemented exactly (signatures, ErrNoToken
      wrapping with `%w`)
- [ ] go.mod/go.sum updated via go get only; no toolchain bump
- [ ] gofmt clean; go vet clean; no unused code (U1000 blocks pre-commit)
- [ ] No network in unit tests (httptest only)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] All gate tests F1, F2 green
- [ ] Contracts 1, 3 verbatim
- [ ] No client per-request rebuild (cache hit path tested)
- [ ] No token logging

## Notes

- go-github `BaseURL` must end with a trailing slash — append if missing.
- Do not add retry/rate-limit machinery here; leaf 03 handles backoff in
  its poll loop.
