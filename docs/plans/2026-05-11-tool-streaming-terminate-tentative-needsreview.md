# Plan: Tool Streaming Progress & Terminate Hints

**Status**: complete (all 3 phases implemented)
**Date**: 2026-05-11
**Completed**: 2026-05-13
**Author**: claude-code
**Estimated effort**: 3 phases, ~1.5 weeks

---

## 1. Problem Statement

### Current Limitations

Meept's tool execution treats tools as opaque black boxes. When the agent loop dispatches tool calls, the TUI and HTTP clients see only two bus events:

1. `agent.action` -- "I am about to execute these tools" (emitted before execution)
2. `agent.result` -- "here are the final results" (emitted after all tools complete)

Between those two events, there is a silent gap. For fast tools (`file_read`, `platform_status`) this gap is imperceptible. For slow tools (`shell` with long-running builds, `web_fetch` for large pages, `web_search` with retries), the user sees no feedback. The TUI shows a static "executing" stage with no indication of which tool is running or how far along it is.

Additionally, the agent loop always follows tool execution with another LLM call to process results. Some tools produce definitive answers that need no further LLM processing -- for example, `memory_store` returning confirmation, `platform_status` returning a static snapshot, or a shell command that directly answers the user's question. The unnecessary follow-up call wastes tokens and latency.

### What Pi Agent Does

Pi Agent has two capabilities that address these gaps:

1. **Tool streaming updates**: Tools can call an `onUpdate` callback during execution to emit partial results. The UI sees real-time progress (e.g., "Reading file... 50%", "Running shell command... output so far: ...").

2. **Terminate hints**: Tools can return `terminate: true` to signal that the follow-up LLM call should be skipped. This only triggers when EVERY tool in the batch agrees, preventing premature termination when one tool produces an intermediate result that another tool needs processed.

### Goals

- Give tools a way to emit progress updates during execution
- Route those progress updates through the message bus so TUI and HTTP clients can display them
- Allow tools to signal that their result is final and does not need LLM follow-up
- Integrate both capabilities with existing result caching, adaptive compression, security checks, and parallel/sequential execution
- Maintain full backward compatibility -- tools that do not opt into streaming or terminate continue to work exactly as before

---

## 2. Current Architecture

### Tool Interface (`internal/tools/interface.go`)

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() llm.FunctionParameters
    Execute(ctx context.Context, args map[string]any) (any, error)
}

type ToolResult struct {
    Success  bool              `json:"success"`
    Result   any               `json:"result,omitempty"`
    Error    string            `json:"error,omitempty"`
    Evidence []models.Evidence `json:"evidence,omitempty"`
}
```

### Executor (`internal/agent/executor.go`)

The `Executor` struct handles tool execution:

```go
type Executor struct {
    registry    ToolRegistry
    security    *security.PermissionChecker
    logger      *slog.Logger
    parallelism int
    agentID     string
    cache       *ResultCache
}
```

Key methods:
- **`Execute(ctx, toolCall)`** -- Runs a single tool call: cache check, security check, `tool.Execute()`, evidence extraction, cache store. Returns `*ExecutionResult`.
- **`ExecuteAll(ctx, toolCalls)`** -- Runs multiple tool calls in parallel (semaphore-limited, default concurrency 4).
- **`ExecuteSequential(ctx, toolCalls)`** -- Runs tool calls one-by-one in order.

### ExecutionResult (`internal/agent/executor.go`)

```go
type ExecutionResult struct {
    ToolCallID string            `json:"tool_call_id"`
    Success    bool              `json:"success"`
    Result     any               `json:"result,omitempty"`
    Error      string            `json:"error,omitempty"`
    Cached     bool              `json:"cached,omitempty"`
    Evidence   []models.Evidence `json:"evidence,omitempty"`
}
```

Results undergo adaptive compression via `ToCompressedJSON(maxTokens)` based on remaining conversation budget.

### Agent Loop Integration (`internal/agent/loop.go`, lines ~1460-1541)

The agent loop flow for tool calls:

1. LLM returns response with tool calls
2. `conv.AddAssistantMessageWithToolCalls()` -- stores assistant message with tool call references
3. `publishAction()` -- emits `agent.action` bus event with tool call details
4. `publishProgress()` -- emits `agent.progress` bus event with stage="executing"
5. `executeToolCalls()` -- dispatches to `executor.ExecuteAll()` (or gates memory tools)
6. Cycle detection on tool calls
7. `result.ToCompressedJSON(dynamicToolBudget)` -- adaptive compression
8. `conv.AddToolResult()` -- stores each tool result in conversation
9. `publishResult()` -- emits `agent.result` bus event
10. `continue` -- loop iterates again for LLM to process tool results

### Message Bus (`internal/bus/bus.go`)

```go
type BusMessage struct {
    ID        string          `json:"id"`
    Type      MessageType     `json:"type"`       // "event", "request", "response", "status_update", "error"
    Topic     string          `json:"topic,omitempty"`
    Source    string          `json:"source"`
    Timestamp time.Time       `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`
    ReplyTo   string          `json:"reply_to,omitempty"`
}
```

Bus supports wildcard topic matching (e.g., `tool.*` matches `tool.progress`).

### TUI Progress (`internal/tui/progress.go`, `internal/tui/models/chat.go`)

The TUI receives `agent.progress` bus events and updates a `ProgressState`:

```go
type ProgressState struct {
    AgentID       string
    Stage         string
    Percent       float64
    CurrentTool   string
    TokensUsed    int
    ContextResets int
    LastUpdate    time.Time
}
```

The TUI app (`internal/tui/app.go`, line ~585) subscribes to `agent.progress` and extracts stage/tool info from the payload.

### HTTP API (`internal/comm/http/server.go`)

Currently has a stub `handleMetricsStream` that returns `{"status": "websocket_not_implemented"}`. No SSE infrastructure exists yet.

---

## 3. Proposed Architecture: Tool Streaming Progress

### 3.1 New Optional Interface: `StreamingTool`

Rather than modifying the existing `Tool` interface (which would break all 30+ tool implementations and MCP tools), we define a new optional interface. Tools that want streaming progress implement both `Tool` and `StreamingTool`.

```go
// StreamingTool is an optional interface that tools can implement to emit
// progress updates during execution. The executor detects this interface
// at runtime and wires up the progress callback automatically.
type StreamingTool interface {
    // ExecuteStreaming runs the tool with a progress callback.
    // The tool calls onUpdate() with ProgressUpdate values during execution.
    // The tool MUST still call Execute() semantics: return (result, error).
    ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(ProgressUpdate)) (any, error)
}
```

### 3.2 ProgressUpdate Struct

```go
// ProgressUpdate represents a streaming progress update from a tool.
type ProgressUpdate struct {
    // Message is a human-readable description of the current step.
    // Examples: "downloading file...", "parsing response...", "running command..."
    Message string `json:"message"`

    // Percent is completion progress from 0-100. Use -1 for indeterminate progress.
    Percent int `json:"percent"`

    // PartialResult is an optional partial result that the UI can display
    // before the final result arrives. For streaming outputs (shell, web_fetch),
    // this accumulates output incrementally.
    PartialResult json.RawMessage `json:"partial_result,omitempty"`

    // ToolCallID links this update to the specific tool call in the batch.
    ToolCallID string `json:"tool_call_id"`
}
```

### 3.3 Executor Changes

The `Executor` gains an optional `bus` field and a `WithExecutorBus` option. When executing a tool, the executor checks if the tool implements `StreamingTool`:

```go
type Executor struct {
    // ... existing fields ...
    bus     *bus.MessageBus       // NEW: for publishing progress events
    agentID string                // already exists
}

func WithExecutorBus(bus *bus.MessageBus) ExecutorOption {
    return func(e *Executor) {
        e.bus = bus
    }
}
```

In `Execute()`, after security checks pass:

```go
// Check if tool supports streaming
if st, ok := tool.(tools.StreamingTool); ok && e.bus != nil {
    toolResult, toolErr = st.ExecuteStreaming(ctx, args, func(pu tools.ProgressUpdate) {
        e.publishToolProgress(ctx, toolCall.ID, toolName, pu)
    })
} else {
    toolResult, toolErr = tool.Execute(ctx, args)
}
```

The `publishToolProgress` method:

```go
func (e *Executor) publishToolProgress(ctx context.Context, toolCallID, toolName string, pu tools.ProgressUpdate) {
    if e.bus == nil {
        return
    }
    payload := map[string]any{
        "tool_call_id":   toolCallID,
        "tool_name":      toolName,
        "agent_id":       e.agentID,
        "message":        pu.Message,
        "percent":        pu.Percent,
        "partial_result": pu.PartialResult,
    }
    msg, err := models.NewBusMessage(models.MessageTypeStatusUpdate, "executor", payload)
    if err != nil {
        e.logger.Warn("Failed to create progress bus message", "error", err)
        return
    }
    e.bus.Publish("tool.execution.progress", msg)
}
```

### 3.4 Bus Event: `tool.execution.progress`

New bus topic `tool.execution.progress` (wildcard: `tool.execution.*`).

Payload structure:
```json
{
  "tool_call_id": "call_abc123",
  "tool_name": "shell",
  "agent_id": "coder",
  "message": "running go build... output so far:",
  "percent": 45,
  "partial_result": "github.com/caimlas/meept/cmd/meept\nok  github.com/caimlas/meept/internal/agent  0.042s\n"
}
```

### 3.5 TUI Integration

The TUI already subscribes to `agent.progress`. We extend the `ProgressState` to carry tool-level detail:

```go
// In internal/tui/models/chat.go
type ProgressUpdateMsg struct {
    // ... existing fields ...
    ToolName      string `json:"tool_name,omitempty"`
    ToolMessage   string `json:"tool_message,omitempty"`
    ToolPercent   int    `json:"tool_percent,omitempty"`
}
```

In the TUI app's bus handler (`internal/tui/app.go`, ~line 585), add a case for the new topic:

```go
case "tool.execution.progress":
    // Extract and display tool-level progress inline
    if payloadMap, ok := e.Payload.(map[string]any); ok {
        // Update current tool detail in progress display
        // Show message/percent in the status line
    }
```

The TUI chat view can render these as transient inline messages or update the existing progress bar with tool-specific detail.

### 3.6 HTTP SSE Endpoint

New endpoint: `GET /api/v1/chat/stream` that provides SSE for real-time progress.

The server holds open an HTTP connection and forwards `tool.execution.progress` bus events as SSE data frames:

```
event: tool_progress
data: {"tool_name":"shell","message":"running go test...","percent":60}
```

This reuses the existing bus subscription pattern from `internal/bus/handler.go`.

### 3.7 Cache Behavior with Streaming

Cached results bypass execution entirely, so no progress events are emitted for cache hits. This is correct behavior -- the user should see the result instantly. However, we emit a synthetic progress event with `percent: 100` and `message: "cache hit"` so the TUI can indicate the source:

```go
if cachedResult, hit := e.cache.Get(toolName, args); hit {
    // Publish cache-hit progress event
    if e.bus != nil {
        e.publishToolProgress(ctx, toolCall.ID, toolName, tools.ProgressUpdate{
            Message:    "cache hit",
            Percent:    100,
            ToolCallID: toolCall.ID,
        })
    }
    // ... return cached result as before ...
}
```

### 3.8 Tools That Should Emit Streaming Progress

Priority tools for streaming implementation:

| Tool | Progress Updates |
|------|-----------------|
| `shell` | "running {cmd}...", output accumulation, exit code |
| `web_fetch` | "fetching {url}...", "parsing HTML...", byte count |
| `web_search` | "searching...", "found N results", result count |
| `file_read` | "reading {path}...", size |
| `file_write` | "writing {path}...", bytes written |
| `list_directory` | "listing {path}...", entry count |

Tools that do NOT need streaming (fast, synchronous):
`platform_status`, `platform_agents`, `platform_tools`, `memory_search`, `memory_store`, `memory_delete`, `task_create`, `task_update`, `delegate_task`, `ast_*`, `lsp_*`

---

## 4. Proposed Architecture: Terminate Hints

### 4.1 Add `Terminate` to ExecutionResult

```go
type ExecutionResult struct {
    ToolCallID string            `json:"tool_call_id"`
    Success    bool              `json:"success"`
    Result     any               `json:"result,omitempty"`
    Error      string            `json:"error,omitempty"`
    Cached     bool              `json:"cached,omitempty"`
    Evidence   []models.Evidence `json:"evidence,omitempty"`
    Terminate  bool              `json:"terminate,omitempty"`  // NEW
}
```

### 4.2 Add `Terminate` to ToolResult

```go
type ToolResult struct {
    Success  bool              `json:"success"`
    Result   any               `json:"result,omitempty"`
    Error    string            `json:"error,omitempty"`
    Evidence []models.Evidence `json:"evidence,omitempty"`
    Terminate bool             `json:"terminate,omitempty"`  // NEW
}
```

### 4.3 New Optional Interface: `TerminatingTool`

Tools that know their result is final can implement this interface:

```go
// TerminatingTool is an optional interface that tools can implement to signal
// that their result does not need further LLM processing. The executor reads
// the TerminateHint() value and propagates it to ExecutionResult.
type TerminatingTool interface {
    // TerminateHint returns true if the tool's result is a final answer
    // that should be returned to the user without LLM follow-up.
    // This is advisory; the executor only acts on it when ALL tools in the
    // batch agree (unanimous consent).
    TerminateHint(args map[string]any) bool
}
```

The `args` parameter allows tools to make per-call decisions. For example, `memory_store` might only hint termination when the call is a simple confirmation.

### 4.4 Executor Propagation

In `Execute()`, after successful tool execution:

```go
// Check for terminate hint
var terminate bool
if tt, ok := tool.(tools.TerminatingTool); ok {
    terminate = tt.TerminateHint(args)
}
// Also check if ToolResult carries Terminate field
if tr, ok := toolResult.(*tools.ToolResult); ok && tr != nil && tr.Terminate {
    terminate = true
}

return &ExecutionResult{
    // ... existing fields ...
    Terminate: terminate,
}
```

### 4.5 Batch-Level Termination Check

Add a helper method:

```go
// ShouldTerminate checks if ALL results in the batch indicate termination.
// Returns true only if every result has Terminate=true and at least one result exists.
func ShouldTerminate(results []*ExecutionResult) bool {
    if len(results) == 0 {
        return false
    }
    for _, r := range results {
        if r == nil || !r.Terminate {
            return false
        }
    }
    return true
}
```

### 4.6 Agent Loop Integration

In the agent loop (`internal/agent/loop.go`, ~line 1537), after `publishResult()`:

```go
// Publish agent result event
l.publishResult(conversationID, iteration, results)

// Check if all tools unanimously signal termination
if ShouldTerminate(results) {
    l.logger.Info("All tools signal termination, skipping LLM follow-up",
        "conversation", conversationID,
        "iteration", iteration,
    )
    // Build final message from the last result
    // The tool results are already in the conversation, so we can
    // synthesize a final response from the tool results directly
    return l.buildTerminateResponse(results), nil
}

// Continue loop for LLM to process tool results
continue
```

The `buildTerminateResponse` method formats the tool results into a user-facing response:

```go
func (l *AgentLoop) buildTerminateResponse(results []*ExecutionResult) string {
    var sb strings.Builder
    for _, r := range results {
        if r == nil || !r.Success {
            continue
        }
        if sb.Len() > 0 {
            sb.WriteString("\n\n")
        }
        if data, err := json.Marshal(r.Result); err == nil {
            sb.WriteString(string(data))
        }
    }
    return sb.String()
}
```

### 4.7 Tools That Should Signal Termination

| Tool | When to Terminate | Rationale |
|------|-------------------|-----------|
| `memory_store` | Always | Confirmation message needs no LLM processing |
| `memory_delete` | Always | Same as above |
| `platform_status` | Always | Static snapshot, no follow-up needed |
| `platform_agents` | Always | Read-only listing |
| `platform_tools` | Always | Read-only listing |
| `task_create` | Always | Returns task ID, confirmation |
| `task_update` | Always | Returns updated task |
| `shell` | Never | Output often needs LLM interpretation |
| `web_fetch` | Never | Content needs LLM analysis |
| `file_read` | Never | Content needs LLM processing |
| `file_write` | Never | May need follow-up actions |

### 4.8 Interaction with Adaptive Compression

Terminate-hinted results skip the follow-up LLM call entirely, so they do not undergo adaptive compression for LLM context. However, they are still stored in the conversation via `conv.AddToolResult()` before the termination check. This is correct -- the conversation record is complete for history/audit, but we avoid the unnecessary token cost of an LLM call that would just parrot the tool result back.

---

## 5. Pros/Cons Analysis

### 5.1 Streaming Progress: Bus Events vs Direct Callback

| Aspect | Pi Agent (callback) | Meept (bus events) |
|--------|--------------------|--------------------|
| Coupling | Tool directly calls callback; executor and tool are tightly coupled | Tool calls callback provided by executor; executor publishes to bus; consumers subscribe independently |
| Parallel safety | Callback is per-tool-call, naturally safe | Bus is thread-safe; each goroutine publishes independently |
| Testability | Must mock callback in tool tests | Tool tests mock callback; bus integration tested separately |
| Extensibility | Only one consumer (the caller) | Any number of consumers (TUI, HTTP, logging, metrics) |
| Overhead | Minimal (direct function call) | Slightly more (JSON marshal + bus publish), but amortized over tool duration |
| Back-pressure | Tool blocks if callback blocks | Bus uses buffered channels; if buffer full, publish returns 0 (no block) |

**Decision**: Meept's bus approach is better for our architecture because:
- Multiple consumers (TUI, HTTP SSE, future websocket clients) can independently subscribe
- Tools are decoupled from presentation concerns
- The bus already exists and has wildcard topic support
- The overhead of a bus publish is negligible compared to tool execution time

### 5.2 Terminate Hints: Unanimous Consent

| Aspect | Unanimous (chosen) | Majority | Any-single |
|--------|---------------------|----------|------------|
| Safety | High: one tool needing follow-up prevents skip | Medium: fast tools could outvote slow ones | Low: one "final" tool skips processing for others |
| Token savings | Conservative: only skips when all agree | Moderate | Aggressive: may skip needed processing |
| Complexity | Simple loop check | Requires counting/voting | Simplest check |
| Example risk | `memory_store` + `shell`: shell output still gets LLM processing (correct) | Same: majority might skip (incorrect) | `memory_store` alone skips, but `shell` result not processed (incorrect) |

**Decision**: Unanimous consent is the right default. It is conservative (never skips processing that might be needed) and simple to implement. If we find specific tools always run alone and always terminate, the agent loop could add a single-tool fast-path optimization later.

### 5.3 Amending vs Replacing Existing Capabilities

| Approach | Pros | Cons |
|----------|------|------|
| **Amend (chosen)** | Backward compatible; existing tools work unchanged; caching/compression/security preserved; incremental migration | Slightly more complex executor (type assertions) |
| **Replace** | Cleaner interface design | Breaks all 30+ tools, MCP tools, tests; must migrate everything at once; risk of regression |

**Decision**: Amend. The optional interface pattern (`StreamingTool`, `TerminatingTool`) lets us add capabilities without touching tools that don't need them. The executor uses runtime type assertions (`if st, ok := tool.(tools.StreamingTool); ok`) which is idiomatic Go.

---

## 6. Implementation Phases

### Phase 1: Core Interfaces & Executor (2-3 days)

**Files to create:**
- `internal/tools/streaming.go` -- `StreamingTool` interface, `ProgressUpdate` struct, `TerminatingTool` interface

**Files to modify:**
- `internal/tools/interface.go` -- Add `Terminate` field to `ToolResult`; add `NewSuccessResultWithTerminate()` helper
- `internal/agent/executor.go`:
  - Add `bus` field to `Executor` struct
  - Add `WithExecutorBus()` option
  - Modify `Execute()` to detect `StreamingTool` and `TerminatingTool` interfaces
  - Add `publishToolProgress()` method
  - Add `ShouldTerminate()` helper function
  - Emit cache-hit progress events
- `internal/agent/executor_test.go` -- Tests for streaming detection, terminate propagation, `ShouldTerminate()`

**Verification:**
- All existing tests pass (no regressions)
- New tests verify type-assertion-based dispatching
- `MockTool` can be extended to `MockStreamingTool` for tests

### Phase 2: Bus Integration & TUI Display (2-3 days)

**Files to create:**
- `internal/comm/http/sse.go` -- SSE writer utility for HTTP streaming

**Files to modify:**
- `internal/agent/loop.go`:
  - Wire bus into executor via `WithExecutorBus(l.bus)` during construction
  - Add termination check after tool results (`ShouldTerminate`)
  - Add `buildTerminateResponse()` method
  - Publish `tool.execution.complete` event with terminate flag
- `internal/agent/loop_test.go` -- Tests for terminate path
- `internal/tui/app.go` -- Subscribe to `tool.execution.progress`, update `ProgressState` with tool-level detail
- `internal/tui/models/chat.go` -- Extend `ProgressUpdateMsg` with tool fields
- `internal/tui/progress.go` -- Render tool-level progress (tool name, message, percent bar)
- `internal/comm/http/server.go`:
  - Add `GET /api/v1/chat/stream` SSE endpoint
  - Subscribe to `tool.execution.progress` and forward as SSE events
  - Subscribe to `agent.progress` and forward as SSE events
- `internal/comm/http/server_test.go` -- Test SSE endpoint

**Verification:**
- TUI shows tool progress during shell/web_fetch execution
- SSE endpoint delivers real-time progress events
- Terminate path skips LLM call and returns tool result directly
- Agent loop logs termination decisions

### Phase 3: Streaming Tool Implementations (3-4 days)

**Files to modify:**
- `internal/tools/builtin/shell.go`:
  - Implement `StreamingTool` on `ShellExecuteTool`
  - Emit progress: "running {cmd}...", accumulate output in `PartialResult`, emit exit code
- `internal/tools/builtin/web_fetch.go`:
  - Implement `StreamingTool` on `WebFetchTool`
  - Emit progress: "fetching {url}...", "received {N} bytes", "parsing HTML..."
- `internal/tools/builtin/filesystem.go`:
  - Implement `StreamingTool` on `FileReadTool` and `FileWriteTool`
  - Emit progress: "reading {path} ({N} bytes)", "writing {path} ({N} bytes)"
- `internal/tools/builtin/platform.go`:
  - Implement `TerminatingTool` on `PlatformStatusTool`, `PlatformAgentsTool`, `PlatformToolsTool`
- `internal/tools/builtin/tool_cron_create.go`, `tool_schedule_create.go`, `tool_schedule_delete.go`:
  - Implement `TerminatingTool` on schedule management tools
- Memory tools (wherever `memory_store`, `memory_delete` are defined):
  - Implement `TerminatingTool`
- `internal/tools/mcp/tool.go`:
  - MCP tools cannot implement `StreamingTool` (protocol limitation), but can implement `TerminatingTool` if the MCP server signals finality

**Verification:**
- Shell tool emits progress events during execution
- Web fetch tool shows download progress
- Platform tools trigger termination path
- Memory store/delete trigger termination path
- MCP tools continue to work without streaming
- Integration test: run agent loop with terminating tools, verify LLM call is skipped

---

## 7. Tool Interface Changes

### Before (current)

```go
// internal/tools/interface.go

type Tool interface {
    Name() string
    Description() string
    Parameters() llm.FunctionParameters
    Execute(ctx context.Context, args map[string]any) (any, error)
}

type ToolResult struct {
    Success  bool              `json:"success"`
    Result   any               `json:"result,omitempty"`
    Error    string            `json:"error,omitempty"`
    Evidence []models.Evidence `json:"evidence,omitempty"`
}
```

### After (proposed)

```go
// internal/tools/interface.go -- UNCHANGED (backward compatible)

type Tool interface {
    Name() string
    Description() string
    Parameters() llm.FunctionParameters
    Execute(ctx context.Context, args map[string]any) (any, error)
}

type ToolResult struct {
    Success   bool              `json:"success"`
    Result    any               `json:"result,omitempty"`
    Error     string            `json:"error,omitempty"`
    Evidence  []models.Evidence `json:"evidence,omitempty"`
    Terminate bool              `json:"terminate,omitempty"` // NEW: advisory flag
}
```

```go
// internal/tools/streaming.go -- NEW FILE

package tools

import (
    "context"
    "encoding/json"
)

// ProgressUpdate represents a streaming progress update from a tool.
type ProgressUpdate struct {
    Message       string          `json:"message"`
    Percent       int             `json:"percent"`         // 0-100, -1 for indeterminate
    PartialResult json.RawMessage `json:"partial_result,omitempty"`
    ToolCallID    string          `json:"tool_call_id"`
}

// StreamingTool is an optional interface for tools that emit progress updates.
type StreamingTool interface {
    ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(ProgressUpdate)) (any, error)
}

// TerminatingTool is an optional interface for tools whose results are final.
type TerminatingTool interface {
    TerminateHint(args map[string]any) bool
}
```

### Executor Dispatch (simplified)

```go
func (e *Executor) Execute(ctx context.Context, toolCall llm.ToolCall) *ExecutionResult {
    // ... cache check, security check (unchanged) ...

    // Execute with optional streaming
    var toolResult any
    var toolErr error

    if st, ok := tool.(tools.StreamingTool); ok && e.bus != nil {
        toolResult, toolErr = st.ExecuteStreaming(ctx, args, func(pu tools.ProgressUpdate) {
            pu.ToolCallID = toolCall.ID
            e.publishToolProgress(toolCall.ID, toolName, pu)
        })
    } else {
        toolResult, toolErr = tool.Execute(ctx, args)
    }

    // ... error handling (unchanged) ...

    // Extract terminate hint
    var terminate bool
    if tt, ok := tool.(tools.TerminatingTool); ok {
        terminate = tt.TerminateHint(args)
    }
    if tr, ok := toolResult.(*tools.ToolResult); ok && tr != nil && tr.Terminate {
        terminate = true
    }

    return &ExecutionResult{
        // ... existing fields ...
        Terminate: terminate,
    }
}
```

---

## 8. Bus Events

### New Topics

| Topic | Type | Source | Payload | When |
|-------|------|--------|---------|------|
| `tool.execution.progress` | `status_update` | `executor` | `{tool_call_id, tool_name, agent_id, message, percent, partial_result}` | During tool execution (StreamingTool only) |
| `tool.execution.complete` | `event` | `executor` | `{tool_call_id, tool_name, agent_id, success, terminate, cached}` | After each tool completes |

### Existing Topics (unchanged)

| Topic | When |
|-------|------|
| `agent.action` | Before tool batch execution |
| `agent.progress` | Stage transitions (thinking, executing, responding) |
| `agent.result` | After tool batch completes |

### Event Flow Diagram

```
Agent Loop
  |
  +-- publishAction() --> bus: "agent.action"
  +-- publishProgress() --> bus: "agent.progress" (stage="executing")
  |
  +-- executor.ExecuteAll()
  |     |
  |     +-- goroutine 1: shell tool
  |     |     +-- publishToolProgress() --> bus: "tool.execution.progress" (percent=10)
  |     |     +-- publishToolProgress() --> bus: "tool.execution.progress" (percent=50)
  |     |     +-- publishToolProgress() --> bus: "tool.execution.progress" (percent=100)
  |     |     +-- return ExecutionResult{Terminate: false}
  |     |
  |     +-- goroutine 2: memory_store tool
  |           +-- return ExecutionResult{Terminate: true}
  |
  +-- ShouldTerminate() --> false (not unanimous)
  +-- publishResult() --> bus: "agent.result"
  +-- continue loop (LLM processes results)
```

Termination flow:

```
Agent Loop
  |
  +-- publishAction() --> bus: "agent.action"
  +-- executor.ExecuteAll()
  |     |
  |     +-- goroutine 1: memory_store
  |     |     +-- return ExecutionResult{Terminate: true}
  |     |
  |     +-- goroutine 2: platform_status
  |           +-- return ExecutionResult{Terminate: true}
  |
  +-- ShouldTerminate() --> true (unanimous)
  +-- buildTerminateResponse() --> return final string
  +-- (no LLM follow-up call)
```

---

## 9. Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Progress events flood the bus for fast tools | Rate-limit progress events to max 10/second per tool call; debounce if percent unchanged |
| MCP tools cannot stream | MCP tools simply don't implement `StreamingTool`; they work as before. Future: MCP protocol may add progress support |
| Terminate hint incorrectly skips LLM processing | Unanimous consent requirement prevents single-tool mistakes; tools that need follow-up never hint termination |
| SSE connection leaks | Use context cancellation, heartbeat pings, client disconnect detection |
| Breaking existing tool implementations | All changes are additive (new interfaces); existing `Tool` interface unchanged; runtime type assertions, not compile-time |
| Cache hit progress events are noise | Cache-hit events are only emitted if bus is configured; they carry `percent: 100` so UI can display them as instant |

---

## 10. Testing Strategy

### Unit Tests
- `internal/tools/streaming_test.go` -- Test `ProgressUpdate` JSON serialization
- `internal/agent/executor_test.go` -- Test `StreamingTool` detection, `TerminatingTool` detection, `ShouldTerminate()`, cache-hit progress
- `internal/comm/http/sse_test.go` -- Test SSE writer

### Integration Tests
- End-to-end: register a `MockStreamingTool`, execute via `Executor`, verify bus receives progress events
- End-to-end: register terminating tools, execute batch, verify `ShouldTerminate()` returns true
- Agent loop: verify terminate path returns without LLM call
- TUI: verify `tool.execution.progress` events update `ProgressState`

### Manual Verification
- Run `./bin/meept chat` and execute a long-running shell command; verify TUI shows progress
- Run HTTP server and connect to `/api/v1/chat/stream`; verify SSE events arrive
- Execute `memory_store` + `platform_status` in a single turn; verify no LLM follow-up
- Execute `shell` + `memory_store` in a single turn; verify LLM follow-up happens (shell blocks termination)
