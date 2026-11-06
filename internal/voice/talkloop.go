package voice

// Talk loop (leaf 14): the per-call goroutine that turns final prompts into
// supervised speech. Contract assembly point for C1 (frames over the text
// socket), C6 (supervisor verdicts), and the leaf-12 binding (employee
// turns on the bound session).
//
// Flow per final prompt (last:true):
//
//	prompt -> filler say at once (fact-free, C1 exception)
//	       -> Binding turn (employee draft on the pinned endpoint)
//	       -> SplitSentences
//	       -> pipelined supervisor Check (sentence N+1 is checked while
//	         sentence N is playing)
//	       -> allowed sentences become say frames; denied sentences are
//	         dropped; a check FAILURE speaks the C6 fallback line
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

// pipeline is one prompt's draft-to-speech state: the generation it was
// spawned under, its cancel func (interrupt/teardown), and the verdict
// channel the checker feeds. said/fallback are owned by the playback
// goroutine (Run), never the checker.
type pipeline struct {
	gen      uint64
	cancel   context.CancelFunc
	checks   chan verdict
	said     int  // allowed sentences spoken so far in this draft
	fallback bool // fallback line already spoken in this draft
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
	// swap (repo mutex-scope rule: no I/O, send, or Check under it).
	interruptMu sync.Mutex
	gen         uint64
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
	// final prompt.
	var silenceTimer *time.Timer
	var silenceC <-chan time.Time
	armSilence := func() {
		if t.cfg.SilencePromptSeconds <= 0 {
			return
		}
		if silenceTimer == nil {
			silenceTimer = time.NewTimer(time.Duration(t.cfg.SilencePromptSeconds) * time.Second)
		} else {
			silenceTimer.Reset(time.Duration(t.cfg.SilencePromptSeconds) * time.Second)
		}
		silenceC = silenceTimer.C
	}
	stopSilence := func() {
		if silenceTimer != nil {
			silenceTimer.Stop()
		}
		silenceC = nil
	}
	armSilence()
	defer stopSilence()

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

	for {
		select {
		case <-ctx.Done():
			t.cancelPipeline()
			t.end(ccid, "hangup")
			return ctx.Err()
		case <-maxC:
			t.cancelPipeline()
			t.end(ccid, fmt.Sprintf("max_call_minutes reached (%d)", t.cfg.MaxCallMinutes))
			return nil
		case <-silenceC:
			silenceC = nil
			if t.cfg.SilencePromptSeconds <= 0 {
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
				armSilence()
				continue
			}
			// Second consecutive fire: hang up with the named reason.
			t.cancelPipeline()
			t.end(ccid, fmt.Sprintf("silence (%ds without caller speech after prompt)", t.cfg.SilencePromptSeconds))
			return nil
		case vr, ok := <-current():
			if !ok {
				// Draft finished (checker closed the channel).
				t.clearPipeline()
				armSilence()
				continue
			}
			t.playVerdict(ccid, vr)
		case f, ok := <-t.frames:
			if !ok {
				t.cancelPipeline()
				t.end(ccid, "hangup")
				return nil
			}
			switch f.Event {
			case EventPrompt:
				if f.Last == nil || !*f.Last {
					// Partial transcript: preview only, never a turn.
					continue
				}
				stopSilence()
				t.silencePrompted = false
				// A newer prompt supersedes any in-flight draft (the
				// barge-in drop rule applies to a fresh question too).
				t.cancelPipeline()
				// Filler say at once (call flow step 6): fact-free, so it
				// needs no supervisor verdict.
				if line := t.fillerLine(); line != "" {
					if err := t.say(ccid, line, false); err != nil {
						t.log.Warn("voice talk loop: filler send failed", "ccid", ccid, "error", err)
					}
				}
				t.spawnPipeline(ctx, call, f.Text)
			case EventInterrupt:
				t.handleInterrupt(ccid)
			case EventEnd:
				t.cancelPipeline()
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
func (t *TalkLoop) spawnPipeline(ctx context.Context, call Instance, prompt string) {
	ccid := call.CallControlID

	turnCtx, cancel := context.WithCancel(ctx)

	t.interruptMu.Lock()
	t.gen++
	p := &pipeline{
		gen:    t.gen,
		cancel: cancel,
		checks: make(chan verdict, 1),
	}
	t.cur = p
	t.interruptMu.Unlock()

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
		// call continues. Spoken once per draft.
		if !p.fallback {
			p.fallback = true
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
		p.said++
		return
	}

	// Denied sentence: dropped, never spoken. When the whole draft ends
	// with nothing allowed, the fallback line keeps the caller informed
	// (spoken at the draft's final slot). The final-flag closure lives in
	// the pipeline drain: a denied FINAL sentence with zero said sentences
	// speaks the fallback as the last say.
	if vr.final && p.said == 0 {
		p.fallback = true
		if err := t.say(ccid, t.fallbackLine(), true); err == nil {
			t.sayProgress(ccid)
		}
	} else {
		t.log.Info("voice talk loop: sentence denied and dropped", "ccid", ccid, "position", p.said)
	}
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
// cancelled context and exits without speaking.
func (t *TalkLoop) cancelPipeline() {
	t.interruptMu.Lock()
	p := t.cur
	t.cur = nil
	if p != nil {
		t.gen++ // stale-proof any in-flight Check racing the cancel
	}
	t.interruptMu.Unlock()
	if p != nil && p.cancel != nil {
		p.cancel()
	}
}

// handleInterrupt runs the barge-in contract for THIS call only: drop the
// pipeline (generation bump + turn cancel), tell the binding, and send a
// clear frame on the text socket (the child flushes its TTS queue; the
// splice owns the Telnyx-side clear).
func (t *TalkLoop) handleInterrupt(ccid string) {
	t.cancelPipeline()
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
	t.cancelPipeline()
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
