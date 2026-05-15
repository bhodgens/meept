# Agent Loop Remediation Plan

**Date**: 2026-05-14
**Status**: Implementation complete, reviewed and verified
**Severity**: High — agent cannot answer basic project questions, tasks silently fail

## Investigation Summary

Multiple interconnected issues prevent the agent from functioning correctly. The root causes fall into four categories: (1) missing context, (2) data loss in task pipeline, (3) invisible failures, and (4) classifier degradation.

---

## P1: Job Result Not Propagated to Step (Data Loss Bug)

**Symptom**: Steps complete with data in the queue DB but the reviewer sees "Empty result field — no output generated." The agent produces correct output, but it's thrown away before review.

**Root Cause**: `internal/queue/queue.go:219-221` — the `queue.job.completed` bus event only includes `job_id`, not the `result`. The orchestrator receives the event, extracts `result` from the payload (which is nil), and passes an empty string to the tactical scheduler, which sets an empty step result.

**Evidence**:
- Queue DB job `job-20260515043231.038846000-0001` has full JSON result with substantive response
- Step `step-task-...-0-1778819551038578000` has empty `result` column in tasks DB
- Reviewer correctly identifies empty result, rejects the step

**Fix**:
```go
// internal/queue/queue.go, Complete() method, line 219
q.publishEvent("queue.job.completed", map[string]any{
    KeyJobID: jobID,
    "result":  result,  // ADD THIS LINE
})
```

**Files to modify**:
- `internal/queue/queue.go` — add `result` to bus event payload

**Verification**: After fix, step results in tasks DB should contain the agent's response. Reviewer should see actual content and approve/deny based on quality.

---

## P2: Review Rejection Invisible to User

**Symptom**: User sees "starting task" then nothing. No feedback about review rejection, revision cycles, or retry attempts.

**Root Cause**: Review events are published on `step.*` topic prefix. The TUI subscribes to `task.*` and `agent.*` — missing `step.*` entirely. `ChatHandler` only subscribes to `task.completed` and `task.failed`.

**Evidence**:
- `step.review_completed` published at `internal/agent/review_manager.go:489`
- `step.review_requested` published at `internal/agent/tactical.go:502`
- TUI subscriptions in `internal/tui/events.go:46-55` — no `step.*`
- ChatHandler subscriptions in `internal/agent/handler.go` — no review events
- Bus wildcard matching requires exact segment count (`step.*` won't match `task.*`)

**Fix (two-part)**:

**Part A**: Re-publish review events under `task.*` prefix for backward compatibility:
```go
// In review_manager.go or tactical.go, also publish:
bus.Publish("task.review_completed", payload)
bus.Publish("task.review_requested", payload)
```

**Part B**: Add handlers in TUI and ChatHandler:

1. `internal/tui/events.go` — add `"step.*"` to subscription topics
2. `internal/tui/app.go` (around line 720) — handle `"step.review_completed"` to show rejection/revision feedback in chat
3. `internal/tui/sidebar.go` (around line 511) — show review status in sidebar
4. `internal/agent/handler.go` — subscribe to `"step.review_completed"` and push to linked sessions via `chat.response`

**Files to modify**:
- `internal/tui/events.go` — add `"step.*"` subscription
- `internal/tui/app.go` — add review event handlers
- `internal/tui/sidebar.go` — add review status display
- `internal/agent/handler.go` — subscribe to review events
- `internal/agent/review_manager.go` — also publish under `task.*` prefix

---

## P3: Agent Cannot Answer "What Is This Project?"

**Symptom**: When asked about the project, the agent only knows about security components (from CLAUDE.md's architecture table). It never reads README.md. It searches memory (empty), web (irrelevant), and produces a narrow security-focused summary.

**Root Causes** (three contributing factors):

### P3a: README.md Never Scanned

The artifact scanner (`internal/context/artifact_scanner.go`) only looks for `CLAUDE.md` and `.claude/`. It never scans `README.md`.

**Fix**: Add README.md scanning to `artifact_scanner.go`:
1. In `ScanWorkingDir()`, after the CLAUDE.md check (line 46), add a README.md check
2. Add a `READMEContent string` field to the `Artifacts` struct in `types.go`
3. In `ContextBuilder`, include README.md content in the prompt (either full content or summary)
4. For README, unlike CLAUDE.md, a summary is appropriate since READMEs can be very long

### P3b: CLAUDE.md Summarized (Should Be Full Content)

The `ContextBuilder` decomposes CLAUDE.md into subsections and only injects task-relevant pieces. For example, `buildGeneralContext()` only extracts architecture summary + conventions. The full CLAUDE.md content is never provided, even though `CLAUDEDocument.RawContent` stores it.

The user requires that CLAUDE.md and AGENT.md content is provided in full — these are authoritative project instructions that should not be lossy-summarized.

**Fix**: Bypass the ContextBuilder for CLAUDE.md. Inject `RawContent` directly:
1. In `artifact_integration.go`, `BuildFullArtifactContext()`: if `CLAUDEMD.RawContent` exists, include it verbatim as a section
2. Remove or deprecate the ContextBuilder's task-type classification for CLAUDE.md
3. Keep the ContextBuilder for README.md (summary is appropriate there)

### P3c: Agent Not Instructed to Read Project Files

The chat agent's AGENT.md / system prompt doesn't instruct it to proactively use `file_read` when asked about the project. It relies on the artifact context (which is lossy) and memory (which is empty).

**Fix**: Add guidance to the chat agent's system prompt or baseline capabilities about using `file_read` for project-related questions. Or better: fix P3a and P3b so the agent already has the information without needing to call tools.

**Files to modify**:
- `internal/context/artifact_scanner.go` — add README.md scanning
- `internal/context/types.go` — add `READMEContent` field to `Artifacts`
- `internal/context/context_builder.go` — include README summary, use CLAUDE.md raw content
- `internal/agent/artifact_integration.go` — inject full CLAUDE.md, add README section

---

## P4: Context Compaction May Lose Artifact Context

**Symptom**: In long conversations, artifact context (CLAUDE.md, README.md) could theoretically be lost if the system prompt is rebuilt differently after compaction.

**Finding**: After investigation, the system prompt is stored separately from the message history and is **preserved by all compaction mechanisms**:
- `ContextCompactor` separates system messages and always preserves them
- `ContextFirewall` keeps system messages when dropping old context
- `TruncateByTokens` accounts for system prompt as overhead, never truncates it
- The system prompt is rebuilt fresh on each agent turn

**Mitigation for P3 changes**: Since the full CLAUDE.md and README.md content will be in the system prompt, they will survive all compaction. No additional fix needed beyond P3.

**However**, there's one edge case: if the artifact context is very large, it eats into the context budget available for conversation messages. We should:
1. Consider re-injecting artifact context as an anchor message (`AddAnchorMessage`) in addition to the system prompt, for double protection
2. Monitor token usage when CLAUDE.md is large

**Files to modify**: None beyond P3 (system prompt preservation is already correct).

---

## P5: Classifier Timeout Cascades into Bad Routing

**Symptom**: LLM classifier fails with `context deadline exceeded` (5s timeout), falls back to keyword matching, which produces weak/spurious results.

**Root Cause**: `internal/agent/llm_classifier.go:17` — `defaultClassifierTimeout = 5 * time.Second`. No dedicated small model configured for classification, so it hits the main `glm-4.7` model which is too slow.

**Evidence**:
```
level=WARN msg="LLM classifier failed, trying keyword" error="request failed: Post \"https://api.z.ai/api/coding/paas/v4/chat/completions\": context deadline exceeded"
```

**Fix**: Increase the default timeout to 10s, and wire the configured small model for classification:
1. `internal/agent/llm_classifier.go:17` — change default to `10 * time.Second`
2. `internal/daemon/components.go` (lines 256-265) — ensure `LLMClassifierConfig.Timeout` is explicitly set from config or use a longer default
3. Verify that the classifier uses the small model (`glm-4.5-air`) when configured

**Files to modify**:
- `internal/agent/llm_classifier.go` — increase default timeout
- `internal/daemon/components.go` — wire classifier config timeout

---

## P6: Compound Intent Detection Fast-Pathed Incorrectly

**Symptom**: Dispatcher detects compound intent (2+ keyword matches), but strategic planner fast-paths it as "simple" because the input is short (< 100 chars).

**Root Cause**: `internal/agent/strategic.go:303-332` — `shouldDecompose()` doesn't have a special case for `IntentCompound`. It falls through to the length check, and short compound inputs get fast-pathed.

**Evidence**:
```
level=INFO msg="Compound intent detected" intents=2 type=parallel
...
level=INFO msg="Fast-path: skipping decomposition for simple request" intent=compound
```

The dispatcher's compound detection is wasted because the planner overrides it.

**Fix**: Add `IntentCompound` to the `shouldDecompose` function:
```go
func (sp *StrategicPlanner) shouldDecompose(req PlanRequest) bool {
    switch req.Intent {
    case string(IntentChat), string(IntentReport), string(IntentRecall),
         string(IntentPlatform), string(IntentSearch), string(IntentAnalyze):
        return false
    case string(IntentCompound):
        return true  // ADD: Compound intents should always decompose
    }
    // ... rest of length/complexity checks
}
```

**Files to modify**:
- `internal/agent/strategic.go` — add `IntentCompound` case returning `true`

---

## P7: Tasks Not Recovered on Daemon Restart

**Symptom**: When the daemon restarts, in-progress tasks remain in `executing`/`pending`/`planning` state in the DB but nothing resumes them. The tasks DB shows tasks from months ago still in `executing`:

```
task-20260227213118... | executing | 2026-02-27 (2.5 months ago)
task-20260417050817... | pending   | 2026-04-17
task-20260411051321... | pending   | 2026-04-11
task-20260515043231... | executing | 2026-05-14 (tonight)
```

**Root Cause**: No startup recovery for tasks. `internal/daemon/daemon.go` has `RecoverPendingFollowUps` for the queue but nothing for the task store. There is no `Recover()` or startup scan method in `internal/task/`.

**Fix**:
1. Add a `RecoverStaleTasks()` method to `internal/task/store.go`:
   - Query all tasks in non-terminal states (`pending`, `planning`, `executing`, `reviewing`)
   - Mark them as `failed` with reason `"daemon_shutdown"`
   - Mark their pending steps as `failed` with reason `"daemon_shutdown"`
   - Return count of recovered tasks for logging
2. Call it during daemon startup in `internal/daemon/daemon.go` (after task registry init, around line 351)
3. Log the count of recovered tasks

**Files to modify**:
- `internal/task/store.go` — add `RecoverStaleTasks()` method
- `internal/daemon/daemon.go` — call recovery on startup

---

## P8: Session-Task Links Not Persisted

**Symptom**: `session_tasks` table has 0 rows despite 12 tasks and many sessions.

**Root Cause**: `Dispatcher.createTask()` at `internal/agent/dispatcher.go:484` calls `t.LinkSession(sessionID)` which only updates the in-memory `Task.LinkedSessions` slice. It never calls `taskStore.LinkSession()` to persist the link to the DB.

**Fix**: After `taskStore.Create(t)`, call `taskStore.LinkSession(t.ID, sessionID)`:
```go
func (d *Dispatcher) createTask(...) *task.Task {
    t := task.NewTask(summary, input)
    t.LinkSession(sessionID)
    if d.taskStore != nil {
        if err := d.taskStore.Create(t); err != nil {
            return nil
        }
        // ADD: Persist the session-task link
        if err := d.taskStore.LinkSession(t.ID, sessionID); err != nil {
            d.logger.Warn("Failed to link session", "error", err)
        }
    }
    return t
}
```

**Files to modify**:
- `internal/agent/dispatcher.go` — add `LinkSession` call after task creation

---

## P9: Step State Transitions Not Tracked

**Symptom**: `task_state_transitions` table has 0 rows despite 17 steps going through multiple state changes.

**Root Cause**: State transitions are only recorded in memory by the orchestrator/tactical scheduler. The `StepStore` update methods don't insert into `task_state_transitions`.

**Fix**: In `StepStore.Update()` or the tactical scheduler's state change methods, insert a row into `task_state_transitions` whenever a step's state changes.

**Files to modify**:
- `internal/task/step.go` — add transition recording in `Update()` or state change methods

---

## Implementation Priority

| Priority | Issue | Impact | Effort |
|----------|-------|--------|--------|
| **P1** | Job result not propagated | All task results lost — critical data loss | Trivial (1 line) |
| **P3** | Agent can't answer project questions | Core functionality broken | Medium (scanner + builder changes) |
| **P2** | Review rejection invisible | Users see no feedback on failures | Medium (TUI + handler changes) |
| **P6** | Compound intent fast-pathed | Compound tasks not decomposed properly | Trivial (1 line) |
| **P7** | Tasks not recovered on restart | Stale tasks accumulate forever | Small (recovery method) |
| **P5** | Classifier timeout | Poor routing quality | Small (config change) |
| **P8** | Session-task links not persisted | Can't correlate sessions to tasks | Trivial (2 lines) |
| **P4** | Context compaction edge case | Low risk — already mitigated | None needed |
| **P9** | State transitions not tracked | No audit trail for debugging | Small |

---

## Diagnostic Commands

Use these to investigate issues in production:

```bash
# Queue jobs (most useful — has actual agent responses)
sqlite3 ~/.meept/queue.db "SELECT id, agent_id, state, substr(result, 1, 300), error FROM jobs ORDER BY rowid DESC LIMIT 10;"

# Dead letter queue (permanently failed jobs)
sqlite3 ~/.meept/queue.db "SELECT * FROM dead_letter ORDER BY rowid DESC LIMIT 10;"

# Task tracking
sqlite3 ~/.meept/tasks.db "SELECT id, name, state, total_jobs, completed_jobs, failed_jobs, created_at FROM tasks ORDER BY created_at DESC;"

# Step results (will be empty until P1 is fixed)
sqlite3 ~/.meept/tasks.db "SELECT id, task_id, description, state, substr(result, 1, 200), revision_count FROM task_steps ORDER BY created_at DESC;"

# Orphaned tasks (non-terminal states)
sqlite3 ~/.meept/tasks.db "SELECT id, name, state, created_at FROM tasks WHERE state NOT IN ('completed', 'failed', 'cancelled');"

# Session-task links (will be empty until P8 is fixed)
sqlite3 ~/.meept/tasks.db "SELECT * FROM session_tasks;"

# Session messages (conversation history)
sqlite3 ~/.meept/sessions.db "SELECT id, session_id, role, substr(content, 1, 200), created_at FROM session_messages ORDER BY rowid DESC LIMIT 20;"
```

---

## Key Files Reference

| File | Role |
|------|------|
| `internal/queue/queue.go` | **P1 bug** — missing `result` in bus event |
| `internal/agent/orchestrator.go` | Receives job events, passes to tactical |
| `internal/agent/tactical.go` | Step lifecycle, review trigger, event publishing |
| `internal/agent/review_manager.go` | Review execution, rejection, revision creation |
| `internal/agent/strategic.go` | **P6 bug** — fast-path for compound intents |
| `internal/agent/llm_classifier.go` | **P5** — 5s timeout |
| `internal/agent/dispatcher.go` | Intent classification, task creation, **P8** |
| `internal/agent/handler.go` | ChatHandler — missing review subscriptions |
| `internal/context/artifact_scanner.go` | **P3a** — no README.md scanning |
| `internal/context/context_builder.go` | **P3b** — lossy CLAUDE.md summarization |
| `internal/context/types.go` | Artifacts struct — needs README field |
| `internal/agent/artifact_integration.go` | Artifact context injection into prompts |
| `internal/tui/events.go` | **P2** — missing `step.*` subscription |
| `internal/tui/app.go` | TUI event dispatch — no review handlers |
| `internal/task/store.go` | Task persistence — **P7** recovery needed |
| `internal/task/step.go` | Step persistence, state transitions |
| `internal/daemon/daemon.go` | Startup sequence — needs task recovery |
| `internal/daemon/components.go` | Component wiring — classifier config |
