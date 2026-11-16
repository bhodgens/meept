package voice

// Audio-aware silence deferral pins (self-talk fix, call
// local-53b347f028f0a43f, defects S1+S2):
//
//	S1 - the TEXT-drain re-arm counted the S window down while the
//	     reply's AUDIO still paced (measured pacer stall gaps of
//	     1.3-3.8 s), so the nudge fired over the agent's own voice.
//	S2 - the loop-start window fired over the greeting's audio tail.
//
// The fix: at fire time, when the optional TalkDeps.AudioActive seam
// reports the call's own audio active inside the expiring window, the
// window RE-ARMS instead of speaking, bounded at
// silenceAudioDeferralCap consecutive deferrals. At the cap a still-active
// tail does not speak the nudge; the hangup ladder advances instead
// (TestTalkLoopSilenceCapSkipsNudgeAndHangsUp). These pins run at ms
// scale via silenceWindowUnit (same discipline as the silence pins file:
// no t.Parallel anywhere in the silence path). Emitted telemetry kinds
// and payloads (EventKindSilenceDeferred) are preserved.

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// countingAudioActive is a fake seam: reports audible for the first N
// consultations (or forever when always), counting the calls and the
// last window. Consultations are Run-goroutine-only (fire-time) and the
// mu makes the assertions race-safe under -race anyway.
type countingAudioActive struct {
	mu     sync.Mutex
	remain int // remaining "true" verdicts (ignored when always)
	always bool
	calls  int
	win    time.Duration
}

func (a *countingAudioActive) fn(ccid string, window time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.win = window
	if a.always {
		return true
	}
	if a.remain > 0 {
		a.remain--
		return true
	}
	return false
}

func (a *countingAudioActive) lastWindow() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.win
}

// audioTestLoop builds a talk loop with the AudioActive seam wired, ms
// silence windows, and the configured-line nudge (so a fire is
// unambiguous in the sink).
func audioTestLoop(t *testing.T, active func(string, time.Duration) bool, onEvent func(string, EventInput) uint64, sink *recordSink, capH *captureHandler, endReasons chan string) *TalkLoop {
	t.Helper()
	deps := TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      &cannedTurns{reply: "Back with an answer."},
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) { endReasons <- reason },
		Logger:     slog.New(capH),
	}
	if active != nil {
		deps.AudioActive = active
	}
	if onEvent != nil {
		deps.OnEvent = onEvent
	}
	return NewTalkLoop(deps, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      20,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 20,
	})
}

// countDeferred fires the INFO "silence window deferred" log per
// deferral; count the records for the cap pin.
func countDeferred(h *captureHandler) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if strings.Contains(r.Message, "silence window deferred") {
			n++
		}
	}
	return n
}

// Pin S2/S1 mechanism: ONE deferral, then the nudge fires. The seam says
// "audible" for the first window only; the second window (true silence)
// speaks the nudge. Asserts: (a) the nudge was NOT spoken on the first
// fire, (b) exactly one deferral log, (c) the deferral emitted a
// silence_deferred telemetry event with the running count, (d) the seam
// received the armed window (20 * unit) - not a guessed constant.
func TestTalkLoopSilenceDefersWhileAudioActive(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)
	var eventsMu sync.Mutex
	var events []EventInput
	active := &countingAudioActive{remain: 1} // first consult true, rest false

	loop := audioTestLoop(t, active.fn, func(_ string, in EventInput) uint64 {
		eventsMu.Lock()
		events = append(events, in)
		eventsMu.Unlock()
		return 1
	}, sink, capH, endReasons)

	cancel, done := runLoop(t, loop, "cc-defer")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "nudge never spoken after audio stopped", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?")
	})

	// Exactly one deferral preceded the fire.
	if got := countDeferred(capH); got != 1 {
		t.Errorf("deferred log count = %d, want 1", got)
	}
	// The nudge landed once (the deferral must not latch silencePrompted
	// or consume the line twice).
	nudges := 0
	for _, tx := range sink.sayTexts() {
		if tx == "Are you still there?" {
			nudges++
		}
	}
	if nudges != 1 {
		t.Errorf("nudge spoken %d times, want exactly 1", nudges)
	}
	// Telemetry: a silence_deferred event with reason "1" rides the
	// ring seam.
	eventsMu.Lock()
	defer eventsMu.Unlock()
	deferred := 0
	for _, in := range events {
		if in.Kind == EventKindSilenceDeferred {
			deferred++
			if in.Reason != "1" {
				t.Errorf("deferred reason = %q, want \"1\"", in.Reason)
			}
		}
	}
	if deferred != 1 {
		t.Errorf("silence_deferred events = %d, want 1 (%+v)", deferred, events)
	}
	// The seam saw the LIVE window (20 units), proving the re-arm keeps
	// the same duration (not S-hardcoded after an H window, not zero).
	if w := active.lastWindow(); w != 20*silenceWindowUnit {
		t.Errorf("seam window = %v, want %v", w, 20*silenceWindowUnit)
	}
}

// Cap pin: a PERMANENTLY audible stream cannot suppress hangup forever,
// and it must not be answered with the nudge. The seam always reports
// active; after exactly silenceAudioDeferralCap deferrals the ladder
// advances and the call hangs up with the nudge line never said.
func TestTalkLoopSilenceDeferralIsBounded(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)
	active := &countingAudioActive{always: true}

	loop := audioTestLoop(t, active.fn, nil, sink, capH, endReasons)

	cancel, done := runLoop(t, loop, "cc-defer-cap")
	defer func() { cancel(); <-done }()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want the silence hangup", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("audible stream never reached hangup despite the deferral cap")
	}
	if got := countDeferred(capH); got != silenceAudioDeferralCap {
		t.Errorf("deferred log count = %d, want exactly the cap (%d)", got, silenceAudioDeferralCap)
	}
	for _, tx := range sink.sayTexts() {
		if tx == "Are you still there?" {
			t.Errorf("nudge spoken over a permanently audible stream: %q", sink.sayTexts())
		}
	}
}

// Cap-while-audible pin (live call local-a7588dbe40dbe78c): the deferral
// cap must not speak the nudge over the agent's own audio. AudioActive
// stays true. After exactly silenceAudioDeferralCap deferrals the next
// fire advances the hangup ladder (no say of the nudge line) and the
// FOLLOWING fire hangs up. S and H differ so that following fire is
// observable: the seam's last window is H, consulted only after the
// ladder re-arms. A same-fire hangup would leave the seam on S.
func TestTalkLoopSilenceCapSkipsNudgeAndHangsUp(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)
	active := &countingAudioActive{always: true}

	const promptS = 20
	const hangupS = 35
	deps := TalkDeps{
		Supervisor:  &cannedSupervisor{},
		Turns:       &cannedTurns{reply: "Back with an answer."},
		SessionFor:  constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:        sink.Send,
		OnEnd:       func(ccid, reason string) { endReasons <- reason },
		Logger:      slog.New(capH),
		AudioActive: active.fn,
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      promptS,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: hangupS,
	})
	cancel, done := runLoop(t, loop, "cc-cap-skip")
	defer func() { cancel(); <-done }()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want the silence hangup", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("audible-at-cap stream never reached hangup")
	}
	for _, tx := range sink.sayTexts() {
		if tx == "Are you still there?" {
			t.Errorf("say called with the nudge over active agent audio: %q", sink.sayTexts())
		}
	}
	if got := countDeferred(capH); got != silenceAudioDeferralCap {
		t.Errorf("deferred log count = %d, want exactly the cap (%d); the following fire must hang up, not defer again", got, silenceAudioDeferralCap)
	}
	wantH := time.Duration(hangupS) * silenceWindowUnit
	if w := active.lastWindow(); w != wantH {
		t.Errorf("seam window at hangup = %v, want hangup window %v (following fire after the ladder re-arm)", w, wantH)
	}
}

// Byte-identical posture pin: with the seam NIL (phone legs, every
// legacy/test caller) the nudge fires on the FIRST window with zero
// deferral traffic, even while "audio" would be active. This is the
// non-link/local mirror rule: a nil seam changes nothing.
func TestTalkLoopSilenceNilSeamFiresImmediately(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)

	loop := audioTestLoop(t, nil, nil, sink, capH, endReasons)

	cancel, done := runLoop(t, loop, "cc-nil-seam")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "nudge never spoken with a nil seam", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?")
	})
	if got := countDeferred(capH); got != 0 {
		t.Errorf("nil seam produced %d deferral logs, want 0 (byte-identical posture)", got)
	}
	if len(sink.all()) == 0 {
		t.Fatal("sink recorded no say frames")
	}
}

// Deferral-streak reset pin: a REAL nudge (seam quiet, under the cap)
// resets the streak. The post-nudge hangup window then defers under a
// fresh streak while audio is active again, and still hangs up once
// that streak is spent (already prompted → existing hangup leg).
// Consult 2 must be quiet: a seam that is still active at the cap
// skips the nudge (see TestTalkLoopSilenceCapSkipsNudgeAndHangsUp)
// and would not exercise the reset.
func TestTalkLoopHangupWindowDefersAndStillHangsUp(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)
	consults := 0
	var mu sync.Mutex
	active := func(string, time.Duration) bool {
		mu.Lock()
		defer mu.Unlock()
		consults++
		return consults != 2
	}

	loop := audioTestLoop(t, active, nil, sink, capH, endReasons)

	cancel, done := runLoop(t, loop, "cc-hangup-defer")
	defer func() { cancel(); <-done }()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want the silence hangup", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("silence hangup never fired")
	}
	// One pre-nudge deferral plus a fresh H-window streak (at least one
	// more). The cap on that fresh streak is what reaches the hangup leg.
	if got := countDeferred(capH); got < 2 {
		t.Errorf("deferred logs = %d, want >= 2 (nudge-phase deferral plus a fresh H-window streak)", got)
	}
	if !strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?") {
		t.Error("nudge never spoken before the hangup")
	}
}

// Caller speech holds the nudge. AudioActive is false. CallerActive is
// true for the first fire only. The nudge must not speak on that fire,
// and the hold must not count as an agent-audio deferral.
func TestTalkLoopSilenceHoldsWhileCallerSpeaking(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 1)
	caller := &countingAudioActive{remain: 1}
	var eventsMu sync.Mutex
	var events []EventInput

	deps := TalkDeps{
		Supervisor:   &cannedSupervisor{},
		Turns:        &cannedTurns{reply: "Back with an answer."},
		SessionFor:   constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:         sink.Send,
		OnEnd:        func(ccid, reason string) { endReasons <- reason },
		Logger:       slog.New(capH),
		CallerActive: caller.fn,
		OnEvent: func(_ string, in EventInput) uint64 {
			eventsMu.Lock()
			events = append(events, in)
			eventsMu.Unlock()
			return 1
		},
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      20,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 20,
	})
	cancel, done := runLoop(t, loop, "cc-caller")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "nudge never spoken after caller went quiet", func() bool {
		return strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?")
	})
	if got := countDeferred(capH); got != 0 {
		t.Errorf("agent-audio deferrals = %d, want 0", got)
	}
	capH.mu.Lock()
	held := 0
	for _, r := range capH.records {
		if strings.Contains(r.Message, "caller speaking") {
			held++
		}
	}
	capH.mu.Unlock()
	if held != 1 {
		t.Errorf("caller holds = %d, want 1", held)
	}
	if caller.lastWindow() != silenceCallerLookback {
		t.Errorf("caller lookback = %s, want %s", caller.lastWindow(), silenceCallerLookback)
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, in := range events {
		if in.Kind == EventKindSilenceDeferred && in.Reason != "caller" {
			t.Errorf("hold reason = %q, want caller", in.Reason)
		}
	}
}

// callerHoldAudio is a fake CallerActive seam: true for the first N
// consultations (or forever when always). Fire-time-only, like
// countingAudioActive.
type callerHoldAudio struct {
	mu     sync.Mutex
	remain int
	always bool
	calls  int
}

func (c *callerHoldAudio) fn(ccid string, window time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.always {
		return true
	}
	if c.remain > 0 {
		c.remain--
		return true
	}
	return false
}

// callerHolds counts the "caller speaking" hold logs.
func callerHolds(h *captureHandler) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if strings.Contains(r.Message, "caller speaking") {
			n++
		}
	}
	return n
}

// V2 pin (a): the caller-hold branch must NOT rewind the escalation
// ladder. The caller is loud on EVERY fire; the nudge speaks once, the
// remaining fires burn callerDeferrals to silenceCallerDeferralCap, and
// the NEXT fire takes the hangup leg (bounded). Pre-fix this loop
// spun forever: each hold reset silencePrompted (so the nudge leg
// repeated) and zeroed silenceDeferrals, and the hangup was
// unreachable.
func TestTalkLoopCallerHoldEscalatesToHangup(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 16)
	caller := &callerHoldAudio{always: true}

	deps := TalkDeps{
		Supervisor:   &cannedSupervisor{},
		Turns:        &cannedTurns{reply: "Back with an answer."},
		SessionFor:   constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:         sink.Send,
		OnEnd:        func(ccid, reason string) { endReasons <- reason },
		Logger:       slog.New(capH),
		CallerActive: caller.fn,
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      20,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 20,
	})
	cancel, done := runLoop(t, loop, "cc-hold-esc")
	defer func() { cancel(); <-done }()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want the silence hangup", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("caller-hold escalation never reached the hangup leg (unbounded loop?)")
	}
	// The nudge spoke exactly once before the hangup (silencePrompted
	// was never rewound by a hold).
	nudges := 0
	for _, tx := range sink.sayTexts() {
		if tx == "Are you still there?" {
			nudges++
		}
	}
	if nudges != 1 {
		t.Errorf("nudge spoken %d times, want exactly 1 before hangup", nudges)
	}
	// Every fire under the sustained loud caller held. The ladder has
	// TWO hold runs: pre-nudge holds burn callerDeferrals to the cap
	// and the next fire takes the nudge leg; the nudge fall-through
	// resets callerDeferrals, so the post-nudge H-window holds burn to
	// the cap again and the next fire takes the hangup leg.
	// 2 x silenceCallerDeferralCap holds total, then hangup - bounded.
	if got := callerHolds(capH); got != 2*silenceCallerDeferralCap {
		t.Errorf("caller holds = %d, want exactly 2 x cap (%d)", got, 2*silenceCallerDeferralCap)
	}
}

// V2 pin (b): an ALTERNATING loud-mic / agent-audio-tail pattern must
// terminate. Pre-fix, each caller hold reset silencePrompted AND zeroed
// silenceDeferrals while the audio-deferral lane ran its own bounded
// streak, so the two lanes kept each other's progress markers rewound
// and BOTH counters stayed permanently under cap. Post-fix each lane
// keeps its accumulated escalation state, so the ladder reaches the
// hangup regardless of the alternation order. The seams share one
// parity counter: odd consult -> caller-loud, even consult ->
// agent-audio-tail (consults happen fire-time, caller lane first -
// the branch order in the loop).
func TestTalkLoopAlternatingCallerAudioTerminates(t *testing.T) {
	withMsSilenceUnit(t)
	sink := &recordSink{}
	capH := &captureHandler{}
	endReasons := make(chan string, 16)
	consult := 0
	var consultMu sync.Mutex
	callerSeam := func(ccid string, window time.Duration) bool {
		consultMu.Lock()
		defer consultMu.Unlock()
		consult++
		return consult%2 == 1
	}
	audioSeam := func(ccid string, window time.Duration) bool {
		consultMu.Lock()
		defer consultMu.Unlock()
		consult++
		return consult%2 == 0
	}

	deps := TalkDeps{
		Supervisor:   &cannedSupervisor{},
		Turns:        &cannedTurns{reply: "Back with an answer."},
		SessionFor:   constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:         sink.Send,
		OnEnd:        func(ccid, reason string) { endReasons <- reason },
		Logger:       slog.New(capH),
		CallerActive: callerSeam,
		AudioActive:  audioSeam,
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{
		GreetingLine:              "Welcome.",
		SilencePromptSeconds:      20,
		SilencePromptLine:         "Are you still there?",
		SilenceHangupAfterSeconds: 20,
	})
	cancel, done := runLoop(t, loop, "cc-alt")
	defer func() { cancel(); <-done }()

	select {
	case r := <-endReasons:
		if !strings.Contains(r, "silence") {
			t.Fatalf("end reason = %q, want the silence hangup", r)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("alternating loud-mic/audio-tail escalation never terminated")
	}
	// The call DID escalate: the nudge was spoken along the way.
	if !strings.Contains(strings.Join(sink.sayTexts(), "|"), "Are you still there?") {
		t.Error("nudge never spoken during the alternating escalation")
	}
}
