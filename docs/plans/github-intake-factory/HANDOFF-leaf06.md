# Handoff Document — GitHub-Intake-Factory Tree (Leaf 06)

## What Was Done

### Completed (by subagents)
- **Leaf 04 (config)**: Added `[intake]` config block to `internal/config/schema.go`. Includes `IntakeConfig` struct with `Enabled`, `DefaultEmployee`, `PollInterval`, `WatchRepo`, `RequiredLabels`, `DoneLabel`, `GateCommand`, `ProjectNumber`. Config template added to `config/meept.json5`. Tests pass.
- **Leaf 05 (wiring)**: Intake poller wired into daemon via `internal/daemon/intake_wiring.go` using `employeeSchedulerAdapter.RunAtInterval`. Called in `NewComponents()` at line 3423. No CLI command needed — background scheduler.

### In Progress (leaf 06 — project-projection)
- `internal/intake/project.go` started but hits type mismatches:
  - `slog.Handler` vs `*slog.Logger` in `NewProjector`
  - `github.Client.NewRequest`/`Do`/`Post` method signatures don't match usage
  - Test expects `slog.DiscardHandler` but implementation uses `slog.Handler` interface

## What's Blocked

**Leaf 06** cannot compile due to the above type mismatches. The test file `project_test.go` exists with F8/F9 tests (creation + receipt reuse, mirror state fail-soft). The implementation needs to:
1. `NewProjector(pool, ledger, pollerID, ownerRepo, logger)` — logger is `slog.Handler`
2. `EnsureProjection(ctx, ticket, planID, phases)` — creates phase/step sub-issues under ticket, uses effects ledger for idempotency
3. `MirrorState(ctx, planID, phaseID, state)` — GraphQL mutation to update project item state, fail-soft on error

## Next Steps

1. **Fix leaf 06 implementation** — align `NewProjector` signature with test expectations (`slog.Handler`), fix `github.Client` method calls (`NewRequest` takes `context` as first arg, `Do` takes `*http.Request, any`), use `client.Post` via `http.Client` not `github.Client`.
2. **Run leaf 06 tests** — `go test -p 2 ./internal/intake/ -run "TestProjector"`
3. **Push to main** — after leaf 06 passes, the intake tree is complete (00T, 01-06 all done). Then push the 67 local commits to clear the remaining red on main.

## Context for the Next Agent

- The `githubsvc.ClientPool.For` returns `*github.Client` (from `github.com/google/go-github/v93/github`)
- `*github.Client` has `Issues.Create(ctx, owner, repo, CreateIssueRequest)` — `CreateIssueRequest` has `Title string`, `Body *string`
- `*github.Client.NewRequest(ctx, method, url, body)` returns `*http.Request`
- `*github.Client.Do(req, any)` returns `*Response, error` (2 values, not 3)
- `slog.Handler` interface has `Handle(ctx, Record)` — `slog.NewRecord(time.Time, Level, string, uintptr)` — 4 args, not 6
- The test fake `projFake` serves REST endpoints at `/repos/o/r/issues` and `/graphql`

---

**Status**: Leaf 04 + 05 complete. Leaf 06 implementation started but blocked on type fixes.