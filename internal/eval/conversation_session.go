package eval

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/pkg/id"
)

// -----------------------------------------------------------------------
// AnalysisMode — the HALO-style three-phase conversation strategy.
// -----------------------------------------------------------------------

// AnalysisMode controls how the session structures its analysis.
type AnalysisMode int

const (
	ModeDiscovery AnalysisMode = iota // Broad exploration of failure patterns
	ModeSurgical                      // Targeted deep-dive into specific spans/traces
	ModeSynthesis                     // Report generation and recommendation synthesis
)

func (m AnalysisMode) String() string {
	switch m {
	case ModeDiscovery:
		return "discovery"
	case ModeSurgical:
		return "surgical"
	case ModeSynthesis:
		return "synthesis"
	default:
		return "unknown"
	}
}

// ConversationState represents the lifecycle state of a ConversationSession.
// We use ConversationState (not SessionState) to avoid redeclaration conflicts
// with the AnalysisSession in analysis_session.go which defines its own SessionState.
type ConversationState int

const (
	// StateIdle — waiting for input.
	StateIdle ConversationState = iota
	// StateProcessing — currently analyzing a turn.
	StateProcessing
	// StateComplete — analysis finished, session closed.
	StateComplete
	// StateFailed — error occurred.
	StateFailed
)

func (s ConversationState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateProcessing:
		return "processing"
	case StateComplete:
		return "complete"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------
// TurnRecord — a single exchanged turn with optional metadata.
// -----------------------------------------------------------------------

// TurnRecord represents a single question-and-answer exchange in the session.
type TurnRecord struct {
	TurnID        string    `json:"turn_id"`
	Index         int       `json:"index"`
	Timestamp     time.Time `json:"timestamp"`
	UserInput     string    `json:"user_input"`
	AssistantResp string    `json:"assistant_response"`
	Mode          string    `json:"mode"`
	TraceRefs     []string  `json:"trace_refs,omitempty"`
	Errors        []string  `json:"errors,omitempty"`
}

// -----------------------------------------------------------------------
// SessionConfig — options for a conversation session.
// -----------------------------------------------------------------------

// SessionConfig controls the behavior of a ConversationSession.
type SessionConfig struct {
	// MaxTurns caps the number of turns. Zero means no limit.
	MaxTurns int
	// Model is the LLM model ID used for analysis responses.
	Model string
	// TraceStorePath is the path to the trace JSONL file (passed to RLM).
	TraceStorePath string
	// EnableTools lists tool names that are active during synthesis.
	// An empty list means all available tools are enabled.
	EnableTools []string
	// Mode overrides the auto-switching behavior. Zero means auto.
	Mode AnalysisMode
	// PromptSeed is an optional starting prompt that guides the first turn.
	PromptSeed string
}

// effectiveMaxTurns returns the configured MaxTurns clamped to a sane default.
func (c *SessionConfig) effectiveMaxTurns() int {
	if c.MaxTurns <= 0 {
		return 50
	}
	return c.MaxTurns
}

// DefaultSessionConfig returns sensible defaults matching HALO-style patterns.
func DefaultSessionConfig() *SessionConfig {
	return &SessionConfig{
		MaxTurns: 50,
	}
}

// -----------------------------------------------------------------------
// ChatMessage / ChatResult — lightweight LLM interface for eval.
// -----------------------------------------------------------------------

// ChatMessage mirrors the common shape of LLM chat messages.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResult is the shape of an LLM chat response.
type ChatResult struct {
	Content string `json:"content"`
}

// LLMClient is the minimal interface an LLM client must implement to be
// used with ConversationSession. This avoids a hard dependency on
// internal/llm from the eval package.
type LLMClient interface {
	Chat(messages []ChatMessage) (*ChatResult, error)
}

// -----------------------------------------------------------------------
// ConversationSession — the main session manager.
// -----------------------------------------------------------------------

// ConversationSession manages a HALO-style multi-turn evaluation conversation.
type ConversationSession struct {
	config      *SessionConfig
	state       ConversationState
	sessionID   string
	createdAt   time.Time
	updatedAt   time.Time
	turns       []TurnRecord
	currentMode AnalysisMode
	mu          sync.RWMutex

	store    agent.TraceStoreReader
	llmClient LLMClient
	turnCount int
	err       error
}

// NewConversationSession creates a session with the given config.
// The session is in StateIdle until Start is called.
func NewConversationSession(config *SessionConfig) *ConversationSession {
	now := time.Now()
	mode := config.Mode
	if mode == 0 {
		mode = ModeDiscovery
	}
	return &ConversationSession{
		config:      config,
		state:       StateIdle,
		sessionID:   id.Generate("conv-"),
		createdAt:   now,
		updatedAt:   now,
		turns:       make([]TurnRecord, 0),
		currentMode: mode,
		turnCount:   0,
	}
}

// Start initializes the session with a trace store reader.
// After Start the session is in StateIdle, ready to accept turns.
func (s *ConversationSession) Start(store agent.TraceStoreReader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
	s.state = StateIdle
	s.updatedAt = time.Now()
}

// StartWithLLM initializes the session with a trace store and LLM client.
func (s *ConversationSession) StartWithLLM(store agent.TraceStoreReader, llmClient LLMClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
	s.llmClient = llmClient
	s.state = StateIdle
	s.updatedAt = time.Now()
}

// ProcessTurn sends a user input and gets a response.
// Returns (response, done, error). When done is true the session is finished.
func (s *ConversationSession) ProcessTurn(userInput string) (response string, done bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateComplete || s.state == StateFailed {
		return "", true, fmt.Errorf("session %s is %s", s.sessionID, s.state)
	}

	s.state = StateProcessing

	// Check max turns.
	if s.turnCount >= s.config.effectiveMaxTurns() {
		s.state = StateComplete
		s.updatedAt = time.Now()
		return "", true, fmt.Errorf("max turns (%d) reached", s.config.effectiveMaxTurns())
	}

	s.turnCount++
	turnIdx := s.turnCount

	// Determine mode for this turn.
	mode := s.determineModeLocked(turnIdx)

	// Build prompts.
	systemPrompt := s.buildSystemPrompt(mode)
	historyMessages := s.buildHistoryMessages(mode)

	// Combine history with current input.
	allMessages := append(historyMessages, ChatMessage{Role: "user", Content: userInput})

	// Generate response.
	resp, innerErr := s.invokeAnalysisLocked(systemPrompt, allMessages, s.store)
	if innerErr != nil {
		s.state = StateFailed
		s.err = innerErr
		s.updatedAt = time.Now()
		return "", false, fmt.Errorf("conversation session analysis: %w", innerErr)
	}

	// Check if we should switch modes based on turn progress.
	if s.currentMode != mode {
		s.currentMode = mode
	}

	// Record the turn.
	record := TurnRecord{
		TurnID:        id.Generate("trn-"),
		Index:         turnIdx,
		Timestamp:     time.Now(),
		UserInput:     userInput,
		AssistantResp: resp,
		Mode:          mode.String(),
	}
	s.turns = append(s.turns, record)
	s.updatedAt = time.Now()
	s.state = StateIdle

	done = s.doneLocked()
	if done {
		s.state = StateComplete
	}

	return resp, done, nil
}

// GetHistory returns a copy of the turn history.
func (s *ConversationSession) GetHistory() []TurnRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := make([]TurnRecord, len(s.turns))
	copy(cp, s.turns)
	return cp
}

// GetSessionID returns the session's unique ID.
func (s *ConversationSession) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// GetState returns the current session state.
func (s *ConversationSession) GetState() ConversationState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// GetCurrentMode returns the current analysis mode.
func (s *ConversationSession) GetCurrentMode() AnalysisMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentMode
}

// GetTurnCount returns the number of processed turns.
func (s *ConversationSession) GetTurnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turnCount
}

// Close marks the session as complete and releases resources.
func (s *ConversationSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endSessionLocked()
}

// GetError returns the last error that occurred, or nil.
func (s *ConversationSession) GetError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// QueryTraces runs a lightweight RLM failure-mode analysis over all indexed traces.
func (s *ConversationSession) QueryTraces(ctx context.Context) ([]string, []agent.FailureMode, error) {
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	if store == nil {
		return nil, nil, fmt.Errorf("session has no trace store: call Start first")
	}

	traceIDs, err := store.ListTraceIDs()
	if err != nil {
		return nil, nil, fmt.Errorf("list trace IDs: %w", err)
	}
	if len(traceIDs) == 0 {
		return traceIDs, nil, nil
	}

	// Run a lightweight RLM analysis over all traces.
	rlmCfg := agent.DefaultRLMConfig()
	rlmCfg.MaximumDepth = 1
	analyzer := agent.NewRLMAnalyzer(rlmCfg, store, nil, nil)

	result, err := analyzer.Analyze(ctx, "Identify the top failure modes across all traces.")
	if err != nil {
		return traceIDs, nil, fmt.Errorf("RLM analysis: %w", err)
	}

	return traceIDs, result.FailureModes, nil
}

// -----------------------------------------------------------------------
// Mode management
// -----------------------------------------------------------------------

// SwitchTo forces the session into a specific analysis mode.
// It is a no-op on completed/failed sessions.
func (s *ConversationSession) SwitchTo(mode AnalysisMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateComplete || s.state == StateFailed {
		return
	}
	s.currentMode = mode
	s.updatedAt = time.Now()
}

// determineModeLocked selects the best mode for the given turn index
// using auto-switching: turns 1-3 (discovery) -> 4-10 (surgical) -> 11+ (synthesis).
func (s *ConversationSession) determineModeLocked(turn int) AnalysisMode {
	// If the config pinned a mode, always use it.
	if s.config.Mode != 0 {
		return s.config.Mode
	}

	if turn <= 3 {
		return ModeDiscovery
	}
	if turn <= 10 {
		return ModeSurgical
	}
	return ModeSynthesis
}

// -----------------------------------------------------------------------
// Prompt construction
// -----------------------------------------------------------------------

func (s *ConversationSession) buildSystemPrompt(mode AnalysisMode) string {
	var sb strings.Builder

	sb.WriteString("You are a trace analysis consultant for the Meept agent framework.\n")
	sb.WriteString(fmt.Sprintf("Current analysis mode: %s.\n\n", mode))

	switch mode {
	case ModeDiscovery:
		sb.WriteString("DISCOVERY PHASE: Broadly explore the available execution traces.\n")
		sb.WriteString("- Look for common failure patterns across traces.\n")
		sb.WriteString("- Identify high-severity issues (panics, fatal errors, refusals).\n")
		sb.WriteString("- List trace IDs that appear most problematic.\n")
		sb.WriteString("- Keep responses concise; this is a survey phase.\n")
	case ModeSurgical:
		sb.WriteString("SURGICAL PHASE: Deep-dive into specific trace spans.\n")
		sb.WriteString("- Focus on the traces and spans identified in earlier phases.\n")
		sb.WriteString("- Examine input/output patterns, token usage, and error details.\n")
		sb.WriteString("- Use view_spans-style queries to inspect specific spans.\n")
		sb.WriteString("- Per-attribute truncation budget: up to 16 KB.\n")
	case ModeSynthesis:
		sb.WriteString("SYNTHESIS PHASE: Generate a consolidated report with recommendations.\n")
		sb.WriteString("- Summarize all findings from discovery and surgical phases.\n")
		sb.WriteString("- Provide an ordered list of actionable remediation steps.\n")
		sb.WriteString("- Include severity ratings and trace references.\n")
		sb.WriteString("- Keep output structured for downstream processing.\n")
	}

	if len(s.config.EnableTools) > 0 {
		sb.WriteString(fmt.Sprintf("\nActive tools: %s\n", strings.Join(s.config.EnableTools, ", ")))
	}

	return sb.String()
}

// buildHistoryMessages constructs a truncated message history for LLM context.
func (s *ConversationSession) buildHistoryMessages(mode AnalysisMode) []ChatMessage {
	if len(s.turns) == 0 {
		return nil
	}

	var messages []ChatMessage

	// Include only recent turns to manage context budget.
	const contextWindow = 6
	start := len(s.turns) - contextWindow
	if start < 0 {
		start = 0
	}

	// Brief summary of omitted earlier turns.
	if start > 0 {
		aggregated := s.summarizeEarlyTurns(start)
		if aggregated != "" {
			messages = append(messages, ChatMessage{Role: "system", Content: aggregated})
		}
	}

	for _, t := range s.turns[start:] {
		messages = append(messages, ChatMessage{Role: "user", Content: t.UserInput})
		messages = append(messages, ChatMessage{
			Role:    "assistant",
			Content: truncateStr(t.AssistantResp, modeResponseBudget(mode)),
		})
	}

	return messages
}

// summarizeEarlyTurns creates a one-line-per-turn summary for older turns.
func (s *ConversationSession) summarizeEarlyTurns(upto int) string {
	var parts []string
	for i, t := range s.turns[:upto] {
		parts = append(parts, fmt.Sprintf("Turn %d [%s]: %s -> %.80s…",
			i+1, t.Mode, truncateStr(t.UserInput, 100), truncateStr(t.AssistantResp, 120)))
	}
	return "Early conversation history (full context omitted):\n" + strings.Join(parts, "\n")
}

func modeResponseBudget(mode AnalysisMode) int {
	switch mode {
	case ModeDiscovery:
		return 4096
	case ModeSurgical:
		return 16384
	default:
		return 24576
	}
}

// -----------------------------------------------------------------------
// Analysis execution
// -----------------------------------------------------------------------

// invokeAnalysisLocked generates an analysis response using the LLM (or deterministic fallback).
// The caller MUST hold s.mu.
func (s *ConversationSession) invokeAnalysisLocked(systemPrompt string, messages []ChatMessage, store agent.TraceStoreReader) (string, error) {
	if s.llmClient != nil {
		return s.llmCallLocked(systemPrompt, messages)
	}
	return s.deterministicAnalysis(systemPrompt, messages, store)
}

// llmCallLocked sends messages to the LLM and returns the result.
func (s *ConversationSession) llmCallLocked(systemPrompt string, messages []ChatMessage) (string, error) {
	all := make([]ChatMessage, 0, 1+len(messages))
	all = append(all, ChatMessage{Role: "system", Content: systemPrompt})
	all = append(all, messages...)

	result, err := s.llmClient.Chat(all)
	if err != nil {
		return "", fmt.Errorf("LLM chat: %w", err)
	}
	return result.Content, nil
}

// deterministicAnalysis produces a pattern-based analysis without an LLM client.
// The caller holds s.mu.
func (s *ConversationSession) deterministicAnalysis(systemPrompt string, messages []ChatMessage, store agent.TraceStoreReader) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Analysis (deterministic, %s) ===\n\n", s.currentMode))

	if store != nil {
		traceIDs, err := store.ListTraceIDs()
		if err != nil {
			sb.WriteString(fmt.Sprintf("Error listing traces: %v\n", err))
		} else if len(traceIDs) == 0 {
			sb.WriteString("No traces available for analysis.\n")
		} else {
			sb.WriteString(fmt.Sprintf("Trace store: %d traces indexed.\n", len(traceIDs)))
			for i := 0; i < len(traceIDs) && i < 5; i++ {
				sb.WriteString(fmt.Sprintf("  trace %s\n", traceIDs[i]))
			}
		}
	} else {
		sb.WriteString("No trace store configured. Use Start() to initialize.\n")
	}

	sb.WriteString("\n--- Conversation context ---\n")
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", msg.Role, truncateStr(msg.Content, 500)))
	}

	return sb.String(), nil
}

// -----------------------------------------------------------------------
// State management (locked variants)
// -----------------------------------------------------------------------

func (s *ConversationSession) endSessionLocked() {
	if s.state == StateComplete || s.state == StateFailed {
		return
	}
	s.updatedAt = time.Now()
	s.state = StateComplete
}

func (s *ConversationSession) doneLocked() bool {
	return s.config.effectiveMaxTurns() > 0 && s.turnCount >= s.config.effectiveMaxTurns()
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// -----------------------------------------------------------------------
// ConversationSessionManager — manages multiple sessions.
// -----------------------------------------------------------------------

// ConversationSessionManager stores and retrieves conversation sessions.
type ConversationSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*ConversationSession
}

// NewConversationSessionManager creates a new session manager.
func NewConversationSessionManager() *ConversationSessionManager {
	return &ConversationSessionManager{
		sessions: make(map[string]*ConversationSession),
	}
}

// CreateSession creates a new session with the given config.
func (m *ConversationSessionManager) CreateSession(config *SessionConfig) *ConversationSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := NewConversationSession(config)
	m.sessions[session.sessionID] = session
	return session
}

// GetSession returns the session with the given ID, or nil if not found.
func (m *ConversationSessionManager) GetSession(sessionID string) *ConversationSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

// ListSessions returns all sessions, sorted by creation time (newest first).
func (m *ConversationSessionManager) ListSessions() []*ConversationSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*ConversationSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}

	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].createdAt.After(result[i].createdAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// DeleteSession removes a session by ID. Returns error if not found.
func (m *ConversationSessionManager) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[sessionID]; !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	delete(m.sessions, sessionID)
	return nil
}

// SessionCount returns the number of managed sessions.
func (m *ConversationSessionManager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// -----------------------------------------------------------------------
// Top-level convenience functions for HTTP handlers and CLI.
// -----------------------------------------------------------------------

// TraceIDsForMode returns trace IDs relevant to the given analysis mode.
func TraceIDsForMode(mode AnalysisMode, store agent.TraceStoreReader) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	return store.ListTraceIDs()
}

// ProcessConversationTurn delegates ProcessTurn on a session retrieved from a manager.
func ProcessConversationTurn(mgr *ConversationSessionManager, sessionID, userInput string) (response string, done bool, err error) {
	s := mgr.GetSession(sessionID)
	if s == nil {
		return "", true, fmt.Errorf("session %s not found", sessionID)
	}
	return s.ProcessTurn(userInput)
}
