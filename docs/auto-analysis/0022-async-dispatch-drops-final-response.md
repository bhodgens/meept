# Async Dispatch Drops Final Response - User Never Sees LLM Output or Error

**Date**: 2026-05-15
**Phase**: 3 (multi-agent orchestration)
**Severity**: high
**Component**: `internal/agent/dispatcher.go`, `internal/rpc/`, `cmd/meept/chat.go`

## Description

When the dispatcher classifies a request and routes it to a specialist agent, it uses "async dispatch": the CLI receives an immediate plan acknowledgment and disconnects. The actual LLM response (or error) is published to a message bus topic that the CLI is no longer subscribed to. The user sees only the task plan summary, never the actual response content.

For compound intents, this is especially bad: the CLI prints a plan with "3 subtasks | est. 12-17 min" and exits, while the task runs entirely in the background. Task completion or failure messages are published to topics with zero subscribers (`delivered=0`), so they are lost entirely.

## Reproduction

1. Start the daemon with dispatcher enabled (`use_dispatcher=true`)
2. Send: `./bin/meept chat "hello, what can you do?"`
3. CLI output shows only:
```
## starting task

**task:** hello, what can you do?
**id:** task-20260516032128.250653000
**plan:** task-20260516032128.250653000 | 3 subtasks | est. 12-17 min

**agents:** chat, scheduler
**subtasks:**
- hello, what can you do? (chat)
- hello, what can you do? (scheduler)
- hello, what can you do? (chat)

you will receive updates as subtasks complete.
```
4. CLI exits with code 0
5. No further output is ever shown to the user
6. In daemon logs, the actual response/error is published to `task-failed-task-XXX` with `delivered=0`

## Evidence

```
level=INFO msg="Async dispatch: sending ack and publishing plan request"
level=DEBUG msg="Sent chat response" reply_to=proxy-XXX delivered=1
level=DEBUG msg="rpc: read error" error="failed to read length: EOF"
```

(The CLI has disconnected at this point)

Later, when the task actually completes or fails:
```
level=WARN msg="Task failed, pushing error to chat" task_id=task-XXX name="hello, what can you do?"
level=DEBUG msg="Sent chat response" reply_to=task-failed-task-XXX delivered=0
```

The `delivered=0` means no subscriber was listening for the result.

## Root Cause

The async dispatch pattern was designed for long-running tasks where the user would maintain a persistent connection (e.g., TUI interactive mode). For single-shot CLI invocations (`meept chat "message"`), the CLI sends the message, receives the plan acknowledgment, and exits. There is no mechanism to:

1. Wait for the actual response before exiting
2. Stream results back to the CLI as subtasks complete
3. Deliver the final answer to the user

The fundamental issue is that the CLI subscribes to `chat.response` for a single response, but the dispatcher sends the plan ack on that topic and the actual results are sent on task-specific topics.

## Fix Applied (2026-05-16)

Added synchronous dispatch mode and budget pre-check to `ChatHandler` in `internal/agent/handler.go`.

### Changes
1. Added `budget *llm.Budget` field and `SetBudget()` method to `ChatHandler` — wired from daemon at `internal/daemon/components.go`.
2. Added `syncMode bool` field and `SetSyncMode()` method to `ChatHandler` — enables synchronous dispatch mode.
3. Added `waitForTaskCompletion(ctx, taskID)` helper that polls `taskStore.GetByID()` on a 2-second ticker (up to 10-minute timeout) waiting for the task to reach a terminal state.
4. In the async dispatch path of `handleRequest`:
   - **Budget check (Issue 0039)**: If budget is exceeded, the task is immediately set to `failed` state and the budget error is returned instead of sending an async ack.
   - **Sync mode (Issue 0022)**: When `syncMode` is enabled, the handler publishes the plan request and then calls `waitForTaskCompletion()`, blocking until the task reaches a terminal state before returning the result.

### Remaining work for full end-to-end CLI fix
The handler-side sync mode blocks the goroutine and returns the result through the normal `chat.response` path. For the CLI client to actually receive it, the caller (e.g. `cmd/meept/chat.go` or RPC proxy) must keep the connection open. This is handled by the RPC proxy which waits for the response.

For truly async tasks (sync mode disabled), full end-to-end result delivery requires additional work:
- Subscribe CLI clients to `task-completed-*` and `task-failed-*` topics
- Implement `meept tasks show <task-id>` for retrieving results later

## Status

**FIXED** (handler-side). Sync mode and budget pre-check are implemented. Full end-to-end CLI delivery for async dispatch still requires caller-side connection management in the CLI tooling.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
