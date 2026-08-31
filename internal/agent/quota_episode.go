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

// QuotaBlockStatus represents an active quota block on a provider credential.
type QuotaBlockStatus struct {
	AgentID       string
	ProviderKey   string
	ModelID       string
	CredentialKey string
	UnblockAt     time.Time
	EscalationTier int // 0=new, 1=12h, 2=20h, 3=24h(blocked)
}

// QuotaEpisode tracks a single quota block episode for an agent+provider combo.
type QuotaEpisode struct {
	AgentID     string
	ProviderKey string
	ModelID     string
	CredentialKey string
	UnblockAt   time.Time
	StartedAt   time.Time
	TierFired   [3]bool // tier 0=12h, tier 1=20h, tier 2=24h
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
type QuotaEpisodeTracker struct {
	mu         sync.Mutex
	episodes   map[string]*QuotaEpisode // key = agentID+"|"+providerKey
	logger     *slog.Logger
	clock      func() time.Time      // injected for tests
	tiers      [3]time.Duration      // [0]=12h, [1]=20h, [2]=24h
	publisher  func(topic string, msg any) // injected bus publisher
}

// NewQuotaEpisodeTracker creates a tracker with default escalation thresholds.
func NewQuotaEpisodeTracker(logger *slog.Logger) *QuotaEpisodeTracker {
	return &QuotaEpisodeTracker{
		episodes: make(map[string]*QuotaEpisode),
		logger:   logger,
		tiers:    [3]time.Duration{12 * time.Hour, 20 * time.Hour, 24 * time.Hour},
	}
}

// SetPublisher injects a bus publisher for tests.
func (t *QuotaEpisodeTracker) SetPublisher(p func(topic string, msg any)) {
	t.publisher = p
}

// SetClock injects a clock for tests.
func (t *QuotaEpisodeTracker) SetClock(fn func() time.Time) {
	t.clock = fn
}

func (t *QuotaEpisodeTracker) now() time.Time {
	if t.clock != nil {
		return t.clock()
	}
	return time.Now()
}

// Key returns the map key for an episode.
func Key(agentID, providerKey string) string {
	return agentID + "|" + providerKey
}

// Enter registers or extends an episode for agent+provider.
// If an episode already exists with a later unblock time, it updates without
// re-firing already-fired escalation tiers.
func (t *QuotaEpisodeTracker) Enter(agentID, providerKey, modelID string, unblockAt time.Time, taskID string) {
	key := Key(agentID, providerKey)

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	ep, exists := t.episodes[key]

	if exists {
		// Extend if unblock is later
		if unblockAt.After(ep.UnblockAt) {
			ep.UnblockAt = unblockAt
		}
		// Don't re-fire already-fired tiers
		if ep.TierFired[0] || ep.TierFired[1] || ep.TierFired[2] {
			return
		}
	} else {
		ep = &QuotaEpisode{
			AgentID:       agentID,
			ProviderKey:   providerKey,
			ModelID:       modelID,
			CredentialKey: QuotaCredentialKey(modelID),
			UnblockAt:     unblockAt,
			StartedAt:     now,
		}
		t.episodes[key] = ep
	}

	// Schedule/fire escalation tiers
	t.fireTiersLocked(ep, taskID)
}

// fireTiersLocked checks and fires any overdue escalation tiers.
// Caller must hold t.mu.
func (t *QuotaEpisodeTracker) fireTiersLocked(ep *QuotaEpisode, taskID string) {
	now := t.now()
	elapsed := now.Sub(ep.StartedAt)

	// Tier 0: 12h warn
	if !ep.TierFired[0] && elapsed >= t.tiers[0] {
		ep.TierFired[0] = true
		t.publishLocked(ep, "warn", "", taskID)
	}

	// Tier 1: 20h action recommended
	if !ep.TierFired[1] && elapsed >= t.tiers[1] {
		ep.TierFired[1] = true
		t.publishLocked(ep, "action_recommended", "", taskID)
	}

	// Tier 2: 24h blocked
	if !ep.TierFired[2] && elapsed >= t.tiers[2] {
		ep.TierFired[2] = true
		t.publishLocked(ep, "blocked", "blocked", taskID)
	}
}

// publishLocked emits a bus event for the given escalation state.
// Caller must hold t.mu.
func (t *QuotaEpisodeTracker) publishLocked(ep *QuotaEpisode, reason, newState, taskID string) {
	if t.publisher == nil {
		return
	}

	unblockAt := ""
	if !ep.UnblockAt.IsZero() {
		unblockAt = ep.UnblockAt.Format(time.RFC3339)
	}

	msg := &QuotaEvent{
		AgentID:       ep.AgentID,
		TaskID:        taskID,
		From:          "running",
		To:            newState,
		Reason:        reason,
		ProviderID:    ep.ProviderKey,
		CredentialKey: ep.CredentialKey,
		ModelID:       ep.ModelID,
		UnblockAt:     unblockAt,
		Escalation:    reason,
		Timestamp:     t.now(),
	}

	t.publisher("agent.quota_wait", msg)
}

// Clear ends the episode and emits a quota_cleared event.
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

	if t.publisher == nil {
		return
	}

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

	t.publisher("agent.quota_wait", msg)
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

	if !exists || t.publisher == nil {
		return
	}

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

	t.publisher("agent.quota_wait", msg)
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

// QuotaCredentialKey derives a credential key from a model ID for dedup purposes.
func QuotaCredentialKey(modelID string) string {
	// Simplified: use model ID as proxy. In production, this would extract
	// the actual credential key from the provider config.
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
