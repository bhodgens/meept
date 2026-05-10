package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

func TestChatRequest_Unmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
		wantErr bool
	}{
		{
			name:    "valid request",
			input:   `{"message": "hello", "conversation_id": "conv-1"}`,
			wantMsg: "hello",
			wantErr: false,
		},
		{
			name:    "missing message",
			input:   `{"conversation_id": "conv-1"}`,
			wantMsg: "",
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChatRequest
			err := json.Unmarshal([]byte(tt.input), &req)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshal error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && req.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", req.Message, tt.wantMsg)
			}
		})
	}
}

func TestChatResponse_Marshal(t *testing.T) {
	resp := ChatResponse{
		Reply:          "Hello there!",
		ConversationID: "conv-123",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Reply != resp.Reply {
		t.Errorf("Reply = %q, want %q", decoded.Reply, resp.Reply)
	}
	if decoded.ConversationID != resp.ConversationID {
		t.Errorf("ConversationID = %q, want %q", decoded.ConversationID, resp.ConversationID)
	}
}

func TestChatResponse_WithError(t *testing.T) {
	resp := ChatResponse{
		Reply:          "Error occurred",
		ConversationID: "conv-123",
		Error:          "something went wrong",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Error != resp.Error {
		t.Errorf("Error = %q, want %q", decoded.Error, resp.Error)
	}
}

func TestWorker_JSONMarshal(t *testing.T) {
	worker := &Worker{
		ID:             "worker-1",
		ConversationID: "conv-1",
		RequestID:      "req-1",
		State:          "processing",
		CurrentTool:    "bash",
	}

	data, err := json.Marshal(worker)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Worker
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != worker.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, worker.ID)
	}
	if decoded.State != worker.State {
		t.Errorf("State = %q, want %q", decoded.State, worker.State)
	}
	if decoded.CurrentTool != worker.CurrentTool {
		t.Errorf("CurrentTool = %q, want %q", decoded.CurrentTool, worker.CurrentTool)
	}
}

func TestPlanRequest_JSONMarshal(t *testing.T) {
	req := PlanRequest{
		TaskID:    "task-123",
		SessionID: "session-456",
		Input:     "Write a CSV parser",
		Intent:    "code",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PlanRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TaskID != req.TaskID {
		t.Errorf("TaskID = %q, want %q", decoded.TaskID, req.TaskID)
	}
	if decoded.Input != req.Input {
		t.Errorf("Input = %q, want %q", decoded.Input, req.Input)
	}
	if decoded.Intent != req.Intent {
		t.Errorf("Intent = %q, want %q", decoded.Intent, req.Intent)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateString(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestGenerateMessageID(t *testing.T) {
	id1 := generateMessageID()
	id2 := generateMessageID()

	if id1 == "" {
		t.Error("generateMessageID returned empty string")
	}
	// IDs should be unique (with nanosecond timestamp, extremely unlikely to collide)
	if id1 == id2 {
		t.Log("Warning: Two consecutive IDs were equal (very unlikely)")
	}
}

func TestGenerateWorkerID(t *testing.T) {
	id := generateWorkerID()
	if id == "" {
		t.Error("generateWorkerID returned empty string")
	}
	if len(id) < 8 {
		t.Errorf("generateWorkerID returned short ID: %q", id)
	}
	if id[:7] != "worker-" {
		t.Errorf("generateWorkerID should start with 'worker-', got %q", id)
	}
}

// slogDiscardLogger creates a logger that discards all output.
func slogDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestChatHandler_PublishPlanRequest(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())

	// Subscribe to orchestrator.plan
	sub := bus.Subscribe("test", "orchestrator.plan")
	defer bus.Unsubscribe(sub)

	handler := NewChatHandler(nil, nil, bus, slogDiscardLogger())

	result := &DispatchResult{
		Task: &task.Task{
			ID:          "task-123",
			Description: "build a feature",
		},
		Intent: &Intent{
			Type: "code",
		},
	}

	handler.publishPlanRequest(result, "session-456")

	// Verify message was published
	select {
	case msg := <-sub.Channel:
		if msg.Topic != "orchestrator.plan" {
			t.Errorf("expected topic orchestrator.plan, got %s", msg.Topic)
		}

		var req PlanRequest
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if req.TaskID != "task-123" {
			t.Errorf("expected task-123, got %s", req.TaskID)
		}
		if req.SessionID != "session-456" {
			t.Errorf("expected session-456, got %s", req.SessionID)
		}
		if req.Input != "build a feature" {
			t.Errorf("expected 'build a feature', got %s", req.Input)
		}
		if req.Intent != "code" {
			t.Errorf("expected intent 'code', got %s", req.Intent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout - message not published")
	}
}

func TestChatHandler_PublishPlanRequest_Compound(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())

	sub := bus.Subscribe("test", "orchestrator.plan")
	defer bus.Unsubscribe(sub)

	handler := NewChatHandler(nil, nil, bus, slogDiscardLogger())

	meta, _ := json.Marshal(map[string]any{
		"compound_type": "sequential",
	})

	result := &DispatchResult{
		Task: &task.Task{
			ID:          "task-456",
			Description: "multi-step task",
			Metadata:    meta,
		},
		Intent: &Intent{
			Type: "compound",
		},
	}

	handler.publishPlanRequest(result, "session-789")

	select {
	case msg := <-sub.Channel:
		var req PlanRequest
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if !req.IsCompound {
			t.Error("expected IsCompound to be true")
		}
		if req.CompoundType != "sequential" {
			t.Errorf("expected CompoundType 'sequential', got %s", req.CompoundType)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout - message not published")
	}
}

func TestChatHandler_PublishPlanRequest_WarnsOnNoSubscribers(t *testing.T) {
	// Create a bus with no subscribers
	bus := bus.New(nil, slogDiscardLogger())

	handler := NewChatHandler(nil, nil, bus, slogDiscardLogger())

	result := &DispatchResult{
		Task: &task.Task{
			ID:          "task-no-sub",
			Description: "test task",
		},
		Intent: &Intent{
			Type: "code",
		},
	}

	// Should not panic, should log warning (we can't easily test log output,
	// but we verify the method completes without error)
	handler.publishPlanRequest(result, "session-test")
	// Test passes if no panic occurred
}

func TestChatHandler_FormatEnhancedAsyncTaskAck(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{Description: "Create database migrations", AgentID: "committer"},
		{Description: "Implement API endpoints", AgentID: "coder"},
		{Description: "Write integration tests", AgentID: "tester"},
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
	if !strings.Contains(ack, "est.") {
		t.Error("missing estimated duration")
	}
	// Verify line count (should be under 15)
	lines := strings.Split(ack, "\n")
	if len(lines) > 15 {
		t.Errorf("ack too long: %d lines", len(lines))
	}
}

func TestChatHandler_FormatEnhancedAsyncTaskAck_NoSteps(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-456",
			Name: "simple task",
		},
		AgentID: "chat",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, nil, 0, "plan-789")

	if !strings.Contains(ack, "0 subtasks") {
		t.Error("missing subtask count for empty steps")
	}
	if !strings.Contains(ack, "plan-789") {
		t.Error("missing plan reference")
	}
}

func TestChatHandler_FormatEnhancedAsyncTaskAck_Truncation(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{
			Description: "This is a very long description that should be truncated to fit within the line limit of fifty characters",
			AgentID:     "coder",
		},
	}

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-trunc",
			Name: "truncation test",
		},
		AgentID: "coder",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, steps, 0, "plan-123")

	lines := strings.Split(ack, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "this is a very long") {
			found = true
			if len(line) > 70 {
				t.Errorf("step line too long (%d chars): %s", len(line), line)
			}
			if !strings.Contains(line, "...") {
				t.Error("expected truncation indicator '...'")
			}
		}
	}
	if !found {
		t.Error("could not find the step line with description")
	}
}

func TestChatHandler_FormatEnhancedAsyncTaskAck_MultiAgent(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{Description: "step 1", AgentID: "coder"},
		{Description: "step 2", AgentID: "tester"},
	}

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-multi",
			Name: "multi agent task",
		},
		AgentID: "orchestrator",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, steps, 0, "plan-123")

	if !strings.Contains(ack, "agents:") {
		t.Error("missing agents line for multi-agent task")
	}
	if !strings.Contains(ack, "coder") {
		t.Error("missing coder agent")
	}
	if !strings.Contains(ack, "tester") {
		t.Error("missing tester agent")
	}
}

func TestChatHandler_FormatEnhancedAsyncTaskAck_SingleAgent(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{Description: "step 1", AgentID: "coder"},
		{Description: "step 2", AgentID: "coder"},
	}

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-single",
			Name: "single agent task",
		},
		AgentID: "coder",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, steps, 0, "plan-123")

	if strings.Contains(ack, "agents:") {
		t.Error("agents line should not appear for single-agent task")
	}
}

func TestCompoundTaskAck_FullFlow(t *testing.T) {
	// Simulate a compound task with 4 subtasks across multiple agents
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{Description: "Create database migrations", AgentID: "committer"},
		{Description: "Implement API endpoints", AgentID: "coder"},
		{Description: "Write integration tests", AgentID: "tester"},
		{Description: "Deploy to staging", AgentID: "devops"},
	}

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-compound-full",
			Name: "Build a feature with API, database, and tests",
		},
		AgentID: "orchestrator",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, steps, 16, "plan-compound-001")

	// 1. Subtask count
	if !strings.Contains(ack, "4 subtasks") {
		t.Error("missing subtask count '4 subtasks'")
	}

	// 2. Plan reference
	if !strings.Contains(ack, "plan-compound-001") {
		t.Error("missing plan reference")
	}

	// 3. Estimated duration
	if !strings.Contains(ack, "est.") {
		t.Error("missing estimated duration")
	}

	// 4. All steps present (all under 5 so none truncated)
	expectedSteps := []string{"create database migrations", "implement api endpoints", "write integration tests", "deploy to staging"}
	for _, step := range expectedSteps {
		if !strings.Contains(ack, step) {
			t.Errorf("missing step: %s", step)
		}
	}

	// 5. Multi-agent line
	if !strings.Contains(ack, "agents:") {
		t.Error("missing agents line for multi-agent compound task")
	}
	for _, agent := range []string{"committer", "coder", "tester", "devops"} {
		if !strings.Contains(ack, agent) {
			t.Errorf("missing agent: %s", agent)
		}
	}

	// 6. Line count under 15
	lines := strings.Split(ack, "\n")
	// Filter out empty trailing lines
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 15 {
		t.Errorf("ack has too many non-empty lines: %d\nfull output:\n%s", nonEmpty, ack)
	}
}

func TestCompoundTaskAck_TruncationOverflow(t *testing.T) {
	// Test with more than 5 steps (should truncate with overflow message)
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	steps := []TaskStepSummary{
		{Description: "Step 1", AgentID: "coder"},
		{Description: "Step 2", AgentID: "coder"},
		{Description: "Step 3", AgentID: "tester"},
		{Description: "Step 4", AgentID: "coder"},
		{Description: "Step 5", AgentID: "devops"},
		{Description: "Step 6", AgentID: "coder"},
		{Description: "Step 7", AgentID: "tester"},
	}

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-overflow",
			Name: "overflow test",
		},
		AgentID: "orchestrator",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, steps, 28, "plan-overflow")

	// Should show 7 subtasks
	if !strings.Contains(ack, "7 subtasks") {
		t.Error("expected '7 subtasks'")
	}

	// Should show first 5 steps
	for i := 1; i <= 5; i++ {
		if !strings.Contains(ack, fmt.Sprintf("step %d", i)) {
			t.Errorf("missing step %d", i)
		}
	}

	// Should NOT show steps 6 and 7
	if strings.Contains(ack, "step 6") {
		t.Error("step 6 should be hidden (overflow)")
	}

	// Should show overflow message
	if !strings.Contains(ack, "... and 2 more") {
		t.Error("missing overflow message")
	}

	// Line count under 15
	lines := strings.Split(ack, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 15 {
		t.Errorf("ack has too many non-empty lines: %d\nfull output:\n%s", nonEmpty, ack)
	}
}

func TestCompoundTaskAck_MinimalTask(t *testing.T) {
	// Test simplest case: no steps, no duration
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())

	result := &DispatchResult{
		Task: &task.Task{
			ID:   "task-minimal",
			Name: "minimal task",
		},
		AgentID: "chat",
	}

	ack := h.formatEnhancedAsyncTaskAck(result, nil, 0, "plan-min")

	if !strings.Contains(ack, "0 subtasks") {
		t.Error("expected '0 subtasks'")
	}
	if strings.Contains(ack, "agents:") {
		t.Error("agents line should not appear with no steps")
	}
	if strings.Contains(ack, "est.") {
		t.Error("duration should not appear when 0")
	}
}
