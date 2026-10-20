package acp

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	decision Decisioner
	relay    *Relay
	notifier PermissionNotifier
	mu       sync.Mutex
	sessions map[string]*Session
	pending  map[string]*startWait
}

// SetDecisionClient injects the permission lane's decision seam. Nil-safe
// per the repo setter rule; nil clears it (decision mode then fails closed
// with no_decisioner). The threshold is NOT a setter arg — it already lives
// on m.cfg (ACPConfig.DecisionThreshold, leaf 01) and flows through at
// SessionConfig construction.
func (m *Manager) SetDecisionClient(d Decisioner) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.decision = d
	m.mu.Unlock()
}

// HasDecisionClient reports whether a decision seam is currently injected.
// Read-only inspection for wiring tests (acp-permission-decision leaf 03):
// the daemon layer asserts SetDecisionClient was applied without reaching
// into unexported state. Nil-safe.
func (m *Manager) HasDecisionClient() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decision != nil
}

// SetPermissionRelay injects the human relay registry + publish seam
// (acp-human-permission-relay leaf 02, drift 3: ONE setter). Nil-safe per
// the repo setter rule; nil relay/notifier clears the seam (human mode
// then fails closed, decision escalation falls back to plain cancel). The
// timeout + escalate flag are NOT setter args — they already live on
// m.cfg (ACPConfig.RelayTimeout / DecisionEscalateToHuman, leaf 01) and
// flow through at SessionConfig construction, like DecisionThreshold.
func (m *Manager) SetPermissionRelay(relay *Relay, notifier PermissionNotifier) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.relay = relay
	m.notifier = notifier
	m.mu.Unlock()
}

// HasPermissionRelay reports whether BOTH relay halves (registry +
// notifier) are currently injected. Read-only inspection for wiring
// tests. Nil-safe.
func (m *Manager) HasPermissionRelay() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.relay != nil && m.notifier != nil
}

// PendingPermissions returns the still-answerable human relay requests
// (leaf 03's RPC surface). Nil-safe: a manager without a relay reports
// none.
func (m *Manager) PendingPermissions() []PendingPermission {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	rl := m.relay
	m.mu.Unlock()
	return rl.Pending()
}

// ResolvePermission delivers a human verdict for one pending relay request
// (acp-human-permission-relay leaf 03, Contract C — the RPC/HTTP resolve
// surface). It is the manager-side accessor for Relay.Resolve so consumers
// never hold the registry directly: the manager stays the single owner of
// the seam. Nil-safe: a manager without a relay reports ErrRelayNotFound.
// Decision normalization ("anything not allow/always -> deny") lives inside
// the relay; callers at API boundaries validate the verb explicitly BEFORE
// calling (leaf 03 context 4) so a typo never silently denies.
func (m *Manager) ResolvePermission(id string, decision string) error {
	if m == nil {
		return ErrRelayNotFound
	}
	m.mu.Lock()
	rl := m.relay
	m.mu.Unlock()
	return rl.Resolve(id, decision)
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
	dec := m.decision
	rl := m.relay
	notf := m.notifier
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
	// Decision seam + threshold flow ONLY in decision mode: permissive/deny
	// SessionConfigs stay byte-identical to the pre-decision lane. The
	// threshold is read straight off m.cfg (leaf 01's ACPConfig field) — no
	// separate Manager field.
	if sc.PermissionMode == "decision" {
		sc.Decision = dec
		sc.DecisionThreshold = m.cfg.DecisionThreshold
	}
	// Human relay lane (acp-human-permission-relay leaf 02, drift 3):
	// stamped ONLY for mode "human", or mode "decision" with
	// decision_escalate_to_human. RelayTimeout is read straight off m.cfg
	// (seconds -> Duration, like DecisionThreshold / DialTimeout).
	// permissive/deny/decision-without-escalate SessionConfigs stay
	// byte-identical (relays zero-valued).
	if sc.PermissionMode == "human" {
		sc.Relay = rl
		sc.Notifier = notf
		sc.RelayTimeout = time.Duration(m.cfg.RelayTimeout) * time.Second
	} else if sc.PermissionMode == "decision" && m.cfg.DecisionEscalateToHuman {
		sc.Relay = rl
		sc.Notifier = notf
		sc.RelayTimeout = time.Duration(m.cfg.RelayTimeout) * time.Second
		sc.DecisionEscalateToHuman = true
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
	maps.Copy(snap, m.sessions)
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
	maps.Copy(out, in)
	return out
}
