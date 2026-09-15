package metrics

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
)

// BurstDetectionConfig mirrors config.BurstDetectionConfig (issue #43).
// Defined here, not imported, because internal/config already depends on
// this package transitively (config -> llm -> metrics): importing config
// from metrics would close an import cycle. This mirrors the established
// pattern in collector.go (event payload structs mirrored to avoid the
// agent -> metrics cycle). Field names, types, and defaults match
// internal/config/schema.go's BurstDetectionConfig exactly; wiring sites
// copy the four fields straight across.
type BurstDetectionConfig struct {
	// Enabled turns on the burst detector. Default false.
	Enabled bool
	// WindowSize is the length of the rolling outcome window, in
	// turns. 0 uses the built-in default (20).
	WindowSize int
	// Threshold is the anomaly score above which a burst is signaled.
	// 0 uses the built-in default (0.50).
	Threshold float64
	// LogOnly makes the signal observational: bursts are logged but
	// never trigger replans. Default true. Not read by the detector
	// itself — it reports only; LogOnly is enforced at the call site.
	LogOnly bool
}

// BurstDetector watches the per-session dispatch outcome stream
// (ok / corrected / failed_replan) for tool-failure bursts (meept issue
// #43). The sibling of agent.EmbedHealthCheck (issue #42): pure Go,
// deterministic, no network, log-only.
//
// Mechanism — temporal integration with a per-session adaptation
// baseline. Each outcome maps to a numeric failure signal:
//
//	ok             -> 0.0  (routed and stayed routed: nothing failed)
//	corrected      -> 0.5  (re-route within window: partial failure)
//	failed_replan  -> 1.0  (step failed hard enough to replan)
//
// corrected earns 0.5 because it means the first routing was wrong
// enough that the user (or the re-route detector) had to fix it, but the
// turn still completed without a replan — halfway between a clean
// dispatch and a hard failure. The 0/0.5/1 ladder is ordinal; no claim
// is made that corrected is exactly half as bad.
//
// Per session, two exponentially-weighted means run over that signal:
//
//	ewmaFast = ewmaFast*(1-aFast) + s*aFast   (aFast = 0.40)
//	ewmaSlow = ewmaSlow*(1-aSlow) + s*aSlow   (aSlow = 0.02)
//
// The burst score is their difference, clamped to [0,1]:
//
//	score = clamp(ewmaFast - ewmaSlow, 0, 1)
//
// The slow mean is the session's adaptation baseline: it tracks the
// session's ordinary failure mixture and cancels any constant component.
// A *burst* — a recent run of failures above that baseline — pushes
// ewmaFast up 20x faster than the baseline moves, so the gap widens
// quickly when failures cluster and stays near zero when they don't.
// Clamping to [0,1] keeps the score a rate-like quantity (a session
// running better than its baseline simply reads 0).
//
// Constants were calibrated on synthetic outcome streams (verified in
// burst_detector_test.go): with aFast=0.40, aSlow=0.02 and the warm-up
// rule below, Threshold default 0.50 kept the empirical false-alarm rate
// at 0/1000 known-good synthetic sessions (all-ok, 15% and 30%
// corrected mixes, 120 turns each) — comfortably under the <5%
// acceptance bound — while a gradual degradation (failure probability
// ramping 0.02 -> 1.0 over 100 turns) raised the score past threshold a
// median of ~12 turns before the plain "3 consecutive failures" rule,
// and an abrupt burst (2% -> 100% step at turn 60) fired within ~1 turn
// of it (the threshold rule is faster on a pure all-fail step; see the
// test's honest comparison).
//
// Warm-up: no signal until the rolling window has filled once, so
// startup sequences can never read as a burst. The window is a rolling
// list of the last cfg.WindowSize signals (default 20, floored at 5)
// kept so ThresholdRule can read the same history and the window-fill
// rule is exact.
//
// Policy: the detector REPORTS ONLY. Replan / clarify / model-switch
// decisions stay at the call site; LogOnly (config) is likewise a
// call-site concern. Privacy: inputs and state are session ids and
// outcome labels only — no raw text exists on this API by construction.
//
// Concurrency: all per-session state is mutex-guarded; safe for
// concurrent use. The per-session map is capped (burstMaxSessions):
// the least-recently-touched session is evicted when the cap is hit,
// and ResetSession drops one session's state explicitly.
type BurstDetector struct {
	mu      sync.Mutex
	enabled bool
	window  int
	thr     float64
	aFast   float64
	aSlow   float64
	// maxSessions caps the per-session map; 0 = unlimited.
	maxSessions int
	// sessions is keyed by session id; order is the LRU touch order
	// (oldest first) so eviction is deterministic. Both are only
	// accessed under mu.
	sessions map[string]*burstState
	order    []string
	logger   *slog.Logger
}

// burstState is one session's rolling outcome window and EWMA pair.
type burstState struct {
	window   []float64 // recent signals, oldest first
	ewmaFast float64
	ewmaSlow float64
}

// Burst detector constants. Window: 20 covers a session's recent
// history without diluting a burst; 5 is the smallest window where the
// warm-up rule still admits a meaningful EWMA comparison. Threshold
// 0.50: see the calibration paragraph in the BurstDetector comment.
const (
	burstDefaultWindow    = 20
	burstMinWindow        = 5
	burstDefaultThreshold = 0.50
	burstDefaultAlphaFast = 0.40
	burstDefaultAlphaSlow = 0.02

	// burstMaxSessions caps tracked sessions; least-recently-touched
	// is evicted at the cap. Session count is bounded in practice by
	// daemon lifetime, but an unbounded map is a slow leak on
	// long-lived daemons.
	burstMaxSessions = 4096
)

// NewBurstDetector returns a detector configured from cfg. When
// cfg.Enabled is false the detector is INERT: ObserveOutcome and
// ThresholdRule return no-signal immediately, mirroring the inert
// posture of the embed health check. A nil logger falls back to
// slog.Default().
func NewBurstDetector(cfg BurstDetectionConfig, logger *slog.Logger) *BurstDetector {
	if logger == nil {
		logger = slog.Default()
	}
	d := &BurstDetector{
		enabled:     cfg.Enabled,
		window:      burstDefaultWindow,
		thr:         burstDefaultThreshold,
		aFast:       burstDefaultAlphaFast,
		aSlow:       burstDefaultAlphaSlow,
		maxSessions: burstMaxSessions,
		sessions:    make(map[string]*burstState),
		logger:      logger.With("component", "burst_detector"),
	}
	if cfg.WindowSize > 0 {
		d.window = cfg.WindowSize
	}
	if d.window < burstMinWindow {
		d.window = burstMinWindow
	}
	if cfg.Threshold > 0 {
		d.thr = cfg.Threshold
	}
	return d
}

// Enabled reports whether the detector is active. False = ObserveOutcome
// and ThresholdRule are inert (never signal).
func (d *BurstDetector) Enabled() bool { return d.enabled }

// Threshold returns the effective burst threshold (for log lines and
// diagnostics at wiring/call sites).
func (d *BurstDetector) Threshold() float64 { return d.thr }

// WindowSize returns the effective rolling-window size (diagnostics).
func (d *BurstDetector) WindowSize() int { return d.window }

// outcomeSignal maps an outcome label to its failure weight. Unknown
// labels (including 'pending') map to 0 — they carry no failure
// evidence either way, and the stream this detector consumes is
// resolved rows only.
func outcomeSignal(outcome string) float64 {
	switch outcome {
	case "ok":
		return 0.0
	case "corrected":
		return 0.5
	case "failed_replan":
		return 1.0
	default:
		return 0.0
	}
}

// ObserveOutcome feeds one resolved outcome into the session's rolling
// window and returns the burst verdict: burst=true when the score is at
// or above the threshold and the window has filled (warm-up). The score
// is returned even when no burst fired so callers can log the
// trajectory.
//
// The detector reports only; replan/clarify decisions stay at the call
// site.
func (d *BurstDetector) ObserveOutcome(sessionID string, outcome string) (burst bool, score float64) {
	if !d.enabled {
		return false, 0
	}
	s := outcomeSignal(outcome)

	d.mu.Lock()
	defer d.mu.Unlock()

	st, ok := d.sessions[sessionID]
	if !ok {
		st = &burstState{}
		d.sessions[sessionID] = st
		d.order = append(d.order, sessionID)
		if len(d.order) > d.maxSessions {
			evict := d.order[0]
			d.order = d.order[1:]
			delete(d.sessions, evict)
		}
	} else {
		// Refresh LRU position (touch).
		for i, id := range d.order {
			if id == sessionID {
				copy(d.order[i:], d.order[i+1:])
				d.order[len(d.order)-1] = sessionID
				break
			}
		}
	}

	st.window = append(st.window, s)
	if len(st.window) > d.window {
		st.window = st.window[1:]
	}
	st.ewmaFast = st.ewmaFast*(1-d.aFast) + s*d.aFast
	st.ewmaSlow = st.ewmaSlow*(1-d.aSlow) + s*d.aSlow

	score = st.ewmaFast - st.ewmaSlow
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	burst = len(st.window) >= d.window && score >= d.thr
	if burst {
		d.logger.Info("tool-failure burst detected",
			"session", sessionID,
			"score", fmt.Sprintf("%.3f", score),
			"threshold", fmt.Sprintf("%.3f", d.thr),
			"window_size", d.window,
		)
	}
	return burst, score
}

// ThresholdRule is the baseline comparator for the mandated two-layer
// comparison: plain "the last n outcomes in this session's window were
// all failed_replan". It reads the same window ObserveOutcome
// maintains — call ObserveOutcome for each outcome, then poll this.
// n <= 0 defaults to 3 (3 consecutive hard failures is the smallest
// pattern a human would call a burst). Inert when disabled.
//
// The rule has no adaptation baseline and no memory beyond "the last n
// all failed"; it exists so the EWMA detector's latency can be measured
// against it on identical streams (issue #43 acceptance criterion 2).
func (d *BurstDetector) ThresholdRule(sessionID string, n int) bool {
	if !d.enabled {
		return false
	}
	if n <= 0 {
		n = 3
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st, ok := d.sessions[sessionID]
	if !ok || len(st.window) < n {
		return false
	}
	for i := len(st.window) - n; i < len(st.window); i++ {
		if st.window[i] < 1.0 {
			return false
		}
	}
	return true
}

// ResetSession drops a session's detector state (window and EWMAs).
// Safe to call for unknown sessions; inert when disabled.
func (d *BurstDetector) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.sessions[sessionID]; !ok {
		return
	}
	delete(d.sessions, sessionID)
	for i, id := range d.order {
		if id == sessionID {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}

// ResolvedOutcome is one resolved dispatch outcome reconstructed from
// dispatch_log (see Store.LoadSessionOutcomes).
type ResolvedOutcome struct {
	TurnNo         int
	Outcome        string
	CorrectedAgent string
}

// LoadSessionOutcomes reconstructs a session's resolved dispatch
// outcome sequence from dispatch_log so callers can replay it through
// BurstDetector.ObserveOutcome (issue #43: offline backtest over real
// session history). Returns the most recent `limit` rows (limit <= 0
// defaults to the burst detector window, 20) ordered by turn_no
// ascending. Only existing columns are read; the schema is untouched.
func (s *Store) LoadSessionOutcomes(sessionID string, limit int) ([]ResolvedOutcome, error) {
	if limit <= 0 {
		limit = burstDefaultWindow
	}
	var rows []struct {
		TurnNo         int    `db:"turn_no"`
		Outcome        string `db:"outcome"`
		CorrectedAgent string `db:"corrected_agent"`
	}
	if err := s.db.Select(&rows,
		`SELECT turn_no, outcome, corrected_agent FROM dispatch_log
		 WHERE session_id = ? ORDER BY turn_no DESC LIMIT ?`,
		sessionID, limit,
	); err != nil {
		return nil, fmt.Errorf("failed to load dispatch outcomes for session %s: %w", sessionID, err)
	}
	out := make([]ResolvedOutcome, 0, len(rows))
	for _, r := range slices.Backward(rows) {
		out = append(out, ResolvedOutcome{
			TurnNo:         r.TurnNo,
			Outcome:        r.Outcome,
			CorrectedAgent: r.CorrectedAgent,
		})
	}
	return out, nil
}
