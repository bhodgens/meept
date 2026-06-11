# Plan: Security Tooling Wiring — Memory Reflect, Fence Checker, Taint Tracking

**Date**: 2026-06-11
**Priority**: High — these are security features that exist but are not wired, leaving agents unprotected.
**Scope**: 3 bugs, all implementation-complete subsystems that just need wiring.

---

## Summary

Three implemented subsystems are not wired into the daemon, making them unavailable to agents:

1. **`memory_reflect` tool** — agents cannot perform meta-cognition on memories (tool commented out)
2. **`FenceChecker`** — agents have no path sandboxing, can read/write anywhere on disk
3. **Taint tracking** — agents have no information flow control for shell/network operations

---

## Bug 1: `memory_reflect` Tool Commented Out

### Problem
`registerBuiltinTools` in `components.go:2537-2540` has the `MemoryReflectTool` registration commented out because the function doesn't receive an `llmClient` parameter. This tool uses the LLM to analyze accumulated memories and generate higher-level insights — a core agent capability.

### Fix
Add `llmClient *llm.Client` parameter to `registerBuiltinTools()` and uncomment the registration.

### Files
- `internal/daemon/components.go`
  - Add `llmClient *llm.Client` parameter to `registerBuiltinTools()` signature (line ~2457)
  - Uncomment `MemoryReflectTool` registration (lines 2537-2540)
  - Pass `c.LLMClient` at the call site (line 1083)
- `internal/daemon/components.go` — deferred scheduler registration also calls with nil sched (line 1563+), no llmClient needed there

### Verification
- [ ] Build compiles: `go build ./...`
- [ ] `grep -r "memory_reflect" internal/daemon/components.go` shows it uncommented
- [ ] `go test ./internal/tools/builtin/... -v` passes

---

## Bug 2: FenceChecker Not Wired to Tools

### Problem
`FenceChecker` (`internal/security/fence.go`) is fully implemented with path validation for read/write/exec operations, but it's never passed to any tool. Agents can access any path on the filesystem.

The `FenceConfig` has `Enabled` and `RootPath` fields but there's no plumbing from config → Components → tools.

### Fix
1. Add `FenceChecker *security.FenceChecker` field to `Components` struct
2. Create the `FenceChecker` in `NewComponents` from `cfg.Security.FenceEnabled` and working dir
3. Pass it to `registerBuiltinTools()`
4. Add `SetFenceChecker(fc *security.FenceChecker)` to `ShellExecuteTool` and the file tools (`ReadFileTool`, `WriteFileTool`, `FileEditTool`, `DeleteFileTool`)
5. Call `fc.CheckPath(path, op)` before each file/shell operation
6. Wire from `ChatHandler` into per-session fence creation based on project worktree

### Files
- `internal/daemon/components.go`
  - Add `FenceChecker *security.FenceChecker` field to `Components`
  - Create in `NewComponents` when `cfg.Security.FenceEnabled` is true
  - Pass to `registerBuiltinTools()`
- `internal/tools/builtin/shell.go`
  - Add `fenceChecker *security.FenceChecker` field
  - Add `SetFenceChecker(fc *security.FenceChecker)` method
  - Check `fenceChecker.CheckCommand(cmd, workDir)` before execution
- `internal/tools/builtin/file_tools.go` (or individual files)
  - Add `SetFenceChecker` to `ReadFileTool`, `WriteFileTool`, `FileEditTool`, `DeleteFileTool`
  - Check `fc.CheckPath(path, "read"|"write")` before file operations
- `config/meept.json5` — add `fence_enabled` and `fence_allow_read` defaults

### Verification
- [ ] Build compiles: `go build ./...`
- [ ] `go test ./internal/security/... -run TestFence -v` passes
- [ ] `go test ./internal/tools/builtin/... -v` passes
- [ ] Shell tool blocks execution outside project root when fencing enabled

---

## Bug 3: Taint Tracking Not Wired

### Problem
`internal/security/taint/` has a complete implementation:
- `Tracker` with mark/store/retrieve/propagate/check operations
- `ExtendedTracker` with context scoping and logging
- `PatternMatcher` for injection/exfiltration detection
- `ShellExecSink`, `NetFetchSink`, `AgentMessageSink`
- `TaintConfig` already exists in `internal/config/schema.go`

But none of it is wired. The daemon never creates a tracker, never passes it to the security orchestrator, and no tools call taint checks.

### Fix
1. Add `TaintTracker *taint.ExtendedTracker` field to `Components`
2. Create in `NewComponents` when `cfg.Security.Taint.Enabled` is true
3. Add `taintTracker *taint.ExtendedTracker` field to `security.Orchestrator`
4. In `Orchestrator.ScanShellCommand()`, add taint check before Tirith scanning
5. Add `SetTaintTracker` to `ShellExecuteTool` and `WebFetchTool`
6. In shell tool: mark user-provided commands as `TaintUserInput`, check against `ShellExecSink`
7. In web fetch tool: check URLs against `NetFetchSink` for exfiltration
8. Wire taint tracker through `registerBuiltinTools` or directly to tools

### Files
- `internal/daemon/components.go`
  - Add `TaintTracker` field to `Components`
  - Create `taint.NewExtendedTracker(logger)` when config enabled
  - Pass to security orchestrator
- `internal/security/orchestrator.go`
  - Add `taintTracker *taint.ExtendedTracker` field
  - Add `SetTaintTracker(tt *taint.ExtendedTracker)` method
  - Integrate into `ScanShellCommand()` — check taint before/after Tirith
- `internal/tools/builtin/shell.go`
  - Add `taintTracker` field and setter
  - Before exec: check command against `taint.ShellExecSink()`
  - Log violation and block if tainted
- `internal/tools/builtin/web_fetch.go` (or web.go)
  - Add `taintTracker` field and setter
  - Before fetch: check URL against `taint.NetFetchSink()`
  - Block URLs with secret exfiltration patterns
- `config/meept.json5` — ensure `taint` section exists with defaults

### Verification
- [ ] Build compiles: `go build ./...`
- [ ] `go test ./internal/security/taint/... -v` passes
- [ ] `go test ./internal/security/... -v` passes
- [ ] Shell tool blocks externally-tainted commands when taint enabled
- [ ] Web fetch blocks URLs with secret parameters when taint enabled

---

## Implementation Order

1. **Bug 1** (memory_reflect) — simplest, just add a parameter and uncomment
2. **Bug 3** (taint tracking) — medium complexity, wire through orchestrator
3. **Bug 2** (fence checker) — most complex, needs per-tool integration

## Success Criteria

- [x] All 3 features compile and pass tests
- [x] `go build ./...` succeeds
- [x] `go test ./...` succeeds (zero failures)
- [x] `memory_reflect` tool appears in `registry.List()` output
- [x] Fence checker blocks out-of-sandbox file access when enabled
- [x] Taint tracker blocks shell exfil when enabled

## Implementation Complete — 2026-06-11

All three bugs fixed in a single pass:

1. **memory_reflect**: Added `llmClient` parameter to `registerBuiltinTools()`, uncommented registration.
2. **Taint tracking**: Wired `ExtendedTracker` into `SecurityOrchestrator`, added `ScanShellCommand` and `CheckWebFetch` taint checks, wired in `createSecurityOrchestrator()`.
3. **Fence checker**: Added `FenceChecker` field to `Components`, created in `NewComponents`, wired to `ShellExecuteTool`, `ReadFileTool`, `WriteFileTool`, `DeleteFileTool`, and `WebFetchTool`.
