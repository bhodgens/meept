package voice

// Gate tests for contract C6 (master.md, quoted verbatim): hard timeout
// voice.supervisor_timeout_ms, on timeout/error/quota the caller hears the
// fallback line and the call CONTINUES - a voice call is never quota-parked.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// blockingChatter blocks past any deadline and records Ask invocations.
type blockingChatter struct {
	mu   sync.Mutex
	asks int
}

func (b *blockingChatter) Ask(_ context.Context, _ string) (string, error) {
	b.mu.Lock()
	b.asks++
	b.mu.Unlock()
	<-time.After(5 * time.Second) // far past the supervisor timeout
	return "ALLOW", nil
}

func (b *blockingChatter) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.asks
}

// scriptedChatter returns canned answers and records calls.
type scriptedChatter struct {
	mu      sync.Mutex
	asks    int
	reply   string
	err     error
	errOnce bool
}

func (s *scriptedChatter) Ask(_ context.Context, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks++
	if s.err != nil {
		return "", s.err
	}
	return s.reply, nil
}

func (s *scriptedChatter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asks
}

func supervisorCfg() SupervisorConfig {
	return SupervisorConfig{
		Enabled:      true,
		Timeout:      400 * time.Millisecond, // C3: supervisor_timeout_ms(400)
		FallbackLine: "I cannot confirm that yet.",
	}
}

// TestSupervisorTimeout pins C6: with a chatter that blocks longer than
// supervisor_timeout_ms, Check returns the fallback verdict fast (within
// timeout + 50ms), the call CONTINUES (fallback Allowed=false verdict with
// the fallback line as the spoken text), and nothing parks.
func TestSupervisorTimeout(t *testing.T) {
	sup := NewSupervisor(supervisorCfg(), &blockingChatter{}, testLogger())

	start := time.Now()
	v, err := sup.Check(context.Background(), "cc-sup-1", "What time is my meeting?")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Check returned error on timeout: %v (C6: timeout maps to a fallback verdict, never an error)", err)
	}
	if elapsed > 450*time.Millisecond {
		t.Errorf("Check took %v, want <= timeout(400ms) + 50ms", elapsed)
	}
	if v.Allowed {
		t.Errorf("timeout verdict Allowed=true, want false (fallback line plays instead)")
	}
	if !strings.Contains(strings.ToLower(v.Reason), "timeout") {
		t.Errorf("timeout verdict Reason = %q, want it to name timeout", v.Reason)
	}
}

// TestSupervisorQuotaNeverParks pins the C6 rule verbatim: a quota error
// from the model maps to the same fallback verdict shape with nil error -
// no park, no retry (one Ask, never more).
func TestSupervisorQuotaNeverParks(t *testing.T) {
	quotaErr := &llm.QuotaResetError{
		ProviderID: "test",
		ModelID:    "test-model",
		Code:       "usage_limit_reached",
	}
	ch := &scriptedChatter{err: quotaErr}
	sup := NewSupervisor(supervisorCfg(), ch, testLogger())

	v, err := sup.Check(context.Background(), "cc-sup-2", "Say something factual.")
	if err != nil {
		t.Fatalf("Check surfaced quota error %v (C6: a voice call is never quota-parked)", err)
	}
	if v.Allowed {
		t.Errorf("quota verdict Allowed=true, want false (fallback line)")
	}
	if got := ch.count(); got != 1 {
		t.Errorf("Ask called %d times on quota error, want exactly 1 (no retry)", got)
	}
}

// TestSupervisorVerdicts pins the parse contract: ALLOW passes, DENY fails
// with the reason carried, and an unparseable reply denies (fail-closed).
func TestSupervisorVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		reply   string
		allowed bool
	}{
		{"allow", "ALLOW", true},
		{"deny", "DENY: speculates about user finances", false},
		{"unparseable", "maybe? probably fine", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &scriptedChatter{reply: tc.reply}
			sup := NewSupervisor(supervisorCfg(), ch, testLogger())
			v, err := sup.Check(context.Background(), "cc-sup-3", "some sentence")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if v.Allowed != tc.allowed {
				t.Errorf("reply %q: Allowed = %v, want %v", tc.reply, v.Allowed, tc.allowed)
			}
			if !tc.allowed && tc.name == "deny" && !strings.Contains(strings.ToLower(v.Reason), "deny") {
				t.Errorf("deny verdict Reason = %q, want it to carry the deny reason", v.Reason)
			}
		})
	}
}

// TestSupervisorNever pins the NeverPatterns pre-check: a matching sentence
// is denied WITHOUT any model call (zero Ask invocations).
func TestSupervisorNever(t *testing.T) {
	ch := &scriptedChatter{reply: "ALLOW"}
	cfg := supervisorCfg()
	cfg.NeverPatterns = []string{"home address"}
	sup := NewSupervisor(cfg, ch, testLogger())

	v, err := sup.Check(context.Background(), "cc-sup-4", "Your home address is 123 Main St.")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if v.Allowed {
		t.Errorf("never-pattern sentence Allowed=true, want denied without a model call")
	}
	if got := ch.count(); got != 0 {
		t.Errorf("Ask called %d times for a never-pattern match, want 0 (pre-check, no model spend)", got)
	}
}

// TestSupervisorDisabled pins the toggle: supervisor_enabled=false skips
// Check entirely (Allowed=true, Reason names disabled, no Ask).
func TestSupervisorDisabled(t *testing.T) {
	ch := &scriptedChatter{reply: "DENY: anything"}
	cfg := supervisorCfg()
	cfg.Enabled = false
	sup := NewSupervisor(cfg, ch, testLogger())

	v, err := sup.Check(context.Background(), "cc-sup-5", "any sentence")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !v.Allowed {
		t.Errorf("disabled supervisor Allowed=false, want true (gate off)")
	}
	if got := ch.count(); got != 0 {
		t.Errorf("Ask called %d times with the gate disabled, want 0", got)
	}
}

// TestSplitSentences pins the draft splitter the talk loop pipelines with.
func TestSplitSentences(t *testing.T) {
	got := SplitSentences("First sentence. Second one! Third?")
	if len(got) != 3 {
		t.Fatalf("SplitSentences returned %d sentences (%v), want 3, order kept", len(got), got)
	}
	if got[0] != "First sentence." {
		t.Errorf("first sentence = %q, want %q", got[0], "First sentence.")
	}
	if s := SplitSentences(""); len(s) != 0 {
		t.Errorf("empty draft split = %v, want no sentences", s)
	}
}
