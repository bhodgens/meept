package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// QuotaDeferralManager tracks active deferrals for a daemon.
type QuotaDeferralManager struct {
	mu        sync.Mutex
	deferrals map[string]*quotaDeferral // key = taskID
	logger    *slog.Logger
}

type quotaDeferral struct {
	TaskID      string
	SessionID   string
	ProviderKey string
	UnblockAt   time.Time
	CreatedAt   time.Time
}

// NewQuotaDeferralManager creates a new deferral manager.
func NewQuotaDeferralManager(logger *slog.Logger) *QuotaDeferralManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &QuotaDeferralManager{
		deferrals: make(map[string]*quotaDeferral),
		logger:    logger,
	}
}

// RecordDeferral records a new deferral. Returns the created deferral ID.
func (m *QuotaDeferralManager) RecordDeferral(taskID, sessionID, providerKey string, unblockAt time.Time) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	d := &quotaDeferral{
		TaskID:      taskID,
		SessionID:   sessionID,
		ProviderKey: providerKey,
		UnblockAt:   unblockAt,
		CreatedAt:   time.Now(),
	}
	m.deferrals[taskID] = d
	return taskID
}

// GetPending returns deferrals that haven't expired.
func (m *QuotaDeferralManager) GetPending(now time.Time) []*quotaDeferral {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*quotaDeferral
	for _, d := range m.deferrals {
		if now.Before(d.UnblockAt) {
			result = append(result, d)
		}
	}
	return result
}

// Remove removes a deferral by task ID.
func (m *QuotaDeferralManager) Remove(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.deferrals, taskID)
}

// CheckQuotaError checks if an error is a quota reset error and returns the
// unblock time if available.
func CheckQuotaError(err error) (*llm.QuotaResetError, bool) {
	if qe, ok := llm.AsQuotaResetError(err); ok {
		return qe, true
	}
	return nil, false
}

// FormatQuotaDeferralMessage formats a quota deferral message with provider/model/wait info.
func FormatQuotaDeferralMessage(providerID, modelID string, wait time.Duration) string {
	return fmt.Sprintf("quota limit reached on %s/%s — resets in ~%s. task paused, will resume automatically.",
		providerID, modelID, durationStr(wait))
}

// durationStr formats a duration for human-readable output.
func durationStr(d time.Duration) string {
	d = d.Round(time.Second)
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
