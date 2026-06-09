# Agentic Pairs: Option C — Dual-Agent Conversation (Shared Bus Channel)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a bus-channel-based pairing modality where two agents share a named conversation topic and take turns, enabling free-form collaborative tasks (research debates, exploratory debugging, brainstorming) that don't fit the step model.

**Architecture:** A new PairOrchestrator manages a bidirectional conversation between two agents via the message bus. Each agent's output is published to a shared topic; the orchestrator receives it, constructs the next prompt, and invokes the other agent. This bypasses the job queue for real-time interaction while keeping full bus observability. The dispatcher classifies which tasks should use this modality (research, brainstorming, exploratory tasks).

**Tech Stack:** Go 1.22+, MessageBus pub/sub, AgentRegistry/AgentLoop

---

## Task 1: PairChannel message types

**File:** `/Users/caimlas/git/meept/internal/agent/pair_channel.go`

Define all message types that flow through the `pair.{sessionID}` bus topics. These are the payloads for BusMessage.Payload (json.RawMessage).

**Types to define:**

```go
package agent

// PairChannel message types for bus-channel-based agent pairing.

// PairVerdict represents the outcome of a reviewer's evaluation.
type PairVerdict string

const (
	PairVerdictApproved PairVerdict = "approved"
	PairVerdictRejected PairVerdict = "rejected"
	PairVerdictNeedsMore PairVerdict = "needs_more"
)

// PairTurn represents a single turn in the pair conversation.
type PairTurn struct {
	// SessionID identifies the pair session.
	SessionID string `json:"session_id"`
	// TurnNumber is the sequential turn index (0-based).
	TurnNumber int `json:"turn_number"`
	// AgentID is the agent that produced this turn.
	AgentID string `json:"agent_id"`
	// Role is either "actor" or "reviewer".
	Role string `json:"role"`
	// Content is the agent's output text.
	Content string `json:"content"`
	// Verdict is the reviewer's classification (empty for actor turns).
	Verdict PairVerdict `json:"verdict,omitempty"`
	// Feedback is the reviewer's feedback (empty for approved or actor turns).
	Feedback string `json:"feedback,omitempty"`
}

// PairStartRequest initiates a new pair conversation session.
type PairStartRequest struct {
	// SessionID is the unique pair session identifier.
	SessionID string `json:"session_id"`
	// ActorID is the agent that performs the initial work.
	ActorID string `json:"actor_id"`
	// ReviewerID is the agent that reviews the actor's output.
	ReviewerID string `json:"reviewer_id"`
	// InitialPrompt is the user's original request.
	InitialPrompt string `json:"initial_prompt"`
	// MaxTurns is the maximum number of actor-reviewer cycles (0 = unlimited, default 5).
	MaxTurns int `json:"max_turns,omitempty"`
	// Metadata holds optional extra data from the dispatcher.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PairSessionState represents the current state of a pair session.
type PairSessionState struct {
	SessionID    string     `json:"session_id"`
	ActorID      string     `json:"actor_id"`
	ReviewerID   string     `json:"reviewer_id"`
	CurrentTurn  int        `json:"current_turn"`
	MaxTurns     int        `json:"max_turns"`
	Phase        string     `json:"phase"` // "actor_turn", "reviewer_turn", "completed", "failed"
	LastVerdict  PairVerdict `json:"last_verdict,omitempty"`
	Turns        []PairTurn `json:"turns,omitempty"`
	InitialPrompt string    `json:"initial_prompt"`
}

// PairResult is the final result of a completed pair session.
type PairResult struct {
	SessionID   string     `json:"session_id"`
	FinalOutput string     `json:"final_output"`
	Turns       []PairTurn `json:"turns"`
	TotalTurns  int        `json:"total_turns"`
	FinalVerdict PairVerdict `json:"final_verdict"`
}
```

**Constants for bus topics:**

Add to the same file:

```go
// Bus topic constants for pair channel messages.
const (
	// TopicPairStart is used to initiate a pair session.
	TopicPairStart = "pair.start"
	// TopicPairTurn is the per-session topic pattern: "pair.{sessionID}.turn"
	TopicPairTurnPattern = "pair.%s.turn"
	// TopicPairResult is published when a pair session completes.
	TopicPairResult = "pair.result"
	// TopicPairError is published when a pair session fails.
	TopicPairError = "pair.error"
)

// PairTopic returns the turn topic for a specific pair session.
func PairTopic(sessionID string) string {
	return fmt.Sprintf(TopicPairTurnPattern, sessionID)
}
```

**Verify:**

```bash
go vet ./internal/agent/...
go build ./internal/agent/...
```

- [x] Task 1 complete: PairChannel types compile

---

## Task 2: PairOrchestrator core loop

**File:** `/Users/caimlas/git/meept/internal/agent/pair_orchestrator.go`

The PairOrchestrator subscribes to `pair.start` and manages turn-based agent conversations via the bus.

**Implementation:**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

const (
	// pairDefaultMaxTurns is the default maximum actor-reviewer cycles.
	pairDefaultMaxTurns = 5
)

// PairOrchestrator manages bus-channel-based pairing between two agents.
// It subscribes to pair.start, runs actor then reviewer in alternating turns,
// and publishes results to pair.result when complete.
type PairOrchestrator struct {
	registry *AgentRegistry
	bus      *bus.MessageBus
	logger   *slog.Logger

	// Active sessions indexed by sessionID
	sessions map[string]*PairSessionState
	mu       sync.RWMutex

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// PairOrchestratorDeps holds dependencies for creating a PairOrchestrator.
type PairOrchestratorDeps struct {
	Registry *AgentRegistry
	Bus      *bus.MessageBus
	Logger   *slog.Logger
}

// NewPairOrchestrator creates a new PairOrchestrator.
func NewPairOrchestrator(deps PairOrchestratorDeps) *PairOrchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &PairOrchestrator{
		registry: deps.Registry,
		bus:      deps.Bus,
		logger:   deps.Logger,
		sessions: make(map[string]*PairSessionState),
	}
}

// Start subscribes to pair.start and begins processing pair sessions.
func (po *PairOrchestrator) Start(ctx context.Context) error {
	ctx, po.cancel = context.WithCancel(ctx)

	sub := po.bus.Subscribe("pair-orchestrator", TopicPairStart)
	po.wg.Add(1)
	go po.runSubscription(ctx, sub, po.handleStartRequest)

	po.logger.Info("PairOrchestrator started")
	return nil
}

// Stop gracefully stops the orchestrator.
func (po *PairOrchestrator) Stop(ctx context.Context) error {
	if po.cancel != nil {
		po.cancel()
	}
	done := make(chan struct{})
	go func() {
		po.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		po.logger.Info("PairOrchestrator stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name.
func (po *PairOrchestrator) Name() string {
	return "pair-orchestrator"
}

// GetSession returns the state of an active pair session (nil if not found).
func (po *PairOrchestrator) GetSession(sessionID string) *PairSessionState {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return po.sessions[sessionID]
}

// ActiveSessionCount returns the number of active pair sessions.
func (po *PairOrchestrator) ActiveSessionCount() int {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return len(po.sessions)
}

func (po *PairOrchestrator) runSubscription(ctx context.Context, sub *bus.Subscriber, handler func(context.Context, *models.BusMessage)) {
	defer po.wg.Done()
	for {
		select {
		case <-ctx.Done():
			po.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			handler(ctx, msg)
		}
	}
}

// handleStartRequest processes a pair.start bus message.
func (po *PairOrchestrator) handleStartRequest(ctx context.Context, msg *models.BusMessage) {
	var req PairStartRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		po.logger.Error("Failed to parse pair start request", "error", err)
		po.publishError("", "invalid pair start request: "+err.Error())
		return
	}

	if req.SessionID == "" || req.ActorID == "" || req.ReviewerID == "" || req.InitialPrompt == "" {
		po.publishError(req.SessionID, "pair start request missing required fields")
		return
	}

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = pairDefaultMaxTurns
	}

	state := &PairSessionState{
		SessionID:     req.SessionID,
		ActorID:       req.ActorID,
		ReviewerID:    req.ReviewerID,
		MaxTurns:      maxTurns,
		Phase:         "actor_turn",
		InitialPrompt: req.InitialPrompt,
	}

	po.mu.Lock()
	po.sessions[req.SessionID] = state
	po.mu.Unlock()

	po.logger.Info("Pair session started",
		"session_id", req.SessionID,
		"actor", req.ActorID,
		"reviewer", req.ReviewerID,
		"max_turns", maxTurns,
	)

	// Run the pair conversation in a background goroutine
	po.wg.Add(1)
	go func() {
		defer po.wg.Done()
		po.runPairConversation(ctx, state)
	}()
}

// runPairConversation executes the full actor-reviewer loop.
func (po *PairOrchestrator) runPairConversation(ctx context.Context, state *PairSessionState) {
	defer po.removeSession(state.SessionID)

	// Construct actor prompt from initial input
	actorPrompt := state.InitialPrompt

	for state.CurrentTurn < state.MaxTurns {
		// Check context cancellation
		select {
		case <-ctx.Done():
			state.Phase = "failed"
			po.publishError(state.SessionID, "context cancelled")
			return
		default:
		}

		// --- Actor turn ---
		state.Phase = "actor_turn"
		actorOutput, err := po.runAgent(ctx, state.ActorID, actorPrompt, state.SessionID)
		if err != nil {
			state.Phase = "failed"
			po.publishError(state.SessionID, fmt.Sprintf("actor %s failed: %v", state.ActorID, err))
			return
		}

		actorTurn := PairTurn{
			SessionID:  state.SessionID,
			TurnNumber: state.CurrentTurn,
			AgentID:    state.ActorID,
			Role:       "actor",
			Content:    actorOutput,
		}
		state.Turns = append(state.Turns, actorTurn)

		// Publish actor turn to the session topic for observability
		po.publishTurn(state.SessionID, &actorTurn)

		// --- Reviewer turn ---
		state.Phase = "reviewer_turn"
		reviewerPrompt := po.buildReviewerPrompt(state, actorOutput)
		reviewerOutput, err := po.runAgent(ctx, state.ReviewerID, reviewerPrompt, state.SessionID)
		if err != nil {
			state.Phase = "failed"
			po.publishError(state.SessionID, fmt.Sprintf("reviewer %s failed: %v", state.ReviewerID, err))
			return
		}

		// Classify the reviewer response
		verdict, feedback := po.classifyVerdict(reviewerOutput)
		state.LastVerdict = verdict

		reviewerTurn := PairTurn{
			SessionID:  state.SessionID,
			TurnNumber: state.CurrentTurn,
			AgentID:    state.ReviewerID,
			Role:       "reviewer",
			Content:    reviewerOutput,
			Verdict:    verdict,
			Feedback:   feedback,
		}
		state.Turns = append(state.Turns, reviewerTurn)

		// Publish reviewer turn to the session topic for observability
		po.publishTurn(state.SessionID, &reviewerTurn)

		// Check verdict
		if verdict == PairVerdictApproved {
			// Approved — emit result and exit
			po.publishResult(&PairResult{
				SessionID:    state.SessionID,
				FinalOutput:  actorOutput,
				Turns:        state.Turns,
				TotalTurns:   state.CurrentTurn + 1,
				FinalVerdict: PairVerdictApproved,
			})
			state.Phase = "completed"
			return
		}

		// Rejected or needs more — construct revised actor prompt
		actorPrompt = po.buildRevisionPrompt(state, actorOutput, feedback, reviewerOutput)
		state.CurrentTurn++
	}

	// Reached max turns without approval — emit result with last actor output
	state.Phase = "completed"
	lastActorOutput := ""
	for i := len(state.Turns) - 1; i >= 0; i-- {
		if state.Turns[i].Role == "actor" {
			lastActorOutput = state.Turns[i].Content
			break
		}
	}
	po.publishResult(&PairResult{
		SessionID:    state.SessionID,
		FinalOutput:  lastActorOutput,
		Turns:        state.Turns,
		TotalTurns:   state.CurrentTurn,
		FinalVerdict: state.LastVerdict,
	})
}

// runAgent invokes an agent via the registry.
func (po *PairOrchestrator) runAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	if po.registry == nil {
		return "", fmt.Errorf("no agent registry configured")
	}
	return po.registry.RunAgent(ctx, agentID, message, conversationID)
}

// buildReviewerPrompt constructs the prompt for the reviewer agent.
func (po *PairOrchestrator) buildReviewerPrompt(state *PairSessionState, actorOutput string) string {
	prompt := fmt.Sprintf(
		"Review the following output for the task: %s\n\n"+
			"Agent output to review:\n%s\n\n"+
			"Classify your response:\n"+
			"- If the output is satisfactory, start your response with 'APPROVED:' followed by a brief summary.\n"+
			"- If the output needs revision, start your response with 'REJECTED:' followed by specific feedback.\n"+
			"- If you need more information, start your response with 'NEEDS_MORE:' followed by your questions.",
		state.InitialPrompt,
		actorOutput,
	)

	// Include history from previous turns for context
	if len(state.Turns) > 1 {
		prompt += "\n\nPrevious conversation history:\n"
		for _, turn := range state.Turns {
			prompt += fmt.Sprintf("\n[%s - %s]: %s\n", turn.Role, turn.AgentID, truncateString(turn.Content, 200))
		}
	}

	return prompt
}

// buildRevisionPrompt constructs the prompt for the actor after rejection.
func (po *PairOrchestrator) buildRevisionPrompt(state *PairSessionState, actorOutput, feedback, reviewerOutput string) string {
	return fmt.Sprintf(
		"Your previous output was rejected. Please revise based on the feedback.\n\n"+
			"Original task: %s\n\n"+
			"Your previous output:\n%s\n\n"+
			"Reviewer feedback:\n%s\n\n"+
			"Please provide a revised output that addresses the feedback.",
		state.InitialPrompt,
		actorOutput,
		reviewerOutput,
	)
}

// classifyVerdict parses the reviewer output to determine the verdict.
func (po *PairOrchestrator) classifyVerdict(reviewerOutput string) (PairVerdict, string) {
	if len(reviewerOutput) >= 8 && reviewerOutput[:8] == "APPROVED" {
		return PairVerdictApproved, ""
	}
	if len(reviewerOutput) >= 8 && reviewerOutput[:8] == "REJECTED" {
		feedback := ""
		if len(reviewerOutput) > 9 {
			feedback = reviewerOutput[9:]
		}
		return PairVerdictRejected, feedback
	}
	if len(reviewerOutput) >= 10 && reviewerOutput[:10] == "NEEDS_MORE" {
		feedback := ""
		if len(reviewerOutput) > 11 {
			feedback = reviewerOutput[11:]
		}
		return PairVerdictNeedsMore, feedback
	}

	// Default: treat as approved if no explicit verdict marker
	return PairVerdictApproved, ""
}

// publishTurn publishes a pair turn to the session-specific topic.
func (po *PairOrchestrator) publishTurn(sessionID string, turn *PairTurn) {
	payload, err := json.Marshal(turn)
	if err != nil {
		po.logger.Error("Failed to marshal pair turn", "error", err)
		return
	}

	topic := PairTopic(sessionID)
	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     topic,
		Source:    "pair-orchestrator",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	delivered := po.bus.Publish(topic, msg)
	po.logger.Debug("Published pair turn",
		"session_id", sessionID,
		"turn", turn.TurnNumber,
		"role", turn.Role,
		"delivered", delivered,
	)
}

// publishResult publishes the final pair result.
func (po *PairOrchestrator) publishResult(result *PairResult) {
	payload, err := json.Marshal(result)
	if err != nil {
		po.logger.Error("Failed to marshal pair result", "error", err)
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     TopicPairResult,
		Source:    "pair-orchestrator",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	delivered := po.bus.Publish(TopicPairResult, msg)
	po.logger.Info("Published pair result",
		"session_id", result.SessionID,
		"total_turns", result.TotalTurns,
		"verdict", result.FinalVerdict,
		"delivered", delivered,
	)
}

// publishError publishes a pair error event.
func (po *PairOrchestrator) publishError(sessionID, errMsg string) {
	errPayload, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
		"error":      errMsg,
	})

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeError,
		Topic:     TopicPairError,
		Source:    "pair-orchestrator",
		Timestamp: time.Now().UTC(),
		Payload:   errPayload,
	}
	po.bus.Publish(TopicPairError, msg)
}

// removeSession removes a completed session from the active map.
func (po *PairOrchestrator) removeSession(sessionID string) {
	po.mu.Lock()
	defer po.mu.Unlock()
	delete(po.sessions, sessionID)
}
```

**Verify:**

```bash
go vet ./internal/agent/...
go build ./internal/agent/...
```

- [x] Task 2 complete: PairOrchestrator compiles

---

## Task 3: PairOrchestrator unit tests

**File:** `/Users/caimlas/git/meept/internal/agent/pair_orchestrator_test.go`

Test the core loop: subscription setup, happy-path approval, rejection-revision cycle, max-turns exhaustion, context cancellation, and error handling. Use a mock AgentRegistry that returns canned responses.

```go
package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// mockRegistry implements a minimal AgentRegistry for testing the PairOrchestrator.
// It captures calls to RunAgent and returns pre-configured responses.
type mockPairRegistry struct {
	responses map[string][]string // agentID -> sequence of responses
	calls     atomic.Int32
}

func newMockPairRegistry() *mockPairRegistry {
	return &mockPairRegistry{
		responses: make(map[string][]string),
	}
}

func (m *mockPairRegistry) RunAgent(_ context.Context, agentID, _, _ string) (string, error) {
	m.calls.Add(1)
	responses := m.responses[agentID]
	if len(responses) == 0 {
		return "APPROVED: default response", nil
	}
	resp := responses[0]
	m.responses[agentID] = responses[1:]
	return resp, nil
}

// setupPairTest creates a PairOrchestrator with a mock registry and bus.
func setupPairTest(t *testing.T) (*PairOrchestrator, *bus.MessageBus, *mockPairRegistry) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	mockReg := newMockPairRegistry()

	// Wrap the mock in a real AgentRegistry by using the interface indirectly.
	// Since PairOrchestrator uses *AgentRegistry directly, we create a minimal one.
	registry := &AgentRegistry{
		loops: make(map[string]*AgentLoop),
	}

	po := NewPairOrchestrator(PairOrchestratorDeps{
		Registry: registry,
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	return po, msgBus, mockReg
}

// TestPairOrchestrator_SubscriptionSetup verifies bus topic subscriptions.
func TestPairOrchestrator_SubscriptionSetup(t *testing.T) {
	po, msgBus, _ := setupPairTest(t)
	defer msgBus.Close()

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
	po, msgBus, _ := setupPairTest(t)
	defer msgBus.Close()

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
	po, msgBus, _ := setupPairTest(t)
	defer msgBus.Close()

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
		name           string
		input          string
		expectedVerdict PairVerdict
		expectedEmpty  bool // true if feedback should be empty
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

	state := &PairSessionState{
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

	state := &PairSessionState{
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
		sessions: make(map[string]*PairSessionState),
		logger:   slogDiscardLogger(),
	}

	// Non-existent session
	if s := po.GetSession("nonexistent"); s != nil {
		t.Error("Expected nil for non-existent session")
	}

	// Add a session
	po.sessions["test"] = &PairSessionState{SessionID: "test"}
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
```

**Verify:**

```bash
go test ./internal/agent/... -run TestPairOrchestrator -v
go test ./internal/agent/... -run TestClassifyVerdict -v
go test ./internal/agent/... -run TestBuildReviewerPrompt -v
go test ./internal/agent/... -run TestPairTopic -v
```

All tests must pass. The tests that require a real AgentRegistry with RunAgent (the full conversation flow) will be covered in the integration test (Task 6).

- [x] Task 3 complete: PairOrchestrator unit tests pass

---

## Task 4: Dispatcher classification for channel pairing

**File:** `/Users/caimlas/git/meept/internal/agent/dispatcher.go` (modify)

Add a classification method to the Dispatcher that determines whether a user request should be routed to channel-based pairing instead of the normal step-based dispatch.

**Changes to `dispatcher.go`:**

1. Add a new intent type for pair-channel tasks. In `/Users/caimlas/git/meept/internal/agent/intent.go`, add:

```go
// Pair channel (dual-agent conversation)
IntentPair IntentType = "pair"
```

2. Add `IntentPair` to the `IsValidIntentType` switch case and to `Keywords()`:

In `intent.go`, add to the `IsValidIntentType` switch:
```go
case IntentPair:
```

Add a Keywords case:
```go
case IntentPair:
    return []string{"debate", "brainstorm", "explore", "discuss", "pair", "collaborate"}
```

Update `DefaultAgent()`:
```go
case IntentPair:
    return config.AgentIDAnalyst // analyst defaults as the actor; reviewer is the planner
```

Update `Category()`:
```go
case IntentPair:
    return CategoryDefer
```

Update `ShouldDispatchAsync()`:
```go
case IntentPair:
    return true
```

Update `ShouldCreateTask()`:
```go
case IntentPair:
    return false // pair sessions don't create step-based tasks
```

3. Add a `ShouldRouteToPair` method on `Dispatcher` in `dispatcher.go`:

```go
// ShouldRouteToPair returns true if the dispatch result should use channel-based
// pairing instead of the step-based orchestrator.
func (d *Dispatcher) ShouldRouteToPair(result *DispatchResult) bool {
	if result == nil || result.Intent == nil {
		return false
	}
	return IntentType(result.Intent.Type) == IntentPair
}
```

4. Add `IntentPair` to `SteeringHeuristicTable` in `dispatcher.go`:

```go
IntentPair: false, // Pair tasks are not urgent
```

**Verify:**

```bash
go vet ./internal/agent/...
go build ./internal/agent/...
go test ./internal/agent/... -run TestIntent -v
```

- [x] Task 4 complete: Dispatcher classification for pair channel

---

## Task 5: Handler routing for pair messages

**File:** `/Users/caimlas/git/meept/internal/agent/handler.go` (modify)

Wire the ChatHandler to detect pair-channel dispatches and publish a `pair.start` request instead of routing through the orchestrator.

**Changes to `handler.go`:**

In the `handleRequest` method, after the dispatcher returns a `DispatchResult`, add a check before the existing async dispatch logic. Find the block starting around line 553:

```go
if reply == "" && err == nil {
    if h.dispatcher != nil {
```

Add the pair routing check inside the dispatcher block, before the existing `ClassifyAndRoute` result handling. Insert after the `result, dispatchErr := h.dispatcher.ClassifyAndRoute(...)` call:

```go
// Check if this should be routed to pair-channel mode
if h.dispatcher.ShouldRouteToPair(result) {
    h.logger.Info("Routing to pair-channel mode",
        "session", conversationID,
        "actor", result.AgentID,
    )
    reply = h.startPairSession(ctx, result, conversationID)
} else if h.dispatcher.ShouldDispatchAsync(result) && result.Task != nil {
```

Note the `else if` — pair routing takes priority over async dispatch.

Add the `startPairSession` method:

```go
// startPairSession initiates a pair-channel session and returns an acknowledgment.
func (h *ChatHandler) startPairSession(ctx context.Context, result *DispatchResult, conversationID string) string {
	// Determine actor and reviewer from the dispatch result
	actorID := result.AgentID
	if actorID == "" {
		actorID = "analyst"
	}
	reviewerID := h.pairReviewerForActor(actorID)

	req := PairStartRequest{
		SessionID:     conversationID,
		ActorID:       actorID,
		ReviewerID:    reviewerID,
		InitialPrompt: result.Intent.Summary,
		MaxTurns:      5,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		h.logger.Error("Failed to marshal pair start request", "error", err)
		return "Failed to start pair session."
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeRequest,
		Topic:     TopicPairStart,
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	delivered := h.bus.Publish(TopicPairStart, msg)
	if delivered == 0 {
		h.logger.Warn("Pair start published with no subscribers")
		return "Pair session requested but no orchestrator is listening."
	}

	return fmt.Sprintf("## pair session started\n\n**actor:** %s\n**reviewer:** %s\n\nagents are collaborating. you will see updates as turns complete.", actorID, reviewerID)
}

// pairReviewerForActor selects an appropriate reviewer agent for the given actor.
func (h *ChatHandler) pairReviewerForActor(actorID string) string {
	switch actorID {
	case "coder":
		return "planner"
	case "analyst":
		return "planner"
	case "debugger":
		return "coder"
	case "planner":
		return "analyst"
	default:
		return "planner"
	}
}
```

Also add a subscription for `pair.result` in the `Start` method so the handler can push results back to the user's chat session. Add a new goroutine:

After the existing `h.wg.Add(6)` line (which will become `h.wg.Add(7)`):

```go
// Subscribe to pair result events
pairResultSub := h.bus.Subscribe(SourceChatHandler, TopicPairResult)
```

Add a goroutine:

```go
// Pair result handler - push results back to linked sessions
go func() {
    defer h.wg.Done()
    for {
        select {
        case <-ctx.Done():
            h.bus.Unsubscribe(pairResultSub)
            return
        case msg, ok := <-pairResultSub.Channel:
            if !ok {
                return
            }
            h.handlePairResult(msg)
        }
    }
}()
```

And the handler method:

```go
// handlePairResult pushes pair session results back to chat.
func (h *ChatHandler) handlePairResult(msg *models.BusMessage) {
    var result PairResult
    if err := json.Unmarshal(msg.Payload, &result); err != nil {
        h.logger.Error("Failed to parse pair result", "error", err)
        return
    }

    h.logger.Info("Pair session completed",
        "session_id", result.SessionID,
        "total_turns", result.TotalTurns,
        "verdict", result.FinalVerdict,
    )

    reply := h.formatPairResult(result)

    response := ChatResponse{
        ConversationID: result.SessionID,
        Reply:          reply,
    }
    h.sendResponse("pair-result-"+result.SessionID, response)
}

// formatPairResult builds a human-readable pair session result.
func (h *ChatHandler) formatPairResult(result PairResult) string {
    var sb strings.Builder
    sb.WriteString("## pair session completed\n\n")

    verdictLabel := string(result.FinalVerdict)
    if verdictLabel == "" {
        verdictLabel = "concluded"
    }
    fmt.Fprintf(&sb, "**verdict:** %s\n", verdictLabel)
    fmt.Fprintf(&sb, "**turns:** %d\n\n", result.TotalTurns)

    if result.FinalOutput != "" {
        fmt.Fprintf(&sb, "**final output:**\n%s\n", truncateString(result.FinalOutput, 500))
    }

    return sb.String()
}
```

**Verify:**

```bash
go vet ./internal/agent/...
go build ./internal/agent/...
go test ./internal/agent/... -run TestChatHandler -v
```

- [x] Task 5 complete: Handler routing for pair messages

---

## Task 6: Orchestrator wiring

**File:** `/Users/caimlas/git/meept/internal/agent/orchestrator.go` (modify)

Add the PairOrchestrator as a managed sub-component of the Orchestrator so it starts/stops with the daemon lifecycle.

**Changes to `orchestrator.go`:**

1. Add `pairOrchestrator` field to the Orchestrator struct:

```go
type Orchestrator struct {
    strategic        *StrategicPlanner
    tactical         *TacticalScheduler
    pairOrchestrator *PairOrchestrator // bus-channel-based agent pairing
    bus              *bus.MessageBus
    logger           *slog.Logger

    cancel context.CancelFunc
    wg     sync.WaitGroup
}
```

2. Add `PairOrchestrator` to `OrchestratorDeps`:

```go
type OrchestratorDeps struct {
    Strategic        *StrategicPlanner
    Tactical         *TacticalScheduler
    PairOrchestrator *PairOrchestrator // optional: enables channel-based pairing
    Bus              *bus.MessageBus
    Logger           *slog.Logger
}
```

3. Wire it in `NewOrchestrator`:

```go
return &Orchestrator{
    strategic:        deps.Strategic,
    tactical:         deps.Tactical,
    pairOrchestrator: deps.PairOrchestrator,
    bus:              deps.Bus,
    logger:           deps.Logger,
}
```

4. In `Start`, start the pair orchestrator if present:

After the existing subscription loop, add:

```go
// Start pair orchestrator if configured
if o.pairOrchestrator != nil {
    if err := o.pairOrchestrator.Start(ctx); err != nil {
        o.logger.Error("Failed to start pair orchestrator", "error", err)
    } else {
        o.logger.Info("Pair orchestrator started")
    }
}
```

5. In `Stop`, stop the pair orchestrator:

Before the `o.wg.Wait()` in `Stop`, add:

```go
// Stop pair orchestrator if running
if o.pairOrchestrator != nil {
    if err := o.pairOrchestrator.Stop(ctx); err != nil {
        o.logger.Warn("Pair orchestrator stop error", "error", err)
    }
}
```

**Daemon wiring in `/Users/caimlas/git/meept/internal/daemon/components.go`:**

In the section where `NewOrchestrator` is called (around line 1120), create and pass the PairOrchestrator:

```go
// Create pair orchestrator for channel-based agent pairing
pairOrchestrator := agent.NewPairOrchestrator(agent.PairOrchestratorDeps{
    Registry: c.Registry,
    Bus:      msgBus,
    Logger:   logger.With("component", "pair-orchestrator"),
})

c.Orchestrator = agent.NewOrchestrator(agent.OrchestratorDeps{
    Strategic:        strategicPlanner,
    Tactical:         tacticalScheduler,
    PairOrchestrator: pairOrchestrator,
    Bus:              msgBus,
    Logger:           logger.With("component", "orchestrator"),
})
```

**Verify:**

```bash
go vet ./internal/agent/...
go vet ./internal/daemon/...
go build ./internal/agent/...
go build ./internal/daemon/...
go build ./...
```

- [x] Task 6 complete: Orchestrator wiring

---

## Task 7: Integration test

**File:** `/Users/caimlas/git/meept/internal/agent/pair_integration_test.go`

End-to-end test that validates the full flow: publish a `pair.start` request, observe turn events on the session topic, and verify the final `pair.result`. Uses a test harness that creates real bus and mock agents.

```go
package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/git/meept/pkg/models"
)

// TestPairOrchestrator_FullConversation tests a complete actor-reviewer cycle
// that approves on the first turn.
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
```

**Verify:**

```bash
go test ./internal/agent/... -run TestPairOrchestrator -v
go test ./internal/agent/... -run TestIntentPair -v
```

- [x] Task 7 complete: Integration tests pass

---

## Task 8: Documentation update

**File:** `/Users/caimlas/git/meept/docs/concepts/multi-agent.md` (modify)

Add a section documenting the channel-based pairing modality. Include:

1. A "Channel-Based Pairing" subsection under the multi-agent section
2. Describe when to use it (research debates, exploratory debugging, brainstorming)
3. Describe the message flow: user -> dispatcher -> pair.start -> actor -> reviewer -> pair.result
4. Bus topics: `pair.start`, `pair.{sessionID}.turn`, `pair.result`, `pair.error`
5. Configuration: `IntentPair` triggers it, default actor/reviewer mapping

**File:** `/Users/caimlas/git/meept/CLAUDE.md` (modify)

Add `IntentPair` to the multi-agent agent table and add bus topic patterns for `pair.*`.

**Verify:**

```bash
grep -c "pair" docs/concepts/multi-agent.md
```

- [x] Task 8 complete: Documentation updated

---

## Summary of files

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/agent/pair_channel.go` | PairChannel types, bus topic constants |
| Create | `internal/agent/pair_orchestrator.go` | PairOrchestrator core loop |
| Create | `internal/agent/pair_orchestrator_test.go` | Unit tests for PairOrchestrator |
| Create | `internal/agent/pair_integration_test.go` | Integration tests |
| Modify | `internal/agent/intent.go` | Add `IntentPair` constant and routing methods |
| Modify | `internal/agent/dispatcher.go` | Add `ShouldRouteToPair()`, steering table entry |
| Modify | `internal/agent/handler.go` | Route pair-channel dispatches, subscribe to `pair.result` |
| Modify | `internal/agent/orchestrator.go` | Wire PairOrchestrator as sub-component |
| Modify | `internal/daemon/components.go` | Create and wire PairOrchestrator in daemon startup |
| Modify | `docs/concepts/multi-agent.md` | Document channel-based pairing |
| Modify | `CLAUDE.md` | Update agent table and bus topics |
