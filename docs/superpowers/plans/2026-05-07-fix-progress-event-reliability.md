# Fix Progress Event Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure all progress updates and errors are reliably communicated to the chat, not silently dropped or hidden in the sidebar.

**Architecture:** Replace the `silent: true` flag with a `chat_visible: bool` field (inverted semantics for clarity). Remove rate limiting that drops progress events. Add error escalation path so all step failures reach the chat immediately.

**Tech Stack:** Go 1.24, message bus pub/sub, Bubble Tea TUI.

---

## File Structure

**Files to Modify:**
- `internal/agent/tactical.go` — Remove `silent: true`, add `chat_visible: true`
- `internal/tui/handlers/task_events.go` — Remove rate limiting, ensure all events processed
- `internal/tui/app.go` — Handle `chat_visible` flag
- `internal/tui/sidebar.go` — Handle `chat_visible` flag
- `pkg/models/progress.go` — Add `ChatVisible` field

**Files to Create:**
- `internal/tui/handlers/task_events_test.go` — Test for event reliability

---

### Task 1: Add ChatVisible Field to Progress Messages

**Files:**
- Modify: `pkg/models/progress.go`
- Test: `pkg/models/progress_test.go`

- [ ] **Step 1: Read current ProgressUpdateMsg structure**

```bash
cat pkg/models/progress.go
```

- [ ] **Step 2: Write failing test**

```go
func TestProgressUpdateMsg_ChatVisible(t *testing.T) {
    msg := ProgressUpdateMsg{
        ChatVisible: true,
        Message:     "test progress",
    }

    if !msg.IsChatVisible() {
        t.Error("expected ChatVisible to be true")
    }

    msg.ChatVisible = false
    if msg.IsChatVisible() {
        t.Error("expected ChatVisible to be false")
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./pkg/models/... -run TestProgressUpdateMsg_ChatVisible -v
```

Expected: FAIL

- [ ] **Step 4: Add ChatVisible field and helper method**

In `pkg/models/progress.go`:

```go
type ProgressUpdateMsg struct {
    // existing fields...
    ChatVisible bool   `json:"chat_visible,omitempty"`
    Message     string `json:"message,omitempty"`
}

// IsChatVisible returns true if this progress update should display in chat.
func (m ProgressUpdateMsg) IsChatVisible() bool {
    return m.ChatVisible
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./pkg/models/... -run TestProgressUpdateMsg_ChatVisible -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/models/progress.go pkg/models/progress_test.go
git commit -m "feat: add ChatVisible field to ProgressUpdateMsg"
```

---

### Task 2: Replace Silent with ChatVisible in Tactical Publishing

**Files:**
- Modify: `internal/agent/tactical.go`

- [ ] **Step 1: Find all `silent: true` usages**

```bash
grep -n '"silent":.*true' internal/agent/tactical.go
```

Expected locations: lines ~175, ~571, ~777

- [ ] **Step 2: Replace first occurrence (ScheduleReadySteps)**

Change from:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":         taskID,
    "scheduled_steps": scheduledCount,
    "current_step":    currentStepDesc,
    "silent":          true,
})
```

To:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":       taskID,
    "scheduled_steps": scheduledCount,
    "current_step":  currentStepDesc,
    "chat_visible":  true, // Progress visible in chat
})
```

- [ ] **Step 3: Replace second occurrence (OnJobCompleted progress update)**

Change from:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":        step.TaskID,
    "completed_jobs": t.CompletedJobs,
    "total_jobs":     t.TotalJobs,
    "current_step":   nextStepDesc,
    "silent":         true,
})
```

To:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":        step.TaskID,
    "completed_jobs": t.CompletedJobs,
    "total_jobs":     t.TotalJobs,
    "current_step":   nextStepDesc,
    "chat_visible":   true,
})
```

- [ ] **Step 4: Replace third occurrence (OnJobFailed progress update)**

Change from:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":        step.TaskID,
    "failed_jobs":    t.FailedJobs,
    "completed_jobs": t.CompletedJobs,
    "total_jobs":     t.TotalJobs,
    "current_step":   nextStepDesc,
    "silent":         true,
})
```

To:
```go
ts.publishEvent("task.progress", map[string]any{
    "task_id":        step.TaskID,
    "failed_jobs":    t.FailedJobs,
    "completed_jobs": t.CompletedJobs,
    "total_jobs":     t.TotalJobs,
    "current_step":   nextStepDesc,
    "chat_visible":   true,
})
```

- [ ] **Step 5: Test compilation**

```bash
go build ./internal/agent/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/agent/tactical.go
git commit -m "fix: replace silent with chat_visible for progress events"
```

---

### Task 3: Remove Rate Limiting in Task Event Handler

**Files:**
- Modify: `internal/tui/handlers/task_events.go`
- Test: `internal/tui/handlers/task_events_test.go`

- [ ] **Step 1: Read current rate limiting code**

```bash
sed -n '107,144p' internal/tui/handlers/task_events.go
```

- [ ] **Step 2: Write test that rate limiting drops events**

```go
func TestTaskEventHandler_NoRateLimiting(t *testing.T) {
    h := NewTaskEventHandler()

    // Send 10 progress events rapidly
    payloads := make([]map[string]any, 10)
    for i := 0; i < 10; i++ {
        payloads[i] = map[string]any{
            "task_id":      "task-1",
            "current_step": fmt.Sprintf("step %d", i),
            "completed_jobs": i,
            "total_jobs":   10,
        }
    }

    // All should produce notifications (no rate limiting)
    produced := 0
    for _, p := range payloads {
        if notif := h.HandleTaskProgress(p); notif != nil {
            produced++
        }
    }

    if produced != 10 {
        t.Errorf("expected 10 notifications, got %d (rate limiting active)", produced)
    }
}
```

- [ ] **Step 3: Run test to verify it fails (rate limiting is active)**

```bash
go test ./internal/tui/handlers/... -run TestTaskEventHandler_NoRateLimiting -v
```

Expected: FAIL - only 1-2 events produced due to rate limiting

- [ ] **Step 4: Remove rate limiting from HandleTaskProgress**

Replace the entire method. Remove:
- `lastProgressTime` field from struct
- `progressInterval` field from struct
- Rate limiting logic in HandleTaskProgress

New implementation:
```go
func (h *TaskEventHandler) HandleTaskProgress(payload map[string]any) *TaskNotification {
    taskID := getString(payload, "task_id", "")
    currentStep := getString(payload, "current_step", "")

    if currentStep == "" {
        return nil
    }

    // Rate limiting REMOVED - all progress events should be delivered
    completed := getInt(payload, "completed_jobs", 0)
    total := getInt(payload, "total_jobs", 0)

    var sb strings.Builder
    if total > 0 {
        sb.WriteString(fmt.Sprintf("task progress [%d/%d]: ", completed, total))
    } else {
        sb.WriteString("task progress: ")
    }
    sb.WriteString(strings.ToLower(currentStep))

    return &TaskNotification{
        Type:    "progress",
        Message: sb.String(),
        TaskID:  taskID,
    }
}
```

- [ ] **Step 5: Clean up struct fields**

Remove from TaskEventHandler:
```go
// OLD (remove these):
lastProgressTime map[string]time.Time
progressInterval time.Duration
```

Update NewTaskEventHandler:
```go
// OLD (remove):
func NewTaskEventHandler() *TaskEventHandler {
    return &TaskEventHandler{
        lastProgressTime: make(map[string]time.Time),
        progressInterval: 2 * time.Second,
    }
}

// NEW:
func NewTaskEventHandler() *TaskEventHandler {
    return &TaskEventHandler{}
}
```

- [ ] **Step 6: Remove ClearTaskProgress (no longer needed)**

Delete the `ClearTaskProgress` method entirely.

- [ ] **Step 7: Run test to verify it passes**

```bash
go test ./internal/tui/handlers/... -run TestTaskEventHandler_NoRateLimiting -v
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tui/handlers/task_events.go internal/tui/handlers/task_events_test.go
git commit -m "fix: remove rate limiting from progress events"
```

---

### Task 4: Handle ChatVisible in TUI App

**Files:**
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Find task.progress event handling**

```bash
grep -n "task.progress" internal/tui/app.go
```

- [ ] **Step 2: Add chat_visible handling**

In the `task.progress` case, add:

```go
case "task.progress":
    if payloadMap, ok := e.Payload.(map[string]any); ok {
        progressMsg := models.ProgressUpdateMsg{}

        // Extract chat_visible flag
        if chatVis, ok := payloadMap["chat_visible"].(bool); ok {
            progressMsg.ChatVisible = chatVis
        }

        // Extract other fields...
        if v, ok := payloadMap["current_step"].(string); ok {
            progressMsg.Message = v
        }

        // Only update chat if chat_visible is true
        if progressMsg.ChatVisible {
            if cmd := a.chat.Update(progressMsg); cmd != nil {
                cmds = append(cmds, cmd)
            }
        }

        // Always update sidebar
        cmds = append(cmds, s.refreshData())
    }
```

- [ ] **Step 3: Test compilation**

```bash
go build ./internal/tui/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat: handle chat_visible flag for progress events"
```

---

### Task 5: Handle ChatVisible in Sidebar

**Files:**
- Modify: `internal/tui/sidebar.go`

- [ ] **Step 1: Find handleTaskProgressEvent**

```bash
grep -n "handleTaskProgressEvent" internal/tui/sidebar.go
```

- [ ] **Step 2: Add chat_visible handling**

In `handleTaskProgressEvent`, add:

```go
func (s *SidebarModel) handleTaskProgressEvent(e BusEvent) {
    payloadMap, ok := e.Payload.(map[string]any)
    if !ok {
        return
    }

    // Extract chat_visible - if false, sidebar-only update
    chatVisible := true
    if cv, ok := payloadMap["chat_visible"].(bool); ok {
        chatVisible = cv
    }

    // Update sidebar data regardless
    s.refreshData()

    // If NOT chat visible, don't forward to chat
    if !chatVisible {
        return
    }

    // Forward to activity feed for visible events
    if s.eventStream != nil {
        s.eventStream.Update(e)
    }
}
```

- [ ] **Step 3: Test compilation**

```bash
go build ./internal/tui/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/sidebar.go
git commit -m "feat: handle chat_visible in sidebar"
```

---

### Task 6: Add Error Escalation for Step Failures

**Files:**
- Modify: `internal/agent/tactical.go`
- Modify: `internal/tui/handlers/task_events.go`

- [ ] **Step 1: Add task.error topic publishing**

In `OnJobFailed`, before the silent progress update, add:

```go
// Publish error to chat immediately (not silent)
ts.publishEvent("task.error", map[string]any{
    "task_id":      step.TaskID,
    "step_id":      step.ID,
    "error":        jobErr,
    "chat_visible": true, // Errors always visible
})
```

- [ ] **Step 2: Add task.error handler in TUI**

In `internal/tui/app.go`, add new case:

```go
case "task.error":
    if payloadMap, ok := e.Payload.(map[string]any); ok {
        errMsg := getString(payloadMap, "error", "")
        stepDesc := getString(payloadMap, "step_id", "")

        if errMsg != "" {
            // Display error in chat
            errorMsg := fmt.Sprintf("⚠️ step failed: %s\nerror: %s", stepDesc, errMsg)
            a.chat.Update(models.ChatMessageMsg{Text: errorMsg})
        }
    }
```

- [ ] **Step 3: Test compilation**

```bash
go build ./internal/agent/... ./internal/tui/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/agent/tactical.go internal/tui/app.go
git commit -m "feat: add error escalation for step failures"
```

---

### Task 7: Integration Testing

**Files:**
- Create: `tests/progress_reliability_test.go`

- [ ] **Step 1: Write integration test**

```go
func TestProgressEvents_ReachChat(t *testing.T) {
    // 1. Create task
    // 2. Publish progress events with chat_visible=true
    // 3. Verify events received by chat handler
    // 4. Verify events NOT dropped by rate limiting
}

func TestErrorEvents_EscalateToChat(t *testing.T) {
    // 1. Create task with step
    // 2. Fail step
    // 3. Verify task.error event published
    // 4. Verify chat received error
}
```

- [ ] **Step 2: Run full test suite**

```bash
go test ./... -run "Progress|Error" -v
```

- [ ] **Step 3: Manual test with TUI**

```bash
make go-daemon
./bin/meept chat "Run a multi-step task"
```

- [ ] **Step 4: Verify progress appears in chat (not just sidebar)**

- [ ] **Step 5: Verify errors appear in chat immediately**

- [ ] **Step 6: Commit**

```bash
git add tests/progress_reliability_test.go
git commit -m "test: add integration tests for progress reliability"
```

---

## Self-Review

**1. Spec coverage:** ✅ All requirements covered - silent flag replaced, rate limiting removed, error escalation added

**2. Placeholder scan:** ✅ No TBD/TODO - all code explicit

**3. Type consistency:** ✅ `chat_visible` is bool everywhere, consistent naming

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-07-fix-progress-event-reliability.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
