package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/session"
)

// AgentHandler routes Telegram messages to the agent system.
type AgentHandler struct {
	mu         sync.RWMutex
	sessionMgr session.Store
	agentLoop  *agent.AgentLoop
	logger     *slog.Logger

	// chatID -> sessionID mapping
	sessions    map[int64]string
	sessionsDir string
}

// NewAgentHandler creates a new agent handler.
func NewAgentHandler(sessionMgr session.Store, agentLoop *agent.AgentLoop, dataDir string, logger *slog.Logger) *AgentHandler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &AgentHandler{
		sessionMgr:  sessionMgr,
		agentLoop:   agentLoop,
		logger:      logger,
		sessions:    make(map[int64]string),
		sessionsDir: dataDir,
	}

	// Try to load persisted sessions
	if err := h.loadSessions(); err != nil {
		h.logger.Warn("failed to load telegram sessions", "error", err)
	}

	return h
}

// Handle processes incoming Telegram messages and returns the agent response.
func (h *AgentHandler) Handle(ctx context.Context, msg *Message) (string, error) {
	chatID := msg.Chat.ID

	// Get or create session for this chat
	sessionID := h.getOrCreateSession(chatID)
	if sessionID == "" {
		return "", fmt.Errorf("failed to get session for chat %d", chatID)
	}

	// Send typing indicator (handled by bot wrapper, not here)

	// Route to agent loop
	response, err := h.agentLoop.RunOnce(ctx, msg.Text, sessionID)
	if err != nil {
		h.logger.Error("agent error", "error", err, "chat_id", chatID)
		return fmt.Sprintf("Error: %v", err), nil
	}

	return response, nil
}

// ResetSession clears the session mapping for a chat, forcing a new session
// on the next message.
func (h *AgentHandler) ResetSession(chatID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.sessions[chatID]; exists {
		delete(h.sessions, chatID)
		h.logger.Info("session reset for chat", "chat_id", chatID)
	}
}

// getOrCreateSession returns the session ID for a chat, creating one if needed.
func (h *AgentHandler) getOrCreateSession(chatID int64) string {
	h.mu.RLock()
	if sid, ok := h.sessions[chatID]; ok {
		h.mu.RUnlock()
		return sid
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check after acquiring write lock
	if sid, ok := h.sessions[chatID]; ok {
		return sid
	}

	// Create a new session via the store
	sess, err := h.sessionMgr.Create(fmt.Sprintf("telegram-%d", chatID))
	if err != nil {
		h.logger.Error("failed to create session", "error", err, "chat_id", chatID)
		return ""
	}

	h.sessions[chatID] = sess.ConversationID
	h.logger.Info("created session for telegram chat",
		"chat_id", chatID,
		"session_id", sess.ID,
		"conversation_id", sess.ConversationID,
	)

	// Persist the mapping (we hold the write lock, so use saveSessionsLocked)
	if saveErr := h.saveSessionsLocked(); saveErr != nil {
		h.logger.Warn("failed to persist telegram sessions", "error", saveErr)
	}

	return sess.ConversationID
}

// GetSessionCount returns the number of active chat-to-session mappings.
func (h *AgentHandler) GetSessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// loadSessions loads chat-to-session mappings from disk.
func (h *AgentHandler) loadSessions() error {
	if h.sessionsDir == "" {
		return nil
	}

	path := filepath.Join(h.sessionsDir, "telegram_sessions.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sessions file: %w", err)
	}

	var sessions map[int64]string
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("unmarshal sessions: %w", err)
	}

	h.mu.Lock()
	h.sessions = sessions
	h.mu.Unlock()

	h.logger.Info("loaded telegram sessions", "count", len(sessions))
	return nil
}

// saveSessions persists chat-to-session mappings to disk.
// Caller must NOT hold h.mu (this method acquires its own read lock).
func (h *AgentHandler) saveSessions() error {
	if h.sessionsDir == "" {
		return nil
	}

	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(h.sessionsDir, 0o755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	h.mu.RLock()
	data, err := json.MarshalIndent(h.sessions, "", "  ")
	h.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}

	path := filepath.Join(h.sessionsDir, "telegram_sessions.json")
	// Restrict to owner read/write: the file contains user-identifying data
	// (chat IDs mapped to sessions).
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write sessions file: %w", err)
	}

	return nil
}

// saveSessionsLocked persists sessions to disk. The caller must hold h.mu
// (either read or write) -- this method snapshots the map without acquiring
// the lock itself.
func (h *AgentHandler) saveSessionsLocked() error {
	if h.sessionsDir == "" {
		return nil
	}

	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(h.sessionsDir, 0o755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	data, err := json.MarshalIndent(h.sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}

	path := filepath.Join(h.sessionsDir, "telegram_sessions.json")
	// Restrict to owner read/write: the file contains user-identifying data
	// (chat IDs mapped to sessions).
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write sessions file: %w", err)
	}

	return nil
}
