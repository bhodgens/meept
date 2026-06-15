package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Bus topic constants for team coordination.
const (
	// TopicTeamStart is published when a team session is requested.
	TopicTeamStart = "team.start"
	// TopicTeamResult is published when a team session completes.
	TopicTeamResult = "team.result"
	// TopicTeamError is published when a team session fails.
	TopicTeamError = "team.error"
	// TopicTeamStatusPattern is the per-session shared task board topic.
	TopicTeamStatusPattern = "team.%s.status"
	// TopicTeamMessagePattern is the per-session inter-agent communication topic.
	TopicTeamMessagePattern = "team.%s.message"
	// TopicTeamResultPattern is the per-session partial results aggregation topic.
	TopicTeamResultPattern = "team.%s.result"
)

// TeamStatusPattern is the per-session shared task board topic.
func TeamStatusTopic(sessionID string) string {
	return fmt.Sprintf(TopicTeamStatusPattern, sessionID)
}

// TeamMessageTopic returns the per-session inter-agent communication topic.
func TeamMessageTopic(sessionID string) string {
	return fmt.Sprintf(TopicTeamMessagePattern, sessionID)
}

// TeamResultTopic returns the per-session partial results aggregation topic.
func TeamResultTopic(sessionID string) string {
	return fmt.Sprintf(TopicTeamResultPattern, sessionID)
}

// MemberStatus represents the execution status of a team member.
type MemberStatus string

const (
	MemberPending MemberStatus = "pending"
	MemberRunning MemberStatus = "running"
	MemberDone    MemberStatus = "done"
	MemberFailed  MemberStatus = "failed"
)

// TeamMemberResult holds the outcome of a single team member's subtask.
type TeamMemberResult struct {
	AgentID  string        `json:"agent_id"`
	Subtask  string        `json:"subtask"`
	Output   string        `json:"output,omitempty"`
	Status   MemberStatus  `json:"status"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// TeamConfig holds configuration for a parallel team session.
type TeamConfig struct {
	LeadAgent     string   `json:"lead_agent"`
	Roster        []string `json:"roster"`
	MaxConcurrent int      `json:"max_concurrent"`
	MemberTimeout time.Duration `json:"member_timeout"`
}

// TeamStatus holds the overall team state and per-member results.
type TeamStatus struct {
	SessionID     string                       `json:"session_id"`
	LeadAgent     string                       `json:"lead_agent"`
	Phase         string                       `json:"phase"` // "fan_out", "collect", "synthesize", "completed", "failed"
	MemberResults map[string]*TeamMemberResult `json:"member_results"`
}

// ParallelTeamDriver implements CollaborationMode for N-agent parallel team
// execution with a lead agent that synthesizes partial results.
type ParallelTeamDriver struct {
	registry   *AgentRegistry
	workspace  *WorkspaceManager
	pairMgr    *PairManager
	bus        *bus.MessageBus
	logger     *slog.Logger

	conversations map[string]*TeamStatus
	convMu        sync.RWMutex
}

// ParallelTeamDriverDeps holds dependencies.
type ParallelTeamDriverDeps struct {
	Registry   *AgentRegistry
	Workspace *WorkspaceManager
	PairManager *PairManager
	Bus        *bus.MessageBus
	Logger     *slog.Logger
}

// NewParallelTeamDriver creates a new parallel team driver.
func NewParallelTeamDriver(deps ParallelTeamDriverDeps) *ParallelTeamDriver {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &ParallelTeamDriver{
		registry:      deps.Registry,
		workspace:     deps.Workspace,
		pairMgr:       deps.PairManager,
		bus:           deps.Bus,
		logger:        deps.Logger,
		conversations: make(map[string]*TeamStatus),
	}
}

// Name returns the mode name.
func (d *ParallelTeamDriver) Name() string { return "team_parallel" }

// CanInitiate returns true if the reason mentions team, multi, or parallel.
func (d *ParallelTeamDriver) CanInitiate(_ string, reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "team") ||
		strings.Contains(lower, "multi") ||
		strings.Contains(lower, "parallel")
}

// Run executes the parallel team collaboration pipeline:
//  1. Parse team configuration from session metadata
//  2. Fan out subtasks to roster agents concurrently
//  3. Collect results via errgroup
//  4. Lead agent synthesizes final result from partial results
//  5. Return CollaborationResult with aggregated output
func (d *ParallelTeamDriver) Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error) {
	if len(sess.Participants) < 2 {
		return nil, NewCollaborationError(ErrCodeInvalidMode, sess.ID, "init",
			"team_parallel requires at least 2 participants (lead + 1 specialist)")
	}

	teamCfg := d.parseTeamConfig(sess)
	if teamCfg.LeadAgent == "" {
		teamCfg.LeadAgent = sess.Participants[0]
	}
	if len(teamCfg.Roster) == 0 && len(sess.Participants) > 1 {
		teamCfg.Roster = sess.Participants[1:]
	}
	if teamCfg.MaxConcurrent <= 0 {
		teamCfg.MaxConcurrent = len(teamCfg.Roster)
	}
	if teamCfg.MemberTimeout <= 0 {
		teamCfg.MemberTimeout = sess.TurnTimeout
		if teamCfg.MemberTimeout <= 0 {
			teamCfg.MemberTimeout = 5 * time.Minute
		}
	}

	d.logger.Info("Starting parallel team session",
		"session_id", sess.ID,
		"task_id", sess.TaskID,
		"lead_agent", teamCfg.LeadAgent,
		"roster", teamCfg.Roster,
		"max_concurrent", teamCfg.MaxConcurrent,
	)

	// Create workspace
	workspacePath, err := d.createWorkspace(ctx, sess)
	if err != nil {
		return nil, NewCollaborationError(ErrCodeWorkspace, sess.ID, "init",
			fmt.Sprintf("workspace creation failed: %v", err))
	}
	sess.Workspace = workspacePath

	// Track team state
	teamStatus := &TeamStatus{
		SessionID:     sess.ID,
		LeadAgent:     teamCfg.LeadAgent,
		Phase:         "fan_out",
		MemberResults: make(map[string]*TeamMemberResult),
	}
	for _, member := range teamCfg.Roster {
		teamStatus.MemberResults[member] = &TeamMemberResult{
			AgentID: member,
			Status:  MemberPending,
		}
	}
	d.convMu.Lock()
	d.conversations[sess.ID] = teamStatus
	d.convMu.Unlock()
	defer d.cleanupSession(sess.ID)

	sess.MarkActive()
	d.publishEvent(sess.ID, TopicCollabSessionCreated, map[string]any{
		"session_id":   sess.ID,
		"mode":         "team_parallel",
		"participants": sess.Participants,
		"task_id":      sess.TaskID,
		"lead_agent":   teamCfg.LeadAgent,
		"roster":       teamCfg.Roster,
	})

	// Enforce time budget via derived context
	runCtx := ctx
	if sess.TimeBudget > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, sess.TimeBudget)
		defer cancel()
	}

	startTime := time.Now()

	// Phase 1: Fan out subtasks to roster agents concurrently
	resultsMap, err := d.fanOut(runCtx, sess, teamCfg)
	if err != nil {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabError, map[string]any{
			"session_id": sess.ID,
			"error":      err.Error(),
			"phase":      "fan_out",
		})
		return nil, err
	}

	// Update team status with collected results
	teamStatus.Phase = "collect"
	for memberID, result := range resultsMap {
		teamStatus.MemberResults[memberID] = result
	}

	d.publishTeamStatus(sess.ID, teamStatus)

	// Phase 2: Lead agent synthesizes final result from partial results
	teamStatus.Phase = "synthesize"
	synthesizedOutput, err := d.synthesize(runCtx, sess, teamCfg, resultsMap)
	if err != nil {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabError, map[string]any{
			"session_id": sess.ID,
			"error":      err.Error(),
			"phase":      "synthesize",
		})
		return nil, fmt.Errorf("lead agent synthesis failed: %w", err)
	}

	// Publish partial results to per-session topic
	d.publishPartialResults(sess.ID, resultsMap)

	sess.MarkConverged()
	duration := time.Since(startTime)
	teamStatus.Phase = "completed"

	d.publishEvent(sess.ID, TopicCollabResult, map[string]any{
		"session_id":  sess.ID,
		"state":       string(SessionConverged),
		"turn_count":  sess.TurnCount(),
		"workspace":   workspacePath,
		"duration_ms": duration.Milliseconds(),
		"lead_agent":  teamCfg.LeadAgent,
		"members":     len(teamCfg.Roster),
	})

	return &CollaborationResult{
		SessionID:   sess.ID,
		State:       SessionConverged,
		FinalOutput: synthesizedOutput,
		Workspace:   workspacePath,
		TurnCount:   sess.TurnCount(),
		Duration:    duration,
	}, nil
}

// fanOut runs all roster agents concurrently, bounded by MaxConcurrent.
// Each agent receives a subtask assignment and works independently.
func (d *ParallelTeamDriver) fanOut(ctx context.Context, sess *CollaborationSession, cfg TeamConfig) (map[string]*TeamMemberResult, error) {
	results := make(map[string]*TeamMemberResult)
	var resultsMu sync.Mutex

	// Use semaphore to limit concurrency
	sem := make(chan struct{}, cfg.MaxConcurrent)
	var eg errgroup.Group

	for _, memberID := range cfg.Roster {
		memberID := memberID // capture loop variable

		eg.Go(func() error {
			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return fmt.Errorf("fan out cancelled before assigning %s: %w", memberID, ctx.Err())
			}

			// Per-member timeout
			memberCtx, cancel := context.WithTimeout(ctx, cfg.MemberTimeout)
			defer cancel()

			// Mark member as running
			d.updateMemberStatus(sess.ID, memberID, MemberRunning, "")

			// Build subtask prompt for this member
			subtaskPrompt := d.buildMemberPrompt(sess, memberID, cfg)

			// Run the agent
			output, err := d.runAgent(memberCtx, memberID, subtaskPrompt,
				fmt.Sprintf("team-%s-%s", sess.ID, memberID))
			if err != nil {
				d.updateMemberStatus(sess.ID, memberID, MemberFailed, err.Error())

				resultsMu.Lock()
				results[memberID] = &TeamMemberResult{
					AgentID: memberID,
					Status:  MemberFailed,
					Error:   err.Error(),
				}
				resultsMu.Unlock()

				// Log but don't abort the entire team on individual failure
				d.logger.Warn("Team member failed",
					"session_id", sess.ID,
					"member", memberID,
					"error", err,
				)
				return nil // errgroup: don't return error, collect partial results
			}

			// Record successful result
			resultsMu.Lock()
			results[memberID] = &TeamMemberResult{
				AgentID: memberID,
				Subtask: sess.TaskID,
				Output:  output,
				Status:  MemberDone,
			}
			resultsMu.Unlock()

			d.updateMemberStatus(sess.ID, memberID, MemberDone, "")
			d.publishMemberCompleted(sess.ID, memberID, cfg.LeadAgent)

			// Record a turn entry for the session
			sess.AddTurn(TurnEntry{
				AgentID:   memberID,
				Role:      "member",
				Content:   output,
				Action:    "complete",
				Timestamp: time.Now().UTC(),
			})

			return nil
		})
	}

	// Wait for all members to finish
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("team fan-out failed: %w", err)
	}

	return results, nil
}

// synthesize sends all partial results to the lead agent for aggregation.
func (d *ParallelTeamDriver) synthesize(ctx context.Context, sess *CollaborationSession, cfg TeamConfig, results map[string]*TeamMemberResult) (string, error) {
	prompt := d.buildLeadSynthesisPrompt(sess, cfg, results)

	output, err := d.runAgent(ctx, cfg.LeadAgent, prompt,
		fmt.Sprintf("team-%s-%s-synthesis", sess.ID, cfg.LeadAgent))
	if err != nil {
		return "", fmt.Errorf("lead agent %s synthesis failed: %w", cfg.LeadAgent, err)
	}

	// Record the lead agent's turn
	sess.AddTurn(TurnEntry{
		AgentID:   cfg.LeadAgent,
		Role:      "lead",
		Content:   output,
		Action:    "synthesize",
		Timestamp: time.Now().UTC(),
	})

	return output, nil
}

// buildMemberPrompt constructs the subtask prompt for a specialist agent.
func (d *ParallelTeamDriver) buildMemberPrompt(sess *CollaborationSession, memberID string, cfg TeamConfig) string {
	prompt := fmt.Sprintf("## Team Member Task Assignment\n\n")
	prompt += fmt.Sprintf("**Session:** %s\n", sess.ID)
	prompt += fmt.Sprintf("**Lead Agent:** %s\n", cfg.LeadAgent)
	prompt += fmt.Sprintf("**Your Role:** Specialist team member (%s)\n\n", memberID)
	prompt += fmt.Sprintf("## Task\n\n%s\n\n", sess.TaskID)
	prompt += "## Instructions\n"
	prompt += "- You are a specialist on a parallel team working on this task.\n"
	prompt += "- Work independently on your portion of the task.\n"
	prompt += "- Provide a complete, focused output addressing your area of expertise.\n"
	prompt += "- Use tools to read files, write code, execute commands as needed.\n"
	prompt += "- Your output will be aggregated with outputs from other team members by the lead agent.\n"
	prompt += "- Focus on quality and completeness in your specific domain.\n"
	return prompt
}

// buildLeadSynthesisPrompt constructs the prompt for the lead agent to aggregate partial results.
func (d *ParallelTeamDriver) buildLeadSynthesisPrompt(sess *CollaborationSession, cfg TeamConfig, results map[string]*TeamMemberResult) string {
	prompt := fmt.Sprintf("## Team Lead: Synthesize Partial Results\n\n")
	prompt += fmt.Sprintf("**Session:** %s\n", sess.ID)
	prompt += fmt.Sprintf("**Your Role:** Lead agent (%s) - aggregate and synthesize\n\n", cfg.LeadAgent)
	prompt += fmt.Sprintf("## Original Task\n\n%s\n\n", sess.TaskID)
	prompt += fmt.Sprintf("## Team Member Results (%d members)\n\n", len(cfg.Roster))

	for _, memberID := range cfg.Roster {
		result, ok := results[memberID]
		if !ok {
			prompt += fmt.Sprintf("### %s: NO RESULT\n\n", memberID)
			continue
		}

		switch result.Status {
		case MemberDone:
			prompt += fmt.Sprintf("### %s: COMPLETED\n\n", memberID)
			prompt += fmt.Sprintf("%s\n\n", truncateString(result.Output, 2000))
		case MemberFailed:
			prompt += fmt.Sprintf("### %s: FAILED\n\n", memberID)
			prompt += fmt.Sprintf("Error: %s\n\n", result.Error)
		default:
			prompt += fmt.Sprintf("### %s: %s\n\n", memberID, result.Status)
		}
	}

	prompt += "## Your Task as Lead\n"
	prompt += "1. Review all partial results from the team members.\n"
	prompt += "2. Identify areas of agreement and disagreement.\n"
	prompt += "3. Resolve any conflicts between member outputs.\n"
	prompt += "4. Synthesize a unified, coherent final output.\n"
	prompt += "5. The final output should be the best combination of all contributions.\n"

	return prompt
}

// parseTeamConfig extracts TeamConfig from session metadata or participants.
func (d *ParallelTeamDriver) parseTeamConfig(sess *CollaborationSession) TeamConfig {
	cfg := TeamConfig{
		Roster:        []string{},
		MaxConcurrent: len(sess.Participants),
		MemberTimeout: sess.TurnTimeout,
	}

	if len(sess.Participants) > 0 {
		cfg.LeadAgent = sess.Participants[0]
	}
	if len(sess.Participants) > 1 {
		cfg.Roster = make([]string, len(sess.Participants)-1)
		copy(cfg.Roster, sess.Participants[1:])
	}

	return cfg
}

func (d *ParallelTeamDriver) createWorkspace(ctx context.Context, sess *CollaborationSession) (string, error) {
	if d.workspace != nil {
		return d.workspace.Create(ctx, sess.ID,
			fmt.Sprintf("Parallel team session for %s", sess.TaskID))
	}

	baseDir := getCollabWorkspaceBase()
	wsPath := filepath.Join(baseDir, "team-"+sess.ID)
	if err := os.MkdirAll(wsPath, 0755); err != nil {
		return "", NewCollaborationError(ErrCodeWorkspace, sess.ID, "init",
			fmt.Sprintf("failed to create team workspace: %v", err))
	}
	return wsPath, nil
}

func (d *ParallelTeamDriver) runAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	if d.registry == nil {
		return "", NewCollaborationError(ErrCodeAgentFailed, "", "run_agent",
			"agent registry not configured")
	}
	return d.registry.RunAgent(ctx, agentID, message, conversationID)
}

func (d *ParallelTeamDriver) updateMemberStatus(sessionID, memberID string, status MemberStatus, errMsg string) {
	d.convMu.Lock()
	defer d.convMu.Unlock()
	ts, ok := d.conversations[sessionID]
	if !ok {
		return
	}

	if result, exists := ts.MemberResults[memberID]; exists {
		result.Status = status
		if errMsg != "" {
			result.Error = errMsg
		}
	}
}

func (d *ParallelTeamDriver) cleanupSession(sessionID string) {
	d.convMu.Lock()
	delete(d.conversations, sessionID)
	d.convMu.Unlock()
}

// publishEvent publishes a collaboration event to the message bus.
func (d *ParallelTeamDriver) publishEvent(sessionID, topic string, data map[string]any) {
	if d.bus == nil {
		return
	}
	data["session_id"] = sessionID
	data["timestamp"] = time.Now().UTC()
	d.logger.Debug("team collaboration event", "topic", topic, "data", data)

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "parallel-team-driver", data)
	if err != nil {
		d.logger.Error("failed to create team bus message", "error", err)
		return
	}
	msg.Topic = topic
	d.bus.Publish(topic, msg)
}

// publishTeamStatus publishes the shared task board for a team session.
func (d *ParallelTeamDriver) publishTeamStatus(sessionID string, status *TeamStatus) {
	if d.bus == nil {
		return
	}

	topic := TeamStatusTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeStatusUpdate, "parallel-team-driver", status)
	if err != nil {
		d.logger.Error("failed to marshal team status", "error", err)
		return
	}
	msg.Topic = topic
	d.bus.Publish(topic, msg)
	d.logger.Debug("published team status", "session_id", sessionID, "phase", status.Phase)
}

// publishMemberCompleted publishes a member completion event.
func (d *ParallelTeamDriver) publishMemberCompleted(sessionID, memberID, leadAgent string) {
	if d.bus == nil {
		return
	}

	topic := TeamMessageTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "parallel-team-driver", map[string]any{
		"session_id":  sessionID,
		"member_id":    memberID,
		"lead_agent":   leadAgent,
		"event":        "member_completed",
		"timestamp":    time.Now().UTC(),
	})
	if err != nil {
		d.logger.Error("failed to create member completed message", "error", err)
		return
	}
	msg.Topic = topic
	d.bus.Publish(topic, msg)
}

// publishPartialResults publishes all partial results to the per-session result topic.
func (d *ParallelTeamDriver) publishPartialResults(sessionID string, results map[string]*TeamMemberResult) {
	if d.bus == nil {
		return
	}

	topic := TeamResultTopic(sessionID)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "parallel-team-driver", map[string]any{
		"session_id": sessionID,
		"event":       "partial_results",
		"results":     results,
		"timestamp":   time.Now().UTC(),
	})
	if err != nil {
		d.logger.Error("failed to create partial results message", "error", err)
		return
	}
	msg.Topic = topic
	d.bus.Publish(topic, msg)
	d.logger.Info("published team partial results",
		"session_id", sessionID,
		"members", len(results),
	)
}
