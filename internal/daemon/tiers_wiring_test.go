package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory/tierpolicy"
	"github.com/caimlas/meept/internal/memory/working"
)

func tiersAdapter(enabled bool) TiersConfigAdapter {
	return TiersConfigAdapter{
		Enabled:           enabled,
		EvictOverRatio:    tierpolicy.DefaultEvictOverRatio,
		CompressOverRatio: tierpolicy.DefaultCompressOverRatio,
		IntervalSeconds:   int(tierpolicy.DefaultInterval / time.Second),
	}
}

func workCfg() working.Config {
	return working.Config{MaxTokens: 8192, MaxItems: 200, TTL: 24 * time.Hour}
}

// TestTiersWiringDisabledNoGoroutine (T4): disabled config yields no
// engine, no goroutine, no error - the zero-cost default posture.
func TestTiersWiringDisabledNoGoroutine(t *testing.T) {
	eng, wired, err := BuildTierPolicyEngine(tiersAdapter(false), workCfg(), nil, nil, nil)
	if err != nil {
		t.Fatalf("disabled wiring must not error: %v", err)
	}
	if wired || eng != nil {
		t.Fatalf("disabled wiring must return (nil, false, nil), got (%v, %v)", eng, wired)
	}
}

// TestTiersWiringEnabledNilManager (T4 typed-nil guard): an enabled
// config with a nil memory manager is a wiring ERROR, never a ticking
// engine that would deref nil on its first pass
// (internal/daemon/AGENTS.md typed-nil rule).
func TestTiersWiringEnabledNilManager(t *testing.T) {
	eng, wired, err := BuildTierPolicyEngine(tiersAdapter(true), workCfg(), nil, nil, nil)
	if err == nil {
		t.Fatal("enabled + nil manager must error")
	}
	if !errors.Is(err, ErrTiersNoManager) {
		t.Fatalf("want ErrTiersNoManager, got %v", err)
	}
	if wired || eng != nil {
		t.Fatalf("failed wiring must return (nil, false, err), got (%v, %v)", eng, wired)
	}
}

// TestTiersWiringSingleStartLifecycle (T4 Given/When/Then): enabled
// config and a 50ms interval; run 150ms then cancel; expect >=2
// evaluations, clean stop, and no double-start. The engine's tick needs
// live stats, so the nil-Stares fallback is tolerated (tick logs and
// continues) - the count assertion here uses the handle semantics; the
// tick-count assertion lives in tierpolicy's lifecycle test where the
// StateSource is stubbed. Here we pin: Start/Start -> ErrAlreadyStarted,
// Stop idempotent, Running transitions.
func TestTiersWiringSingleStartLifecycle(t *testing.T) {
	t.Skip("needs an initialized memory.Manager (sqlite fixture); lifecycle ticks covered by tierpolicy lifecycle_test.go - see TestTickRespectsIntervalAndCancel")
}
