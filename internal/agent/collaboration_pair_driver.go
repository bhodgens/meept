package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// PairProgrammingDriver runs a symmetric peer collaboration session where two agents
// share a workspace and take turns holding the editor token.
type PairProgrammingDriver struct {
	registry  *AgentRegistry
	workspace *WorkspaceManager
	bus       *bus.MessageBus
	logger    *slog.Logger

	conversations map[string]*PPConversation
	convMu        sync.RWMutex
}

// PPConversation holds the shared state for a pair programming session.
type PPConversation struct {
	SessionID   string
	Workspace   string
	LastDiff    string
	TurnManager *TurnManager
	Converged   bool
}

// PairProgrammingDriverDeps holds dependencies.
type PairProgrammingDriverDeps struct {
	Registry  *AgentRegistry
	Workspace *WorkspaceManager
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewPairProgrammingDriver creates a new pair programming driver.
func NewPairProgrammingDriver(deps PairProgrammingDriverDeps) *PairProgrammingDriver {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &PairProgrammingDriver{
		registry:      deps.Registry,
		workspace:     deps.Workspace,
		bus:           deps.Bus,
		logger:        deps.Logger,
		conversations: make(map[string]*PPConversation),
	}
}

func (d *PairProgrammingDriver) Name() string { return "pair_programming" }

func (d *PairProgrammingDriver) CanInitiate(agentID string, reason string) bool {
	return true
}

func (d *PairProgrammingDriver) Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error) {
	if len(sess.Participants) < 2 {
		return nil, NewCollaborationError(ErrCodeInvalidMode, sess.ID, "init", "pair_programming requires at least 2 participants")
	}

	d.logger.Info("Starting pair programming session",
		"session_id", sess.ID,
		"task_id", sess.TaskID,
		"participants", sess.Participants,
	)

	workspacePath, err := d.createWorkspace(ctx, sess)
	if err != nil {
		return nil, NewCollaborationError(ErrCodeWorkspace, sess.ID, "init", fmt.Sprintf("workspace creation failed: %v", err))
	}
	sess.Workspace = workspacePath

	tm := NewTurnManager(sess.Participants, sess.MaxTurns, 8192, sess.TurnTimeout)
	conv := &PPConversation{
		SessionID:   sess.ID,
		Workspace:   workspacePath,
		TurnManager: tm,
	}
	d.convMu.Lock()
	d.conversations[sess.ID] = conv
	d.convMu.Unlock()
	defer d.cleanupSession(sess.ID)

	sess.MarkActive()
	d.publishEvent(sess.ID, TopicCollabSessionCreated, map[string]any{
		"session_id":   sess.ID,
		"mode":         "pair_programming",
		"participants": sess.Participants,
		"task_id":      sess.TaskID,
	})

	// Enforce time budget via derived context
	runCtx := ctx
	if sess.TimeBudget > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, sess.TimeBudget)
		defer cancel()
	}

	startTime := time.Now()
	result, err := d.runTurnLoop(runCtx, sess, conv, tm)
	duration := time.Since(startTime)

	if err != nil {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabError, map[string]any{
			"session_id": sess.ID,
			"error":      err.Error(),
			"phase":      "runTurnLoop",
		})
		return nil, err
	}

	result.Duration = duration
	d.publishEvent(sess.ID, TopicCollabResult, map[string]any{
		"session_id":  sess.ID,
		"state":       string(sess.GetState()),
		"turn_count":  result.TurnCount,
		"workspace":   workspacePath,
		"duration_ms": duration.Milliseconds(),
	})

	return result, nil
}

func (d *PairProgrammingDriver) createWorkspace(ctx context.Context, sess *CollaborationSession) (string, error) {
	if d.workspace != nil {
		return d.workspace.Create(ctx, sess.ID, fmt.Sprintf("Pair programming session for %s", sess.TaskID))
	}
	baseDir := getCollabWorkspaceBase()
	wsPath := filepath.Join(baseDir, sess.ID)
	if err := os.MkdirAll(wsPath, 0755); err != nil {
		return "", NewCollaborationError(ErrCodeWorkspace, sess.ID, "init", fmt.Sprintf("failed to create workspace: %v", err))
	}
	return wsPath, nil
}

func getCollabWorkspaceBase() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meept", "workspaces")
}

func (d *PairProgrammingDriver) runTurnLoop(ctx context.Context, sess *CollaborationSession, conv *PPConversation, tm *TurnManager) (*CollaborationResult, error) {
	for !tm.IsExhausted() {
		// Check context cancellation (covers time budget timeout)
		select {
		case <-ctx.Done():
			sess.MarkFailed()
			return nil, NewCollaborationError(ErrCodeCollabTimeout, sess.ID, "turn_loop", ctx.Err().Error())
		default:
		}

		// Check token budget
		if sess.TokenBudget > 0 {
			if sess.TotalTokensUsed() >= sess.TokenBudget {
				sess.MarkExhausted()
				return nil, ErrBudgetExceeded
			}
		}

		driverID := tm.TokenHolder()
		observerID := d.getOtherParticipant(sess.Participants, driverID)

		driverPrompt := d.buildDriverPrompt(sess, conv, driverID, observerID)
		output, err := d.runAgent(ctx, driverID, driverPrompt, fmt.Sprintf("pp-%s-%s-driven", sess.ID, driverID))
		if err != nil {
			sess.MarkFailed()
			return nil, NewCollaborationError(ErrCodeAgentFailed, sess.ID, "turn_loop", fmt.Sprintf("driver %s failed: %v", driverID, err))
		}

		sess.AddTurn(TurnEntry{
			AgentID:   driverID,
			Role:      "driver",
			Content:   output,
			Action:    string(TurnYield),
			Timestamp: time.Now().UTC(),
		})

		d.publishEvent(sess.ID, TopicCollabTurnCompleted, map[string]any{
			"session_id":  sess.ID,
			"agent_id":    driverID,
			"turn_number": tm.CurrentTurn(),
			"action":      "yield",
		})

		d.commitWorkspace(ctx, sess.ID, fmt.Sprintf("Turn %d: %s driver changes", tm.CurrentTurn(), driverID))

		observerPrompt := d.buildObserverPrompt(sess, conv, observerID, driverID, output)
		observerOutput, err := d.runAgent(ctx, observerID, observerPrompt, fmt.Sprintf("pp-%s-%s-observed", sess.ID, observerID))
		if err != nil {
			sess.MarkFailed()
			return nil, NewCollaborationError(ErrCodeAgentFailed, sess.ID, "turn_loop", fmt.Sprintf("observer %s failed: %v", observerID, err))
		}

		action, feedback := d.parseObserverResponse(observerOutput)

		sess.AddTurn(TurnEntry{
			AgentID:   observerID,
			Role:      "observer",
			Content:   observerOutput,
			Action:    action,
			Feedback:  feedback,
			Timestamp: time.Now().UTC(),
		})

		d.publishEvent(sess.ID, TopicCollabTurnCompleted, map[string]any{
			"session_id":  sess.ID,
			"agent_id":    observerID,
			"turn_number": tm.CurrentTurn(),
			"action":      action,
		})

		if action == "approve" {
			conv.Converged = true
			sess.MarkConverged()
			d.publishEvent(sess.ID, TopicCollabConsensusReached, map[string]any{
				"session_id":   sess.ID,
				"turns":        sess.TurnCount(),
				"participants": sess.Participants,
			})
			return &CollaborationResult{
				SessionID:   sess.ID,
				State:       SessionConverged,
				FinalOutput: output,
				Workspace:   sess.Workspace,
				TurnCount:   sess.TurnCount(),
			}, nil
		}

		switch action {
		case "request_token":
			if _, err := tm.RequestToken(observerID); err != nil {
				d.logger.Warn("token request failed, continuing", "session_id", sess.ID, "error", err)
			}
		case "approve", "request_changes":
			if err := tm.Yield(); err != nil {
				d.logger.Warn("yield failed", "session_id", sess.ID, "error", err)
			}
		}
	}

	sess.MarkExhausted()
	lastDriverOutput := sess.LastContentByRole("driver")
	return &CollaborationResult{
		SessionID:   sess.ID,
		State:       SessionExhausted,
		FinalOutput: lastDriverOutput,
		Workspace:   sess.Workspace,
		TurnCount:   sess.TurnCount(),
	}, nil
}

func (d *PairProgrammingDriver) buildDriverPrompt(sess *CollaborationSession, conv *PPConversation, driverID, observerID string) string {
	prompt := "## You are the CURRENT DRIVER in a pair programming session\n\n"
	prompt += fmt.Sprintf("**Session:** %s\n", sess.ID)
	prompt += "**Your role:** Driver (you have the editor token)\n"
	prompt += fmt.Sprintf("**Observer:** %s\n\n", observerID)
	prompt += fmt.Sprintf("## Task\n\n%s\n\n", sess.TaskID)

	turnLog := sess.CopyTurnLog()
	if len(turnLog) > 0 {
		prompt += "## Conversation History\n\n"
		for _, turn := range turnLog {
			prompt += fmt.Sprintf("**%s (%s):** %s\n\n", turn.AgentID, turn.Role, truncateString(turn.Content, 1000))
		}
	}

	if conv.LastDiff != "" {
		prompt += fmt.Sprintf("## Changes since your last turn\n\n```diff\n%s\n```\n\n", conv.LastDiff)
	}

	prompt += "## Instructions\n"
	prompt += "- You are the active driver. Write code, run tests, make changes.\n"
	prompt += "- Use tools to read files, write files, execute shell commands.\n"
	prompt += "- When done, call `workspace_yield` with action 'approve' to pass the turn.\n"
	prompt += "- If you want to hand off driving, call `workspace_yield` with action 'request_token'.\n"

	return prompt
}

func (d *PairProgrammingDriver) buildObserverPrompt(sess *CollaborationSession, conv *PPConversation, observerID, driverID, driverOutput string) string {
	prompt := "## You are the OBSERVER in a pair programming session\n\n"
	prompt += fmt.Sprintf("**Session:** %s\n", sess.ID)
	prompt += fmt.Sprintf("**Driver:** %s\n", driverID)
	prompt += "**Your role:** Observer (review and provide feedback)\n\n"
	prompt += fmt.Sprintf("## Task\n\n%s\n\n", sess.TaskID)

	turnLog := sess.CopyTurnLog()
	if len(turnLog) > 0 {
		prompt += "## Conversation History\n\n"
		for _, turn := range turnLog {
			prompt += fmt.Sprintf("**%s (%s):** %s\n\n", turn.AgentID, turn.Role, truncateString(turn.Content, 1000))
		}
	}

	prompt += fmt.Sprintf("## Driver's latest output\n\n%s\n\n", driverOutput)

	if conv.LastDiff != "" {
		prompt += fmt.Sprintf("## Recent changes (diff)\n\n```diff\n%s\n```\n\n", conv.LastDiff)
	}

	prompt += "## Instructions\n"
	prompt += "- Review the driver's work.\n"
	prompt += "- Respond with a JSON block containing your action and feedback:\n"
	prompt += "  ```json\n  {\"action\": \"approve\", \"feedback\": \"your review notes\"}\n  ```\n"
	prompt += "- Valid actions:\n"
	prompt += "  **approve**: Looks good, pass turn back to driver.\n"
	prompt += "  **request_changes**: Point out issues for the driver to fix.\n"
	prompt += "  **request_token**: Ask to become the driver yourself.\n"

	return prompt
}

func (d *PairProgrammingDriver) parseObserverResponse(output string) (action, feedback string) {
	// Try structured JSON parsing first
	if action, feedback, ok := parseStructuredObserverResponse(output); ok {
		return action, feedback
	}
	// Fallback to keyword matching
	lower := toLower(output)
	if containsAny(lower, []string{"request_token", "let me drive", "i want to be driver"}) {
		return "request_token", output
	}
	if containsAny(lower, []string{"approve", "looks good", "lgtm", "approved"}) {
		return "approve", output
	}
	return "request_changes", output
}

// parseStructuredObserverResponse attempts to extract structured action/feedback from LLM output.
// The observer is prompted to include a JSON block like: {"action":"approve","feedback":"looks good"}
// Returns (action, feedback, true) if structured output found, ("", "", false) otherwise.
func parseStructuredObserverResponse(output string) (string, string, bool) {
	actionIdx := strings.LastIndex(output, `"action"`)
	if actionIdx == -1 {
		return "", "", false
	}

	// Scan backward from "action" to find the matching opening brace,
	// skipping balanced braces encountered along the way.  This avoids
	// picking up stray "{" from markdown code fences or nested objects
	// that happen to contain the literal string "action" in them.
	jsonStart := -1
	depth := 0
	for i := actionIdx - 1; i >= 0; i-- {
		switch output[i] {
		case '}':
			depth++
		case '{':
			if depth == 0 {
				jsonStart = i
			} else {
				depth--
			}
		}
		if jsonStart >= 0 {
			break
		}
	}
	if jsonStart == -1 {
		return "", "", false
	}

	// Find the closing brace that matches the opening brace.
	rest := output[jsonStart:]
	braceDepth := 0
	jsonEnd := -1
	for i := range rest {
		switch rest[i] {
		case '{':
			braceDepth++
		case '}':
			braceDepth--
			if braceDepth == 0 {
				jsonEnd = i + 1
			}
		}
		if jsonEnd > 0 {
			break
		}
	}
	if jsonEnd == -1 {
		return "", "", false
	}

	jsonStr := output[jsonStart : jsonStart+jsonEnd]

	var parsed struct {
		Action   string `json:"action"`
		Feedback string `json:"feedback"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return "", "", false
	}

	switch parsed.Action {
	case "approve", "request_changes", "request_token":
		if parsed.Feedback == "" {
			parsed.Feedback = output
		}
		return parsed.Action, parsed.Feedback, true
	default:
		return "", "", false
	}
}

func (d *PairProgrammingDriver) runAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	if d.registry == nil {
		return "", NewCollaborationError(ErrCodeAgentFailed, "", "run_agent", "agent registry not configured")
	}
	return d.registry.RunAgent(ctx, agentID, message, conversationID)
}

func (d *PairProgrammingDriver) getOtherParticipant(participants []string, current string) string {
	for _, p := range participants {
		if p != current {
			return p
		}
	}
	return ""
}

func (d *PairProgrammingDriver) commitWorkspace(ctx context.Context, sessionID, message string) {
	if d.workspace == nil {
		return
	}
	if err := d.workspace.Commit(ctx, sessionID, message, nil); err != nil {
		d.logger.Warn("workspace commit failed", "session_id", sessionID, "error", err)
	}
}

func (d *PairProgrammingDriver) cleanupSession(sessionID string) {
	d.convMu.Lock()
	delete(d.conversations, sessionID)
	d.convMu.Unlock()
}

func (d *PairProgrammingDriver) publishEvent(sessionID, topic string, data map[string]any) {
	if d.bus == nil {
		return
	}
	data["session_id"] = sessionID
	data["timestamp"] = time.Now().UTC()
	d.logger.Debug("collaboration event", "topic", topic, "data", data)

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "pair-programming-driver", data)
	if err != nil {
		d.logger.Error("failed to create bus message", "error", err)
		return
	}
	d.bus.Publish(topic, msg)
}
