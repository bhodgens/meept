package voice

// Dead-air call reaper (leaf 02, plan 20261110-uptractor-ports; contract C3).
//
// Ported from uptractor phone/core/internal/callsvc/reaper.go (branch
// fix/coordinator-missed-hangup-watchdog) — structure and defaults frozen
// (Interval 30s, DeadAir 60s [source MinAge under owner-ratified DEAD-AIR
// semantics], MaxDuration 30m) — adapted to meept's Manager:
//
//   - DEAD-AIR semantics (owner 2026-11-11): a call is released only after
//     DeadAir with NO caller-side activity, NOT merely age-since-answer.
//     The reaper keeps its OWN {ccid: lastActivity} map — seeded via the
//     ADDITIVE answered-observer lane, REFRESHED on every caller-side
//     audio/turn event via the ADDITIVE activity-observer lane — so no
//     Manager state or constructor changes (frozen-funnel doctrine).
//   - Release rides the EXISTING FailOutcome funnel (manager.go), never
//     HandleCallEvent (whose hangup branch hard-codes reason "call.hangup").
//     The frozen reason strings "reaped:hangup-lost" / "reaped:max-duration"
//     ride the release; FailOutcome on an already-released ccid is a no-op
//     via releaseOnce, so a reaper/webhook race releases exactly once and
//     the reaper needs no latch of its own.
//   - telnyx.CallController is NOT extended: CallStatusChecker is a
//     STANDALONE interface satisfied by *telnyx.Client (leaf 01).
//
// Only PHONE-leg sessions are probed (local/signal legs have no Telnyx
// authority). A probe error is logged and retried next tick — it never
// kills the loop; only a clear "not alive" (or MaxDuration) ends a call,
// never an unanswered question (uptractor posture, kept).

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// CallStatusChecker asks the carrier about one call. *telnyx.Client
// satisfies it (leaf 01's GetCallStatus). Deliberately NOT part of
// telnyx.CallController: adding a method there breaks every existing
// CallController test fake.
type CallStatusChecker interface {
	GetCallStatus(ctx context.Context, callControlID string) (telnyx.CallStatus, error)
}

// CallErrorLogger records a fault against a call. *CallStore implements it
// (LogCallError, the additive call_errors table).
type CallErrorLogger interface {
	LogCallError(callControlID, message string) error
}

// probeTimeout bounds one carrier status probe. Sweeps are serial (the
// uptractor posture: a parallel fan-out adds races for no benefit at
// single-digit call counts), so this also bounds one sweep's wall time.
const probeTimeout = 10 * time.Second

// hangupTimeout bounds the best-effort carrier HangupCall.
const hangupTimeout = 10 * time.Second

// ReaperConfig tunes the reaper. Zero values take the defaults noted
// (frozen from the uptractor source).
type ReaperConfig struct {
	Status      CallStatusChecker // required; nil => Error log, no goroutine
	Errors      CallErrorLogger   // optional; callstore implements
	Interval    time.Duration     // how often to sweep; default 30s
	DeadAir     time.Duration     // release only after this much caller silence; default 60s
	MaxDuration time.Duration     // a call longer than this is hung up and reaped; default 30m
}

func (c ReaperConfig) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return 30 * time.Second
}

func (c ReaperConfig) deadAir() time.Duration {
	if c.DeadAir > 0 {
		return c.DeadAir
	}
	return 60 * time.Second
}

func (c ReaperConfig) maxDuration() time.Duration {
	if c.MaxDuration > 0 {
		return c.MaxDuration
	}
	return 30 * time.Minute
}

// reaper is the per-manager reaper instance: its own bounded
// {ccid: lastActivity} map plus the bookkeeping the sweep needs. Entries
// are removed on the terminal observer, so the map never outlives a call.
type reaper struct {
	mgr    *Manager
	tel    telnyx.CallController // carrier hangup seam (may be nil in tests)
	cfg    ReaperConfig
	logger *slog.Logger

	mu           sync.Mutex
	lastActivity map[string]time.Time // ccid -> last caller-side activity
}

// RunReaper sweeps every cfg.Interval until ctx is canceled. It is the
// Manager's dead-air reaper: no-op + Error log when Status is nil (the
// reaper is meaningless without a carrier authority). Registered observer
// lanes are ADDITIVE — the release hook, the answer hook, and the Manager's
// per-ccid state are untouched.
func (m *Manager) RunReaper(ctx context.Context, cfg ReaperConfig) {
	if cfg.Status == nil {
		slog.Error("voice reaper not started: no status checker")
		return
	}
	r := &reaper{
		mgr:          m,
		tel:          m.tel,
		cfg:          cfg,
		logger:       slog.Default().With("component", "voice-reaper"),
		lastActivity: make(map[string]time.Time),
	}
	// DEAD-AIR feed, lane 1 (seed): the additive answered observer seeds
	// lastActivity at call.answered. The observer runs on the webhook
	// goroutine; it only takes the reaper's own tiny mutex — never a
	// manager lock, never I/O.
	m.AddAnsweredObserver(func(ccid string) {
		r.touch(ccid)
	})
	// DEAD-AIR feed, lane 2 (refresh): every caller-side audio/turn edge
	// resets the dead-air clock for that call.
	m.AddActivityObserver(r.touch)
	// Bounded map: entries die with the call on the terminal lane.
	m.AddTerminalObserver(func(ccid string, _ Outcome, _ string) {
		r.forget(ccid)
	})
	// Seed the map with the sessions the manager ALREADY tracks (a reaper
	// started after calls were answered; the observers above only see
	// future edges). Only phone-leg ids are probed — the tracked set minus
	// the local/signal prefixes.
	now := time.Now()
	for _, ccid := range m.trackedCallIDs() {
		r.touchAt(ccid, now)
	}
	r.logger.Info("voice reaper started",
		"interval", cfg.interval().String(),
		"dead_air", cfg.deadAir().String(),
		"max_duration", cfg.maxDuration().String())
	t := time.NewTicker(cfg.interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("voice reaper stopped")
			return
		case <-t.C:
			r.sweep(ctx)
		}
	}
}

// touch refreshes lastActivity for ccid to now.
func (r *reaper) touch(ccid string) {
	r.touchAt(ccid, time.Now())
}

// touchAt refreshes lastActivity for ccid to at.
func (r *reaper) touchAt(ccid string, at time.Time) {
	if ccid == "" {
		return
	}
	r.mu.Lock()
	r.lastActivity[ccid] = at
	r.mu.Unlock()
}

// forget drops ccid from the reaper's map (terminal observer lane): the
// map stays bounded by the live-call count.
func (r *reaper) forget(ccid string) {
	r.mu.Lock()
	delete(r.lastActivity, ccid)
	r.mu.Unlock()
}

// sweep runs one sweep: copy the {ccid: lastActivity} map under the reaper
// mutex, then probe SERIALLY outside every lock (never hold locks across
// HTTP). MaxDuration overrides dead air; dead air gates the hangup-lost
// path. A probe error is logged and the call left alone — retried next
// tick, never fatal to the loop.
func (r *reaper) sweep(ctx context.Context) {
	r.mu.Lock()
	snapshot := make(map[string]time.Time, len(r.lastActivity))
	maps.Copy(snapshot, r.lastActivity)
	r.mu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	now := time.Now()
	for ccid, last := range snapshot {
		if ctx.Err() != nil {
			return
		}
		silent := now.Sub(last)
		// MaxDuration is the absolute cap from call start: force-hangup
		// WITHOUT asking the carrier about aliveness (an over-long call
		// ends even if the carrier still reports it alive). The seed time
		// (answered observer / startup snapshot) IS the call start on the
		// reaper's clock; caller speech refreshes lastActivity, so a call
		// silent the whole call ages exactly like the call itself.
		if silent > r.cfg.maxDuration() {
			r.reapMaxDuration(ctx, ccid, silent)
			continue
		}
		// DEAD-AIR gate: fresh caller activity means never probe-release,
		// regardless of call age. (The probe still runs so the aliveness
		// evidence stays fresh — the release is what dead air gates.)
		st, err := r.probe(ctx, ccid)
		if err != nil {
			r.logger.Warn("voice reaper: status check failed (retried next sweep)",
				"err", err, "call_control_id", ccid, "silent_seconds", int(silent.Seconds()))
			continue
		}
		if st.Known && st.Alive {
			r.logger.Debug("voice reaper: call still alive",
				"call_control_id", ccid, "silent_seconds", int(silent.Seconds()))
			continue
		}
		if silent <= r.cfg.deadAir() {
			// Carrier already dead but the caller was active within
			// DeadAir: hold the release until true silence. (A dead
			// carrier with fresh local activity is the webhook-loss race;
			// the DEAD-AIR rule is the owner's ratified posture.)
			continue
		}
		r.reapHangupLost(ctx, ccid, silent)
	}
}

// probe asks the carrier about one call under its own timeout context
// (per-probe timeout, serial; the sweep ctx can still cancel it).
func (r *reaper) probe(ctx context.Context, ccid string) (telnyx.CallStatus, error) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return r.cfg.Status.GetCallStatus(pctx, ccid)
}

// reapHangupLost releases a carrier-dead call through the FailOutcome
// funnel: best-effort carrier hangup first (a missed webhook usually means
// the carrier still holds the leg), then the failed release with the
// frozen reason so CallEventErrored rides the leg bus. releaseOnce
// dedupes against a concurrent real hangup.
func (r *reaper) reapHangupLost(ctx context.Context, ccid string, silent time.Duration) {
	r.logger.Warn("voice reaper: releasing a call whose hangup never arrived (dead air past DeadAir)",
		"call_control_id", ccid, "reason", "reaped:hangup-lost",
		"silent_seconds", int(silent.Seconds()))
	r.hangupCarrier(ctx, ccid)
	r.logError(ccid, fmt.Sprintf("call reaped: hangup webhook never arrived (silent %ds)", int(silent.Seconds())))
	r.mgr.FailOutcome(ccid, OutcomeFailed, "reaped:hangup-lost")
}

// reapMaxDuration force-ends a call past MaxDuration: carrier hangup
// best-effort, then the failed release with the frozen reason.
func (r *reaper) reapMaxDuration(ctx context.Context, ccid string, age time.Duration) {
	r.logger.Warn("voice reaper: ending a call past max duration",
		"call_control_id", ccid, "reason", "reaped:max-duration",
		"age_seconds", int(age.Seconds()))
	r.hangupCarrier(ctx, ccid)
	r.logError(ccid, fmt.Sprintf("call reaped: max duration exceeded (%ds)", int(age.Seconds())))
	r.mgr.FailOutcome(ccid, OutcomeFailed, "reaped:max-duration")
}

// hangupCarrier is the best-effort carrier POST (phone-led only; a nil
// seam skips it — the releaseOnce teardown still completes).
func (r *reaper) hangupCarrier(ctx context.Context, ccid string) {
	if r.tel == nil {
		return
	}
	hctx, cancel := context.WithTimeout(ctx, hangupTimeout)
	defer cancel()
	if err := r.tel.HangupCall(hctx, ccid); err != nil {
		r.logger.Warn("voice reaper: carrier hangup failed (best-effort)",
			"err", err, "call_control_id", ccid)
	}
}

// logError records the fault when the Errors logger is set; a logging
// failure is loud but never fatal (the release proceeds).
func (r *reaper) logError(ccid, msg string) {
	if r.cfg.Errors == nil {
		return
	}
	if err := r.cfg.Errors.LogCallError(ccid, msg); err != nil {
		r.logger.Error("voice reaper: could not record call_error",
			"err", err, "call_control_id", ccid)
	}
}
