package agent

import (
	"log/slog"
	"testing"
	"time"
)

func TestQuotaEpisodeTracker_EnterAndClear(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	published := false
	tracker.SetPublisher(func(topic string, msg any) {
		published = true
	})

	tracker.Enter("agent-1", "openrouter", "claude-3-opus", time.Now().Add(3*time.Hour), "task-1")

	unblockAt := tracker.BlockedUntil("agent-1", "openrouter")
	if unblockAt.IsZero() {
		t.Fatal("expected blocked until time")
	}

	tracker.Clear("agent-1", "openrouter")

	unblockAt = tracker.BlockedUntil("agent-1", "openrouter")
	if !unblockAt.IsZero() {
		t.Fatalf("expected zero after clear, got %v", unblockAt)
	}
	if !published {
		t.Fatal("expected publish on clear")
	}
}

func TestQuotaEpisodeTracker_EscalationTiers(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var tiers []string

	// Inject clock: start at T=0
	now := time.Now()
	tracker.SetClock(func() time.Time { return now })

	tracker.SetPublisher(func(topic string, msg any) {
		qe := msg.(*QuotaEvent)
		tiers = append(tiers, qe.Escalation)
	})

	// Enter with 24h unblock
	unblockAt := now.Add(24 * time.Hour)
	tracker.Enter("agent-1", "openrouter", "claude-3", unblockAt, "task-1")

	// Verify no tiers fired yet (time hasn't advanced)
	if len(tiers) != 0 {
		t.Fatalf("expected no tiers fired immediately, got %v", tiers)
	}

	// Advance to 12h
	now = now.Add(12 * time.Hour)
	tracker.fireTiersLocked(tracker.episodes["agent-1|openrouter"], "task-1")

	if len(tiers) != 1 || tiers[0] != "warn" {
		t.Fatalf("expected warn tier, got %v", tiers)
	}

	// Advance to 20h
	now = now.Add(8 * time.Hour)
	tracker.fireTiersLocked(tracker.episodes["agent-1|openrouter"], "task-1")

	if len(tiers) != 2 || tiers[1] != "action_recommended" {
		t.Fatalf("expected action_recommended tier, got %v", tiers)
	}

	// Advance to 24h
	now = now.Add(4 * time.Hour)
	tracker.fireTiersLocked(tracker.episodes["agent-1|openrouter"], "task-1")

	if len(tiers) != 3 || tiers[2] != "blocked" {
		t.Fatalf("expected blocked tier, got %v", tiers)
	}
}

func TestQuotaEpisodeTracker_ReEnterExtendsWithoutReFire(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	var tiers []string

	now := time.Now()
	tracker.SetClock(func() time.Time { return now })
	tracker.SetPublisher(func(topic string, msg any) {
		qe := msg.(*QuotaEvent)
		tiers = append(tiers, qe.Escalation)
	})

	// Enter with 24h unblock
	unblockAt := now.Add(24 * time.Hour)
	tracker.Enter("agent-1", "openrouter", "claude-3", unblockAt, "task-1")

	// Advance to 12h, fire warn tier
	now = now.Add(12 * time.Hour)
	ep := tracker.episodes["agent-1|openrouter"]
	tracker.fireTiersLocked(ep, "task-1")

	if len(tiers) != 1 {
		t.Fatalf("expected 1 tier, got %d", len(tiers))
	}

	// Re-enter with extended unblock (no new tiers should fire)
	now = now.Add(1 * time.Hour)
	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(23*time.Hour), "task-2")

	if len(tiers) != 1 {
		t.Fatalf("expected still 1 tier after re-enter, got %d", len(tiers))
	}
}

func TestQuotaEpisodeTracker_BlockedByEscalation(t *testing.T) {
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
		t.Fatalf("expected blocked, got %s", lastMsg.To)
	}
}

func TestQuotaEpisodeTracker_ActiveEpisodes(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	tracker.Enter("agent-1", "openrouter", "claude-3", time.Now().Add(1*time.Hour), "task-1")
	tracker.Enter("agent-2", "openrouter", "gpt-4", time.Now().Add(2*time.Hour), "task-2")

	active := tracker.ActiveEpisodes()
	if len(active) != 2 {
		t.Fatalf("expected 2 active episodes, got %d", len(active))
	}
}

func TestQuotaEpisodeTracker_ZeroValueSafe(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	// Should not panic
	tracker.Enter("a", "b", "c", time.Now().Add(time.Hour), "t")
	tracker.Clear("a", "b")
	tracker.BlockedByEscalation("a", "b")
	_ = tracker.ActiveEpisodes()
	_ = tracker.BlockedUntil("a", "b")
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Minute, "30m"},
		{1 * time.Hour, "1h"},
		{1*time.Hour + 30*time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.input)
		if got != tt.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestQuotaCredentialKey(t *testing.T) {
	key := QuotaCredentialKey("claude-3-opus")
	if key != "claude-3-opus:default" {
		t.Errorf("expected 'claude-3-opus:default', got %q", key)
	}
}
