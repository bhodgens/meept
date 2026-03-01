# Plan: Agent Communication and Reporting Improvements

## Problem Summary

Analysis of daemon logs revealed multiple systemic issues in the multi-agent orchestration system:

1. **Incorrect Intent Classification**: "give me a report on what you did" was classified as `schedule` intent instead of `chat`/`report`
2. **Over-decomposition**: Simple requests get decomposed into 4 steps when they should run as single-agent tasks
3. **Token Budget Retry Storm**: All 4 workers retry the same failing job, creating a cascade of failures
4. **Worker State Machine Bug**: Invalid state transitions (complete/error → claiming)
5. **Non-retryable Errors Trigger Retries**: Token budget exhaustion should not trigger job retries
6. **Poor Reporting**: Agents don't provide clear summaries of completed work

---

## Root Cause Analysis

### 1. Intent Classification Gaps (`internal/agent/dispatcher.go:365-428`)

The `KeywordClassifier` lacks patterns for:
- Report/summary requests ("give me a report", "summarize", "what did you do")
- Conversational queries about prior work

### 2. Async Dispatch Over-reach (`internal/agent/dispatcher.go:521-538`)

`ShouldDispatchAsync()` returns `true` for ALL `schedule` intents, even simple ones:
```go
case "code", "debug", "plan", "schedule":
    return true
```

This forces every matching intent through the strategic planner → tactical scheduler pipeline, creating multi-step tasks even when unnecessary.

### 3. Strategic Planner Always Decomposes (`internal/agent/strategic.go:180-201`)

The planner prompt is hardcoded to decompose tasks into multiple steps with no fast-path for simple requests:
```go
prompt := fmt.Sprintf(plannerPromptTemplate, sp.maxPlanSteps, req.Input)
```

### 4. Worker State Machine Mismatch (`internal/worker/state.go:44-52`)

`CanClaim()` returns true for `StateComplete` and `StateError`:
```go
func (s State) CanClaim() bool {
    return s == StateIdle || s == StateComplete || s == StateError
}
```

But `ValidTransitions` only allows `Complete→Idle` and `Error→Idle`, not `→Claiming`:
```go
StateComplete:   {StateIdle, StateStopping},
StateError:      {StateIdle, StateStopping},
```

### 5. Indiscriminate Retry Logic (`internal/worker/worker.go:254-259`)

Retries happen for ALL errors without checking if the error is recoverable:
```go
if job.CanRetry() {
    if err := w.queue.Retry(ctx, job.ID); err != nil {
        // ...
    }
}
```

### 6. No Retry Backoff/Cooldown (`internal/queue/store.go`)

Retried jobs go directly back to `pending` state with no delay, allowing immediate re-claiming.

### 7. Missing Conversation Context for Reports

Agents searching for "what I did" lack access to:
- Recent task completion history
- Session-specific conversation summaries
- Step results from just-completed tasks

---

## Implementation Plan

### Phase 1: Intent Classification Improvements

**Files to modify:**
- `internal/agent/dispatcher.go`

**Changes:**

1. Add new keyword patterns to `KeywordClassifier.Classify()`:
```go
// Report/summary requests - route to chat with NO planning
{[]string{"report", "summary", "summarize", "what did", "what have you", "status update"}, "report", "chat", 0.85, false},

// Introspection queries - route to chat
{[]string{"what have i asked", "previous conversation", "earlier today"}, "recall", "chat", 0.8, false},
```

2. Add new `report` intent type handling in `shouldCreateTask()`:
```go
case "report", "recall", "chat":
    return false  // Never create tasks for these
```

3. Update `ShouldDispatchAsync()` to exclude simple intents:
```go
func (d *Dispatcher) ShouldDispatchAsync(result *DispatchResult) bool {
    // Never async for simple queries
    if result.Intent.Type == "report" || result.Intent.Type == "recall" || result.Intent.Type == "chat" {
        return false
    }
    // ...existing logic
}
```

### Phase 2: Strategic Planner Fast-Path

**Files to modify:**
- `internal/agent/strategic.go`

**Changes:**

1. Add `ShouldDecompose()` check before calling LLM:
```go
func (sp *StrategicPlanner) shouldDecompose(req PlanRequest) bool {
    // Simple intents don't need decomposition
    simpleIntents := map[string]bool{
        "chat": true, "report": true, "recall": true,
        "search": true, "analyze": true,
    }
    if simpleIntents[req.Intent] {
        return false
    }

    // Short requests without action verbs don't need decomposition
    if len(req.Input) < 100 && !containsActionVerbs(req.Input) {
        return false
    }

    return true
}
```

2. Modify `Plan()` to create single-step fallback for simple requests:
```go
func (sp *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) error {
    // ...existing setup...

    var steps []*task.TaskStep
    if sp.shouldDecompose(req) {
        steps, err = sp.generatePlan(ctx, req)
        if err != nil {
            steps = sp.createFallbackSteps(req)
        }
    } else {
        // Fast-path: single step, no LLM call
        steps = sp.createFallbackSteps(req)
    }
    // ...rest of method
}
```

### Phase 3: Fix Worker State Machine

**Files to modify:**
- `internal/worker/state.go`
- `internal/worker/worker.go`

**Changes:**

1. Update `ValidTransitions` to allow direct claiming from terminal states:
```go
var ValidTransitions = map[State][]State{
    StateIdle:       {StateClaiming, StateStopping},
    StateClaiming:   {StateProcessing, StateIdle, StateError, StateStopping},
    StateProcessing: {StateComplete, StateError, StateStopping},
    StateComplete:   {StateIdle, StateClaiming, StateStopping},  // Added Claiming
    StateError:      {StateIdle, StateClaiming, StateStopping},  // Added Claiming
    StateStopping:   {StateStopped},
    StateStopped:    {StateIdle},
}
```

OR (preferred): Force transition through Idle in `tryProcessJob()`:
```go
func (w *Worker) tryProcessJob(ctx context.Context) (bool, error) {
    w.mu.Lock()
    // Ensure we're in a claimable state, transitioning through Idle if needed
    if w.State == StateComplete || w.State == StateError {
        w.setState(StateIdle)
    }
    if !w.State.CanClaim() {
        w.mu.Unlock()
        return false, nil
    }
    w.setState(StateClaiming)
    w.mu.Unlock()
    // ...
}
```

### Phase 4: Non-Retryable Error Handling

**Files to modify:**
- `internal/llm/client.go` (add error types)
- `internal/worker/worker.go`
- `internal/queue/job.go`

**Changes:**

1. Define non-retryable error interface in `internal/llm/errors.go`:
```go
// NonRetryableError indicates an error that should not trigger job retries
type NonRetryableError interface {
    error
    NonRetryable() bool
}

// Ensure BudgetExceededError implements NonRetryableError
func (e *BudgetExceededError) NonRetryable() bool { return true }
```

2. Update worker retry logic:
```go
// In worker.go tryProcessJob()
if processErr != nil {
    // Check if error is non-retryable
    var nonRetryable llm.NonRetryableError
    if errors.As(processErr, &nonRetryable) && nonRetryable.NonRetryable() {
        // Don't retry - mark as dead immediately
        if err := w.queue.Fail(ctx, job.ID, processErr); err != nil {
            w.logger.Error("Failed to mark job as failed", "job", job.ID, "error", err)
        }
        // Don't call Retry
        return true, processErr
    }

    // Existing retry logic for retryable errors
    if job.CanRetry() {
        if err := w.queue.Retry(ctx, job.ID); err != nil {
            // ...
        }
    }
}
```

### Phase 5: Retry Backoff/Cooldown

**Files to modify:**
- `internal/queue/job.go`
- `internal/queue/store.go`

**Changes:**

1. Add `NextRetryAt` field to Job:
```go
type Job struct {
    // ...existing fields
    NextRetryAt  *time.Time `json:"next_retry_at,omitempty"`
}
```

2. Implement exponential backoff in `Store.Retry()`:
```go
func (s *Store) Retry(jobID string) error {
    job, err := s.GetByID(jobID)
    if err != nil {
        return err
    }

    // Calculate backoff: 2^retry * base (e.g., 2s, 4s, 8s)
    backoffSeconds := int(math.Pow(2, float64(job.RetryCount))) * 2
    nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

    return s.db.Exec(`
        UPDATE jobs
        SET state = ?, retry_count = retry_count + 1,
            next_retry_at = ?, updated_at = ?
        WHERE id = ?`,
        StatePending, nextRetry, time.Now().UTC(), jobID,
    ).Error
}
```

3. Update `ClaimNext()` to respect retry time:
```go
func (s *Store) ClaimNext(workerID string, caps []string) (*Job, error) {
    // Add WHERE clause: next_retry_at IS NULL OR next_retry_at <= NOW()
    query := `
        SELECT * FROM jobs
        WHERE state = ?
          AND (next_retry_at IS NULL OR next_retry_at <= ?)
        ORDER BY priority DESC, created_at ASC
        LIMIT 1
    `
    // ...
}
```

### Phase 6: Conversation-Aware Reporting

**Files to modify:**
- `internal/agent/loop.go`
- `internal/tools/builtin/platform_tools.go` (new tool)

**Changes:**

1. Add `session_history` tool for agents to query recent conversation/task history:
```go
// In platform_tools.go
func (pt *PlatformTools) sessionHistory(ctx context.Context, args map[string]any) (any, error) {
    sessionID, _ := args["session_id"].(string)
    limit := 10
    if l, ok := args["limit"].(float64); ok {
        limit = int(l)
    }

    // Get recent tasks for this session
    tasks, _ := pt.taskStore.ListBySession(sessionID, limit)

    // Get step results for completed tasks
    var history []map[string]any
    for _, t := range tasks {
        steps, _ := pt.stepStore.ListByTaskID(t.ID)
        history = append(history, map[string]any{
            "task_id":     t.ID,
            "name":        t.Name,
            "state":       t.State,
            "completed":   t.CompletedJobs,
            "total":       t.TotalJobs,
            "steps":       summarizeSteps(steps),
            "created_at":  t.CreatedAt,
            "completed_at": t.CompletedAt,
        })
    }

    return history, nil
}
```

2. Register tool in agent toolkit:
```go
tools = append(tools, &llm.Tool{
    Name:        "session_history",
    Description: "Get recent task and conversation history for this session. Use when asked about previous work, to summarize what was done, or to provide status reports.",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "session_id": map[string]any{"type": "string", "description": "Session ID (current session if omitted)"},
            "limit":      map[string]any{"type": "integer", "description": "Max results (default 10)"},
        },
    },
})
```

3. Add system prompt guidance for report-style queries:
```go
// In agent system prompts (e.g., chat agent)
const reportGuidance = `
When asked for a report or summary of work done:
1. Use the session_history tool to get recent tasks and their results
2. Summarize completed tasks with their outcomes
3. Note any failures and their causes
4. Be specific about what was accomplished
`
```

---

## File Changes Summary

| File | Changes |
|------|---------|
| `internal/agent/dispatcher.go` | Add report/recall intent patterns, update shouldCreateTask and ShouldDispatchAsync |
| `internal/agent/strategic.go` | Add shouldDecompose() fast-path for simple requests |
| `internal/worker/state.go` | Fix ValidTransitions or update CanClaim semantics |
| `internal/worker/worker.go` | Force Idle transition before claiming, add non-retryable error check |
| `internal/llm/errors.go` (new) | Define NonRetryableError interface |
| `internal/llm/client.go` | Make BudgetExceededError implement NonRetryableError |
| `internal/queue/job.go` | Add NextRetryAt field |
| `internal/queue/store.go` | Add exponential backoff to Retry(), respect NextRetryAt in ClaimNext() |
| `internal/tools/builtin/platform_tools.go` | Add session_history tool |
| `internal/agent/agents/*.yaml` | Add report guidance to agent system prompts |

---

## Verification Plan

1. **Intent Classification Test**:
   - Send "give me a report on what you did"
   - Verify intent is classified as `report` with `chat` agent
   - Verify no task is created
   - Verify single-agent response (no decomposition)

2. **Worker State Machine Test**:
   - Monitor logs for "Invalid state transition" warnings
   - Verify clean transitions: complete→idle→claiming

3. **Retry Behavior Test**:
   - Trigger a token budget error
   - Verify job is NOT retried (goes to `dead` state)
   - Verify only one worker attempts the job

4. **Backoff Test**:
   - Create a job that fails with a retryable error
   - Verify retry delays increase: 2s, 4s, 8s

5. **Reporting Test**:
   - Execute a multi-step task
   - Request "give me a report on what you did"
   - Verify agent uses session_history tool
   - Verify response includes specific task/step details

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Changing state machine could affect running jobs | Deploy during low-activity period, monitor worker logs |
| New intent patterns may misclassify existing queries | Add comprehensive test cases, log classification confidence |
| Retry backoff could delay legitimate retries | Start with conservative backoff (2s base), tune as needed |
| session_history tool adds DB queries | Add caching, limit default results |
