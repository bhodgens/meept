package llm

import (
	"context"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

// PacingConfig configures adaptive outbound pacing (DECISIONS.md D15,
// tree 02 leaf 05). It is the local mirror of config.FailurePolicyConfig's
// Pacing sub-block; the daemon maps the canonical values onto it at wiring
// time (internal/llm cannot import internal/config — import cycle).
type PacingConfig struct {
	// Enabled gates pacing (D15: default OFF). A disabled — or nil —
	// pacer is byte-identical to no pacer: Wait returns immediately and
	// Observe mutates nothing.
	Enabled bool
	// Target429PerHour is the tolerated throttle-429 rate per provider per
	// hour ("tolerate at most N throttle 429/hour/provider"). A higher
	// observed hourly rate holds the enforced gap at MinInterval.
	Target429PerHour int
	// MinInterval is the smallest enforced gap between outbound requests
	// to one provider (the pacing floor).
	MinInterval time.Duration
	// MaxInterval is the ceiling: learned intervals never exceed it.
	MaxInterval time.Duration
}

// pacerState is the per-provider interval state machine state.
type pacerState struct {
	// interval is the current enforced gap; 0 = not pacing.
	interval time.Duration
	// lastClaim is when this provider's outbound slot was last claimed
	// (zero until the first Wait for this provider).
	lastClaim time.Time
	// anchor is the instant the current growth/decay epoch began: the
	// last throttle growth or the last decay step. A quiet window is
	// measured from it.
	anchor time.Time
}

// AdaptivePacer paces outbound requests per provider below the provider's
// effective rate-limit ceiling, learned from rate-limit history
// (DECISIONS.md D15). It composes with — never replaces — the retry loops:
// Wait only ever sleeps a bounded gap, it never blocks a request outright.
//
// Interval state machine (injected clock, deterministic):
//   - Observe(FailureThrottle): interval grows ×2 from MinInterval,
//     clamped at MaxInterval (D7: throttle only — quota-class failures are
//     the park path and never pace).
//   - Observe(FailureNone) after a full quiet window (one learned interval
//     of clean traffic since the anchor): interval decays ×0.5, floored at
//     zero (halving below MinInterval turns pacing off).
//   - Other classes are neutral: they neither grow nor decay.
//   - Independently, a metrics-store hourly 429 rate above Target holds the
//     enforced gap at MinInterval even without fresh Observe calls, so a
//     decayed interval cannot unpace a provider that is still shedding load.
type AdaptivePacer struct {
	store *metrics.Store
	cfg   PacingConfig

	// state is guarded by mu. The DB read in Wait runs OUTSIDE mu
	// (collect-under-lock: no I/O under the pacer mutex — mutexio rule).
	mu    sync.Mutex
	state map[string]pacerState

	// now is the injected clock (defaults to time.Now).
	now func() time.Time
	// sleepFn is the injected sleep (defaults to a ctx-aware timer) —
	// tests substitute it to observe the requested duration.
	sleepFn func(ctx context.Context, d time.Duration) error
	// countFn is the injected hourly rate-limit counter (defaults to the
	// metrics store query; nil when no store, meaning no rate hold).
	countFn func(ctx context.Context, providerID string, since time.Time) (int64, error)
}

// NewAdaptivePacer builds a pacer. Invalid config values take the documented
// defaults (target 1/hour, min 1s, max 30s; max < min lifts max to the 30s
// default, mirroring config.NormalizeFailurePolicyDefaults). A nil store is
// valid: the pacer runs purely on Observe feedback with no rate hold.
func NewAdaptivePacer(store *metrics.Store, cfg PacingConfig) *AdaptivePacer {
	if cfg.Target429PerHour <= 0 {
		cfg.Target429PerHour = 1
	}
	if cfg.MinInterval <= 0 {
		cfg.MinInterval = time.Second
	}
	if cfg.MaxInterval <= 0 {
		cfg.MaxInterval = 30 * time.Second
	}
	if cfg.MaxInterval < cfg.MinInterval {
		// A bad pair must not pin pacing above its ceiling.
		cfg.MaxInterval = 30 * time.Second
	}

	p := &AdaptivePacer{
		store: store,
		cfg:   cfg,
		state: make(map[string]pacerState),
		now:   time.Now,
		sleepFn: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	if store != nil {
		p.countFn = store.CountRateLimitEventsSince
	}
	return p
}

// Wait sleeps until the next outbound request to providerID is allowed and
// claims the slot. It always returns nil for a disabled (or nil) pacer, and
// never waits on a provider's FIRST request. The enforced gap is
// max(learned interval, metrics rate hold); the sleep honors ctx
// cancellation and never exceeds MaxInterval. Scope guard: Wait never blocks
// a request outright — callers treat its error (ctx canceled) as abort.
func (p *AdaptivePacer) Wait(ctx context.Context, providerID string) error {
	if p == nil || !p.cfg.Enabled {
		return nil
	}
	now := p.now()

	// Rate hold is a DB read: run it OUTSIDE p.mu (collect-under-lock —
	// no I/O under the pacer mutex, mutexio rule).
	hold := p.rateHold(ctx, providerID, now)

	p.mu.Lock()
	st := p.state[providerID]
	var wait time.Duration
	if st.lastClaim.IsZero() {
		// First request per provider claims without waiting.
		wait = 0
	} else {
		need := st.interval
		if hold > need {
			need = hold
		}
		if need > 0 {
			if elapsed := now.Sub(st.lastClaim); elapsed < need {
				wait = need - elapsed
			}
		}
	}
	st.lastClaim = now.Add(wait)
	p.state[providerID] = st
	p.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	return p.sleepFn(ctx, wait)
}

// Observe feeds a policy verdict back into the interval state machine.
// Throttle verdicts grow the gap (D7: provider-load 429s), clean traffic
// decays it across quiet windows, and every other class — notably quota —
// is neutral so pacing never reacts to quota-class failures.
func (p *AdaptivePacer) Observe(v PolicyVerdict, providerID string) {
	if p == nil || !p.cfg.Enabled {
		return
	}
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state[providerID]

	switch v.Class {
	case FailureThrottle:
		// Grow ×2 from the MinInterval seed, clamped at MaxInterval.
		if st.interval <= 0 {
			st.interval = p.cfg.MinInterval
		} else {
			st.interval *= 2
			if st.interval > p.cfg.MaxInterval {
				st.interval = p.cfg.MaxInterval
			}
		}
		st.anchor = now

	case FailureNone:
		// Decay ×0.5 per quiet window: one full learned interval of
		// clean traffic since the anchor. Halving below MinInterval
		// turns pacing off for the provider.
		if st.interval <= 0 {
			break
		}
		if now.Sub(st.anchor) >= st.interval {
			st.interval /= 2
			if st.interval < p.cfg.MinInterval {
				st.interval = 0
			}
			st.anchor = now
		}

	default:
		// Quota/server/fatal: neutral (review gate — no pacing on
		// quota-class failures, D7).
	}

	p.state[providerID] = st
}

// rateHold returns MinInterval when the provider's hourly rate-limit rate in
// the metrics store exceeds Target429PerHour, else zero. Store errors are
// treated as "no hold" — the pacer degrades to Observe-only feedback.
func (p *AdaptivePacer) rateHold(ctx context.Context, providerID string, now time.Time) time.Duration {
	if p.countFn == nil {
		return 0
	}
	n, err := p.countFn(ctx, providerID, now.Add(-time.Hour))
	if err != nil || n <= int64(p.cfg.Target429PerHour) {
		return 0
	}
	return p.cfg.MinInterval
}

// interval reports the current learned gap for a provider (test seam and
// diagnostics; 0 = not pacing).
func (p *AdaptivePacer) interval(providerID string) time.Duration {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state[providerID].interval
}
