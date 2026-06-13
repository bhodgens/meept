package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	sessions map[string]*BusPairSessionState
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
		sessions: make(map[string]*BusPairSessionState),
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

// GetSession returns a snapshot of the state of an active pair session.
// Returns nil, false if the session is not found.
// The snapshot is mutex-free for safe concurrent access.
func (po *PairOrchestrator) GetSession(sessionID string) (*BusPairSessionStateSnapshot, bool) {
	po.mu.RLock()
	state, ok := po.sessions[sessionID]
	po.mu.RUnlock()
	if !ok {
		return nil, false
	}

	state.mu.RLock()
	defer state.mu.RUnlock()
	
	return &BusPairSessionStateSnapshot{
		SessionID:     state.SessionID,
		ActorID:       state.ActorID,
		ReviewerID:    state.ReviewerID,
		CurrentTurn:   state.CurrentTurn,
		MaxTurns:      state.MaxTurns,
		Phase:         state.Phase,
		LastVerdict:   state.LastVerdict,
		Turns:         append([]PairTurn(nil), state.Turns...),
		InitialPrompt: state.InitialPrompt,
	}, true
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

	state := &BusPairSessionState{
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
func (po *PairOrchestrator) runPairConversation(ctx context.Context, state *BusPairSessionState) {
	defer po.removeSession(state.SessionID)
	var ct int

	// Safely read initial config
	state.mu.RLock()
	actorPrompt := state.InitialPrompt
	maxTurns := state.MaxTurns
	agentID := state.ActorID
	reviewerID := state.ReviewerID
	sessionID := state.SessionID
	state.mu.RUnlock()

	ct = 0
	for {
		state.mu.RLock()
		ct = state.CurrentTurn
		state.mu.RUnlock()

		if ct >= maxTurns {
			break
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			state.mu.Lock()
			state.Phase = "failed"
			state.mu.Unlock()
			po.publishError(sessionID, "context cancelled")
			return
		default:
		}

		// --- Actor turn ---
		state.mu.Lock()
		state.Phase = "actor_turn"
		state.mu.Unlock()
		actorOutput, err := po.runAgent(ctx, agentID, actorPrompt, sessionID)
		if err != nil {
			state.mu.Lock()
			state.Phase = "failed"
			state.mu.Unlock()
			po.publishError(sessionID, fmt.Sprintf("actor %s failed: %v", agentID, err))
			return
		}

		state.mu.RLock()
		ct = state.CurrentTurn
		state.mu.RUnlock()
		actorTurn := PairTurn{
			SessionID:  sessionID,
			TurnNumber: ct,
			AgentID:    agentID,
			Role:       "actor",
			Content:    actorOutput,
		}
		state.mu.Lock()
		state.Turns = append(state.Turns, actorTurn)
		state.mu.Unlock()

		// Publish actor turn to the session topic for observability
		po.publishTurn(sessionID, &actorTurn)

		// --- Reviewer turn ---
		state.mu.Lock()
		state.Phase = "reviewer_turn"
		state.mu.Unlock()
		reviewerPrompt := po.buildReviewerPrompt(state, actorOutput)
		reviewerOutput, err := po.runAgent(ctx, reviewerID, reviewerPrompt, sessionID)
		if err != nil {
			state.mu.Lock()
			state.Phase = "failed"
			state.mu.Unlock()
			po.publishError(sessionID, fmt.Sprintf("reviewer %s failed: %v", reviewerID, err))
			return
		}

		// Classify the reviewer response
		verdict, feedback := po.classifyVerdict(reviewerOutput)
		state.mu.Lock()
		state.LastVerdict = verdict
		state.mu.Unlock()

		reviewerTurn := PairTurn{
			SessionID:  sessionID,
			TurnNumber: ct,
			AgentID:    reviewerID,
			Role:       "reviewer",
			Content:    reviewerOutput,
			Verdict:    verdict,
			Feedback:   feedback,
		}
		state.mu.Lock()
		state.Turns = append(state.Turns, reviewerTurn)
		state.mu.Unlock()

		// Publish reviewer turn to the session topic for observability
		po.publishTurn(sessionID, &reviewerTurn)

		// Check verdict
		if verdict == PairVerdictApproved {
			// Approved -- emit result and exit
			state.mu.RLock()
			resultTurns := make([]PairTurn, len(state.Turns))
			copy(resultTurns, state.Turns)
			state.mu.RUnlock()
			po.publishResult(&PairResult{
				SessionID:    sessionID,
				FinalOutput:  actorOutput,
				Turns:        resultTurns,
				TotalTurns:   ct + 1,
				FinalVerdict: PairVerdictApproved,
			})
			state.mu.Lock()
			state.Phase = "completed"
			state.mu.Unlock()
			return
		}

		// Rejected or needs more -- construct revised actor prompt
		actorPrompt = po.buildRevisionPrompt(state, actorOutput, feedback, reviewerOutput)
		state.mu.Lock()
		state.CurrentTurn++
		state.mu.Unlock()
	}

	// Reached max turns without approval -- emit result with last actor output
	state.mu.Lock()
	state.Phase = "completed"
	lastActorOutput := ""
	resultTurns := make([]PairTurn, len(state.Turns))
	copy(resultTurns, state.Turns)
	lastVerdict := state.LastVerdict
	for i := len(state.Turns) - 1; i >= 0; i-- {
		if state.Turns[i].Role == "actor" {
			lastActorOutput = state.Turns[i].Content
			break
		}
	}
	state.mu.Unlock()
	po.publishResult(&PairResult{
		SessionID:    sessionID,
		FinalOutput:  lastActorOutput,
		Turns:        resultTurns,
		TotalTurns:   ct,
		FinalVerdict: lastVerdict,
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
func (po *PairOrchestrator) buildReviewerPrompt(state *BusPairSessionState, actorOutput string) string {
	state.mu.RLock()
	initialPrompt := state.InitialPrompt
	turnsCopy := make([]PairTurn, len(state.Turns))
	copy(turnsCopy, state.Turns)
	state.mu.RUnlock()

	prompt := fmt.Sprintf(
		"Review the following output for the task: %s\n\n"+
			"Agent output to review:\n%s\n\n"+
			"Classify your response:\n"+
			"- If the output is satisfactory, start your response with 'APPROVED:' followed by a brief summary.\n"+
			"- If the output needs revision, start your response with 'REJECTED:' followed by specific feedback.\n"+
			"- If you need more information, start your response with 'NEEDS_MORE:' followed by your questions.",
		initialPrompt,
		actorOutput,
	)

	// Include history from previous turns for context
	if len(turnsCopy) > 1 {
		prompt += "\n\nPrevious conversation history:\n"
		for _, turn := range turnsCopy {
			prompt += fmt.Sprintf("\n[%s - %s]: %s\n", turn.Role, turn.AgentID, truncateString(turn.Content, 200))
		}
	}

	return prompt
}

// buildRevisionPrompt constructs the prompt for the actor after rejection.
func (po *PairOrchestrator) buildRevisionPrompt(state *BusPairSessionState, actorOutput, feedback, reviewerOutput string) string {
	state.mu.RLock()
	initialPrompt := state.InitialPrompt
	state.mu.RUnlock()
	return fmt.Sprintf(
		"Your previous output was rejected. Please revise based on the feedback.\n\n"+
			"Original task: %s\n\n"+
			"Your previous output:\n%s\n\n"+
			"Reviewer feedback:\n%s\n\n"+
			"Please provide a revised output that addresses the feedback.",
		initialPrompt,
		actorOutput,
		reviewerOutput,
	)
}

// classifyVerdict parses the reviewer output to determine the verdict.
func (po *PairOrchestrator) classifyVerdict(reviewerOutput string) (PairVerdict, string) {
	trimmed := strings.TrimSpace(reviewerOutput)

	if strings.HasPrefix(trimmed, "APPROVED:") {
		feedback := strings.TrimSpace(trimmed[9:])
		return PairVerdictApproved, feedback
	}
	if strings.HasPrefix(trimmed, "APPROVED") {
		return PairVerdictApproved, ""
	}
	if strings.HasPrefix(trimmed, "REJECTED:") {
		feedback := strings.TrimSpace(trimmed[9:])
		return PairVerdictRejected, feedback
	}
	if strings.HasPrefix(trimmed, "REJECTED") {
		feedback := strings.TrimSpace(trimmed[8:])
		return PairVerdictRejected, feedback
	}
	if strings.HasPrefix(trimmed, "NEEDS_MORE:") {
		feedback := strings.TrimSpace(trimmed[11:])
		return PairVerdictNeedsMore, feedback
	}
	if strings.HasPrefix(trimmed, "NEEDS_MORE") {
		feedback := strings.TrimSpace(trimmed[10:])
		return PairVerdictNeedsMore, feedback
	}

	// Default: treat as needs_more if no explicit verdict marker
	return PairVerdictNeedsMore, trimmed
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
