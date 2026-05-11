package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"

	_ "github.com/mattn/go-sqlite3"
)

// TestOrchestrator_SubscriptionSetup verifies that the Orchestrator
// subscribes to the expected bus topics.
func TestOrchestrator_SubscriptionSetup(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())
	defer bus.Close()

	// Create minimal orchestrator with nil deps (we're only testing subscriptions)
	orchestrator := &Orchestrator{
		bus:    bus,
		logger: slogDiscardLogger(),
	}

	ctx := t.Context()

	// Start orchestrator
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer orchestrator.Stop(context.Background())

	// Give the subscription goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Verify bus has subscribers for expected topics
	stats := bus.Stats()

	expectedTopics := []string{
		"orchestrator.plan",
		"orchestrator.schedule",
		"queue.job.completed",
		"queue.job.failed",
	}

	for _, topic := range expectedTopics {
		count, ok := stats[topic]
		if !ok {
			t.Errorf("expected subscriber for topic %q, not found", topic)
		}
		if count < 1 {
			t.Errorf("expected at least 1 subscriber for topic %q, got %d", topic, count)
		}
	}
}

// TestOrchestrator_handlePlanRequest_InvalidPayload verifies error handling for invalid payloads.
func TestOrchestrator_handlePlanRequest_InvalidPayload(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())
	defer bus.Close()

	orchestrator := &Orchestrator{
		bus:    bus,
		logger: slogDiscardLogger(),
	}

	ctx := context.Background()

	// Create invalid JSON payload
	msg := &models.BusMessage{
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.plan",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{invalid json}`),
	}

	// Call handler - should not panic, should log error
	orchestrator.handlePlanRequest(ctx, msg)
	// Test passes if no panic occurred
}

// TestOrchestrator_handleScheduleRequest_InvalidPayload verifies error handling for invalid payloads.
func TestOrchestrator_handleScheduleRequest_InvalidPayload(t *testing.T) {
	bus := bus.New(nil, slogDiscardLogger())
	defer bus.Close()

	orchestrator := &Orchestrator{
		bus:    bus,
		logger: slogDiscardLogger(),
	}

	ctx := context.Background()

	// Create invalid JSON payload
	msg := &models.BusMessage{
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.schedule",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{invalid json}`),
	}

	// Call handler - should not panic
	orchestrator.handleScheduleRequest(ctx, msg)
	// Test passes if no panic occurred
}

// TestPlanRequest_JsonParsing verifies that PlanRequest JSON is parsed correctly.
// This is a unit test for the JSON structure, not the handler integration.
func TestPlanRequest_JsonParsing(t *testing.T) {
	req := PlanRequest{
		TaskID:       "task-123",
		SessionID:    "session-456",
		Input:        "build a feature",
		Intent:       "code",
		IsCompound:   true,
		CompoundType: "sequential",
	}

	// Marshal
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal
	var parsed PlanRequest
	err = json.Unmarshal(payload, &parsed)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify
	if parsed.TaskID != req.TaskID {
		t.Errorf("TaskID: expected %s, got %s", req.TaskID, parsed.TaskID)
	}
	if parsed.SessionID != req.SessionID {
		t.Errorf("SessionID: expected %s, got %s", req.SessionID, parsed.SessionID)
	}
	if parsed.Input != req.Input {
		t.Errorf("Input: expected %s, got %s", req.Input, parsed.Input)
	}
	if parsed.Intent != req.Intent {
		t.Errorf("Intent: expected %s, got %s", req.Intent, parsed.Intent)
	}
	if !parsed.IsCompound {
		t.Error("expected IsCompound to be true")
	}
	if parsed.CompoundType != req.CompoundType {
		t.Errorf("CompoundType: expected %s, got %s", req.CompoundType, parsed.CompoundType)
	}
}

// TestOrchestrator_PlanRequestHandling verifies that the Orchestrator correctly
// receives a plan request from the bus and delegates it to the StrategicPlanner.
// It uses a real bus with a subscription on orchestrator.plan and confirms the
// handler parses the payload and forwards it to the planner.
func TestOrchestrator_PlanRequestHandling(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create a StrategicPlanner with a stubbed Plan method by using the real
	// planner but intercepting via the bus subscription in the orchestrator.
	// We use a minimal planner with nil registry (fallback path only) and a
	// task/step store so Plan() actually runs.
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	// Create a task in the store so Plan() can find it
	tsk := newTestTask("task-plan-test", "build a feature")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	stepStore := taskStore.StepStore()

	// Create a queue so the tactical scheduler can enqueue jobs
	queueDBPath := filepath.Join(tmpDir, "queue.db")
	q, err := queue.NewPersistentQueue(queueDBPath, msgBus, slogDiscardLogger())
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	defer q.Close()

	strategic := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       nil,
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})

	tactical := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     q,
		Registry:  nil,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	orchestrator := NewOrchestrator(OrchestratorDeps{
		Strategic: strategic,
		Tactical:  tactical,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	ctx := t.Context()

	// Subscribe to task.planned to verify the full flow completes
	taskPlannedSub := msgBus.Subscribe("test-observer", "task.planned")
	defer msgBus.Unsubscribe(taskPlannedSub)

	// Start the orchestrator (subscribes to orchestrator.plan)
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer orchestrator.Stop(context.Background())

	// Give subscriptions time to register
	time.Sleep(20 * time.Millisecond)

	// Publish a plan request to the bus (simulating ChatHandler.publishPlanRequest)
	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-orch-test",
		Input:     "build a feature",
		Intent:    "code",
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal plan request: %v", err)
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.plan",
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	msgBus.Publish("orchestrator.plan", msg)

	// Wait for the strategic planner to publish task.planned (which means it
	// successfully parsed the request, created steps, and completed planning).
	select {
	case msg := <-taskPlannedSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("Failed to unmarshal task.planned event: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task.planned event task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		if event["session_id"] != "session-orch-test" {
			t.Errorf("task.planned event session_id = %v, want session-orch-test", event["session_id"])
		}
		totalSteps, ok := event["total_steps"].(float64)
		if !ok || totalSteps < 1 {
			t.Errorf("task.planned event total_steps = %v, want >= 1", event["total_steps"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.planned event - orchestrator may not have handled plan request")
	}
}

// TestScheduleRequest_JsonParsing verifies that schedule request JSON is parsed correctly.
func TestScheduleRequest_JsonParsing(t *testing.T) {
	req := map[string]string{"task_id": "task-789"}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed map[string]string
	err = json.Unmarshal(payload, &parsed)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed["task_id"] != "task-789" {
		t.Errorf("expected task-789, got %s", parsed["task_id"])
	}
}

// newTestTaskStore creates a temporary SQLite-backed task store for testing.
func newTestTaskStore(tmpDir string) (*task.Store, error) {
	dbPath := filepath.Join(tmpDir, "test-tasks.db")
	return task.NewStore(dbPath, slogDiscardLogger())
}

// newTestTask creates a task for testing with a deterministic ID.
func newTestTask(name, description string) *task.Task {
	t := task.NewTask(name, description)
	return t
}
