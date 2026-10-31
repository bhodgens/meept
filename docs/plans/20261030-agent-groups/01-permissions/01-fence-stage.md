# 01: Fence Stage in pkg/security - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles git after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, never read back.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Port the dead engine's integrated fence stage
  (internal/security/engine.go:427-461 shape) into
  pkg/security.PermissionChecker as a first-class stage; per-session fence
  injection in the daemon; REMOVE the per-tool SetFenceChecker opt-in wiring.
- **Dependencies:** 00T-tests.md (TESTS_VALIDATED)
- **Estimated Context:** 60K
- **Concurrency Group:** A (SERIALIZED with 02, 03 - shared files)

## Goal

Fence enforcement stops depending on each tool remembering to consult its
injected checker. Every file/shell permission check runs the fence stage
centrally, with a per-session fence instance (never the shared mutable
singleton), and the per-tool wiring is deleted.

## Interface Contracts (From Parent)

C1 (verbatim): FenceStage interface {CheckPath(path, op string) error;
CheckCommand(command, workDir string) error}; SetFence typed-nil guarded;
stage order = after path globs, before confirmation; nil fence = legacy skip.
The interface MUST NOT import internal/ - the daemon adapts its FenceChecker.

## Verified Anchors

- The stage to port: internal/security/engine.go:427-461 (action->op mapping,
  shell CheckCommand with workdir default "."). Read it with terminal cat.
- Per-tool wiring to REMOVE: internal/daemon/components.go:6649-6766
  (SetFenceChecker injections). Remove ONLY after the central stage is
  enforced - one leaf, both halves, so no window of double/no enforcement.
- THE RACE THIS FIXES: internal/agent/handler.go:2990-2998 mutates ONE shared
  FenceChecker RootPath per session. Replace with a per-session FenceChecker
  constructed where the session binds its working dir (handler.go sessionLoop
  + dispatcher.go:843 resolveAgent - BOTH sites, three-site rule).
- Executor seam: internal/agent/executor.go:1675-1716 checkPermission ->
  CheckPermissionForAgent call site (details keys: path, command, workdir).
- pkg/security/permissions.go:110 NewPermissionChecker; CheckPermissionForAgent
  at :356 (stage order reference).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | FenceStage interface + SetFence + stage in CheckPermissionForAgent (file actions: op map read/write/delete; shell: CheckCommand) | pkg/security/permissions.go (+fence_stage_test.go) | `go test -p 2 ./pkg/security/ -run TestFenceStage` | symbol absent | fence error => Allowed:false Reason=err; nil fence => unchanged; stage order per C1 |
| T2 | Per-session FenceStage adapter in daemon: construct at session working-dir binding, inject into the loop's PermissionChecker view; BOTH creation sites (sessionLoop, resolveAgent) | internal/daemon/components.go, internal/agent/handler.go, internal/agent/dispatcher.go | `go test -p 2 ./internal/daemon/ ./internal/agent/ -run TestSessionFence` | adapter absent | two concurrent sessions with different roots: each fenced to ITS root (regression pin for the RootPath race) |
| T3 | Remove per-tool SetFenceChecker wiring (components.go:6649-6766 block + tool constructor params that only fed it) | internal/daemon/components.go, internal/tools/builtin/*.go (param removals only) | `go build ./...` | n/a (removal) | build green; no tool holds a fence checker; central stage still denies (gate F6 member GREEN) |
| T4 | handler.go:2991 shared-root mutation replaced by per-session instances (no shared mutable FenceChecker remains) | internal/agent/handler.go | `go test -race -p 2 ./internal/agent/ -run TestFence` | shared mutation persists | no RootPath write on a shared instance anywhere (grep + test) |

## Self-Verification Checklist

- [ ] pkg/security has NO internal/ import (cycle direction)
- [ ] Both loop-creation sites inject per-session fence (three-site rule)
- [ ] Removal (T3) and central enforcement (T1) land in ONE leaf - no gap
- [ ] -race green on touched packages; gate tests GREEN and unmodified
- [ ] gofmt clean; no debug artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify command + output: [last 20 lines each]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] C1 stage order exact; typed-nil guard on SetFence
- [ ] NO remaining SetFenceChecker call sites (grep zero)
- [ ] Concurrent-session fence pin test exists and is race-clean
- [ ] handler.go no longer mutates a shared instance
