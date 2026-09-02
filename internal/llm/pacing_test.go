package llm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	llmmetrics "github.com/caimlas/meept/internal/llm/metrics"
)

// pacingClock is an injected clock for deterministic interval state-machine
// tests (SHARED-CONVENTIONS §5: time-dependent logic uses an injected clock,
// no real sleeps).
type pacingClock struct {
	t time.Time
}

func (c *pacingClock) Now() time.Time          { return c.t }
func (c *pacingClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestPacer builds an enabled pacer with the injected clock and a fake
// sleep that records the requested duration and advances the clock, so Wait
// semantics are fully deterministic.
func newTestPacer(t *testing.T, store *llmmetrics.Store, cfg PacingConfig) (*AdaptivePacer, *pacingClock, *[]time.Duration) {
	t.Helper()
	clock := &pacingClock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	p := NewAdaptivePacer(store, cfg)
	p.now = clock.Now
	slept := &[]time.Duration{}
	p.sleepFn = func(ctx context.Context, d time.Duration) error {
		*slept = append(*slept, d)
		clock.Advance(d)
		return nil
	}
	return p, clock, slept
}

func testPacingCfg() PacingConfig {
	return PacingConfig{
		Enabled:          true,
		Target429PerHour: 1,
		MinInterval:      40 * time.Millisecond,
		MaxInterval:      160 * time.Millisecond,
	}
}

// TestAdaptivePacer_DisabledWaitReturnsImmediately pins the D15 default-OFF
// gate: a disabled pacer's Wait is a no-op that never sleeps and never
// touches the store, and Observe mutates nothing (disabled path identical to
// today).
func TestAdaptivePacer_DisabledWaitReturnsImmediately(t *testing.T) {
	cfg := testPacingCfg()
	cfg.Enabled = false
	p, _, slept := newTestPacer(t, nil, cfg)

	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("Wait on disabled pacer = %v, want nil", err)
	}
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if got := p.interval("prov"); got != 0 {
		t.Errorf("interval after Observe on disabled pacer = %v, want 0", got)
	}
	if len(*slept) != 0 {
		t.Errorf("disabled pacer slept %d times, want 0", len(*slept))
	}
}

// TestAdaptivePacer_FirstRequestNoWait pins: the first request per provider
// never waits, and an enabled pacer with no throttle history and no metrics
// rows never waits either (zero rows → no wait, leaf Task 2).
func TestAdaptivePacer_FirstRequestNoWait(t *testing.T) {
	p, _, slept := newTestPacer(t, nil, testPacingCfg())

	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("first Wait = %v, want nil", err)
	}
	if len(*slept) != 0 {
		t.Fatalf("first request slept %v, want none", *slept)
	}
	// Immediate second request with no learned interval and no rate-limit
	// history: still no wait.
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("second Wait = %v, want nil", err)
	}
	if len(*slept) != 0 {
		t.Errorf("unpaced provider slept %v, want none", *slept)
	}
}

// TestAdaptivePacer_ThrottleGrowsInterval pins the growth rule: the first
// FailureThrottle raises the gap to MinInterval, each further throttle
// doubles it, and growth clamps at MaxInterval (leaf Task 1).
func TestAdaptivePacer_ThrottleGrowsInterval(t *testing.T) {
	p, clock, _ := newTestPacer(t, nil, testPacingCfg())

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if got := p.interval("prov"); got != 40*time.Millisecond {
		t.Errorf("interval after 1st throttle = %v, want %v", got, 40*time.Millisecond)
	}
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if got := p.interval("prov"); got != 80*time.Millisecond {
		t.Errorf("interval after 2nd throttle = %v, want %v", got, 80*time.Millisecond)
	}
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if got := p.interval("prov"); got != 160*time.Millisecond {
		t.Errorf("interval after 3rd throttle = %v, want %v", got, 160*time.Millisecond)
	}
	// Clamped: never beyond MaxInterval no matter how many throttles.
	clock.Advance(200 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if got := p.interval("prov"); got != 160*time.Millisecond {
		t.Errorf("interval after 4th throttle = %v, want clamped %v", got, 160*time.Millisecond)
	}
}

// TestAdaptivePacer_WaitEnforcesLearnedInterval pins the enforcement side of
// the state machine: after a throttle, the next rapid request sleeps the
// learned gap, and back-to-back requests keep spacing by the same gap.
func TestAdaptivePacer_WaitEnforcesLearnedInterval(t *testing.T) {
	p, _, slept := newTestPacer(t, nil, testPacingCfg())

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("Wait after throttle = %v, want nil", err)
	}
	if len(*slept) != 0 {
		// The first request after the Observe claims the slot without
		// waiting; the NEXT rapid one pays the gap.
		t.Fatalf("first request after Observe slept %v, want none", *slept)
	}
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("second rapid Wait = %v, want nil", err)
	}
	want := []time.Duration{40 * time.Millisecond}
	if len(*slept) != 1 || (*slept)[0] != want[0] {
		t.Errorf("sleeps = %v, want %v", *slept, want)
	}
}

// TestAdaptivePacer_CleanDecayAfterQuietWindows pins the decay rule: one
// FailureNone per quiet window (a full learned interval of clean traffic)
// halves the gap, floored at 0 — once below MinInterval pacing switches off.
func TestAdaptivePacer_CleanDecayAfterQuietWindows(t *testing.T) {
	p, clock, _ := newTestPacer(t, nil, testPacingCfg())

	// Grow to the ceiling.
	for i := 0; i < 3; i++ {
		p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
		clock.Advance(time.Second)
	}
	if got := p.interval("prov"); got != 160*time.Millisecond {
		t.Fatalf("setup: interval = %v, want %v", got, 160*time.Millisecond)
	}

	// Quiet window 1: a full 160ms of clean traffic, then a clean verdict.
	clock.Advance(160 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 80*time.Millisecond {
		t.Errorf("interval after 1st quiet window = %v, want %v", got, 80*time.Millisecond)
	}

	// Quiet window 2.
	clock.Advance(80 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 40*time.Millisecond {
		t.Errorf("interval after 2nd quiet window = %v, want %v", got, 40*time.Millisecond)
	}

	// Quiet window 3: halving lands below MinInterval → pacing off.
	clock.Advance(40 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 0 {
		t.Errorf("interval after 3rd quiet window = %v, want 0", got)
	}
}

// TestAdaptivePacer_DecayRequiresQuietWindow pins that clean verdicts inside
// the current interval do NOT decay: decay only after a full quiet window.
func TestAdaptivePacer_DecayRequiresQuietWindow(t *testing.T) {
	p, clock, _ := newTestPacer(t, nil, testPacingCfg())

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	clock.Advance(10 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 40*time.Millisecond {
		t.Fatalf("interval after immediate clean = %v, want unchanged %v", got, 40*time.Millisecond)
	}
	clock.Advance(20 * time.Millisecond) // 30ms total < 40ms window
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 40*time.Millisecond {
		t.Fatalf("interval after partial window = %v, want unchanged %v", got, 40*time.Millisecond)
	}
	clock.Advance(10 * time.Millisecond) // 40ms total == one window
	p.Observe(PolicyVerdict{Class: FailureNone}, "prov")
	if got := p.interval("prov"); got != 0 {
		t.Errorf("interval after full quiet window = %v, want 0", got)
	}
}

// TestAdaptivePacer_QuotaAndServerVerdictsNeverPace pins the review gate: no
// pacing decisions on quota-class failures — Observe gates on Class, so
// quota/server/fatal verdicts neither grow nor decay the interval (D7).
func TestAdaptivePacer_QuotaAndServerVerdictsNeverPace(t *testing.T) {
	p, clock, _ := newTestPacer(t, nil, testPacingCfg())

	p.Observe(PolicyVerdict{Class: FailureQuota}, "prov")
	p.Observe(PolicyVerdict{Class: FailureServerError}, "prov")
	p.Observe(PolicyVerdict{Class: FailureFatal}, "prov")
	if got := p.interval("prov"); got != 0 {
		t.Fatalf("interval after non-throttle failures = %v, want 0", got)
	}

	// After a throttle, non-clean verdicts are neutral: no growth, and no
	// decay even across a quiet window (only FailureNone decays).
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	clock.Advance(time.Second)
	p.Observe(PolicyVerdict{Class: FailureQuota}, "prov")
	if got := p.interval("prov"); got != 40*time.Millisecond {
		t.Errorf("interval after quota verdict = %v, want unchanged %v", got, 40*time.Millisecond)
	}
}

// TestAdaptivePacer_WaitPropagatesSleepError pins that Wait surfaces a
// context cancellation from the sleep instead of swallowing it.
func TestAdaptivePacer_WaitPropagatesSleepError(t *testing.T) {
	p, _, _ := newTestPacer(t, nil, testPacingCfg())
	p.sleepFn = func(ctx context.Context, d time.Duration) error {
		return context.Canceled
	}

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if err := p.Wait(context.Background(), "prov"); err != nil {
		_ = err // first Wait claims the slot without sleeping
	}
	if err := p.Wait(context.Background(), "prov"); err == nil {
		t.Fatal("second Wait = nil, want the sleep error propagated")
	}
}

// TestAdaptivePacer_NilReceiverSafe pins repo nil-safety: a nil *AdaptivePacer
// is inert, so a typed-nil field can never panic at the call site.
func TestAdaptivePacer_NilReceiverSafe(t *testing.T) {
	var p *AdaptivePacer
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Errorf("nil pacer Wait = %v, want nil", err)
	}
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
}

// TestPacing_WaitStretchesWithSeededRateLimitRows is the leaf Task 2 metrics
// integration: a provider with rate_limit rows above target in the last hour
// gets the stretched (floor) gap enforced by Wait with NO Observe calls; a
// provider with zero rows does not wait.
func TestPacing_WaitStretchesWithSeededRateLimitRows(t *testing.T) {
	store := newPacingTestStore(t)
	defer store.Close()

	clock := &pacingClock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	ctx := context.Background()

	// Seed 3 rate-limit rows for hotprov inside the last hour (target = 1).
	seedPacingRows(t, store, "hotprov", 3, clock.t.Add(-10*time.Minute))
	waitForPacingCount(t, store, "hotprov", clock.t.Add(-time.Hour), 3)

	p := NewAdaptivePacer(store, testPacingCfg())
	p.now = clock.Now
	slept := &[]time.Duration{}
	p.sleepFn = func(ctx context.Context, d time.Duration) error {
		*slept = append(*slept, d)
		clock.Advance(d)
		return nil
	}

	// Zero-row provider: no wait, twice.
	if err := p.Wait(ctx, "coldprov"); err != nil {
		t.Fatalf("cold provider Wait = %v, want nil", err)
	}
	if err := p.Wait(ctx, "coldprov"); err != nil {
		t.Fatalf("cold provider second Wait = %v, want nil", err)
	}
	if len(*slept) != 0 {
		t.Fatalf("cold provider slept %v, want none", *slept)
	}

	// Hot provider: first Wait claims, second rapid Wait pays the stretched
	// floor gap even though Observe was never called (rate-hold rule).
	if err := p.Wait(ctx, "hotprov"); err != nil {
		t.Fatalf("hot provider first Wait = %v, want nil", err)
	}
	if err := p.Wait(ctx, "hotprov"); err != nil {
		t.Fatalf("hot provider second Wait = %v, want nil", err)
	}
	want := 40 * time.Millisecond
	if len(*slept) != 1 || (*slept)[0] != want {
		t.Errorf("hot provider sleeps = %v, want [%v]", *slept, want)
	}
}

// TestPacing_RateHoldKeepsMinimumUnderDecay pins the hold rule end-to-end: a
// learned interval decayed to zero does NOT unpace a provider whose hourly
// rate-limit rate is still above target — Wait keeps enforcing MinInterval.
func TestPacing_RateHoldKeepsMinimumUnderDecay(t *testing.T) {
	store := newPacingTestStore(t)
	defer store.Close()

	clock := &pacingClock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	ctx := context.Background()

	seedPacingRows(t, store, "hotprov", 3, clock.t.Add(-10*time.Minute))
	waitForPacingCount(t, store, "hotprov", clock.t.Add(-time.Hour), 3)

	p := NewAdaptivePacer(store, testPacingCfg())
	p.now = clock.Now
	slept := &[]time.Duration{}
	p.sleepFn = func(ctx context.Context, d time.Duration) error {
		*slept = append(*slept, d)
		clock.Advance(d)
		return nil
	}

	// Learn a 40ms interval, then decay it away via one quiet window.
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "hotprov")
	clock.Advance(40 * time.Millisecond)
	p.Observe(PolicyVerdict{Class: FailureNone}, "hotprov")
	if got := p.interval("hotprov"); got != 0 {
		t.Fatalf("setup: interval = %v, want 0", got)
	}

	// Rate is still hot: Wait must re-impose the floor gap.
	if err := p.Wait(ctx, "hotprov"); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if err := p.Wait(ctx, "hotprov"); err != nil {
		t.Fatalf("second Wait = %v, want nil", err)
	}
	want := 40 * time.Millisecond
	if len(*slept) != 1 || (*slept)[0] != want {
		t.Errorf("sleeps = %v, want [%v] (rate hold must keep the floor)", *slept, want)
	}
}

// newPacingTestStore builds an in-memory-file metrics store following the
// metrics/store_test.go fixture pattern.
func newPacingTestStore(t *testing.T) *llmmetrics.Store {
	t.Helper()
	store, err := llmmetrics.NewStore(llmmetrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "pacing.db"),
		RetentionDays:    7,
		StatsWindowHours: 24,
		RefreshInterval:  1 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// Record() is asynchronous: the flush worker only starts in
	// StartBackground.
	store.StartBackground(context.Background())
	return store
}

// seedPacingRows records n rate-limit rows for a provider.
func seedPacingRows(t *testing.T, store *llmmetrics.Store, provider string, n int, at time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		err := store.Record(context.Background(), llmmetrics.RequestRecord{
			Timestamp:  at.Add(time.Duration(i) * time.Second),
			ProviderID: provider,
			ModelID:    "model-1",
			HTTPStatus: 429,
			ErrorType:  llmmetrics.ErrorTypeRateLimit,
			Success:    false,
		})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
}

// waitForPacingCount polls the store until the async record worker flushes
// the seeded rows (no single sleep over 100ms, SHARED-CONVENTIONS §5).
func waitForPacingCount(t *testing.T, store *llmmetrics.Store, provider string, since time.Time, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := store.CountRateLimitEventsSince(context.Background(), provider, since)
		if err == nil && got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rate-limit count for %s since %v: got %d (err %v), want %d",
				provider, since, got, err, want)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
