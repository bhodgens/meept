package builtin

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// mockHandoffBus captures published messages for inspection.
type mockHandoffBus struct {
	mu     sync.Mutex
	events []busEvent
}

type busEvent struct {
	topic string
	msg   *models.BusMessage
}

func (m *mockHandoffBus) Publish(topic string, msg *models.BusMessage) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, busEvent{topic: topic, msg: msg})
	return 1
}

func (m *mockHandoffBus) lastEvent() *busEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) == 0 {
		return nil
	}
	return &m.events[len(m.events)-1]
}

// --- Tool interface compliance ---

func TestRequestHandoffTool_ImplementsTool(t *testing.T) {
	var _ tools.Tool = (*RequestHandoffTool)(nil)
}

func TestRequestHandoffTool_Name(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })
	if tool.Name() != "request_handoff" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "request_handoff")
	}
}

func TestRequestHandoffTool_Category(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })
	if tool.Category() != "platform" {
		t.Errorf("Category() = %q, want %q", tool.Category(), "platform")
	}
}

func TestRequestHandoffTool_Description(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestRequestHandoffTool_Parameters(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })
	params := tool.Parameters()

	if params.Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q, want %q", params.Type, schemaTypeObject)
	}

	// Verify required fields
	required := map[string]bool{}
	for _, r := range params.Required {
		required[r] = true
	}
	for _, field := range []string{"task_id", "from_step_id", "to_agent_id", "description"} {
		if !required[field] {
			t.Errorf("required field %q missing from Parameters", field)
		}
	}

	// Verify optional fields are defined in properties
	for _, field := range []string{"tool_hint", "reason", "partial_result"} {
		if _, ok := params.Properties[field]; !ok {
			t.Errorf("optional field %q missing from Properties", field)
		}
	}
}

// --- Success case ---

func TestRequestHandoffTool_Execute_Success(t *testing.T) {
	bus := &mockHandoffBus{}
	agentExists := func(id string) bool {
		return id == "coder"
	}
	tool := NewRequestHandoffTool(bus, agentExists)

	args := map[string]any{
		"task_id":      "task-123",
		"from_step_id": "step-5",
		"to_agent_id":  "coder",
		"description":  "Need code review for auth module",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}

	if !hr.Success {
		t.Errorf("Success = false, want true")
	}
	if hr.TaskID != "task-123" {
		t.Errorf("TaskID = %q, want %q", hr.TaskID, "task-123")
	}
	if hr.ToAgentID != "coder" {
		t.Errorf("ToAgentID = %q, want %q", hr.ToAgentID, "coder")
	}
	if hr.Description != "Need code review for auth module" {
		t.Errorf("Description = %q, want %q", hr.Description, "Need code review for auth module")
	}
	if hr.Message == "" {
		t.Error("Message should not be empty on success")
	}
	if hr.Error != "" {
		t.Errorf("Error = %q, want empty", hr.Error)
	}

	// Verify bus event was published
	evt := bus.lastEvent()
	if evt == nil {
		t.Fatal("expected a bus event to be published, got nil")
	}
	if evt.topic != "orchestrator.handoff" {
		t.Errorf("published topic = %q, want %q", evt.topic, "orchestrator.handoff")
	}

	// Verify BusMessage fields
	busMsg := evt.msg
	if busMsg == nil {
		t.Fatal("expected bus msg to be *models.BusMessage, got nil")
	}
	if busMsg.ID == "" {
		t.Error("bus message missing 'id' field")
	}
	if busMsg.Type != models.MessageTypeEvent {
		t.Errorf("bus message type = %q, want %q", busMsg.Type, models.MessageTypeEvent)
	}
	if busMsg.Source != "request_handoff" {
		t.Errorf("bus message source = %q, want %q", busMsg.Source, "request_handoff")
	}
	if busMsg.Timestamp.IsZero() {
		t.Error("bus message timestamp should not be zero")
	}

	// Payload should contain HandoffPayload data (Payload is json.RawMessage)
	var payload HandoffPayload
	if err := json.Unmarshal(busMsg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.TaskID != "task-123" {
		t.Errorf("payload.TaskID = %q, want %q", payload.TaskID, "task-123")
	}
	if payload.FromStepID != "step-5" {
		t.Errorf("payload.FromStepID = %q, want %q", payload.FromStepID, "step-5")
	}
	if payload.ToAgentID != "coder" {
		t.Errorf("payload.ToAgentID = %q, want %q", payload.ToAgentID, "coder")
	}
	if payload.Description != "Need code review for auth module" {
		t.Errorf("payload.Description = %q, want %q", payload.Description, "Need code review for auth module")
	}
	if !payload.InjectAfter {
		t.Errorf("payload.InjectAfter = %v, want true", payload.InjectAfter)
	}
	if payload.Timestamp == "" {
		t.Error("payload.Timestamp should not be empty")
	}
	// Verify timestamp is parseable
	if _, err := time.Parse(time.RFC3339, payload.Timestamp); err != nil {
		t.Errorf("payload.Timestamp not valid RFC3339: %v", err)
	}
}

// --- Success with optional fields ---

func TestRequestHandoffTool_Execute_SuccessWithOptionalFields(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(id string) bool { return id == "debugger" })

	args := map[string]any{
		"task_id":        "task-456",
		"from_step_id":   "step-10",
		"to_agent_id":    "debugger",
		"description":    "Crash in import pipeline",
		"tool_hint":      "shell_execute",
		"reason":         "requires debugging expertise",
		"partial_result": "completed 3 of 5 imports",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	if !hr.Success {
		t.Error("Success = false, want true")
	}

	// Verify optional fields in payload
	evt := bus.lastEvent()
	var payload HandoffPayload
	if err := json.Unmarshal(evt.msg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.ToolHint != "shell_execute" {
		t.Errorf("payload.ToolHint = %q, want %q", payload.ToolHint, "shell_execute")
	}
	if payload.Reason != "requires debugging expertise" {
		t.Errorf("payload.Reason = %q, want %q", payload.Reason, "requires debugging expertise")
	}
	if payload.PartialResult != "completed 3 of 5 imports" {
		t.Errorf("payload.PartialResult = %q, want %q", payload.PartialResult, "completed 3 of 5 imports")
	}
}

// --- Invalid agent ---

func TestRequestHandoffTool_Execute_InvalidAgent(t *testing.T) {
	bus := &mockHandoffBus{}
	agentExists := func(id string) bool {
		return id == "coder" || id == "planner"
	}
	tool := NewRequestHandoffTool(bus, agentExists)

	args := map[string]any{
		"task_id":      "task-789",
		"from_step_id": "step-2",
		"to_agent_id":  "nonexistent",
		"description":  "Some task",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}

	if hr.Success {
		t.Error("Success = true, want false for invalid agent")
	}
	if hr.ToAgentID != "nonexistent" {
		t.Errorf("ToAgentID = %q, want %q", hr.ToAgentID, "nonexistent")
	}
	if hr.Error == "" {
		t.Error("Error should not be empty for invalid agent")
	}
	if hr.Description != "Some task" {
		t.Errorf("Description = %q, want %q", hr.Description, "Some task")
	}

	// Verify no bus event was published
	evt := bus.lastEvent()
	if evt != nil {
		t.Error("expected no bus event for invalid agent, but one was published")
	}
}

// --- Missing required fields ---

func TestRequestHandoffTool_Execute_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing task_id",
			args: map[string]any{
				"from_step_id": "step-1",
				"to_agent_id":  "coder",
				"description":  "some desc",
			},
		},
		{
			name: "missing from_step_id",
			args: map[string]any{
				"task_id":     "task-1",
				"to_agent_id": "coder",
				"description": "some desc",
			},
		},
		{
			name: "missing to_agent_id",
			args: map[string]any{
				"task_id":      "task-1",
				"from_step_id": "step-1",
				"description":  "some desc",
			},
		},
		{
			name: "missing description",
			args: map[string]any{
				"task_id":      "task-1",
				"from_step_id": "step-1",
				"to_agent_id":  "coder",
			},
		},
		{
			name: "all fields empty",
			args: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := &mockHandoffBus{}
			tool := NewRequestHandoffTool(bus, func(string) bool { return true })

			result, err := tool.Execute(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}

			hr, ok := result.(HandoffResult)
			if !ok {
				t.Fatalf("expected HandoffResult, got %T", result)
			}

			if hr.Success {
				t.Error("Success = true, want false for missing fields")
			}
			if hr.Error == "" {
				t.Error("Error should not be empty for missing fields")
			}

			// Verify no bus event was published
			if evt := bus.lastEvent(); evt != nil {
				t.Error("expected no bus event for missing fields, but one was published")
			}
		})
	}
}

// --- Agent exists callback returns false ---

func TestRequestHandoffTool_Execute_AgentNotExistCallbackReturnsFalse(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return false })

	args := map[string]any{
		"task_id":      "task-999",
		"from_step_id": "step-99",
		"to_agent_id":  "anyone",
		"description":  "test",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}

	if hr.Success {
		t.Error("Success = true, want false when agent doesn't exist")
	}
}

// --- Non-string argument types are handled gracefully ---

func TestRequestHandoffTool_Execute_NonStringArgs(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })

	// Pass numeric types as args; should be treated as empty strings
	args := map[string]any{
		"task_id":      12345,
		"from_step_id": true,
		"to_agent_id":  nil,
		"description":  "has desc",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	// At minimum, description is valid, but other fields fail type assertion to ""
	if hr.Success {
		t.Error("Success = true, want false for non-string required fields")
	}
}

// --- SetFromAgentID ---

func TestRequestHandoffTool_SetFromAgentID(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(id string) bool { return id == "coder" })

	tool.SetFromAgentID("analyst")

	args := map[string]any{
		"task_id":      "task-abc",
		"from_step_id": "step-1",
		"to_agent_id":  "coder",
		"description":  "handoff with from agent",
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	hr, ok := result.(HandoffResult)
	if !ok {
		t.Fatalf("expected HandoffResult, got %T", result)
	}
	if !hr.Success {
		t.Fatal("Success = false, want true")
	}

	// Verify from_agent_id in the published payload
	evt := bus.lastEvent()
	var payload HandoffPayload
	if err := json.Unmarshal(evt.msg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.FromAgentID != "analyst" {
		t.Errorf("payload.FromAgentID = %q, want %q", payload.FromAgentID, "analyst")
	}
}

// --- TerminatingTool ---

func TestRequestHandoffTool_ImplementsTerminatingTool(t *testing.T) {
	var _ tools.TerminatingTool = (*RequestHandoffTool)(nil)
}

func TestRequestHandoffTool_TerminateHint(t *testing.T) {
	bus := &mockHandoffBus{}
	tool := NewRequestHandoffTool(bus, func(string) bool { return true })
	if !tool.TerminateHint(nil) {
		t.Error("TerminateHint should return true")
	}
}
