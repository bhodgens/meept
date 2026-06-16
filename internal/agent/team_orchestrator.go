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
	// teamDefaultMaxMembers is the default maximum number of team members.
	teamDefaultMaxMembers = 8
)

// TeamStartRequest initiates a new team parallel session.
type TeamStartRequest struct {
	// SessionID is the unique team session identifier.
	SessionID string `json:"session_id"`
	// TaskID is the parent task ID.
	TaskID string `json:"task_id"`
	// LeadAgent is the lead agent that synthesizes results.
	LeadAgent string `json:"lead_agent"`
	// Roster is the list of specialist agent IDs.
	Roster []string `json:"roster"`
	// TaskDescription is the task to be distributed to team members.
	TaskDescription string `json:"task_description"`
	// MaxConcurrent limits parallel execution (0 = all at once).
	MaxConcurrent int `json:"max_concurrent,omitempty"`
	// MemberTimeout is per-member timeout (0 = default from session config).
	MemberTimeout time.Duration `json:"member_timeout,omitempty"`
}

// TeamSessionState holds the runtime state of an active team session.
type TeamSessionState struct {
	SessionID     string                       `json:"session_id"`
	TaskID        string                       `json:"task_id"`
	LeadAgent     string                       `json:"lead_agent"`
	Roster        []string                     `json:"roster"`
	Phase         string                       `json:"phase"` // "running", "synthesizing", "completed", "failed"
	MemberResults map[string]*TeamMemberResult `json:"member_results,omitempty"`
	FinalOutput   string                       `json:"final_output,omitempty"`
	StartTime     time.Time                    `json:"start_time"`
}

// SubtaskAssignment holds a subtask assignment for a team member.
type SubtaskAssignment struct {
	AgentID  string
	Subtask  string
	Priority string
}

// TeamBroadcastMessage holds a message to send within a team.
type TeamBroadcastMessage struct {
	Content     string
	TargetAgent string // empty for broadcast
	MessageType string
}

// TeamMemberResultSubmission holds a partial result submission from a team member.
type TeamMemberResultSubmission struct {
	AgentID   string
	Output    string
	Status    string
	Artifacts []string
}

// TeamOrchestrator manages team lifecycle via bus subscriptions.
// It subscribes to team.start to initiate teams, runs the ParallelTeamDriver,
// and publishes results to team.result and errors to team.error.
type TeamOrchestrator struct {
	collabEngine *CollaborationEngine
	bus          *bus.MessageBus
	logger       *slog.Logger

	// Active teams indexed by sessionID
	teams sync.Map // sessionID -> *TeamSessionState

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// TeamOrchestratorDeps holds dependencies for creating a TeamOrchestrator.
type TeamOrchestratorDeps struct {
	CollabEngine *CollaborationEngine
	Bus          *bus.MessageBus
	Logger       *slog.Logger
}

// NewTeamOrchestrator creates a new TeamOrchestrator.
func NewTeamOrchestrator(deps TeamOrchestratorDeps) *TeamOrchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &TeamOrchestrator{
		collabEngine: deps.CollabEngine,
		bus:          deps.Bus,
		logger:       deps.Logger,
	}
}

// Start subscribes to team.start and begins processing team requests.
func (to *TeamOrchestrator) Start(ctx context.Context) error {
	ctx, to.cancel = context.WithCancel(ctx)

	sub := to.bus.Subscribe("team-orchestrator", TopicTeamStart)
	to.wg.Add(1)
	go to.runSubscription(ctx, sub, to.handleTeamStart)

	to.logger.Info("TeamOrchestrator started")
	return nil
}

// Stop gracefully stops the orchestrator.
func (to *TeamOrchestrator) Stop(ctx context.Context) error {
	if to.cancel != nil {
		to.cancel()
	}
	done := make(chan struct{})
	go func() {
		to.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		to.logger.Info("TeamOrchestrator stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name.
func (to *TeamOrchestrator) Name() string {
	return "team-orchestrator"
}

// GetTeam returns the state of an active team session (nil if not found).
func (to *TeamOrchestrator) GetTeam(sessionID string) *TeamSessionState {
	val, ok := to.teams.Load(sessionID)
	if !ok {
		return nil
	}
	return val.(*TeamSessionState)
}

// ActiveTeamCount returns the number of active team sessions.
func (to *TeamOrchestrator) ActiveTeamCount() int {
	count := 0
	to.teams.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (to *TeamOrchestrator) runSubscription(ctx context.Context, sub *bus.Subscriber, handler func(context.Context, *models.BusMessage)) {
	defer to.wg.Done()
	for {
		select {
		case <-ctx.Done():
			to.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			handler(ctx, msg)
		}
	}
}

// handleTeamStart processes a team.start bus message.
func (to *TeamOrchestrator) handleTeamStart(ctx context.Context, msg *models.BusMessage) {
	var req TeamStartRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		to.logger.Error("Failed to parse team start request", "error", err)
		to.publishError("", "invalid team start request: "+err.Error())
		return
	}

	if req.SessionID == "" || req.LeadAgent == "" || len(req.Roster) == 0 || req.TaskDescription == "" {
		to.publishError(req.SessionID, "team start request missing required fields (session_id, lead_agent, roster, task_description)")
		return
	}

	if len(req.Roster) > teamDefaultMaxMembers {
		originalCount := len(req.Roster)
		req.Roster = req.Roster[:teamDefaultMaxMembers]
		to.logger.Warn("Roster truncated to max members",
			"session_id", req.SessionID,
			"original_count", originalCount,
			"max", teamDefaultMaxMembers,
		)
	}

	// Create team state
	state := &TeamSessionState{
		SessionID:     req.SessionID,
		TaskID:        req.TaskID,
		LeadAgent:     req.LeadAgent,
		Roster:        req.Roster,
		Phase:         "running",
		StartTime:     time.Now().UTC(),
		MemberResults: make(map[string]*TeamMemberResult),
	}
	for _, member := range req.Roster {
		state.MemberResults[member] = &TeamMemberResult{
			AgentID: member,
			Status:  MemberPending,
		}
	}

	to.teams.Store(req.SessionID, state)

	to.logger.Info("Team session started",
		"session_id", req.SessionID,
		KeyTaskID, req.TaskID,
		"lead", req.LeadAgent,
		"roster", req.Roster,
		"max_concurrent", req.MaxConcurrent,
	)

	// Build participants list: lead first, then roster
	participants := make([]string, 0, len(req.Roster)+1)
	participants = append(participants, req.LeadAgent)
	participants = append(participants, req.Roster...)

	// Create session config from request params
	cfg := DefaultSessionConfig()
	if req.MemberTimeout > 0 {
		cfg.TurnTimeout = req.MemberTimeout
	}

	// If we have a collaboration engine, use it to create and run a team_parallel session
	if to.collabEngine != nil {
		mode, _ := to.collabEngine.GetMode("team_parallel")
		if mode == nil {
			to.logger.Error("team_parallel mode not registered in collaboration engine")
			to.publishError(req.SessionID, "team_parallel mode not registered")
			to.teams.Delete(req.SessionID)
			return
		}

		sess, err := to.collabEngine.CreateSession("team_parallel", req.TaskDescription, participants, cfg)
		if err != nil {
			to.logger.Error("Failed to create collaboration session", "error", err)
			to.publishError(req.SessionID, fmt.Sprintf("failed to create session: %v", err))
			to.teams.Delete(req.SessionID)
			return
		}

		// Run the team session in a background goroutine
		to.wg.Add(1)
		go func() {
			defer to.wg.Done()
			defer to.teams.Delete(req.SessionID)

			result, err := to.collabEngine.RunSession(ctx, sess.ID)
			if err != nil {
				state.Phase = "failed"
				to.publishError(req.SessionID, fmt.Sprintf("team session failed: %v", err))
				return
			}

			state.Phase = "completed"
			state.FinalOutput = result.FinalOutput

			to.publishResult(state)
		}()
	} else {
		// No collaboration engine available -- log and publish error
		to.logger.Error("No collaboration engine configured for team orchestrator")
		to.publishError(req.SessionID, "no collaboration engine configured")
		to.teams.Delete(req.SessionID)
	}
}

// publishResult publishes the final team result.
func (to *TeamOrchestrator) publishResult(state *TeamSessionState) {
	payload, err := json.Marshal(state)
	if err != nil {
		to.logger.Error("Failed to marshal team result", "error", err)
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     TopicTeamResult,
		Source:    "team-orchestrator",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	delivered := to.bus.Publish(TopicTeamResult, msg)
	to.logger.Info("Published team result",
		"session_id", state.SessionID,
		"phase", state.Phase,
		"lead", state.LeadAgent,
		"members", len(state.Roster),
		"delivered", delivered,
	)
}

// AssignSubtask assigns a subtask to a specific team member. The assignment
// is published to the per-session message topic so the member's running
// agent loop can pick it up, and the team state is updated.
func (to *TeamOrchestrator) AssignSubtask(ctx context.Context, sessionID string, assignment SubtaskAssignment) error {
	val, ok := to.teams.Load(sessionID)
	if !ok {
		return fmt.Errorf("team %q not found", sessionID)
	}
	state := val.(*TeamSessionState)

	// Verify agent is on the roster
	found := false
	for _, m := range state.Roster {
		if m == assignment.AgentID {
			found = true
			break
		}
	}
	if !found && assignment.AgentID != state.LeadAgent {
		return fmt.Errorf("agent %q is not a member of team %q", assignment.AgentID, sessionID)
	}

	// Update member result with the assigned subtask
	if mr, exists := state.MemberResults[assignment.AgentID]; exists {
		mr.Subtask = assignment.Subtask
		mr.Status = MemberPending
	}

	// Publish assignment to per-session message topic
	topic := TeamMessageTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "team-orchestrator", map[string]any{
		"session_id": sessionID,
		"event":      "subtask_assigned",
		"agent_id":   assignment.AgentID,
		"subtask":    assignment.Subtask,
		"priority":   assignment.Priority,
		"timestamp":  time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("failed to create assignment message: %w", err)
	}
	msg.Topic = topic
	to.bus.Publish(topic, msg)

	to.logger.Info("Subtask assigned to team member",
		"session_id", sessionID,
		"agent_id", assignment.AgentID,
		"subtask", assignment.Subtask,
		"priority", assignment.Priority,
	)
	return nil
}

// Status returns the current status of a team session.
func (to *TeamOrchestrator) Status(ctx context.Context, sessionID string) (*TeamSessionState, error) {
	val, ok := to.teams.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("team %q not found", sessionID)
	}
	return val.(*TeamSessionState), nil
}

// BroadcastMessage sends a message to team members via the per-session
// message topic. If targetAgent is empty, the message is a broadcast.
func (to *TeamOrchestrator) BroadcastMessage(ctx context.Context, sessionID string, tm TeamBroadcastMessage) error {
	val, ok := to.teams.Load(sessionID)
	if !ok {
		return fmt.Errorf("team %q not found", sessionID)
	}
	state := val.(*TeamSessionState)

	// Validate target agent if specified
	if tm.TargetAgent != "" {
		found := false
		for _, m := range state.Roster {
			if m == tm.TargetAgent {
				found = true
				break
			}
		}
		if !found && tm.TargetAgent != state.LeadAgent {
			return fmt.Errorf("agent %q is not a member of team %q", tm.TargetAgent, sessionID)
		}
	}

	topic := TeamMessageTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "team-orchestrator", map[string]any{
		"session_id":   sessionID,
		"event":        "team_message",
		"content":      tm.Content,
		"target_agent": tm.TargetAgent,
		"message_type": tm.MessageType,
		"timestamp":    time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("failed to create team message: %w", err)
	}
	msg.Topic = topic
	to.bus.Publish(topic, msg)

	target := "all members"
	if tm.TargetAgent != "" {
		target = tm.TargetAgent
	}
	to.logger.Debug("Team message sent",
		"session_id", sessionID,
		"target", target,
		"message_type", tm.MessageType,
	)
	return nil
}

// ReceiveResult records a partial result from a team member and publishes
// it to the per-session result topic.
func (to *TeamOrchestrator) ReceiveResult(ctx context.Context, sessionID string, result TeamMemberResultSubmission) error {
	val, ok := to.teams.Load(sessionID)
	if !ok {
		return fmt.Errorf("team %q not found", sessionID)
	}
	state := val.(*TeamSessionState)

	// Verify agent is on the roster
	found := false
	for _, m := range state.Roster {
		if m == result.AgentID {
			found = true
			break
		}
	}
	if !found && result.AgentID != state.LeadAgent {
		return fmt.Errorf("agent %q is not a member of team %q", result.AgentID, sessionID)
	}

	// Update team state with the member's result
	status := MemberDone
	if result.Status == "failed" {
		status = MemberFailed
	} else if result.Status == "partial" {
		status = MemberRunning
	}

	if mr, exists := state.MemberResults[result.AgentID]; exists {
		mr.Output = result.Output
		mr.Status = status
	} else {
		state.MemberResults[result.AgentID] = &TeamMemberResult{
			AgentID: result.AgentID,
			Output:  result.Output,
			Status:  status,
		}
	}

	// Publish partial result to per-session result topic
	topic := TeamResultTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "team-orchestrator", map[string]any{
		"session_id": sessionID,
		"event":      "member_result",
		"agent_id":   result.AgentID,
		"output":     result.Output,
		"status":     string(status),
		"artifacts":  result.Artifacts,
		"timestamp":  time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("failed to create result message: %w", err)
	}
	msg.Topic = topic
	to.bus.Publish(topic, msg)

	to.logger.Info("Team member result received",
		"session_id", sessionID,
		"agent_id", result.AgentID,
		"status", string(status),
	)
	return nil
}

// publishError publishes a team error event.
func (to *TeamOrchestrator) publishError(sessionID, errMsg string) {
	errPayload, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
		"error":      errMsg,
	})

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeError,
		Topic:     TopicTeamError,
		Source:    "team-orchestrator",
		Timestamp: time.Now().UTC(),
		Payload:   errPayload,
	}
	to.bus.Publish(TopicTeamError, msg)
}
