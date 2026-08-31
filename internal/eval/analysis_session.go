package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/pkg/id"
)

// SessionState represents the lifecycle state of an analysis session.
type SessionState int

const (
	SessionActive    SessionState = iota // Session is accepting new turns
	SessionPaused                        // Session is temporarily paused
	SessionCompleted                     // Session is closed and read-only
	SessionExported                      // Session has been exported
)

func (s SessionState) String() string {
	switch s {
	case SessionActive:
		return "active"
	case SessionPaused:
		return "paused"
	case SessionCompleted:
		return "completed"
	case SessionExported:
		return "exported"
	default:
		return "unknown"
	}
}

// ConversationTurn records a single turn in the analysis conversation.
type ConversationTurn struct {
	TurnID             string    `json:"turn_id"`
	Timestamp          time.Time `json:"timestamp"`
	UserQuery          string    `json:"user_query"`
	AnalystResponse    string    `json:"analyst_response"`
	ReferencedTraceIDs []string  `json:"referenced_trace_ids,omitempty"`
	ReferencedSpanIDs  []string  `json:"referenced_span_ids,omitempty"`
	FollowUpQuestions  []string  `json:"follow_up_questions,omitempty"`
}

// AnalysisSession manages a multi-turn conversation about trace analysis results.
type AnalysisSession struct {
	SessionID    string              `json:"session_id"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	TraceIDs     []string            `json:"trace_ids"`
	FailureModes []agent.FailureMode `json:"failure_modes,omitempty"`
	Turns        []ConversationTurn  `json:"turns"`
	State        SessionState        `json:"state"`
	Metadata     map[string]string   `json:"metadata,omitempty"`
}

// NewSession creates a new analysis session for the given traces and failure modes.
func NewSession(traceIDs []string, failureModes []agent.FailureMode) *AnalysisSession {
	now := time.Now()
	return &AnalysisSession{
		SessionID:    id.Generate("sess-"),
		CreatedAt:    now,
		UpdatedAt:    now,
		TraceIDs:     traceIDs,
		FailureModes: failureModes,
		Turns:        make([]ConversationTurn, 0),
		State:        SessionActive,
		Metadata:     make(map[string]string),
	}
}

// AddTurn appends a conversation turn to the session.
// Returns nil if the session is not active.
func (s *AnalysisSession) AddTurn(query, response string, traceRefs, spanRefs []string) *ConversationTurn {
	if s.State != SessionActive {
		return nil
	}

	now := time.Now()
	turn := ConversationTurn{
		TurnID:             id.Generate("turn-"),
		Timestamp:          now,
		UserQuery:          query,
		AnalystResponse:    response,
		ReferencedTraceIDs: traceRefs,
		ReferencedSpanIDs:  spanRefs,
	}

	// Generate follow-up suggestions from the last response.
	turn.FollowUpQuestions = generateFollowUps(query, response, s.FailureModes)

	s.Turns = append(s.Turns, turn)
	s.UpdatedAt = now

	return &s.Turns[len(s.Turns)-1]
}

// GetFollowUpSuggestions returns suggested follow-up questions based on the last turn.
func (s *AnalysisSession) GetFollowUpSuggestions() []string {
	if len(s.Turns) == 0 {
		return nil
	}
	last := s.Turns[len(s.Turns)-1]
	return last.FollowUpQuestions
}

// Close marks the session as completed. No more turns can be added.
func (s *AnalysisSession) Close() {
	if s.State == SessionActive || s.State == SessionPaused {
		s.State = SessionCompleted
		s.UpdatedAt = time.Now()
	}
}

// Pause marks the session as paused.
func (s *AnalysisSession) Pause() {
	if s.State == SessionActive {
		s.State = SessionPaused
		s.UpdatedAt = time.Now()
	}
}

// Resume marks the session as active again.
func (s *AnalysisSession) Resume() {
	if s.State == SessionPaused {
		s.State = SessionActive
		s.UpdatedAt = time.Now()
	}
}

// ExportJSON serializes the full session as JSON for audit trail purposes.
func (s *AnalysisSession) ExportJSON() ([]byte, error) {
	exp := struct {
		SessionID    string              `json:"session_id"`
		CreatedAt    time.Time           `json:"created_at"`
		UpdatedAt    time.Time           `json:"updated_at"`
		TraceIDs     []string            `json:"trace_ids"`
		FailureModes []agent.FailureMode `json:"failure_modes,omitempty"`
		Turns        []ConversationTurn  `json:"turns"`
		State        string              `json:"state"`
		Metadata     map[string]string   `json:"metadata,omitempty"`
	}{
		SessionID:    s.SessionID,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		TraceIDs:     s.TraceIDs,
		FailureModes: s.FailureModes,
		Turns:        s.Turns,
		State:        s.State.String(),
		Metadata:     s.Metadata,
	}

	return json.MarshalIndent(exp, "", "  ")
}

// SetMetadata sets a metadata key-value pair on the session.
func (s *AnalysisSession) SetMetadata(key, value string) {
	s.Metadata[key] = value
	s.UpdatedAt = time.Now()
}

// LastTurn returns the most recent turn, or nil if there are none.
func (s *AnalysisSession) LastTurn() *ConversationTurn {
	if len(s.Turns) == 0 {
		return nil
	}
	return &s.Turns[len(s.Turns)-1]
}

// TurnCount returns the number of turns in the session.
func (s *AnalysisSession) TurnCount() int {
	return len(s.Turns)
}

// generateFollowUps produces suggested follow-up questions based on the context.
func generateFollowUps(query string, response string, failureModes []agent.FailureMode) []string {
	var suggestions []string

	// If there are failure modes, suggest deepening into the highest severity.
	for _, fm := range failureModes {
		if fm.Severity == "critical" || fm.Severity == "high" {
			suggestions = append(suggestions,
				fmt.Sprintf("Deepen analysis on %s failure mode: %s", fm.Category, fm.Description))
			break // Just the top one
		}
	}

	// Fallback generic suggestions when no high-severity mode found.
	if len(suggestions) == 0 && len(response) > 0 {
		suggestions = []string{
			"Deepen analysis on the identified issues",
			"Compare findings across related traces",
			"Summarize actionable recommendations",
		}
	}

	return suggestions
}

// -----------------------------------------------------------------------
// AnalysisSessionManager
// -----------------------------------------------------------------------

// AnalysisSessionManager creates, stores, and retrieves analysis sessions.
type AnalysisSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*AnalysisSession
	basePath string
}

// NewAnalysisSessionManager creates a new session manager.
// basePath is the directory used for file-based exports.
func NewAnalysisSessionManager(basePath string) *AnalysisSessionManager {
	return &AnalysisSessionManager{
		sessions: make(map[string]*AnalysisSession),
		basePath: basePath,
	}
}

// CreateSession creates a new analysis session and registers it with this manager.
func (m *AnalysisSessionManager) CreateSession(traceIDs []string, failureModes []agent.FailureMode) *AnalysisSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := NewSession(traceIDs, failureModes)
	m.sessions[session.SessionID] = session

	return session
}

// GetSession returns the session with the given ID, or nil if not found.
func (m *AnalysisSessionManager) GetSession(sessionID string) *AnalysisSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.sessions[sessionID]
}

// ListSessions returns all sessions, sorted by creation time (newest first).
func (m *AnalysisSessionManager) ListSessions() []*AnalysisSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*AnalysisSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}

	// Sort by CreatedAt descending.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].CreatedAt.After(result[i].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// DeleteSession removes the session with the given ID. Returns an error if not found.
func (m *AnalysisSessionManager) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[sessionID]; !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	delete(m.sessions, sessionID)
	return nil
}

// ExportSession writes the session JSON to the given path using basePath as a guide.
func (m *AnalysisSessionManager) ExportSession(sessionID, filename string) error {
	session := m.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	data, err := session.ExportJSON()
	if err != nil {
		return fmt.Errorf("export session: %w", err)
	}

	dir := m.basePath
	if dir == "" {
		dir = "."
	}

	path := filepath.Join(dir, filename)
	return os.WriteFile(path, data, 0o644)
}

// SessionCount returns the number of sessions managed.
func (m *AnalysisSessionManager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
