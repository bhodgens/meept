package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Sentinel errors returned by Manager.GetOrCreate. Callers match with errors.Is.
var (
	ErrDisabled      = errors.New("acp: disabled")
	ErrAgentNotFound = errors.New("acp: agent not found")
	ErrAgentDisabled = errors.New("acp: agent disabled")
	ErrMaxAgents     = errors.New("acp: max agents")
)

type startFunc func(ctx context.Context, cfg SessionConfig) (*Session, error)

type startWait struct {
	done chan struct{}
	sess *Session
	err  error
}

// Manager is the daemon-held registry of live ACP sessions.
type Manager struct {
	cfg      config.ACPConfig
	agents   []config.ACPAgentEntry
	startFn  startFunc
	mu       sync.Mutex
	sessions map[string]*Session
	pending  map[string]*startWait
}

// NewManager constructs a Manager from an already-loaded catalog.
// Tests override startFn after construction; production uses Start.
func NewManager(cfg config.ACPConfig, catalog *config.ACPAgentsConfig) *Manager {
	return &Manager{
		cfg:      cfg,
		agents:   snapshotAgents(catalog),
		startFn:  Start,
		sessions: make(map[string]*Session),
		pending:  make(map[string]*startWait),
	}
}

// NewManagerFromFiles loads the agents catalog from cfg.AgentsFile.
func NewManagerFromFiles(cfg config.ACPConfig) (*Manager, error) {
	catalog, err := config.LoadACPAgents(cfg.AgentsFile)
	if err != nil {
		return nil, fmt.Errorf("acp: load agents catalog: %w", err)
	}
	return NewManager(cfg, catalog), nil
}

// Enabled reports whether [acp] enabled=true.
func (m *Manager) Enabled() bool {
	return m.cfg.Enabled
}

// Agents returns a snapshot of the catalog captured at construction.
func (m *Manager) Agents() []config.ACPAgentEntry {
	return snapshotAgents(&config.ACPAgentsConfig{Agents: m.agents})
}

// GetOrCreate returns the live session for agentID, starting it if needed.
func (m *Manager) GetOrCreate(ctx context.Context, agentID, workdir string) (*Session, error) {
	if !m.cfg.Enabled {
		return nil, ErrDisabled
	}
	entry, ok := m.lookup(agentID)
	if !ok {
		return nil, ErrAgentNotFound
	}
	if !entry.Enabled {
		return nil, ErrAgentDisabled
	}

	m.mu.Lock()
	if s, exists := m.sessions[agentID]; exists {
		m.mu.Unlock()
		return s, nil
	}
	if w, exists := m.pending[agentID]; exists {
		m.mu.Unlock()
		<-w.done
		return w.sess, w.err
	}
	if len(m.sessions)+len(m.pending) >= m.cfg.MaxAgents {
		m.mu.Unlock()
		return nil, ErrMaxAgents
	}
	w := &startWait{done: make(chan struct{})}
	m.pending[agentID] = w
	fn := m.startFn
	m.mu.Unlock()

	cwd := entry.Cwd
	if cwd == "" {
		cwd = workdir
	}
	sc := SessionConfig{
		AgentID:        agentID,
		Command:        append([]string(nil), entry.Command...),
		Env:            copyEnv(entry.Env),
		Cwd:            cwd,
		DefaultMode:    entry.DefaultMode,
		DialTimeout:    time.Duration(m.cfg.DialTimeout) * time.Second,
		CallTimeout:    time.Duration(m.cfg.CallTimeout) * time.Second,
		PermissionMode: m.cfg.PermissionMode,
	}
	if fn == nil {
		fn = Start
	}
	sess, err := fn(ctx, sc)

	m.mu.Lock()
	delete(m.pending, agentID)
	w.sess = sess
	w.err = err
	if err == nil {
		m.sessions[agentID] = sess
	}
	close(w.done)
	m.mu.Unlock()
	return sess, err
}

// Stop closes the live session for agentID and drops it from the registry.
func (m *Manager) Stop(agentID string) error {
	if !m.cfg.Enabled {
		return nil
	}
	for {
		m.mu.Lock()
		if w, ok := m.pending[agentID]; ok {
			m.mu.Unlock()
			<-w.done
			continue
		}
		s := m.sessions[agentID]
		delete(m.sessions, agentID)
		m.mu.Unlock()
		if s == nil {
			return nil
		}
		return s.Close()
	}
}

// StopAll closes every live session. Idempotent. No-op when disabled.
func (m *Manager) StopAll() {
	if !m.cfg.Enabled {
		return
	}
	for {
		m.mu.Lock()
		waits := make([]*startWait, 0, len(m.pending))
		for _, w := range m.pending {
			waits = append(waits, w)
		}
		m.mu.Unlock()
		if len(waits) == 0 {
			break
		}
		for _, w := range waits {
			<-w.done
		}
	}
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if err := s.Close(); err != nil {
			continue
		}
	}
}

// LiveSessions returns a copy of agentID -> current SessionState.
func (m *Manager) LiveSessions() map[string]SessionState {
	m.mu.Lock()
	snap := make(map[string]*Session, len(m.sessions))
	for id, s := range m.sessions {
		snap[id] = s
	}
	m.mu.Unlock()
	out := make(map[string]SessionState, len(snap))
	for id, s := range snap {
		if s == nil {
			out[id] = StateClosed
			continue
		}
		out[id] = s.State()
	}
	return out
}

func (m *Manager) lookup(id string) (config.ACPAgentEntry, bool) {
	for _, a := range m.agents {
		if a.ID == id {
			return a, true
		}
	}
	return config.ACPAgentEntry{}, false
}

func snapshotAgents(catalog *config.ACPAgentsConfig) []config.ACPAgentEntry {
	if catalog == nil || len(catalog.Agents) == 0 {
		return []config.ACPAgentEntry{}
	}
	out := make([]config.ACPAgentEntry, len(catalog.Agents))
	for i, a := range catalog.Agents {
		out[i] = a
		if a.Command != nil {
			cmd := make([]string, len(a.Command))
			copy(cmd, a.Command)
			out[i].Command = cmd
		}
		out[i].Env = copyEnv(a.Env)
	}
	return out
}

func copyEnv(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
