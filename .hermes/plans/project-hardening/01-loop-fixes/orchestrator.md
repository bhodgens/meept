# Orchestrator: Loop Fixes (Items 2 + 5)

## Scope

Two fixes that both modify `internal/agent/loop.go`. They must be serialized
because they touch adjacent code regions.

## Interface Contracts

Both leaves modify `loop.go` but in different functions:
- Leaf 01: `buildTerminateResponse` (~line 5686)
- Leaf 02: `resolveProjectInfo` (~line 4227) and `gitCurrentBranch`/`gitIsDirty`

No shared types or contracts between them.

## Child Index

| # | Document | Est. Context | Dependencies |
|---|----------|-------------|--------------|
| 1a | `01-terminate-json.md` | ~25K | None |
| 1b | `02-git-probe-cache.md` | ~35K | 1a (both modify loop.go) |

## Dispatch Protocol

1. Dispatch leaf 1a (terminate-json). Wait for completion + review.
2. Dispatch leaf 1b (git-probe-cache). Wait for completion + review.
3. Commit both.

## Review Checklist

- [ ] `buildTerminateResponse` formats non-string results as natural language
- [ ] No raw `json.Marshal` output reaches the user
- [ ] Git probe cache has 5s TTL, thread-safe (sync.RWMutex or atomic)
- [ ] Cache key includes workingDir (different dirs = different cache entries)
- [ ] No mutex held across subprocess I/O
- [ ] `go build ./...` passes
- [ ] `go test ./internal/agent/...` passes

## Coding Conventions

- Go: follow CLAUDE.md (nil-guard setters, no mutex across I/O, lowercase UI text)
- Tests in `_test.go` files alongside source
- New RPC methods registered in `RegisterProjectMethods`

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-terminate-json | COMPLETE 2026-07-29 100% | formatToolResult replaces raw json.Marshal |
| 02-git-probe-cache | COMPLETE 2026-07-29 100% | 5s TTL, sync.RWMutex, per-dir keying |

## Integration Test Plan

- Verify `buildTerminateResponse` with a mock non-string result returns formatted text
- Verify `resolveProjectInfo` called twice within 5s returns cached data (no subprocess)
- Verify cache invalidates after 5s

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Rewrite buildTerminateResponse formatting | 01-terminate-json.md | (pending) | (pending) | PENDING |
| F2 | Add formatToolResult helper | 01-terminate-json.md | (pending) | (pending) | PENDING |
| F3 | Test | 01-terminate-json.md | (pending) | (pending) | PENDING |
| F4 | Add a TTL cache for git probe results | 02-git-probe-cache.md | (pending) | (pending) | PENDING |
| F5 | Use cache in resolveProjectInfo | 02-git-probe-cache.md | (pending) | (pending) | PENDING |
| F6 | Test | 02-git-probe-cache.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)
