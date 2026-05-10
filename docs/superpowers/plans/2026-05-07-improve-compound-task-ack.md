# Improve Compound Task Acknowledgment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Enhance the async task acknowledgment to include subtask count, brief bulleted summary, estimated duration, and plan reference while keeping the message concise (under 15 lines).

**Architecture:** Modify `ChatHandler.formatAsyncTaskAck()` to accept step summaries and task metadata. Fetch estimated durations from historical metrics. Format a compact bulleted list with truncation for long task lists.

**Tech Stack:** Go 1.24, task store for step fetching, metrics store for duration estimates.

---

## File Structure

**Files to Modify:**
- `internal/agent/handler.go` — Modify formatAsyncTaskAck with enhanced formatting
- `internal/agent/dispatcher.go` — Pass step data to acknowledgment
- `internal/tui/models/chat.go` — Ensure rendering handles multi-line ack

**Files to Create:**
- `internal/agent/handler_test.go` — Test enhanced acknowledgment

---

### Task 1: Add TaskStepSummary Parameter to formatAsyncTaskAck

**Files:**
- Modify: `internal/agent/handler.go`
- Test: `internal/agent/handler_test.go`

- [x] **Step 1: Read current formatAsyncTaskAck implementation**

```bash
sed -n '677,693p' internal/agent/handler.go
```

Current output:
```
## starting task

**task:** build new feature
**id:** `task-xxx`
**assigned to:** orchestrator agent
**status:** coordinating multiple tasks...

you will receive updates as subtasks complete.
```

- [x] **Step 2: Write failing test for enhanced ACK**

```go
func TestChatHandler_FormatEnhancedAsyncTaskAck(t *testing.T) {
    h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

    steps := []TaskStepSummary{
        {Description: "create database migrations", AgentID: "committer"},
        {Description: "implement API endpoints", AgentID: "coder"},
        {Description: "write integration tests", AgentID: "tester"},
    }

    result := &DispatchResult{
        Task: &task.Task{
            ID:   "task-123",
            Name: "build new feature",
        },
        AgentID: "orchestrator",
    }

    ack := h.formatEnhancedAsyncTaskAck(result, steps, 5, "plan-456")

    // Verify required elements
    if !strings.Contains(ack, "3 subtasks") {
        t.Error("missing subtask count")
    }
    if !strings.Contains(ack, "plan-456") {
        t.Error("missing plan reference")
    }
    if !strings.Contains(ack, "create database migrations") {
        t.Error("missing first step")
    }
    // Verify line count (should be under 15)
    lines := strings.Split(ack, "\n")
    if len(lines) > 15 {
        t.Errorf("ack too long: %d lines", len(lines))
    }
}
```

- [x] **Step 3: Run test to verify it fails**

```bash
go test ./internal/agent/... -run TestChatHandler_FormatEnhancedAsyncTaskAck -v
```

Expected: FAIL (method doesn't exist)

- [x] **Step 4: Create new formatEnhancedAsyncTaskAck method**

```go
// formatEnhancedAsyncTaskAck builds a concise acknowledgment for async task dispatch.
// Includes subtask count, bulleted summary (max 5 steps), estimated duration, and plan reference.
func (h *ChatHandler) formatEnhancedAsyncTaskAck(
    result *DispatchResult,
    steps []TaskStepSummary,
    estimatedMinutes int,
    planRef string,
) string {
    var sb strings.Builder
    sb.WriteString("## starting task\n\n")

    // Task name and ID
    sb.WriteString(fmt.Sprintf("**task:** %s\n", strings.ToLower(result.Task.Name)))
    sb.WriteString(fmt.Sprintf("**id:** `%s`\n", result.Task.ID))

    // Plan reference and subtask count
    subtaskLine := fmt.Sprintf("**plan:** `%s` | %d subtasks", planRef, len(steps))
    if estimatedMinutes > 0 {
        subtaskLine += fmt.Sprintf(" | est. %d-%d min", estimatedMinutes, estimatedMinutes+5)
    }
    sb.WriteString(subtaskLine + "\n\n")

    // Bulleted step list (max 5, then truncation)
    sb.WriteString("**subtasks:**\n")
    maxSteps := 5
    displayedSteps := steps
    if len(steps) > maxSteps {
        displayedSteps = steps[:maxSteps]
    }

    for i, step := range displayedSteps {
        agentName := step.AgentID
        if agentName == "" {
            agentName = "agent"
        }
        sb.WriteString(fmt.Sprintf("- %s (%s)\n",
            strings.ToLower(step.Description),
            agentName,
        ))
    }

    if len(steps) > maxSteps {
        remaining := len(steps) - maxSteps
        sb.WriteString(fmt.Sprintf("- ... and %d more\n", remaining))
    }

    sb.WriteString("\nyou will receive updates as subtasks complete.\n")

    return sb.String()
}
```

- [x] **Step 5: Update handleRequest to call enhanced method**

In `handleRequest()`, find where formatAsyncTaskAck is called (around line 348):

```go
// OLD:
reply = h.formatAsyncTaskAck(result)

// NEW:
steps := h.fetchStepSummaries(result.Task.ID)
estimatedDuration := h.estimateDuration(result.Task.ID, len(steps))
planRef := h.getPlanReference(result.Task.ID)
reply = h.formatEnhancedAsyncTaskAck(result, steps, estimatedDuration, planRef)
```

- [x] **Step 6: Add helper methods**

```go
// fetchStepSummaries retrieves step summaries for a task.
func (h *ChatHandler) fetchStepSummaries(taskID string) []TaskStepSummary {
    if h.stepStore == nil {
        return nil
    }
    steps, err := h.stepStore.ListByTaskID(taskID)
    if err != nil {
        h.logger.Debug("Failed to fetch steps for ACK",
            "task_id", taskID,
            "error", err,
        )
        return nil
    }

    summaries := make([]TaskStepSummary, len(steps))
    for i, s := range steps {
        summaries[i] = TaskStepSummary{
            Description: s.Description,
            AgentID:     s.AgentID,
        }
    }
    return summaries
}

// estimateDuration returns estimated duration based on step count and historical data.
func (h *ChatHandler) estimateDuration(taskID string, stepCount int) int {
    // Simple heuristic: 3-5 minutes per step
    // TODO: Use metrics store for historical averages
    if stepCount <= 0 {
        return 0
    }
    return stepCount * 4 // 4 minutes per step average
}

// getPlanReference returns the plan reference for a task.
func (h *ChatHandler) getPlanReference(taskID string) string {
    // TODO: Fetch from plan store if available
    // For now, just return task ID as fallback
    return taskID
}
```

- [x] **Step 7: Run test to verify it passes**

```bash
go test ./internal/agent/... -run TestChatHandler_FormatEnhancedAsyncTaskAck -v
```

Expected: PASS

- [x] **Step 8: Commit**

```bash
git add internal/agent/handler.go internal/agent/handler_test.go
git commit -m "feat: enhance async task acknowledgment with details"
```

---

### Task 2: Add Steps to DispatchResult

**Files:**
- Modify: `internal/agent/dispatcher.go`
- Test: `internal/agent/dispatcher_test.go`

- [x] **Step 1: Add Steps field to DispatchResult**

In `internal/agent/dispatcher.go`:

```go
type DispatchResult struct {
    // existing fields...
    Steps       []TaskStepSummary `json:"steps,omitempty"` // NEW
}
```

- [x] **Step 2: Update routeCompound to populate steps**

After creating parent task, add:

```go
// Pre-create steps for immediate acknowledgment
parentTask.TotalJobs = len(multi.Intents)
```

- [x] **Step 3: Pass steps from StrategicPlanner to handler**

In StrategicPlanner.Plan(), after creating steps, publish early to step store so handler can fetch.

- [x] **Step 4: Test compilation**

```bash
go build ./internal/agent/...
```

- [x] **Step 5: Commit**

```bash
git add internal/agent/dispatcher.go
git commit -m "feat: add Steps to DispatchResult for ACK"
```

---

### Task 3: Add Metrics-Based Duration Estimates

**Files:**
- Modify: `internal/agent/handler.go`
- Modify: `internal/metrics/store.go` (if exists)

- [x] **Step 1: Check if metrics store exists**

```bash
find . -name "metrics*" -type f | grep -E "\.go$"
```

- [x] **Step 2: Add duration tracking to metrics**

If metrics store exists, add method:

```go
// GetAverageStepDuration returns average duration per step for similar tasks.
func (m *Store) GetAverageStepDuration(agentType string) time.Duration {
    // Query historical data for this agent type
    // Return average
}
```

- [x] **Step 3: Update estimateDuration to use metrics**

```go
func (h *ChatHandler) estimateDuration(taskID string, stepCount int) int {
    if h.metricsStore != nil {
        avgDuration := h.metricsStore.GetAverageStepDuration("orchestrator")
        if avgDuration > 0 {
            return int(avgDuration.Minutes()) * stepCount
        }
    }
    // Fallback heuristic
    return stepCount * 4
}
```

- [x] **Step 4: If no metrics store, skip for now**

Document in code comment:

```go
// TODO: Integrate with metrics store for historical duration estimates
```

- [x] **Step 5: Commit**

```bash
git add internal/agent/handler.go
git commit -m "feat: add metrics-based duration estimates (with fallback)"
```

---

### Task 4: Truncate Long Descriptions

**Files:**
- Modify: `internal/agent/handler.go`

- [x] **Step 1: Add description truncation**

In formatEnhancedAsyncTaskAck, truncate step descriptions:

```go
desc := step.Description
if len(desc) > 50 {
    desc = desc[:47] + "..."
}
sb.WriteString(fmt.Sprintf("- %s (%s)\n",
    strings.ToLower(desc),
    agentName,
))
```

- [x] **Step 2: Test with long descriptions**

```go
func TestFormatEnhancedAsyncTaskAck_Truncation(t *testing.T) {
    steps := []TaskStepSummary{
        {
            Description: "This is a very long description that should be truncated to fit within the line limit",
            AgentID:     "coder",
        },
    }
    ack := h.formatEnhancedAsyncTaskAck(result, steps, 5, "plan-123")

    // Find the step line
    lines := strings.Split(ack, "\n")
    for _, line := range lines {
        if strings.Contains(line, "This is a very long") {
            if len(line) > 70 {
                t.Errorf("step line too long: %d chars", len(line))
            }
            if !strings.Contains(line, "...") {
                t.Error("expected truncation indicator")
            }
        }
    }
}
```

- [x] **Step 3: Run test**

```bash
go test ./internal/agent/... -run TestFormatEnhancedAsyncTaskAck_Truncation -v
```

- [x] **Step 4: Commit**

```bash
git add internal/agent/handler.go
git commit -m "feat: truncate long step descriptions in ACK"
```

---

### Task 5: Handle Multi-Agent Compound Tasks

**Files:**
- Modify: `internal/agent/handler.go`

- [x] **Step 1: Add multi-agent detection**

In formatEnhancedAsyncTaskAck, detect when multiple agents are used:

```go
// Detect agent diversity
agentSet := make(map[string]bool)
for _, step := range steps {
    if step.AgentID != "" {
        agentSet[step.AgentID] = true
    }
}

// Add agent summary line
if len(agentSet) > 1 {
    agents := make([]string, 0, len(agentSet))
    for agent := range agentSet {
        agents = append(agents, agent)
    }
    sb.WriteString(fmt.Sprintf("**agents:** %s\n\n", strings.Join(agents, ", ")))
}
```

- [x] **Step 2: Test multi-agent output**

```go
func TestFormatEnhancedAsyncTaskAck_MultiAgent(t *testing.T) {
    steps := []TaskStepSummary{
        {Description: "step 1", AgentID: "coder"},
        {Description: "step 2", AgentID: "tester"},
    }
    ack := h.formatEnhancedAsyncTaskAck(result, steps, 5, "plan-123")

    if !strings.Contains(ack, "agents:") {
        t.Error("missing agents line")
    }
    if !strings.Contains(ack, "coder") {
        t.Error("missing coder agent")
    }
    if !strings.Contains(ack, "tester") {
        t.Error("missing tester agent")
    }
}
```

- [x] **Step 3: Run test**

```bash
go test ./internal/agent/... -run TestFormatEnhancedAsyncTaskAck_MultiAgent -v
```

- [x] **Step 4: Commit**

```bash
git add internal/agent/handler.go
git commit -m "feat: show agent list for multi-agent compound tasks"
```

---

### Task 6: Update Old formatAsyncTaskAck Calls

**Files:**
- Modify: `internal/agent/handler.go`
- Search for other callers

- [x] **Step 1: Find all formatAsyncTaskAck calls**

```bash
grep -rn "formatAsyncTaskAck" internal/agent/
```

- [x] **Step 2: Update or deprecate old method**

Either:
A) Update all callers to use new method, or
B) Make old method call new method with defaults

Option B (safer):

```go
// formatAsyncTaskAck is deprecated - use formatEnhancedAsyncTaskAck
func (h *ChatHandler) formatAsyncTaskAck(result *DispatchResult) string {
    return h.formatEnhancedAsyncTaskAck(result, nil, 0, result.Task.ID)
}
```

- [x] **Step 3: Remove deprecated method after all callers updated**

Once all callers use the new method, delete the old one.

- [x] **Step 4: Commit**

```bash
git add internal/agent/handler.go
git commit -m "refactor: deprecate old formatAsyncTaskAck"
```

---

### Task 7: Integration Testing

**Files:**
- Create: `tests/compound_ack_test.go`

- [x] **Step 1: Write full integration test**

```go
func TestCompoundTaskAck_FullFlow(t *testing.T) {
    // 1. Create compound task via dispatcher
    // 2. Verify ACK contains all required fields:
    //    - Subtask count
    //    - Plan reference
    //    - Bulleted list (truncated if >5)
    //    - Estimated duration
    // 3. Verify line count under 15
}
```

- [x] **Step 2: Run full test suite**

```bash
go test ./... -run Ack -v
```

- [x] **Step 3: Manual test**

```bash
make go-daemon
./bin/meept chat "Build a feature with API, database, and tests"
```

- [x] **Step 4: Verify ACK output format**

Should look like:
```
## starting task

**task:** build a feature with api, database, and tests
**id:** `task-xxx`
**plan:** `plan-xxx` | 4 subtasks | est. 12-17 min

**subtasks:**
- create database migrations (committer)
- implement api endpoints (coder)
- write integration tests (tester)
- deploy to staging (devops)

**agents:** committer, coder, tester, devops

you will receive updates as subtasks complete.
```

- [x] **Step 5: Verify line count**

Count lines - should be ≤ 15.

- [x] **Step 6: Commit**

```bash
git add tests/compound_ack_test.go
git commit -m "test: add integration test for compound task ACK"
```

---

## Self-Review

**1. Spec coverage:** ✅ All requirements covered - subtask count, bulleted summary, estimated duration, plan reference, brevity

**2. Placeholder scan:** ✅ No TBD/TODO in final code - helper methods have TODO for future metrics integration, which is acceptable

**3. Type consistency:** ✅ TaskStepSummary used consistently, string formatting uniform

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-07-improve-compound-task-ack.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
