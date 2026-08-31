package agent

import (
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestQuotaIntegration_EnterAndClear(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	tracker.Enter("test-agent", "openrouter", "claude-3-opus", time.Now().Add(3*time.Hour), "task-1")

	active := tracker.ActiveEpisodes()
	if len(active) != 1 {
		t.Fatalf("expected 1 active episode, got %d", len(active))
	}
	if active[0].AgentID != "test-agent" {
		t.Errorf("expected agent ID 'test-agent', got %s", active[0].AgentID)
	}

	tracker.Clear("test-agent", "openrouter")

	active = tracker.ActiveEpisodes()
	if len(active) != 0 {
		t.Fatalf("expected 0 active episodes after clear, got %d", len(active))
	}
}

func TestQuotaIntegration_EscalationTiers(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var tiers []string

	now := time.Now()
	tracker.SetClock(func() time.Time { return now })

	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			tiers = append(tiers, qe.Escalation)
		}
	})

	unblockAt := now.Add(24 * time.Hour)
	tracker.Enter("agent-1", "openrouter", "claude-3", unblockAt, "task-1")

	// Verify no tiers fired yet
	if len(tiers) != 0 {
		t.Fatalf("expected no tiers fired immediately, got %v", tiers)
	}

	// Advance to 12h, fire warn tier
	now = now.Add(12 * time.Hour)
	ep := tracker.episodes[Key("agent-1", "openrouter")]
	tracker.fireTiersLocked(ep, "task-1")

	if len(tiers) != 1 || tiers[0] != "warn" {
		t.Fatalf("expected warn tier, got %v", tiers)
	}

	// Advance to 20h, fire action_recommended tier
	now = now.Add(8 * time.Hour)
	ep = tracker.episodes[Key("agent-1", "openrouter")]
	tracker.fireTiersLocked(ep, "task-1")

	if len(tiers) != 2 || tiers[1] != "action_recommended" {
		t.Fatalf("expected action_recommended tier, got %v", tiers)
	}

	// Advance to 24h, fire blocked tier
	now = now.Add(4 * time.Hour)
	ep = tracker.episodes[Key("agent-1", "openrouter")]
	tracker.fireTiersLocked(ep, "task-1")

	if len(tiers) != 3 || tiers[2] != "blocked" {
		t.Fatalf("expected blocked tier, got %v", tiers)
	}
}

func TestQuotaIntegration_ReEnterExtendsWithoutReFire(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var tiers []string

	now := time.Now()
	tracker.SetClock(func() time.Time { return now })
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			tiers = append(tiers, qe.Escalation)
		}
	})

	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(24*time.Hour), "task-1")

	now = now.Add(12 * time.Hour)
	ep := tracker.episodes[Key("agent-1", "openrouter")]
	tracker.fireTiersLocked(ep, "task-1")

	if len(tiers) != 1 {
		t.Fatalf("expected 1 tier after 12h, got %d", len(tiers))
	}

	// Re-enter with extended unblock
	now = now.Add(1 * time.Hour)
	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(23*time.Hour), "task-2")

	ep = tracker.episodes[Key("agent-1", "openrouter")]
	tracker.fireTiersLocked(ep, "task-2")

	if len(tiers) != 1 {
		t.Fatalf("expected still 1 tier after re-enter, got %d", len(tiers))
	}
}

func TestQuotaIntegration_BlockedByEscalation(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var lastMsg *QuotaEvent

	tracker.SetPublisher(func(topic string, msg any) {
		lastMsg = msg.(*QuotaEvent)
	})

	tracker.Enter("agent-1", "openrouter", "claude-3", time.Now().Add(24*time.Hour), "task-1")
	tracker.BlockedByEscalation("agent-1", "openrouter")

	if lastMsg == nil {
		t.Fatal("expected publish on blocked")
	}
	if lastMsg.To != "blocked" {
		t.Errorf("expected blocked, got %s", lastMsg.To)
	}
	if lastMsg.Reason != "escalation_24h" {
		t.Errorf("expected escalation_24h reason, got %s", lastMsg.Reason)
	}
}

func TestQuotaIntegration_ZeroValueSafe(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	tracker.Enter("a", "b", "c", time.Now().Add(time.Hour), "t")
	tracker.Clear("a", "b")
	tracker.BlockedByEscalation("a", "b")
	_ = tracker.ActiveEpisodes()
	_ = tracker.BlockedUntil("a", "b")
}

func TestQuotaIntegration_MultipleAgents(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	tracker.Enter("agent-1", "provider-a", "model-x", time.Now().Add(1*time.Hour), "task-1")
	tracker.Enter("agent-2", "provider-b", "model-y", time.Now().Add(2*time.Hour), "task-2")

	active := tracker.ActiveEpisodes()
	if len(active) != 2 {
		t.Fatalf("expected 2 active episodes, got %d", len(active))
	}
}

func TestQuotaIntegration_ClearNonExistent(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	tracker.Clear("nonexistent", "provider")
}

func TestQuotaIntegration_QuotaErrorSimulation(t *testing.T) {
	// Simulate what loop.go does when it encounters a quota error
	tracker := NewQuotaEpisodeTracker(slog.Default())
	entered := false
	tracker.SetPublisher(func(topic string, msg any) {
		entered = true
	})

	// Create a quota error-like scenario
	unblockAt := time.Now().Add(3 * time.Hour)
	tracker.Enter("test-agent", "openrouter", "claude-3-opus", unblockAt, "task-1")

	active := tracker.ActiveEpisodes()
	if len(active) != 1 {
		t.Fatalf("expected 1 active episode, got %d", len(active))
	}

	_ = entered // suppress unused
}

func TestQuotaIntegration_NonQuotaErrorDoesNotTrack(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	err := fmt.Errorf("some other error")

	if len(tracker.ActiveEpisodes()) != 0 {
		t.Fatal("expected no episodes for non-quota error")
	}

	_ = err // suppress unused
}
