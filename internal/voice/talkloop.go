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
// path (silence_prompt_seconds) speaks one filler-class prompt after the
// first quiet window and hangs up if the silence persists another window.
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
	"time"
)

// fallbackSentence is the C6 fail-closed line spoken when a supervisor
// check fails or when a draft ends with nothing allowed. FallbackLine is
// the operator override; this constant is the wiring default.
const fallbackSentence = "I cannot confirm that yet."

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
	// final prompt, the loop speaks one filler-class prompt; if the
	// silence persists another full window, the call hangs up. Zero
	// disables.
	SilencePromptSeconds int
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
	// wiring passes voice.filler_grace_ms (default 2500). The
	// silence-prompt filler is NEVER grace-gated - that line is the
	// dead-air nudge itself.
	FillerGrace time.Duration
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
	// fillerIdx round-robins over the filler list.
	fillerIdx int

	// interruptMu guards ONLY the generation counter and pipeline pointer
	// swap (repo mutex-scope rule: no I/O, send, or Check under it). The
	// delayed-filler latch (pipeline.filler*/utteranceOut) also rides this
	// lock; no I/O happens while it is held.
	interruptMu sync.Mutex
	gen         uint64
	seq         uint64
	cur         *pipeline

	// silencePrompted latches the one filler-class silence prompt: the
	// first silence fire speaks, the second hangs up.
	silencePrompted bool

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
		deps:   deps,
		cfg:    cfg,
		log:    logger,
		now:    now,
		frames: make(chan Frame, 16),
	}
}

// Frames returns the inbound frame sink. The wiring routes this call's C1
// frames (prompt/interrupt/end) from the TextServer handler here.
func (t *TalkLoop) Frames() chan<- Frame { return t.frames }

// greetingLine returns the configured greeting, falling back to the first
// filler line (the greeting is filler-class; an operator who configured no
// filler gets no greeting rather than a hardcoded line).
func (t *TalkLoop) greetingLine() string {
	if t.cfg.GreetingLine != "" {
		return t.cfg.GreetingLine
	}
	if len(t.cfg.FillerLines) > 0 {
		return t.cfg.FillerLines[0]
	}
	return ""
}

// fillerLine picks the next fact-free line round-robin over the config
// list; an empty list disables the filler.
func (t *TalkLoop) fillerLine() string {
	if len(t.cfg.FillerLines) == 0 {
		return ""
	}
	line := t.cfg.FillerLines[t.fillerIdx%len(t.cfg.FillerLines)]
	t.fillerIdx++
	return line
}

// fallbackLine returns the configured C6 fallback line or the default.
func (t *TalkLoop) fallbackLine() string {
	if t.cfg.FallbackLine != "" {
		return t.cfg.FallbackLine
	}
	return fallbackSentence
}

// say sends one say frame. last marks the final sentence of a turn.
func (t *TalkLoop) say(ccid, text string, last bool) error {
	lastFlag := last
	return t.deps.Send(Frame{
		CallID: ccid,
		Event:  EventSay,
		Text:   text,
		Last:   &lastFlag,
	})
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
		t.deps.OnEnd(ccid, reason)
	})
}

// Run consumes this call's frames and plays the pipelined verdicts until an
// end frame, a timer, or ctx cancellation ends the call. It lives for the
// call: the wiring spawns it when the media bridge registers and cancels it
// on teardown.
func (t *TalkLoop) Run(ctx context.Context, call Instance) error {
	ccid := call.CallControlID

	// Greeting first (spec call flow: the agent speaks first on a
	// connected call; fact-free, interruptible, never supervisor-gated).
	if line := t.greetingLine(); line != "" {
		if err := t.say(ccid, line, false); err != nil {
			t.log.Warn("voice talk loop: greeting send failed", "ccid", ccid, "error", err)
		}
	}

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
	// legacy two-fire silence path is byte-identical.
	//
	// Timer discipline (M5): never Reset a timer whose fire may already
	// be buffered (or racing Stop) — the stale value would be consumed
	// by the next select and double-fire the silence path. Instead every
	// re-arm abandons the old timer and arms a FRESH one: a stale fire
	// lands in an orphaned channel nobody reads and is GC'd with the old
	// timer; the new window starts clean.
	var silenceTimer *time.Timer
	var silenceC <-chan time.Time
	rearmSilence := func() {
		if t.cfg.SilencePromptSeconds <= 0 {
			return
		}
		if silenceTimer != nil {
			silenceTimer.Stop()
		}
		silenceTimer = time.NewTimer(time.Duration(t.cfg.SilencePromptSeconds) * time.Second)
		silenceC = silenceTimer.C
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
	// prompts.
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
			if !t.silencePrompted {
				// First fire: one filler-class prompt, then re-arm.
				t.silencePrompted = true
				if line := t.fillerLine(); line != "" {
					if sayErr := t.say(ccid, line, false); sayErr != nil {
						t.log.Debug("talkloop: partial say dropped", "ccid", ccid, "err", sayErr)
					}
				}
				rearmSilence()
				continue
			}
			// Second consecutive fire: hang up with the named reason.
			t.cancelPipeline(ccid, "teardown")
			t.end(ccid, fmt.Sprintf("silence (%ds without caller speech after prompt)", t.cfg.SilencePromptSeconds))
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
				continue
			}
			t.playVerdict(ccid, vr)
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
				// verdict).
				t.spawnPipeline(ctx, call, f.Text, t.fillerLine())
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
func (t *TalkLoop) spawnPipeline(ctx context.Context, call Instance, prompt, fillerLine string) {
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

	if fillerLine != "" {
		if t.cfg.FillerGrace <= 0 {
			// Legacy: speak immediately at dispatch (order pinned by
			// TestTalkLoopGreetingAndFiller's grace-0 sibling runs).
			t.interruptMu.Lock()
			p.utteranceOut = true // no filler can fire later for this draft
			p.fillerFired = true
			after := int64(0)
			t.interruptMu.Unlock()
			if err := t.say(ccid, fillerLine, false); err != nil {
				t.log.Warn("voice talk loop: filler send failed", "ccid", ccid, "error", err)
			}
			t.log.Info("voice talk loop: turn filler",
				"ccid", ccid, "seq", seq, "kind", "filler",
				"line", fillerLine, "after_ms", after)
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
				if err := t.say(ccid, p.fillerText, false); err != nil {
					t.log.Warn("voice talk loop: delayed filler send failed", "ccid", ccid, "error", err)
					return
				}
				t.log.Info("voice talk loop: turn filler",
					"ccid", ccid, "seq", seq, "kind", "filler",
					"line", p.fillerText, "after_ms", after)
			})
			t.interruptMu.Unlock()
		}
	}

	go func() {
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
			if err := t.say(ccid, t.fallbackLine(), true); err == nil {
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
		if err := t.say(ccid, t.fallbackLine(), true); err == nil {
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
	if p.cancel != nil {
		p.cancel()
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
}

// wordCount counts whitespace-separated words in spoken text
// (telemetry preview only - exactness does not matter).
func wordCount(text string) int {
	return len(strings.Fields(text))
}

// handleInterrupt runs the barge-in contract for THIS call only: drop the
// pipeline (generation bump + turn cancel), tell the binding, and send a
// clear frame on the text socket (the child flushes its TTS queue; the
// splice owns the Telnyx-side clear).
func (t *TalkLoop) handleInterrupt(ccid string) {
	t.cancelPipeline(ccid, "barge-in")
	if t.deps.Binding != nil {
		t.deps.Binding.PublishInterrupt(ccid)
	}
	if err := t.deps.Send(Frame{CallID: ccid, Event: EventClear}); err != nil {
		t.log.Warn("voice talk loop: clear send failed", "ccid", ccid, "error", err)
	}
	t.log.Info("voice talk loop: barge-in", "ccid", ccid)
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
