# Review Gaps — Research & Design Investigation

> **For Claude:** This is a research plan, not an implementation plan. Each task produces findings and a recommended approach. Do NOT write code — only investigate and document.

**Goal:** Investigate 5 gaps that need design decisions before fixing. Each task produces: (a) findings from code investigation, (b) a recommended approach, (c) estimated fix complexity. No code changes until the user approves each recommended approach.

**Context:** These issues were identified by 7 parallel review agents during the full codebase review (2026-06-12). They require understanding intent, evaluating trade-offs, or profiling before a fix can be designed.

---

## Task 1: Investigate Reflection Loop Single-Iteration Behavior

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/reflection.go` — loop body (lines 85-164)
- `internal/agent/orchestrator.go` — caller of reflection
- Any tests in `internal/agent/reflection_test.go`

**Problem:** The reflection loop iterates up to `MaxReflections` times, but every code path inside the loop returns before reaching the bottom of the for-loop body. If linters find errors → request fix and return. If linters pass but tests fail → request fix and return. If both pass → return with `Fixed = true`. There is no path that continues to the next iteration.

**Investigation questions:**
1. Was multi-iteration reflection ever implemented, or was `MaxReflections` aspirational?
2. What would a second iteration do differently? Would it re-run linters on the fixed code and then ask the LLM again?
3. Is there a risk of infinite loops if multi-iteration is enabled (LLM fix introduces new errors → loop forever)?
4. Do the callers (orchestrator) expect `PendingFix` to be applied and then re-entered?
5. What does the reflection flow look like end-to-end: who calls `Reflect`, who applies the `PendingFix`, who calls `Reflect` again?

**Deliverable:** A section documenting the current flow, whether multi-iteration makes sense, and a recommended approach (enable multi-iteration with a circuit breaker, or remove `MaxReflections` config and document single-iteration as intentional).

### GLM Findings: Reflection Loop Single-Iteration Behavior

**Current State:**

The `MaxReflections` loop in `reflection.go:85-164` is **dead code**. Every code path inside the loop body returns before reaching the bottom of the for-loop. The loop always executes exactly 1 iteration. The original plan (`docs/plans/20260609-auto-lint-test-reflection-implementation.md`, lines 709-720) called for `requestFix` to return `(bool, error)` with `continue` statements for retry. What was implemented instead returns `(*FixAttempt, error)` — a pending fix that the **caller** (orchestrator) applies.

**End-to-End Flow:**

1. **Trigger**: Orchestrator subscribes to `"tool.execution.complete"` bus topic, fires on `file_edit` tool success (`orchestrator.go:627-646`)
2. **Pass 1**: `reflectionEngine.RunReflection(ctx, editedFiles)` — runs linters/tests, returns `PendingFix` if errors found (`orchestrator.go:656`)
3. **Fix Application**: `orchestrator.applyFix(ctx, result.PendingFix)` — writes LLM fix to disk (`orchestrator.go:674`)
4. **Pass 2**: `reflectionEngine.RunReflection(ctx, appliedFiles)` — re-runs linters on fixed code (`orchestrator.go:682`)
5. **Pass 3 (final)**: If pass 2 still has errors, applies one more fix but does **not** re-verify (`orchestrator.go:698-699`)
6. **Result**: Logs outcome, publishes `"reflection.complete"` bus event if gave up

The orchestrator implements a **hardcoded 3-pass maximum** (initial + retry + one more apply without verify). The comment on line 693 says `"single retry pass to avoid infinite loop"`, confirming awareness of loop risk.

**Multi-Iteration Risk Assessment:**

If the internal loop were fixed to use `continue`, there is a real oscillation risk because `parseFixResponse` is extremely permissive — ANY non-empty LLM response produces a `FixAttempt`. The `MaxReflections` cap (default 3) is the only circuit breaker, and there is no convergence detection (no way to detect "same errors as last iteration").

**Recommendation:**

**Remove the dead `MaxReflections` loop and document the orchestrator's hardcoded 3-pass approach as intentional.** The current design is actually sound — the orchestrator controls retry externally with a clear 3-pass ceiling. The internal loop just adds confusion. Fix the `for` loop to be a simple function body (no loop at all). Estimated complexity: **trivial** (remove loop wrapper, keep function body logic).

### Qwen Findings:

**Current Flow:**
The reflection loop in `reflection.go:85-164` is **dead code**. It declares `for i := 0; i < MaxReflections; i++` but every code path inside the loop body contains a `return` statement:
- Lint errors exist → request fix from LLM → return at line 117
- Test failures exist → request fix from LLM → return at line 148
- All pass → set Fixed=true, return at line 156

The loop always executes exactly **one** iteration. There is no path that reaches the bottom of the for-loop body and continues to the next iteration.

**MaxReflections Usage:**
`MaxReflections` is declared in `ReflectionConfig` and `AgentReflectionConfig`, wired through `daemon/components.go`, but **never functionally used**. The actual retry logic lives in the orchestrator's `handleToolExecutionComplete()` (`orchestrator.go:654-744`), which has a **hardcoded 3-pass maximum** completely independent of `MaxReflections`:
- Pass 1: Call `RunReflection()`, apply `PendingFix` if returned
- Pass 2: Re-call `RunReflection()` on applied fix files
- Pass 3: If still `PendingFix`, apply one final time **without re-verification**

**Caller Behavior:**
The orchestrator waits for `tool.execution.complete` events on `file_edit` tools, then spawns a goroutine that sequentially calls `RunReflection()` → applies fix → calls `RunReflection()` again → applies final fix. No code path re-enters reflection after a fix is applied within the loop itself. The orchestrator controls retry externally.

**Recommendation:**
**Remove the dead `MaxReflections` loop** from `RunReflection()` and document the orchestrator's hardcoded 3-pass approach as intentional. The internal loop serves no purpose because:
1. It has no fix application logic (only the orchestrator applies fixes)
2. Multi-iteration would just re-lint the same unchanged files and request identical fixes
3. The orchestrator already provides a clear, documented 3-pass ceiling

Keep `MaxReflections` as a no-op config field for backward compatibility with a deprecation comment, or remove it entirely.

**Fix Complexity:** LOW (~10 lines removed, plus config cleanup)

---

## Task 2: Investigate parseFixResponse Indiscriminate File Targeting

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/reflection.go` — `parseFixResponse` (lines 295-319) and `applyFix` caller
- `internal/agent/orchestrator.go` — how `FixAttempt.Files` is consumed

**Problem:** `parseFixResponse` returns a `FixAttempt` with `targetFiles` set to ALL original files, regardless of whether the LLM response addresses those files. The file-reference check loop is a no-op (`continue` does nothing). When `applyFix` iterates over `fix.Files`, it writes the same content to every file.

**Investigation questions:**
1. How does `applyFix` actually use `FixAttempt.Files`? Does it write the same `FixText` to each file, or does it try to extract per-file edits?
2. What's the actual failure mode? Does it overwrite file contents with the full LLM response text?
3. Could we parse the LLM response for per-file code blocks (````filepath\n...code...````) and map them to the correct files?
4. Or should we just filter `targetFiles` to only include files the LLM response actually references?
5. What format does the LLM actually return fixes in? Read the prompt in `formatLintFixRequest` and `formatTestFixRequest`.

**Deliverable:** A section documenting the current fix application flow, what the LLM actually returns, and a recommended parsing strategy.

### GLM Findings: parseFixResponse Indiscriminate File Targeting

**Current State — Three Compounding Bugs:**

1. **`parseFixResponse`** (`reflection.go:295-320`): Unconditionally copies ALL `originalFiles` into `FixAttempt.Files`. The file-reference heuristic loop (lines 308-312) checks if the LLM response mentions each file, but the result is **completely discarded** — it only produces a debug log. `targetFiles` is never filtered.

2. **`extractCodeFromMarkdown`** (`orchestrator.go:809-848`): Finds the **first** `\`\`\`...\`\`\`` code block in the response and returns everything between the fences. If the LLM returns multiple code blocks (one per file), only the first is extracted.

3. **`applyFix`** (`orchestrator.go:750-804`): Writes the **exact same content** (first code block or entire raw LLM text) to **every file** in `fix.Files`.

**Actual Failure Mode:**

When linters find errors in 2+ files (e.g., `reflection.go` and `orchestrator.go`):
1. `parseFixResponse` sets `Files = ["reflection.go", "orchestrator.go"]` and `FixText` = entire LLM response
2. LLM returns a response with multiple code blocks, one per file
3. `extractCodeFromMarkdown` grabs only the first code block (e.g., the fix for `reflection.go`)
4. `applyFix` writes that first code block to **both** files
5. **`orchestrator.go` is overwritten with code meant for `reflection.go`**, destroying its original content

If `extractCodeFromMarkdown` finds no code blocks (LLM didn't use markdown), the **entire raw LLM prose response** gets written as file content for every target file.

**What the LLM Prompt Asks For:**

The `buildFixPrompt` (`reflection.go:424`) asks: "provide corrected code. Use the file_edit tool to apply fixes, or if you're providing code directly, format it as a complete patch with the file path and corrected content." This is ambiguous — the LLM has no tool-calling capability in this context (it's a raw `Chat()` call), so it falls back to natural markdown code blocks, typically with file path annotations like:

```
// File: internal/agent/reflection.go
```go
...
```
```

**Test Coverage:** Zero. No tests exist for `parseFixResponse`, `applyFix`, or `extractCodeFromMarkdown`.

**Recommendation:**

Replace `parseFixResponse` with a proper per-file parser that:
1. Parses the LLM response for all markdown code blocks
2. Extracts file path annotations from headers/comments preceding each block (patterns: `## filepath`, `// File: path`, `path:`)
3. Builds a `map[string]string` (file path → code content)
4. Only targets files that have matching code blocks in the response
5. Falls back to single-file behavior if only one code block is found

Update `applyFix` to accept per-file content instead of writing the same blob to all files. Estimated complexity: **medium** (new parser ~50 lines, `applyFix` refactor ~20 lines, tests ~80 lines).

### Qwen Findings:

**Current parseFixResponse Behavior:**
`parseFixResponse` is completely broken. It does **not** parse file references from the LLM response at all:
- Lines 301-304: `targetFiles` is set to ALL `originalFiles`, unconditionally
- Lines 308-312: The file-reference check loop is a no-op — it checks if content contains the filename but only `continue`s (does nothing with this information)
- Result: `FixAttempt.Files` always equals `originalFiles` — every file that was edited in the initial `file_edit` is included regardless of whether the LLM response even mentions it

**Prompt Format Used:**
- `formatLintFixRequest` (lines 322-355): Lists lint errors grouped by file with tree-sitter context. Does **not** instruct the LLM to return per-file code blocks.
- `formatTestFixRequest` (lines 358-380): Lists failing tests with name, file, error, and output snippets. Does **not** specify an LLM response format.
- `buildFixPrompt` (lines 424-432) wraps these and appends: "Please analyze the errors above and provide corrected code. Use the file_edit tool to apply fixes, or if you're providing code directly, format it as a complete patch with the file path and corrected content."

The prompt asks for "patch with the file path and corrected content" but `parseFixResponse` has **zero** logic to parse file paths or patches. This is a complete disconnect.

**applyFix Behavior:**
`applyFix` (orchestrator.go:750-804) iterates over `fix.Files` and writes the **same** `content` (extracted from `fix.FixText` via `extractCodeFromMarkdown`) to **every** file. `extractCodeFromMarkdown` grabs only the **first** markdown code block and writes that exact string to **all files** in `fix.Files`.

**LLM Response Format Analysis:**
The LLM is prompted to "format it as a complete patch with the file path and corrected content" but may return multiple code blocks for multiple files. The current parsing broadcasts the first code block to all files, potentially overwriting unrelated files with incorrect content.

**Recommendation:**
The core issue is that the orchestration layer runs as a hardcoded 3-pass external retry loop. The fix has two parts:
1. **Remove the dead `for` loop from `RunReflection`** — trivial, ~10 lines removed
2. **Rewrite `parseFixResponse`** to actually parse the LLM response into per-file content chunks. For higher reliability, implement tool call parsing if the LLM is expected to return `file_edit` tool calls.

`applyFix` would also need a `map[string]string` rewrite (path → content) to support true per-file application.

**Fix Complexity:** MEDIUM (~70-100 lines changed across 2 files: reflection.go and orchestrator.go)

---

## Task 3: Investigate Security Hooks — Intended Enforcement Model

**Severity:** MEDIUM
**Files to investigate:**
- `internal/agent/security_hooks.go` — `checkFilePermission`, `checkNetworkPermission` (lines 84-132)
- `internal/security/` — existing `FenceChecker`, `PermissionChecker`, `Orchestrator`
- `internal/agent/security_hooks.go` — `scanShellCommand` (the one hook that actually works)

**Problem:** `checkFilePermission` and `checkNetworkPermission` log that they perform checks but always return `BlockResult{}` (not blocked). They don't validate path boundaries or check URL allowlists. Only `scanShellCommand` performs actual checks via Tirith.

**Investigation questions:**
1. What is the intended security model? Should file ops be restricted to project worktree boundaries? Should network ops be restricted to specific domains?
2. How does `FenceChecker` (already wired) relate to these hooks? Is there overlap?
3. What does `SecurityOrchestrator.Check()` already cover? Are these hooks redundant?
4. If they're meant to be real checks, what policy should they enforce? Read any security docs in `docs/` for clues.
5. Should these hooks be removed (redundant with FenceChecker) or implemented (defense-in-depth)?

**Deliverable:** A section documenting the existing security layers, any overlap, and a recommendation (remove redundant hooks, implement real checks, or document as intentional placeholders).

### GLM Findings: Security Hooks — Intended Enforcement Model

**Current Security Enforcement Chain:**

```
User Input
    → SecurityTransformContext Hook: orchestrator.SanitizeInput() ✅ WORKING
    → LLM Processing
    → Tool Call Requested
    → SecurityBeforeToolCall Hook:
        - shell: orchestrator.ScanShellCommand() ✅ WORKS
        - file_*: checkFilePermission() ❌ STUB — logs, always passes
        - web_fetch: checkNetworkPermission() ❌ STUB — logs, always passes
    → Tool Execute() method:
        - ShellExecuteTool: FenceChecker.CheckCommand() + ScanShellCommand() ✅ WIRED
        - ReadFile/WriteFile/DeleteFile: FenceChecker.CheckPath() ✅ WIRED
        - WebFetchTool: SetSecurityOrchestrator() exists but NEVER CALLED ❌
    → SecurityEngine.Check() (called by some tools internally) ✅ WORKING
```

**Key Findings:**

1. **`checkFilePermission` and `checkNetworkPermission` are stubs** — they extract the path/URL, log it, then always return `BlockResult{}` (not blocked). They never call `FenceChecker.CheckPath()` or `orchestrator.CheckWebFetch()`.

2. **`FenceChecker` IS properly wired** into all file tools via `SetFenceChecker()` in `components.go:2504-2546`. It enforces worktree boundaries for write/exec ops and allows reads from configured system paths. Symlinks are resolved before checking to prevent bypass attacks.

3. **`scanShellCommand` is the one working hook** — it correctly calls `orchestrator.ScanShellCommand()` which runs taint tracking + Tirith scanning.

4. **`WebFetchTool` has a gap**: `SetSecurityOrchestrator()` method exists but is never called in `components.go`, so `CheckWebFetch()` (taint exfiltration check) is never invoked. This is a separate bug from the stub hooks.

5. **Overlap assessment**: The hooks were designed as a centralized pre-execution gate, but `FenceChecker` and `SecurityEngine.Check()` already provide enforcement at the tool level. The hooks would be **duplicate** enforcement if implemented.

6. **Security model from docs**: File ops restricted to project worktree boundaries (configurable `fence_allow_read` for system paths). Network ops restricted via taint tracking — URLs containing tainted data (secrets, user input) are blocked. Risk classification: SAFE (file_read) → LOW (network_request) → MEDIUM (file_write) → HIGH (shell_execute) → CRITICAL (system_modify).

**Recommendation:**

**Remove the two stub hooks and fix the WebFetchTool wiring gap.** Specifically:
1. Remove `checkFilePermission` from `SecurityBeforeToolCall` — redundant with `FenceChecker` already wired into each file tool
2. Remove `checkNetworkPermission` from `SecurityBeforeToolCall` — instead, wire `SetSecurityOrchestrator()` on `WebFetchTool` in `components.go` so `CheckWebFetch()` runs inside the tool
3. Keep `scanShellCommand` as the sole pre-execution hook (it's the only one that provides unique value)

This is cleaner than implementing defense-in-depth hooks because the per-tool enforcement (`FenceChecker`, `SecurityEngine`) is already complete and tested. Estimated complexity: **low** (remove ~50 lines of stub code, add 1 line of wiring in `components.go`).

### Qwen Findings:

**Current Security Layers:**

| Layer | Component | What it checks | Status |
|-------|-----------|----------------|--------|
| Hook: `scanShellCommand` | Orchestrator | Taint + Tirith | **WIRED, WORKING** |
| Hook: `checkFilePermission` | **No-op** | Nothing | **STUB** |
| Hook: `checkNetworkPermission` | **No-op** | Nothing | **STUB** |
| Hook: `TaintBeforeToolCall` | **Unregistered** | Would check suspicious patterns | **UNWIRED** |
| Tool: `ShellExecuteTool` | Direct call | Fence boundary | **WIRED** |
| Tool: `ReadFileTool` etc. | Direct call | Fence boundary | **WIRED** |
| `Engine.Check()` | SQLite rules | Permission matrix, financial, path rules, confirmation gates | **WORKING** |
| `Orchestrator.SanitizeInput` | Prompt guard | Injection detection | **WORKING** |
| `Orchestrator.CheckWebFetch` | Taint | Secret exfiltration URLs | **WORKING (but not wired to WebFetchTool)** |

**Non-Functional Hooks Behavior:**
Both `checkFilePermission` and `checkNetworkPermission` (`security_hooks.go:84-132`) are pure pass-through stubs:
- They parse the JSON arguments (`path` for file, `url` for network)
- Log a debug message
- Log "security check passed"
- Return `BlockResult{}` (no block)
- They do **not** call `FenceChecker.CheckPath()`, `Engine.Check()`, or `Orchestrator.CheckWebFetch()`

**Overlap Analysis:**
The hooks architecture was designed as a clean extension point for pre/post tool enforcement, but:
- `FenceChecker` is already wired directly into all file/shell tools via `SetFenceChecker()` in `components.go:2504-2546`
- `SecurityEngine.Check()` provides SQLite-backed permission checks
- The hooks would be **redundant** with what the tools enforce directly

**Intended Model:**
File ops restricted to project worktree boundaries (configurable `fence_allow_read` for system paths). Network ops restricted via taint tracking — URLs containing tainted data (secrets, user input) are blocked. The hooks were likely intended as defense-in-depth but were never completed.

**Recommendation:**
**Remove the dead stub hooks** (`checkFilePermission`, `checkNetworkPermission`) from `SecurityBeforeToolCall`. The per-tool enforcement is already complete:
- File tools call `FenceChecker.CheckPath()` directly
- Shell tools call `FenceChecker.CheckCommand()` + `ScanShellCommand()` directly
- The only unique value from hooks is `scanShellCommand` (Tirith scanning)

Additionally, wire `SetSecurityOrchestrator()` on `WebFetchTool` in `components.go` so `CheckWebFetch()` is called (the method exists but is never invoked).

Alternatively, if hooks should be the enforcement layer instead of direct tool calls, implement them to call:
- `Engine.Check(action, toolName, details, conversationID)` for file ops
- `Orchestrator.CheckWebFetch(url)` for network ops
- `FenceChecker.CheckPath(path, op)` as an additional boundary layer

**Fix Complexity:** LOW for removal (~50 lines, 15 min) or MEDIUM for implementation (~1-2 hours)

---

## Task 4: Investigate Streaming Parser Tool Call Delta Handling

**Severity:** MEDIUM
**Files to investigate:**
- `internal/llm/client.go` — `ChatWithDeltaCallback` streaming chunk parser (lines 843-1014)
- `internal/llm/anthropic.go` — Anthropic streaming parser
- OpenAI streaming protocol documentation for `delta.tool_calls` format

**Problem:** The streaming chunk struct only extracts `delta.content`. It does not parse `delta.tool_calls` from the SSE stream. When the model returns tool calls in streaming mode, they are silently dropped. The returned `Response` has empty `ToolCalls` and zero `Usage`.

**Investigation questions:**
1. What does the OpenAI streaming protocol send for tool calls? (SSE chunks with `delta.tool_calls[].function.name` and `delta.tool_calls[].function.arguments` deltas)
2. What does the Anthropic streaming protocol send? (`content_block_start`, `content_block_delta` with `tool_use` type)
3. Is the streaming parser used for agentic workflows (tool calls required), or only for display (text-only OK)?
4. What's the scope of the fix — just parse the deltas, or also accumulate them into complete tool calls?
5. Are there tests for the streaming parser that would need updating?

**Deliverable:** A section documenting the streaming protocol formats, which code paths use streaming, and a recommended implementation approach with estimated complexity.

### GLM Findings: Streaming Parser Tool Call Delta Handling

**Critical Context: Streaming is NOT Used in Agent Workflows**

The streaming parser `ChatWithDeltaCallback` is **never called from the agent loop**. The agent loop (`loop.go:1814`) calls `chatWithFailover()` — the **non-streaming** variant. `chatWithFailoverStream()` is defined at `loop.go:2152` but is **dead code** — never invoked anywhere in the codebase.

This means the missing tool call delta parsing is a **latent bug**, not an active one. It will become critical when streaming is enabled for agentic workflows (which is clearly planned given the TTSR rule-checking infrastructure in `chatWithFailoverRaw`).

**OpenAI Streaming Protocol (what's dropped):**

The OpenAI client chunk struct (`client.go:960-966`) only extracts `delta.content`:
```go
var chunk struct {
    Choices []struct {
        Delta struct {
            Content string `json:"content"`  // ONLY field extracted
        } `json:"delta"`
        FinishReason *string `json:"finish_reason"`
    } `json:"choices"`
}
```

Missing from the struct: `delta.tool_calls[].index`, `.id`, `.function.name`, `.function.arguments` — all silently dropped by `json.Unmarshal`. The returned `Response` has `ToolCalls: nil` and `Usage: TokenUsage{}`.

**Anthropic Streaming Protocol (already correct):**

The Anthropic client's `parseStreamingResponse` (`anthropic.go:917-1007`) **already handles tool calls correctly**:
- `content_block_start` with `tool_use` type captures ID and Name (lines 943-958)
- `content_block_delta` with `input_json_delta` accumulates `PartialJSON` into `InputJSON` (lines 979-983)
- `buildResponseFromBlocks` (lines 1021-1037) converts accumulated blocks into `[]ToolCall`

No fix needed for the Anthropic streaming parser.

**Test Coverage:** Zero. No tests exist for `ChatWithDeltaCallback` (OpenAI) or `parseStreamingResponse` (Anthropic).

**Recommendation:**

**Fix the OpenAI streaming parser only — the Anthropic one is already correct.** The fix is:
1. Expand the chunk struct to include `delta.tool_calls` fields (index, id, function.name, function.arguments)
2. Maintain a `map[int]*ToolCallAccumulator` to accumulate tool call deltas across chunks
3. Build `[]ToolCall` from the accumulator in the final response
4. Add tests covering: text-only streaming, single tool call, multiple parallel tool calls, mixed text+tools
5. Optionally add `stream_options: {"include_usage": true}` to the request payload for usage data

Estimated complexity: **medium** (~40 lines parser changes, ~80 lines tests). Not urgent since streaming is not used in agentic workflows yet, but should be fixed before enabling `chatWithFailoverStream`.

### Qwen Findings:

**Current Streaming Parser Behavior:**

**OpenAI-compatible client** (`client.go:960-986`): The streaming chunk struct only extracts `delta.content`:
```go
var chunk struct {
    Choices []struct {
        Delta struct {
            Content string `json:"content"`  // ONLY field
        } `json:"delta"`
        FinishReason *string `json:"finish_reason"`
    } `json:"choices"`
}
```

There is no `ToolCalls` field on the `Delta` struct. When the model streams back `delta.tool_calls[]`, those fields are silently parsed as zero by JSON unmarshalling and completely ignored. The returned `Response` always has:
- `Content`: accumulated text only
- `ToolCalls`: empty slice (never populated from streaming)
- `Usage`: empty `TokenUsage{}` (no usage tracking from streaming)

**Anthropic client** (`anthropic.go:917-1007`): This parser is **complete and correct**. It handles all Anthropic streaming event types including:
- `content_block_start` — initializes `contentBlockAccum` with ID, name, type (including `tool_use`)
- `content_block_delta` — accumulates `PartialJSON` for tool_use blocks
- `content_block_stop` — marks block complete
- `message_delta` — captures `stop_reason`
- `message_start` / `message_stop` — captures usage

The `buildResponseFromBlocks` method correctly reconstructs `ToolCall` objects from accumulated blocks.

**OpenAI/Anthropic Streaming Protocol Formats:**

**OpenAI:** When `stream: true` is set and the model returns tool calls, SSE chunks look like:
```
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"read_file","arguments":""}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/foo/bar\"}"}}]}}]}
```

Key fields per chunk:
- `delta.tool_calls[0].id` — only in the first chunk
- `delta.tool_calls[0].index` — which tool call in the array
- `delta.tool_calls[0].function.name` — only in the first chunk for that call
- `delta.tool_calls[0].function.arguments` — a JSON string, accumulated across chunks

**Anthropic:** Uses event-based protocol with `content_block_start` (type: tool_use), `content_block_delta` (PartialJSON), and `content_block_stop` events. Already handled correctly.

**Usage Analysis:**

**Streaming is NOT used in the agent loop.** The agent loop (`loop.go:1814`) calls `chatWithFailover(...)` — the **non-streaming** variant (passes `nil` for `onDelta`). This is the **only active caller**.

- `chatWithFailoverStream(...)` at `loop.go:2152` is **dead code** — defined but never invoked anywhere in the codebase
- `chatWithFailoverRaw(...)` is the internal shared implementation. It *can* branch to streaming when `onDelta != nil` and the Chatter implements `StreamingChatter`, and it *does* wire in TTSR checking. But since nothing ever passes a non-nil `onDelta`, this code path is dormant.

**Conclusion:** Streaming is planned and infrastructure exists (TTSR hooks, `StreamingChatter` interface, `DeltaCallback` type), but has never been activated for the agentic loop. The tool call delta parsing bug in the OpenAI client is a **latent bug** that will surface as a critical issue when streaming is enabled.

**Recommendation:**

**Fix needed in `client.go`** — expand the streaming chunk struct to include:
```go
Delta struct {
    Role       string           `json:"role,omitempty"`
    Content    string           `json:"content,omitempty"`
    ToolCalls  []RawToolCall    `json:"tool_calls,omitempty"`
}
```

The `RawToolCall` type already exists in the codebase (`models.go`). It needs to be reused for streaming chunks since OpenAI's streaming delta tool_calls structure is isomorphic to the non-streaming tool_calls format.

**Implementation approach:**
1. Expand the streaming chunk struct to include `[]RawToolCall` in the Delta
2. Accumulate per-index content blocks (name, arguments) across chunks — same as OpenAI's streaming spec requires
3. Build `ToolCall` objects at the end of the stream (from accumulated per-index data) and set them on the returned `Response`
4. Parse `usage` from the last chunk (providers often embed usage in the final `finish_reason` chunk)
5. Add unit tests with mock SSE streams containing tool call deltas

**Fix Complexity:** LOW-MEDIUM
- Struct expansion is trivial (a few lines)
- Per-index accumulation logic is the main work — need to handle multiple tool calls streaming concurrently (via the `index` field)
- Need a `streamingToolAccum` struct per index to track: id, name, arguments
- At stream end, convert accumulators to `[]ToolCall`
- Usage parsing from the final chunk (minor)
- Expected change: ~60-80 lines in `client.go`, ~80 lines of tests, no changes to other files needed
- The Anthropic streaming path is already correct, so only the OpenAI-compatible client needs fixing

---

## Task 5: Investigate TokenCache Growth and Eviction Strategy

**Severity:** LOW
**Files to investigate:**
- `internal/llm/tokenizer.go` — `TokenCache` struct (lines 88-118)
- Callers in `internal/llm/context_firewall.go` and `internal/llm/context_compactor.go`

**Problem:** `TokenCache` uses `sync.Map` with no eviction. Every unique string passed to `CountTokens` is cached forever. In long sessions with diverse inputs, this grows without bound.

**Investigation questions:**
1. What's the typical cache key size? (full message text? truncated hash?)
2. What's the value size? (just an int — 8 bytes)
3. What's the practical growth rate? Count calls per request to estimate entries per hour.
4. Is the cache even effective? What's the hit rate — are the same strings counted repeatedly?
5. What eviction strategies fit? Options:
   - **LRU with max size** (e.g., 10K entries, evict oldest)
   - **TTL-based** (expire after 5 minutes)
   - **Periodic purge** (clear entire cache every N minutes)
   - **Remove entirely** (if hit rate is low, the cache may not be worth the complexity)

**Deliverable:** A section documenting the cache's actual usage pattern, estimated memory footprint, and a recommended strategy with rationale.

### GLM Findings: TokenCache Growth and Eviction Strategy

**Critical Discovery: `tokenizer.TokenCache` is Dead Code**

The `sync.Map`-based `TokenCache` in `tokenizer.go:87-121` is **never used in production**. It is only instantiated in test files (`tokenizer_test.go:64` and `tokenizer_test.go:216`). The production path at `agent/loop.go:986` calls `llm.NewTokenizerForModel(modelID)` directly, which returns a bare `TiktokenTokenizer` or `HeuristicTokenizer` — never wrapped in `NewTokenCache()`.

There are actually **two different "token caches"** in the codebase with confusingly similar names:

1. **`tokenizer.TokenCache`** (`tokenizer.go:87-121`) — string→int memoization wrapper. **Dead code, never wired.**
2. **`TokenCacheCoordinator`** (`token_cache.go:87-403`) — L1 (in-memory LRU) + L2 (SQLite) **LLM response cache**. **Fully wired and has proper eviction.**

**If `tokenizer.TokenCache` Were Wired In (hypothetical analysis):**

| Aspect | Estimate |
|--------|----------|
| Key size | Full raw text string (no hashing) — typically 200-20,000 chars per message |
| Value size | ~24 bytes (boxed `int` in `interface{}`) |
| Growth rate | ~600 unique entries/hour (3 CountTokens calls × 20 messages × 10 turns) |
| Cross-request hit rate | ~80-95% (most context is unchanged between turns) |
| Time saved per request | ~10-50 microseconds (tiktoken is already ~1-5µs per call) |
| Benefit vs. LLM API latency | **Negligible** — saving 50µs vs. 1-30s API calls |

The cache would grow without bound (~14,400 entries/day) with each entry holding a full text string as key. For a 24-hour session: ~14,400 entries × average ~1KB per key = ~14MB. Not catastrophic, but pointless given the negligible performance benefit.

**`TokenCacheCoordinator` (LLM response cache) — Already Has Good Eviction:**

This cache already implements all the eviction strategies from the investigation questions:
- **LRU with max size**: `L1Cache.evictLRU()` evicts at 10,000 entries (`token_cache_l1.go:263-291`)
- **TTL-based**: 30-minute expiry with background cleanup every 2 minutes (L1 + L2)
- **File-aware invalidation**: `InvalidateByFile()` removes entries when source files change
- **L2 SQLite**: TTL cleanup prevents unbounded growth; steady state ~25 entries at typical usage

**Recommendation:**

**Remove `tokenizer.TokenCache` entirely.** It is dead code that would add no value if wired in:
- Tiktoken counting is already ~1-5µs per call — the cache would save <0.001% of request latency
- The `sync.Map` with no eviction is a latent memory leak risk if someone wires it in later
- The tests that reference it can be simplified to test the underlying tokenizer directly
- The `TokenCacheCoordinator` (response cache) already has proper eviction and provides real value

If token counting ever becomes a bottleneck (extremely unlikely), add a bounded LRU cache then. Estimated complexity: **trivial** (remove `TokenCache` struct and `NewTokenCache` function, update 2 test references).
