package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AgentState constants for quota-related states.
const (
	// StateQuotaWait indicates the agent is waiting for a provider quota to reset.
	StateQuotaWait AgentState = iota + 100 // offset to avoid collision with existing states
)

// Quota event contract (master plan contract 8, as implemented).
//
// Deviations from the master doc's payload sketch, deliberate and documented:
//   - escalation vocabulary is the semantic ladder ""|"warn"|
//     "action_recommended"|"blocked" (not the master's "12h"|"20h"|"24h"
//     strings). The 12h/20h/24h timings gate WHICH tier fires
//     (see QuotaEpisodeTracker.tiers); only the wire strings differ.
//   - the INITIAL episode event carries reason="quota_blocked" with
//     escalation=""; escalation stays distinct from reason everywhere.
//   - from/to name the agent state machine states ("running"/"quota_wait"/
//     "blocked"/"running" on clear).

// QuotaBlockStatus represents an active quota block on a provider credential.
type QuotaBlockStatus struct {
	AgentID        string
	ProviderKey    string
	ModelID        string
	CredentialKey  string
	UnblockAt      time.Time
	EscalationTier int // 0=new, 1=12h, 2=20h, 3=24h(blocked)
}

// QuotaEpisode tracks a single quota block episode for an agent+provider combo.
type QuotaEpisode struct {
	AgentID       string
	ProviderKey   string
	ModelID       string
	CredentialKey string
	UnblockAt     time.Time
	StartedAt     time.Time
	TierFired     [3]bool // tier 0=12h, tier 1=20h, tier 2=24h
}

// QuotaEvent is the bus payload for quota lifecycle events.
type QuotaEvent struct {
	AgentID       string    `json:"agent_id"`
	TaskID        string    `json:"task_id,omitempty"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Reason        string    `json:"reason"`
	ProviderID    string    `json:"provider_id"`
	CredentialKey string    `json:"credential_key"`
	ModelID       string    `json:"model_id"`
	UnblockAt     string    `json:"unblock_at,omitempty"` // RFC3339
	Escalation    string    `json:"escalation,omitempty"` // "", "warn", "action_recommended", "blocked"
	FallbackModel string    `json:"fallback_model,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// QuotaEpisodeTracker manages quota episodes across agent+provider combinations.
// It fires escalation events at 12h/20h/24h boundaries and transitions agent
// state from running -> quota_wait -> blocked.
//
// Tier firing is driven by two paths:
//   - Enter(): fires immediately-overdue tiers synchronously.
//   - a background reaper goroutine (started in NewQuotaEpisodeTracker) that
//     sweeps episodes on every reaperInterval tick so escalation events fire
//     even when no new quota errors re-enter the tracker. At the 24h tier the
//     reaper also marks the episode blocked (BlockedByEscalation semantics).
//
// Stop() terminates the reaper and is safe for daemon-lifetime trackers to
// never call (the goroutine exits with the process).
type QuotaEpisodeTracker struct {
	// mu guards the episode map. Callbacks (publisher, stateSetter) are
	// invoked OUTSIDE mu so they may re-enter tracker methods without
	// deadlocking.
	mu       sync.Mutex
	episodes map[string]*QuotaEpisode // key = agentID+"|"+providerKey
	tiers    [3]time.Duration         // [0]=12h, [1]=20h, [2]=24h
	logger   *slog.Logger

	// hooksMu guards the injected hooks; held only while copying the hook
	// value, never across a callback invocation.
	hooksMu     sync.RWMutex
	clock       func() time.Time                                      // injected for tests
	publisher   func(topic string, msg any)                           // injected bus publisher
	stateSetter func(agentID string, state AgentState, reason string) // nil-guarded agent state hook

	// reaper machinery. reaperInterval is a field so tests can inject a tiny
	// cadence; production uses the default (1 minute). Lifecycle channels
	// are guarded by mu.
	reaperInterval time.Duration
	reaperStop     chan struct{}
	reaperDone     chan struct{}
	stopOnce       sync.Once
}

// defaultReaperInterval is the production reaper cadence.
const defaultReaperInterval = 1 * time.Minute

// NewQuotaEpisodeTracker creates a tracker with default escalation thresholds
// and starts the background escalation reaper. The reaper sweeps active
// episodes on every tick and fires due escalation tiers (including the 24h
// blocked transition). Call Stop() to terminate it (no-op if already stopped).
func NewQuotaEpisodeTracker(logger *slog.Logger) *QuotaEpisodeTracker {
	t := &QuotaEpisodeTracker{
		episodes:       make(map[string]*QuotaEpisode),
		logger:         logger,
		tiers:          [3]time.Duration{12 * time.Hour, 20 * time.Hour, 24 * time.Hour},
		reaperInterval: defaultReaperInterval,
		reaperStop:     make(chan struct{}),
		reaperDone:     make(chan struct{}),
	}
	t.startReaper()
	return t
}

// SetPublisher injects a bus publisher for tests.
func (t *QuotaEpisodeTracker) SetPublisher(p func(topic string, msg any)) {
	t.hooksMu.Lock()
	t.publisher = p
	t.hooksMu.Unlock()
}

// SetClock injects a clock for tests.
func (t *QuotaEpisodeTracker) SetClock(fn func() time.Time) {
	t.hooksMu.Lock()
	t.clock = fn
	t.hooksMu.Unlock()
}

// SetStateSetter injects a callback that drives the agent state machine.
// The callback is nil-guarded: a nil setter (or nil tracker) disables state
// propagation. Callers should wire this to the loop's state machine so quota
// episodes move the agent running -> quota_wait -> blocked -> idle.
func (t *QuotaEpisodeTracker) SetStateSetter(fn func(agentID string, state AgentState, reason string)) {
	t.hooksMu.Lock()
	t.stateSetter = fn
	t.hooksMu.Unlock()
}

// setState invokes the state setter outside t.mu so the setter may safely
// re-enter tracker methods.
func (t *QuotaEpisodeTracker) setState(agentID string, state AgentState, reason string) {
	t.hooksMu.RLock()
	setter := t.stateSetter
	t.hooksMu.RUnlock()
	if setter != nil {
		setter(agentID, state, reason)
	}
}

func (t *QuotaEpisodeTracker) now() time.Time {
	t.hooksMu.RLock()
	clock := t.clock
	t.hooksMu.RUnlock()
	if clock != nil {
		return clock()
	}
	return time.Now()
}

// getPublisher returns the current publisher (or nil).
func (t *QuotaEpisodeTracker) getPublisher() func(topic string, msg any) {
	t.hooksMu.RLock()
	p := t.publisher
	t.hooksMu.RUnlock()
	return p
}

// Key returns the map key for an episode.
func Key(agentID, providerKey string) string {
	return agentID + "|" + providerKey
}

// Enter registers or extends an episode for agent+provider.
// If an episode already exists with a later unblock time, it updates without
// re-firing already-fired escalation tiers.
// On a NEW episode it publishes the initial quota_blocked event
// (from="running", to="quota_wait", escalation="") and transitions the agent
// state to StateQuotaWait.
func (t *QuotaEpisodeTracker) Enter(agentID, providerKey, modelID string, unblockAt time.Time, taskID string) {
	key := Key(agentID, providerKey)

	now := t.now()

	t.mu.Lock()
	ep, exists := t.episodes[key]
	if exists {
		// Extend if unblock is later
		if unblockAt.After(ep.UnblockAt) {
			ep.UnblockAt = unblockAt
		}
		t.mu.Unlock()
		// Fire any tiers that became overdue (pull path kept for parity).
		t.fireTiers(ep, taskID)
		return
	}

	ep = &QuotaEpisode{
		AgentID:       agentID,
		ProviderKey:   providerKey,
		ModelID:       modelID,
		CredentialKey: quotaCredentialProxy(modelID),
		UnblockAt:     unblockAt,
		StartedAt:     now,
	}
	t.episodes[key] = ep
	t.mu.Unlock()

	// Initial event on episode creation (master contract 8): the first
	// quota_blocked notification must fire even though no tier is due yet.
	// All callbacks run outside t.mu.
	t.publish(ep, "quota_blocked", "", "quota_wait", taskID)
	t.setState(agentID, StateQuotaWait, "quota_blocked")

	// Fire any immediately-overdue tiers (e.g. tiny tier config in tests).
	t.fireTiers(ep, taskID)
}

// fireTiers is the lock-free entry to the tier sweep for one episode.
func (t *QuotaEpisodeTracker) fireTiers(ep *QuotaEpisode, taskID string) {
	// Classify first under the lock, then invoke callbacks outside it.
	t.mu.Lock()
	fired := t.classifyTiersLocked(ep, taskID)
	t.mu.Unlock()

	t.dispatch(fired)
}

// tierFiring is one due-tier classification awaiting dispatch.
type tierFiring struct {
	ep     *QuotaEpisode
	reason string
	esc    string
	to     string
	taskID string
}

// classifyTiersLocked checks and records any overdue escalation tiers,
// returning the firings for dispatch. Caller must hold t.mu.
func (t *QuotaEpisodeTracker) classifyTiersLocked(ep *QuotaEpisode, taskID string) []tierFiring {
	now := t.now()
	elapsed := now.Sub(ep.StartedAt)

	var fired []tierFiring

	// Tier 0: 12h warn
	if !ep.TierFired[0] && elapsed >= t.tiers[0] {
		ep.TierFired[0] = true
		fired = append(fired, tierFiring{ep, "warn", "warn", "", taskID})
	}

	// Tier 1: 20h action recommended
	if !ep.TierFired[1] && elapsed >= t.tiers[1] {
		ep.TierFired[1] = true
		fired = append(fired, tierFiring{ep, "action_recommended", "action_recommended", "", taskID})
	}

	// Tier 2: 24h blocked — also marks the agent blocked (reuse of
	// BlockedByEscalation semantics).
	if !ep.TierFired[2] && elapsed >= t.tiers[2] {
		ep.TierFired[2] = true
		fired = append(fired, tierFiring{ep, "escalation_24h", "blocked", "blocked", taskID})
	}

	return fired
}

// dispatch emits bus events and state transitions for the given firings.
// Must be called OUTSIDE t.mu: callbacks may re-enter the tracker.
func (t *QuotaEpisodeTracker) dispatch(fired []tierFiring) {
	if len(fired) == 0 {
		return
	}
	publisher := t.getPublisher()
	for _, f := range fired {
		if publisher != nil {
			publisher("agent.quota_wait", t.buildEvent(f.ep, f.reason, f.esc, f.to, f.taskID))
		}
		if f.to == "blocked" {
			t.setState(f.ep.AgentID, StateBlocked, "quota_escalation_24h")
		}
	}
}

// buildEvent constructs a QuotaEvent for the given escalation state.
// reason and escalation are distinct vocabulary: reason names the lifecycle
// step (quota_blocked | warn | action_recommended | escalation_24h |
// quota_cleared), escalation carries the notify tier ("" | warn |
// action_recommended | blocked).
func (t *QuotaEpisodeTracker) buildEvent(ep *QuotaEpisode, reason, escalation, newState, taskID string) *QuotaEvent {
	unblockAt := ""
	if !ep.UnblockAt.IsZero() {
		unblockAt = ep.UnblockAt.Format(time.RFC3339)
	}
	return &QuotaEvent{
		AgentID:       ep.AgentID,
		TaskID:        taskID,
		From:          "running",
		To:            newState,
		Reason:        reason,
		ProviderID:    ep.ProviderKey,
		CredentialKey: ep.CredentialKey,
		ModelID:       ep.ModelID,
		UnblockAt:     unblockAt,
		Escalation:    escalation,
		Timestamp:     t.now(),
	}
}

// publish emits a bus event outside t.mu.
func (t *QuotaEpisodeTracker) publish(ep *QuotaEpisode, reason, escalation, newState, taskID string) {
	if publisher := t.getPublisher(); publisher != nil {
		publisher("agent.quota_wait", t.buildEvent(ep, reason, escalation, newState, taskID))
	}
}

// Clear ends the episode and emits a quota_cleared event.
// Idempotent: clearing a non-existent episode is a no-op (no event, no
// state transition).
func (t *QuotaEpisodeTracker) Clear(agentID, providerKey string) {
	key := Key(agentID, providerKey)

	t.mu.Lock()
	ep, exists := t.episodes[key]
	if exists {
		delete(t.episodes, key)
	}
	t.mu.Unlock()

	if !exists {
		return
	}

	// State machine: quota_wait/blocked -> idle on recovery.
	t.setState(agentID, StateIdle, "quota_cleared")

	if publisher := t.getPublisher(); publisher != nil {
		msg := &QuotaEvent{
			AgentID:       ep.AgentID,
			From:          "quota_wait",
			To:            "running",
			Reason:        "quota_cleared",
			ProviderID:    ep.ProviderKey,
			CredentialKey: ep.CredentialKey,
			ModelID:       ep.ModelID,
			Timestamp:     t.now(),
		}
		publisher("agent.quota_wait", msg)
	}
}

// BlockedByEscalation marks an episode as blocked by the 24h timer.
// Called when the max wait soft-stop is reached.
func (t *QuotaEpisodeTracker) BlockedByEscalation(agentID, providerKey string) {
	key := Key(agentID, providerKey)

	t.mu.Lock()
	ep, exists := t.episodes[key]
	if exists {
		ep.TierFired[2] = true
	}
	t.mu.Unlock()

	if !exists {
		return
	}

	t.setState(agentID, StateBlocked, "quota_escalation_24h")

	if publisher := t.getPublisher(); publisher != nil {
		msg := &QuotaEvent{
			AgentID:       ep.AgentID,
			From:          "quota_wait",
			To:            "blocked",
			Reason:        "escalation_24h",
			ProviderID:    ep.ProviderKey,
			CredentialKey: ep.CredentialKey,
			ModelID:       ep.ModelID,
			Timestamp:     t.now(),
		}
		publisher("agent.quota_wait", msg)
	}
}

// startReaper launches the background escalation reaper. It fires due tiers
// for all active episodes every reaperInterval until Stop is called.
func (t *QuotaEpisodeTracker) startReaper() {
	go func() {
		defer close(t.reaperDone)
		ticker := time.NewTicker(t.reaperInterval)
		defer ticker.Stop()
		for {
			select {
			case <-t.reaperStop:
				return
			case <-ticker.C:
				t.reapOnce()
			}
		}
	}()
}

// reapOnce sweeps all active episodes: classifies due tiers under the lock,
// then dispatches callbacks outside it.
func (t *QuotaEpisodeTracker) reapOnce() {
	t.mu.Lock()
	var all []tierFiring
	for _, ep := range t.episodes {
		all = append(all, t.classifyTiersLocked(ep, ep.AgentID)...)
	}
	t.mu.Unlock()

	t.dispatch(all)
}

// Stop terminates the reaper goroutine and waits for it to exit. Safe to
// call multiple times.
func (t *QuotaEpisodeTracker) Stop() {
	if t == nil || t.reaperStop == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.reaperStop)
	})
	// Wait for the goroutine to exit so tests can assert no leak.
	if t.reaperDone != nil {
		<-t.reaperDone
	}
}

// ActiveEpisodes returns all non-cleared episodes.
func (t *QuotaEpisodeTracker) ActiveEpisodes() []*QuotaEpisode {
	t.mu.Lock()
	defer t.mu.Unlock()

	var result []*QuotaEpisode
	for _, ep := range t.episodes {
		result = append(result, ep)
	}
	return result
}

// BlockedUntil returns when the agent+provider combo unblocks, or zero if not blocked.
func (t *QuotaEpisodeTracker) BlockedUntil(agentID, providerKey string) time.Time {
	key := Key(agentID, providerKey)

	t.mu.Lock()
	defer t.mu.Unlock()

	ep, exists := t.episodes[key]
	if !exists {
		return time.Time{}
	}
	return ep.UnblockAt
}

// quotaCredentialProxy derives a credential key from a model ID for dedup
// purposes. This is a PROXY key: the real credential identity comes from
// llm.QuotaCredentialKey(providerID, cfg), which needs the provider's
// credential config. That config is not plumbed into the tracker yet — the
// tracker only sees (agentID, providerKey, modelID) — so we derive a
// model-scoped stand-in ("<modelID>:default"). Downstream dedup treats it as
// opaque; all events for one episode share the same key, which preserves
// per-episode dedup. Revisit when credential config reaches the tracker.
func quotaCredentialProxy(modelID string) string {
	return fmt.Sprintf("%s:default", modelID)
}

// FormatDuration returns a human-readable duration string.
func FormatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
