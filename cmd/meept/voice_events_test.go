package main

// voice_events_test.go - unit tests for the operator-telemetry CLI layer
// (voice-events tree): the pure event renderer and the poller's
// cursor/print behaviour against a fake RPC client. No daemon, no sockets.
// Pins: cursor echo (C-9), found=false feed end (C-20), bounded round-trip
// + no print after stop (C-11), truncation prints only on genuine
// eviction (C-10).

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

func TestVoiceEventRenderKinds(t *testing.T) {
	cases := []struct {
		ev   voiceEventRow
		want string
	}{
		{voiceEventRow{Kind: "heard", At: "2026-11-11T10:00:01.5Z", Text: "is it done?"},
			"10:00:01 heard: is it done?"},
		{voiceEventRow{Kind: "prompt_dispatch", At: "2026-11-11T10:00:02Z", Text: "is it done?", Words: 3},
			"10:00:02 thinking (3 words): is it done?"},
		{voiceEventRow{Kind: "turn_final", At: "2026-11-11T10:00:05Z", Words: 7, Ms: 3100},
			"10:00:05 replied (7 words, 3100ms)"},
		{voiceEventRow{Kind: "barge_in", At: "2026-11-11T10:00:06Z"},
			"10:00:06 >> barge-in"},
		{voiceEventRow{Kind: "call_ended", At: "2026-11-11T10:00:07Z", Reason: "hangup"},
			"10:00:07 call ended: hangup"},
		{voiceEventRow{Kind: "silence_deferred", At: "2026-11-11T10:00:08Z", Reason: "1"},
			"10:00:08 silence deferred (agent audio playing, x1)"},
		{voiceEventRow{Kind: "silence_deferred", At: "2026-11-11T10:00:08Z", Reason: "caller"},
			"10:00:08 silence held (caller speaking)"},
		{voiceEventRow{Kind: "said", At: "2026-11-11T10:00:04Z", Text: "Are you still there?"},
			"10:00:04 said: Are you still there?"},
		// Per-phase kinds render full text + ms, never a bare kind line.
		{voiceEventRow{Kind: "stt_final", At: "2026-11-11T10:00:02Z", Text: "Hello?", Ms: 40},
			"10:00:02 stt: Hello? (40ms)"},
		{voiceEventRow{Kind: "model_final", At: "2026-11-11T10:00:05Z", Text: "How can I help?", Words: 4, Ms: 771},
			"10:00:05 model: How can I help? (771ms)"},
		{voiceEventRow{Kind: "voice_play", At: "2026-11-11T10:00:05Z", Text: "How can I help?", Ms: 812},
			"10:00:05 voice: How can I help? (sent, 812ms)"},
		{voiceEventRow{Kind: "stt_final", At: "2026-11-11T10:00:02Z", Text: ""},
			"10:00:02 stt: (no text) (0ms)"},
		{voiceEventRow{Kind: "prompt_held", At: "2026-11-11T10:00:08Z", Text: "next question"},
			"10:00:08 held (peer speaking): next question"},
		{voiceEventRow{Kind: "prompt_released", At: "2026-11-11T10:00:09Z", Text: "next question"},
			"10:00:09 released: next question"},
		// Unknown kinds pass through the raw kind (future-proof).
		{voiceEventRow{Kind: "floor_released", At: "short"}, "short floor_released"},
	}
	for _, c := range cases {
		if got := voiceEventRender(c.ev); got != c.want {
			t.Errorf("render(%s) = %q, want %q", c.ev.Kind, got, c.want)
		}
	}
}

// transcriptFakeClient answers voice.local.transcript with stored lines.
type transcriptFakeClient struct {
	payload json.RawMessage
	err     error
}

func (c transcriptFakeClient) Call(_ string, _ any) (json.RawMessage, error) {
	return c.payload, c.err
}

// End-of-call transcript pin (call local-53b347f028f0a43f): the local
// CLI's end path prints the call's role-tagged transcript lines from
// voice.local.transcript — the user explicitly asked for the transcript
// back after the live-feed regression.
func TestPrintLocalCallTranscriptEmitsRoleLines(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"call_id": "local-x", "employee_id": "companion", "session_id": "s-1",
		"ended_at": "2026-11-11T21:39:20Z",
		"lines": []map[string]any{
			{"role": "user", "text": " how are you ", "at": "2026-11-11T21:39:08Z"},
			{"role": "assistant", "text": "Doing well.", "at": "2026-11-11T21:39:13Z"},
			{"role": "assistant", "text": "Anything else?", "at": "2026-11-11T21:39:15Z"},
		},
		"found": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		printLocalCallTranscript(transcriptFakeClient{payload: payload}, "local-x", "companion")
	})
	for _, want := range []string{
		"--- transcript (companion / local-x): 1 user / 2 assistant ---",
		// Each line renders a start→stop window: next line's start for all
		// but the last, the call's ended_at for the final line.
		"21:39:08→21:39:13 user: how are you",
		"21:39:13→21:39:15 assistant: Doing well.",
		"21:39:15→21:39:20 assistant: Anything else?",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript print missing %q in:\n%s", want, out)
		}
	}
}

// The final line without an ended_at (old daemon / live call) renders
// start-only - the window never fabricates a stop it does not have.
func TestPrintLocalCallTranscriptFinalLineStartOnlyWithoutEndedAt(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"call_id": "local-x", "employee_id": "companion", "session_id": "s-1",
		"lines": []map[string]any{
			{"role": "user", "text": "hi", "at": "2026-11-11T21:39:08Z"},
			{"role": "assistant", "text": "hello", "at": "2026-11-11T21:39:09Z"},
		},
		"found": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		printLocalCallTranscript(transcriptFakeClient{payload: payload}, "local-x", "companion")
	})
	for _, want := range []string{
		"21:39:08→21:39:09 user: hi",
		"21:39:09 assistant: hello", // no stop: start-only, no arrow after
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript print missing %q in:\n%s", want, out)
		}
	}
	// The brief flush-race window degrades to start-only rather than
	// fabricating a bad stop.
	if strings.Contains(out, "→") && strings.Count(out, "→") != 1 {
		t.Fatalf("expected exactly one arrow (one bounded line), got:\n%s", out)
	}
}

// callSeqClient answers voice.local.transcript with scripted payloads in
// order, serving the last one forever (the bounded retry's convergence).
type callSeqClient struct {
	responses []json.RawMessage
	served    int
}

func (c *callSeqClient) Call(_ string, _ any) (json.RawMessage, error) {
	idx := c.served
	if idx >= len(c.responses) {
		idx = len(c.responses) - 1
	}
	c.served++
	return c.responses[idx], nil
}

// The transcript read absorbs the end/flush race with a short retry: a
// found row that first returns empty lines resolves on a later attempt.
func TestPrintLocalCallTranscriptRetriesFlushRace(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"call_id": "local-race", "employee_id": "companion", "session_id": "s-1",
		"lines": []map[string]any{
			{"role": "user", "text": "hello", "at": "2026-11-11T21:39:08Z"},
			{"role": "assistant", "text": "hi", "at": "2026-11-11T21:39:09Z"},
		},
		"found": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := json.Marshal(map[string]any{
		"call_id": "local-race", "employee_id": "companion", "session_id": "s-1",
		"lines": []any{}, "found": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &callSeqClient{responses: []json.RawMessage{empty, empty, payload}}
	out := captureStdout(t, func() {
		printLocalCallTranscript(client, "local-race", "companion")
	})
	if !strings.Contains(out, "21:39:08→21:39:09 user: hello") {
		t.Fatalf("flush-race retry must converge to the full transcript, got:\n%s", out)
	}
}

// Best-effort degrade pins: an RPC failure (e.g. an old daemon without
// the method) and a found=false empty envelope print the honest "no
// transcript lines" header and NOTHING else - never a crash, never a
// change to the run's outcome.
func TestPrintLocalCallTranscriptDegradesQuietly(t *testing.T) {
	out := captureStdout(t, func() {
		printLocalCallTranscript(transcriptFakeClient{err: rpcStubError{}}, "local-y", "emp")
	})
	if !strings.Contains(out, "(no transcript lines)") {
		t.Fatalf("RPC-error path must print the honest degrade:\n%s", out)
	}
	notFound, err := json.Marshal(map[string]any{"call_id": "local-y", "lines": []any{}, "found": false})
	if err != nil {
		t.Fatal(err)
	}
	out2 := captureStdout(t, func() {
		printLocalCallTranscript(transcriptFakeClient{payload: notFound}, "local-y", "emp")
	})
	if !strings.Contains(out2, "(no transcript lines)") {
		t.Fatalf("found=false path must print the honest degrade:\n%s", out2)
	}
}

// fakeEventsClient serves scripted voice.local.events envelopes.
type fakeEventsClient struct {
	mu      sync.Mutex
	served  int // events delivered so far
	rows    []voiceEventRow
	errFunc func(n int) error
	calls   int
	delay   time.Duration
}

func (f *fakeEventsClient) Call(_ string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls++
	if f.errFunc != nil {
		if err := f.errFunc(f.calls); err != nil {
			f.mu.Unlock()
			return nil, err
		}
	}
	if f.delay > 0 {
		d := f.delay
		f.mu.Unlock()
		time.Sleep(d)
		f.mu.Lock()
	}
	p, _ := params.(map[string]any)
	cursor, _ := p["cursor"].(uint64)
	var out []voiceEventRow
	for _, ev := range f.rows {
		if ev.Seq > cursor {
			out = append(out, ev)
			f.served++
		}
	}
	f.mu.Unlock()
	var cur = cursor
	for _, ev := range out {
		if ev.Seq > cur {
			cur = ev.Seq
		}
	}
	b, err := json.Marshal(map[string]any{
		"call_id": "cc-1", "events": out, "cursor": cur, "found": true,
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func TestVoiceEventTrackerDrain(t *testing.T) {
	rows := []voiceEventRow{
		{Seq: 1, Kind: "heard", Text: "a"},
		{Seq: 2, Kind: "prompt_dispatch", Text: "a", Words: 1},
	}
	f := &fakeEventsClient{rows: rows}
	tr := newVoiceEventTracker(f, map[string][2]string{"cc-1": {"a", "companion"}})
	if !tr.pollOnce("cc-1", "a", "companion") {
		t.Fatal("first poll must keep the feed open")
	}
	if f.served != 2 {
		t.Fatalf("served = %d, want 2 events delivered", f.served)
	}
	// Second poll: cursor advanced (C-9 echo), nothing new, feed stays.
	if !tr.pollOnce("cc-1", "a", "companion") {
		t.Fatal("second poll (nothing new) must NOT end the feed")
	}
	tr.drainOnce() // must terminate and not re-print
	if tr.cursors["cc-1"] == 0 {
		t.Fatal("cursor did not advance")
	}
}

// rpcStubError is a transient RPC failure (the best-effort retry posture).
type rpcStubError struct{}

func (rpcStubError) Error() string { return "rpc: connection closed" }

func TestVoiceEventTrackerBestEffortOnErrors(t *testing.T) {
	f := &fakeEventsClient{
		rows: []voiceEventRow{{Seq: 1, Kind: "barge_in"}},
	}
	f.errFunc = func(n int) error {
		if n == 1 {
			return rpcStubError{}
		}
		return nil
	}
	tr := newVoiceEventTracker(f, map[string][2]string{"cc-1": {"b", "secretary"}})
	// An errored poll is best-effort: it returns true (retry next tick),
	// never terminates the feed.
	if !tr.pollOnce("cc-1", "b", "secretary") {
		t.Fatal("errored poll must be retried, not end the feed")
	}
	if !tr.pollOnce("cc-1", "b", "secretary") {
		t.Fatal("retry must keep the feed open")
	}
}

// C-20(2): a found=false answer for a ccid the poller already tracked
// ends the poller for that ccid (one line, then stop).
func TestVoiceEventTrackerFoundFalseEndsFeed(t *testing.T) {
	f2 := &foundFalseClient{}
	tr2 := newVoiceEventTracker(f2, map[string][2]string{"cc-2": {"a", "companion"}})
	tr2.cursors["cc-2"] = 3 // had consumed events before eviction
	if tr2.pollOnce("cc-2", "a", "companion") {
		t.Fatal("found=false at cursor > 0 must end the poller for that ccid")
	}
	if !tr2.ended["cc-2"] {
		t.Fatal("found=false must mark the ccid ended")
	}
	// The drain must not re-poll an ended ccid.
	tr2.drainOnce()
	if f2.calls != 1 {
		t.Fatalf("drain re-polled an ended ccid: %d calls, want 1", f2.calls)
	}
}

// REGRESSION pin (call local-53b347f028f0a43f): found=false at cursor 0
// is the ring's LAZY-creation startup race (the first Append that creates
// the ring lands with the first final prompt, seconds after the poller's
// first tick). It must NOT end the poller: fcaa2bab's C-20 treated it as
// feed-end and permanently silenced the live operator feed - the user saw
// only the "call open" line. The poller keeps ticking; a later poll that
// DOES see events still prints them.
func TestVoiceEventTrackerFoundFalseAtZeroKeepsPolling(t *testing.T) {
	f := &foundFalseClient{}
	tr := newVoiceEventTracker(f, map[string][2]string{"cc-3": {"a", "emp"}})
	if !tr.pollOnce("cc-3", "a", "emp") {
		t.Fatal("found=false at cursor 0 must NOT end the live poller (ring-lazy-creation race)")
	}
	if tr.ended["cc-3"] {
		t.Fatal("cursor-0 found=false must not mark the ccid ended")
	}
	// And when the ring DOES come alive, events print (the feed works).
	rows := &fakeEventsClient{rows: []voiceEventRow{
		{Seq: 1, Kind: "heard", Text: "hello?"},
	}}
	tr2 := newVoiceEventTracker(rows, map[string][2]string{"cc-3": {"a", "emp"}})
	out := captureStdout(t, func() {
		if !tr2.pollOnce("cc-3", "a", "emp") {
			t.Fatal("live poll must keep the feed")
		}
	})
	if !strings.Contains(out, "heard: hello?") {
		t.Fatalf("live feed printed no heard line: %q", out)
	}
	// Drain-side parity: the drain at cursor 0 DOES terminate quietly
	// (the live loop is over; the drain must not spin).
	tr3 := newVoiceEventTracker(&foundFalseClient{}, map[string][2]string{"cc-4": {"a", "emp"}})
	if tr3.drainPollOnce("cc-4", "a", "emp") {
		t.Fatal("drain must terminate on found=false even at cursor 0")
	}
	if !tr3.ended["cc-4"] {
		t.Fatal("drain found=false must mark the ccid ended")
	}
}

// foundFalseClient always answers found=false with a cursor echo (C-9).
type foundFalseClient struct{ calls int }

func (c *foundFalseClient) Call(_ string, params any) (json.RawMessage, error) {
	c.calls++
	p, _ := params.(map[string]any)
	cursor, _ := p["cursor"].(uint64)
	b, err := json.Marshal(map[string]any{
		"call_id": "cc-2", "events": []voiceEventRow{}, "cursor": cursor, "found": false,
	})
	return b, err
}

// C-11: stop() cannot hang on a wedged socket - the in-flight round-trip
// is abandoned at voiceEventCallTimeout and nothing prints after stop.
func TestVoiceEventTrackerStopIsBounded(t *testing.T) {
	slow := &slowEventsClient{delay: 30 * voiceEventCallTimeout} // way past the bound
	tr := newVoiceEventTracker(slow, map[string][2]string{"cc-1": {"a", "companion"}})
	ctx := context.Background()
	tr.start(ctx)
	start := time.Now()
	tr.stop()
	elapsed := time.Since(start)
	if elapsed > voiceEventCallTimeout+2*time.Second {
		t.Fatalf("stop() took %s; the bounded round-trip must cap it near %s", elapsed, voiceEventCallTimeout)
	}
	if !tr.stopped {
		t.Fatal("stop must mark the tracker stopped")
	}
}

// slowEventsClient blocks every Call for delay before answering found=true.
type slowEventsClient struct{ delay time.Duration }

func (c *slowEventsClient) Call(_ string, _ any) (json.RawMessage, error) {
	time.Sleep(c.delay)
	b, _ := json.Marshal(map[string]any{
		"call_id": "cc-1", "events": []voiceEventRow{}, "cursor": uint64(0), "found": true,
	})
	return b, nil
}

// C-9 (client side): the fake echoes the request cursor, so a poll with
// nothing new keeps cursor stable - advance-by-assignment is safe.
func TestVoiceEventCursorEchoAdvanceByAssignment(t *testing.T) {
	f := &fakeEventsClient{rows: []voiceEventRow{{Seq: 9, Kind: "heard", Text: "q"}}}
	tr := newVoiceEventTracker(f, map[string][2]string{"cc-1": {"a", "emp"}})
	if !tr.pollOnce("cc-1", "a", "emp") {
		t.Fatal("poll must succeed")
	}
	if tr.cursors["cc-1"] != 9 {
		t.Fatalf("cursor = %d, want 9 (advanced by assignment)", tr.cursors["cc-1"])
	}
	// C-10 (client side): a found=true, nothing-new answer must NOT print
	// the truncation note; only truncated=true (genuine eviction) does.
	if printed := captureStdout(t, func() { _ = tr.pollOnce("cc-1", "a", "emp") }); strings.Contains(printed, "truncated") {
		t.Fatalf("healthy empty poll printed a truncation note: %q", printed)
	}
}

// V4 pin (bughunt 2026-11-15 item 6): a very short reply whose said
// event arrives while no speaker window is open (the audio fully flushes
// below the bound before EOF, so Pending()==0 and noteOn never fires)
// must still print the said line at the EOF flush-complete edge - the
// stored transcript has it, the live feed used to silently drop it.
func TestSpeakerClockShortReplySaidPrintsAtEOF(t *testing.T) {
	clock := newSpeakerClock("local", "companion")
	// The said arrives BEFORE any audio is played: the clock parks it.
	clock.noteSaid("Sure.")
	if !clockHasWait(clock) {
		t.Fatal("said before the window must wait in the clock")
	}
	// Short-reply EOF shape: flushComplete runs with Pending()==0 and no
	// noteOn ever fired - the queued said must print, unstyled by any
	// speaker on/off window (none ever existed).
	out := captureStdout(t, func() {
		clock.endStream()
		clock.noteOff()
	})
	if !strings.Contains(out, "said: Sure.") {
		t.Fatalf("flush-complete edge dropped the queued said line:\n%s", out)
	}
	if strings.Contains(out, "speaker on") || strings.Contains(out, "speaker off") {
		t.Fatalf("no window ever opened; the edge must not fabricate one:\n%s", out)
	}
	if clockHasWait(clock) {
		t.Fatal("endStream must drain the wait queue")
	}
	// A second endStream (no wait left) prints nothing more.
	out2 := captureStdout(t, func() {
		clock.endStream()
	})
	if strings.TrimSpace(out2) != "" {
		t.Fatalf("second endStream re-printed: %q", out2)
	}
}

// V4 pin, normal path: the multi-chunk path still joins said with the
// speaker on/off markers in order - said arrives, noteOn joins it under
// "speaker on", noteOff closes the window. The queued said line carries
// the window's start stamp.
func TestSpeakerClockNormalPathJoinsSaidWithWindow(t *testing.T) {
	clock := newSpeakerClock("local", "companion")
	clock.noteSaid("First sentence.")
	clock.noteSaid("Second sentence.")
	onOut := captureStdout(t, func() {
		clock.noteOn()
	})
	if !strings.Contains(onOut, "speaker on") {
		t.Fatalf("noteOn printed no speaker-on marker:\n%s", onOut)
	}
	onLines := strings.Split(strings.TrimSpace(onOut), "\n")
	if len(onLines) != 3 {
		t.Fatalf("noteOn must print speaker on + 2 joined said lines, got %d lines:\n%s", len(onLines), onOut)
	}
	if !strings.Contains(onOut, "said: First sentence.") || !strings.Contains(onOut, "said: Second sentence.") {
		t.Fatalf("noteOn must join BOTH queued said lines:\n%s", onOut)
	}
	// The window is open now: a fresh said prints immediately.
	liveOut := captureStdout(t, func() {
		clock.noteSaid("Third sentence.")
	})
	if !strings.Contains(liveOut, "said: Third sentence.") || strings.Contains(liveOut, "speaker on") {
		t.Fatalf("open-window said must print at once without a new window:\n%s", liveOut)
	}
	offOut := captureStdout(t, func() {
		clock.endStream() // wait queue already drained: no-op
		clock.noteOff()
	})
	if !strings.Contains(offOut, "speaker off (") {
		t.Fatalf("noteOff printed no speaker-off marker:\n%s", offOut)
	}
	if strings.Contains(offOut, "said:") {
		t.Fatalf("normal path must not re-print joined saids at endStream:\n%s", offOut)
	}
}

// V5 pin (bughunt 2026-11-15 item 7): the drain routes said through the
// SAME speakerClock the live feed used - after stop() drains the clock,
// a drain-side said prints immediately with the join styling ("said:"),
// not the raw voiceEventRender line. Covers both clock wirings: the
// single clock (voice local) and the per-ccid clocks (voice link).
func TestDrainPollOnceSaidUsesClockJoin(t *testing.T) {
	build := func() (*voiceEventTracker, *fakeEventsClient) {
		f := &fakeEventsClient{rows: []voiceEventRow{
			{Seq: 1, Kind: "said", At: "2026-11-15T10:00:04Z", Text: "Drained reply."},
		}}
		tr := newVoiceEventTracker(f, map[string][2]string{"cc-1": {"a", "emp"}})
		return tr, f
	}
	// Single-clock wiring (voice local shape): the live clock holds the
	// said (window never opened), stop() drains it out, then the drain's
	// own said events print immediately through the clock.
	tr, f := build()
	tr.clock = newSpeakerClock("local", "emp")
	tr.clock.noteSaid("Live-window said.")
	out := captureStdout(t, func() {
		tr.stop()
	})
	if !strings.Contains(out, "said: Live-window said.") {
		t.Fatalf("stop() must flush the stranded live said:\n%s", out)
	}
	out = captureStdout(t, func() {
		if !tr.drainPollOnce("cc-1", "a", "emp") {
			t.Fatal("drain must keep polling a live feed")
		}
	})
	if !strings.Contains(out, "said: Drained reply.") {
		t.Fatalf("drain said must print through the clock join:\n%s", out)
	}
	if strings.Contains(out, "10:00:04 said: Drained reply.") && strings.Count(out, "said:") != 1 {
		t.Fatalf("drain said must be the clock-joined shape only:\n%s", out)
	}
	if f.served != 1 {
		t.Fatalf("drain served = %d, want 1", f.served)
	}
	// Per-ccid wiring (voice link shape): a drained clock prints the
	// said at once; a tracker WITHOUT a clock still renders said raw
	// (backwards-compatible nil-clock degrade).
	tr2, _ := build()
	c := newSpeakerClock("a", "emp")
	c.SetDrained()
	tr2.clocks = map[string]*speakerClock{"cc-1": c}
	out2 := captureStdout(t, func() {
		if !tr2.drainPollOnce("cc-1", "a", "emp") {
			t.Fatal("drain must keep polling")
		}
	})
	if !strings.Contains(out2, "said: Drained reply.") {
		t.Fatalf("per-ccid drained clock must print the said:\n%s", out2)
	}
	tr3, _ := build()
	out3 := captureStdout(t, func() {
		_ = tr3.drainPollOnce("cc-1", "a", "emp")
	})
	if !strings.Contains(out3, "10:00:04 said: Drained reply.") {
		t.Fatalf("nil clock must keep the raw render:\n%s", out3)
	}
}

// clockHasWait reports whether the clock is holding queued said lines
// (test-only peek under the clock's own mutex).
func clockHasWait(c *speakerClock) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.wait) > 0
}

// scriptedGapReader feeds nChunks 640 B chunks (one Read each, the
// adapter's chunk size), then on the FOLLOW-UP read (the one that will
// return EOF - after the final chunk has crossed the sink and the bound
// flush) sleeps well past the sink gap so the gap timer closes the
// speaker window (noteOff), fires the late-said hook (the telemetry poll
// delivering a said event AFTER the window closed), and only then
// returns EOF with the sink fully flushed (pending==0).
type scriptedGapReader struct {
	left     int
	payload  byte
	lateSaid func()
	fired    bool
}

func (r *scriptedGapReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		if !r.fired {
			r.fired = true
			// All audio is handed off (pending==0). Well past the
			// 350 ms gap the window closes and the late said
			// queues - then EOF with nothing pending: the exact
			// V4 drop shape. 1200 ms gives the gap timer (350 ms)
			// a wide, scheduling-tolerant margin to close the
			// window BEFORE the said queues.
			time.Sleep(1200 * time.Millisecond)
			if r.lateSaid != nil {
				r.lateSaid()
			}
		}
		return 0, io.EOF
	}
	n := min(len(p), r.left)
	r.left -= n
	for i := range n {
		p[i] = r.payload
	}
	return n, nil
}

// V4 end-to-end pin: the drop shape through drainLiveSpeaker. A reply
// whose audio fully crosses the bound flush (pending==0 afterwards), the
// gap timer closes the speaker window, the said event lands AFTER the
// close (late 500 ms telemetry tick), and EOF arrives with pending==0 -
// notePlayed returns early, noteOn never re-fires, and the OLD code
// silently dropped the said line from the live feed. The flush-complete
// edge must print it.
func TestDrainLiveSpeakerShortReplySaidPrintsAtEOF(t *testing.T) {
	t.Parallel()
	fp := &playoutFakePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour) // gap default irrelevant; the drain arms voice.SinkGap itself
	clock := newSpeakerClock("local", "companion")
	done := make(chan error, 1)
	late := func() {
		clock.noteSaid("Sure.")
	}
	// 19 chunks = 12160 B: the 19th write crosses liveMaxPendingAudioBytes
	// (12000 B), the bound flush hands off everything (pending==0), and
	// no further audio follows.
	r := &scriptedGapReader{left: 19 * liveSpeakerReadChunk, payload: 0x01, lateSaid: late}
	out := captureStdout(t, func() {
		drainLiveSpeaker(r, sink, done, nil, clock)
	})
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(fp.bursts) != 1 {
		t.Fatalf("want 1 bound-flushed burst, got %d", len(fp.bursts))
	}
	if sink.Pending() != 0 {
		t.Fatalf("EOF tail must leave pending==0: %d B", sink.Pending())
	}
	if !strings.Contains(out, "said: Sure.") {
		t.Fatalf("late said line was dropped from the live feed:\n%s", out)
	}
	// The window opened on the bound flush and closed on the gap; the
	// said line must NOT be joined under a fabricated second window.
	found := strings.Contains(out, "speaker on")
	offIdx := strings.Index(out, "speaker off")
	saidIdx := strings.Index(out, "said: Sure.")
	if !found || offIdx < 0 || saidIdx < offIdx {
		t.Fatalf("unexpected ordering; want speaker on -> speaker off -> late said:\n%s", out)
	}
}

// REGRESSION (said-text-gaps): a "said" event with no text must NEVER
// render as a bare "<time> said" line - the operator regression was a
// contentless said with no colon and no visible text. Whether the text is
// genuinely empty (old daemon / truncated row) or populated, a said line
// always ends in "said: <something>".
func TestVoiceEventRenderSaidNeverBare(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   voiceEventRow
		want string
	}{
		{"empty-text", voiceEventRow{Kind: "said", At: "2026-11-18T22:05:06Z", Text: ""},
			"22:05:06 said: (no text)"},
		{"whitespace-text", voiceEventRow{Kind: "said", At: "2026-11-18T22:05:42Z", Text: "   "},
			"22:05:42 said: (no text)"},
		{"full-text", voiceEventRow{Kind: "said", At: "2026-11-18T22:05:42Z", Text: "the answer is forty-two"},
			"22:05:42 said: the answer is forty-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := voiceEventRender(tc.ev)
			if got != tc.want {
				t.Fatalf("render = %q, want %q", got, tc.want)
			}
			if !strings.Contains(got, "said: ") {
				t.Fatalf("said line has no colon+content (bare said regression): %q", got)
			}
		})
	}
}

// saidTextOrMissing is the single content guard both the raw renderer and
// the speaker-clock join use: a blank said must be visibly marked, never
// dropped or printed as a contentless line.
func TestSaidTextOrMissing(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "(no text)"}, {"  	 ", "(no text)"}, {"hello", "hello"},
	} {
		if got := saidTextOrMissing(tc.in); got != tc.want {
			t.Errorf("saidTextOrMissing(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// REGRESSION (said-text-gaps): the SEND time from the event must survive
// the speaker-clock join. A said that queues before audio plays prints at
// its own send stamp when noteOn joins it - NOT at the played stamp - and
// the speaker markers are explicitly labelled "played". This is the
// operator's precise timeline: text + send time vs actual playback.
func TestSpeakerClockSaidKeepsSendStampThroughJoin(t *testing.T) {
	clock := newSpeakerClock("local", "companion")
	// The event's At is the say-send time (events.go), distinct from the
	// wall-clock moment noteOn fires (playback).
	clock.noteSaidAt("2026-11-18T22:05:06Z", "queued before playback")
	out := captureStdout(t, func() {
		clock.noteOn()
		clock.noteOff()
	})
	if !strings.Contains(out, "22:05:06 said: queued before playback") {
		t.Fatalf("joined said lost its SEND stamp (re-stamped to play time?):\n%s", out)
	}
	if !strings.Contains(out, "speaker on (played)") {
		t.Fatalf("speaker-on must be labelled as the PLAYED edge:\n%s", out)
	}
	if !strings.Contains(out, "speaker off (played,") {
		t.Fatalf("speaker-off must be labelled as the PLAYED edge:\n%s", out)
	}
}

// End-to-end (tracker level): said lines always carry their full text at
// the SEND stamp, and the per-turn timeline fires after the played edge —
// the operator sees STT, the model reply, and real audio playback joined.
func TestTrackerTimelineSaidTextAndTurnSummary(t *testing.T) {
	rows := []voiceEventRow{
		{Seq: 1, Kind: "heard", At: "2026-11-18T22:05:00Z", Text: "what time is it"},
		{Seq: 2, Kind: "prompt_dispatch", At: "2026-11-18T22:05:01Z", Text: "what time is it", Words: 4},
		{Seq: 3, Kind: "turn_final", At: "2026-11-18T22:05:03Z", Words: 5, Ms: 2000},
		{Seq: 4, Kind: "said", At: "2026-11-18T22:05:03Z", Text: "It is ten past five."},
		{Seq: 5, Kind: "said", At: "2026-11-18T22:05:04Z", Text: ""}, // empty row -> honest marker
	}
	clock := newSpeakerClock("local", "companion")
	tr := newVoiceEventTracker(&fakeEventsClient{rows: rows}, map[string][2]string{"cc-1": {"a", "companion"}})
	tr.clock = clock
	// Poll the events into the clock, fire the played edge, then drain —
	// all inside the capture so every line is observable.
	out := captureStdout(t, func() {
		tr.pollOnce("cc-1", "a", "companion")
		clock.noteOn()
		tr.stop() // marks stopped + drains the clock
		tr.drainOnce()
	})
	for _, want := range []string{
		"22:05:03 said: It is ten past five.",
		"22:05:04 said: (no text)",
		"turn: heard 22:05:00 -> thinking 22:05:01 -> replied 22:05:03 -> played ",
		"(model 2000ms, 5 words)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("timeline missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "22:05:03 said\n") || strings.Contains(out, " said\n") {
		t.Fatalf("bare said line still printed:\n%s", out)
	}
}

// The per-turn phase summary joins STT (heard), model reply (thinking ->
// replied) and the real playback (played) on one compact line.
func TestSpeakerClockTurnPhaseSummary(t *testing.T) {
	clock := newSpeakerClock("local", "companion")
	clock.notePhase("heard", "2026-11-18T22:05:01Z", 0, 0)
	clock.notePhase("prompt_dispatch", "2026-11-18T22:05:01.5Z", 5, 0)
	clock.notePhase("turn_final", "2026-11-18T22:05:04Z", 5, 2500)
	clock.noteOn() // played edge
	out := captureStdout(t, func() {
		clock.printTurnSummary()
		clock.printTurnSummary() // once-guard: must not double-print
	})
	want := "turn: heard 22:05:01 -> thinking 22:05:01 -> replied 22:05:04 -> played "
	if !strings.Contains(out, want) {
		t.Fatalf("turn summary missing phase chain %q:\n%s", want, out)
	}
	if !strings.Contains(out, "(model 2500ms, 5 words)") {
		t.Fatalf("turn summary missing model timing:\n%s", out)
	}
	if strings.Count(out, "turn: ") != 1 {
		t.Fatalf("turn summary must print exactly once:\n%s", out)
	}
}
