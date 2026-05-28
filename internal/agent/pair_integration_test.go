package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TestPairSessionLifecycle tests the full lifecycle of a pair session:
// create session -> set criteria -> run rounds until convergence.
func TestPairSessionLifecycle(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slogDiscardLogger(),
	})

	session := pm.CreateSession(
		"task-lifecycle-test",
		"implement user registration with validation",
		"coder",
		"planner",
		3,
	)

	session.SetCriteria([]string{
		"implement registration handler",
		"add input validation",
		"write unit tests",
	})

	if session.State != PairSessionActive {
		t.Fatalf("expected active state, got %q", session.State)
	}
	if len(session.Context.PendingCriteria) != 3 {
		t.Fatalf("expected 3 pending criteria, got %d", len(session.Context.PendingCriteria))
	}

	// Verify session is retrievable
	got, ok := pm.GetSessionByTask("task-lifecycle-test")
	if !ok {
		t.Fatal("expected to find session by task")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	// Simulate convergence: directly update context
	session.Context.PendingCriteria = nil
	session.Context.AcceptedCriteria = []string{
		"implement registration handler",
		"add input validation",
		"write unit tests",
	}

	if !session.Context.HasConverged() {
		t.Error("expected convergence after moving all criteria to accepted")
	}

	session.MarkConverged()
	if session.State != PairSessionConverged {
		t.Errorf("expected converged state, got %q", session.State)
	}

	// Verify session is no longer returned by task lookup
	_, ok = pm.GetSessionByTask("task-lifecycle-test")
	if ok {
		t.Error("converged session should not be returned as active")
	}
}

// TestPairSessionExhaustion tests the exhaustion path.
func TestPairSessionExhaustion(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slogDiscardLogger(),
	})

	session := pm.CreateSession("task-exhaust", "spec", "coder", "planner", 2)
	session.SetCriteria([]string{"criterion A"})

	// Simulate two failed rounds
	session.Context.RecordAttempt(&Attempt{
		Round:       1,
		ActorOutput: "attempt 1",
		Review:      &ReviewResult{Status: ReviewRejected, Feedback: "not good enough"},
	})

	session.Context.RecordAttempt(&Attempt{
		Round:       2,
		ActorOutput: "attempt 2",
		Review:      &ReviewResult{Status: ReviewRejected, Feedback: "still not good"},
	})

	if !session.IsExhausted() {
		t.Error("should be exhausted after 2 attempts with max_rounds=2")
	}

	session.MarkExhausted()
	if session.State != PairSessionExhausted {
		t.Errorf("expected exhausted state, got %q", session.State)
	}
}

// TestPairSessionWithTaskStore tests finalization with a real task store.
func TestPairSessionWithTaskStore(t *testing.T) {
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	// Create a task
	tsk := newTestTask("pair-task", "implement feature X")
	tsk.SetState(task.StateExecuting)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	session := pm.CreateSession(tsk.ID, "implement feature X", "coder", "planner", 3)

	// Simulate successful convergence
	session.SetCriteria([]string{"write code", "add tests"})
	session.Context.PendingCriteria = nil
	session.Context.AcceptedCriteria = []string{"write code", "add tests"}

	session.MarkConverged()
	pm.finalizeTask(context.Background(), session, true)

	// Verify task state
	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateCompleted {
		t.Errorf("expected task completed, got %q", updated.State)
	}
}

// TestPairSessionFailureFinalization tests failed finalization.
func TestPairSessionFailureFinalization(t *testing.T) {
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		Logger:    slogDiscardLogger(),
	})

	tsk := newTestTask("pair-fail-task", "implement feature Y")
	tsk.SetState(task.StateExecuting)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	session := pm.CreateSession(tsk.ID, "implement feature Y", "coder", "planner", 2)
	session.MarkExhausted()
	pm.finalizeTask(context.Background(), session, false)

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateFailed {
		t.Errorf("expected task failed, got %q", updated.State)
	}
}

// TestStrategicPlanner_PairSessionPlan verifies the planner creates pair sessions
// for compound intents when PairManager is available.
func TestStrategicPlanner_PairSessionPlan(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slogDiscardLogger(),
	})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:   taskStore,
		StepStore:   stepStore,
		Bus:         msgBus,
		PairManager: pm,
		Logger:      slogDiscardLogger(),
	})

	// Create a task
	tsk := newTestTask("pair-plan-test", "implement auth and add tests")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Plan with compound intent
	err = sp.Plan(context.Background(), PlanRequest{
		TaskID: tsk.ID,
		Input:  "implement authentication module with OAuth2 and write comprehensive tests for login flow",
		Intent: string(IntentCompound),
	})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Verify pair session was created
	session, ok := pm.GetSessionByTask(tsk.ID)
	if !ok {
		t.Fatal("expected pair session to be created for compound task")
	}

	if session.ActorAgentID != "coder" {
		t.Errorf("expected actor 'coder', got %q", session.ActorAgentID)
	}
	if session.ReviewerAgentID != "planner" {
		t.Errorf("expected reviewer 'planner', got %q", session.ReviewerAgentID)
	}

	// Verify steps were created
	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (actor + reviewer), got %d", len(steps))
	}

	// Verify step IDs are tracked in session
	for _, s := range steps {
		if !session.OwnsStep(s.ID) {
			t.Errorf("session should own step %q", s.ID)
		}
	}
}

// TestPairManagerConcurrentAccess tests thread safety of PairManager.
func TestPairManagerConcurrentAccess(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slogDiscardLogger(),
	})

	var createdCount atomic.Int32

	// Create sessions concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			session := pm.CreateSession(
				"task-concurrent",
				"spec",
				"coder",
				"planner",
				3,
			)
			_ = session.ID
			createdCount.Add(1)
			_, _ = pm.GetSessionByTask("task-concurrent")
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no panics and all sessions exist
	all := pm.ListSessions(false)
	if len(all) != 10 {
		t.Errorf("expected 10 sessions, got %d", len(all))
	}

	if createdCount.Load() != 10 {
		t.Errorf("expected 10 creations, got %d", createdCount.Load())
	}
}

// --- Option C (Bus Channel) integration tests ---

// TestPairOrchestrator_FullConversation tests a complete actor-reviewer cycle
// that approves on the first turn (when no real agents are configured, expects error).
func TestPairOrchestrator_FullConversation(t *testing.T) {
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

	// Subscribe to results
	resultSub := msgBus.Subscribe("test-result", TopicPairResult)

	// Subscribe to turns on the session topic
	sessionID := "test-pair-001"
	turnSub := msgBus.Subscribe("test-turn", PairTopic(sessionID))

	// Publish start request
	req := PairStartRequest{
		SessionID:     sessionID,
		ActorID:       "analyst",
		ReviewerID:    "planner",
		InitialPrompt: "Research error handling best practices",
		MaxTurns:      3,
	}
	payload, _ := json.Marshal(req)
	msg := &models.BusMessage{
		ID:        "test-start-1",
		Type:      models.MessageTypeRequest,
		Topic:     TopicPairStart,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	msgBus.Publish(TopicPairStart, msg)

	// Since we don't have real agents, the registry.RunAgent will fail.
	// Verify we get an error on the pair.error topic.
	errSub := msgBus.Subscribe("test-err", TopicPairError)

	select {
	case errMsg := <-errSub.Channel:
		var errPayload map[string]string
		if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
			t.Fatalf("Failed to parse error: %v", err)
		}
		// Expected: agent loops don't exist, so RunAgent returns an error
		if errPayload["session_id"] != sessionID {
			t.Errorf("error session_id = %q, want %q", errPayload["session_id"], sessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for pair error (registry has no agent loops)")
	}

	// Clean up subscriptions
	msgBus.Unsubscribe(turnSub)
	msgBus.Unsubscribe(resultSub)
	msgBus.Unsubscribe(errSub)
}

// TestPairOrchestrator_ErrorOnMissingRegistry verifies that a nil registry
// produces an error rather than a panic.
func TestPairOrchestrator_ErrorOnMissingRegistry(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Registry: nil, // explicitly nil
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	ctx := t.Context()
	if err := po.Start(ctx); err != nil {
		t.Fatalf("Failed to start PairOrchestrator: %v", err)
	}
	defer func() { _ = po.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	errSub := msgBus.Subscribe("test-err", TopicPairError)

	req := PairStartRequest{
		SessionID:     "test-nil-registry",
		ActorID:       "analyst",
		ReviewerID:    "planner",
		InitialPrompt: "test",
	}
	payload, _ := json.Marshal(req)
	msg := &models.BusMessage{
		ID:        "test-nil-2",
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
			t.Fatalf("Failed to parse error: %v", err)
		}
		if errPayload["session_id"] != "test-nil-registry" {
			t.Errorf("error session_id = %q, want %q", errPayload["session_id"], "test-nil-registry")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for error from nil registry")
	}

	msgBus.Unsubscribe(errSub)
}

// TestPairOrchestrator_StartStop verifies lifecycle doesn't leak goroutines.
func TestPairOrchestrator_StartStop(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Bus:    msgBus,
		Logger: slogDiscardLogger(),
	})

	ctx := t.Context()
	if err := po.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := po.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestIntentPair_Valid verifies IntentPair is a recognized intent type.
func TestIntentPair_Valid(t *testing.T) {
	if !IsValidIntentType(string(IntentPair)) {
		t.Errorf("IntentPair should be a valid intent type")
	}
}

// TestIntentPair_DefaultAgent verifies IntentPair routes to a valid agent.
func TestIntentPair_DefaultAgent(t *testing.T) {
	agent := IntentPair.DefaultAgent()
	if agent == "" {
		t.Error("IntentPair.DefaultAgent() returned empty string")
	}
}

// TestIntentPair_Category verifies IntentPair is in the defer category.
func TestIntentPair_Category(t *testing.T) {
	if IntentPair.Category() != CategoryDefer {
		t.Errorf("IntentPair.Category() = %q, want %q", IntentPair.Category(), CategoryDefer)
	}
}

// TestDispatcherShouldRouteToPair verifies the ShouldRouteToPair method.
func TestDispatcherShouldRouteToPair(t *testing.T) {
	d := &Dispatcher{}

	// Nil result
	if d.ShouldRouteToPair(nil) {
		t.Error("ShouldRouteToPair(nil) should return false")
	}

	// Non-pair intent
	if d.ShouldRouteToPair(&DispatchResult{
		Intent: &Intent{Type: string(IntentCode)},
	}) {
		t.Error("ShouldRouteToPair(IntentCode) should return false")
	}

	// Pair intent
	if !d.ShouldRouteToPair(&DispatchResult{
		Intent: &Intent{Type: string(IntentPair)},
	}) {
		t.Error("ShouldRouteToPair(IntentPair) should return true")
	}

	// Nil intent
	if d.ShouldRouteToPair(&DispatchResult{Intent: nil}) {
		t.Error("ShouldRouteToPair(nil intent) should return false")
	}
}
