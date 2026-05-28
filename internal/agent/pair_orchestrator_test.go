package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// TestPairOrchestrator_SubscriptionSetup verifies bus topic subscriptions.
func TestPairOrchestrator_SubscriptionSetup(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	registry := &AgentRegistry{
		loops: make(map[string]*AgentLoop),
	}

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Registry: registry,
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	ctx := t.Context()
	if err := po.Start(ctx); err != nil {
		t.Fatalf("Failed to start PairOrchestrator: %v", err)
	}
	defer func() { _ = po.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	stats := msgBus.Stats()
	count, ok := stats[TopicPairStart]
	if !ok {
		t.Errorf("expected subscriber for topic %q, not found", TopicPairStart)
	}
	if count < 1 {
		t.Errorf("expected at least 1 subscriber for topic %q, got %d", TopicPairStart, count)
	}
}

// TestPairOrchestrator_InvalidPayload verifies error handling for bad payloads.
func TestPairOrchestrator_InvalidPayload(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	registry := &AgentRegistry{
		loops: make(map[string]*AgentLoop),
	}

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Registry: registry,
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	// Subscribe to error topic to capture errors
	errSub := msgBus.Subscribe("test-error", TopicPairError)

	ctx := t.Context()
	if err := po.Start(ctx); err != nil {
		t.Fatalf("Failed to start PairOrchestrator: %v", err)
	}
	defer func() { _ = po.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	// Publish invalid JSON
	msg := &models.BusMessage{
		ID:        "test-1",
		Type:      models.MessageTypeRequest,
		Topic:     TopicPairStart,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{invalid json}`),
	}
	msgBus.Publish(TopicPairStart, msg)

	// Wait for error
	select {
	case errMsg := <-errSub.Channel:
		var payload map[string]string
		if err := json.Unmarshal(errMsg.Payload, &payload); err != nil {
			t.Fatalf("Failed to parse error payload: %v", err)
		}
		if payload["error"] == "" {
			t.Error("Expected non-empty error message")
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for error message")
	}
}

// TestPairOrchestrator_StartRequestValidation verifies that missing fields are rejected.
func TestPairOrchestrator_StartRequestValidation(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	registry := &AgentRegistry{
		loops: make(map[string]*AgentLoop),
	}

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Registry: registry,
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	errSub := msgBus.Subscribe("test-error", TopicPairError)

	ctx := t.Context()
	if err := po.Start(ctx); err != nil {
		t.Fatalf("Failed to start PairOrchestrator: %v", err)
	}
	defer func() { _ = po.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	// Publish request with missing fields
	req := PairStartRequest{
		SessionID: "test-session",
		// Missing ActorID, ReviewerID, InitialPrompt
	}
	payload, _ := json.Marshal(req)
	msg := &models.BusMessage{
		ID:        "test-2",
		Type:      models.MessageTypeRequest,
		Topic:     TopicPairStart,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	msgBus.Publish(TopicPairStart, msg)

	select {
	case errMsg := <-errSub.Channel:
		var errPayload map[string]string
		if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
			t.Fatalf("Failed to parse error payload: %v", err)
		}
		if errPayload["session_id"] != "test-session" {
			t.Errorf("Expected session_id 'test-session', got %q", errPayload["session_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for error message")
	}
}

// TestClassifyVerdict verifies verdict classification logic.
func TestClassifyVerdict(t *testing.T) {
	po := &PairOrchestrator{logger: slogDiscardLogger()}

	tests := []struct {
		name             string
		input            string
		expectedVerdict  PairVerdict
		expectedEmpty    bool // true if feedback should be empty
	}{
		{"approved prefix", "APPROVED: looks great", PairVerdictApproved, true},
		{"rejected prefix", "REJECTED: fix the error handling", PairVerdictRejected, false},
		{"needs_more prefix", "NEEDS_MORE: what about edge cases?", PairVerdictNeedsMore, false},
		{"approved no colon", "APPROVED", PairVerdictApproved, true},
		{"rejected no colon", "REJECTED", PairVerdictRejected, true},
		{"no prefix defaults approved", "This is fine, the output is acceptable.", PairVerdictApproved, true},
		{"empty string", "", PairVerdictApproved, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verdict, feedback := po.classifyVerdict(tt.input)
			if verdict != tt.expectedVerdict {
				t.Errorf("classifyVerdict(%q) verdict = %q, want %q", tt.input, verdict, tt.expectedVerdict)
			}
			if tt.expectedEmpty && feedback != "" {
				t.Errorf("classifyVerdict(%q) feedback = %q, want empty", tt.input, feedback)
			}
		})
	}
}

// TestBuildReviewerPrompt verifies prompt construction.
func TestBuildReviewerPrompt(t *testing.T) {
	po := &PairOrchestrator{logger: slogDiscardLogger()}

	state := &BusPairSessionState{
		SessionID:     "test-session",
		InitialPrompt: "Research best practices for error handling",
	}

	prompt := po.buildReviewerPrompt(state, "Here is my research output...")

	if prompt == "" {
		t.Fatal("Expected non-empty reviewer prompt")
	}
	if len(prompt) < 50 {
		t.Errorf("Reviewer prompt seems too short: %q", prompt)
	}
}

// TestBuildRevisionPrompt verifies revision prompt construction.
func TestBuildRevisionPrompt(t *testing.T) {
	po := &PairOrchestrator{logger: slogDiscardLogger()}

	state := &BusPairSessionState{
		SessionID:     "test-session",
		InitialPrompt: "Research best practices for error handling",
	}

	prompt := po.buildRevisionPrompt(state, "initial output", "fix the tests", "REJECTED: fix the tests")

	if prompt == "" {
		t.Fatal("Expected non-empty revision prompt")
	}
	if len(prompt) < 50 {
		t.Errorf("Revision prompt seems too short: %q", prompt)
	}
}

// TestPairOrchestrator_GetSession verifies session tracking.
func TestPairOrchestrator_GetSession(t *testing.T) {
	po := &PairOrchestrator{
		sessions: make(map[string]*BusPairSessionState),
		logger:   slogDiscardLogger(),
	}

	// Non-existent session
	if s := po.GetSession("nonexistent"); s != nil {
		t.Error("Expected nil for non-existent session")
	}

	// Add a session
	po.sessions["test"] = &BusPairSessionState{SessionID: "test"}
	if s := po.GetSession("test"); s == nil {
		t.Error("Expected non-nil for existing session")
	}

	if po.ActiveSessionCount() != 1 {
		t.Errorf("ActiveSessionCount = %d, want 1", po.ActiveSessionCount())
	}
}

// TestPairTopic verifies topic formatting.
func TestPairTopic(t *testing.T) {
	got := PairTopic("abc-123")
	want := "pair.abc-123.turn"
	if got != want {
		t.Errorf("PairTopic(%q) = %q, want %q", "abc-123", got, want)
	}
}
