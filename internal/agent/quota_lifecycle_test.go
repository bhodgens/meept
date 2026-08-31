package agent

import (
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStateRecorder captures state transitions issued by the tracker's
// stateSetter without needing a full AgentLoop.
type fakeStateRecorder struct {
	mu      sync.Mutex
	agents  []string
	states  []AgentState
	reasons []string
}

func (r *fakeStateRecorder) record(agentID string, state AgentState, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = append(r.agents, agentID)
	r.states = append(r.states, state)
	r.reasons = append(r.reasons, reason)
}

func (r *fakeStateRecorder) snapshot() (agents []string, states []AgentState, reasons []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.agents...), append([]AgentState{}, r.states...), append([]string{}, r.reasons...)
}

// newTestTracker builds a tracker with a tiny reaper interval so tests can
// exercise the background reaper without wall-clock waits.
func newTestTracker(interval time.Duration) *QuotaEpisodeTracker {
	t := NewQuotaEpisodeTracker(slog.Default())
	// The production constructor already started a reaper with the default
	// cadence; stop it and rebuild the reaper machinery with the test
	// cadence on a fresh set of lifecycle channels.
	t.Stop()
	t.reaperStop = make(chan struct{})
	t.reaperDone = make(chan struct{})
	t.stopOnce = sync.Once{}
	t.reaperInterval = interval
	t.startReaper()
	return t
}

func TestQuotaEpisodeTracker_InitialEventOnNewEpisode(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	defer tracker.Stop()

	var events []*QuotaEvent
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			events = append(events, qe)
		}
	})

	now := time.Now()
	tracker.SetClock(func() time.Time { return now })

	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(3*time.Hour), "task-1")

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event on new episode, got %d", len(events))
	}
	ev := events[0]
	if ev.Reason != "quota_blocked" {
		t.Errorf("expected reason quota_blocked, got %q", ev.Reason)
	}
	if ev.From != "running" {
		t.Errorf("expected from running, got %q", ev.From)
	}
	if ev.To != "quota_wait" {
		t.Errorf("expected to quota_wait, got %q", ev.To)
	}
	if ev.Escalation != "" {
		t.Errorf("expected empty escalation on initial event, got %q", ev.Escalation)
	}

	// Re-entering the same episode must NOT publish a second initial event.
	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(4*time.Hour), "task-1")
	if len(events) != 1 {
		t.Fatalf("expected no additional initial event on re-enter, got %d total", len(events))
	}
}

func TestQuotaEpisodeTracker_ReaperFiresTiers(t *testing.T) {
	tracker := newTestTracker(5 * time.Millisecond)
	defer tracker.Stop()

	var mu sync.Mutex
	var escalations []string
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			mu.Lock()
			escalations = append(escalations, qe.Escalation)
			mu.Unlock()
		}
	})

	now := time.Now()
	var nowAtomic atomic.Int64 // reaper goroutine reads the clock concurrently
	nowAtomic.Store(now.UnixNano())
	tracker.SetClock(func() time.Time { return time.Unix(0, nowAtomic.Load()) })

	// Enter with a 25h episode and tiers tiny enough for the reaper.
	tracker.mu.Lock()
	tracker.tiers = [3]time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
	tracker.mu.Unlock()

	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(1*time.Hour), "task-1")

	// Advance the injected clock in steps; wait for the reaper to sweep.
	deadline := time.Now().Add(3 * time.Second)
	targets := []string{"warn", "action_recommended", "blocked"}
	for _, target := range targets {
		now = now.Add(11 * time.Millisecond) // pass next tier boundary
		nowAtomic.Store(now.UnixNano())
		for {
			mu.Lock()
			got := len(escalations) > 0 && escalations[len(escalations)-1] == target
			mu.Unlock()
			if got {
				break
			}
			if time.Now().After(deadline) {
				mu.Lock()
				t.Fatalf("reaper never fired tier %q; got %v", target, escalations)
				mu.Unlock()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}

	// Total tier events: 3 tiers + 1 initial ("" escalation) from Enter.
	mu.Lock()
	defer mu.Unlock()
	if len(escalations) != 4 {
		t.Fatalf("expected 4 events (initial + 3 tiers), got %v", escalations)
	}
}

func TestQuotaEpisodeTracker_Reaper24hMarksBlocked(t *testing.T) {
	tracker := newTestTracker(2 * time.Millisecond)
	defer tracker.Stop()

	var mu sync.Mutex
	var tos []string
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			mu.Lock()
			tos = append(tos, qe.To)
			mu.Unlock()
		}
	})

	now := time.Now()
	var reaperClock atomic.Int64 // reaper goroutine reads the clock concurrently
	reaperClock.Store(now.UnixNano())
	tracker.SetClock(func() time.Time { return time.Unix(0, reaperClock.Load()) })

	tracker.mu.Lock()
	tracker.tiers = [3]time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 15 * time.Millisecond}
	tracker.mu.Unlock()

	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(1*time.Hour), "task-1")

	// Advance past all three tiers; the reaper should fire the 24h tier and
	// the episode's TierFired[2] should be set (blocked semantics).
	now = now.Add(20 * time.Millisecond)
	reaperClock.Store(now.UnixNano())
	deadline := time.Now().Add(3 * time.Second)
	for {
		tracker.mu.Lock()
		ep := tracker.episodes[Key("agent-1", "openrouter")]
		blocked := ep != nil && ep.TierFired[2]
		tracker.mu.Unlock()
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reaper never marked episode blocked at 24h tier")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The reaper sets TierFired under the lock but dispatches events
	// outside it — poll for the published event instead of reading tos
	// immediately (otherwise this check races the dispatch).
	deadline2 := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		found := false
		for _, to := range tos {
			if to == "blocked" {
				found = true
			}
		}
		mu.Unlock()
		if found {
			return
		}
		if time.Now().After(deadline2) {
			mu.Lock()
			t.Fatalf("expected a blocked event, got %v", tos)
			mu.Unlock()
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestQuotaEpisodeTracker_StopNoGoroutineLeak(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())

	// Give the reaper goroutine a moment to start.
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	tracker.Stop()
	tracker.Stop() // idempotent

	// Allow any stragglers to exit.
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	if after > before {
		t.Fatalf("goroutine leak after Stop: before=%d after=%d", before, after)
	}
}

func TestQuotaEpisodeTracker_SetStateSetterTransitions(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	defer tracker.Stop()

	rec := &fakeStateRecorder{}
	tracker.SetStateSetter(rec.record)

	var mu sync.Mutex
	var escalations []string
	tracker.SetPublisher(func(topic string, msg any) {
		if qe, ok := msg.(*QuotaEvent); ok {
			mu.Lock()
			escalations = append(escalations, qe.Escalation)
			mu.Unlock()
		}
	})

	now := time.Now()
	tracker.SetClock(func() time.Time { return now })

	// Enter -> StateQuotaWait
	tracker.Enter("agent-1", "openrouter", "claude-3", now.Add(1*time.Hour), "task-1")

	// Manually advance to the 24h tier -> StateBlocked
	now = now.Add(24 * time.Hour)
	tracker.mu.Lock()
	ep := tracker.episodes[Key("agent-1", "openrouter")]
	tracker.mu.Unlock()
	tracker.fireTiers(ep, "task-1")

	// Clear -> StateIdle
	tracker.Clear("agent-1", "openrouter")

	agents, states, reasons := rec.snapshot()
	if len(states) != 3 {
		t.Fatalf("expected 3 state transitions, got %v over %v", states, reasons)
	}
	if states[0] != StateQuotaWait || reasons[0] != "quota_blocked" {
		t.Errorf("expected StateQuotaWait/quota_blocked, got %v/%v", states[0], reasons[0])
	}
	if states[1] != StateBlocked || reasons[1] != "quota_escalation_24h" {
		t.Errorf("expected StateBlocked/quota_escalation_24h, got %v/%v", states[1], reasons[1])
	}
	if states[2] != StateIdle || reasons[2] != "quota_cleared" {
		t.Errorf("expected StateIdle/quota_cleared, got %v/%v", states[2], reasons[2])
	}
	for _, a := range agents {
		if a != "agent-1" {
			t.Errorf("expected agent-1, got %q", a)
		}
	}
}

func TestQuotaEpisodeTracker_NilStateSetterSafe(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	defer tracker.Stop()

	// No SetStateSetter call — all lifecycle paths must not panic.
	tracker.Enter("a", "b", "c", time.Now().Add(time.Hour), "t")
	tracker.Clear("a", "b")
	tracker.BlockedByEscalation("a", "b")
}

func TestQuotaEpisodeTracker_ClearSetsIdle(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	defer tracker.Stop()

	rec := &fakeStateRecorder{}
	tracker.SetStateSetter(rec.record)

	// Clear with no episode: no state transition (idempotent).
	tracker.Clear("ghost", "provider")

	tracker.Enter("agent-1", "openrouter", "claude-3", time.Now().Add(time.Hour), "task-1")
	tracker.Clear("agent-1", "openrouter")

	agents, states, reasons := rec.snapshot()
	if len(states) != 2 {
		t.Fatalf("expected 2 transitions (enter+clear), got %v/%v", states, reasons)
	}
	if states[1] != StateIdle || reasons[1] != "quota_cleared" {
		t.Errorf("expected StateIdle/quota_cleared on clear, got %v/%v", states[1], reasons[1])
	}
	if agents[0] != "agent-1" || agents[1] != "agent-1" {
		t.Errorf("expected agent-1 both times, got %v", agents)
	}
}

func TestQuotaEpisodeTracker_CredentialProxy(t *testing.T) {
	tracker := NewQuotaEpisodeTracker(slog.Default())
	defer tracker.Stop()

	tracker.Enter("agent-1", "openrouter", "claude-3", time.Now().Add(time.Hour), "t")

	tracker.mu.Lock()
	ep := tracker.episodes[Key("agent-1", "openrouter")]
	cred := ep.CredentialKey
	tracker.mu.Unlock()

	// Proxy derivation: modelID + ":default" until credential config is
	// plumbed to the tracker.
	if cred != "claude-3:default" {
		t.Errorf("expected proxy credential key 'claude-3:default', got %q", cred)
	}
}
