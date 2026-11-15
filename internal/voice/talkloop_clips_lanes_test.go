package voice

// Talk-loop lane wiring + clip_id stamping + R5 farewell rule (plan
// 20261113-filler-clips leaf 04). Pins:
//   - C5 lane pools drive the four resolvers (round-robin per lane with
//     its own cursor); zero-config leaves the scalar chains binding
//     byte-identically (the existing TestTalkLoop* suite is the broad
//     equivalence pin; this file pins the lane-shaped cases).
//   - Identity nil => NO clip_id anywhere; Identity set => every
//     filler-class say (greeting/filler/silence/fragment/fallback/
//     farewell) carries clipstore.ClipKey(line, vendor, model, voice,
//     refHash); reply sentences NEVER do (R6 — live content).
//   - R5: a whole-utterance bye speaks one farewell-lane line as a
//     last:true sayClip, skips the LLM turn, fires exactly once per
//     call; embedded-bye sentences are not farewells.
//   - Telemetry: the turn-filler INFO line gained lane/clip fields.

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/clipstore"
)

func clipsTestIdentity() *ClipIdentity {
	return &ClipIdentity{Vendor: "qwen3", Model: "Qwen/Qwen3-TTS-test", Voice: "vivian"}
}

// clipsGateLanes is the e2e-suite's clipGatePool shape resolved into lanes.
func clipsGateLanes() []LaneLine {
	return []LaneLine{
		{Lane: LaneGreeting, Text: "Hello there!"},
		{Lane: LaneFiller, Text: "One moment."},
		{Lane: LaneFiller, Text: "Just a sec."},
		{Lane: LaneSilence, Text: "Still there?"},
		{Lane: LaneFragment, Text: "Come again?"},
		{Lane: LaneFarewell, Text: "See ya!"},
	}
}

// CL-1: with Identity wired, greeting/filler says carry the exact C1
// clip key of their line; reply sentences never do (R6).
func TestTalkLoopClipIDSentinel(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "The router is up since Tuesday."}
	id := clipsTestIdentity()
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		Lanes:    clipsGateLanes(),
		Identity: id,
	})
	cancel, done := runLoop(t, loop, "cc-clip")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "greeting say", func() bool { return len(sink.says()) >= 1 })
	greet := sink.says()[0]
	if greet.Text != "Hello there!" {
		t.Fatalf("greeting line = %q, want lane line", greet.Text)
	}
	wantKey := clipstore.ClipKey("Hello there!", id.Vendor, id.Model, id.Voice, id.RefHash)
	if greet.ClipID != wantKey {
		t.Fatalf("greeting clip_id = %q, want C1 key %q", greet.ClipID, wantKey)
	}

	last := true
	loop.frames <- Frame{CallID: "cc-clip", Event: EventPrompt, Text: "how is the router", Last: &last}
	waitFor(t, 3*time.Second, "reply spoken", func() bool {
		for _, f := range sink.says() {
			if strings.Contains(f.Text, "router is up") {
				return true
			}
		}
		return false
	})
	for _, f := range sink.says() {
		if strings.Contains(f.Text, "router is up") {
			if f.ClipID != "" {
				t.Fatalf("reply sentence carried clip_id %q — live content must never (R6)", f.ClipID)
			}
		}
		if f.Text == "One moment." || f.Text == "Just a sec." {
			want := clipstore.ClipKey(f.Text, id.Vendor, id.Model, id.Voice, id.RefHash)
			if f.ClipID != want {
				t.Fatalf("filler %q clip_id = %q, want %q", f.Text, f.ClipID, want)
			}
		}
	}
}

// CL-2: Identity nil => no clip_id anywhere, even with lanes wired
// (byte-identical today's frames; omitempty keeps the field off the
// wire — asserted on the encoded shape too).
func TestTalkLoopNilIdentityNoClipID(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "All good."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		Lanes: clipsGateLanes(),
	})
	cancel, done := runLoop(t, loop, "cc-noid")
	defer func() { cancel(); <-done }()

	waitFor(t, 2*time.Second, "greeting say", func() bool { return len(sink.says()) >= 1 })
	last := true
	loop.frames <- Frame{CallID: "cc-noid", Event: EventPrompt, Text: "status?", Last: &last}
	waitFor(t, 3*time.Second, "reply spoken", func() bool { return len(sink.says()) >= 3 })
	for _, f := range sink.says() {
		if f.ClipID != "" {
			t.Fatalf("nil-identity say %q carried clip_id %q", f.Text, f.ClipID)
		}
		enc, err := EncodeFrame(f)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if strings.Contains(string(enc), "clip_id") {
			t.Fatalf("nil-identity frame carries the wire key: %s", enc)
		}
	}
}

// CL-3: per-lane round-robin with independent cursors: two fillers
// alternate while the greeting lane (cursorless) repeats its single
// line; the fragment lane round-robins independently of the filler
// cursor.
func TestTalkLoopLaneRoundRobin(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Working on it."}
	lanes := []LaneLine{
		{Lane: LaneGreeting, Text: "Hi."},
		{Lane: LaneFiller, Text: "F1."},
		{Lane: LaneFiller, Text: "F2."},
		{Lane: LaneFragment, Text: "C1?"},
		{Lane: LaneFragment, Text: "C2?"},
		{Lane: LaneFarewell, Text: "Bye-bye."},
	}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{Lanes: lanes})
	cancel, done := runLoop(t, loop, "cc-rr")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-rr", Event: EventPrompt, Text: "question one", Last: &last}
	waitFor(t, 3*time.Second, "first filler", func() bool {
		for _, f := range sink.says() {
			if f.Text == "F1." {
				return true
			}
		}
		return false
	})
	loop.frames <- Frame{CallID: "cc-rr", Event: EventPrompt, Text: "question two", Last: &last}
	waitFor(t, 3*time.Second, "second filler", func() bool {
		for _, f := range sink.says() {
			if f.Text == "F2." {
				return true
			}
		}
		return false
	})
	if got := sink.sayTexts()[0]; got != "Hi." {
		t.Fatalf("greeting = %q, want Hi.", got)
	}
}

// CL-4: the farewell rule — whole-utterance match only. Positives and
// negatives from the leaf doc, pinned exactly.
func TestTalkLoopIsFarewellUtterance(t *testing.T) {
	yes := []string{"bye", "Bye!", "goodbye", "GOODBYE.", " see ya ", "see you",
		"talk later", "later", "that's all", "see  ya!"}
	no := []string{"good one, now check X", "bye for now to the router issue",
		"ok then later we check the config", "", "   ", "byebye", "maybe later today?",
		"see you at the meeting tomorrow", "bye-bye"}
	for _, s := range yes {
		if !isFarewellUtterance(s) {
			t.Errorf("isFarewellUtterance(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isFarewellUtterance(s) {
			t.Errorf("isFarewellUtterance(%q) = true, want false", s)
		}
	}
}

// CL-5: R5 behavior — a bye prompt speaks the farewell lane line ONCE
// as a last:true clip-bearing say, runs NO LLM turn; a second bye
// speaks nothing more; an embedded bye still runs the turn.
func TestTalkLoopFarewellSpokenOnceLastTrue(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Sure, checking now."}
	id := clipsTestIdentity()
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		Lanes:    clipsGateLanes(),
		Identity: id,
	})
	cancel, done := runLoop(t, loop, "cc-fare")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-fare", Event: EventPrompt, Text: "bye", Last: &last}
	wantKey := clipstore.ClipKey("See ya!", id.Vendor, id.Model, id.Voice, id.RefHash)
	waitFor(t, 3*time.Second, "farewell say", func() bool {
		for _, f := range sink.says() {
			if f.Text == "See ya!" {
				return true
			}
		}
		return false
	})
	var farewells []Frame
	for _, f := range sink.says() {
		if f.Text == "See ya!" {
			farewells = append(farewells, f)
		}
	}
	if len(farewells) != 1 {
		t.Fatalf("farewell spoken %d times, want 1", len(farewells))
	}
	f := farewells[0]
	if f.Last == nil || !*f.Last {
		t.Fatalf("farewell say is not last:true — the child's played/drain guarantee needs it (C1)")
	}
	if f.ClipID != wantKey {
		t.Fatalf("farewell clip_id = %q, want %q", f.ClipID, wantKey)
	}

	// Second bye: exactly-once latch — no additional farewell.
	n := len(sink.says())
	loop.frames <- Frame{CallID: "cc-fare", Event: EventPrompt, Text: "goodbye", Last: &last}
	time.Sleep(150 * time.Millisecond)
	if len(sink.says()) != n {
		t.Fatalf("second bye spoke %d new frames, want 0 (exactly-once)", len(sink.says())-n)
	}
	if turns.started() != 0 {
		t.Fatalf("farewell rule ran an LLM turn (%d starts), want 0", turns.started())
	}

	// Embedded bye: NOT a farewell — the normal turn path runs.
	loop.frames <- Frame{CallID: "cc-fare", Event: EventPrompt, Text: "bye for now to the router issue", Last: &last}
	waitFor(t, 3*time.Second, "embedded-bye turn ran", func() bool { return turns.started() >= 1 })
	for _, f := range sink.says()[n:] {
		if f.Text == "See ya!" {
			t.Fatalf("embedded bye re-triggered the farewell line")
		}
	}
}

// started reads the canned turn runner's start count (the existing
// harness records it under its mutex).
func (ct *cannedTurns) started() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.starts
}

// CL-6: zero-config equivalence for the farewell rule — no farewell
// lane and no FarewellLines scalar => "bye" rides the normal turn path
// byte-identically (today's behavior; the rule must not fire from the
// built-in phrases alone).
func TestTalkLoopFarewellZeroConfigDisabled(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Talk soon then."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One sec."},
		GreetingLine: "Hi.",
	})
	cancel, done := runLoop(t, loop, "cc-fcz")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-fcz", Event: EventPrompt, Text: "bye", Last: &last}
	waitFor(t, 3*time.Second, "bye ran the turn", func() bool { return turns.started() >= 1 })
	for _, f := range sink.says() {
		if strings.Contains(strings.ToLower(f.Text), "goodbye") || strings.Contains(f.Text, "See ya!") {
			t.Fatalf("zero-config farewell line spoke (%q) — rule must never fire unset", f.Text)
		}
	}
}

// CL-7: denied-all-turns fallback is clip-tagged (fallback is
// filler-class, R5 list in the leaf doc) and last:true.
func TestTalkLoopFallbackCarriesClipID(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{"Nope.": false}}
	turns := &cannedTurns{reply: "Nope."}
	id := clipsTestIdentity()
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		Lanes:    clipsGateLanes(),
		Identity: id,
	})
	cancel, done := runLoop(t, loop, "cc-fb")
	defer func() { cancel(); <-done }()

	wantKey := clipstore.ClipKey(fallbackSentence, id.Vendor, id.Model, id.Voice, id.RefHash)
	waitFor(t, 2*time.Second, "greeting", func() bool { return len(sink.says()) >= 1 })
	last := true
	loop.frames <- Frame{CallID: "cc-fb", Event: EventPrompt, Text: "secret stuff", Last: &last}
	waitFor(t, 4*time.Second, "fallback spoken", func() bool {
		for _, f := range sink.says() {
			if f.Text == fallbackSentence {
				return true
			}
		}
		return false
	})
	for _, f := range sink.says() {
		if f.Text == fallbackSentence {
			if f.ClipID != wantKey {
				t.Fatalf("fallback clip_id = %q, want %q", f.ClipID, wantKey)
			}
			if f.Last == nil || !*f.Last {
				t.Fatalf("fallback must carry last:true")
			}
		}
	}
}

// CL-8: telemetry — the turn-filler INFO line gained lane/clip fields
// while keeping every existing key (smoke/gap scripts key off them).
func TestTalkLoopFillerLogLaneClip(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Answer here."}
	capH := &captureHandler{}
	id := clipsTestIdentity()
	loop := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     slog.New(capH),
	}, TalkLoopConfig{
		Lanes:    clipsGateLanes(),
		Identity: id,
	})
	cancel, done := runLoop(t, loop, "cc-tel")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-tel", Event: EventPrompt, Text: "question", Last: &last}
	waitFor(t, 4*time.Second, "filler telemetry", func() bool {
		_, ok := capH.find("voice talk loop: turn filler")
		return ok
	})
	rec, ok := capH.find("voice talk loop: turn filler")
	if !ok {
		t.Fatal("turn filler log missing")
	}
	for _, key := range []string{"ccid", "seq", "kind", "line", "after_ms", "lane", "clip"} {
		if _, found := attrValue(rec, key); !found {
			t.Errorf("turn filler log missing attr %q", key)
		}
	}
	if v, _ := attrValue(rec, "lane"); v != string(LaneFiller) {
		t.Errorf("lane attr = %v, want %q", v, LaneFiller)
	}
	if v, _ := attrValue(rec, "clip"); v != true {
		t.Errorf("clip attr = %v, want true (identity set)", v)
	}
}
