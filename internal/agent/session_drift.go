package agent

import (
	"container/list"
	"log/slog"
	"math"
	"sync"

	"github.com/caimlas/meept/internal/config"
)

// SessionDriftDetector watches the per-session sequence of intent
// embeddings and raises a drift signal when the recent pattern deviates
// from the trajectory established earlier in the session (meept issue
// #41). It is not a classifier — it is a change detector for a temporal
// stream: it never judges what an embedding means, only whether the
// session's embedding distribution has moved. Intended use is alongside
// the per-message classifier: when drift fires, the dispatcher can
// re-run the full classification chain instead of trusting a per-message
// verdict.
//
// Privacy: the detector sees embeddings and session IDs only. Observe
// never accepts raw text, and no raw text ever appears in logs or
// errors — session IDs and numeric scores only.
//
// Score (both halves are of the CURRENT window's content):
//
//	Let W be the rolling window of the last N normalized embeddings,
//	split asymmetrically so the current side is purely post-shift data
//	(a symmetric 6/6 split keeps the "current" half contaminated with
//	pre-shift embeddings for ~N/6 extra turns): B = W[0 : N-ncur]
//	(older baseline) and C = W[N-ncur : N] (newest ncur), with
//	ncur = N/3 clamped to [2, N/2] — 8/4 at the default N=12.
//	Normalize each half's mean to unit length, then
//
//	  shift = 1 - cos(mean(B), mean(C))     (how far the stream moved)
//	  rate  = 1 - cos(prevMean(B), mean(B)) (how fast the baseline is
//	          itself moving; prevMean(B) is the baseline half's mean as
//	          of the previous scored turn, zero-vector at the first
//	          scored turn so rate = 1 there — after clamp, see below)
//	  score = clamp01(shift + 0.5 * rate)
//
//	Kept in [0,1] by construction (cosine distance of unit vectors is
//	in [0,2] in theory but ~[0,1] in practice; the clamp makes the
//	range contract explicit).
//
// Default threshold derivation (measured by simulation, 16-dim noisy
// unit vectors, 8/4 split): the 97.5th-percentile principle says take a
// scaled quantile of the score distribution over stable traffic. At
// turn-to-turn noise sigma=0.3 (cosine similarity to the session intent
// >= ~0.95, a realistic per-message classifier embedding) the stable
// score distribution has p97.5 ~= 0.40 and p99 ~= 0.43; 0.60 sits above
// those with headroom so a 100-turn stable session fires with
// probability ~1.6% — comfortably past the requirement that >= 95% of
// stable sessions produce zero signals. The headroom is deliberate:
// pushing to the raw p97.5 (~0.40) triples the false-alarm rate, while
// at higher noise (sigma=0.4) even the p97.5 is ~0.56, so no threshold
// near the quantile both holds the 95% bar there and detects anything —
// such traffic needs a raised Threshold in config. Detection at the
// default: a 90-degree intent rotation fires within <= 6 turns with
// simulated rate >= 96% at sigma <= 0.3.
//
// Warm-up: no signal until the window has filled once (N observations
// in the session). During warm-up Observe returns drifted=false and
// the score computed so far (0 before the midpoint split exists).
//
// Concurrency: all per-session state is guarded by a single mutex;
// sessions are independent so contention is negligible. The sessions
// map is capped at maxDriftSessions — inserting a new session beyond
// the cap evicts the least-recently-used one (LRU via container/list,
// chosen because Observe is a per-turn hot path and LRU keeps active
// sessions without an arbitrary re-creation cost).
//
// Log-only semantics live at the CALL SITE (config [agent]
// session_drift log_only), not here: Observe only reports (drifted,
// score). The one Info log in this file is the drift signal itself and
// carries session id, score, threshold, and window size — never
// message text.
type SessionDriftDetector struct {
	mu        sync.Mutex
	enabled   bool
	window    int // effective window size (floored at minDriftWindow)
	threshold float64
	max       int // session cap
	sessions  map[string]*driftSession
	lru       *list.List // front = most recently used session id
	logger    *slog.Logger
}

// drift defaults. See SessionDriftDetector for the threshold derivation.
const (
	defaultDriftWindow     = 12
	minDriftWindow         = 4
	defaultDriftThreshold  = 0.60
	defaultMaxDriftSession = 1024
)

// driftSession is one session's rolling embedding window plus the
// previous baseline-half mean for the rate-of-change term.
type driftSession struct {
	window   [][]float64 // normalized, oldest first, len <= window
	prevBase []float64   // previous baseline-half mean, or nil if none
}

// NewSessionDriftDetector builds the detector. A nil logger falls back
// to slog.Default. cfg.Enabled == false (the shipped default) returns
// an inert detector: Enabled() is false, Observe is a no-op returning
// (false, 0), and no per-session state is kept. WindowSize <= 0 uses
// is floored at 4; Threshold <= 0 uses the
// calibrated default (0.60) — 0 is therefore "use the default", not
// "signal on everything" (mirrors SessionDriftConfig's documented
// semantics).
func NewSessionDriftDetector(cfg config.SessionDriftConfig, logger *slog.Logger) *SessionDriftDetector {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Enabled {
		// Inert detector: nil-safe to call, keeps no state, logs
		// nothing (call sites check Enabled() first).
		return &SessionDriftDetector{enabled: false, logger: logger.With("component", "session_drift")}
	}
	w := cfg.WindowSize
	if w <= 0 {
		w = defaultDriftWindow
	}
	if w < minDriftWindow {
		w = minDriftWindow
	}
	thr := cfg.Threshold
	if thr <= 0 {
		thr = defaultDriftThreshold
	}
	return &SessionDriftDetector{
		enabled:   true,
		window:    w,
		threshold: thr,
		max:       defaultMaxDriftSession,
		sessions:  make(map[string]*driftSession),
		lru:       list.New(),
		logger:    logger.With("component", "session_drift"),
	}
}

// Enabled reports whether the detector is active. False = Observe is a
// no-op; call sites check this before doing any work.
func (d *SessionDriftDetector) Enabled() bool { return d != nil && d.enabled }

// Observe records one classified turn's embedding for sessionID and
// reports whether drift fired. One call per classified turn.
//
// Degenerate inputs (empty, dimension mismatch with the session's
// prior window, NaN/Inf, zero norm) are ignored: the observation is
// not recorded and Observe returns (false, 0) with no panic — the
// detector can only abstain, never drift on garbage (mirrors
// EmbedHealthCheck's fail-safe posture). The first accepted embedding
// fixes the session's dimension; later mismatches are dropped.
//
// Warm-up: while the window has fewer than `window` accepted
// observations, no signal can fire (drifted=false) and score is the
// running score (0 until the midpoint split exists).
func (d *SessionDriftDetector) Observe(sessionID string, embedding []float64) (drifted bool, score float64) {
	if !d.Enabled() {
		return false, 0
	}
	norm := normalizeUnit(embedding)
	if norm == nil {
		return false, 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	s, ok := d.sessions[sessionID]
	if !ok {
		s = &driftSession{}
		d.sessions[sessionID] = s
		d.lru.PushFront(sessionID)
		d.evictLocked()
	} else {
		// Touch LRU order.
		for e := d.lru.Front(); e != nil; e = e.Next() {
			if e.Value.(string) == sessionID {
				d.lru.MoveToFront(e)
				break
			}
		}
	}
	if len(s.window) > 0 && len(norm) != len(s.window[0]) {
		return false, 0 // dimension mismatch: drop the observation
	}
	s.window = append(s.window, norm)
	for len(s.window) > d.window {
		s.window = s.window[1:]
	}
	if len(s.window) < d.window {
		return false, 0 // warm-up: window not yet filled
	}

	half := d.window / 3
	if half < 2 {
		half = 2
	}
	if half > d.window/2 {
		half = d.window / 2
	}
	base := meanUnit(s.window[:d.window-half])
	cur := meanUnit(s.window[d.window-half:])
	if base == nil || cur == nil {
		return false, 0 // unreachable (inputs validated), kept defensive
	}
	shift := 1 - dot(base, cur)
	var rate float64
	if s.prevBase != nil {
		rate = 1 - dot(s.prevBase, base)
	}
	score = clamp01(shift + 0.5*rate)
	s.prevBase = base
	drifted = score >= d.threshold
	if drifted {
		d.logger.Info("session drift detected",
			"session_id", sessionID,
			"score", score,
			"threshold", d.threshold,
			"window_size", d.window,
		)
	}
	return drifted, score
}

// ResetSession drops all state for sessionID (next Observe starts a
// fresh warm-up). Safe to call for unknown sessions.
func (d *SessionDriftDetector) ResetSession(sessionID string) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.sessions[sessionID]; !ok {
		return
	}
	delete(d.sessions, sessionID)
	for e := d.lru.Front(); e != nil; e = e.Next() {
		if e.Value.(string) == sessionID {
			d.lru.Remove(e)
			break
		}
	}
}

// WindowSize and Threshold expose the effective configuration (for
// tests and call-site logging only).
func (d *SessionDriftDetector) WindowSize() int    { return d.window }
func (d *SessionDriftDetector) Threshold() float64 { return d.threshold }

// evictLocked enforces the session cap, removing the least-recently-used
// session. Caller holds d.mu.
func (d *SessionDriftDetector) evictLocked() {
	for len(d.sessions) > d.max {
		oldest := d.lru.Back()
		if oldest == nil {
			break
		}
		id := oldest.Value.(string)
		d.lru.Remove(oldest)
		delete(d.sessions, id)
	}
}

// normalizeUnit normalizes embedding to unit length, returning nil for
// degenerate input (nil/empty, NaN/Inf, zero norm).
func normalizeUnit(v []float64) []float64 {
	if len(v) == 0 {
		return nil
	}
	norm := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm <= embedHealthEps {
		return nil
	}
	inv := 1.0 / norm
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

// meanUnit averages rows (all equal length, unit-norm rows) and
// normalizes the mean to unit length. Returns nil for an empty slice.
func meanUnit(rows [][]float64) []float64 {
	if len(rows) == 0 {
		return nil
	}
	out := make([]float64, len(rows[0]))
	for _, r := range rows {
		for i, x := range r {
			out[i] += x
		}
	}
	return normalizeUnit(out)
}

// clamp01 clamps v to [0,1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
