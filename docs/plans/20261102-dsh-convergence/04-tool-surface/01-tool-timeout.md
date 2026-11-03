# 01 Tool Timeout - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate;
> the failing gate tests ALREADY EXIST and are committed (00T-tests.md T1/T1b
> + the tool-surface e2e suite). Your job is to make them pass - do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong gate
> tests to the orchestrator instead). Do NOT commit - the orchestrator handles
> all git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file, do
> NOT read it back - write once and stop. Report what you built, files touched,
> and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add `TimeoutMs() time.Duration` to the Tool interface and arm a
  cooperative context deadline in the live executor dispatch path, returning a
  structured `TOOL_TIMEOUT` result on expiry (master C8, dsh
  guard/timeout-policy).
- **Dependencies:** B1 (parent orchestrator); gate tests in 00T-tests.md
- **Estimated Context:** 18K
- **Concurrency Group:** A
- **Audit references:** /tmp/dsh-audit/runtime-safety.md (timeout-policy F)

## Goal

Give meept a cooperative, opt-in per-tool timeout: a tool that declares a
budget gets a context deadline around its single Execute call, and when that
budget expires the executor returns a model-facing structured `TOOL_TIMEOUT`
result instead of hanging the turn or leaking a raw `context.DeadlineExceeded`.
Tools that declare no budget (0) delegate unchanged. This replaces the current
ad-hoc per-tool literals (shell 30s, web_fetch/pdf_read literals, MCP stdio)
only for tools that opt in; the wrapper composes with the existing
gate/permission/retry pipeline.

## Context

- `internal/tools/interface.go:24-54` - the Tool interface (Name, Description,
  Parameters, Execute, IsReadOnly, IsConcurrencySafe). There is NO TimeoutMs.
- `internal/tools/interface.go:60-68` - `ToolDefaults` struct embedding pattern
  for conservative defaults on the safety-query methods. Add the zero-TimeoutMs
  here so existing tools inherit it without churn.
- `internal/agent/executor.go:1429-1528` - `executeToolWithRetry`, the LIVE
  dispatch path. It already: schema-validates args (:1435), runs the registry
  gate bracket (:1449-1459), then loops retrying `tool.Execute(ctx, args)` at
  :1480 (or `ExecuteStreaming` when the tool is a StreamingTool and a bus is
  present, :1474-1478). The timeout wrapper belongs around that Execute body.
  Retry semantics: each attempt is a fresh tool call, so the deadline should
  bound the LOOP'S allowed wall-clock per the tool's declared budget, OR each
  attempt - see the design decision in Notes; default = one deadline over the
  retry loop so a retry storm cannot outlive the declared budget.
- `internal/agent/executor.go` - `ExecutionResult` (:488-510) carries
  `Success`, `Error`, `ErrCode` (:499). The TOOL_TIMEOUT result sets
  `Success:false`, `Error:"tool call timed out after Nms"`,
  `ErrCode:"TOOL_TIMEOUT"`, `IsBreakableRepeat:false` (a timeout is not a
  repeat-error-breaker input - see executor.go:506 comment on IsBreakableRepeat
  semantics).
- `internal/tools/interface.go` `ToolResult.ErrCode` (:84) - machine-readable
  error class already on the envelope; the wrapper's structured result rides
  the same ErrCode field.

## Verified Anchors

- internal/tools/interface.go:24-54 - Tool interface (extends it)
- internal/tools/interface.go:60-68 - ToolDefaults (add zero TimeoutMs)
- internal/agent/executor.go:1429-1528 - executeToolWithRetry (tool.Execute :1480, ExecuteStreaming :1474-1478)
- internal/agent/executor.go:488-510 - ExecutionResult (Success/Error/ErrCode/IsBreakableRepeat)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/interface.go:
//  Tool interface gains:
//  TimeoutMs() time.Duration        // 0 = no declared timeout (opt-in)
// ToolDefaults + the existing embedded pattern add:
//  func (ToolDefaults) TimeoutMs() time.Duration { return 0 }
// internal/tools/interface.go (or tools/errors.go):
//  var ErrToolTimeout = errors.New("tool call timed out")  // errors.Is routeable
// internal/agent/executor.go:
//  const toolTimeoutErrCode = "TOOL_TIMEOUT"
```

### What This Leaf Consumes

- B1 (parent) - full contract above. No signature is invented; it is the
  parent's frozen B1. Consumers downstream: 02-lsp-navigation declares
  `TimeoutMs()==60s` on `lsp_navigate`; the e2e suite asserts the composed
  path.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | Add TimeoutMs + zero default to the Tool interface | Modify internal/tools/interface.go (Tool interface :24-54; ToolDefaults :60-68) | `go test -p 2 ./internal/tools/... -run Timeout -v` | FAIL: `TimeoutMs` undefined (gate test compiles against it) | PASS: interface + ToolDefaults satisfy the gate test | Given the gate's `timeoutTool` embedding ToolDefaults; When compiles against the interface and default; Then TimeoutMs() returns 0 for the default and the declared value for the declaring tool |
| T2 | ErrToolTimeout sentinel + codes | Modify internal/tools/interface.go (or add internal/tools/errors.go) | `go build ./...` | FAIL: `ErrToolTimeout` undefined (gate test errors.Is) | PASS: sentinel exported and routeable | Given `errors.Is(err, ErrToolTimeout)`; When a timed-out tool errors through the wrapper; Then it classifies as a tool timeout, not a generic context error |
| T3 | Arming the deadline in the executor | Modify internal/agent/executor.go (executeToolWithRetry :1429) | `go test -p 2 ./internal/agent -run Timeout -v` | FAIL: no deadline armed; no timeout result | PASS: timeoutTool returns ErrCode "TOOL_TIMEOUT" + message "timed out after" + errors.Is ErrToolTimeout | Given tool.TimeoutMs()==time.Second and the body sleeps 2s; When the executor dispatches via executeToolWithRetry; Then the call returns within ~1s with Success:false, ErrCode:"TOOL_TIMEOUT", IsBreakableRepeat:false - and the error message carries the budget duration |
| T4 | Zero-budget path delegates unchanged | Modify internal/agent/executor.go | as T3 | FAIL: ToolDefaults default already 0; verify no deadline added | PASS: a default tool (TimeoutMs 0) runs to completion with NO new context deadline, unchanged | Given a tool embedding ToolDefaults; When it dispatches; Then ctx passed to Execute is the upstream ctx unadorned by a derived deadline |
| T5 | Cancellation not masked | Modify internal/agent/executor.go | as T3 | FAIL: wrapper masks upstream cancel | PASS: when the turn-level ctx is cancelled, the wrapper returns the cancellation (or the tool's own abort result), NOT a TOOL_TIMEOUT result | Given an outer ctx cancelled before/independently of the tool's own timer; When the tool returns; Then the wrapper substitutes TOOL_TIMEOUT only when ITS timer fired (scoped by ErrToolTimeout), else returns the tool's result |

**Worked example - T3:**

T3: Arm the deadline and return the structured result.
1. Confirm the committed gate test (00T-tests.md T1b shape, internal/agent/executor_timeout_test.go):
```go
func TestExecutor_Timeout_StructuredTOOL_TIMEOUT(t *testing.T) {
    reg := fakeRegistry{"timeout_tool": &timeoutTool{}}
    ex := NewExecutor(reg, nil, WithExecutorCache(nil))
    res := ex.Execute(ctx, llm.ToolCall{ID:"c1", Function: llm.ToolCallFunction{Name:"timeout_tool"}})
    if res.Success { t.Fatal("expected failure") }
    if res.ErrCode != "TOOL_TIMEOUT" { t.Fatalf("want TOOL_TIMEOUT, got %q", res.ErrCode) }
    if !errors.Is(ErrToolTimeout, ...) { ... }
    if !strings.Contains(res.Error, "timed out after") { ... }
}
```
2. Implementation - inside executeToolWithRetry, after the gate bracket and
   before the `for` retry loop, derive the deadline:
```go
execCtx := ctx
if t, ok := tool.(interface{ TimeoutMs() time.Duration }); ok {
    if d := t.TimeoutMs(); d > 0 {
        var cancel context.CancelFunc
        execCtx, cancel = context.WithTimeout(ctx, d)
        defer cancel()
    }
}
// replace ctx with execCtx in the tool.Execute / ExecuteStreaming calls
```
   On `execCtx.Err() == context.DeadlineExceeded` with `lastErr == nil`, return
   `&tools.ToolResult{Success:false, ErrCode:toolTimeoutErrCode, Error:fmt.Sprintf("tool call timed out after %s", d)}` whose Err wraps ErrToolTimeout. The ExecutionResult must NOT be IsBreakableRepeat.
3. Verify: `go test -p 2 ./internal/agent -run 'Executor_Timeout|ToolDefaults' -v` -> PASS.

Timeout lever details: a context bound to tool.Execute alone dies per attempt;
bounding the whole retry loop keeps a retry storm inside the budget. Either is
defensible - default to bounding the LOOP per Notes unless a gate test
__explicitly__ demands per-attempt (it does not today). Streaming tools
(ExecuteStreaming) must honor the same derived ctx.

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests pass GREEN (00T T1/T1b + e2e tool-surface timeout case)
- [ ] Interface contract B1 satisfied exactly (TimeoutMs zero default, ErrToolTimeout, ErrCode "TOOL_TIMEOUT")
- [ ] All files at exact specified paths
- [ ] No gate test deleted/weakened; gate-test diff empty
- [ ] No deviations (or documented in the report)
- [ ] No scope creep - only the timeout wrapper, not an LSP tool, not a blanket refactor of per-tool literals

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [the verify commands in Tasks; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing and untouched
- [ ] B1 matches exactly (signatures, types, paths)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] Timeout result flows through ExecutionResult with ErrCode + sentinel, and is NOT IsBreakableRepeat (it would otherwise trip the repeat-error breaker)

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh semantics for the structured result (packages/guard/timeout-policy/src/index.ts:41-48): the substituted result is `isError` with a stable `TOOL_TIMEOUT` code, model-facing message "Error: tool call timed out after Nms". Port both the message shape and the stable code. Point-instantiate the sentinel so nested/outer deadlines read as plain cancellation - dsh scopes the code (TOOL_TIMEOUT constant owned by the plugin).
- OPT-IN: do NOT refactor existing per-tool literals (shell, web_fetch, pdf_read, MCP) in this leaf. They already bound themselves; the wrapper is additive. A follow-up could migrate them to declare TimeoutMs, but that is out of scope.
- Retry interplay: `getRetryConfigForTool` (:1535) already caps attempts. Bounding the whole loop by the declared budget means a slow tool that fails+retries cannot exceed the declared timeout. If a gate test later needs per-attempt deadlines, that is a delta the orchestrator owns - report instead of re-deciding here.
