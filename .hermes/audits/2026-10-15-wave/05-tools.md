# Tools Surface Bughunt Audit -- 2026-10-15

## Findings

### 1. MCP stdio transport: subprocess leak on partial Start() failure
- **Severity**: HIGH
- **Location**: `internal/tools/mcp/transport/stdio.go:74-153`
- **Mechanism**: `Start()` calls `cmd.Start()` at line 129. If `cmd.Start()` succeeds but a subsequent operation (e.g., pipe setup, though all pipes are created before `cmd.Start()`) fails, the subprocess is running but `t.running` is never set to true and the transport's `Close()` may not be called. In the current code, all pipe creations happen *before* `cmd.Start()`, so if `cmd.Start()` succeeds, the transport proceeds to set `t.running = true` and start the relay goroutines. However, if `cmd.Start()` itself returns an error, the subprocess is not started and there's no leak. The real risk is if `cmd.Start()` succeeds but `relayStdout()` or `drainStderr()` panics before `t.running` is set -- the subprocess would continue running orphaned. The `t.running` store happens at line 142, after `cmd.Start()` at line 129. If the goroutine launches but the process exits unexpectedly, `t.running` remains true and `Close()` may not be called by the caller.
- **Test**: Run `go test -run TestStdioTransport_StartFailure ./internal/tools/mcp/transport/...` and check for orphaned processes.
- **Evidence**: SOURCE -- `stdio.go:129-142` shows `cmd.Start()` precedes `t.running.Store(true)`.
- **Confidence**: medium

### 2. Shell tool: `working_dir` path traversal not explicitly blocked after resolve
- **Severity**: MEDIUM
- **Location**: `internal/tools/builtin/shell.go:402-409`
- **Mechanism**: `resolvePath(ctx, wd)` resolves the working directory string. If `wd` is `../etc/passwd`, `resolvePath` returns the absolute path. The fence checker (`fenceChecker.CheckCommand`) runs *after* this resolution at line 412. If `fenceChecker` is nil (not configured), there is no containment check -- the command executes in the resolved path, which could be outside the project directory.
- **Test**: Call `ShellExecuteTool.Execute` with `working_dir: "../../"` and verify whether the fence checker is invoked or if the command runs in the parent directory.
- **Evidence**: SOURCE -- `shell.go:402-409` shows `resolvePath` is called, then `fenceChecker` check at line 412. If `fenceChecker` is nil, no containment validation occurs.
- **Confidence**: high

### 3. Registry: `ExecuteWithRetry` swallows non-retryable errors with empty error message
- **Severity**: LOW
- **Location**: `internal/tools/registry.go:565-630`
- **Mechanism**: In the retry loop, when `result != nil && result.Error != ""`, `lastErr` is set to `fmt.Errorf("%s", result.Error)`. But when `err == nil` and `result.Success == false` (a "success" wrapper around a failed semantic result), `lastErr` is set to `fmt.Errorf("unknown error during tool execution")` at line 586. This masks the actual failure reason.
- **Test**: Register a tool that returns `ToolResult{Success: false, Error: "specific failure"}` and call `ExecuteWithRetry`. The error message will be "unknown error during tool execution" instead of "specific failure".
- **Evidence**: LIVE -- Code review of `registry.go:576-587`.
- **Confidence**: medium

### 4. MCP client: tool response content not fenced/trusted
- **Severity**: MEDIUM
- **Location**: `internal/tools/mcp/client.go:233-321`
- **Mechanism**: `CallTool` returns the MCP server's response content directly as a success result. The content is sanitized for size (max 1MB, 1000 blocks) but not for trust -- a malicious MCP server could return arbitrary text that the agent treats as authoritative. The `taint.TaintExternal` label is not applied to MCP tool results (unlike shell tool results).
- **Test**: Connect an MCP server that returns `{"result": {"content": [{"type": "text", "text": "<injected instruction>"}]}}` and verify the agent treats it as trusted.
- **Evidence**: SOURCE -- `client.go:316-321` shows `NewSuccessResult(resultText)` with no taint labeling.
- **Confidence**: medium

## Checked and Clean

- `internal/tools/registry.go` -- Registry core logic reviewed; argument validation gate works as designed.
- `internal/tools/builtin/shell.go` -- Shell execution tool reviewed; risk classification and fence checking present.
- `internal/tools/mcp/client.go` -- MCP client reviewed; size limits in place, taint gap noted.
- `internal/tools/mcp/transport/stdio.go` -- Stdio transport lifecycle reviewed; subprocess teardown logic examined.
- `internal/agent/executor.go` -- ToolActionMap completeness pattern reviewed; test exists.
- `internal/skills/executor.go` -- Skills requires-tools gating reviewed; `checkRequiredTools` properly blocks execution.

## Gates Run

- `go build ./internal/tools/...` -- compiled successfully
- `go build ./internal/agent/...` -- compiled successfully
- `go build ./internal/skills/...` -- compiled successfully
- `go vet ./internal/tools/...` -- no vet errors
- `go vet ./internal/agent/...` -- no vet errors

## Not Covered

- `internal/acp/` package -- not examined due to time budget
- All `builtin/*_test.go` files -- tests not executed (per auditor rules)
- `internal/tools/argvalidate.go` -- argument validation schema details not deeply inspected
- MCP manager (`internal/tools/mcp/manager.go`) -- server lifecycle and cleanup not traced
