package tierpolicy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory/working"
)

// countingStates counts TierStates calls so the test can observe ticks.
type countingStates struct {
	calls atomic.Int64
}

func (c *countingStates) TierStates(context.Context) ([]TierState, error) {
	c.calls.Add(1)
	return []TierState{{Tier: "working", Tokens: 10, Ceiling: 100}}, nil
}

// TestStartSingleStartGuarantee: a started engine refuses a second Start
// (T4); after Stop a restart is allowed (Consolidator tickerActive
// pattern).
func TestStartSingleStartGuarantee(t *testing.T) {
	states := &countingStates{}
	e := New(enabledCfg(), Deps{States: states})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if !e.Running() {
		t.Fatal("engine must report running after Start")
	}
	if err := e.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start must return ErrAlreadyStarted, got %v", err)
	}
	e.Stop()
	if e.Running() {
		t.Fatal("engine must report stopped after Stop")
	}
	// Stop is idempotent.
	e.Stop()
	// Restart after Stop is allowed (no permanently single-use engine).
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := e.Start(ctx2); err != nil {
		t.Fatalf("restart after Stop: %v", err)
	}
	e.Stop()
}

// TestStartDisabled: a disabled engine refuses to start (T4: disabled
// config => no goroutine).
func TestStartDisabled(t *testing.T) {
	states := &countingStates{}
	e := New(Config{}, Deps{States: states})
	if err := e.Start(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled Start must return ErrDisabled, got %v", err)
	}
	if e.Running() {
		t.Fatal("disabled engine must never run")
	}
	time.Sleep(30 * time.Millisecond)
	if states.calls.Load() != 0 {
		t.Fatalf("disabled engine must make zero state calls, got %d", states.calls.Load())
	}
}

// TestTickRespectsIntervalAndCancel (T4 Given/When/Then): enabled config
// and a 20ms interval; run ~70ms then cancel; expect >=2 evaluations and
// a clean stop with no further evaluations.
func TestTickRespectsIntervalAndCancel(t *testing.T) {
	states := &countingStates{}
	cfg := enabledCfg()
	cfg.Interval = 20 * time.Millisecond
	e := New(cfg, Deps{States: states})
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(70 * time.Millisecond)
	cancel()
	e.Stop()
	first := states.calls.Load()
	if first < 2 {
		t.Fatalf("want >=2 evaluations over 70ms at 20ms interval, got %d", first)
	}
	time.Sleep(60 * time.Millisecond)
	if after := states.calls.Load(); after != first {
		t.Fatalf("ticks must stop after cancel: %d -> %d", first, after)
	}
}

// TestNilEngineSafety: typed-nil receivers never panic (repo guard rule;
// internal/daemon/AGENTS.md typed-nil wiring lesson).
func TestNilEngineSafety(t *testing.T) {
	var e *Engine
	if e.Running() {
		t.Fatal("nil engine cannot run")
	}
	e.Stop() // must not panic
	if err := e.Start(context.Background()); err == nil {
		t.Fatal("nil engine Start must error")
	}
	if _, err := e.Evaluate(context.Background(), nil); err == nil {
		t.Fatal("nil engine Evaluate must error")
	}
	if _, ok := e.LastPass("working"); ok {
		t.Fatal("nil engine has no watermarks")
	}
}

// TestSetClockNilGuard: nil and typed-nil clock setters are ignored.
func TestSetClockNilGuard(t *testing.T) {
	e := New(enabledCfg(), Deps{})
	e.SetClock(nil)               // must not panic
	var typedNil func() time.Time // typed nil func
	e.SetClock(typedNil)          // must not panic
}

// TestResolveTiersConfig (T3, contract shape): zero-value stays
// disabled; enabled-but-unset resolves to 0.9/0.8/300s
// (findings-memory-tiers.md R-B2/R-B5).
func TestResolveTiersConfig(t *testing.T) {
	t.Parallel()
	got := ResolveTiersConfig(TiersConfig{})
	if got.Enabled {
		t.Fatal("zero-value config must stay disabled (opt-in-defaults rule)")
	}
	if got.EvictOverRatio != 0 || got.CompressOverRatio != 0 || got.IntervalSeconds != 0 {
		t.Fatalf("disabled config must not gain active values, got %+v", got)
	}
	got = ResolveTiersConfig(TiersConfig{Enabled: true})
	if !got.Enabled {
		t.Fatal("enabled must survive resolution")
	}
	if got.EvictOverRatio != DefaultEvictOverRatio {
		t.Fatalf("evict ratio default: want %v, got %v", DefaultEvictOverRatio, got.EvictOverRatio)
	}
	if got.CompressOverRatio != DefaultCompressOverRatio {
		t.Fatalf("compress ratio default: want %v, got %v", DefaultCompressOverRatio, got.CompressOverRatio)
	}
	if got.IntervalSeconds != int(DefaultInterval/time.Second) {
		t.Fatalf("interval default: want 300, got %d", got.IntervalSeconds)
	}
	// Explicit values win.
	got = ResolveTiersConfig(TiersConfig{Enabled: true, EvictOverRatio: 0.95, CompressOverRatio: 0.6, IntervalSeconds: 60})
	if got.EvictOverRatio != 0.95 || got.CompressOverRatio != 0.6 || got.IntervalSeconds != 60 {
		t.Fatalf("explicit values must win, got %+v", got)
	}
	// Compress band clamps inside the evict band: New (the consumer)
	// clamps the engine-side Config, so assert the resolved pair via the
	// mapping + engine constructor.
	got = ResolveTiersConfig(TiersConfig{Enabled: true, EvictOverRatio: 0.7, CompressOverRatio: 0.9})
	ecfg := got.ToEngineConfig(working.Config{MaxTokens: 100})
	eng := New(ecfg, Deps{})
	if eng.cfg.CompressOverRatio > eng.cfg.EvictOverRatio {
		t.Fatalf("compress band must sit inside the evict band, got compress=%v evict=%v",
			eng.cfg.CompressOverRatio, eng.cfg.EvictOverRatio)
	}
}

// TestToEngineConfig: mapping preserves resolved values and interval.
func TestToEngineConfig(t *testing.T) {
	t.Parallel()
	tiers := ResolveTiersConfig(TiersConfig{Enabled: true})
	ecfg := tiers.ToEngineConfig(working.Config{MaxTokens: 8192, MaxItems: 200, TTL: 24 * time.Hour})
	if !ecfg.Enabled || ecfg.EvictOverRatio != 0.9 || ecfg.CompressOverRatio != 0.8 {
		t.Fatalf("mapping lost values: %+v", ecfg)
	}
	if ecfg.Interval != 300*time.Second {
		t.Fatalf("mapping lost interval: %s", ecfg.Interval)
	}
	if ecfg.WorkingConfig.MaxTokens != 8192 {
		t.Fatalf("mapping lost working config: %+v", ecfg.WorkingConfig)
	}
}

// TestEngineDefaultsOnZeroConfig: New resolves ratio/interval/batch
// defaults when Enabled but unset (T3 Given/When/Then second half).
func TestEngineDefaultsOnZeroConfig(t *testing.T) {
	t.Parallel()
	e := New(Config{Enabled: true}, Deps{})
	if e.cfg.EvictOverRatio != DefaultEvictOverRatio {
		t.Fatalf("evict ratio: got %v", e.cfg.EvictOverRatio)
	}
	if e.cfg.CompressOverRatio != DefaultCompressOverRatio {
		t.Fatalf("compress ratio: got %v", e.cfg.CompressOverRatio)
	}
	if e.cfg.Interval != DefaultInterval {
		t.Fatalf("interval: got %s", e.cfg.Interval)
	}
	if e.cfg.MinBatch != DefaultMinBatch || e.cfg.MaxBatch != DefaultMaxBatch {
		t.Fatalf("batch bounds: got %d/%d", e.cfg.MinBatch, e.cfg.MaxBatch)
	}
}
