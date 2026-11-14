package voice

// Talk loop (leaf 14): the per-call goroutine that turns final prompts into
// supervised speech. Contract assembly point for C1 (frames over the text
// socket), C6 (supervisor verdicts), and the leaf-12 binding (employee
// turns on the bound session).
//
// Flow per final prompt (last:true):
//
//	prompt -> delayed filler: arm a FillerGrace timer and speak the
//	         round-robin filler line only if the turn has not produced
//	         its first allowed sentence inside the grace window
//	         (FillerGrace == 0 keeps the legacy immediate behavior)
//	       -> Binding turn (employee draft on the pinned endpoint)
//	       -> SplitSentences
//	-> pipelined supervisor Check (sentence N+1 is checked while
//	         sentence N is playing)
//	-> allowed sentences become say frames; denied sentences are
//	         dropped; a check FAILURE speaks the C6 fallback line
//	-> the draft's turn-final say flush (last:true) goes out exactly
//	         once per draft: carried by the final allowed sentence or the
//	         fallback line, or sent as an empty-text marker at drain when
//	         no verdict carried it (denied final sentence after earlier
//	         spoken sentences, empty reply)
//
// Fact-free lines (greeting on connect, filler on each final prompt) bypass
// the supervisor by contract C1 ("say only after supervisor allow, except
// filler/greeting").
//
// Concurrency shape: Run is the single goroutine that reads inbound frames
// AND plays verdicts, so barge-in frames are consumed while a draft is
// speaking - the interrupt contract cannot be blocked by playback. The
// checker goroutine runs Supervisor.Check one sentence ahead of playback;
// every pipeline (turn + checks) carries a generation stamped at spawn, and
// an interrupt bumps the generation and cancels the turn context, so stale
// verdicts and queued sentences are dropped unsaid. The interruptMu lock
// guards only the generation counter and cancel swap - never a send, a
// Check, or the frame loop.
//
// Timers with named reasons: max_call_minutes bounds the call; the silence
// path (silence_prompt_seconds) speaks one dedicated nudge line (voice.
// silence_prompt, "Are you still there?" by default; empty config falls
// back to the legacy filler round-robin) after the first quiet window and
// hangs up if the silence persists voice.silence_hangup_after_seconds (H)
// longer - nudge@S, hangup@(S+H). H unset (<=0) reuses S, reproducing the
// legacy double-window shape byte-for-byte.
// The silence clock is TURN-AWARE: while a prompt's employee turn is in
// flight (final prompt dispatched, reply not yet fully spoken), the
// silence window is suspended - a slow cloud LLM (30-90s) must never lose
// the call to the 45s silence reaper before its reply is spoken. Once the
// draft's verdict channel drains (reply spoken or flushed), the window
// re-arms fresh. An opt-in hard cap (voice.turn_timeout_seconds, default
// 0 = disabled) bounds a hung LLM; without it an in-flight turn can hold
// the silence path off indefinitely.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/voice/clipstore"
)

// Turn-state kinds for the link half-duplex seam (TalkDeps.TurnState).
// Emitted ONLY when deps.TurnState is non-nil - a plain (phone or local)
// call never carries turn-state traffic. turn_start = a draft dispatched
// (this side is about to speak); turn_final = the draft's say stream fully
// flushed (this side just finished speaking); idle = the side stopped
// speaking without a final (barge-in cancel, or loop teardown).
const (
	TurnKindStart = "turn_start"
	TurnKindFinal = "turn_final"
	TurnKindIdle  = "idle"
)

// fallbackSentence is the C6 fail-closed line spoken when a supervisor
// check fails or when a draft ends with nothing allowed. FallbackLine is
// the operator override; this constant is the wiring default.
const fallbackSentence = "I cannot confirm that yet."

// silenceWindowUnit converts the integer silence-window config values
// (seconds) into timer durations. It is second-scale in production; the
// test suite shrinks it to millisecond scale so the two-window
// nudge->hangup sequence pins run fast without sleeping 90 s. Tests that
// mutate it run sequentially (no package test in the silence path calls
// t.Parallel - go test runs same-package tests one at a time unless a test
// opts in).
var silenceWindowUnit = time.Second

// TalkLoopConfig carries the leaf-01 config values the loop enforces.
// Zero MaxCallMinutes or SilencePromptSeconds disables the corresponding
// timer (tests and disabled-config cases).
type TalkLoopConfig struct {
	// FillerLines are the fact-free lines spoken immediately on each final
	// prompt while the employee turn runs (config voice.filler).
	FillerLines []string
	// GreetingLine is the fact-free line spoken once on connect. Empty
	// falls back to FillerLines[0]; an empty filler list disables the
	// greeting entirely (the greeting is filler-class, never hardcoded).
	GreetingLine string
	// FallbackLine is spoken in place of failed checks (C6). Empty falls
	// back to fallbackSentence.
	FallbackLine string
	// MaxCallMinutes bounds the call length; zero disables the timer.
	MaxCallMinutes int
	// SilencePromptSeconds arms the silence path: after this long with no
	// final prompt, the loop speaks the silence nudge line; if the
	// silence persists another SilenceHangupAfterSeconds window, the
	// call hangs up. Zero disables.
	SilencePromptSeconds int
	// SilencePromptLine is the dedicated dead-air nudge spoken on the
	// FIRST silence fire (voice.silence_prompt, "Are you still there?"
	// by default). Dead air has nothing pending, so an answer-pending
	// filler promise ("give me a second on that") is the wrong semantics
	// (operator complaint, live call local-4dcfb291c2094ec9). Fallback
	// chain: configured line; empty line -> fillerLine() round-robin
	// (legacy byte-compat for callers that never set this field); empty
	// filler list -> no line spoken (the fire still latches + re-arms).
	SilencePromptLine string
	// SilenceHangupAfterSeconds is the SECOND silence window: how long
	// after the nudge the hangup fires when the caller stays quiet
	// (voice.silence_hangup_after_seconds) - nudge@S, hangup@(S+H).
	// Zero or negative reuses SilencePromptSeconds, reproducing the
	// legacy double-window (hangup at 2x S) exactly.
	SilenceHangupAfterSeconds int
	// TurnTimeoutSeconds is the opt-in hard cap on one in-flight turn
	// (final prompt dispatched -> turn-final say flushed). While a turn
	// is in flight the silence timer is suspended, so without a cap a
	// hung employee LLM would keep the call alive forever. Zero (the
	// default) disables the cap: the legacy behavior where only the
	// silence path ends a quiet call.
	TurnTimeoutSeconds int
	// FillerGrace is how long the loop waits after dispatching a final
	// prompt before speaking the round-robin filler line: the filler is
	// spoken only if the turn has not produced its first allowed
	// sentence inside the window (dead-air finding 2026-11-09: "Hold on,
	// let me check." on every single prompt was noise). Zero keeps the
	// legacy immediate filler (spoken inline at dispatch); the daemon
	// wiring passes voice.filler_grace_ms (default 2500). The silence
	// nudge is NEVER grace-gated - that line is the dead-air prompt
	// itself.
	FillerGrace time.Duration
	// SuppressGreeting skips the on-connect greeting say (link
	// half-duplex, turn-taking leaf: exactly ONE side of a link greets
	// - side A. Side B opens silent and speaks first only after A's
	// greeting crosses its mic as a final prompt). False (the default,
	// every non-link call) keeps the greeting byte-identical.
	SuppressGreeting bool
	// ConnectSentences speaks a reply as ONE connected utterance when
	// no supervisor gate is active. The supervisor is per-SENTENCE:
	// when the gate is off there is nothing to check per sentence, yet
	// the legacy path still SplitSentences'd the draft and submitted
	// each sentence as a separate TTS request — qwen3 then read each
	// sentence as a flat standalone unit, producing a robotic
	// list-cadence plus 400-760 ms artificial gaps between every
	// sentence (live call local-4aaa6ea97de14a22 baseline; the gaps are
	// the TTS SYNTHESIS deferral between independent requests, not the
	// pacer). When true, the WHOLE draft is sent as one say frame and
	// the TTS synthesizes natural cross-sentence prosody with real
	// (short) intra-reply pauses. When a supervisor IS enabled the
	// per-sentence path is mandatory (the gate must check each
	// sentence), so this is only consulted when no check is needed.
	ConnectSentences bool
	// FragmentClarify is the line spoken when a dispatched prompt is an
	// unfinished fragment (whisper cut it mid-thought; the endpointing
	// layer already waited fragment_confirm_ms for the caller to resume
	// and they trailed off anyway). A human agent then says "Was that
	// all? I'm not sure I understand you" rather than coldly answering
	// the half-broken sentence. Empty (the default) preserves legacy
	// behavior (the turn runs on whatever text reached the loop);
	// non-empty speaks the line and SKIPS the LLM turn for that
	// fragment.
	FragmentClarify string
	// Lanes is the C5 lane-pool set for this call (plan
	// 20261113-filler-clips leaf 04): the resolved VoiceLanes output for
	// the call's employee. When non-empty, greeting/filler/silence/
	// fragment/farewell lines resolve through voice.PickLine per lane
	// with the loop's own round-robin cursors; a lane that resolves to
	// "" keeps the legacy scalar fallback chain below, so a partial
	// lane set never loses a line the old resolvers would have spoken.
	// Empty/nil (the default — every caller that has not wired the lane
	// plan) leaves all four resolvers byte-identical to today.
	Lanes []LaneLine
	// FarewellLines is the legacy scalar farewell pool for callers that
	// do not wire Lanes (additive field; empty default = the farewell
	// rule NEVER fires, so zero-config is byte-identical). When Lanes
	// carries a farewell lane it wins; otherwise this list round-robins.
	FarewellLines []string
	// Identity is the TTS voice identity the clip key is computed under
	// (Contracts C1 + C3, leaf 04). Nil (the default) => say frames
	// carry NO clip_id => byte-identical today. When set, every
	// filler-class say (greeting, filler, silence nudge, fragment-
	// clarify, fallback, farewell) gains
	// clip_id = clipstore.ClipKey(text, Vendor, Model, Voice, RefHash)
	// so the child can serve pre-rendered PCM. Reply sentences are
	// live content and NEVER get a clip_id (R6).
	Identity *ClipIdentity
}

// ClipIdentity is the TTS identity half of the C1 clip key: the exact
// (vendor, model, voice, reference-audio-hash) inputs the clip planner
// renders under and the talk loop keys its say frames with, so a clip the
// planner never pre-rendered simply misses in the store and the child
// speaks the frame text live (the text always rides the frame).
// RefHash is nil/empty for preset (non-clone) voices — the C1 formula's
// documented zero-bytes branch.
type ClipIdentity struct {
	Vendor  string
	Model   string
	Voice   string
	RefHash []byte
}

// TalkDeps wires the loop. Send is the C1 seam: the talk loop never touches
// the TextServer directly, so the wiring injects a closure and tests record
// frames through a func. OnEnd tears the call down with a reason (both
// sides of C1 end; the wiring adapts it to the release-hook fan-out).
type TalkDeps struct {
	// Supervisor checks draft sentences (C6). Required.
	Supervisor Supervisor
	// Turns runs one employee turn per final prompt (the leaf-12 binding
	// adapter in the daemon wiring; fakes in tests). Required.
	Turns TurnRunner
	// SessionFor resolves the SessionHandle for a call Instance. In the
	// daemon wiring this reads the binding's per-call state; tests inject
	// a constant. Required.
	SessionFor func(call Instance) SessionHandle
	// Binding publishes per-call progress (say/interrupt kinds); nil
	// drops those events.
	Binding *Binding
	// Send writes one frame toward the child (C1 daemon->child side).
	// Required.
	Send func(Frame) error
	// OnEnd terminates the call with a reason. Required.
	OnEnd func(ccid, reason string)
	// Now is the clock seam for tests; zero value means time.Now.
	Now func() time.Time
	// Logger is optional; nil falls back to slog.Default.
	Logger *slog.Logger
	// HoldPrompt is the link half-duplex seam (turn-taking leaf): when
	// non-nil, every final prompt routes through it BEFORE dispatch.
	// Returning true = dispatch now (the legacy path); false = the
	// coordinator holds the turn because the other side of the link is
	// speaking, and the loop parks the dispatch closure (at most ONE
	// pending held prompt - a second held final REPLACES the pending
	// one, latest question wins, telemetry kind=prompt_held). The
	// coordinator releases via TalkLoop.FlushHeld. Nil (every non-link
	// call) keeps dispatch byte-identical.
	HoldPrompt func(call Instance, dispatch func()) bool
	// TurnState is the link half-duplex observation seam: when non-nil
	// the loop publishes (ccid, seq, kind) at turn start / turn final /
	// idle (barge-in cancel or teardown), so a coordinator can tell when
	// this side is speaking. Nil (every non-link call) = zero emissions.
	// Binding progress was considered and rejected: it carries no seq,
	// has no turn_start/idle kind, and rides the bus (the coordinator
	// needs a synchronous in-process edge).
	TurnState func(ccid string, seq uint64, kind string)
	// AudioActive is the OPTIONAL silence-deferral seam (self-talk fix,
	// call local-53b347f028f0a43f): when non-nil the silence fire checks
	// whether the AGENT's own audio was active within the expiring
	// window (window = the armed S/H duration) BEFORE speaking the nudge
	// or hanging up. A true verdict defers the fire: the window re-arms
	// (same duration) instead, bounded by silenceAudioDeferralCap
	// consecutive deferrals - after the cap the fire proceeds so a
	// permanently-audible stream cannot suppress the nudge forever
	// (mirrors the link margin's bounded re-arm posture). This kills the
	// two live defects: (S1) rearmSilence at TEXT drain counted down
	// while the reply audio still paced, so the nudge fired over the
	// agent's own voice; (S2) the loop-start window fired while the
	// greeting's audio tail was still inside the window. Nil (phone
	// legs, legacy/test callers) = zero behavior change, byte-identical.
	// The daemon wires it for LOCAL legs to
	// LocalSpliceFor(ccid).TailActive(window) (nil-safe splice lookup).
	// It reads audio-truth (pump/pace writes), never the text lane.
	AudioActive func(ccid string, window time.Duration) bool
	// OnEvent is the operator-telemetry append hook (voice-events tree):
	// when non-nil the loop records dispatch / final / barge-in / held /
	// released / dropped / call-ended events at its existing log sites
	// through the shared per-call ring (TalkDeps.OnEvent returns the
	// assigned seq, which the loop never consumes). Nil (the default on
	// every wiring that has not armed the ring) = zero emissions and a
	// byte-identical loop: this seam is observability-only and NEVER
	// gates dispatch, playback, or teardown. The hook is synchronous and
	// must be cheap (a ring append is pure bookkeeping; the repo mutex-
	// scope rule forbids I/O here anyway - the daemon adapter only appends
	// to the in-process TelemetryRing).
	OnEvent func(ccid string, in EventInput) uint64
}

// pipeline is one prompt's draft-to-speech state: the generation and turn
// sequence it was spawned under, its cancel func (interrupt/teardown), and
// the verdict channel the checker feeds. said/fallback are owned by the
// playback goroutine (Run), never the checker. The filler*/utteranceOut/
// dispatchedAt/replyWords fields are guarded by TalkLoop.interruptMu (the
// delayed-filler timer closure runs on the runtime timer goroutine, so the
// latch lives under that lock; the mutex-scope rule still binds — no I/O,
// send, or Check happens while it is held).
type pipeline struct {
	gen      uint64
	seq      uint64
	cancel   context.CancelFunc
	checks   chan verdict
	said     int  // allowed sentences spoken so far in this draft
	fallback bool // fallback line already spoken in this draft
	// turnEnded marks that a last:true say already went out for this
	// draft (final allowed sentence or fallback line). Owned by the
	// playback goroutine (Run); the drain flush checks it so the
	// turn-final marker is sent exactly once (M2).
	turnEnded bool

	// Delayed filler (leaf 02, dead-air fix). Arm sets fillerTimer to a
	// time.AfterFunc closure; the closure re-checks under interruptMu
	// that this pipeline is STILL the live draft (t.cur == p) and nothing
	// has been spoken yet before calling say — the latch that keeps a
	// cancelled/hung-up draft from emitting zombie filler.
	fillerTimer   *time.Timer
	fillerPending bool   // armed and not yet fired/cancelled
	fillerFired   bool   // closure took the latch (filler is being spoken)
	fillerText    string // round-robin line picked at dispatch (Run picks it;
	// the timer goroutine only reads it under interruptMu — no second
	// fillerIdx touch off the Run goroutine)
	utteranceOut  bool // first allowed/fallback sentence went out
	fillerSkipped bool // grace timer was stopped by a real utterance (telemetry)
	dispatchedAt  time.Time
	replyWords    int // words across allowed spoken sentences (turn_final)
}

// stopFillerLocked cancels a pending delayed-filler timer. Callers hold
// t.interruptMu. Returns true when a pending timer was actually stopped
// (i.e. the grace window had not elapsed — "answered inside grace").
func (p *pipeline) stopFillerLocked() bool {
	if !p.fillerPending {
		return false
	}
	p.fillerPending = false
	if p.fillerTimer != nil {
		p.fillerTimer.Stop()
	}
	return true
}

// TalkLoop runs one live call's conversation. Run blocks until the call
// ends (end frame, max duration, silence hangup) or ctx is cancelled.
type TalkLoop struct {
	deps TalkDeps
	cfg  TalkLoopConfig
	log  *slog.Logger
	now  func() time.Time

	// frames is the inbound C1 frame channel; close ends the loop.
	frames chan Frame
	// fillerIdx round-robins over the filler list (legacy scalar pool —
	// the same cursor drives the C5 filler lane when Lanes is wired).
	fillerIdx int
	// Per-lane round-robin cursors (plan 20261113-filler-clips leaf 04,
	// C5: "Pool selection = round-robin per lane with its own cursor").
	// Run-owned like fillerIdx (the delayed-filler timer only reads the
	// picked text under interruptMu, never a cursor).
	silenceIdx  int
	fragmentIdx int
	farewellIdx int
	// farewellSpoken latches the R5 farewell: at most one goodbye per
	// call (never double-speaks with a fallback or a repeat bye).
	farewellSpoken bool

	// interruptMu guards ONLY the generation counter and pipeline pointer
	// swap (repo mutex-scope rule: no I/O, send, or Check under it). The
	// delayed-filler latch (pipeline.filler*/utteranceOut) also rides this
	// lock; no I/O happens while it is held.
	interruptMu sync.Mutex
	gen         uint64
	seq         uint64
	cur         *pipeline

	// runCcid is the call id of the live Run (bughunt 2026-11-11 C-14):
	// the link coordinator's Bind reads it to ring floor-lifecycle
	// telemetry under the side's REAL ccid (the ring is keyed per call).
	// Atomic because Bind can run just before or after Run starts.
	runCcid atomic.Value // string

	// silencePrompted latches the one silence nudge: the first silence
	// fire speaks SilencePromptLine, the second hangs up.
	silencePrompted bool

	// drainSignal wakes Run's select after cancelPipeline drops a LIVE
	// draft (barge-in/supersede). The silence clock is suspended from
	// dispatch until the draft DRAINS, and the drain edge is normally the
	// checker closing p.checks — but a cancelled draft's close is invisible
	// to Run (t.cur is already nil, so current() is the disarmed nil
	// channel forever): bughunt 2026-11-12 defect 7, the silence
	// nudge/hangup never fired after a barge-in with no follow-up speech.
	// Buffered(1): the signal is level-free (any pending wake-up triggers
	// the re-arm check on the next select) and a non-blocking send from the
	// splice's CancelDraftFn goroutine can never wedge the cancel path.
	drainSignal chan struct{}

	// Link half-duplex state (turn-taking leaf). held is the ONE parked
	// prompt (a second held final replaces the pending one, latest
	// question wins); heldMu guards ONLY the swap - the closure itself is
	// always invoked on the Run goroutine, drained from the flush
	// channel (repo mutex-scope rule). flush is buffered(1): FlushHeld
	// signals from the coordinator's goroutine and the signal survives
	// until Run's select drains it, which closes the park-vs-release race
	// (a flush that lands before the park store is still in the buffer
	// when the park completes).
	flush  chan struct{}
	heldMu sync.Mutex
	held   *heldPrompt

	endOnce sync.Once
}

// NewTalkLoop constructs the loop. Deps.Supervisor, deps.SessionFor,
// deps.Send, and deps.OnEnd are required (a nil turns Run into a call that
// ends immediately with a named reason, never a panic); Binding, Now, and
// Logger are nil-safe.
func NewTalkLoop(deps TalkDeps, cfg TalkLoopConfig) *TalkLoop {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &TalkLoop{
		deps:        deps,
		cfg:         cfg,
		log:         logger,
		now:         now,
		frames:      make(chan Frame, 16),
		flush:       make(chan struct{}, 1),
		drainSignal: make(chan struct{}, 1),
	}
}

// Frames returns the inbound frame sink. The wiring routes this call's C1
// frames (prompt/interrupt/end frames) from the TextServer handler here.
func (t *TalkLoop) Frames() chan<- Frame { return t.frames }

// FlushHeld signals the loop that a coordinator wants its parked prompt
// released. Safe from any goroutine: the signal is a non-blocking send on
// a buffered channel; the Run goroutine drains it and performs the
// dispatch (the parked closure is NEVER invoked off the Run goroutine -
// the silence/turn timer discipline and fillerIdx ownership stay intact).
func (t *TalkLoop) FlushHeld() {
	select {
	case t.flush <- struct{}{}:
	default:
	}
}

// publishTurnState emits one turn-state edge when the seam is wired
// (link calls only). Never called with deps.TurnState == nil by callers
// behind this helper.
func (t *TalkLoop) publishTurnState(ccid string, seq uint64, kind string) {
	if t.deps.TurnState != nil {
		t.deps.TurnState(ccid, seq, kind)
	}
}

// emitEvent appends one operator-telemetry event when the OnEvent seam is
// wired (nil = zero work, byte-identical loop). The hook is the daemon's
// per-call ring append - pure bookkeeping, never I/O, and its return
// value (the assigned seq) is deliberately ignored: the loop's own seq
// space (pipeline.seq) and the ring's cursor space are independent.
func (t *TalkLoop) emitEvent(ccid string, in EventInput) {
	if t.deps.OnEvent != nil {
		t.deps.OnEvent(ccid, in)
	}
}

// ccidForTelemetry returns the call id the link coordinator rings this
// side's floor-lifecycle events under (bughunt 2026-11-11 C-14): the
// Run call's ccid (the wiring arms Run with the live Instance; before
// Run starts the zero value surfaces, and the coordinator falls back to
// the link id). Read-only accessor for link.go's Bind.
func (t *TalkLoop) ccidForTelemetry() string {
	if v, ok := t.runCcid.Load().(string); ok {
		return v
	}
	return ""
}

// heldPrompt is ONE parked final-prompt dispatch: the Run-owned closure,
// its text (telemetry preview), and the call Instance the re-gate needs.
type heldPrompt struct {
	dispatch func()
	text     string
	call     Instance
}

// nextSeq reads the seq the NEXT spawnPipeline will claim (telemetry
// correlation only - the authoritative seq still increments at dispatch).
func (t *TalkLoop) nextSeq() uint64 {
	t.interruptMu.Lock()
	defer t.interruptMu.Unlock()
	return t.seq + 1
}

// parkPrompt stores the pending held dispatch (at most ONE: a second
// held final REPLACES the pending one - latest question wins) and logs
// the prompt_held telemetry line. Called on the Run goroutine after
// deps.HoldPrompt returned false.
func (t *TalkLoop) parkPrompt(call Instance, dispatch func(), text string) {
	t.heldMu.Lock()
	replaced := t.held != nil
	t.held = &heldPrompt{dispatch: dispatch, text: text, call: call}
	t.heldMu.Unlock()

	ccid := call.CallControlID
	t.log.Info("voice talk loop: prompt held",
		"ccid", ccid, "seq", t.nextSeq(), "kind", "prompt_held",
		"text_preview", previewPrompt(text), "replaced_pending", replaced)
	t.emitEvent(ccid, EventInput{Kind: EventKindHeld, Text: previewPrompt(text), Words: wordCount(text)})
}

// releaseHeld dispatches the parked prompt (if any) with the
// prompt_released telemetry line. Runs on the Run goroutine (flush
// channel consumer). A stale signal with nothing parked is a no-op.
//
// Re-gate: when HoldPrompt is wired, the coordinator is consulted once
// more before dispatch - a flush signal that raced the peer starting a
// NEW turn (margin fired, then the peer spoke again) must re-park, not
// speak over the fresh turn. Re-parked prompts ride the next terminal
// edge (every turn ends in turn_final or idle, both signal a release),
// so the re-gate cannot strand a prompt.
func (t *TalkLoop) releaseHeld(ccid string) {
	t.heldMu.Lock()
	h := t.held
	t.held = nil
	t.heldMu.Unlock()
	if h == nil {
		return
	}
	if t.deps.HoldPrompt != nil && !t.deps.HoldPrompt(h.call, h.dispatch) {
		t.heldMu.Lock()
		t.held = h
		t.heldMu.Unlock()
		t.log.Info("voice talk loop: release re-held (peer busy again)",
			"ccid", ccid, "kind", "prompt_reheld",
			"text_preview", previewPrompt(h.text))
		t.emitEvent(ccid, EventInput{Kind: EventKindHeld, Text: previewPrompt(h.text), Words: wordCount(h.text)})
		return
	}
	t.log.Info("voice talk loop: prompt released",
		"ccid", ccid, "seq", t.nextSeq(), "kind", "prompt_released",
		"text_preview", previewPrompt(h.text))
	t.emitEvent(ccid, EventInput{Kind: EventKindReleased, Text: previewPrompt(h.text), Words: wordCount(h.text)})
	h.dispatch()
}

// dropHeld logs and discards a parked prompt (a newer prompt took the
// floor, or loop exit). heldMu is taken so a racing FlushHeld's later
// signal finds nothing.
func (t *TalkLoop) dropHeld(ccid, reason string) {
	t.heldMu.Lock()
	h := t.held
	t.held = nil
	t.heldMu.Unlock()
	if h == nil {
		return
	}
	t.log.Info("voice talk loop: held prompt dropped",
		"ccid", ccid, "kind", "prompt_dropped", "reason", reason,
		"text_preview", previewPrompt(h.text))
	t.emitEvent(ccid, EventInput{Kind: EventKindDropped, Text: previewPrompt(h.text), Reason: reason})
}

// previewPrompt truncates prompt text for a telemetry line.
func previewPrompt(text string) string {
	preview := text
	if runes := []rune(preview); len(runes) > 40 {
		preview = string(runes[:40]) + "…"
	}
	return preview
}

// greetingLine returns the configured greeting, falling back to the first
// filler line (the greeting is filler-class; an operator who configured no
// filler gets no greeting rather than a hardcoded line). With the C5 lane
// pool wired the greeting lane resolves first; an empty lane keeps this
// exact legacy chain (GreetingLine -> FillerLines[0] -> "").
func (t *TalkLoop) greetingLine() string {
	if line := PickLine(t.cfg.Lanes, LaneGreeting, nil); line != "" {
		return line
	}
	if t.cfg.GreetingLine != "" {
		return t.cfg.GreetingLine
	}
	if len(t.cfg.FillerLines) > 0 {
		return t.cfg.FillerLines[0]
	}
	return ""
}

// fillerLine picks the next fact-free line round-robin over the config
// list; an empty list disables the filler. With the C5 lane pool wired,
// the filler lane round-robins through the SAME cursor (fillerIdx), so a
// single-line pool equals the legacy pick order exactly; an empty lane
// falls back to the legacy scalar list byte-identically (callers that
// never set Lanes keep today's behavior).
func (t *TalkLoop) fillerLine() string {
	if line := PickLine(t.cfg.Lanes, LaneFiller, &t.fillerIdx); line != "" {
		return line
	}
	if len(t.cfg.FillerLines) == 0 {
		return ""
	}
	line := t.cfg.FillerLines[t.fillerIdx%len(t.cfg.FillerLines)]
	t.fillerIdx++
	return line
}

// silencePromptLine picks the line for the FIRST silence fire (the
// dead-air nudge). Fallback chain (contract: no filler-class promise on
// the silence path when a dedicated line is configured): C5 silence lane
// (round-robin, own cursor); configured SilencePromptLine; empty line ->
// fillerLine() round-robin (legacy callers that never set the new field
// keep byte-compatible behavior, INCLUDING consuming a filler index);
// empty filler -> "" (no line — the fire still latches and re-arms the
// hangup window).
func (t *TalkLoop) silencePromptLine() string {
	if line := PickLine(t.cfg.Lanes, LaneSilence, &t.silenceIdx); line != "" {
		return line
	}
	if t.cfg.SilencePromptLine != "" {
		return t.cfg.SilencePromptLine
	}
	return t.fillerLine()
}

// fragmentClarifyLine picks the clarification line for an unfinished
// fragment. C5 fragment lane (round-robin, own cursor) first; the legacy
// scalar FragmentClarify otherwise (empty = no clarify, turn runs).
func (t *TalkLoop) fragmentClarifyLine() string {
	if line := PickLine(t.cfg.Lanes, LaneFragment, &t.fragmentIdx); line != "" {
		return line
	}
	return t.cfg.FragmentClarify
}

// farewellLine picks the R5 goodbye line: the C5 farewell lane first
// (round-robin, own cursor), the legacy FarewellLines scalar pool
// otherwise. Empty from both = the farewell rule never speaks (a caller's
// bye rides the normal turn path, exactly today's behavior).
func (t *TalkLoop) farewellLine() string {
	if line := PickLine(t.cfg.Lanes, LaneFarewell, &t.farewellIdx); line != "" {
		return line
	}
	if len(t.cfg.FarewellLines) == 0 {
		return ""
	}
	line := t.cfg.FarewellLines[t.farewellIdx%len(t.cfg.FarewellLines)]
	t.farewellIdx++
	return line
}

// silenceAudioDeferralCap bounds CONSECUTIVE audio-aware deferrals of a
// silence window (self-talk fix, call local-53b347f028f0a43f): after this
// many deferrals the fire proceeds regardless, so a permanently-audible
// stream (or a stuck seam) cannot suppress the nudge - and through it the
// hangup - forever. Mirrors the link margin's bounded re-arm posture
// (margin_rearmed re-arms ONCE, the second fire releases).
const silenceAudioDeferralCap = 2

// silenceAudioDefers reports whether a silence fire must DEFER because
// the agent's own audio was active inside the expiring window. False
// whenever: the seam is nil (phone/legacy callers stay byte-identical),
// the consecutive-deferral cap is reached (the nudge must still
// eventually fire on a permanently-audible stream), or the window length
// is unrecordable (<= 0 - nothing to consult the seam about).
func (t *TalkLoop) silenceAudioDefers(ccid string, armedSeconds int) bool {
	if t.deps.AudioActive == nil || armedSeconds <= 0 {
		return false
	}
	// The seam's window is the LIVE window length in real time: at
	// production scale armedSeconds are seconds; under the ms test unit
	// they are ms. TailActive's own clock is wall time, so a test seam
	// receives the same unit the timer was armed with.
	return t.deps.AudioActive(ccid, time.Duration(armedSeconds)*silenceWindowUnit)
}

// itoaSilence renders a deferral count for the telemetry Reason field
// (additive wire info only; the count never gates behavior).
func itoaSilence(n int) string { return fmt.Sprintf("%d", n) }

// fallbackLine returns the configured C6 fallback line or the default.
func (t *TalkLoop) fallbackLine() string {
	if t.cfg.FallbackLine != "" {
		return t.cfg.FallbackLine
	}
	return fallbackSentence
}

// say sends one say frame. last marks the final sentence of a turn.
// It delegates to sayClip with an empty clip key, so the single-audible
// choke (RecordSpoken on every successful send — echo_roster.go) lives in
// exactly one place.
func (t *TalkLoop) say(ccid, text string, last bool) error {
	return t.sayClip(ccid, text, "", last)
}

// sayClip sends one say frame that may carry a pre-rendered clip key
// (Contracts C1 + C3, leaf 04). clipID is the 64-hex clipstore key of
// text when the call's TTS identity resolved; an EMPTY clipID omits the
// wire field entirely (omitempty), so the frame is byte-identical to a
// plain say — the nil-identity / live-synth path is today's shape. The
// text ALWAYS rides the frame: a child whose store misses the key
// synthesizes frame.Text live (internal/voicechild/clips.go), so a
// clip_id on a line the planner never rendered is a miss, never a mute.
// This is the single-audible choke for ALL agent speech (CROSS-CALL ECHO
// CONTRACT, 2026-11-11): say delegates here; every new say variant MUST
// too. RecordSpoken runs only on a successful send (a frame that never
// left the daemon produced no room audio, so it must not gate anything).
func (t *TalkLoop) sayClip(ccid, text, clipID string, last bool) error {
	lastFlag := last
	err := t.deps.Send(Frame{
		CallID: ccid,
		Event:  EventSay,
		Text:   text,
		Last:   &lastFlag,
		ClipID: clipID,
	})
	// CROSS-CALL ECHO CONTRACT (2026-11-11): say is the single choke for
	// ALL agent speech (greeting, filler, replies, fallback, fragment
	// clarify), so this is where the process-wide roster learns what each
	// live call is speaking. Recorded only on a successful send (a frame
	// that never left the daemon produced no room audio, so it must not
	// gate anything). The empty turn-final flush marker is a no-op inside
	// the roster. See echo_roster.go for the full contract.
	if err == nil {
		RecordSpoken(ccid, text)
	}
	return err
}

// speakFarewell speaks one farewell-lane goodbye for the R5 rule: it
// cancels any in-flight draft (a bye supersedes an answer the caller is
// walking away from — the same skip shape as the fragment clarify, no LLM
// turn), then sends the line as a FINAL (last:true) say with clip_id so
// the child's turn-final flush + played-drain machinery completes the
// audio before teardown. Exactly-once per call (farewellSpoken latch).
// Runs on the Run goroutine (dispatchPrompt), so the cursor and latch are
// lock-free like fillerIdx. Returns false when the latch already fired
// (a repeat bye never double-speaks) or no line resolves.
func (t *TalkLoop) speakFarewell(ccid string) bool {
	if t.farewellSpoken {
		t.log.Info("voice talk loop: farewell already spoken; bye ignored",
			"ccid", ccid, "lane", string(LaneFarewell))
		return false
	}
	line := t.farewellLine()
	if line == "" {
		return false
	}
	t.farewellSpoken = true
	// Cancel any live draft so its verdicts cannot interleave a reply
	// under the goodbye (and the grace filler cannot zombie behind it).
	t.cancelPipeline(ccid, "farewell")
	if err := t.sayClip(ccid, line, t.clipID(line), true); err != nil {
		t.log.Warn("voice talk loop: farewell send failed", "ccid", ccid, "error", err)
		return true // the latch fired: a failed send must not re-arm the rule
	}
	t.log.Info("voice talk loop: farewell",
		"ccid", ccid, "kind", "farewell", "line", line,
		"lane", string(LaneFarewell), "clip", t.cfg.Identity != nil)
	t.sayProgress(ccid)
	return true
}

// clipID returns the C1 clip key of a filler-class line under the call's
// resolved TTS identity — the EXACT inputs the clip planner renders and
// the store keys by, so a loop-said line hits the store iff the planner
// pre-rendered it. "" (no clip_id on the frame) whenever the identity is
// nil (a call whose daemon predates the clip plan — byte-identical
// today) or the frame carries no speakable text (the turn-final flush
// marker).
func (t *TalkLoop) clipID(text string) string {
	if t.cfg.Identity == nil || text == "" {
		return ""
	}
	id := t.cfg.Identity
	return clipstore.ClipKey(text, id.Vendor, id.Model, id.Voice, id.RefHash)
}

// farewellPhrases is the builtin R5 bye-phrase set (leaf
// 20261113-filler-clips/04): matched against the WHOLE normalized caller
// utterance, case- and punctuation-insensitive. A phrase embedded in a
// longer sentence never matches ("bye for now to the router issue" is a
// real question). Keep the list small and documented — a cheap rule,
// deliberately no LLM.
var farewellPhrases = []string{"bye", "goodbye", "see ya", "see you", "talk later", "later", "that's all"}

// isFarewellUtterance reports whether the caller's final prompt is a
// whole-utterance goodbye per the R5 builtin rule.
func isFarewellUtterance(text string) bool {
	norm := normalizeUtterance(text)
	if norm == "" {
		return false
	}
	for _, p := range farewellPhrases {
		if norm == normalizeUtterance(p) {
			return true
		}
	}
	return false
}

// normalizeUtterance lowercases, strips punctuation, and collapses
// whitespace for whole-utterance bye matching ("Goodbye!" -> "goodbye",
// "See  ya!" -> "see ya", "that's all" -> "thats all").
func normalizeUtterance(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true // suppresses a leading space
	for _, r := range s {
		switch {
		case r == ' ' || r == '	' || r == '\n' || r == '\r':
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case unicodeIsPunct(r):
			// stripped; a comma never joins words into a phrase
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// unicodeIsPunct reports whether r is a unicode punct/symbol (the
// farewell-rule's strip set) without pulling unicode into the import
// block used by the hot path.
func unicodeIsPunct(r rune) bool {
	switch r {
	case '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
		':', ';', '<', '=', '>', '?', '@', '[', ']', '^', '_', '`', '{', '|', '}', '~',
		'…':
		return true
	}
	return false
}

// farewellPoolConfigured reports whether ANY farewell line is resolvable
// (C5 lane or the legacy scalar pool) without consuming a round-robin
// cursor — the rule must not advance the pool on non-bye prompts.
func (t *TalkLoop) farewellPoolConfigured() bool {
	if len(t.cfg.FarewellLines) > 0 {
		return true
	}
	for _, ll := range t.cfg.Lanes {
		if ll.Lane == LaneFarewell {
			return true
		}
	}
	return false
}

// sayProgress publishes one allowed-say progress event through the binding
// (nil-safe: tests without a binding skip it).
func (t *TalkLoop) sayProgress(ccid string) {
	if t.deps.Binding != nil {
		t.deps.Binding.PublishSay(ccid)
	}
}

// end tears the call down once with the given reason.
func (t *TalkLoop) end(ccid, reason string) {
	t.endOnce.Do(func() {
		t.log.Info("voice talk loop: call ended", "ccid", ccid, "reason", reason)
		t.emitEvent(ccid, EventInput{Kind: EventKindCallEnded, Reason: reason})
		// CROSS-CALL ECHO CONTRACT (2026-11-11): drop this call's roster
		// entries at teardown (hygiene only — TTL ages them out within
		// 30 s regardless; a dead call's speech should stop gating early).
		ClearSpoken(ccid)
		t.deps.OnEnd(ccid, reason)
	})
}

// Run consumes this call's frames and plays the pipelined verdicts until an
// end frame, a timer, or ctx cancellation ends the call. It lives for the
// call: the wiring spawns it when the media bridge registers and cancels it
// on teardown.
func (t *TalkLoop) Run(ctx context.Context, call Instance) error {
	ccid := call.CallControlID
	// Telemetry key (bughunt 2026-11-11 C-14): publish the live ccid for
	// the link coordinator's floor-lifecycle ring emissions (Bind reads
	// it). Stored before the greeting so an early idle edge is already
	// keyed correctly.
	t.runCcid.Store(ccid)

	// Greeting first (spec call flow: the agent speaks first on a
	// connected call; fact-free, interruptible, never supervisor-gated).
	// SuppressGreeting (link half-duplex B side): exactly ONE side of a
	// link greets - A's greeting crosses B's mic as B's first final
	// prompt (the conversation seed), so B opening silent prevents the
	// live-run defect of two employees saying hello at once.
	if line := t.greetingLine(); line != "" && !t.cfg.SuppressGreeting {
		if err := t.sayClip(ccid, line, t.clipID(line), false); err != nil {
			t.log.Warn("voice talk loop: greeting send failed", "ccid", ccid, "error", err)
		}
	}

	// Link half-duplex: a prompt parked at loop exit is logged and
	// dropped, never silently leaked into a dead call (the closure would
	// dispatch into a loop nobody is selecting on).
	defer t.dropHeld(ccid, "loop ended")

	// Max-duration timer (voice.max_call_minutes).
	var maxC <-chan time.Time
	if t.cfg.MaxCallMinutes > 0 {
		timer := time.NewTimer(time.Duration(t.cfg.MaxCallMinutes) * time.Minute)
		defer timer.Stop()
		maxC = timer.C
	}

	// Silence timer (voice.silence_prompt_seconds): re-armed after every
	// turn (final prompt dispatched -> reply fully spoken).
	//
	// TURN-AWARE silence (bughunt 2026-11-08): while a turn is in flight
	// (t.cur != nil, i.e. prompt dispatched, turn-final say not yet
	// flushed), the silence window is SUSPENDED - a slow cloud LLM (30-90s)
	// must not be reaped by the 45s silence clock before its reply is
	// spoken. Concretely: the final-prompt handler stops the silence timer
	// instead of re-arming it; the draft-drain branch re-arms a fresh
	// window once the reply is out. Between prompts (caller quiet), the
	// two-fire silence structure is unchanged: first fire nudges, second
	// hangs up.
	//
	// Timer discipline (M5): never Reset a timer whose fire may already
	// be buffered (or racing Stop) — the stale value would be consumed
	// by the next select and double-fire the silence path. Instead every
	// re-arm abandons the old timer and arms a FRESH one: a stale fire
	// lands in an orphaned channel nobody reads and is GC'd with the old
	// timer; the new window starts clean.
	//
	// Two windows: the FIRST window (and every re-arm after a spoken
	// turn) is S = SilencePromptSeconds; after the nudge fires the re-arm
	// uses H = SilenceHangupAfterSeconds (S when H <= 0 - legacy
	// double-window equivalence). silenceWindowUnit is the ms-tunable
	// seam for tests (second scale by default; the package test suite
	// mutates it only from silence-window tests, which never run in
	// parallel - go test runs package tests sequentially).
	var silenceTimer *time.Timer
	var silenceC <-chan time.Time
	// silenceArmedAt stamps when the live window was armed, so the fire
	// telemetry can report the REAL elapsed window (after_s) - with H
	// configured the two windows differ and the log line alone would
	// under-report the gap since the nudge.
	var silenceArmedAt time.Time
	// silenceArmedSeconds remembers the window length the live timer was
	// armed with, so an audio-aware DEFERRAL re-arms the SAME window
	// (self-talk fix, call local-53b347f028f0a43f) instead of guessing.
	var silenceArmedSeconds int
	// silenceDeferrals counts CONSECUTIVE audio-aware deferrals of the
	// live window (capped at silenceAudioDeferralCap): a text-drain
	// re-arm or a real fire starts a fresh silent stretch and resets it.
	var silenceDeferrals int
	rearmSilenceWindow := func(seconds int) {
		if seconds <= 0 {
			return
		}
		if silenceTimer != nil {
			silenceTimer.Stop()
		}
		silenceTimer = time.NewTimer(time.Duration(seconds) * silenceWindowUnit)
		silenceC = silenceTimer.C
		silenceArmedAt = t.now()
		silenceArmedSeconds = seconds
	}
	rearmSilence := func() {
		if t.cfg.SilencePromptSeconds <= 0 {
			return
		}
		// A fresh turn just drained: the audio-deferral streak restarts
		// with the new window (the fire-time check below still defers
		// while this turn's reply audio paces - defect S1).
		silenceDeferrals = 0
		rearmSilenceWindow(t.cfg.SilencePromptSeconds)
	}
	stopSilence := func() {
		if silenceTimer != nil {
			silenceTimer.Stop()
		}
		silenceC = nil
	}
	rearmSilence()
	defer stopSilence()

	// Turn hard cap (voice.turn_timeout_seconds, opt-in, default 0 =
	// disabled): bounds a hung employee LLM. The silence timer cannot
	// reap an in-flight turn, so without this cap a wedged turn keeps
	// the call alive forever. The timer arms FRESH at each final-prompt
	// dispatch and stops when the draft drains; a fire with no live
	// pipeline is a dropped no-op (the turn finished inside the cap).
	var turnTimer *time.Timer
	var turnTimerC <-chan time.Time
	armTurn := func() {
		if t.cfg.TurnTimeoutSeconds <= 0 {
			return
		}
		if turnTimer != nil {
			turnTimer.Stop()
		}
		turnTimer = time.NewTimer(time.Duration(t.cfg.TurnTimeoutSeconds) * time.Second)
		turnTimerC = turnTimer.C
	}
	stopTurn := func() {
		if turnTimer != nil {
			turnTimer.Stop()
		}
		turnTimerC = nil
	}
	defer stopTurn()

	// current drains the live pipeline's verdict channel, or nil between
	// prompts. A nil channel is the DISARMED verdict arm: select never
	// picks it, so the drain branch stays parked while a turn is in
	// flight (the silence clock is suspended). The cancel path re-arms
	// the clock through the dedicated drain channel instead (see
	// drainSignal) rather than by making this channel readable.
	current := func() chan verdict {
		t.interruptMu.Lock()
		defer t.interruptMu.Unlock()
		if t.cur == nil {
			return nil
		}
		return t.cur.checks
	}

	// flushTurnEnd sends the turn-final say flush (last:true) exactly
	// once per draft (M2): the final verdict usually carries it, but a
	// draft whose final sentence was DENIED after earlier sentences were
	// spoken (or which ended with an empty reply) reaches the channel
	// close with no last:true sent, leaving the child's turn-taking
	// without its end marker.
	flushTurnEnd := func(ccid string) {
		t.interruptMu.Lock()
		p := t.cur
		t.interruptMu.Unlock()
		if p == nil || p.turnEnded {
			return
		}
		p.turnEnded = true
		if err := t.say(ccid, "", true); err != nil {
			t.log.Warn("voice talk loop: final flush send failed", "ccid", ccid, "error", err)
		}
	}

	// dispatchPrompt is the EventPrompt final-prompt body, hoisted
	// into a Run-owned closure so the link half-duplex seam can park
	// it verbatim: held dispatch runs LATER on this same goroutine
	// (timer discipline + fillerIdx ownership untouched). stopSilence/
	// armTurn close over Run's timer locals, so this MUST live here.
	dispatchPrompt := func(text string) {
		// Farewell rule (R5, plan 20261113-filler-clips leaf 04): a
		// whole-utterance bye never starts an LLM turn — the loop
		// speaks one farewell-lane line as a FINAL (last:true) say
		// carrying the clip_id, then ends the call through the loop's
		// own teardown (t.end -> OnEnd -> managerHangup -> releaseCall
		// -> sendEndFrame), riding the SAME once-guarded fan-out a
		// silence hangup uses — never a second release path. The
		// farewell say is written to the shared text connection BEFORE
		// the end frame (program order on this goroutine), and the
		// last:true shape hands the child its turn-final flush +
		// played-drain guarantee (C1), so the goodbye audio completes
		// before teardown kills the leg. Zero-config (no farewell pool
		// resolvable) = the rule NEVER fires: "bye" rides the normal
		// turn path byte-identically. dispatchPrompt (and the
		// link-park release that re-runs it) lives on the Run
		// goroutine, so the farewell cursor/latch need no lock
		// (same discipline as fillerIdx).
		if t.farewellPoolConfigured() && isFarewellUtterance(text) {
			if t.speakFarewell(ccid) {
				t.end(ccid, "caller farewell")
			}
			return
		}
		// The clock stops while the turn runs: the silence window
		// is turn-aware, and the draft-drain branch re-arms it
		// fresh once the reply is fully spoken (a slow 30-90s LLM
		// must not lose the call mid-generation). The turn cap
		// (opt-in) arms fresh per prompt for the same window.
		stopSilence()
		armTurn()
		t.silencePrompted = false
		// A newer prompt supersedes any in-flight draft (the
		// barge-in drop rule applies to a fresh question too).
		t.cancelPipeline(ccid, "supersede")
		// Pick the filler line on THIS goroutine (fillerIdx is
		// Run-owned); whether it speaks now (legacy grace==0) or
		// after the grace window is spawnPipeline's call (call
		// flow step 6: fact-free, so it needs no supervisor
		// verdict). The fragment-clarify line is picked here too,
		// but ONLY when the prompt's trailing-ellipsis shape trips
		// (isTrailingEllipsis is pure over the immutable text), so
		// the fragment cursor never advances on a non-fragment
		// prompt — today's scalar path only reads
		// cfg.FragmentClarify on the ellipsis shape, and the lane
		// pick must not consume a round on every dispatch.
		fragmentLine := ""
		if isTrailingEllipsis(text) {
			fragmentLine = t.fragmentClarifyLine()
		}
		t.spawnPipeline(ctx, call, text, t.fillerLine(), fragmentLine)
	}

	for {
		select {
		case <-ctx.Done():
			t.cancelPipeline(ccid, "teardown")
			t.end(ccid, "hangup")
			return ctx.Err()
		case <-maxC:
			t.cancelPipeline(ccid, "teardown")
			t.end(ccid, fmt.Sprintf("max_call_minutes reached (%d)", t.cfg.MaxCallMinutes))
			return nil
		case <-silenceC:
			silenceC = nil
			if t.cfg.SilencePromptSeconds <= 0 {
				continue
			}
			// A turn was in flight when this stale fire landed (the
			// final-prompt handler stops the timer; a fire already
			// buffered before the stop is possible). The silence path
			// must never reap an in-flight turn: drop the fire, keep
			// the clock stopped until the turn drains and re-arms it.
			if t.curPipeline() != nil {
				continue
			}
			// Audio-aware deferral (self-talk fix, defect S1/S2, call
			// local-53b347f028f0a43f): the TEXT lane drains seconds
			// before the reply's AUDIO finishes pacing (measured: the
			// drain re-arm fired a 10 s window while the pacer was
			// still stalling chunks 1.3-3.8 s apart, and the nudge
			// landed on top of the agent's own voice - the operator
			// heard "two agents talking"). When the seam reports the
			// call's own audio active inside the expiring window,
			// DEFER: re-arm the same window instead of speaking.
			// Bounded at silenceAudioDeferralCap consecutive deferrals
			// so a permanently-audible stream cannot suppress the
			// nudge (and through it the hangup) forever. The same
			// check covers the loop-start window over the greeting's
			// audio tail (S2): the greeting rides the same pump and
			// stamps the splice's audio activity.
			if silenceDeferrals < silenceAudioDeferralCap &&
				t.silenceAudioDefers(ccid, silenceArmedSeconds) {
				silenceDeferrals++
				t.log.Info("voice talk loop: silence window deferred (agent audio active)",
					"ccid", ccid, "window_s", float64(silenceArmedSeconds),
					"deferrals", silenceDeferrals)
				t.emitEvent(ccid, EventInput{
					Kind:   EventKindSilenceDeferred,
					Reason: itoaSilence(silenceDeferrals),
				})
				rearmSilenceWindow(silenceArmedSeconds)
				continue
			}
			silenceDeferrals = 0
			if !t.silencePrompted {
				// First fire: the dedicated dead-air nudge, then re-arm
				// with the hangup window H (S when H unset - legacy
				// double-window). Fallback chain inside
				// silencePromptLine: configured line -> fillerLine() ->
				// nothing.
				t.silencePrompted = true
				line := t.silencePromptLine()
				afterS := t.now().Sub(silenceArmedAt).Seconds()
				t.log.Info("voice talk loop: silence prompt",
					"ccid", ccid, "line", line, "after_s", afterS,
					"lane", string(LaneSilence), "clip", t.cfg.Identity != nil && line != "")
				if line != "" {
					if sayErr := t.sayClip(ccid, line, t.clipID(line), false); sayErr != nil {
						t.log.Debug("talkloop: partial say dropped", "ccid", ccid, "err", sayErr)
					}
				}
				hangupWindow := t.cfg.SilenceHangupAfterSeconds
				if hangupWindow <= 0 {
					hangupWindow = t.cfg.SilencePromptSeconds
				}
				rearmSilenceWindow(hangupWindow)
				continue
			}
			// Second consecutive fire: hang up with the named reason.
			t.cancelPipeline(ccid, "teardown")
			hangupWindow := t.cfg.SilenceHangupAfterSeconds
			if hangupWindow <= 0 {
				hangupWindow = t.cfg.SilencePromptSeconds
			}
			t.end(ccid, fmt.Sprintf("silence (%ds without caller speech after prompt)", hangupWindow))
			return nil
		case <-turnTimerC:
			// Hard cap on ONE in-flight turn (opt-in). The fire is
			// consulted once: nil pipeline (turn already drained or
			// never started) means the cap missed its window - drop it
			// and let the silence path own the call from here.
			turnTimerC = nil
			if t.curPipeline() == nil {
				continue
			}
			t.cancelPipeline(ccid, "teardown")
			t.end(ccid, fmt.Sprintf("turn timeout (%ds without a spoken reply)", t.cfg.TurnTimeoutSeconds))
			return nil
		case <-t.drainSignal:
			// A cancelled draft ends the silence suspension (defect 7).
			// Only re-arm when NOTHING is in flight and the clock is
			// still stopped: after a supersede the signal lands while
			// the NEWER draft is already live, and re-arming there would
			// unsuspend the clock mid-generation (the fresh draft's own
			// drain branch re-arms instead). A live silenceC means some
			// other path already re-armed — never stomp a fresher window.
			if t.curPipeline() == nil && silenceC == nil {
				rearmSilence()
			}
		case vr, ok := <-current():
			if !ok {
				// Draft finished (checker closed the channel): the
				// turn is no longer in flight, so the silence clock
				// re-arms fresh from NOW (turn-aware suspension ends)
				// and the turn cap stands down.
				p := t.curPipeline()
				stopTurn()
				flushTurnEnd(ccid)
				t.logTurnFinal(ccid, p)
				t.clearPipeline()
				rearmSilence()
				if p != nil {
					// Link half-duplex: the speaker's stream is fully
					// flushed - the coordinator arms its drain margin
					// on this edge and releases the held side.
					t.publishTurnState(ccid, p.seq, TurnKindFinal)
				}
				continue
			}
			t.playVerdict(ccid, vr)
		case <-t.flush:
			// Link half-duplex release signal (coordinator's margin
			// fired, or teardown flush): dispatch the parked prompt on
			// THIS goroutine, preserving the Run-owned timer discipline.
			t.releaseHeld(ccid)
		case f, ok := <-t.frames:
			if !ok {
				t.cancelPipeline(ccid, "teardown")
				t.end(ccid, "hangup")
				return nil
			}
			switch f.Event {
			case EventPrompt:
				if f.Last == nil || !*f.Last {
					// Partial transcript: preview only, never a turn.
					continue
				}
				// CROSS-CALL ECHO CONTRACT (2026-11-11): a final prompt
				// that fuzzy-matches agent speech spoken by ANOTHER live
				// call inside the roster TTL is room echo (call A's TTS
				// out the speakers -> call B's mic -> whisper final ->
				// B would answer A: the "two agents answering each other"
				// defect, audit symptom (b)). Drop the DISPATCH — the call
				// lives on. This gate lives ONLY in the voice talk loop's
				// prompt path; agent/chat dispatch paths are untouched
				// by contract. LINK calls are exempt: a link is a
				// DELIBERATE two-agent conversation (the half-duplex
				// coordinator pins A's greeting crossing B's mic as the
				// conversation seed — internal/voice/AGENTS.md turn-taking
				// section), and the only loops that ever receive the
				// HoldPrompt/TurnState seams are link loops, so the seam
				// is the discriminator. Link speech is still RECORDED
				// (say-choke): a plain local/phone call that overhears a
				// link conversation must still drop the echo. The
				// short-prompt floor (echoMinPromptWords)
				// is enforced inside the roster; interaction with the
				// sibling 9f860991 min_turn_words gate is documented in
				// echo_roster.go (harmless overlap: that gate drops short
				// finals by length for every call, this floor just
				// declines to run the scorer on them).
				//
				// Telemetry posture: routeFrame already appended the
				// `heard` ring event BEFORE enqueueing this frame (the
				// mic really did hear it — keeping the ring honest means
				// we do NOT retroactively hide that), so we only append
				// the dispatch-drop marker here. The gate runs BEFORE
				// any dispatch bookkeeping (no timers stop, no pipeline
				// cancels, no park slot touched).
				if t.deps.HoldPrompt == nil && RecentOtherSpeech(ccid, f.Text) {
					// Pure no-op dispatch: no cancelPipeline, no timer
					// churn, no floor effects — an echo must not interrupt
					// a legitimate in-flight reply or touch the link park.
					t.log.Info("voice talk loop: echo prompt dropped (cross-call)",
						"ccid", ccid, "text_preview", previewPrompt(f.Text),
						"words", wordCount(f.Text))
					t.emitEvent(ccid, EventInput{
						Kind:   EventKindDropped,
						Text:   previewPrompt(f.Text),
						Reason: "cross-call echo",
					})
					continue
				}
				// Link half-duplex seam: when a coordinator holds (the
				// other side of the link is mid-turn), park the dispatch
				// closure instead of running it - the prompt waits for
				// the speaker's turn_final + margin, then releases on
				// THIS goroutine via the flush channel. Nil HoldPrompt
				// (every non-link call) dispatches inline, byte-identical.
				if t.deps.HoldPrompt != nil {
					text := f.Text
					dispatch := func() { dispatchPrompt(text) }
					if !t.deps.HoldPrompt(call, dispatch) {
						t.parkPrompt(call, dispatch, text)
						continue
					}
					// Coordinator allows dispatch NOW: a prompt parked
					// from an earlier question is stale under the
					// latest-question-wins rule - drop it, then speak.
					t.dropHeld(ccid, "newer prompt took the floor")
					dispatch()
					continue
				}
				dispatchPrompt(f.Text)
			case EventInterrupt:
				t.handleInterrupt(ccid)
			case EventEnd:
				t.cancelPipeline(ccid, "teardown")
				t.end(ccid, namedEndReason(f.Reason))
				return nil
			default:
				// setup and unknown events are not the talk loop's
				// business; the splice and wiring own them.
			}
		}
	}
}

// spawnPipeline starts one prompt's draft-to-speech pipeline: the employee
// turn runs on its own context (cancelled on interrupt/teardown), sentences
// split, and a checker goroutine walks them one Check ahead of playback.
// Verdicts flow through p.checks; Run plays them in its select.
//
// Filler posture (leaf 02, dead-air fix): fillerLine is picked here (Run
// goroutine owns fillerIdx). With cfg.FillerGrace == 0 it speaks inline at
// dispatch (legacy behavior, byte-identical order pin); with a positive
// grace it arms a time.AfterFunc that speaks the line ONLY if the draft has
// still not produced its first allowed sentence when the window elapses.
// The closure re-checks under interruptMu that this pipeline is still the
// live draft before saying anything — a cancelled or hung-up draft never
// emits zombie filler.
//
// fragmentLine is the Run-picked clarification line ("" = no fragment
// handling — either no lane configured or the prompt is not a fragment);
// it is passed in, never re-picked here, so the fragment cursor stays
// Run-owned (spawnPipeline's checker goroutine must not touch it).
func (t *TalkLoop) spawnPipeline(ctx context.Context, call Instance, prompt, fillerLine, fragmentLine string) {
	ccid := call.CallControlID

	turnCtx, cancel := context.WithCancel(ctx)

	t.interruptMu.Lock()
	t.gen++
	t.seq++
	seq := t.seq
	p := &pipeline{
		gen:          t.gen,
		seq:          seq,
		cancel:       cancel,
		checks:       make(chan verdict, 1),
		fillerText:   fillerLine,
		dispatchedAt: t.now(),
	}
	t.cur = p
	t.interruptMu.Unlock()

	// Telemetry: the prompt actually dispatched a turn (non-speech finals
	// never reach this point — the child-side guard drops them before the
	// frame is sent). kind=prompt_dispatch.
	preview := prompt
	if runes := []rune(preview); len(runes) > 40 {
		preview = string(runes[:40]) + "…"
	}
	t.log.Info("voice talk loop: turn dispatch",
		"ccid", ccid, "seq", seq, "kind", "prompt_dispatch",
		"text_preview", preview, "words", wordCount(prompt))
	t.emitEvent(ccid, EventInput{Kind: EventKindDispatch, Text: preview, Words: wordCount(prompt)})

	// Link half-duplex: this side is now the speaker (the turn will
	// emit say frames) - the coordinator marks it busy so the OTHER
	// side's next final prompt parks instead of talking over it.
	if t.deps.TurnState != nil {
		t.publishTurnState(ccid, seq, TurnKindStart)
	}

	if fillerLine != "" {
		if t.cfg.FillerGrace <= 0 {
			// Legacy: speak immediately at dispatch (order pinned by
			// TestTalkLoopGreetingAndFiller's grace-0 sibling runs).
			t.interruptMu.Lock()
			p.utteranceOut = true // no filler can fire later for this draft
			p.fillerFired = true
			after := int64(0)
			t.interruptMu.Unlock()
			if err := t.sayClip(ccid, fillerLine, t.clipID(fillerLine), false); err != nil {
				t.log.Warn("voice talk loop: filler send failed", "ccid", ccid, "error", err)
			}
			t.log.Info("voice talk loop: turn filler",
				"ccid", ccid, "seq", seq, "kind", "filler",
				"line", fillerLine, "after_ms", after,
				"lane", string(LaneFiller), "clip", t.cfg.Identity != nil)
		} else {
			grace := t.cfg.FillerGrace
			t.interruptMu.Lock()
			p.fillerPending = true
			p.fillerTimer = time.AfterFunc(grace, func() {
				// Timer goroutine: latch under interruptMu BEFORE any
				// I/O. Emit only if this draft is still live, nothing
				// has gone out for it yet, and its filler is still
				// pending (not superseded/cancelled/answered).
				t.interruptMu.Lock()
				if t.cur != p || !p.fillerPending || p.utteranceOut || p.fillerFired {
					t.interruptMu.Unlock()
					return
				}
				p.fillerPending = false
				p.fillerFired = true
				after := t.now().Sub(p.dispatchedAt).Milliseconds()
				t.interruptMu.Unlock()
				if err := t.sayClip(ccid, p.fillerText, t.clipID(p.fillerText), false); err != nil {
					t.log.Warn("voice talk loop: delayed filler send failed", "ccid", ccid, "error", err)
					return
				}
				t.log.Info("voice talk loop: turn filler",
					"ccid", ccid, "seq", seq, "kind", "filler",
					"line", p.fillerText, "after_ms", after,
					"lane", string(LaneFiller), "clip", t.cfg.Identity != nil)
			})
			t.interruptMu.Unlock()
		}
	}

	go func() {
		// Fragment-clarify (voice.fragment_clarify): if the caller
		// trailed off into an unfinished fragment (whisper cut it
		// mid-thought and the endpointing layer already waited
		// fragment_confirm_ms with no continuation), speak the
		// configured clarification line and SKIP the LLM turn — a human
		// says "Was that all? I'm not sure I understand you" rather than
		// coldly answering the half-broken sentence. Fact-free like
		// filler, so it bypasses the supervisor gate. Empty config keeps
		// the legacy ready path; only the trailing-ellipsis shape trips
		// (a completed word/sentence never does), so a real one-word
		// command like "Stop." is never clarified away. The line was
		// picked on the Run goroutine (fragmentIdx, like fillerIdx); the
		// empty string means either no lane configured or the prompt is
		// not a fragment.
		if fragmentLine != "" && isTrailingEllipsis(prompt) {
			t.interruptMu.Lock()
			stale := t.gen != p.gen
			if !stale {
				p.utteranceOut = true
				p.fillerPending = false
				if p.fillerTimer != nil {
					p.fillerTimer.Stop()
					p.fillerTimer = nil
				}
				p.turnEnded = true
			}
			t.interruptMu.Unlock()
			if !stale {
				if err := t.sayClip(ccid, fragmentLine, t.clipID(fragmentLine), true); err == nil {
					t.log.Info("voice talk loop: fragment clarify", "ccid", ccid, "seq", seq, "kind", "fragment_clarify", "line", fragmentLine, "lane", string(LaneFragment), "clip", t.cfg.Identity != nil)
					t.sayProgress(ccid)
				} else {
					t.log.Warn("voice talk loop: fragment clarify send failed", "ccid", ccid, "error", err)
				}
			}
			close(p.checks)
			return
		}
		// The turn itself: run AFTER the pipeline is registered so an
		// interrupt racing the spawn still bumps past this gen.
		reply, err := t.deps.Turns.Run(turnCtx, t.deps.SessionFor(call), prompt)
		if err != nil {
			if turnCtx.Err() != nil {
				return // interrupt/teardown: the draft dies silently
			}
			t.log.Error("voice talk loop: employee turn failed", "ccid", ccid, "error", err)
			p.checks <- verdict{fallback: true, final: true}
			close(p.checks)
			return
		}
		if strings.TrimSpace(reply) == "" {
			close(p.checks)
			return
		}
		// Connected-utterance path (ConnectSentences, wired for a disabled
		// supervisor gate): the per-sentence split exists ONLY so the
		// supervisor can check each sentence. With no gate to satisfy, sort
		// by the whole draft as ONE connected utterance — the TTS then
		// produces natural cross-sentence prosody and real short pauses
		// instead of a flat per-sentence list reading of independent
		// requests (the 400-760 ms artificial gaps in the baseline live
		// call). The utterance is still capped so a runaway draft cannot
		// turn into one unbounded TTS request; beyond the cap it falls back
		// to split sentences so no speech is ever lost.
		unit := strings.TrimSpace(reply)
		if t.cfg.ConnectSentences {
			if n := utf8.RuneCountInString(unit); n > maxSentenceRunes*3 {
				r := []rune(unit)
				unit = string(r[:maxSentenceRunes*3])
			}
			// Fast-first-audio: split the FIRST sentence off as its own
			// (short, fast-to-synthesize) say, then the REMAINDER as the
			// final connected utterance. The child's depth-2 prefetch
			// overlaps the remainder's synthesis under the first
			// sentence's playback, so the caller hears the opening within
			// ~1-2s instead of waiting for the WHOLE reply to synthesize
			// (qwen3 does not stream; the full-utterance synthesis latency
			// was the dominant post-dispatch delay — measured 2-5.6s in
			// live call local-fbf9a76ef9357454). A single-sentence reply
			// stays a single final utterance. Both utterances are checked
			// once (gate-off = instant allow); delivery splits them.
			v, err := t.deps.Supervisor.Check(turnCtx, ccid, unit)
			defer close(p.checks)
			if err != nil {
				if turnCtx.Err() != nil {
					return
				}
				t.interruptMu.Lock()
				stale := t.gen != p.gen
				t.interruptMu.Unlock()
				if stale {
					return
				}
				select {
				case p.checks <- verdict{fallback: true, final: true}:
				case <-turnCtx.Done():
					return
				}
				return
			}
			first, rest := splitFirstSentence(unit)
			if first != "" && rest != "" {
				t.interruptMu.Lock()
				stale := t.gen != p.gen
				t.interruptMu.Unlock()
				if stale {
					return
				}
				// First sentence out first, non-final; the remainder
				// follows as the final connected utterance.
				select {
				case p.checks <- verdict{sentence: first, allowed: v.Allowed, final: false}:
				case <-turnCtx.Done():
					return
				}
				select {
				case p.checks <- verdict{sentence: rest, allowed: v.Allowed, final: true}:
				case <-turnCtx.Done():
					return
				}
				return
			}
			// Single-sentence (or empty rest) reply: one connected final
			// utterance.
			t.interruptMu.Lock()
			stale := t.gen != p.gen
			t.interruptMu.Unlock()
			if stale {
				return
			}
			select {
			case p.checks <- verdict{sentence: unit, allowed: v.Allowed, final: true}:
			case <-turnCtx.Done():
				return
			}
			return
		}
		sentences := SplitSentences(reply)
		defer close(p.checks)
		for i, sentence := range sentences {
			v, err := t.deps.Supervisor.Check(turnCtx, ccid, sentence)
			if err != nil {
				if turnCtx.Err() != nil {
					// Transport-level cancel (interrupt/teardown): the
					// whole pipeline dies silently (C6 contract).
					return
				}
				// Genuine check failure (timeout/quota/transport error on
				// a live call): fail closed via the fallback verdict; the
				// caller hears the fallback line and the call continues
				// (never quota-parked, never silent).
				t.interruptMu.Lock()
				stale := t.gen != p.gen
				t.interruptMu.Unlock()
				if stale {
					return
				}
				select {
				case p.checks <- verdict{fallback: true, final: i == len(sentences)-1}:
				case <-turnCtx.Done():
					return
				}
				continue
			}
			t.interruptMu.Lock()
			stale := t.gen != p.gen
			t.interruptMu.Unlock()
			if stale {
				return // superseded by an interrupt or newer prompt
			}
			select {
			case p.checks <- verdict{
				sentence: sentence,
				allowed:  v.Allowed,
				final:    i == len(sentences)-1,
			}:
			case <-turnCtx.Done():
				return
			}
		}
	}()
}

// playVerdict speaks one verdict: allowed sentences become say frames;
// denied sentences are dropped (never enter a say frame); a fallback
// verdict (check failure) speaks the C6 line. A stale verdict (older
// generation) is dropped unsaid.
func (t *TalkLoop) playVerdict(ccid string, vr verdict) {
	t.interruptMu.Lock()
	p := t.cur
	t.interruptMu.Unlock()
	if p == nil {
		return
	}

	if vr.fallback {
		// Check failure (C6): the caller hears the fallback line, the
		// call continues. Spoken once per draft, with the turn-final
		// flush carried on it.
		if !p.fallback {
			p.fallback = true
			p.turnEnded = true
			t.interruptMu.Lock()
			p.utteranceOut = true // fallback counts as the utterance
			p.stopFillerLocked()
			t.interruptMu.Unlock()
			line := t.fallbackLine()
			if err := t.sayClip(ccid, line, t.clipID(line), true); err == nil {
				t.log.Info("voice talk loop: fallback line",
					"ccid", ccid, "line", line,
					"lane", "fallback", "clip", t.cfg.Identity != nil)
				t.sayProgress(ccid)
			}
		}
		return
	}

	if vr.allowed {
		if err := t.say(ccid, vr.sentence, vr.final); err != nil {
			t.log.Warn("voice talk loop: say send failed", "ccid", ccid, "error", err)
		} else {
			t.sayProgress(ccid)
		}
		t.interruptMu.Lock()
		p.said++
		// Delayed-filler latch: something of THIS turn's reached the
		// caller, so the grace timer must not speak filler behind it.
		p.utteranceOut = true
		if p.stopFillerLocked() {
			p.fillerSkipped = true
		}
		p.replyWords += wordCount(vr.sentence)
		if vr.final {
			p.turnEnded = true
		}
		t.interruptMu.Unlock()
		return
	}

	// Denied sentence: dropped, never spoken. When the whole draft ends
	// with nothing allowed, the fallback line keeps the caller informed
	// (spoken at the draft's final slot). Otherwise the flush at drain
	// sends the turn-final marker, so a denied final sentence after
	// spoken sentences still ends the child's turn (M2).
	if vr.final && p.said == 0 {
		p.fallback = true
		p.turnEnded = true
		t.interruptMu.Lock()
		p.utteranceOut = true // fallback counts as the utterance
		p.stopFillerLocked()
		t.interruptMu.Unlock()
		line := t.fallbackLine()
		if err := t.sayClip(ccid, line, t.clipID(line), true); err == nil {
			t.log.Info("voice talk loop: fallback line (all sentences denied)",
				"ccid", ccid, "line", line,
				"lane", "fallback", "clip", t.cfg.Identity != nil)
			t.sayProgress(ccid)
		}
	} else {
		t.log.Info("voice talk loop: sentence denied and dropped", "ccid", ccid, "position", p.said)
	}
}

// curPipeline returns the live pipeline pointer (nil between turns). The
// select loop uses it to make the silence path turn-aware: a nil pipeline
// means no turn is in flight, so the silence clock may act.
func (t *TalkLoop) curPipeline() *pipeline {
	t.interruptMu.Lock()
	defer t.interruptMu.Unlock()
	return t.cur
}

// clearPipeline drops the reference to a finished pipeline (the checker's
// close already told Run it ended).
func (t *TalkLoop) clearPipeline() {
	t.interruptMu.Lock()
	t.cur = nil
	t.interruptMu.Unlock()
}

// cancelPipeline cancels the live pipeline (interrupt/teardown semantics)
// and removes it. The checker goroutine observes the generation bump or the
// cancelled context and exits without speaking. reason feeds the
// turn-cancelled telemetry line (leaf 02): "barge-in" | "supersede" |
// "teardown". A pending delayed-filler timer is stopped here - the
// zombie-fillers pin (a cancelled draft never speaks filler afterwards).
func (t *TalkLoop) cancelPipeline(ccid, reason string) {
	t.interruptMu.Lock()
	p := t.cur
	t.cur = nil
	if p != nil {
		t.gen++ // stale-proof any in-flight Check racing the cancel
		p.stopFillerLocked()
	}
	t.interruptMu.Unlock()
	if p == nil {
		return
	}
	t.log.Info("voice talk loop: turn cancelled",
		"ccid", ccid, "seq", p.seq, "kind", "draft_cancel", "reason", reason)
	// Link half-duplex: a barge-in/teardown cancel means THIS side
	// stopped speaking without a turn_final - the coordinator releases
	// the peer's parked prompt immediately (nothing is playing anymore).
	// A "supersede" cancel does NOT idle: the same side immediately
	// spawns a newer draft (turn_start follows), so the speaker slot
	// stays held and no premature release fires.
	if t.deps.TurnState != nil && reason != "supersede" {
		t.publishTurnState(ccid, p.seq, TurnKindIdle)
	}
	if p.cancel != nil {
		p.cancel()
	}
	// Defect-7 re-arm wake-up: a cancelled draft is a DRAIN for the
	// silence suspension, but its p.checks close is invisible to Run
	// (t.cur was already dropped, so current() is the disarmed nil
	// channel). Wake the select so the Run-goroutine handler can restart
	// the clock. Non-blocking send on a buffered(1) channel: cancelPipeline
	// may run on the splice's CancelDraftFn goroutine, and one pending
	// wake-up is all the level-free re-arm check needs (teardown sends are
	// harmless — Run returns right after and never reads the signal).
	select {
	case t.drainSignal <- struct{}{}:
	default:
	}
}

// logTurnFinal emits the per-turn completion telemetry (leaf 02): words
// spoken + dispatch->final wall time. p may be nil (no draft was live).
func (t *TalkLoop) logTurnFinal(ccid string, p *pipeline) {
	if p == nil {
		return
	}
	t.interruptMu.Lock()
	words := p.replyWords
	ms := t.now().Sub(p.dispatchedAt).Milliseconds()
	p.stopFillerLocked()
	skipped := p.fillerSkipped
	t.interruptMu.Unlock()
	if skipped {
		t.log.Info("voice talk loop: turn filler skipped",
			"ccid", ccid, "seq", p.seq, "kind", "filler_skipped",
			"reason", "answered inside grace")
	}
	t.log.Info("voice talk loop: turn final",
		"ccid", ccid, "seq", p.seq, "kind", "turn_final",
		"reply_words", words, "ms", ms)
	t.emitEvent(ccid, EventInput{Kind: EventKindFinal, Words: words, Ms: ms})
}

// wordCount counts whitespace-separated words in spoken text
// (telemetry preview only - exactness does not matter).
func wordCount(text string) int {
	return len(strings.Fields(text))
}

// splitFirstSentence returns (first, rest): the leading sentence (ending at
// the first terminal '.?!' or newline) and everything after it, both
// trimmed. Used by the fast-first-audio connected-utterance path so the
// opener can synthesize and play while the remainder generates.
func splitFirstSentence(text string) (string, string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", ""
	}
	for i, r := range t {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			first := strings.TrimSpace(t[:i+1])
			rest := strings.TrimSpace(t[i+1:])
			if first != "" && rest != "" {
				return first, rest
			}
		}
	}
	return t, ""
}

// isTrailingEllipsis reports whether text ends in an ellipsis (the U+2026
// rune or an ASCII multi-dot run) — whisper's mark for a mid-thought
// cutoff ("...got any t..."). Single terminal '.?!' is a completed
// sentence. Mirrors voicechild.isUnfinishedReport at the dispatch layer so
// the talk loop can decide fragment-clarify without importing the child.
func isTrailingEllipsis(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	runes := []rune(t)
	if len(runes) == 0 {
		return false
	}
	last := runes[len(runes)-1]
	if last == '…' {
		return true
	}
	if last != '.' {
		return false
	}
	dots := 0
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '.' || runes[i] == '…' {
			dots++
		} else {
			break
		}
	}
	return dots >= 2
}

// handleInterrupt runs the barge-in contract for THIS call only: drop the
// pipeline (generation bump + turn cancel), tell the binding, and send a
// clear frame on the text socket (the child flushes its TTS queue; the
// splice owns the Telnyx-side clear).
//
// Local-leg media latch (bughunt 2026-11-11 C-1): the child interrupt
// frame is ALSO the local leg's cancel trigger — the live splice's
// HandleInterrupt is what latches the outbound pacer, bumps the
// fresh-audio gate, sends the child clear, and fires CancelDraft. The
// glue's cancelDraftHook only ever runs from the splice-side CancelDraft
// dep (RMS trips), so without this call the frame path left the splice
// un-latched: no gate bump, no CancelDraft, and the pacer kept draining
// buffered .out audio over the caller's new turn. HandleInterrupt is
// ccid-guarded (foreign/no live splice = no-op) and per-barge-in latched
// (a second call for the same cancel is a no-op), so phone and plain
// local calls resolve to nil and stay byte-identical. The RMS seam never
// re-runs it: the splice latches cancelOne first, and the loop's
// cancelPipeline is idempotent per generation.
func (t *TalkLoop) handleInterrupt(ccid string) {
	if sp := LocalSpliceFor(ccid); sp != nil {
		sp.HandleInterrupt(ccid)
	}
	t.cancelPipeline(ccid, "barge-in")
	if t.deps.Binding != nil {
		t.deps.Binding.PublishInterrupt(ccid)
	}
	if err := t.deps.Send(Frame{CallID: ccid, Event: EventClear}); err != nil {
		t.log.Warn("voice talk loop: clear send failed", "ccid", ccid, "error", err)
	}
	t.log.Info("voice talk loop: barge-in", "ccid", ccid)
	t.emitEvent(ccid, EventInput{Kind: EventKindBargeIn})
}

// HandleBargeIn is the media-side barge-in seam (the splice's
// CancelDraftFn): identical semantics to a child interrupt frame, minus the
// clear echo on the text socket (the splice itself sent clear toward
// Telnyx; the child's queue flush still applies).
func (t *TalkLoop) HandleBargeIn(ccid string) {
	t.cancelPipeline(ccid, "barge-in")
	if t.deps.Binding != nil {
		t.deps.Binding.PublishInterrupt(ccid)
	}
	if err := t.deps.Send(Frame{CallID: ccid, Event: EventClear}); err != nil {
		t.log.Warn("voice talk loop: media clear send failed", "ccid", ccid, "error", err)
	}
	t.log.Info("voice talk loop: media barge-in", "ccid", ccid)
	t.emitEvent(ccid, EventInput{Kind: EventKindBargeIn})
}

// verdict is one supervisor outcome flowing from the checker to playback.
type verdict struct {
	sentence string
	allowed  bool
	final    bool
	// fallback marks a check failure (turn error): speak the C6 line.
	fallback bool
}

// namedEndReason maps a child end frame's reason onto the hangup reason
// recorded by the release fan-out. An empty reason stays "hangup".
func namedEndReason(reason string) string {
	if reason == "" {
		return "hangup"
	}
	return "child end: " + reason
}
