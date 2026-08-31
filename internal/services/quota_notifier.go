package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// taskCountAbsent is the sentinel taskCount value indicating the event
// carried no task_count field; formatMessage omits the count clause.
const taskCountAbsent = -1

// QuotaNotification represents a formatted quota notification message.
type QuotaNotification struct {
	AgentID    string
	ProviderID string
	ModelID    string
	Title      string
	Message    string
	Priority   PushPriority
	Escalation string // "", "warn", "action_recommended", "blocked"
}

// QuotaNotifier subscribes to agent.quota_wait bus events and delivers
// push notifications with per-credential-key dedup.
//
// Lifecycle: NewQuotaNotifier subscribes to the bus AND auto-starts the
// event pump, so a daemon component only needs to construct it — no extra
// Start call required. Call Stop() to unsubscribe and terminate the pump
// goroutine (optional for daemon-lifetime components).
type QuotaNotifier struct {
	mu      sync.Mutex
	bus     *bus.MessageBus
	pushSvc *PushService
	logger  *slog.Logger

	// seen tracks which (credential_key, escalation) pairs have already
	// fired to prevent duplicate notifications for the same episode.
	// Entries are cleared per-episode on quota_cleared so a subsequent
	// re-block of the same credential notifies again.
	seen map[string]bool

	// pump state (created in NewQuotaNotifier, guarded by mu)
	sub    *bus.Subscriber
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewQuotaNotifier creates a notifier that subscribes to quota events and
// auto-starts the event pump goroutine. pushSvc is nil-guarded: with a nil
// push service the notifier still subscribes and drains events but skips
// delivery (logged at Debug).
func NewQuotaNotifier(b *bus.MessageBus, pushSvc *PushService, logger *slog.Logger) *QuotaNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	qn := &QuotaNotifier{
		bus:     b,
		pushSvc: pushSvc,
		logger:  logger,
		seen:    make(map[string]bool),
	}

	if b != nil {
		qn.sub = b.Subscribe("quota-notifier", "agent.quota_wait")
	}
	// Auto-start the pump so callers need zero extra lifecycle calls.
	qn.start()

	return qn
}

// start spawns the pump goroutine draining sub.Channel into processEvent.
// Caller must hold qn.mu.
func (qn *QuotaNotifier) start() {
	if qn.sub == nil {
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	qn.stopCh = stopCh
	qn.doneCh = doneCh
	go qn.pump(stopCh, doneCh)
}

// pump drains the subscriber channel until stopCh is closed or the channel
// closes (bus shutdown). Channels are passed by value so a concurrent Stop
// resetting the struct fields cannot race the goroutine's exit.
func (qn *QuotaNotifier) pump(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)
	for {
		select {
		case <-stopCh:
			return
		case msg, ok := <-qn.sub.Channel:
			if !ok {
				return
			}
			qn.processEvent(msg)
		}
	}
}

// Start ensures the event pump is running. Present for explicit lifecycle
// symmetry with Stop; NewQuotaNotifier already auto-starts the pump, so this
// is a no-op in normal use.
func (qn *QuotaNotifier) Start() {
	qn.mu.Lock()
	defer qn.mu.Unlock()
	if qn.sub != nil && qn.stopCh == nil {
		qn.start()
	}
}

// Stop unsubscribes from the bus and terminates the pump goroutine, waiting
// for it to exit. Safe to call multiple times (subsequent calls are no-ops),
// and a following Start() may relaunch the pump.
func (qn *QuotaNotifier) Stop() {
	qn.mu.Lock()
	stopCh := qn.stopCh
	doneCh := qn.doneCh
	sub := qn.sub
	qn.stopCh = nil
	qn.doneCh = nil
	// Close under mu so concurrent Start/Stop calls cannot race the close.
	if stopCh != nil {
		close(stopCh)
	}
	qn.mu.Unlock()

	if sub != nil && qn.bus != nil {
		qn.bus.Unsubscribe(sub)
	}
	if doneCh != nil {
		<-doneCh
	}
}

// key returns the dedup key for an event.
func key(agentID, providerKey, escalation string) string {
	return agentID + "|" + providerKey + "|" + escalation
}

// formatMessage formats the notification message based on escalation level.
// A taskCount of taskCountAbsent (-1) omits the "N task(s) affected" clause
// (the event carried no task_count field).
func formatMessage(providerID, modelID string, unblockAt time.Time, escalation string, taskCount int) string {
	dur := formatDuration(time.Until(unblockAt))

	switch escalation {
	case "warn":
		return fmt.Sprintf("still quota-blocked on %s/%s (%s).", providerID, modelID, dur)
	case "action_recommended":
		return fmt.Sprintf("quota still blocked on %s/%s (%s) — action recommended.", providerID, modelID, dur)
	case "blocked":
		return fmt.Sprintf("quota blocked on %s/%s for 24h — manual action required. agent blocked.", providerID, modelID)
	case "quota_cleared":
		return fmt.Sprintf("quota recovered on %s/%s — resumed.", providerID, modelID)
	default: // initial blocked
		if taskCount == taskCountAbsent {
			return fmt.Sprintf("quota limit reached on %s/%s — resets in ~%s. will resume automatically.",
				providerID, modelID, dur)
		}
		return fmt.Sprintf("quota limit reached on %s/%s — resets in ~%s. %d task(s) affected. will resume automatically.",
			providerID, modelID, dur, taskCount)
	}
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "now"
	}
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

// processEvent handles a single quota event from the bus.
func (qn *QuotaNotifier) processEvent(msg *models.BusMessage) {
	if msg == nil || len(msg.Payload) == 0 {
		return
	}

	// BusMessage.Payload is json.RawMessage; decode to a generic object.
	var p map[string]any
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return
	}

	agentID, _ := p["agent_id"].(string)
	providerID, _ := p["provider_id"].(string)
	modelID, _ := p["model_id"].(string)
	credentialKey, _ := p["credential_key"].(string)
	reason, _ := p["reason"].(string)
	escalation, _ := p["escalation"].(string)
	unblockAtStr, _ := p["unblock_at"].(string)
	taskCount, hasTaskCount := p["task_count"].(float64)

	if agentID == "" {
		return
	}

	// Normalize cleared payloads: the tracker sends reason="quota_cleared"
	// with escalation="" — route through the quota_cleared branch so the
	// recovery text renders (escalation stays distinct from reason
	// everywhere else).
	if escalation == "" && reason == "quota_cleared" {
		escalation = "quota_cleared"
	}

	// Parse unblock_at if present
	var unblockAt time.Time
	if unblockAtStr != "" {
		if t, err := time.Parse(time.RFC3339, unblockAtStr); err == nil {
			unblockAt = t
		}
	}

	// Cleared resets the dedup window for this episode: delete ALL seen
	// entries for agentID|credentialKey so a cleared-then-reblocked
	// credential notifies again from tier 0.
	if escalation == "quota_cleared" {
		prefix := agentID + "|" + credentialKey + "|"
		qn.mu.Lock()
		for k := range qn.seen {
			if strings.HasPrefix(k, prefix) {
				delete(qn.seen, k)
			}
		}
		qn.mu.Unlock()
		// Cleared events themselves are not deduped: always render.
	} else {
		// Dedup key: per credential_key per escalation tier
		dedupKey := key(agentID, credentialKey, escalation)

		qn.mu.Lock()
		if qn.seen[dedupKey] {
			qn.mu.Unlock()
			return
		}
		qn.seen[dedupKey] = true
		qn.mu.Unlock()
	}

	// Format notification
	taskCountVal := taskCountAbsent
	if hasTaskCount {
		taskCountVal = int(taskCount)
	}
	msgText := formatMessage(providerID, modelID, unblockAt, escalation, taskCountVal)

	// Create and send push notification
	priority := PushPriorityNormal
	switch escalation {
	case "blocked":
		priority = PushPriorityUrgent
	case "warn", "action_recommended":
		priority = PushPriorityHigh
	}

	if qn.pushSvc == nil {
		// Nil push service: nothing to deliver to. Log and return — the
		// notifier still consumed the event and advanced dedup state.
		qn.logger.Debug("quota notification skipped: no push service",
			"agent", agentID,
			"provider", providerID,
			"escalation", escalation,
		)
		return
	}

	_, err := qn.pushSvc.Push(context.Background(), &PushRequest{
		Content:  msgText,
		Type:     PushTypeAlert,
		Priority: priority,
		Source:   "quota",
	})
	if err != nil {
		qn.logger.Warn("quota notification push failed", "error", err)
	}

	qn.logger.Info("quota notification delivered",
		"agent", agentID,
		"provider", providerID,
		"escalation", escalation,
	)
}

// Cleanup removes old dedup entries (for memory safety on long runs).
func (qn *QuotaNotifier) Cleanup() {
	qn.mu.Lock()
	defer qn.mu.Unlock()
	// Keep only last 100 entries
	if len(qn.seen) > 100 {
		qn.seen = make(map[string]bool)
	}
}
