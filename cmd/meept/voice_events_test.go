package main

// voice_events_test.go - unit tests for the operator-telemetry CLI layer
// (voice-events tree): the pure event renderer and the poller's
// cursor/print behaviour against a fake RPC client. No daemon, no sockets.

import (
	"encoding/json"
	"sync"
	"testing"
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
	p, _ := params.(map[string]any)
	cursor, _ := p["cursor"].(uint64)
	var out []voiceEventRow
	for _, ev := range f.rows {
		if ev.Seq > cursor {
			out = append(out, ev)
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
	n := tr.pollOnce("cc-1", "a", "companion")
	if n != 2 {
		t.Fatalf("first poll printed %d, want 2", n)
	}
	// Second poll: cursor advanced, nothing new -> 0 (no re-print).
	if n := tr.pollOnce("cc-1", "a", "companion"); n != 0 {
		t.Fatalf("second poll printed %d, want 0 (cursor must advance)", n)
	}
	tr.drainOnce() // must terminate and not re-print
	if tr.cursors["cc-1"] == 0 {
		t.Fatal("cursor did not advance")
	}
}

func TestVoiceEventTrackerBestEffortOnErrors(t *testing.T) {
	f := &fakeEventsClient{
		rows: []voiceEventRow{{Seq: 1, Kind: "barge_in"}},
	}
	f.errFunc = func(n int) error {
		if n == 1 {
			return jsonErrorStub{}
		}
		return nil
	}
	tr := newVoiceEventTracker(f, map[string][2]string{"cc-1": {"b", "secretary"}})
	if n := tr.pollOnce("cc-1", "b", "secretary"); n != 0 {
		t.Fatalf("errored poll printed %d, want 0", n)
	}
	if n := tr.pollOnce("cc-1", "b", "secretary"); n != 1 {
		t.Fatalf("retry printed %d, want 1", n)
	}
}

type jsonErrorStub struct{}

func (jsonErrorStub) Error() string { return "rpc: connection closed" }
