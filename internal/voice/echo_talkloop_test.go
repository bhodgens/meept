package voice

// Talk-loop pins for the CROSS-CALL ECHO CONTRACT (2026-11-11): the
// prompt-intake gate in Run's EventPrompt branch + the say-choke feed.
// Style follows talkloop_test.go (canned deps doubles). All fixtures use
// a closed-verse nonsense vocabulary (zibble/quorf/flim/glorp) so entries
// these tests leave in the PROCESS-WIDE crossCallEcho roster can never
// fuzzy-match another test's prompts — the package suite runs
// sequentially and shares the global roster; every fixture also clears
// its seeding ccid.

import (
	"slices"
	"testing"
	"time"
)

// echoSeed records agent speech for a DIFFERENT call (what call A's mic
// would capture as its own outbound audio).
func echoSeed(ccid, text string) { RecordSpoken(ccid, text) }

// echoTurnStarts counts cannedTurns dispatches (its starts field is the
// dispatch evidence; the lock is its own).
func echoTurnStarts(turns *cannedTurns) int {
	turns.mu.Lock()
	defer turns.mu.Unlock()
	return turns.starts
}

// TestTalkLoopEchoPromptDroppedCrossCall: a final prompt fuzzy-matching
// another live call's recorded speech is NOT dispatched; the following
// unrelated prompt IS dispatched. The drop is dispatch-only: the call
// keeps running (no end frame, no timer churn, partials unaffected).
func TestTalkLoopEchoPromptDroppedCrossCall(t *testing.T) {
	const said = "Quorf the zibble plow invoice, stap vinyl manifest before nun."
	echoSeed("cc-echo-sayer", said)
	t.Cleanup(func() { ClearSpoken("cc-echo-sayer") })

	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Flim flam sock puppet dances tuesday morning."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{})

	cancel, done := runLoop(t, loop, "cc-echo-listener")
	defer func() { cancel(); <-done }()

	last := true
	notLast := false
	// 1. Verbatim echo of call A's speech (case-insensitive; whisper
	//    strips punctuation) -> dropped, no turn, no reply say.
	loop.frames <- Frame{CallID: "cc-echo-listener", Event: EventPrompt,
		Text: "quorf the zibble plow invoice stap vinyl manifest before nun", Last: &last}
	// 2. A garbled echo (a couple of letters off) -> dropped too.
	loop.frames <- Frame{CallID: "cc-echo-listener", Event: EventPrompt,
		Text: "quorf the zibble plow invoiice stap vinyl manifst before nun", Last: &last}
	// 3. A partial of the echo (last:false) -> never a turn anyway.
	loop.frames <- Frame{CallID: "cc-echo-listener", Event: EventPrompt,
		Text: "quorf the zibble plow invoice", Last: &notLast}

	// Give the loop generous time to (not) dispatch.
	time.Sleep(200 * time.Millisecond)
	if got := echoTurnStarts(turns); got != 0 {
		t.Fatalf("echo prompts dispatched %d turns, want 0 (cross-call echo must drop)", got)
	}
	if texts := sink.sayTexts(); len(texts) != 0 {
		t.Fatalf("echo drop emitted says %v, want none", texts)
	}

	// 4. Unrelated prompt -> dispatches normally (the gate is not a mute).
	loop.frames <- Frame{CallID: "cc-echo-listener", Event: EventPrompt,
		Text: "glorp the nubbin wrench, calibrate sprocket axis tonight", Last: &last}
	waitFor(t, 2*time.Second, "unrelated prompt never dispatched", func() bool {
		return echoTurnStarts(turns) >= 1
	})
	waitFor(t, 2*time.Second, "unrelated prompt reply never spoken", func() bool {
		return slices.Contains(sink.sayTexts(), "Flim flam sock puppet dances tuesday morning.")
	})
	// The call survived the drops (the gate tore nothing down).
	for _, f := range sink.all() {
		if f.Event == EventEnd {
			t.Fatalf("echo drop tore the call down: %v", f)
		}
	}
}

// TestTalkLoopOwnSpeechNeverGatesSelf pins rule 2 end-to-end: the speech
// THIS loop says (greeting/filler/reply all ride the roster under its own
// ccid) never gates its own prompts — a later final that repeats the
// agent's own reply verbatim still dispatches on the same call (self-echo
// is the child-side window's contract, not the roster's).
func TestTalkLoopOwnSpeechNeverGatesSelf(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	reply := "Zibble quorf the plow invoice, stap vinyl manifest before nun."
	turns := &cannedTurns{reply: reply}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{})

	cancel, done := runLoop(t, loop, "cc-echo-self")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-echo-self", Event: EventPrompt,
		Text: "glorp the nubbin wrench axis tonight", Last: &last}
	waitFor(t, 2*time.Second, "first turn never dispatched", func() bool {
		return echoTurnStarts(turns) == 1 && slices.Contains(sink.sayTexts(), reply)
	})

	// Now a prompt that repeats the agent's OWN reply verbatim — the
	// roster must skip cc-echo-self's own entries and dispatch.
	loop.frames <- Frame{CallID: "cc-echo-self", Event: EventPrompt,
		Text: "zibble quorf the plow invoice stap vinyl manifest before nun", Last: &last}
	waitFor(t, 2*time.Second, "own-speech echo prompt never dispatched (roster must exclude same-ccid)", func() bool {
		return echoTurnStarts(turns) == 2
	})
}

// TestTalkLoopSayFramesFeedRoster pins the feed: greeting, filler, and
// reply says all land in the cross-call roster, and a DIFFERENT call's
// matching prompt gets gated while an unrelated one does not.
func TestTalkLoopSayFramesFeedRoster(t *testing.T) {
	greeting := "Quorf, the zibble plow invoice stap ready."
	filler := "Vinyl manifest before nun flurbs quarx now."
	reply := "Flim flam sock puppet dances tuesday morning."

	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: reply}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{filler},
		GreetingLine: greeting,
	})

	cancel, done := runLoop(t, loop, "cc-echo-feed")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-echo-feed", Event: EventPrompt,
		Text: "glorp the nubbin wrench, calibrate axis tonight", Last: &last}
	waitFor(t, 2*time.Second, "greeting+filler+reply never all said", func() bool {
		texts := sink.sayTexts()
		return slices.Contains(texts, greeting) &&
			slices.Contains(texts, filler) &&
			slices.Contains(texts, reply)
	})

	// Every spoken line now gates OTHER calls' prompts (an echo of any
	// of the three speech classes riding call X's mic).
	if !RecentOtherSpeech("cc-outsider", "quorf the zibble plow invoice stap ready") {
		t.Error("greeting speech did not feed the roster")
	}
	if !RecentOtherSpeech("cc-outsider", "vinyl manifest before nun flurbs quarx now") {
		t.Error("filler speech did not feed the roster")
	}
	if !RecentOtherSpeech("cc-outsider", "flim flam sock puppet dances tuesday morning") {
		t.Error("reply speech did not feed the roster")
	}
	// ...and never its own.
	if RecentOtherSpeech("cc-echo-feed", "flim flam sock puppet dances tuesday morning") {
		t.Error("own-call speech must never gate own prompts")
	}
	// Unrelated text stays clean.
	if RecentOtherSpeech("cc-outsider", "the baker queued forty crates of engine oil") {
		t.Error("unrelated prompt must not gate")
	}
}

// TestTalkLoopCallEndClearsRoster pins teardown: the loop's end() drops
// its own ccid's entries (hygiene; TTL would age them anyway).
func TestTalkLoopCallEndClearsRoster(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Zibble quorf the plow invoice, stap vinyl manifest now."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		GreetingLine: "Quorf, the zibble plow invoice stap ready.",
	})

	ctx := t.Context()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := loop.Run(ctx, Instance{CallControlID: "cc-echo-teardown"}); err != nil {
			t.Logf("talk loop ended: %v", err)
		}
	}()

	waitFor(t, 2*time.Second, "greeting say never recorded", func() bool {
		return len(sink.sayTexts()) >= 1
	})
	if !RecentOtherSpeech("cc-outsider", "quorf the zibble plow invoice stap ready") {
		t.Fatal("greeting should gate other calls while the call is live")
	}

	// End frame -> teardown -> ClearSpoken for this ccid.
	loop.frames <- Frame{CallID: "cc-echo-teardown", Event: EventEnd, Reason: "bye"}
	<-done

	if RecentOtherSpeech("cc-outsider", "quorf the zibble plow invoice stap ready") {
		t.Fatal("ended call's speech must be cleared from the roster at teardown")
	}
}

// TestTalkLoopLinkCallsExemptFromEchoGate pins contract rule 2's link
// exemption: a link-side loop (HoldPrompt seam present — the only loops
// the daemon ever wires it on) dispatches a prompt that verbatim echoes
// the peer's recorded speech. The half-duplex seed IS A's greeting
// arriving in B's mic; gating it would break the documented link
// turn-taking contract.
func TestTalkLoopLinkCallsExemptFromEchoGate(t *testing.T) {
	const said = "Quorf the zibble plow invoice, stap vinyl manifest before nun."
	echoSeed("cc-link-peer", said)
	t.Cleanup(func() { ClearSpoken("cc-link-peer") })

	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Flim."}
	hold := &fakeHold{}
	hold.allow() // the link coordinator grants the floor
	loop := newHoldTalkLoop(t, sink, sup, turns, hold.hold, testLogger(), TalkLoopConfig{})

	cancel, done := runLoop(t, loop, "cc-link-side")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-link-side", Event: EventPrompt,
		Text: "quorf the zibble plow invoice stap vinyl manifest before nun", Last: &last}
	waitFor(t, 2*time.Second, "link-side echo prompt never dispatched (link must be exempt)", func() bool {
		return echoTurnStarts(turns) == 1
	})
}
