package voice

// Partial context warm — debounce + routing (phase2-context-warm leaf 02,
// root Contract C6).
//
// A live voice call streams STT PARTIALS (C1 prompt frames with last !=
// true) at the daemon long before the caller finishes. The imminent real
// turn pays full-prompt prefill on its first token; a max_tokens=1 warm
// request (llm.Client.Warm, Contract C4) over the SAME rendered prefix,
// issued under the SAME session-affinity scope, plants that prefix in the
// provider-side prompt cache (and the llama.cpp KV slot) so the turn that
// follows pays decode, not prefill.
//
// Design (leaf 02 scope): this file is SELF-CONTAINED in package voice —
// it never imports internal/daemon (import cycle). The daemon's
// integration commit supplies the two real closures (the prefix builder
// renders the turn's messages exactly as the imminent turn will; the warm
// fn forwards to the call's reconfigured local llm client) and calls the
// package-level Route* seam from routeFrame / releaseCall. See
// SetContextWarmRouting for the wiring seam.
//
// Invariants:
//   - voice.context_warm defaults FALSE: disabled ⇒ every entry point is a
//     pure no-op (byte-identical dispatch), no timer, no builder call.
//   - No I/O under the mutex (mutexio): the lock guards map/timer state
//     only; BuildPrefix and Warm run after unlock.
//   - Partial text is NEVER logged at INFO; Debug level only, length-only
//     by default.
//   - At most one in-flight warm per session client-side: per-session
//     debounce window (resolved voice.turn_confirm_ms; 0/legacy ⇒ 900 ms
//     fixed, the same default the child uses) plus a 150 ms fire throttle.
//     (The llm warm lane is cap-1 replace-discard anyway — this is the
//     cheap client-side backstop the C6 contract pins.)
//   - A final (last:true) or call end cancels the pending timer AND the
//     in-flight warm's context (Warm derives its request ctx from the
//     cancel func passed per fire), so no zombie request outlives the
//     superseding turn or the call.
//   - nil warmer / nil deps ⇒ inert (every entry point nil-safe).

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Default values for the context-warm knobs (C6 + the leaf-02 brief).
const (
	// DefaultContextWarmWindowMs is the fixed debounce window when the
	// resolved voice.turn_confirm_ms is 0 (legacy per-final mode has no
	// window to inherit; 900 ms matches the child's shipped default).
	DefaultContextWarmWindowMs = 900

	// DefaultContextWarmMinTokens is C6's voice.context_warm_min_tokens
	// default: warming a trivial prefix is pointless traffic.
	DefaultContextWarmMinTokens = 60

	// defaultContextWarmThrottleMs is the per-session minimum spacing
	// between two warm dispatches (cheap client-side throttle; the
	// replace-discard lane makes a faster cadence useless anyway).
	defaultContextWarmThrottleMs = 150
)

// WarmFn is the warm-dispatch seam (mirrors llm.Client.Warm's signature so
// the daemon wires it as a direct method value). Fire-and-forget: it never
// returns a result; errors are the callee's (DEBUG, swallowed) per C4.
type WarmFn func(ctx context.Context, alias string, messages []llm.ChatMessage, scope string)

// WarmPrefix is one rendered warm candidate: the EXACT message list the
// imminent real turn will send (system + persona + history + partial user
// text), the alias the turn will use (Warm's DEBUG attribution only), and
// the session-affinity scope string the turn stamps via WithTaskScope —
// never a new scope (a divergence on ANY field silently kills the cache
// hit, which is the whole feature).
type WarmPrefix struct {
	Messages []llm.ChatMessage
	Alias    string
	Scope    string
}

// WarmPrefixFn renders the warm prefix for a live call session, or ok=false
// when there is nothing warmable (no session, no history, unresolvable
// turn client). ok=false skips the fire (Debug log, no counter).
type WarmPrefixFn func(employeeID, sessionID, partialText string) (WarmPrefix, bool)

// ContextWarmConfig carries the resolved C6 knobs. Zero value = disabled.
type ContextWarmConfig struct {
	Enabled   bool          // voice.context_warm
	MinTokens int           // voice.context_warm_min_tokens (<=0 ⇒ default 60)
	Window    time.Duration // debounce window = resolved voice.turn_confirm_ms (<=0 ⇒ 900 ms default)
	Throttle  time.Duration // min spacing between fires per session (<=0 ⇒ 150 ms default)
}

// ContextWarmDeps are the constructed dependencies. All optional: a nil
// Warm or BuildPrefix makes the warmer inert (never a panic, never a
// partial wiring that dispatches unrendered traffic).
type ContextWarmDeps struct {
	Warm        WarmFn
	BuildPrefix WarmPrefixFn
	// EstimateTokens overrides the rough prefix-token estimator (tests);
	// nil ⇒ the package heuristic (3 chars/token over message content,
	// mirroring llm.EstimateTokenCountHeuristic).
	EstimateTokens func(msgs []llm.ChatMessage) int
	// Logger is the voice subsystem logger (Debug-only emissions); nil ⇒
	// slog.Default().
	Logger *slog.Logger
	// BaseCtx parents every warm request ctx (the daemon passes the
	// subsystem ctx so daemon shutdown cancels in-flight warms); nil ⇒
	// context.Background().
	BaseCtx context.Context
}

// ContextWarmer is the per-daemon debounce/routing state machine for
// partial→warm traffic, keyed by call_control_id. Construct it once with
// NewContextWarmer after config load; register it with
// SetContextWarmRouting so the routeFrame seam finds it.
type ContextWarmer struct {
	cfg  ContextWarmConfig
	deps ContextWarmDeps
	log  *slog.Logger

	mu       sync.Mutex
	sessions map[string]*warmSession

	warms   atomic.Int64 // dispatched WarmFn calls (DEBUG-grade counter)
	skipped atomic.Int64 // floor/skip outcomes (DEBUG-grade counter)
}

// warmSession is one call's debounce state. Owned by ContextWarmer.mu;
// never touched under another lock; never does I/O.
type warmSession struct {
	employeeID string
	sessionID  string
	pending    string             // latest partial text (replaced on every OnPartial)
	timer      *time.Timer        // pending debounce timer (nil when nothing pending)
	lastFire   time.Time          // wall clock of the last dispatched fire (throttle)
	gen        uint64             // bumped on every (re)scheduled timer; stale callbacks self-retire
	cancelled  bool               // OnFinal/OnCallEnd set; a stale in-goroutine fire checks it
	inflight   context.CancelFunc // cancel of the most recent dispatched warm request
}

// NewContextWarmer builds the warmer. A disabled cfg (the default) yields
// an inert instance: every entry point returns before touching state, so
// the pre-feature dispatch timing is byte-identical.
func NewContextWarmer(cfg ContextWarmConfig, deps ContextWarmDeps) *ContextWarmer {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.MinTokens <= 0 {
		cfg.MinTokens = DefaultContextWarmMinTokens
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultContextWarmWindowMs * time.Millisecond
	}
	if cfg.Throttle <= 0 {
		cfg.Throttle = defaultContextWarmThrottleMs * time.Millisecond
	}
	if deps.BaseCtx == nil {
		deps.BaseCtx = context.Background()
	}
	return &ContextWarmer{
		cfg:      cfg,
		deps:     deps,
		log:      log.With("sub", "context-warm"),
		sessions: map[string]*warmSession{},
	}
}

// Enabled reports whether this warmer dispatches anything.
func (cw *ContextWarmer) Enabled() bool {
	return cw != nil && cw.cfg.Enabled && cw.deps.Warm != nil && cw.deps.BuildPrefix != nil
}

// Stats returns the DEBUG-grade counters (dispatched warms, floor/skip
// discards). The leaf-02 contract keeps telemetry "DEBUG-grade cheap":
// no new bus topic, no metrics.Store write — the llm_calls table shows
// zero warm rows by C4 design.
func (cw *ContextWarmer) Stats() (warms int64, skipped int64) {
	if cw == nil {
		return 0, 0
	}
	return cw.warms.Load(), cw.skipped.Load()
}

// OnPartial records a live-call STT partial (C1 prompt frame, last !=
// true) and (re)arms the debounce timer. The newest partial replaces the
// pending text: at most ONE warm per window per session, always with the
// latest words. Disabled / empty text ⇒ pure no-op, no state allocated.
func (cw *ContextWarmer) OnPartial(ccid, employeeID, sessionID, text string) {
	if !cw.Enabled() {
		return
	}
	text = strings.TrimSpace(text)
	if ccid == "" || text == "" {
		return
	}

	cw.mu.Lock()
	st := cw.sessions[ccid]
	if st == nil {
		st = &warmSession{}
		cw.sessions[ccid] = st
	}
	st.employeeID = employeeID
	st.sessionID = sessionID
	st.pending = text
	st.cancelled = false
	// Latest-wins: stop the old timer and re-arm the window from NOW.
	// The stale callback (if it already started running) self-retires on
	// the gen check / empty-pending read under the lock.
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	st.gen++
	gen := st.gen
	window := cw.cfg.Window
	cw.mu.Unlock()

	// time.AfterFunc's callback runs on the runtime timer goroutine;
	// fireLocked-then-dispatch does all real work OUTSIDE cw.mu.
	timer := time.AfterFunc(window, func() { cw.fire(ccid, gen) })

	cw.mu.Lock()
	// The session may have ended between unlock and timer creation —
	// only keep the timer if this gen is still the live one.
	if cur := cw.sessions[ccid]; cur != nil && cur.gen == gen {
		cur.timer = timer
	} else {
		timer.Stop()
	}
	cw.mu.Unlock()
}

// OnFinal marks the call's superseding real turn: cancel the pending
// debounce and the in-flight warm's context. The warm is fire-and-forget
// (nothing to await); abandoning it is exactly "cancel pending + discard
// in-flight" per C6. A later partial in the same call re-arms normally
// (the caller kept talking — the next turn is warmable again).
func (cw *ContextWarmer) OnFinal(ccid string) {
	cw.abandon(ccid, "final")
}

// OnCallEnd tears the session down entirely (hangup / releaseCall /
// reaper): state entry deleted, timer stopped, in-flight warm's ctx
// cancelled. Safe on unknown ccids (idempotent teardown).
func (cw *ContextWarmer) OnCallEnd(ccid string) {
	if cw == nil {
		return
	}
	cw.mu.Lock()
	st := cw.sessions[ccid]
	if st != nil {
		delete(cw.sessions, ccid)
	}
	cw.mu.Unlock()
	cw.abandonState(ccid, st, "call_end")
}

func (cw *ContextWarmer) abandon(ccid, reason string) {
	if cw == nil {
		return
	}
	cw.mu.Lock()
	st := cw.sessions[ccid]
	cw.mu.Unlock()
	cw.abandonState(ccid, st, reason)
}

// abandonState stops/cancels one session's pending + in-flight warm.
// Called OUTSIDE cw.mu except for the map reads in the callers; the
// session pointer's fields are themselves cw.mu-guarded.
func (cw *ContextWarmer) abandonState(ccid string, st *warmSession, reason string) {
	if st == nil {
		return
	}
	cw.mu.Lock()
	st.cancelled = true
	st.pending = ""
	timer := st.timer
	st.timer = nil
	inflight := st.inflight
	st.inflight = nil
	cw.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if inflight != nil {
		inflight()
	}
	cw.log.Debug("context warm abandoned", "ccid", ccid, "reason", reason)
}

// fire is the debounce expiry path. Under the lock it validates the
// generation, applies the throttle (re-arm, never drop: the latest text
// still deserves its warm when the window is shorter than the throttle),
// and snapshots the render inputs; all I/O (render + dispatch) happens
// after unlock.
func (cw *ContextWarmer) fire(ccid string, gen uint64) {
	cw.mu.Lock()
	st := cw.sessions[ccid]
	if st == nil || st.gen != gen || st.cancelled || st.pending == "" {
		cw.mu.Unlock() // stale callback (replaced / abandoned / ended)
		return
	}
	now := time.Now()
	if !st.lastFire.IsZero() && now.Sub(st.lastFire) < cw.cfg.Throttle {
		// Throttled: keep the text pending and re-arm for the remainder.
		remain := cw.cfg.Throttle - now.Sub(st.lastFire) + time.Millisecond
		st.gen++
		nextGen := st.gen
		cw.mu.Unlock()
		timer := time.AfterFunc(remain, func() { cw.fire(ccid, nextGen) })
		cw.mu.Lock()
		if cur := cw.sessions[ccid]; cur != nil && cur.gen == nextGen {
			cur.timer = timer
		} else {
			timer.Stop()
		}
		cw.mu.Unlock()
		return
	}
	text := st.pending
	employeeID := st.employeeID
	sessionID := st.sessionID
	st.pending = ""
	st.timer = nil
	st.lastFire = now
	// One in-flight per session: a previous warm still riding the request
	// ctx is cancelled here (replace semantics, latest prefix wins).
	prevInflight := st.inflight
	st.inflight = nil
	cw.mu.Unlock()

	if prevInflight != nil {
		prevInflight()
	}

	// I/O-free snapshot done — render OUTSIDE the lock.
	prefix, ok := cw.deps.BuildPrefix(employeeID, sessionID, text)
	if !ok {
		cw.skipped.Add(1)
		cw.log.Debug("context warm skipped: no prefix", "ccid", ccid, "employee_id", employeeID)
		return
	}
	if tokens := cw.estimate(prefix.Messages); tokens < cw.cfg.MinTokens {
		cw.skipped.Add(1)
		cw.log.Debug("context warm skipped: below token floor",
			"ccid", ccid, "est_tokens", tokens, "floor", cw.cfg.MinTokens)
		return
	}

	reqCtx, cancel := context.WithCancel(cw.deps.BaseCtx)
	cw.mu.Lock()
	if cur := cw.sessions[ccid]; cur != nil && cur.gen == gen && !cur.cancelled {
		cur.inflight = cancel
		cw.mu.Unlock()
	} else {
		// Abandoned or superseded between snapshot and dispatch: never
		// issue a warm for a dead call.
		cw.mu.Unlock()
		cancel()
		return
	}

	cw.warms.Add(1)
	// Warm is fire-and-forget by C4; the ctx is this request's abandon
	// handle (OnFinal/OnCallEnd cancel it). The completion is discarded
	// and never logged by the callee.
	cw.deps.Warm(reqCtx, prefix.Alias, prefix.Messages, prefix.Scope)
}

// estimate is the rough token floor (never precise — the floor only
// rejects trivial prefixes). The default mirrors the llm heuristic
// tokenizer (3 chars/token) over message content + names.
func (cw *ContextWarmer) estimate(msgs []llm.ChatMessage) int {
	if cw.deps.EstimateTokens != nil {
		return cw.deps.EstimateTokens(msgs)
	}
	total := 0
	for _, m := range msgs {
		total += llm.EstimateTokenCountHeuristic(m.Content)
		if m.Name != "" {
			total += llm.EstimateTokenCountHeuristic(m.Name)
		}
	}
	return total
}

// ---- daemon wiring seam ----
//
// The daemon's VoiceWiring owns exactly three integration points and this
// package owns the state machine; the glue is ONE registered warmer so the
// routeFrame hot path needs no pointer threading through the binding
// layers. nil-safe in every direction: an unregistered warmer (feature
// disabled, or a wiring-less build/test harness) makes Route* pure no-ops.

var contextWarmRouting atomic.Pointer[ContextWarmer]

// SetContextWarmRouting registers the daemon's warmer (integration commit;
// call once after config load, or nil at teardown to unregister). A
// disabled warmer is cheap to register — its entry points no-op before any
// allocation — but passing nil when voice.context_warm is false keeps the
// routing path allocation-free.
func SetContextWarmRouting(cw *ContextWarmer) {
	if cw == nil {
		contextWarmRouting.Store(nil)
		return
	}
	contextWarmRouting.Store(cw)
}

// ContextWarmRouting returns the registered warmer (nil when unset).
func ContextWarmRouting() *ContextWarmer { return contextWarmRouting.Load() }

// RoutePartial / RouteFinal / RouteEnd are the nil-safe package entry
// points the daemon calls from VoiceWiring.routeFrame (partial/final prompt
// branches) and VoiceWiring.releaseCall. They are the ONLY symbols the
// integration commit needs from this file beyond the constructor.
func RoutePartial(ccid, employeeID, sessionID, text string) {
	if cw := contextWarmRouting.Load(); cw != nil {
		cw.OnPartial(ccid, employeeID, sessionID, text)
	}
}

// RouteFinal abandons pending/in-flight warms when a last:true prompt
// frame starts the real turn (the turn supersedes every warm).
func RouteFinal(ccid string) {
	if cw := contextWarmRouting.Load(); cw != nil {
		cw.OnFinal(ccid)
	}
}

// RouteEnd tears the call's warm state down at release.
func RouteEnd(ccid string) {
	if cw := contextWarmRouting.Load(); cw != nil {
		cw.OnCallEnd(ccid)
	}
}
