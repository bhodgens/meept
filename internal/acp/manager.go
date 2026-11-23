package acp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
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
	// ErrPermissiveRefused is the fail-closed answer for a caller that has
	// declared (via WithNoPermissive) that its turn must never run with
	// permissive permissions but whose effective mode — from ANY of the
	// three sources (call option, catalog entry, global) — resolved to
	// permissive. The session is never created/returned in that case, so a
	// tier-3 employee cannot auto-approve external permission requests via
	// an operator-owned catalog entry (bughunt H5).
	ErrPermissiveRefused = errors.New("acp: permissive mode refused for no-permissive caller")
)

// noPermissiveKey scopes the fail-closed context marker used by a tier-3
// employee's ACP turn: the marker rides the same ctx that reaches
// GetOrCreatePersona, so the manager can refuse a permissive session
// regardless of which source selected permissive.
type noPermissiveKey struct{}

// WithNoPermissive marks ctx as belonging to a caller whose turns must
// never run with permissive permissions. Nil-safe and idempotent: an empty
// context returns ctx unchanged; the value is a boolean marker only.
func WithNoPermissive(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, noPermissiveKey{}, true)
}

// NoPermissiveFromContext reports whether ctx was stamped by
// WithNoPermissive. A nil ctx is an unmarked caller.
func NoPermissiveFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	marked, _ := ctx.Value(noPermissiveKey{}).(bool)
	return marked
}

// turnCostSinkKey scopes the per-turn cost recorder that the daemon
// employee adapter plants on the ctx. It flows with the ctx down through
// the agent loop into Session.Send, so an ACP harness that reports cost on
// its prompt response can forward it to the goal-loop budget lane even
// though the ACP path bypasses reasoningCycle (bughunt M2, D4 decision C:
// wire-forward usage iff the harness emits it).
type turnCostSinkKey struct{}

// WithTurnCostSink returns a ctx carrying sink for this turn's provider
// cost in dollars. The sink must be non-blocking and additive (it may be
// called once per cost-bearing response in a turn). Nil sink or nil ctx
// leaves the ctx cost-dark: cost is then simply not reported.
func WithTurnCostSink(ctx context.Context, sink func(costUSD float64)) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, turnCostSinkKey{}, sink)
}

// ReportTurnCost forwards a reported provider cost to the turn's sink (if
// any). Non-positive costs are ignored (unconfigured prices / absent data
// must not look like a reported zero-cost turn).
func ReportTurnCost(ctx context.Context, costUSD float64) {
	if costUSD <= 0 || ctx == nil {
		return
	}
	if sink, ok := ctx.Value(turnCostSinkKey{}).(func(costUSD float64)); ok && sink != nil {
		sink(costUSD)
	}
}

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
	// onSessionCreated is the post-creation hook fired once per freshly
	// started session (inter-agent-coordination leaf 04-01, C8). The
	// daemon installs it to arm the per-session turn-state publisher.
	onSessionCreated func(*Session)
	mu               sync.Mutex
	sessions         map[string]*Session
	pending          map[string]*startWait
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

// SetOnSessionCreated installs the post-creation hook (inter-agent
// coordination leaf 04-01, C8): fired once per freshly STARTED session,
// outside the manager mutex, with the live session — never for sessions
// served from the sessions/pending caches. Nil-safe per the repo setter
// rule; nil clears the hook. The daemon uses it to arm
// sess.SetTurnStatePublisher at the single construction seam
// (GetOrCreatePersona), so lazily created ACP sessions publish
// agent.turn.state edges without every caller re-wiring the seam.
func (m *Manager) SetOnSessionCreated(fn func(*Session)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onSessionCreated = fn
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

// AppendPermissionNotifier fans an additional PermissionNotifier into the
// armed relay (hitl-notifications leaf 01: the Slack approval channel
// receives every requested/resolved event beside the existing bus
// notifier). No-op when the relay is not armed or n is nil. The added
// notifier must honor the seam's non-blocking contract (relay.go:85-98).
func (m *Manager) AppendPermissionNotifier(n PermissionNotifier) {
	if m == nil || n == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.relay == nil || m.notifier == nil {
		return
	}
	if fan, ok := m.notifier.(*fanoutNotifier); ok {
		fan.add(n)
		return
	}
	m.notifier = &fanoutNotifier{delegates: []PermissionNotifier{m.notifier, n}}
}

// fanoutNotifier composes several PermissionNotifiers behind the single
// seam. Each delegate is itself required to be non-blocking (mutexio
// rule); the fanout holds no lock across the delegate calls - it
// snapshots the slice under m's guard discipline via its own mutex that
// is never held across a call.
type fanoutNotifier struct {
	mu        sync.Mutex
	delegates []PermissionNotifier
}

func (f *fanoutNotifier) add(n PermissionNotifier) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delegates = append(f.delegates, n)
}

// NotifyPermissionRequested fans out; true when ANY delegate reports a
// live listener (the seam's "a missed frame does not cancel the wait"
// semantics - a reconnecting surface can still answer).
func (f *fanoutNotifier) NotifyPermissionRequested(p PendingPermission) bool {
	f.mu.Lock()
	delegates := append([]PermissionNotifier(nil), f.delegates...)
	f.mu.Unlock()
	any := false
	for _, n := range delegates {
		if n.NotifyPermissionRequested(p) {
			any = true
		}
	}
	return any
}

// NotifyPermissionResolved fans out (void on the seam).
func (f *fanoutNotifier) NotifyPermissionResolved(p PendingPermission, decision, reason string, approver Approver) {
	f.mu.Lock()
	delegates := append([]PermissionNotifier(nil), f.delegates...)
	f.mu.Unlock()
	for _, n := range delegates {
		n.NotifyPermissionResolved(p, decision, reason, approver)
	}
}

var _ PermissionNotifier = (*fanoutNotifier)(nil)

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
	return m.ResolvePermissionWithApprover(context.Background(), id, decision, Approver{})
}

// ResolvePermissionWithApprover is ResolvePermission plus the approver
// identity (audit M5). The RPC boundary passes the kernel-verified socket
// peer; the HTTP boundary passes the key fingerprint. A zero Approver keeps
// the legacy unattributed behavior for internal callers.
//
// When [acp] resolve_rank is on, an allow/always resolve that reaches here
// (i.e. already past the identity gate at the RPC boundary) is offered to
// the decision-model second opinion first: a confident "block" answer
// returns ErrResolveRanked and the relay is left UNTOUCHED, so the pending
// request stays answerable. The rank is tighten-only — it can reject an
// allow but never approves and never sees a deny (resolve_rank.go).
func (m *Manager) ResolvePermissionWithApprover(ctx context.Context, id string, decision string, approver Approver) error {
	if m == nil {
		return ErrRelayNotFound
	}
	// Tighten-only second opinion BEFORE the relay is touched: on a
	// confident block the pending request must stay answerable, so the
	// rank runs first and a rejection short-circuits without consuming it.
	if err := m.rankResolve(ctx, id, decision, approver); err != nil {
		return err
	}
	m.mu.Lock()
	rl := m.relay
	m.mu.Unlock()
	return rl.ResolveWithApprover(id, decision, approver)
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

// PersonaOpts carries the per-persona overrides for GetOrCreatePersona
// (acp-employees dim 2B). Zero values fall back to the manager defaults:
// CommandArgs appends extra argv to the catalog entry's command, an empty
// PermissionMode uses m.cfg.PermissionMode, a zero DecisionThreshold uses
// m.cfg.DecisionThreshold, and an empty Cwd uses the existing resolution
// (entry.Cwd, else the workdir argument).
type PersonaOpts struct {
	CommandArgs       []string
	PermissionMode    string
	DecisionThreshold float64
	Cwd               string
}

// personaKey builds the composite session-map key: catalog agent id +
// NUL separator + persona name ("" for the legacy no-persona lane). The
// NUL never appears in agent ids or persona names, so the split is
// unambiguous.
func personaKey(agentID, persona string) string {
	return agentID + "\x00" + persona
}

// GetOrCreate returns the live session for agentID, starting it if needed.
// It is the no-persona lane: it delegates to GetOrCreatePersona with an
// empty persona and default opts, so existing callers see byte-identical
// SessionConfig values.
func (m *Manager) GetOrCreate(ctx context.Context, agentID, workdir string) (*Session, error) {
	return m.GetOrCreatePersona(ctx, agentID, "", workdir, PersonaOpts{})
}

// GetOrCreatePersona returns the live session for one (agentID, persona)
// pair, starting it if needed. Several personas may share one catalog
// entry; each gets its own subprocess session keyed by agentID+"\x00"+
// persona in m.sessions and m.pending.
func (m *Manager) GetOrCreatePersona(ctx context.Context, agentID, persona, workdir string, opts PersonaOpts) (*Session, error) {
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

	// Resolve the EFFECTIVE permission mode with the exact precedence the
	// session will run under (call option > catalog entry > global [acp]).
	// Computing it BEFORE the pending insert lets the no-permissive
	// fail-closed guard below return without leaving a dangling waiter.
	permMode := m.effectivePermissionMode(entry, opts.PermissionMode)

	// Fail-closed for no-permissive callers (bughunt H5): a tier-3 acp
	// employee stamps WithNoPermissive on its turn ctx. Even when the
	// operator-owned catalog entry (or the global) says permissive while
	// the wiring-time guard saw only "human", the session is NEVER created
	// permissive — so acp/session.go's auto-approve branch is unreachable
	// for that employee regardless of which source selected permissive.
	if NoPermissiveFromContext(ctx) && permMode == "permissive" {
		slog.Warn("SECURITY: refusing permissive acp session for no-permissive caller",
			"component", "acp",
			"agent", agentID,
			"call_option", opts.PermissionMode,
			"catalog_mode", entry.PermissionMode,
			"global_mode", m.cfg.PermissionMode)
		return nil, ErrPermissiveRefused
	}

	key := personaKey(agentID, persona)
	m.mu.Lock()
	if s, exists := m.sessions[key]; exists {
		m.mu.Unlock()
		return s, nil
	}
	if w, exists := m.pending[key]; exists {
		m.mu.Unlock()
		<-w.done
		return w.sess, w.err
	}
	// MaxAgents bounds the TOTAL number of live + starting sessions; with
	// the persona lane the maps are keyed per (agent, persona), so the cap
	// now counts persona sessions too (acp-employees: a runaway roster of
	// personas hits ErrMaxAgents, not fork-bomb).
	if len(m.sessions)+len(m.pending) >= m.cfg.MaxAgents {
		m.mu.Unlock()
		return nil, ErrMaxAgents
	}
	w := &startWait{done: make(chan struct{})}
	m.pending[key] = w
	fn := m.startFn
	dec := m.decision
	rl := m.relay
	notf := m.notifier
	m.mu.Unlock()

	cwd := entry.Cwd
	if cwd == "" {
		cwd = workdir
	}
	if opts.Cwd != "" {
		cwd = opts.Cwd
	}
	if permMode == "permissive" && (opts.PermissionMode == "permissive" || entry.PermissionMode == "permissive") {
		slog.Warn("SECURITY: acp connection runs with permissive permission mode; every external permission request is auto-approved",
			"component", "acp",
			"agent", agentID,
			"permission_mode", permMode)
	}
	threshold := m.cfg.DecisionThreshold
	if opts.DecisionThreshold != 0 {
		threshold = opts.DecisionThreshold
	}
	sc := SessionConfig{
		AgentID:        agentID,
		Persona:        persona,
		Command:        append([]string(nil), entry.Command...),
		CommandArgs:    append([]string(nil), opts.CommandArgs...),
		Env:            copyEnv(entry.Env),
		Cwd:            cwd,
		DefaultMode:    entry.DefaultMode,
		DialTimeout:    time.Duration(m.cfg.DialTimeout) * time.Second,
		CallTimeout:    time.Duration(m.cfg.CallTimeout) * time.Second,
		PermissionMode: permMode,
	}
	// Decision seam + threshold flow ONLY in decision mode: permissive/deny
	// SessionConfigs stay byte-identical to the pre-decision lane. The
	// threshold comes from opts when overridden, else straight off m.cfg
	// (leaf 01's ACPConfig field) — no separate Manager field.
	if sc.PermissionMode == "decision" {
		sc.Decision = dec
		sc.DecisionThreshold = threshold
	}
	// Human relay lane (acp-human-permission-relay leaf 02, drift 3):
	// stamped ONLY for mode "human", or mode "decision" with
	// decision_escalate_to_human. RelayTimeout is read straight off m.cfg
	// (seconds -> Duration, like DecisionThreshold / DialTimeout).
	// permissive/deny/decision-without-escalate SessionConfigs stay
	// byte-identical (relays zero-valued). A PersonaOpts.PermissionMode
	// override participates in the SAME conditionals.
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
	delete(m.pending, key)
	w.sess = sess
	w.err = err
	if err == nil {
		m.sessions[key] = sess
	}
	close(w.done)
	hook := m.onSessionCreated
	m.mu.Unlock()

	// Post-creation hook (inter-agent-coordination leaf 04-01, C8): fired
	// OUTSIDE the mutex, only for a freshly started session (cache hits
	// and concurrent waiters return above without reaching this point).
	// A nil hook or failed start is a no-op — the session then runs with
	// an unarmed turn-state seam, exactly like the pre-leaf-04 build.
	if err == nil && hook != nil {
		hook(sess)
	}
	return sess, err
}

// Stop closes the live session for agentID (no-persona lane) and drops it
// from the registry. Persona sessions ride under composite keys; use
// StopPersona for those.
func (m *Manager) Stop(agentID string) error {
	return m.stopKey(personaKey(agentID, ""))
}

// StopPersona closes the live session for one (agentID, persona) pair and
// drops it from the registry. Idempotent; no-op when disabled or absent.
func (m *Manager) StopPersona(agentID, persona string) error {
	return m.stopKey(personaKey(agentID, persona))
}

func (m *Manager) stopKey(key string) error {
	if !m.cfg.Enabled {
		return nil
	}
	for {
		m.mu.Lock()
		if w, ok := m.pending[key]; ok {
			m.mu.Unlock()
			<-w.done
			continue
		}
		s := m.sessions[key]
		delete(m.sessions, key)
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

// LiveSessions returns a copy of session key -> current SessionState. The
// key is the catalog agent id for no-persona sessions (unchanged for
// existing consumers) and "agentID/persona" for persona sessions — the
// same "hermes/bob" render the relay summary uses. The internal NUL-keyed
// maps never leak their raw keys.
func (m *Manager) LiveSessions() map[string]SessionState {
	m.mu.Lock()
	snap := make(map[string]*Session, len(m.sessions))
	maps.Copy(snap, m.sessions)
	m.mu.Unlock()
	out := make(map[string]SessionState, len(snap))
	for key, s := range snap {
		label := sessionLabel(key)
		if s == nil {
			out[label] = StateClosed
			continue
		}
		out[label] = s.State()
	}
	return out
}

// sessionLabel converts an internal composite key (agentID+"\x00"+persona)
// into its external form: the bare agent id when persona is empty, else
// "agentID/persona".
func sessionLabel(key string) string {
	agentID, persona, found := strings.Cut(key, "\x00")
	if !found || persona == "" {
		return agentID
	}
	return agentID + "/" + persona
}

// effectivePermissionMode resolves the permission mode a session will
// actually run under: explicit call option > catalog entry > global [acp].
// This is the SINGLE precedence used by GetOrCreatePersona and exposed via
// EffectivePermissionMode for callers (e.g. the daemon's tier-3 D3 guard)
// that must reason about the same effective value the session sees.
func (m *Manager) effectivePermissionMode(entry config.ACPAgentEntry, callMode string) string {
	if callMode != "" {
		return callMode
	}
	if entry.PermissionMode != "" {
		return entry.PermissionMode
	}
	return m.cfg.PermissionMode
}

// EffectivePermissionMode returns the mode a persona session for agentID
// would run under given an optional call-level override. It applies the
// same precedence the session uses (call option > catalog entry > global),
// so a guard consulting it cannot diverge from the session's real mode.
// An unknown agent or a nil manager falls back to the global mode (the
// call option, when set, still wins) — matching the manager's own lookups
// which error on unknown agents before a session is created.
func (m *Manager) EffectivePermissionMode(agentID, callMode string) string {
	if callMode != "" {
		return callMode
	}
	if m == nil {
		return ""
	}
	if entry, ok := m.lookup(agentID); ok && entry.PermissionMode != "" {
		return entry.PermissionMode
	}
	return m.cfg.PermissionMode
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
