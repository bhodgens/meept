# Agent Re-Routing: Dynamic Mid-Task Handoff Implementation Plan

> **STATUS: FULLY IMPLEMENTED** — All 6 tasks complete. Code in `internal/tools/builtin/handoff.go`, `internal/agent/tactical.go`, `internal/agent/orchestrator.go`, `internal/daemon/components.go`. This plan is kept as historical reference.

**Goal:** Allow agents executing within the orchestrator pipeline to dynamically inject new steps and re-route to other agents mid-task, without going through the dispatcher or waiting for the full DAG to complete.

**Architecture:** Add a new `request_handoff` agent tool that publishes a handoff request on the bus. The TacticalScheduler subscribes to `orchestrator.handoff`, inserts a new step (or modifies an existing one) into the running task's DAG, and schedules it. Supports both a direct creation path and an amendment-based path (configurable via `HandoffUseAmendment`), keeping the handoff auditable and subject to existing validation/review gates. Rate limiting via `MaxHandoffSteps` (default 5) prevents runaway handoff chains. The calling agent's step continues to completion normally; the receiving agent gets full accumulated context.

**Tech Stack:** Go 1.22+, existing bus pub/sub, existing amendment system, existing TacticalScheduler

---

## The Gap

Currently, when an agent (say `coder`) discovers mid-execution that it needs `debugger` expertise:

1. **`delegate_task` tool** — synchronous, blocking. The coder waits while the debugger runs. No DAG integration, no validation, no review. Results are just text, not structured steps.
2. **Finish step and hope** — the planner's pre-defined DAG must have anticipated the need. If not, the task stalls.
3. **`workspace_yield`** — pair programming only, requires a pre-existing pair session.

There is **no mechanism** for an agent to say "I've done my part, but the next step needs to be handled by debugger instead of what the planner originally intended." The `SuggestedNextAgent` field on `AgentReport` exists but only works in the synchronous `RouteToAgent` path, not in the async orchestrator pipeline where most real work happens.

## Design

### New Tool: `request_handoff`

An agent calls this tool to request that the orchestrator insert a new step and route it to a specific agent. The tool:

1. Validates the target agent exists
2. Publishes an `orchestrator.handoff` bus event with the handoff details
3. Returns success to the calling agent (its step continues to completion normally)

### TacticalScheduler Extension

The TacticalScheduler already subscribes to `orchestrator.*` topics via the Orchestrator. We add:

1. A new handler: `handleHandoffRequest` — processes the handoff event
2. The handler creates a new step via `AmendmentAddStep` (reuses existing amendment system)
3. The new step's `AccumulatedContext` includes the calling agent's partial results
4. Steps that depend on the original step now transitively depend on the injected step

### Bus Topic

```
orchestrator.handoff — published by request_handoff tool
```

The tool constructs a proper `models.BusMessage` via `models.NewBusMessage(models.MessageTypeEvent, "request_handoff", payload)`. The `Payload` field is `json.RawMessage` containing the `HandoffPayload` struct serialized as JSON:

```json
{
  "task_id": "task-123",
  "from_step_id": "step-456",
  "from_agent_id": "coder",
  "to_agent_id": "debugger",
  "description": "Debug the nil pointer dereference in auth.go:47",
  "tool_hint": "debug",
  "reason": "runtime error discovered during implementation",
  "partial_result": "... context from calling agent ...",
  "inject_after": true,
  "timestamp": "2026-06-05T12:34:56Z"
}
```

### Handoff vs Delegate

| | `delegate_task` | `request_handoff` |
|---|---|---|
| Execution | Synchronous (caller waits) | Async (caller continues) |
| DAG integration | None | Creates real step with dependencies |
| Validation/review | Bypassed | Subject to existing gates |
| Audit trail | None | Step + amendment record |
| Context | Ad-hoc string | Full AccumulatedContext propagation |
| Retry/escalation | None | Inherited from TacticalScheduler |

---

## File Structure

| File | Action | Purpose | Status |
|------|--------|---------|--------|
| `internal/tools/builtin/handoff.go` | Create | `request_handoff` tool implementation | Done |
| `internal/tools/builtin/handoff_test.go` | Create | Tests for `request_handoff` tool | Done |
| `internal/agent/tactical.go` | Modify | Add `HandleHandoff`, `rewireDownstreamDeps`, `agentIDToToolHint` | Done |
| `internal/agent/orchestrator.go` | Modify | Subscribe to `orchestrator.handoff` topic | Done |
| `internal/agent/handoff_test.go` | Create | Unit + integration tests for handoff flow | Done |
| `internal/daemon/components.go` | Modify | Wire tool into daemon tool registry | Done |

**Note:** Integration tests were merged into `handoff_test.go` rather than a separate `handoff_integration_test.go`.

> **Implementation divergence from code snippets below:** The actual code uses `slogDiscardLogger()` (not `testLogger()`), `newTestTaskAndStepStore(t)` (not separate `setupTestTaskStore`/`setupTestStepStore` helpers), and `taskStore.Create(&task.Task{...})` (not `taskStore.Create("name", "desc")`). The code snippets below represent the original plan's intended approach; the implementation adapted to existing test infrastructure.

---

### Task 1: Create the `request_handoff` tool

**Files:**
- Create: `internal/tools/builtin/handoff.go`
- Test: `internal/tools/builtin/handoff_test.go`

- [x] **Step 1: Write the failing test for `request_handoff` tool**

```go
// internal/tools/builtin/handoff_test.go
package builtin

import (
	"context"
	"testing"
)

func TestRequestHandoffTool_Execute_Success(t *testing.T) {
	t.Parallel()

	var capturedTopic string
	var capturedPayload any

	bus := &mockHandoffBus{
		publishFn: func(topic string, msg any) {
			capturedTopic = topic
			capturedPayload = msg
		},
	}

	tool := NewRequestHandoffTool(bus, mockHandoffGetAgentExists("debugger"))

	result, err := tool.Execute(context.Background(), map[string]any{
		"task_id":       "task-123",
		"from_step_id":  "step-456",
		"to_agent_id":   "debugger",
		"description":   "Debug the nil pointer dereference in auth.go:47",
		"reason":        "runtime error discovered",
		"partial_result": "partially implemented auth module",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	if !hr.Success {
		t.Errorf("expected success, got error: %s", hr.Error)
	}
	if capturedTopic != "orchestrator.handoff" {
		t.Errorf("expected topic orchestrator.handoff, got %s", capturedTopic)
	}
	if capturedPayload == nil {
		t.Error("expected payload to be published")
	}
}

func TestRequestHandoffTool_Execute_InvalidAgent(t *testing.T) {
	t.Parallel()

	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, mockHandoffGetAgentExists("coder")) // only "coder" exists

	result, err := tool.Execute(context.Background(), map[string]any{
		"task_id":      "task-123",
		"from_step_id": "step-456",
		"to_agent_id":  "nonexistent",
		"description":  "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	if hr.Success {
		t.Error("expected failure for nonexistent agent")
	}
}

func TestRequestHandoffTool_Execute_MissingFields(t *testing.T) {
	t.Parallel()

	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, mockHandoffGetAgentExists("debugger"))

	result, _ := tool.Execute(context.Background(), map[string]any{
		"task_id": "task-123",
		// missing from_step_id, to_agent_id, description
	})

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	if hr.Success {
		t.Error("expected failure for missing required fields")
	}
}

// mockHandoffBus is a mock bus for testing.
type mockHandoffBus struct {
	publishFn func(topic string, msg *models.BusMessage)
}

func (m *mockHandoffBus) Publish(topic string, msg *models.BusMessage) int {
	if m.publishFn != nil {
		m.publishFn(topic, msg)
	}
	return 1
}

// mockHandoffGetAgentExists returns a function that reports only listed agents as existing.
func mockHandoffGetAgentExists(agents ...string) func(agentID string) bool {
	set := make(map[string]bool, len(agents))
	for _, a := range agents {
		set[a] = true
	}
	return func(agentID string) bool { return set[agentID] }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/builtin/... -run TestRequestHandoff -v`
Expected: FAIL — `NewRequestHandoffTool` undefined

- [x] **Step 3: Implement the `request_handoff` tool**

```go
// internal/tools/builtin/handoff.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// HandoffPayload is the bus event payload published by request_handoff.
type HandoffPayload struct {
	TaskID        string `json:"task_id"`
	FromStepID    string `json:"from_step_id"`
	FromAgentID   string `json:"from_agent_id"`
	ToAgentID     string `json:"to_agent_id"`
	Description   string `json:"description"`
	ToolHint      string `json:"tool_hint,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	InjectAfter   bool   `json:"inject_after"`
	Timestamp     string `json:"timestamp"`
}

// HandoffResult is returned to the calling agent.
type HandoffResult struct {
	Success     bool   `json:"success"`
	TaskID      string `json:"task_id"`
	ToAgentID   string `json:"to_agent_id"`
	Description string `json:"description"`
	Message     string `json:"message"`
	Error       string `json:"error,omitempty"`
}

// handoffBus is the bus interface needed by RequestHandoffTool.
type handoffBus interface {
	Publish(topic string, msg *models.BusMessage) int
}

// RequestHandoffTool allows an agent to request a handoff to another agent
// mid-task. It publishes an orchestrator.handoff event and returns success
// immediately — the calling agent's step continues to completion normally.
type RequestHandoffTool struct {
	bus        handoffBus
	agentExist func(agentID string) bool
}

// NewRequestHandoffTool creates a new handoff tool.
// agentExist checks if an agent ID is registered (nil = always allow).
func NewRequestHandoffTool(bus handoffBus, agentExist func(agentID string) bool) *RequestHandoffTool {
	return &RequestHandoffTool{
		bus:        bus,
		agentExist: agentExist,
	}
}

func (t *RequestHandoffTool) Name() string { return "request_handoff" }

func (t *RequestHandoffTool) Category() string { return "platform" }

func (t *RequestHandoffTool) Description() string {
	return "Request a handoff to another agent mid-task. " +
		"Creates a new step in the task DAG routed to the target agent. " +
		"Use this when you discover the next work requires expertise you don't have. " +
		"Your current step continues to completion; the new step is queued after it."
}

func (t *RequestHandoffTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"task_id": {
				Type:        schemaTypeString,
				Description: "The current task ID (from your step context).",
			},
			"from_step_id": {
				Type:        schemaTypeString,
				Description: "Your current step ID (the step you're executing right now).",
			},
			"to_agent_id": {
				Type:        schemaTypeString,
				Description: "The agent to hand off to (e.g., 'debugger', 'analyst', 'coder'). Use platform_agents to discover available agents.",
			},
			"description": {
				Type:        schemaTypeString,
				Description: "Description of what the target agent should do.",
			},
			"tool_hint": {
				Type:        schemaTypeString,
				Description: "Hint for step routing: 'code', 'debug', 'analyze', 'git', 'plan'. Defaults to matching the target agent's primary skill.",
			},
			"reason": {
				Type:        schemaTypeString,
				Description: "Why you're requesting the handoff (for audit trail).",
			},
			"partial_result": {
				Type:        schemaTypeString,
				Description: "Summary of what you've accomplished so far, to give the next agent context.",
			},
		},
		Required: []string{"task_id", "from_step_id", "to_agent_id", "description"},
	}
}

func (t *RequestHandoffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	taskID, _ := args["task_id"].(string)
	fromStepID, _ := args["from_step_id"].(string)
	toAgentID, _ := args["to_agent_id"].(string)
	description, _ := args["description"].(string)

	// Validate required fields
	if taskID == "" || fromStepID == "" || toAgentID == "" || description == "" {
		return HandoffResult{
			Success: false,
			Error:   "task_id, from_step_id, to_agent_id, and description are required",
		}, nil
	}

	// Validate target agent exists
	if t.agentExist != nil && !t.agentExist(toAgentID) {
		return HandoffResult{
			Success:     false,
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Error:       fmt.Sprintf("agent %q not found; use platform_agents to list available agents", toAgentID),
		}, nil
	}

	toolHint, _ := args["tool_hint"].(string)
	reason, _ := args["reason"].(string)
	partialResult, _ := args["partial_result"].(string)

	// Build and publish handoff event
	payload := HandoffPayload{
		TaskID:        taskID,
		FromStepID:    fromStepID,
		ToAgentID:     toAgentID,
		Description:   description,
		ToolHint:      toolHint,
		Reason:        reason,
		PartialResult: partialResult,
		InjectAfter:   true,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return HandoffResult{
			Success: false,
			Error:   fmt.Sprintf("failed to marshal handoff payload: %v", err),
		}, nil
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "request_handoff", payload)
	if err != nil {
		return HandoffResult{
			Success: false,
			Error:   fmt.Sprintf("failed to create bus message: %v", err),
		}, nil
	}
	msg.Topic = "orchestrator.handoff"

	if t.bus != nil {
		t.bus.Publish("orchestrator.handoff", msg)
	}

	return HandoffResult{
		Success:     true,
		TaskID:      taskID,
		ToAgentID:   toAgentID,
		Description: description,
		Message: fmt.Sprintf(
			"Handoff requested to %s: %s. Your step will complete normally; the new step is queued.",
			toAgentID, truncateHandoffDesc(description, 80),
		),
	}, nil
}

// Ensure RequestHandoffTool implements the Tool interface.
var _ tools.Tool = (*RequestHandoffTool)(nil)

func truncateHandoffDesc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/builtin/... -run TestRequestHandoff -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/tools/builtin/handoff.go internal/tools/builtin/handoff_test.go
git commit -m "feat(tools): add request_handoff tool for dynamic agent re-routing"
```

---

### Task 2: Add handoff handler to TacticalScheduler

**Files:**
- Modify: `internal/agent/tactical.go` (add `HandleHandoff` method)
- Create: `internal/agent/handoff_test.go`

- [x] **Step 1: Write the failing test for `HandleHandoff`**

```go
// internal/agent/handoff_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestTacticalScheduler_HandleHandoff_CreatesNewStep(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()

	taskStore := setupTestTaskStore(t, tmpDir)
	stepStore := setupTestStepStore(t, tmpDir)

	// Create a parent task
	tsk, err := taskStore.Create("test-task", "implement auth")
	if err != nil {
		t.Fatal(err)
	}

	// Create the originating step (the one the calling agent is executing)
	fromStep := &task.TaskStep{
		ID:          "step-from",
		TaskID:      tsk.ID,
		Description: "implement auth module",
		ToolHint:    "code",
		AgentID:     "coder",
		State:       task.StepScheduled,
		Sequence:    0,
	}
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatal(err)
	}

	// Update task with job count
	tsk.TotalJobs = 1
	if err := taskStore.Update(tsk); err != nil {
		t.Fatal(err)
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})

	// Build handoff payload
	payload := map[string]any{
		"task_id":        tsk.ID,
		"from_step_id":   fromStep.ID,
		"from_agent_id":  "coder",
		"to_agent_id":    "debugger",
		"description":    "Debug the nil pointer dereference in auth.go:47",
		"tool_hint":      "debug",
		"reason":         "runtime error discovered",
		"partial_result": "auth module partially implemented",
		"inject_after":   true,
	}

	payloadJSON, _ := json.Marshal(payload)
	busMsg := &models.BusMessage{
		ID:      "msg-handoff-1",
		Type:    models.MessageTypeEvent,
		Topic:   "orchestrator.handoff",
		Source:  "request_handoff_tool",
		Payload: payloadJSON,
	}

	// Handle the handoff
	err = scheduler.HandleHandoff(context.Background(), busMsg)
	if err != nil {
		t.Fatalf("HandleHandoff returned error: %v", err)
	}

	// Verify: a new step was created
	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (original + handoff), got %d", len(steps))
	}

	// Find the new step (not the original)
	var newStep *task.TaskStep
	for _, s := range steps {
		if s.ID != fromStep.ID {
			newStep = s
			break
		}
	}
	if newStep == nil {
		t.Fatal("new handoff step not found")
	}

	// Verify the new step properties
	if newStep.ToolHint != "debug" {
		t.Errorf("expected tool_hint 'debug', got %q", newStep.ToolHint)
	}
	if newStep.Description != "Debug the nil pointer dereference in auth.go:47" {
		t.Errorf("unexpected description: %q", newStep.Description)
	}
	// The new step should depend on the originating step
	if len(newStep.DependsOn) != 1 || newStep.DependsOn[0] != fromStep.ID {
		t.Errorf("expected depends_on [%s], got %v", fromStep.ID, newStep.DependsOn)
	}
	// AccumulatedContext should include the partial result
	if newStep.AccumulatedContext == "" {
		t.Error("expected AccumulatedContext to be set")
	}
}

func TestTacticalScheduler_HandleHandoff_InvalidPayload(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		Logger: logger,
	})

	// Send garbage payload
	busMsg := &models.BusMessage{
		ID:      "msg-bad",
		Type:    models.MessageTypeEvent,
		Topic:   "orchestrator.handoff",
		Payload: json.RawMessage(`{invalid json`),
	}

	err := scheduler.HandleHandoff(context.Background(), busMsg)
	if err == nil {
		t.Error("expected error for invalid JSON payload")
	}
}

func TestTacticalScheduler_HandleHandoff_MissingTask(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()

	taskStore := setupTestTaskStore(t, tmpDir)
	stepStore := setupTestStepStore(t, tmpDir)

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})

	payload := map[string]any{
		"task_id":      "nonexistent-task",
		"from_step_id": "step-1",
		"to_agent_id":  "debugger",
		"description":  "test",
	}

	payloadJSON, _ := json.Marshal(payload)
	busMsg := &models.BusMessage{
		ID:      "msg-missing",
		Type:    models.MessageTypeEvent,
		Topic:   "orchestrator.handoff",
		Payload: payloadJSON,
	}

	err := scheduler.HandleHandoff(context.Background(), busMsg)
	if err == nil {
		t.Error("expected error for missing task")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestTacticalScheduler_HandleHandoff -v`
Expected: FAIL — `HandleHandoff` undefined

- [x] **Step 3: Implement `HandleHandoff` on TacticalScheduler**

Add the following method to `internal/agent/tactical.go`:

```go
// HandoffRequest is the parsed payload from a request_handoff tool invocation.
type HandoffRequest struct {
	TaskID        string `json:"task_id"`
	FromStepID    string `json:"from_step_id"`
	FromAgentID   string `json:"from_agent_id"`
	ToAgentID     string `json:"to_agent_id"`
	Description   string `json:"description"`
	ToolHint      string `json:"tool_hint,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	InjectAfter   bool   `json:"inject_after"`
}

// HandleHandoff processes an orchestrator.handoff bus event.
// It creates a new step in the task DAG, sets dependencies so it runs
// after the originating step, and schedules it via the existing amendment
// flow.
func (ts *TacticalScheduler) HandleHandoff(ctx context.Context, msg *models.BusMessage) error {
	var req HandoffRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return fmt.Errorf("failed to parse handoff request: %w", err)
	}

	ts.logger.Info("Processing handoff request",
		"task_id", req.TaskID,
		"from_step", req.FromStepID,
		"from_agent", req.FromAgentID,
		"to_agent", req.ToAgentID,
		"description", truncateString(req.Description, 80),
	)

	// Validate task exists
	t, err := ts.taskStore.GetByID(req.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", req.TaskID, err)
	}
	if t == nil {
		return fmt.Errorf("task %s not found", req.TaskID)
	}

	// Validate the originating step exists
	fromStep, err := ts.stepStore.GetByID(req.FromStepID)
	if err != nil {
		ts.logger.Warn("Handoff originating step not found, proceeding without dependency",
			"step_id", req.FromStepID,
			"task_id", req.TaskID,
		)
	}

	// Derive tool_hint from target agent if not provided
	toolHint := req.ToolHint
	if toolHint == "" {
		toolHint = agentIDToToolHint(req.ToAgentID)
	}

	// Build accumulated context from the handoff request
	accumulatedContext := ""
	if req.PartialResult != "" {
		accumulatedContext = fmt.Sprintf("[Handoff from %s]: %s", req.FromAgentID, req.PartialResult)
	}
	if req.Reason != "" {
		if accumulatedContext != "" {
			accumulatedContext += "\n"
		}
		accumulatedContext += fmt.Sprintf("Reason: %s", req.Reason)
	}

	// Create the new step
	dependsOn := []string{}
	if fromStep != nil && req.InjectAfter {
		dependsOn = append(dependsOn, req.FromStepID)
	}

	newStep := &task.TaskStep{
		TaskID:             req.TaskID,
		Description:        req.Description,
		ToolHint:           toolHint,
		DependsOn:          dependsOn,
		AccumulatedContext:  accumulatedContext,
		State:              task.StepPending,
	}

	if err := ts.stepStore.Create(newStep); err != nil {
		return fmt.Errorf("failed to create handoff step: %w", err)
	}

	// Update task's total job count
	t.TotalJobs++
	if err := ts.taskStore.Update(t); err != nil {
		ts.logger.Error("Failed to update task job count after handoff", "error", err)
	}

	// Promote the new step to ready if its dependencies are met
	promoted, err := ts.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		ts.logger.Error("Failed to promote handoff step", "error", err)
	}

	ts.logger.Info("Handoff step created",
		"step_id", newStep.ID,
		"task_id", req.TaskID,
		"to_agent", req.ToAgentID,
		"tool_hint", toolHint,
		"depends_on", dependsOn,
		"promoted", len(promoted),
	)

	// Publish handoff event for audit
	ts.publishEvent("task.handoff_created", map[string]any{
		KeyTaskID:      req.TaskID,
		KeyStepID:      newStep.ID,
		"from_step":    req.FromStepID,
		"from_agent":   req.FromAgentID,
		KeyAgentID:     req.ToAgentID,
		"description":  req.Description,
		"reason":       req.Reason,
	})

	// Schedule ready steps (may schedule the new step if dependencies met)
	if len(promoted) > 0 {
		if err := ts.ScheduleReadySteps(ctx, req.TaskID); err != nil {
			ts.logger.Error("Failed to schedule handoff step", "error", err)
		}
	}

	return nil
}

// agentIDToToolHint maps an agent ID to a default tool hint.
func agentIDToToolHint(agentID string) string {
	switch agentID {
	case config.AgentIDCoder:
		return string(IntentCode)
	case config.AgentIDDebugger:
		return string(IntentDebug)
	case config.AgentIDAnalyst:
		return string(IntentAnalyze)
	case config.AgentIDCommitter:
		return string(IntentGit)
	case config.AgentIDScheduler:
		return string(IntentSchedule)
	case config.AgentIDPlanner:
		return string(IntentPlan)
	default:
		return "chat"
	}
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestTacticalScheduler_HandleHandoff -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent/tactical.go internal/agent/handoff_test.go
git commit -m "feat(agent): add HandleHandoff to TacticalScheduler for dynamic step injection"
```

---

### Task 3: Wire handoff into the Orchestrator

**Files:**
- Modify: `internal/agent/orchestrator.go` (subscribe to `orchestrator.handoff`)

- [x] **Step 1: Write the failing test**

```go
// Add to internal/agent/handoff_test.go

func TestOrchestrator_HandoffSubscription(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()

	taskStore := setupTestTaskStore(t, tmpDir)
	stepStore := setupTestStepStore(t, tmpDir)

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})

	orch := NewOrchestrator(OrchestratorDeps{
		Tactical: scheduler,
		Bus:      msgBus,
		Logger:   logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := orch.Start(ctx); err != nil {
		t.Fatalf("orchestrator start failed: %v", err)
	}

	// Create a task and step
	tsk, _ := taskStore.Create("handoff-test", "test task")
	fromStep := &task.TaskStep{
		ID: "step-origin", TaskID: tsk.ID,
		Description: "original step", ToolHint: "code",
		AgentID: "coder", State: task.StepScheduled, Sequence: 0,
	}
	stepStore.Create(fromStep)
	tsk.TotalJobs = 1
	taskStore.Update(tsk)

	// Publish handoff event
	payload, _ := json.Marshal(map[string]any{
		"task_id":        tsk.ID,
		"from_step_id":   fromStep.ID,
		"from_agent_id":  "coder",
		"to_agent_id":    "debugger",
		"description":    "debug runtime error",
		"tool_hint":      "debug",
		"partial_result": "partial work",
		"inject_after":   true,
	})
	msg := &models.BusMessage{
		ID: "msg-orch-handoff", Type: models.MessageTypeEvent,
		Topic: "orchestrator.handoff", Source: "test",
		Payload: payload,
	}
	msgBus.Publish("orchestrator.handoff", msg)

	// Wait for async processing
	time.Sleep(200 * time.Millisecond)

	// Verify new step was created
	steps, _ := stepStore.ListByTaskID(tsk.ID)
	if len(steps) != 2 {
		t.Errorf("expected 2 steps after handoff, got %d", len(steps))
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestOrchestrator_HandoffSubscription -v`
Expected: FAIL — orchestrator doesn't subscribe to `orchestrator.handoff`

- [x] **Step 3: Add `orchestrator.handoff` subscription to Orchestrator.Start()**

In `internal/agent/orchestrator.go`, modify the `Start()` method's topics map (around line 60) to add:

```go
"orchestrator.handoff": o.handleHandoff,
```

And add the handler method to `orchestrator.go`:

```go
// handleHandoff processes handoff requests from agents.
func (o *Orchestrator) handleHandoff(ctx context.Context, msg *models.BusMessage) {
	if err := o.tactical.HandleHandoff(ctx, msg); err != nil {
		o.logger.Error("Failed to handle handoff request",
			"error", err,
		)
	}
}
```

The topics map in `Start()` should now look like:

```go
topics := map[string]func(context.Context, *models.BusMessage){
	"orchestrator.plan":     o.handlePlanRequest,
	"orchestrator.schedule": o.handleScheduleRequest,
	"orchestrator.handoff":  o.handleHandoff,
	"queue.job.completed":   o.handleJobCompleted,
	"queue.job.failed":      o.handleJobFailed,
	"task.amend.applied":    o.handleAmendmentApplied,
	"task.amend.rejected":   o.handleAmendmentRejected,
	"pair.session_created":  o.handlePairSessionCreated,
	"pair.converged":        o.handlePairConverged,
	"pair.exhausted":        o.handlePairExhausted,
	"pair.round_failed":     o.handlePairRoundFailed,
	"collaboration.session_created":   o.handleCollabSessionCreated,
	"collaboration.consensus_reached": o.handleCollabConsensus,
	"collaboration.divergence":        o.handleCollabDivergence,
	"collaboration.result":            o.handleCollabResult,
	"collaboration.error":             o.handleCollabError,
	"collaboration.requested":         o.handleCollabRequested,
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestOrchestrator_HandoffSubscription -v`
Expected: PASS

- [x] **Step 5: Run all existing orchestrator tests**

Run: `go test ./internal/agent/... -run TestOrchestrator -v`
Expected: All existing tests still PASS (no regressions)

- [x] **Step 6: Commit**

```bash
git add internal/agent/orchestrator.go internal/agent/handoff_test.go
git commit -m "feat(agent): wire handoff handler into Orchestrator bus subscriptions"
```

---

### Task 4: Wire tool into daemon and tool registry

**Files:**
- Modify: `internal/daemon/daemon.go` (or wherever tools are registered — find the wiring location)
- Add `request_handoff` to agent tool lists

- [x] **Step 1: Find where platform tools are registered**

Search for where `NewDelegateTaskTool`, `NewPlatformAgentsTool` are instantiated in the daemon wiring code.

Run: `grep -rn "NewDelegateTaskTool\|NewPlatformAgentsTool" internal/daemon/`

This will identify the exact file and line where tools are wired up.

- [x] **Step 2: Write the failing test**

This is a wiring task. Verify the tool appears in the registry after daemon init.

```go
// Add to an appropriate test file where daemon wiring is tested
// Or create a simple integration test:

func TestDaemonWiring_RequestHandoffTool(t *testing.T) {
	// Verify that the request_handoff tool is registered
	// by checking the tool registry after daemon initialization
}
```

- [x] **Step 3: Wire the tool**

In the daemon wiring code (found in Step 1), add after the existing platform tool registrations:

```go
// request_handoff tool — needs bus and agent existence check
agentExistFn := func(agentID string) bool {
	_, ok := agentRegistry.GetSpec(agentID)
	return ok
}
handoffTool := builtin.NewRequestHandoffTool(msgBus, agentExistFn)
toolRegistry.Register(handoffTool)
```

Also wire the `request_handoff` tool into the tool lists for agents that should be able to hand off work. At minimum, these agents should have it:
- `coder` → can hand off to debugger, analyst
- `debugger` → can hand off to coder
- `analyst` → can hand off to planner, coder

- [x] **Step 4: Verify the tool is registered**

Run: `go build ./cmd/meept-daemon/`
Expected: Build succeeds with no errors

- [x] **Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat(daemon): wire request_handoff tool into daemon and agent tool lists"
```

---

### Task 5: Update dependent steps to include injected steps

**Files:**
- Modify: `internal/agent/tactical.go` (in `HandleHandoff`, rewire downstream step dependencies)

- [x] **Step 1: Write the failing test for dependency rewiring**

When a handoff injects a step between the originating step and downstream steps that depend on it, the downstream steps should be rewired to depend on the injected step instead.

```go
// Add to internal/agent/handoff_test.go

func TestTacticalScheduler_HandleHandoff_RewiresDownstreamDependencies(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()

	taskStore := setupTestTaskStore(t, tmpDir)
	stepStore := setupTestStepStore(t, tmpDir)

	tsk, _ := taskStore.Create("rewire-test", "test dependency rewiring")

	// Step A (coder) -> Step B (committer, depends on A)
	stepA := &task.TaskStep{
		ID: "step-a", TaskID: tsk.ID,
		Description: "implement feature", ToolHint: "code",
		AgentID: "coder", State: task.StepScheduled, Sequence: 0,
	}
	stepB := &task.TaskStep{
		ID: "step-b", TaskID: tsk.ID,
		Description: "commit changes", ToolHint: "git",
		AgentID: "committer", State: task.StepPending, Sequence: 1,
		DependsOn: []string{"step-a"},
	}
	stepStore.Create(stepA)
	stepStore.Create(stepB)
	tsk.TotalJobs = 2
	taskStore.Update(tsk)

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})

	// Handoff: coder -> debugger, inject between A and B
	payload, _ := json.Marshal(map[string]any{
		"task_id":        tsk.ID,
		"from_step_id":   "step-a",
		"from_agent_id":  "coder",
		"to_agent_id":    "debugger",
		"description":    "debug the failing test",
		"tool_hint":      "debug",
		"partial_result": "feature implemented but test fails",
		"inject_after":   true,
	})
	busMsg := &models.BusMessage{
		ID: "msg-rewire", Type: models.MessageTypeEvent,
		Topic: "orchestrator.handoff", Source: "test",
		Payload: payload,
	}

	err := scheduler.HandleHandoff(context.Background(), busMsg)
	if err != nil {
		t.Fatalf("HandleHandoff: %v", err)
	}

	// Verify: step-b should now depend on the injected step, not step-a
	updatedB, _ := stepStore.GetByID("step-b")
	if len(updatedB.DependsOn) != 1 {
		t.Fatalf("expected step-b to have 1 dependency, got %d", len(updatedB.DependsOn))
	}
	if updatedB.DependsOn[0] == "step-a" {
		t.Error("step-b still depends on step-a; should depend on the injected handoff step")
	}
	// The injected step should depend on step-a
	steps, _ := stepStore.ListByTaskID(tsk.ID)
	var injectedStep *task.TaskStep
	for _, s := range steps {
		if s.ID != "step-a" && s.ID != "step-b" {
			injectedStep = s
			break
		}
	}
	if injectedStep == nil {
		t.Fatal("injected step not found")
	}
	if len(injectedStep.DependsOn) != 1 || injectedStep.DependsOn[0] != "step-a" {
		t.Errorf("injected step should depend on step-a, got %v", injectedStep.DependsOn)
	}
	if updatedB.DependsOn[0] != injectedStep.ID {
		t.Errorf("step-b should depend on injected step %s, got %s", injectedStep.ID, updatedB.DependsOn[0])
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestTacticalScheduler_HandleHandoff_RewiresDownstreamDependencies -v`
Expected: FAIL — step-b still depends on step-a

- [x] **Step 3: Add dependency rewiring to `HandleHandoff`**

In `HandleHandoff`, after creating the new step, add logic to find steps that depended on the originating step and rewire them to depend on the injected step:

```go
// Rewire downstream dependencies: steps that depended on fromStep
// should now depend on the injected step instead.
if fromStep != nil && req.InjectAfter {
	downstreamSteps, dsErr := ts.stepStore.ListByTaskID(req.TaskID)
	if dsErr == nil {
		for _, ds := range downstreamSteps {
			if ds.ID == newStep.ID || ds.ID == req.FromStepID {
				continue
			}
			for i, dep := range ds.DependsOn {
				if dep == req.FromStepID {
					ds.DependsOn[i] = newStep.ID
					if err := ts.stepStore.Update(ds); err != nil {
						ts.logger.Error("Failed to rewire downstream step dependency",
							"step_id", ds.ID, "error", err)
					} else {
						ts.logger.Info("Rewired downstream dependency",
							"step_id", ds.ID,
							"old_dep", req.FromStepID,
							"new_dep", newStep.ID,
						)
					}
					break
				}
			}
		}
	}
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestTacticalScheduler_HandleHandoff_RewiresDownstreamDependencies -v`
Expected: PASS

- [x] **Step 5: Run all handoff tests**

Run: `go test ./internal/agent/... -run TestTacticalScheduler_HandleHandoff -v`
Expected: All PASS

- [x] **Step 6: Commit**

```bash
git add internal/agent/tactical.go internal/agent/handoff_test.go
git commit -m "feat(agent): rewire downstream step dependencies on handoff injection"
```

---

### Task 6: Integration test — full handoff flow

**Files:**
- Create: `internal/agent/handoff_integration_test.go`

- [x] **Step 1: Write the integration test**

This test exercises the full flow: tool → bus → orchestrator → tactical → new step scheduled.

```go
// internal/agent/handoff_integration_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestHandoffIntegration_FullFlow(t *testing.T) {
	t.Parallel()

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()

	taskStore := setupTestTaskStore(t, tmpDir)
	stepStore := setupTestStepStore(t, tmpDir)

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})

	orch := NewOrchestrator(OrchestratorDeps{
		Tactical: scheduler,
		Bus:      msgBus,
		Logger:   logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := orch.Start(ctx); err != nil {
		t.Fatalf("orchestrator start: %v", err)
	}

	// Setup: create task with 3 steps (A→B→C)
	tsk, _ := taskStore.Create("integration-test", "build and test feature")

	stepA := &task.TaskStep{
		ID: "s-a", TaskID: tsk.ID,
		Description: "implement feature", ToolHint: "code",
		AgentID: "coder", State: task.StepScheduled, Sequence: 0,
	}
	stepB := &task.TaskStep{
		ID: "s-b", TaskID: tsk.ID,
		Description: "write tests", ToolHint: "code",
		AgentID: "coder", State: task.StepPending, Sequence: 1,
		DependsOn: []string{"s-a"},
	}
	stepC := &task.TaskStep{
		ID: "s-c", TaskID: tsk.ID,
		Description: "commit changes", ToolHint: "git",
		AgentID: "committer", State: task.StepPending, Sequence: 2,
		DependsOn: []string{"s-b"},
	}
	stepStore.Create(stepA)
	stepStore.Create(stepB)
	stepStore.Create(stepC)
	tsk.TotalJobs = 3
	taskStore.Update(tsk)

	// Agent (coder on stepA) discovers a bug and requests handoff to debugger
	handoffPayload, _ := json.Marshal(map[string]any{
		"task_id":        tsk.ID,
		"from_step_id":   "s-a",
		"from_agent_id":  "coder",
		"to_agent_id":    "debugger",
		"description":    "Fix nil pointer in feature.go:42",
		"tool_hint":      "debug",
		"reason":         "runtime panic during implementation",
		"partial_result": "Feature mostly implemented but crashes on nil input",
		"inject_after":   true,
	})

	msgBus.Publish("orchestrator.handoff", &models.BusMessage{
		ID: "integration-handoff", Type: models.MessageTypeEvent,
		Topic: "orchestrator.handoff", Source: "test",
		Payload: handoffPayload,
	})

	// Wait for async processing
	time.Sleep(300 * time.Millisecond)

	// Verify: 4 steps now (A, injected, B, C)
	steps, _ := stepStore.ListByTaskID(tsk.ID)
	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}

	// Find injected step
	var injected *task.TaskStep
	for _, s := range steps {
		if s.ID != "s-a" && s.ID != "s-b" && s.ID != "s-c" {
			injected = s
		}
	}
	if injected == nil {
		t.Fatal("injected step not found")
	}

	// Verify chain: A → injected → B → C
	if !containsStep(injected.DependsOn, "s-a") {
		t.Error("injected step should depend on s-a")
	}
	// B should have been rewired from s-a to injected
	updatedB, _ := stepStore.GetByID("s-b")
	if !containsStep(updatedB.DependsOn, injected.ID) {
		t.Errorf("s-b should depend on injected step %s, got %v", injected.ID, updatedB.DependsOn)
	}
	// C should still depend on B
	updatedC, _ := stepStore.GetByID("s-c")
	if !containsStep(updatedC.DependsOn, "s-b") {
		t.Error("s-c should still depend on s-b")
	}

	// Verify tool_hint and description
	if injected.ToolHint != "debug" {
		t.Errorf("expected tool_hint 'debug', got %q", injected.ToolHint)
	}
	if injected.AccumulatedContext == "" {
		t.Error("injected step should have accumulated context from handoff")
	}

	t.Logf("Handoff chain: s-a → %s → s-b → s-c", injected.ID)
	t.Logf("Injected step context: %s", injected.AccumulatedContext)
}

func containsStep(deps []string, id string) bool {
	for _, d := range deps {
		if d == id {
			return true
		}
	}
	return false
}
```

- [x] **Step 2: Run the integration test**

Run: `go test ./internal/agent/... -run TestHandoffIntegration -v -timeout 30s`
Expected: PASS

- [x] **Step 3: Run all tests to check for regressions**

Run: `go test ./internal/agent/... -v -timeout 120s`
Expected: All PASS

- [x] **Step 4: Commit**

```bash
git add internal/agent/handoff_integration_test.go
git commit -m "test(agent): add integration test for full handoff flow with dependency rewiring"
```

---

## Self-Review

**1. Spec coverage:**
- New `request_handoff` tool: Task 1 — DONE (`internal/tools/builtin/handoff.go`)
- TacticalScheduler handoff handling: Task 2 — DONE (`internal/agent/tactical.go`)
- Orchestrator bus subscription: Task 3 — DONE (`internal/agent/orchestrator.go`)
- Daemon wiring: Task 4 — DONE (`internal/daemon/components.go`)
- Dependency rewiring: Task 5 — DONE (extracted to `rewireDownstreamDeps` method)
- Integration test: Task 6 — DONE (merged into `handoff_test.go`)

**2. Placeholder scan:**
- Task 4 Step 1 uses `grep` to find wiring location — this was a discovery step, not a placeholder. The wiring code is at `internal/daemon/components.go` lines 2148-2154.

**3. Type consistency:**
- `HandoffPayload` in tool (Task 1) matches `HandoffRequest` in tactical (Task 2) — field names are consistent.
- `HandoffResult` used in tool tests matches the struct in tool implementation.
- `selectAgent` uses same `config.AgentID*` constants as `agentIDToToolHint`.
- Step creation uses `task.TaskStep` struct with correct field names.
- Bus message format matches between publisher (`request_handoff` tool) and consumer (`HandleHandoff`).
- `bus.Publish` signature is `Publish(topic string, msg *models.BusMessage) int` (not `any`).
- `BusMessage.Payload` is `json.RawMessage` (not `any`).
- `agentRegistry.GetSpec()` returns `(_, bool)` (not `(_, error)`).
- Task store type is `task.Store` (not `task.TaskStore`).

**4. Implementation enhancements beyond plan:**
- **Rate limiting:** `MaxHandoffSteps` config (default 5 per task) prevents runaway handoff chains.
- **Amendment system integration:** `HandoffUseAmendment` config flag enables routing through `AmendmentSubmitter` for review/approval before step creation, with fallback to direct creation.
- **`TerminatingTool` interface:** `RequestHandoffTool` implements `TerminateHint() bool` (returns `true`), signaling the agent loop that this tool terminates the turn.
- **Extracted `rewireDownstreamDeps`:** Plan inlined the rewiring logic; implementation extracts it into a separate method with logging.
- **`isHandoffStep` helper:** Uses `[Handoff from` sentinel in `AccumulatedContext`. TODO in code: replace with dedicated `IsHandoff bool` field on `TaskStep`.
- **`request_handoff` NOT in `BaselineTools`:** The tool is registered globally but must be added to individual agent `additional_tools` lists in `config/agents/*.json5`. Currently available to agents that opt in.

**5. Known technical debt:**
- `isHandoffStep()` uses string matching on `AccumulatedContext` instead of a dedicated boolean field on `TaskStep`.
- Integration tests merged into `handoff_test.go` rather than a separate file (minor organizational difference).
