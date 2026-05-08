package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
