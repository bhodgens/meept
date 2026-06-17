package agent

import (
	"context"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// SessionState represents the lifecycle state of a collaboration session.
type SessionState string

const (
	SessionCreated   SessionState = "created"
	SessionActive    SessionState = "active"
	SessionConverged SessionState = "converged"
	SessionExhausted SessionState = "exhausted"
	SessionFailed    SessionState = "failed"
)

// IsTerminal returns true if the collaboration session is in a terminal state.
func (s SessionState) IsTerminal() bool {
	return s == SessionConverged || s == SessionExhausted || s == SessionFailed
}

// TurnEntry records a single turn in a collaboration session.
type TurnEntry struct {
	TurnNumber int       `json:"turn_number"`
	AgentID    string    `json:"agent_id"`
	Role       string    `json:"role"` // "driver" or "observer"
	Content    string    `json:"content"`
	Action     string    `json:"action,omitempty"` // "approve", "request_changes", "request_token", "yield"
	Feedback   string    `json:"feedback,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	TokensUsed int64     `json:"tokens_used,omitempty"`
}

// CollaborationSession represents an active collaboration instance.
type CollaborationSession struct {
	ID           string
	Mode         string // "pair_programming" | "differential"
	TaskID       string // parent task
	State        SessionState
	Workspace    string      // base workspace path
	Participants []string    // agent IDs involved
	TurnLog      []TurnEntry // complete turn history
	ParentID     string      // for nested (agent-initiated) sessions
	TokenBudget  int64
	TimeBudget   time.Duration
	TurnTimeout  time.Duration // max time per turn
	MaxTurns     int
	CreatedAt    time.Time
	UpdatedAt    time.Time

	mu sync.RWMutex
}

// CollaborationResult holds the outcome of a completed session.
type CollaborationResult struct {
	SessionID   string        `json:"session_id"`
	State       SessionState  `json:"state"`
	FinalOutput string        `json:"final_output,omitempty"`
	Workspace   string        `json:"workspace,omitempty"`
	TurnCount   int           `json:"turn_count"`
	Duration    time.Duration `json:"duration"`
}

// CollaborationMode is the interface for pluggable collaboration modes.
type CollaborationMode interface {
	Name() string
	Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error)
	CanInitiate(agentID string, reason string) bool
}

// Bus topic constants for collaboration events.
const (
	TopicCollabSessionCreated   = "collaboration.session_created"
	TopicCollabTurnCompleted    = "collaboration.turn_completed"
	TopicCollabPhaseCompleted   = "collaboration.phase_completed"
	TopicCollabConsensusReached = "collaboration.consensus_reached"
	TopicCollabDivergence       = "collaboration.divergence"
	TopicCollabResult           = "collaboration.result"
	TopicCollabError            = "collaboration.error"
	TopicCollabRequested        = "collaboration.requested"
)

// NewCollaborationSession creates a new collaboration session.
func NewCollaborationSession(mode, taskID string, participants []string, config SessionConfig) *CollaborationSession {
	now := time.Now().UTC()
	return &CollaborationSession{
		ID:           id.Generate("collab-") + "-" + taskID,
		Mode:         mode,
		TaskID:       taskID,
		State:        SessionCreated,
		Participants: participants,
		TurnLog:      []TurnEntry{},
		MaxTurns:     config.MaxTurns,
		TurnTimeout:  config.TurnTimeout,
		TokenBudget:  config.TokenBudget,
		TimeBudget:   config.TimeBudget,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// SessionConfig holds creation-time configuration.
type SessionConfig struct {
	MaxTurns    int
	TurnTimeout time.Duration
	TokenBudget int64
	TimeBudget  time.Duration
}

// DefaultSessionConfig returns sensible defaults.
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		MaxTurns:    10,
		TurnTimeout: 5 * time.Minute,
		TokenBudget: 50000,
		TimeBudget:  30 * time.Minute,
	}
}

// AddTurn appends a turn entry (thread-safe).
func (s *CollaborationSession) AddTurn(entry TurnEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.TurnNumber = len(s.TurnLog) + 1
	s.TurnLog = append(s.TurnLog, entry)
	s.UpdatedAt = time.Now().UTC()
}

// TurnCount returns the number of completed turns.
func (s *CollaborationSession) TurnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.TurnLog)
}

// GetState returns the current session state (thread-safe).
func (s *CollaborationSession) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// MarkActive transitions to active state.
func (s *CollaborationSession) MarkActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = SessionActive
	s.UpdatedAt = time.Now().UTC()
}

// MarkConverged transitions to converged state.
func (s *CollaborationSession) MarkConverged() {
	s.mu.Lock()
	s.State = SessionConverged
	s.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// MarkExhausted transitions to exhausted state.
func (s *CollaborationSession) MarkExhausted() {
	s.mu.Lock()
	s.State = SessionExhausted
	s.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// MarkFailed transitions to failed state.
func (s *CollaborationSession) MarkFailed() {
	s.mu.Lock()
	s.State = SessionFailed
	s.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// CopyTurnLog returns a snapshot copy of the turn log (thread-safe).
func (s *CollaborationSession) CopyTurnLog() []TurnEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TurnEntry, len(s.TurnLog))
	copy(out, s.TurnLog)
	return out
}

// TotalTokensUsed sums TokensUsed across all turns (thread-safe).
func (s *CollaborationSession) TotalTokensUsed() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	for _, turn := range s.TurnLog {
		total += turn.TokensUsed
	}
	return total
}

// LastContentByRole returns the content of the most recent turn with the
// given role, or "" if none exists (thread-safe).
func (s *CollaborationSession) LastContentByRole(role string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.TurnLog) - 1; i >= 0; i-- {
		if s.TurnLog[i].Role == role {
			return s.TurnLog[i].Content
		}
	}
	return ""
}
