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
	"strings"
	"sync"
	"testing"
	"time"
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
		{voiceEventRow{Kind: "prompt_held", At: "2026-11-11T10:00:08Z", Text: "next question"},
			"10:00:08 held (peer speaking): next question"},
		// Unknown kinds pass through the raw kind (future-proof).
		{voiceEventRow{Kind: "floor_released", At: "short"}, "short floor_released"},
	}
	for _, c := range cases {
		if got := voiceEventRender(c.ev); got != c.want {
			t.Errorf("render(%s) = %q, want %q", c.ev.Kind, got, c.want)
		}
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
	var cur uint64 = cursor
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

// rpcErrStub is a transient RPC failure (the best-effort retry posture).
type rpcErrStub struct{}

func (rpcErrStub) Error() string { return "rpc: connection closed" }

func TestVoiceEventTrackerBestEffortOnErrors(t *testing.T) {
	f := &fakeEventsClient{
		rows: []voiceEventRow{{Seq: 1, Kind: "barge_in"}},
	}
	f.errFunc = func(n int) error {
		if n == 1 {
			return rpcErrStub{}
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
		t.Fatal("found=false must end the poller for that ccid")
	}
	if !tr2.ended["cc-2"] {
		t.Fatal("found=false must mark the ccid ended")
	}
	// The drain must not re-poll an ended ccid.
	tr2.drainOnce()
	if f2.calls != 1 {
		t.Fatalf("drain re-polled an ended ccid: %d calls, want 1", f2.calls)
	}
	// A never-tracked ccid (cursor 0, nothing consumed) ends SILENTLY -
	// the ring-less old daemon is the quiet teardown posture.
	tr3 := newVoiceEventTracker(&foundFalseClient{}, map[string][2]string{"cc-3": {"a", "emp"}})
	if tr3.pollOnce("cc-3", "a", "emp") {
		t.Fatal("found=false must end the poller")
	}
	if !tr3.ended["cc-3"] {
		t.Fatal("silent feed end must still stop polling")
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

type contextTodo struct{}

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
