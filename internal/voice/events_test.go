package voice

// Telemetry ring tests (voice-events tree): append/cursor/overflow/
// eviction on the ring itself, plus the TalkLoop OnEvent hook firing at
// each log site (and the nil-hook byte-identical posture).

import (
	"slices"
	"sync"
	"testing"
	"time"
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
	// EventKindSaid rides the same ring (the say() choke emits one per
	// spoken sentence: filler, reply, final flush excluded) - it is a
	// FEATURE marker, not an ordering event. Filter it out so the
	// dispatch-first / final-last ordering assertion stays meaningful,
	// then pin the said events themselves below.
	var saidTexts []string
	for _, ev := range hook.all() {
		if ev.Kind == EventKindSaid {
			saidTexts = append(saidTexts, ev.Text)
			continue
		}
		kinds = append(kinds, ev.Kind)
	}
	// dispatch precedes final.
	if len(kinds) < 2 || kinds[0] != EventKindDispatch || kinds[len(kinds)-1] != EventKindFinal {
		t.Fatalf("event kinds = %v, want dispatch first, final last", kinds)
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
	var dispatch EventInput
	for _, ev := range events {
		if ev.Kind == EventKindDispatch {
			dispatch = ev
			break
		}
	}
	if dispatch.Words != 3 || dispatch.Text == "" {
		t.Errorf("dispatch payload = %+v, want words=3 + preview text", dispatch)
	}
	final := events[len(events)-1]
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
