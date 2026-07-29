// Package agent provides the agent loop implementation.
package agent

import (
	"context"
	"fmt"
	"log/slog"
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

// GetOrCreateWired returns an existing loop for sessionID, or creates a new
// one whose configuration (LLM client, tools, skills, hooks) is mirrored
// from template. The template is typically the daemon's singleton AgentLoop.
//
// workingDir is the project directory for the new loop; it overrides any
// value on the template (per-session isolation). All other config is copied
// via template.ConfigSnapshot().
func (m *Manager) GetOrCreateWired(sessionID, workingDir string, template *AgentLoop) (*AgentLoop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if loop, ok := m.loops[sessionID]; ok {
		// BUG FIX: If the session's project changed (workingDir differs from
		// the cached loop's), evict the stale loop and create a fresh one.
		// Without this, /project set would keep using the old project's
		// working directory for the rest of the session.
		if loop.GetWorkingDir() != workingDir {
			slog.Default().Info("evicting session-scoped loop: project changed",
				"session", sessionID,
				"old_working_dir", loop.GetWorkingDir(),
				"new_working_dir", workingDir,
			)
			delete(m.loops, sessionID)
			// Fall through to create a new loop
		} else {
			return loop, nil
		}
	}
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID required")
	}
	if workingDir == "" {
		return nil, fmt.Errorf("workingDir required")
	}
	if template == nil {
		return nil, fmt.Errorf("template loop required")
	}

	opts := template.ConfigSnapshot()
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

// LoopsForTask returns all AgentLoops whose currentTaskID matches the given
// taskID. Used by the daemon's phase-transition hook to advance budget
// hierarchies only on loops actually working on the transitioning task.
// Returns nil if no loops match. Safe for concurrent use.
func (m *Manager) LoopsForTask(taskID string) []*AgentLoop {
	if taskID == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*AgentLoop
	for _, loop := range m.loops {
		if loop == nil {
			continue
		}
		loop.mu.RLock()
		match := loop.currentTaskID == taskID
		loop.mu.RUnlock()
		if match {
			result = append(result, loop)
		}
	}
	return result
}

// ResolveProjectPath looks up a project's LocalPath by its ID using the
// project manager wired into the Manager. Returns empty string if the
// project manager is nil, the project ID is empty, or the lookup fails.
// This is used to recover the working directory for legacy sessions that
// have a ProjectID but no ProjectPath (pre-fix sessions).
func (m *Manager) ResolveProjectPath(ctx context.Context, projectID string) string {
	if m == nil || m.projectMgr == nil || projectID == "" {
		return ""
	}
	p, err := m.projectMgr.Get(ctx, projectID)
	if err != nil || p == nil {
		return ""
	}
	return p.LocalPath
}
