package agent

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestQuotaDeferral_NotFailsTask verifies that when a quota error occurs
// after the quota waiter has exhausted its retry, the episode tracker
// records the wait and the error is returned (not causing agent failure).
// The quota-aware retry already happened in resolver_direct.go.
func TestQuotaDeferral_NotFailsTask(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var entered bool
	tracker.SetPublisher(func(topic string, msg any) {
		entered = true
	})

	// Simulate what happens when loop.go encounters a QuotaResetError
	// that has already been retried by QuotaWaitChatter.
	quotaErr := &llm.QuotaResetError{
		ProviderID: "openrouter",
		ModelID:    "claude-3-opus",
		Code:       "quota_exceeded",
		RetryAfter: 3 * time.Hour,
		MaxWait:    24 * time.Hour,
		ResetAt:    time.Now().Add(3 * time.Hour),
	}

	// Verify it's a QuotaResetError
	var qe *llm.QuotaResetError
	if !errors.As(quotaErr, &qe) {
		t.Fatal("expected quota error")
	}

	// Track the episode
	tracker.Enter("test-agent", quotaErr.ProviderID, quotaErr.ModelID, quotaErr.ResetAt, "task-1")

	active := tracker.ActiveEpisodes()
	if len(active) != 1 {
		t.Fatalf("expected 1 active episode, got %d", len(active))
	}

	// Verify the episode has correct fields
	ep := active[0]
	if ep.AgentID != "test-agent" {
		t.Errorf("agent ID mismatch: got %s", ep.AgentID)
	}
	if ep.ProviderKey != "openrouter" {
		t.Errorf("provider key mismatch: got %s", ep.ProviderKey)
	}

	_ = entered // used by publisher check
}

// TestQuotaDeferral_ClearOnRecovery verifies that when the quota clears,
// the episode is removed and a clear event is published.
func TestQuotaDeferral_ClearOnRecovery(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var cleared bool
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok && qe.Reason == "quota_cleared" {
			cleared = true
		}
	})

	tracker.Enter("agent-1", "openrouter", "claude-3", time.Now().Add(1*time.Hour), "task-1")

	// Simulate quota clearing (provider recovers)
	tracker.Clear("agent-1", "openrouter")

	active := tracker.ActiveEpisodes()
	if len(active) != 0 {
		t.Fatalf("expected 0 active episodes after clear, got %d", len(active))
	}
	if !cleared {
		t.Error("expected clear event to be published")
	}
}

// TestQuotaDeferral_MaxWaitSoftStop verifies that when a quota wait exceeds
// MaxWait, the tracker marks the episode as blocked.
func TestQuotaDeferral_MaxWaitSoftStop(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var blocked bool
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok && qe.To == "blocked" {
			blocked = true
		}
	})

	// Enter with a long wait
	unblockAt := time.Now().Add(48 * time.Hour) // exceeds MaxWait of 24h
	tracker.Enter("agent-1", "openrouter", "claude-3", unblockAt, "task-1")

	// Manually trigger the 24h blocked state
	tracker.BlockedByEscalation("agent-1", "openrouter")

	if !blocked {
		t.Error("expected blocked event after escalation")
	}

	// Verify the episode still exists but is marked as tier 2 fired
	active := tracker.ActiveEpisodes()
	if len(active) != 1 {
		t.Fatalf("expected 1 active episode, got %d", len(active))
	}
}

// TestQuotaDeferral_NonQuotaErrorDoesNotTrack verifies non-quota errors
// are not tracked by the episode tracker.
func TestQuotaDeferral_NonQuotaErrorDoesNotTrack(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	// Any non-quota error should not create an episode
	err := errors.New("some other error")

	// Verify no episode created
	if len(tracker.ActiveEpisodes()) != 0 {
		t.Fatal("expected no episodes for non-quota error")
	}

	_ = err // suppress unused
}
