// Package agent provides the agent loop implementation.
package agent

import (
	"fmt"
	"sync"

	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// DetectionContext captures client-side context for session creation.
type DetectionContext struct {
	CWD               string   `json:"cwd,omitempty"`
	DetectedProjectID string   `json:"detected_project_id,omitempty"`
	CLIArgs           []string `json:"cli_args,omitempty"`
}

// Manager manages per-session AgentLoop instances.
type Manager struct {
	mu           sync.RWMutex
	loops        map[string]*AgentLoop // sessionID → loop
	sessionStore session.Store
	projectMgr   *project.ProjectManager
}

// ManagerConfig holds configuration for the manager.
type ManagerConfig struct {
	SessionStore session.Store
	ProjectMgr   *project.ProjectManager
}

// NewManager creates a new AgentLoop manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		loops:        make(map[string]*AgentLoop),
		sessionStore: cfg.SessionStore,
		projectMgr:   cfg.ProjectMgr,
	}
}

// GetOrCreate returns existing loop or creates new one for session.
// workingDir is the project path for agent execution context.
func (m *Manager) GetOrCreate(sessionID string, workingDir string, opts ...LoopOption) (*AgentLoop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return existing
	if loop, ok := m.loops[sessionID]; ok {
		return loop, nil
	}

	// Validate inputs
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID required")
	}
	if workingDir == "" {
		return nil, fmt.Errorf("workingDir required")
	}

	// Create new loop with explicit session context
	loop := NewAgentLoop(sessionID, workingDir, opts...)
	m.loops[sessionID] = loop

	return loop, nil
}

// Get returns existing loop without creating.
func (m *Manager) Get(sessionID string) (*AgentLoop, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loop, ok := m.loops[sessionID]
	return loop, ok
}

// Remove deletes a loop from the manager.
func (m *Manager) Remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.loops, sessionID)
}

// List returns all managed loops (for debugging/monitoring).
func (m *Manager) List() map[string]*AgentLoop {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*AgentLoop, len(m.loops))
	for k, v := range m.loops {
		result[k] = v
	}
	return result
}
