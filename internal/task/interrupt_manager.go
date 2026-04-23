package task

import (
	"log/slog"
	"sync"
)

// InterruptManager manages interrupt tokens for all active tasks.
type InterruptManager struct {
	mu     sync.RWMutex
	tokens map[string]*InterruptToken
	logger *slog.Logger
}

// NewInterruptManager creates a new interrupt manager.
func NewInterruptManager(logger *slog.Logger) *InterruptManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &InterruptManager{
		tokens: make(map[string]*InterruptToken),
		logger: logger,
	}
}

// GetOrCreate returns an existing token or creates a new one.
func (m *InterruptManager) GetOrCreate(taskID string) *InterruptToken {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tok, ok := m.tokens[taskID]; ok {
		return tok
	}

	tok := NewInterruptToken(taskID)
	m.tokens[taskID] = tok
	m.logger.Debug("Created interrupt token", "task_id", taskID)
	return tok
}

// Get returns a token if it exists.
func (m *InterruptManager) Get(taskID string) (*InterruptToken, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tok, ok := m.tokens[taskID]
	return tok, ok
}

// Trigger triggers a task's interrupt token.
func (m *InterruptManager) Trigger(taskID string, reason InterruptReason, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tok, ok := m.tokens[taskID]
	if !ok {
		// Create token and trigger immediately
		tok = NewInterruptToken(taskID)
		m.tokens[taskID] = tok
	}

	tok.Trigger(reason, message)
	m.logger.Info("Task interrupted",
		"task_id", taskID,
		"reason", reason,
		"message", message,
	)
	return nil
}

// Remove removes a token (called when task completes).
func (m *InterruptManager) Remove(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tokens, taskID)
	m.logger.Debug("Removed interrupt token", "task_id", taskID)
}

// ListActive returns all active task IDs with interrupt tokens.
func (m *InterruptManager) ListActive() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.tokens))
	for id := range m.tokens {
		ids = append(ids, id)
	}
	return ids
}

// Close shuts down the manager.
func (m *InterruptManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tok := range m.tokens {
		tok.Trigger(ReasonResourceLimit, "InterruptManager closed")
	}
	m.tokens = make(map[string]*InterruptToken)
	return nil
}
