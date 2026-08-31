package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

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
type QuotaNotifier struct {
	mu      sync.Mutex
	bus     *bus.MessageBus
	pushSvc *PushService
	logger  *slog.Logger

	// seen tracks which (credential_key, escalation) pairs have already
	// fired to prevent duplicate notifications for the same episode.
	seen map[string]bool
}

// NewQuotaNotifier creates a notifier that subscribes to quota events.
func NewQuotaNotifier(b *bus.MessageBus, pushSvc *PushService, logger *slog.Logger) *QuotaNotifier {
	qn := &QuotaNotifier{
		bus:     b,
		pushSvc: pushSvc,
		logger:  logger,
		seen:    make(map[string]bool),
	}

	if b != nil {
		b.Subscribe("quota-notifier", "agent.quota_wait")
	}

	return qn
}

// key returns the dedup key for an event.
func key(agentID, providerKey, escalation string) string {
	return agentID + "|" + providerKey + "|" + escalation
}

// formatMessage formats the notification message based on escalation level.
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
	escalation, _ := p["escalation"].(string)
	unblockAtStr, _ := p["unblock_at"].(string)
	taskCount, _ := p["task_count"].(float64)

	if agentID == "" {
		return
	}

	// Parse unblock_at if present
	var unblockAt time.Time
	if unblockAtStr != "" {
		if t, err := time.Parse(time.RFC3339, unblockAtStr); err == nil {
			unblockAt = t
		}
	}

	// Dedup key: per credential_key per escalation tier
	dedupKey := key(agentID, credentialKey, escalation)

	qn.mu.Lock()
	if qn.seen[dedupKey] {
		qn.mu.Unlock()
		return
	}
	qn.seen[dedupKey] = true
	qn.mu.Unlock()

	// Format notification
	msgText := formatMessage(providerID, modelID, unblockAt, escalation, int(taskCount))

	// Create and send push notification
	priority := PushPriorityNormal
	if escalation == "blocked" {
		priority = PushPriorityUrgent
	} else if escalation == "warn" || escalation == "action_recommended" {
		priority = PushPriorityHigh
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
