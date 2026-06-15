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
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

const (
	// DefaultPairMaxRounds is the default maximum number of actor->reviewer rounds.
	DefaultPairMaxRounds = 3

	// Actor conversation ID prefix
	actorConvPrefix = "pair-actor"

	// Reviewer conversation ID prefix
	reviewerConvPrefix = "pair-reviewer"
)

// PairManager drives the multi-round actor->reviewer loop for pair sessions.
// It holds active sessions in memory and publishes bus events on state changes.
type PairManager struct {
	mu       sync.RWMutex
	sessions map[string]*PairSession // session ID -> session

	registry  *AgentRegistry
	taskStore *task.Store
	stepStore *task.StepStore
	bus       *bus.MessageBus
	logger    *slog.Logger
}

// PairManagerConfig holds configuration for creating a PairManager.
type PairManagerConfig struct {
	Registry  *AgentRegistry
	TaskStore *task.Store
	StepStore *task.StepStore
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewPairManager creates a new pair manager.
func NewPairManager(cfg PairManagerConfig) *PairManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &PairManager{
		sessions:  make(map[string]*PairSession),
		registry:  cfg.Registry,
		taskStore: cfg.TaskStore,
		stepStore: cfg.StepStore,
		bus:       cfg.Bus,
		logger:    cfg.Logger,
	}
}

// CreateSession creates a new pair session for a task and registers it.
func (pm *PairManager) CreateSession(taskID, spec, actorID, reviewerID string, maxRounds int) *PairSession {
	if maxRounds <= 0 {
		maxRounds = DefaultPairMaxRounds
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	session := NewPairSession(taskID, spec, actorID, reviewerID, maxRounds)
	pm.sessions[session.ID] = session

	pm.logger.Info("Pair session created",
		"session_id", session.ID,
		KeyTaskID, taskID,
		"actor", actorID,
		"reviewer", reviewerID,
		"max_rounds", maxRounds,
	)

	return session
}

// GetSession returns a pair session by ID.
func (pm *PairManager) GetSession(sessionID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	s, ok := pm.sessions[sessionID]
	return s, ok
}

// GetSessionByTask returns the active pair session for a task, if any.
func (pm *PairManager) GetSessionByTask(taskID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, s := range pm.sessions {
		if s.TaskID == taskID && !s.State.IsTerminal() {
			return s, true
		}
	}
	return nil, false
}

// GetSessionByStep returns the pair session that owns the given step, if any.
func (pm *PairManager) GetSessionByStep(stepID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, s := range pm.sessions {
		if s.OwnsStep(stepID) {
			return s, true
		}
	}
	return nil, false
}

// RunRound executes one actor->reviewer round for the given session.
// It runs the actor agent, then the reviewer agent, and records the attempt.
// Returns the attempt and an error if the round could not complete.
func (pm *PairManager) RunRound(ctx context.Context, sessionID string) (*Attempt, error) {
	pm.mu.Lock()
	session, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return nil, fmt.Errorf("pair session not found: %s", sessionID)
	}
	if session.State.IsTerminal() {
		pm.mu.Unlock()
		return nil, fmt.Errorf("pair session is terminal: %s", session.State)
	}
	pm.mu.Unlock()

	round := session.CurrentRound()
	pm.logger.Info("Starting pair round",
		"session_id", sessionID,
		KeyTaskID, session.TaskID,
		"round", round,
		"max_rounds", session.MaxRounds,
	)

	// --- Actor phase ---
	actorPrompt := session.Context.ActorPrompt()
	actorConvID := fmt.Sprintf("%s-%s-r%d-%d", actorConvPrefix, session.TaskID, round, time.Now().UnixNano())

	startedAt := time.Now().UTC()
	actorOutput, err := pm.runAgent(ctx, session.ActorAgentID, actorPrompt, actorConvID)
	if err != nil {
		pm.logger.Error("Actor agent failed",
			"session_id", sessionID,
			"round", round,
			"error", err,
		)
		session.MarkFailed()
		pm.publishEvent("pair.round_failed", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"round":      round,
			"phase":      "actor",
			"error":      err.Error(),
		})
		return nil, fmt.Errorf("actor failed in round %d: %w", round, err)
	}

	// Check for context cancellation between actor and reviewer phases
	select {
	case <-ctx.Done():
		pm.logger.Warn("Context cancelled between actor and reviewer phases",
			"session_id", sessionID,
			"round", round,
		)
		session.MarkFailed()
		pm.publishEvent("pair.round_failed", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"round":      round,
			"phase":      "context_cancelled",
			"error":      ctx.Err().Error(),
		})
		return nil, fmt.Errorf("context cancelled after actor phase in round %d: %w", round, ctx.Err())
	default:
		// Context still valid, continue to reviewer phase
	}

	// --- Reviewer phase ---
	reviewerPrompt := session.Context.ReviewerPrompt(actorOutput)
	reviewerConvID := fmt.Sprintf("%s-%s-r%d-%d", reviewerConvPrefix, session.TaskID, round, time.Now().UnixNano())

	reviewOutput, err := pm.runAgent(ctx, session.ReviewerAgentID, reviewerPrompt, reviewerConvID)
	if err != nil {
		pm.logger.Error("Reviewer agent failed",
			"session_id", sessionID,
			"round", round,
			"error", err,
		)
		session.MarkFailed()
		pm.publishEvent("pair.round_failed", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"round":      round,
			"phase":      "reviewer",
			"error":      err.Error(),
		})
		return nil, fmt.Errorf("reviewer failed in round %d: %w", round, err)
	}

	// Parse review output into a ReviewResult
	reviewResult := pm.parseReviewOutput(reviewOutput)

	// Record the attempt
	attempt := &Attempt{
		Round:       round,
		ActorOutput: actorOutput,
		Review:      reviewResult,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
	}

	session.Context.RecordAttempt(attempt)

	// Update criteria based on review
	pm.updateCriteria(session, reviewResult)

	pm.logger.Info("Pair round completed",
		"session_id", sessionID,
		KeyTaskID, session.TaskID,
		"round", round,
		"review_status", string(reviewResult.Status),
		"pending", len(session.Context.PendingCriteria),
		"accepted", len(session.Context.AcceptedCriteria),
	)

	// Check convergence
	if session.Context.HasConverged() {
		session.MarkConverged()
		pm.publishEvent("pair.converged", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"rounds":     round,
		})
		pm.finalizeTask(ctx, session, true)
		return attempt, nil
	}

	// Check exhaustion
	if session.IsExhausted() {
		session.MarkExhausted()
		pm.publishEvent("pair.exhausted", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"rounds":     round,
			"max_rounds": session.MaxRounds,
		})
		pm.finalizeTask(ctx, session, false)
		return attempt, nil
	}

	// Round completed but more rounds needed
	pm.publishEvent("pair.round_completed", map[string]any{
		"session_id":        sessionID,
		KeyTaskID:           session.TaskID,
		"round":             round,
		"review_status":     string(reviewResult.Status),
		"pending_criteria":  len(session.Context.PendingCriteria),
		"accepted_criteria": len(session.Context.AcceptedCriteria),
		KeyChatVisible:      true,
	})

	return attempt, nil
}

// RunAllRounds runs the full loop until convergence, exhaustion, or error.
func (pm *PairManager) RunAllRounds(ctx context.Context, sessionID string) (*PairSession, error) {
	for {
		pm.mu.RLock()
		session, ok := pm.sessions[sessionID]
		if !ok {
			pm.mu.RUnlock()
			return nil, fmt.Errorf("pair session not found: %s", sessionID)
		}
		if session.State.IsTerminal() {
			pm.mu.RUnlock()
			return session, nil
		}
		pm.mu.RUnlock()

		_, err := pm.RunRound(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}
}

// runAgent executes a single agent loop iteration.
func (pm *PairManager) runAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	if pm.registry == nil {
		return "", fmt.Errorf("agent registry not configured")
	}
	return pm.registry.RunAgent(ctx, agentID, message, conversationID)
}

// parseReviewOutput converts raw reviewer output into a ReviewResult.
// If the output contains structured JSON it uses that; otherwise it
// heuristically determines the status from keywords.
func (pm *PairManager) parseReviewOutput(output string) *ReviewResult {
	// Try structured JSON parse
	result := &ReviewResult{}
	if err := parseReviewJSON(output, result); err == nil {
		return result
	}

	// Heuristic: check for approval keywords
	lower := toLower(output)
	if containsAny(lower, []string{"approved", "all requirements met", "looks good", "lgtm"}) {
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   output,
			Confidence: 0.8,
		}
	}

	// Default: rejected with the output as feedback
	issues := extractIssueLines(output)
	return &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   output,
		Issues:     issues,
		Confidence: 0.7,
	}
}

// updateCriteria moves criteria from pending to accepted if the review approved.
func (pm *PairManager) updateCriteria(session *PairSession, review *ReviewResult) {
	if review.Status != ReviewApproved {
		return
	}

	// On approval, all remaining pending criteria become accepted
	session.Context.AcceptedCriteria = append(
		session.Context.AcceptedCriteria,
		session.Context.PendingCriteria...,
	)
	session.Context.PendingCriteria = nil
}

// finalizeTask updates the parent task state based on session outcome.
func (pm *PairManager) finalizeTask(ctx context.Context, session *PairSession, success bool) {
	if pm.taskStore == nil {
		return
	}

	t, err := pm.taskStore.GetByID(session.TaskID)
	if err != nil || t == nil {
		pm.logger.Error("Failed to get task for finalization",
			KeyTaskID, session.TaskID,
			"error", err,
		)
		return
	}

	if success {
		t.SetState(task.StateCompleted)
		pm.logger.Info("Pair session task completed",
			"session_id", session.ID,
			KeyTaskID, session.TaskID,
		)
	} else {
		t.SetState(task.StateFailed)
		pm.logger.Warn("Pair session task failed (exhausted)",
			"session_id", session.ID,
			KeyTaskID, session.TaskID,
		)
	}

	if err := pm.taskStore.Update(t); err != nil {
		pm.logger.Error("Failed to update task after pair finalization",
			KeyTaskID, session.TaskID,
			"error", err,
		)
	}
}

// RemoveSession removes a completed session from the manager.
func (pm *PairManager) RemoveSession(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.sessions, sessionID)
}

// ActiveSessionCount returns the number of active (non-terminal) sessions.
func (pm *PairManager) ActiveSessionCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, s := range pm.sessions {
		if !s.State.IsTerminal() {
			count++
		}
	}
	return count
}

// ListSessions returns all sessions, optionally filtered by state.
func (pm *PairManager) ListSessions(activeOnly bool) []*PairSession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var result []*PairSession
	for _, s := range pm.sessions {
		if activeOnly && s.State.IsTerminal() {
			continue
		}
		result = append(result, s)
	}
	return result
}

// publishEvent publishes a bus event from the pair manager.
func (pm *PairManager) publishEvent(topic string, data map[string]any) {
	if pm.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "pair-manager", data)
	if err != nil {
		pm.logger.Error("Failed to create pair manager bus message", "error", err)
		return
	}

	pm.bus.Publish(topic, msg)
}

// parseReviewJSON attempts to unmarshal a JSON ReviewResult from output.
func parseReviewJSON(output string, result *ReviewResult) error {
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return fmt.Errorf("no JSON in review output")
	}

	// Try wrapping in a ReviewResult structure
	wrapped := struct {
		Status     string   `json:"status"`
		Feedback   string   `json:"feedback"`
		Issues     []string `json:"issues,omitempty"`
		Confidence float64  `json:"confidence"`
	}{}

	if err := jsonUnmarshalHelper([]byte(jsonStr), &wrapped); err != nil {
		return err
	}

	result.Status = ReviewStatus(wrapped.Status)
	result.Feedback = wrapped.Feedback
	result.Issues = wrapped.Issues
	result.Confidence = wrapped.Confidence
	return nil
}

// toLower is a simple wrapper for strings.ToLower.
func toLower(s string) string {
	return strings.ToLower(s)
}

// containsAny checks if s contains any of the given substrings.
func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractIssueLines extracts non-empty lines from review output as issues.
func extractIssueLines(output string) []string {
	lines := strings.Split(output, "\n")
	var issues []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			issues = append(issues, trimmed)
		}
	}
	if len(issues) > 5 {
		issues = issues[:5] // Cap at 5 issues
	}
	return issues
}

// jsonUnmarshalHelper wraps encoding/json.Unmarshal.
func jsonUnmarshalHelper(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
