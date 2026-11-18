package voice

// Gate tests for contract C6 (master.md, quoted verbatim): hard timeout
// voice.supervisor_timeout_ms, on timeout/error/quota the caller hears the
// fallback line and the call CONTINUES - a voice call is never quota-parked.

import (
	"context"
	"fmt"
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

// scriptedChatter returns canned answers and records calls.
type scriptedChatter struct {
	mu    sync.Mutex
	asks  int
	reply string
	err   error
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

// --- allow-cache + counters -------------------------------------------------

// withCacheTTL pins the TTL seam for one test body (sequential package
// runs, same discipline as silenceWindowUnit).
func withCacheTTL(t *testing.T, ttl time.Duration) {
	t.Helper()
	prev := supervisorCacheTTL
	supervisorCacheTTL = ttl
	t.Cleanup(func() { supervisorCacheTTL = prev })
}

// TestSupervisorCacheMemoizesAllows pins the core win: a repeat ALLOW
// sentence costs one Ask, not two, and the cached verdict still Allows.
// Normalization: case/whitespace variants hit the same entry.
func TestSupervisorCacheMemoizesAllows(t *testing.T) {
	withCacheTTL(t, time.Hour)
	ch := &scriptedChatter{reply: "ALLOW"}
	sup := NewSupervisor(supervisorCfg(), ch, testLogger())

	v1, err := sup.Check(context.Background(), "cc-cache-1", "The meeting is at noon.")
	if err != nil || !v1.Allowed {
		t.Fatalf("first check = %+v err %v, want allowed", v1, err)
	}
	v2, err := sup.Check(context.Background(), "cc-cache-1", "the   meeting is AT noon.")
	if err != nil || !v2.Allowed {
		t.Fatalf("cached check = %+v err %v, want allowed", v2, err)
	}
	if v2.Reason != "cached" {
		t.Errorf("cached verdict Reason = %q, want %q", v2.Reason, "cached")
	}
	if got := ch.count(); got != 1 {
		t.Errorf("Ask called %d times for a normalized repeat, want 1 (cache hit)", got)
	}
	st := sup.Stats()
	if st.Checks != 2 || st.CacheHits != 1 || st.Denied != 0 {
		t.Errorf("stats = %+v, want Checks=2 CacheHits=1 Denied=0", st)
	}
}

// TestSupervisorCacheNeverCachesDenies pins fail-closed: a DENY re-runs the
// model on every identical sentence (a stale policy must never be
// memoized, and a later ALLOW must still be reachable).
func TestSupervisorCacheNeverCachesDenies(t *testing.T) {
	withCacheTTL(t, time.Hour)
	ch := &scriptedChatter{reply: "DENY: speculates"}
	sup := NewSupervisor(supervisorCfg(), ch, testLogger())

	for i := range 3 {
		v, err := sup.Check(context.Background(), "cc-cache-2", "He earns six figures.")
		if err != nil || v.Allowed {
			t.Fatalf("check %d = %+v err %v, want denied", i, v, err)
		}
	}
	if got := ch.count(); got != 3 {
		t.Errorf("Ask called %d times for repeated DENIES, want 3 (denies never cached)", got)
	}
	if st := sup.Stats(); st.Denied != 3 || st.CacheHits != 0 {
		t.Errorf("stats = %+v, want Denied=3 CacheHits=0", st)
	}
}

// TestSupervisorCacheTTLExpiry pins the bounded staleness: after the TTL
// the same sentence re-asks (a policy reversal must not ride a dead entry).
func TestSupervisorCacheTTLExpiry(t *testing.T) {
	withCacheTTL(t, 20*time.Millisecond)
	ch := &scriptedChatter{reply: "ALLOW"}
	sup := NewSupervisor(supervisorCfg(), ch, testLogger())

	if _, err := sup.Check(context.Background(), "cc-cache-3", "The store closes at nine."); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	v, err := sup.Check(context.Background(), "cc-cache-3", "The store closes at nine.")
	if err != nil || !v.Allowed {
		t.Fatalf("post-TTL check = %+v err %v", v, err)
	}
	if v.Reason == "cached" {
		t.Errorf("expired entry answered from cache (Reason=cached) — TTL not honored")
	}
	if got := ch.count(); got != 2 {
		t.Errorf("Ask called %d times across the TTL boundary, want 2 (re-ask after expiry)", got)
	}
}

// TestSupervisorCacheCapEvictsLRU pins the bound: the map never exceeds
// supervisorCacheCap entries, and eviction is least-recently-used (the
// entry touched most recently survives).
func TestSupervisorCacheCapEvictsLRU(t *testing.T) {
	withCacheTTL(t, time.Hour)
	ch := &scriptedChatter{reply: "ALLOW"}
	sup := NewSupervisor(supervisorCfg(), ch, testLogger())

	// Fill the cache with cap distinct sentences, then re-touch key 0
	// (making it most-recent) before adding one more to force an eviction.
	for i := range supervisorCacheCap {
		if _, err := sup.Check(context.Background(), "cc-cache-4", fmt.Sprintf("Sentence number %d.", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sup.Check(context.Background(), "cc-cache-4", "Sentence number 0."); err != nil {
		t.Fatal(err) // refresh recency (cache hit, no Ask)
	}
	if _, err := sup.Check(context.Background(), "cc-cache-4", "One sentence past the cap."); err != nil {
		t.Fatal(err) // inserts one past the cap -> evicts LRU (sentence 1)
	}
	if n := sup.cacheLen(); n > supervisorCacheCap {
		t.Fatalf("cache length %d exceeds cap %d", n, supervisorCacheCap)
	}
	// Sentence 1 was the LRU: it must re-ask. Sentence 0 survived.
	before := ch.count()
	if _, err := sup.Check(context.Background(), "cc-cache-4", "Sentence number 1."); err != nil {
		t.Fatal(err)
	}
	if got := ch.count(); got != before+1 {
		t.Errorf("evicted LRU entry answered from cache (no Ask), want a re-ask")
	}
	before = ch.count()
	if _, err := sup.Check(context.Background(), "cc-cache-4", "Sentence number 0."); err != nil {
		t.Fatal(err)
	}
	if got := ch.count(); got != before {
		t.Errorf("recently-refreshed entry was evicted, want it to survive the cap")
	}
}

// TestSupervisorCacheSkipsDisabledGate pins that the gate-off fast path
// (the shipped default) neither caches nor counts — byte-identical posture
// when supervisor_enabled=false.
func TestSupervisorCacheSkipsDisabledGate(t *testing.T) {
	ch := &scriptedChatter{reply: "ALLOW"}
	cfg := supervisorCfg()
	cfg.Enabled = false
	sup := NewSupervisor(cfg, ch, testLogger())

	for i := range 3 {
		v, err := sup.Check(context.Background(), "cc-cache-5", "Any sentence.")
		if err != nil || !v.Allowed || v.Reason != "disabled" {
			t.Fatalf("disabled check %d = %+v err %v", i, v, err)
		}
	}
	if got := ch.count(); got != 0 {
		t.Errorf("Ask called %d times with the gate disabled, want 0", got)
	}
	if st := sup.Stats(); st != (SupervisorStats{}) {
		t.Errorf("stats = %+v with the gate disabled, want all zero", st)
	}
	if n := sup.cacheLen(); n != 0 {
		t.Errorf("disabled gate stored %d cache entries, want 0", n)
	}
}

// TestSupervisorTimeoutCounts pins the timeout counter rides the fail-closed
// timeout verdict (and lands in Denied too).
func TestSupervisorTimeoutCounts(t *testing.T) {
	sup := NewSupervisor(supervisorCfg(), &blockingChatter{}, testLogger())
	v, err := sup.Check(context.Background(), "cc-cache-6", "Anything at all.")
	if err != nil || v.Allowed {
		t.Fatalf("timeout check = %+v err %v, want denied", v, err)
	}
	st := sup.Stats()
	if st.Checks != 1 || st.Timeouts != 1 || st.Denied != 1 {
		t.Errorf("stats = %+v, want Checks=1 Timeouts=1 Denied=1", st)
	}
}
