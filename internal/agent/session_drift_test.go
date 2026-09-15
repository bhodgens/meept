package agent

import (
	"log/slog"
	"math"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// driftTestLogger writes critical-level records to /dev/null so the
// drift-fired Info lines don't spam test output.
func driftTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return slog.New(slog.NewTextHandler(f, nil))
}

// driftConfig returns an enabled SessionDriftConfig with overrides.
func driftConfig(window int, threshold float64) config.SessionDriftConfig {
	return config.SessionDriftConfig{
		Enabled:    true,
		WindowSize: window,
		Threshold:  threshold,
		LogOnly:    true,
	}
}

// noisyUnit makes a unit vector pointing at dir with isotropic noise.
func noisyUnit(rnd *rand.Rand, dir []float64, sigma float64) []float64 {
	v := make([]float64, len(dir))
	norm := 0.0
	for i, d := range dir {
		v[i] = d + rnd.NormFloat64()*sigma
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	return v
}

// unitRandomDir makes a random unit direction (seeded).
func unitRandomDir(rnd *rand.Rand, dim int) []float64 {
	v := make([]float64, dim)
	norm := 0.0
	for i := range v {
		v[i] = rnd.NormFloat64()
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	return v
}

// TestSessionDriftStableSingleIntent is the >= 95% acceptance criterion:
// noisy embeddings around one direction over 100 turns produce zero
// drift signals for the overwhelming majority of stable sessions.
func TestSessionDriftStableSingleIntent(t *testing.T) {
	const (
		turns    = 100
		sessions = 200
		sigma    = 0.3
	)
	// Pass rate must hold across independent seeds, not one lucky seed.
	for seed := int64(1); seed <= 3; seed++ {
		rnd := rand.New(rand.NewSource(seed))
		d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
		fired := 0
		for s := 0; s < sessions; s++ {
			id := "stable-" + string(rune('a'+s%26)) + string(rune('0'+s/26))
			dir := unitRandomDir(rnd, 16)
			for turn := 0; turn < turns; turn++ {
				drifted, _ := d.Observe(id, noisyUnit(rnd, dir, sigma))
				if drifted {
					fired++
				}
			}
		}
		rate := float64(fired) / float64(sessions)
		if rate > 0.05 {
			t.Errorf("seed %d: %d/%d stable sessions fired (rate %.3f), want <= 5%% (>=95%% acceptance)",
				seed, fired, sessions, rate)
		}
	}
}

// TestSessionDriftMidSessionShift is the detection criterion: embeddings
// rotate to a new direction at turn k and a signal fires within a small
// number of turns after k.
func TestSessionDriftMidSessionShift(t *testing.T) {
	const (
		k        = 30
		turns    = 45
		sessions = 100
		sigma    = 0.3
		within   = 5 // turns after k to fire
	)
	rnd := rand.New(rand.NewSource(1234))
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	detected := 0
	for s := 0; s < sessions; s++ {
		id := "shift-" + string(rune('a'+s%26)) + string(rune('0'+s/26))
		dir1 := unitRandomDir(rnd, 16)
		dir2 := unitRandomDir(rnd, 16)
		fired := false
		for turn := 0; turn < turns; turn++ {
			dir := dir1
			if turn >= k {
				dir = dir2
			}
			drifted, _ := d.Observe(id, noisyUnit(rnd, dir, sigma))
			if drifted && turn >= k && turn < k+within {
				fired = true
			}
		}
		if fired {
			detected++
		}
	}
	rate := float64(detected) / float64(sessions)
	if rate < 0.95 {
		t.Errorf("only %d/%d shift sessions detected within %d turns (rate %.3f), want >= 95%%",
			detected, sessions, within, rate)
	}
}

// TestSessionDriftWarmUp: no signal before the window has filled once.
func TestSessionDriftWarmUp(t *testing.T) {
	rnd := rand.New(rand.NewSource(99))
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	if got := d.WindowSize(); got != defaultDriftWindow {
		t.Fatalf("default window = %d, want %d", got, defaultDriftWindow)
	}
	dir := unitRandomDir(rnd, 16)
	// Feed extremely opposite signals after fill; before fill nothing
	// may fire regardless of input.
	for turn := 0; turn < d.WindowSize()-1; turn++ {
		var emb []float64
		if turn%2 == 0 {
			emb = noisyUnit(rnd, dir, 0.1)
		} else {
			emb = noisyUnit(rnd, negDir(dir), 0.1) // maximally adversarial
		}
		drifted, score := d.Observe("warmup", emb)
		if drifted {
			t.Fatalf("turn %d (< window %d): drifted fired during warm-up", turn, d.WindowSize())
		}
		if score != 0 {
			t.Fatalf("turn %d: warm-up score = %v, want 0", turn, score)
		}
	}
}

// negDir returns -dir.
func negDir(dir []float64) []float64 {
	out := make([]float64, len(dir))
	for i, v := range dir {
		out[i] = -v
	}
	return out
}

// TestSessionDriftDefaultsSane checks the 0-means-default contract for
// both knobs and that the derived defaults are the documented ones.
func TestSessionDriftDefaultsSane(t *testing.T) {
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	if !d.Enabled() {
		t.Fatal("enabled config produced inert detector")
	}
	if d.WindowSize() != defaultDriftWindow {
		t.Errorf("WindowSize() = %d, want %d", d.WindowSize(), defaultDriftWindow)
	}
	if d.Threshold() != defaultDriftThreshold {
		t.Errorf("Threshold() = %v, want %v", d.Threshold(), defaultDriftThreshold)
	}
	// Floor: a tiny explicit window floors at 4.
	d2 := NewSessionDriftDetector(driftConfig(1, 0), driftTestLogger(t))
	if d2.WindowSize() < minDriftWindow {
		t.Errorf("window floor: got %d, want >= %d", d2.WindowSize(), minDriftWindow)
	}
	// Explicit threshold overrides the default.
	d3 := NewSessionDriftDetector(driftConfig(0, 0.9), driftTestLogger(t))
	if d3.Threshold() != 0.9 {
		t.Errorf("explicit threshold: got %v, want 0.9", d3.Threshold())
	}
}

// TestSessionDriftLogOnlyNoEffect: LogOnly is a call-site concern and
// must not change detector output.
func TestSessionDriftLogOnlyNoEffect(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	dir := unitRandomDir(rnd, 16)
	logOnly := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	logOnlyCfg := driftConfig(0, 0)
	logOnlyCfg.LogOnly = false
	enforcing := NewSessionDriftDetector(logOnlyCfg, driftTestLogger(t))
	for turn := 0; turn < 40; turn++ {
		emb := noisyUnit(rnd, dir, 0.3)
		d1, s1 := logOnly.Observe("s", emb)
		d2, s2 := enforcing.Observe("s", emb)
		if d1 != d2 || s1 != s2 {
			t.Fatalf("turn %d: log-only output (%v,%v) != enforcing output (%v,%v)",
				turn, d1, s1, d2, s2)
		}
	}
}

// TestSessionDriftConcurrentObserve exercises multi-goroutine Observe
// under -race.
func TestSessionDriftConcurrentObserve(t *testing.T) {
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	const (
		goroutines = 8
		turns      = 50
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(int64(1000 + g)))
			dir := unitRandomDir(rnd, 16)
			id := "goroutine-" + string(rune('a'+g))
			for turn := 0; turn < turns; turn++ {
				d.Observe(id, noisyUnit(rnd, dir, 0.3))
			}
		}(g)
	}
	wg.Wait()
	// Same session from concurrent goroutines too (contention path).
	wg = sync.WaitGroup{}
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(int64(2000 + g)))
			dir := unitRandomDir(rnd, 16)
			for turn := 0; turn < turns; turn++ {
				d.Observe("shared-session", noisyUnit(rnd, dir, 0.3))
			}
		}(g)
	}
	wg.Wait()
}

// TestSessionDriftCapEvictionAndReset covers the session cap (LRU
// eviction) and ResetSession.
func TestSessionDriftCapEvictionAndReset(t *testing.T) {
	rnd := rand.New(rand.NewSource(55))
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	// Fill one session partially so it is distinguishable.
	d.Observe("first", noisyUnit(rnd, unitRandomDir(rnd, 16), 0.1))
	// Overflow the map: enough unique new sessions must evict the
	// oldest down to the cap.
	for i := 0; i < defaultMaxDriftSession+50; i++ {
		d.Observe("cap-"+strconv.Itoa(i), noisyUnit(rnd, unitRandomDir(rnd, 16), 0.1))
	}
	d.mu.Lock()
	n := len(d.sessions)
	_, firstKept := d.sessions["first"]
	d.mu.Unlock()
	if n > defaultMaxDriftSession {
		t.Errorf("sessions map = %d, want <= cap %d", n, defaultMaxDriftSession)
	}
	if firstKept {
		t.Error("oldest session 'first' survived cap eviction; LRU not applied")
	}
	// ResetSession removes state and restarts warm-up.
	d.Observe("reset-me", noisyUnit(rnd, unitRandomDir(rnd, 16), 0.1))
	d.ResetSession("reset-me")
	drifted, _ := d.Observe("reset-me", noisyUnit(rnd, unitRandomDir(rnd, 16), 0.1))
	if drifted {
		t.Error("drift fired on first observation after ResetSession; warm-up not restarted")
	}
	// Reset of an unknown session is a safe no-op.
	d.ResetSession("never-seen")
}

// TestSessionDriftDegenerateInputs: NaN/Inf/zero-norm/empty/dim-mismatch
// inputs return (false, 0), never panic, and leave the session usable.
func TestSessionDriftDegenerateInputs(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	d := NewSessionDriftDetector(driftConfig(0, 0), driftTestLogger(t))
	dir := unitRandomDir(rnd, 16)

	cases := []struct {
		name string
		emb  []float64
	}{
		{"empty", []float64{}},
		{"nil", nil},
		{"nan", []float64{math.NaN(), 1, 0}},
		{"pos-inf", []float64{math.Inf(1), 1, 0}},
		{"neg-inf", []float64{math.Inf(-1), 1, 0}},
		{"zero-norm", []float64{0, 0, 0}},
		{"dim-mismatch", []float64{1, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drifted, score := d.Observe("degenerate", tc.emb)
			if drifted || score != 0 {
				t.Errorf("degenerate input %q: got (%v,%v), want (false,0)", tc.name, drifted, score)
			}
		})
	}
	// Session still usable and still in warm-up (degenerate inputs were
	// not recorded).
	for turn := 0; turn < d.WindowSize(); turn++ {
		drifted, _ := d.Observe("degenerate", noisyUnit(rnd, dir, 0.1))
		if drifted {
			t.Fatal("drift fired immediately after warm-up with clean feed; degenerate inputs polluted the window")
		}
	}
}

// TestSessionDriftInertWhenDisabled: the disabled detector is nil-safe,
// inert, and keeps no state.
func TestSessionDriftInertWhenDisabled(t *testing.T) {
	cfg := driftConfig(0, 0)
	cfg.Enabled = false
	d := NewSessionDriftDetector(cfg, driftTestLogger(t))
	if d.Enabled() {
		t.Fatal("disabled config produced enabled detector")
	}
	for i := 0; i < 30; i++ {
		drifted, score := d.Observe("x", noisyUnit(rand.New(rand.NewSource(1)), []float64{1, 0, 0}, 0.3))
		if drifted || score != 0 {
			t.Fatalf("disabled detector fired: (%v,%v)", drifted, score)
		}
	}
	// Nil detector is also safe.
	var nd *SessionDriftDetector
	if nd.Enabled() {
		t.Fatal("nil detector reported enabled")
	}
	if drifted, score := nd.Observe("x", []float64{1, 0, 0}); drifted || score != 0 {
		t.Fatalf("nil detector Observe: (%v,%v), want (false,0)", drifted, score)
	}
}
