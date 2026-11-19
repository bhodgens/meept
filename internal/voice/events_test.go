package voice

// Telemetry ring tests (voice-events tree): append/cursor/overflow/
// eviction on the ring itself, plus the TalkLoop OnEvent hook firing at
// each log site (and the nil-hook byte-identical posture).

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTelemetryRingAppendAndCursor(t *testing.T) {
	r := NewTelemetryRing(4, 8)
	for i := range 3 {
		seq := r.Append("cc-1", EventInput{Kind: EventKindDispatch, Text: "hello"})
		if seq != uint64(i+1) {
			t.Fatalf("seq = %d, want %d", seq, i+1)
		}
	}
	events, truncated, hasMore, found := r.Since("cc-1", 0, 0)
	if !found || truncated || hasMore {
		t.Fatalf("found=%v truncated=%v hasMore=%v, want true/false/false", found, truncated, hasMore)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	for i, ev := range events {
		if ev.Seq != uint64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
	// Cursor advances: only seq > cursor returns.
	events, _, _, _ = r.Since("cc-1", 2, 0)
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("cursor read = %+v, want [seq 3]", events)
	}
	// Fully consumed cursor -> empty list (found stays true).
	events, truncated, hasMore, found = r.Since("cc-1", 3, 0)
	if len(events) != 0 || truncated || hasMore || !found {
		t.Fatalf("drained read = %d events, truncated=%v hasMore=%v found=%v", len(events), truncated, hasMore, found)
	}
	// Unknown call -> not found, empty.
	events, _, _, found = r.Since("cc-nope", 0, 0)
	if found || len(events) != 0 {
		t.Fatalf("unknown call: found=%v events=%d", found, len(events))
	}
}

func TestTelemetryRingOverflowDropsOldest(t *testing.T) {
	r := NewTelemetryRing(4, 8)
	for range 10 {
		r.Append("cc-1", EventInput{Kind: EventKindHeard, Text: "t"})
	}
	// Full window: last 4 events retained (seqs 7..10).
	events, truncated, _, found := r.Since("cc-1", 0, 0)
	if !found {
		t.Fatal("ring vanished after overflow")
	}
	if !truncated {
		t.Fatal("cursor 0 after overflow must report truncated")
	}
	if len(events) != 4 || events[0].Seq != 7 || events[3].Seq != 10 {
		t.Fatalf("overflow window = %+v, want seqs 7..10", events)
	}
	// A cursor at the retained boundary is NOT truncated.
	_, truncated, _, _ = r.Since("cc-1", 6, 0)
	if truncated {
		t.Fatal("cursor 6 (droppedEnd) must not report truncated")
	}
	// A limit-capped read is NOT truncated (C-10): the rows are all
	// retained, the client just reads again - hasMore is the signal.
	var hasMore bool
	events, truncated, hasMore, _ = r.Since("cc-1", 6, 2)
	if len(events) != 2 || truncated || !hasMore {
		t.Fatalf("limited read = %d events truncated=%v hasMore=%v, want 2/false/true", len(events), truncated, hasMore)
	}
}

func TestTelemetryRingCallEviction(t *testing.T) {
	r := NewTelemetryRing(2, 2)
	r.Append("cc-1", EventInput{Kind: EventKindHeard})
	r.Append("cc-2", EventInput{Kind: EventKindHeard})
	r.Append("cc-3", EventInput{Kind: EventKindHeard}) // evicts cc-1
	if _, _, _, found := r.Since("cc-1", 0, 0); found {
		t.Fatal("cc-1 must be evicted once 2 newer calls exist")
	}
	if _, _, _, found := r.Since("cc-3", 0, 0); !found {
		t.Fatal("cc-3 must be retained")
	}
}

func TestTelemetryRingNilSafe(t *testing.T) {
	var r *TelemetryRing
	if seq := r.Append("cc", EventInput{}); seq != 0 {
		t.Fatalf("nil Append = %d, want 0", seq)
	}
	if _, _, _, found := r.Since("cc", 0, 0); found {
		t.Fatal("nil Since must report not-found")
	}
	if r.HasRing("cc") {
		t.Fatal("nil HasRing must report no ring")
	}
}

// recorderHook captures every OnEvent emission in order.
type recorderHook struct {
	mu     sync.Mutex
	events []EventInput
}

func (h *recorderHook) append(ccid string, in EventInput) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, in)
	return uint64(len(h.events))
}

func (h *recorderHook) all() []EventInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]EventInput, len(h.events))
	copy(out, h.events)
	return out
}

func TestTalkLoopOnEventFiresAtLogSites(t *testing.T) {
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "The report is ready."}
	hook := &recorderHook{}
	deps := TalkDeps{
		Supervisor: sup,
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s-1", ConversationID: "c-1"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		OnEvent:    hook.append,
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{
		FillerLines:  []string{"Hold on, let me check."},
		GreetingLine: "",
	})
	cancel, done := runLoop(t, loop, "cc-ev")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-ev", Event: EventPrompt, Text: "is it done?", Last: &last}

	waitFor(t, 2*time.Second, "no turn_final event", func() bool {
		for _, ev := range hook.all() {
			if ev.Kind == EventKindFinal {
				return true
			}
		}
		return false
	})

	var kinds []string
	// The per-phase kinds (stt_final / voice_play / model_final) and
	// EventKindSaid ride the same ring as additive markers. Filter them
	// out so the dispatch-first / turn_final-last ordering assertion
	// stays meaningful for the ORIGINAL lifecycle kinds, then pin the
	// phase events themselves below.
	var saidTexts []string
	var sttFinal, modelFinal, voicePlay []EventInput
	for _, ev := range hook.all() {
		switch ev.Kind {
		case EventKindSaid:
			saidTexts = append(saidTexts, ev.Text)
			continue
		case EventKindSttFinal:
			sttFinal = append(sttFinal, ev)
			continue
		case EventKindModelFinal:
			modelFinal = append(modelFinal, ev)
			continue
		case EventKindVoicePlay:
			voicePlay = append(voicePlay, ev)
			continue
		}
		kinds = append(kinds, ev.Kind)
	}
	// dispatch precedes final.
	if len(kinds) < 2 || kinds[0] != EventKindDispatch || kinds[len(kinds)-1] != EventKindFinal {
		t.Fatalf("event kinds = %v, want dispatch first, final last", kinds)
	}
	// Positive pins for the new per-phase events: each carries the FULL
	// text (not a preview) and a non-negative duration, and each fires
	// exactly once alongside its original-kind sibling.
	if len(sttFinal) != 1 || sttFinal[0].Text != "is it done?" || sttFinal[0].Ms < 0 {
		t.Errorf("stt_final events = %+v, want one with full prompt text + ms", sttFinal)
	}
	if len(modelFinal) != 1 || modelFinal[0].Text != "The report is ready." || modelFinal[0].Ms < 0 {
		t.Errorf("model_final events = %+v, want one with full reply text + ms", modelFinal)
	}
	if len(voicePlay) != 1 || voicePlay[0].Text != "The report is ready." || voicePlay[0].Ms < 0 {
		t.Errorf("voice_play events = %+v, want one with full spoken text + ms", voicePlay)
	}
	// Positive pin for the say() telemetry (ee465418): every spoken
	// sentence reaches the ring with its full text and word count.
	// The filler ("Hold on, let me check.") and the reply
	// ("The report is ready.") are both fact-free/supervised speech
	// through the same choke.
	wantSaid := map[string]bool{
		"Hold on, let me check.": false,
		"The report is ready.":   false,
	}
	for _, tx := range saidTexts {
		if _, ok := wantSaid[tx]; ok {
			wantSaid[tx] = true
		}
	}
	for tx, seen := range wantSaid {
		if !seen {
			t.Errorf("said events = %q, want one carrying %q", saidTexts, tx)
		}
	}
	for _, ev := range hook.all() {
		if ev.Kind == EventKindSaid && ev.Text == "The report is ready." {
			if ev.Words != 4 {
				t.Errorf("said reply words = %d, want 4", ev.Words)
			}
		}
	}
	// The dispatch carries the preview + words; the final the reply words.
	events := hook.all()
	var dispatch, final EventInput
	for _, ev := range events {
		if ev.Kind == EventKindDispatch && dispatch.Kind == "" {
			dispatch = ev
		}
		if ev.Kind == EventKindFinal {
			final = ev
		}
	}
	if dispatch.Words != 3 || dispatch.Text == "" {
		t.Errorf("dispatch payload = %+v, want words=3 + preview text", dispatch)
	}
	if final.Words == 0 || final.Ms < 0 {
		t.Errorf("final payload = %+v, want reply words + ms", final)
	}

	// Barge-in: an interrupt frame while a turn is in flight emits it.
	slow := &slowTurns{reply: "long", delay: 300 * time.Millisecond}
	loop2Hook := &recorderHook{}
	deps.Turns = slow
	deps.OnEvent = loop2Hook.append
	loop2 := NewTalkLoop(deps, TalkLoopConfig{})
	cancel2, done2 := runLoop(t, loop2, "cc-bar")
	defer func() { cancel2(); <-done2 }()
	loop2.frames <- Frame{CallID: "cc-bar", Event: EventPrompt, Text: "hello there", Last: &last}
	waitFor(t, 2*time.Second, "turn not dispatched", func() bool {
		for _, ev := range loop2Hook.all() {
			if ev.Kind == EventKindDispatch {
				return true
			}
		}
		return false
	})
	loop2.frames <- Frame{CallID: "cc-bar", Event: EventInterrupt}
	waitFor(t, 2*time.Second, "no barge_in event", func() bool {
		for _, ev := range loop2Hook.all() {
			if ev.Kind == EventKindBargeIn {
				return true
			}
		}
		return false
	})

	// Call-ended: an end frame fires it with the named reason.
	loop2.frames <- Frame{CallID: "cc-bar", Event: EventEnd, Reason: "user hangup"}
	waitFor(t, 2*time.Second, "no call_ended event", func() bool {
		for _, ev := range loop2Hook.all() {
			if ev.Kind == EventKindCallEnded && ev.Reason == "child end: user hangup" {
				return true
			}
		}
		return false
	})
}

func TestPerPhaseEventsCarryFullText(t *testing.T) {
	// The per-phase kinds must carry FULL text, never the 40-rune preview
	// the original prompt_dispatch/turn_final rows use. A >40-rune prompt
	// proves stt_final is not truncated; the model/play rows carry the
	// full reply. Driven through the real loop so the emit sites are
	// exercised, not a hand-built EventInput.
	const longPrompt = "please summarize the entire quarterly report for me right now"
	const reply = "The quarterly report shows steady growth across all regions."
	sink := &recordSink{}
	hook := &recorderHook{}
	deps := TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      &cannedTurns{reply: reply},
		SessionFor: constSession(SessionHandle{ID: "s-p", ConversationID: "c-p"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		OnEvent:    hook.append,
	}
	loop := NewTalkLoop(deps, TalkLoopConfig{})
	cancel, done := runLoop(t, loop, "cc-phase")
	defer func() { cancel(); <-done }()

	last := true
	loop.frames <- Frame{CallID: "cc-phase", Event: EventPrompt, Text: longPrompt, Last: &last}
	waitFor(t, 2*time.Second, "no final phase event", func() bool {
		for _, ev := range hook.all() {
			if ev.Kind == EventKindModelFinal {
				return true
			}
		}
		return false
	})

	want := map[string]string{
		EventKindSttFinal:   longPrompt,
		EventKindModelFinal: reply,
		EventKindVoicePlay:  reply,
	}
	counts := map[string]int{}
	for _, ev := range hook.all() {
		if w, ok := want[ev.Kind]; ok {
			counts[ev.Kind]++
			if ev.Text != w {
				t.Errorf("%s text = %q, want full %q", ev.Kind, ev.Text, w)
			}
			if ev.Ms < 0 {
				t.Errorf("%s ms = %d, want >= 0", ev.Kind, ev.Ms)
			}
		}
	}
	for k := range want {
		if counts[k] != 1 {
			t.Errorf("%s fired %d times, want 1", k, counts[k])
		}
	}
}

func TestTalkLoopNilOnEventIsNoop(t *testing.T) {
	// The byte-identical posture: a loop built WITHOUT OnEvent must run a
	// full dispatch->final turn without touching the seam (and without
	// panicking on the nil func).
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turns := &cannedTurns{reply: "Done."}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{})
	cancel, done := runLoop(t, loop, "cc-nil")
	defer cancel()

	last := true
	loop.frames <- Frame{CallID: "cc-nil", Event: EventPrompt, Text: "ready?", Last: &last}
	waitFor(t, 2*time.Second, "no reply say", func() bool {
		texts := sink.sayTexts()
		return slices.Contains(texts, "Done.")
	})
	cancel()
	<-done
}

// TestTelemetryRingTextCap pins bughunt 2026-11-19 L4: full reply text
// rides the ring, so stored Text is capped at maxEventTextBytes with a
// rune-safe cut + ellipsis marker; Seq/kind/Ms stay untouched and short
// text passes through byte-identical.
func TestTelemetryRingTextCap(t *testing.T) {
	r := NewTelemetryRing(0, 0)

	// Short text: byte-identical, no marker.
	r.Append("cc-short", EventInput{Kind: EventKindSaid, Text: "short reply", Ms: 7})
	evs, _, _, found := r.Since("cc-short", 0, 0)
	if !found || len(evs) != 1 {
		t.Fatalf("short append: found=%v evs=%d", found, len(evs))
	}
	if evs[0].Text != "short reply" || evs[0].Seq != 1 || evs[0].Kind != EventKindSaid || evs[0].Ms != 7 {
		t.Fatalf("short event mutated: %+v", evs[0])
	}

	// Exactly at the cap: still byte-identical (the cap is inclusive).
	atCap := strings.Repeat("a", maxEventTextBytes)
	r.Append("cc-atcap", EventInput{Kind: EventKindModelFinal, Text: atCap})
	evs, _, _, _ = r.Since("cc-atcap", 0, 0)
	if evs[0].Text != atCap {
		t.Fatal("text exactly at the cap must pass through unchanged")
	}

	// Long ASCII reply: capped, ellipsis-marked, fields untouched.
	long := strings.Repeat("b", maxEventTextBytes*3)
	r.Append("cc-long", EventInput{Kind: EventKindModelFinal, Text: long, Words: 99, Ms: 1234})
	evs, _, _, _ = r.Since("cc-long", 0, 0)
	got := evs[0].Text
	if len(got) > maxEventTextBytes {
		t.Fatalf("stored text %d bytes exceeds the %d cap", len(got), maxEventTextBytes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("capped text missing the ellipsis marker: tail=%q", got[len(got)-8:])
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Fatal("capped text is not a prefix of the original")
	}
	if evs[0].Seq != 1 || evs[0].Kind != EventKindModelFinal || evs[0].Words != 99 || evs[0].Ms != 1234 {
		t.Fatalf("cap touched non-Text fields: %+v", evs[0])
	}

	// Rune-safe cut: a multi-byte rune straddling the cut boundary must
	// never be split (the stored string stays valid UTF-8).
	multi := strings.Repeat("é", maxEventTextBytes) // 2 bytes/rune: boundary lands mid-rune
	r.Append("cc-multi", EventInput{Kind: EventKindSttFinal, Text: multi})
	evs, _, _, _ = r.Since("cc-multi", 0, 0)
	got = evs[0].Text
	if len(got) > maxEventTextBytes {
		t.Fatalf("multi-byte capped text %d bytes exceeds cap", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("rune-unsafe cut: stored text is not valid UTF-8")
	}
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(multi, strings.TrimSuffix(got, "…")) {
		t.Fatal("multi-byte capped text lost the marker or prefix property")
	}

	// A 4-byte rune at the boundary (emoji) is likewise never split.
	emoji := strings.Repeat("🎙", maxEventTextBytes)
	r.Append("cc-emoji", EventInput{Kind: EventKindHeard, Text: emoji})
	evs, _, _, _ = r.Since("cc-emoji", 0, 0)
	if !utf8.ValidString(evs[0].Text) || len(evs[0].Text) > maxEventTextBytes {
		t.Fatalf("emoji cut invalid: len=%d valid=%v", len(evs[0].Text), utf8.ValidString(evs[0].Text))
	}
}
