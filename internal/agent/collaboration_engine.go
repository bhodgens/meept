package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// MaxCollaborationDepth is the default max nesting depth for agent-initiated collaboration.
const MaxCollaborationDepth = 1

// CollaborationEngineDeps holds dependencies.
type CollaborationEngineDeps struct {
	Bus         *bus.MessageBus
	Registry    *AgentRegistry
	Workspaces  *WorkspaceManager
	PairManager *PairManager
	Logger      *slog.Logger
}

// CollaborationEngine manages collaboration sessions and registered modes.
//
// Each mode implements the CollaborationMode interface (see collaboration.go)
// and is registered via RegisterMode. The engine dispatches RunSession calls
// to the appropriate driver based on the session's mode string.
//
// # Current Modes
//   - "pair_programming": Two agents alternate editor token in a shared workspace
//     (PairProgrammingDriver in collaboration_pair_driver.go).
//   - "differential": Four-phase A/B pipeline with independent branches and a
//     synthesizing differentiator (DifferentialDriver in collaboration_diff_driver.go).
//   - "team_parallel": N-agent parallel teams with lead agent synthesis
//     (ParallelTeamDriver in collaboration_team_driver.go).
//
// The ParallelTeamDriver uses message bus topics for N-agent coordination:
//   - team.{sessionID}.status   -- shared task board
//   - team.{sessionID}.message  -- inter-agent broadcast/targeted messages
//   - team.{sessionID}.result   -- partial results aggregation
//
// No changes to existing modes or the CollaborationMode interface are required.
// The CollaborationMode interface is intentionally minimal (Name, Run, CanInitiate)
// to support heterogeneous collaboration strategies.
//
// See docs/concepts/multi-agent-parallelism.md for the full design.
type CollaborationEngine struct {
	modes       map[string]CollaborationMode
	sessions    map[string]*CollaborationSession
	nestedCount map[string]int
	bus         *bus.MessageBus
	registry    *AgentRegistry
	workspaces  *WorkspaceManager
	pairMgr     *PairManager
	logger      *slog.Logger
	mu          sync.RWMutex
}

// NewCollaborationEngine creates a new collaboration engine.
func NewCollaborationEngine(deps CollaborationEngineDeps) *CollaborationEngine {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &CollaborationEngine{
		modes:       make(map[string]CollaborationMode),
		sessions:    make(map[string]*CollaborationSession),
		nestedCount: make(map[string]int),
		bus:         deps.Bus,
		registry:    deps.Registry,
		workspaces:  deps.Workspaces,
		pairMgr:     deps.PairManager,
		logger:      deps.Logger,
	}
}

// RegisterMode registers a collaboration mode.
func (e *CollaborationEngine) RegisterMode(name string, mode CollaborationMode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modes[name] = mode
	e.logger.Info("Registered collaboration mode", "name", name)
}

// GetMode returns a registered mode by name.
func (e *CollaborationEngine) GetMode(name string) (CollaborationMode, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m, ok := e.modes[name]
	return m, ok
}

// CreateSession creates a new collaboration session.
func (e *CollaborationEngine) CreateSession(mode, taskID string, participants []string, config SessionConfig) (*CollaborationSession, error) {
	sess := NewCollaborationSession(mode, taskID, participants, config)
	e.mu.Lock()
	e.sessions[sess.ID] = sess
	e.mu.Unlock()

	e.publishCollaborationEvent(TopicCollabSessionCreated, map[string]any{
		"session_id":   sess.ID,
		"mode":         mode,
		"participants": participants,
		"task_id":      taskID,
	})

	return sess, nil
}

// CreateNestedSession creates a nested collaboration session.
func (e *CollaborationEngine) CreateNestedSession(parentID, mode, taskDesc string, preferredAgents []string, config SessionConfig) (*CollaborationSession, error) {
	currentDepth := e.nestedDepth(parentID)
	if currentDepth >= MaxCollaborationDepth {
		return nil, ErrDepthExceeded
	}

	participants := e.resolveParticipants(mode, preferredAgents)
	if len(participants) < 2 {
		return nil, fmt.Errorf("could not resolve at least 2 participants for %s mode", mode)
	}

	sess := NewCollaborationSession(mode, taskDesc, participants, config)
	sess.ParentID = parentID

	e.mu.Lock()
	e.sessions[sess.ID] = sess
	e.nestedCount[sess.ID] = currentDepth + 1
	e.mu.Unlock()

	e.publishCollaborationEvent(TopicCollabRequested, map[string]any{
		"parent_session_id": parentID,
		"session_id":        sess.ID,
		"mode":              mode,
		"task_description":  taskDesc,
		"preferred_agents":  preferredAgents,
	})

	return sess, nil
}

// RunSession executes a collaboration session.
func (e *CollaborationEngine) RunSession(ctx context.Context, sessionID string) (*CollaborationResult, error) {
	e.mu.RLock()
	sess, ok := e.sessions[sessionID]
	if !ok {
		e.mu.RUnlock()
		return nil, NewCollaborationError(ErrCodeSessionNotFound, sessionID, "", "session not found")
	}
	mode, modeOk := e.modes[sess.Mode]
	e.mu.RUnlock()

	if !modeOk {
		return nil, NewCollaborationError(ErrCodeInvalidMode, sessionID, "", fmt.Sprintf("mode %s not registered", sess.Mode))
	}
	e.logger.Info("Running collaboration session",
		"session_id", sessionID,
		"mode", sess.Mode,
	)
	return mode.Run(ctx, sess)
}

// GetSession returns a session by ID.
func (e *CollaborationEngine) GetSession(id string) (*CollaborationSession, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.sessions[id]
	return s, ok
}

// HandleInitiatedCollaboration is the callback for the initiate_collaboration tool.
func (e *CollaborationEngine) HandleInitiatedCollaboration(ctx context.Context, mode, taskDesc, reason string, preferredAgents []string) (string, error) {
	taskID := taskDesc
	if len(taskID) > 50 {
		taskID = taskID[:50]
	}

	cfg := DefaultSessionConfig()
	sess, err := e.CreateNestedSession("agent-initiated", mode, taskID, preferredAgents, cfg)
	if err != nil {
		return "", err
	}

	result, err := e.RunSession(ctx, sess.ID)
	if err != nil {
		return "", err
	}

	e.logger.Info("Agent-initiated collaboration complete",
		"session_id", sess.ID,
		"state", result.State,
		"turns", result.TurnCount,
		"reason", reason,
	)

	return sess.ID, nil
}

// nestedDepth returns the nesting depth for a session.
func (e *CollaborationEngine) nestedDepth(sessionID string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.nestedCount[sessionID]
}

// resolveParticipants resolves agent IDs for a collaboration mode.
// When team_parallel mode is added, this switch should be extended to
// resolve a lead agent + specialist roster based on the mode's team config.
func (e *CollaborationEngine) resolveParticipants(mode string, preferred []string) []string {
	if len(preferred) >= 2 {
		return preferred
	}

	switch mode {
	case "pair_programming":
		return append(preferred, "coder", "planner")
	case "differential":
		return append(preferred, "coder", "planner", "analyst")
	case "team_parallel":
		// Team mode: lead (planner) + specialist roster (coder, analyst, debugger)
		if len(preferred) == 0 {
			preferred = []string{"planner"}
		}
		return append(preferred, "coder", "analyst", "debugger")
	default:
		return append(preferred, "coder", "planner")
	}
}

// ActiveSessionCount returns the number of active sessions.
func (e *CollaborationEngine) ActiveSessionCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, s := range e.sessions {
		if !s.State.IsTerminal() {
			count++
		}
	}
	return count
}

// ListSessions returns all sessions, optionally filtered.
func (e *CollaborationEngine) ListSessions(activeOnly bool) []*CollaborationSession {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var result []*CollaborationSession
	for _, s := range e.sessions {
		if activeOnly && s.State.IsTerminal() {
			continue
		}
		result = append(result, s)
	}
	return result
}

// publishCollaborationEvent publishes a bus message for a collaboration event.
func (e *CollaborationEngine) publishCollaborationEvent(topic string, data map[string]any) {
	if e.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "collaboration-engine", data)
	if err != nil {
		e.logger.Error("Failed to create collaboration bus message", "error", err)
		return
	}
	msg.Topic = topic
	e.bus.Publish(topic, msg)
}
