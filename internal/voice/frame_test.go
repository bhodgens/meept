package voice

import (
	"encoding/json"
	"strings"
	"testing"
)

// T1: encode/decode round-trip, exact JSON keys, Last *bool semantics,
// unknown-event (dtmf) decode.
func TestFrameRoundTrip(t *testing.T) {
	last := true
	f := Frame{
		CallID:     "ccid-1",
		Event:      EventPrompt,
		Text:       "hello there",
		Last:       &last,
		From:       "+15551230000",
		To:         "+15559876543",
		Direction:  "inbound",
		Codec:      "L16",
		SampleRate: 16000,
	}
	b, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("encoded frame must end with newline, got %q", string(b))
	}
	got, err := DecodeFrame(b)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	// Re-encode and compare bytes: Frame holds *Last, so struct equality
	// would compare pointers, not values.
	gotB, err := EncodeFrame(got)
	if err != nil {
		t.Fatalf("EncodeFrame(got): %v", err)
	}
	if string(gotB) != string(b) {
		t.Fatalf("round trip mismatch:\n got %s\nwant %s", string(gotB), string(b))
	}
	if got.Last == nil || !*got.Last {
		t.Fatalf("Last must round-trip as pointer to true, got %#v", got.Last)
	}
}

// C1 keys are byte-exact; a wrong key silently breaks the child.
func TestFrameExactKeys(t *testing.T) {
	f := Frame{
		CallID:     "c",
		Event:      "setup",
		Text:       "t",
		From:       "f",
		To:         "to",
		Direction:  "inbound",
		Codec:      "PCMU",
		SampleRate: 8000,
		Reason:     "hangup",
	}
	b, _ := EncodeFrame(f)
	b = b[:len(b)-1] // strip newline for raw JSON inspection
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{"call_id", "event", "text", "from", "to", "direction", "codec", "sample_rate", "reason"}
	if len(raw) != len(wantKeys) {
		t.Fatalf("got %d keys (%v), want exactly %v", len(raw), raw, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing C1 key %q in %s", k, string(b))
		}
	}
}

// Last omitted != Last false: the key must not appear when nil, and a
// decoded nil Last must be distinguishable from an explicit false.
func TestFrameLastPointer(t *testing.T) {
	f := Frame{CallID: "c", Event: EventInterrupt}
	b, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	if strings.Contains(string(b), "last") {
		t.Fatalf("omitted Last must not emit a key, got %s", string(b))
	}
	got, err := DecodeFrame(b)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if got.Last != nil {
		t.Fatalf("Last must be nil when key absent, got %#v", got.Last)
	}

	falseVar := false
	got2, err := DecodeFrame([]byte(`{"call_id":"c","event":"prompt","text":"x","last":false}`))
	if err != nil {
		t.Fatalf("DecodeFrame explicit false: %v", err)
	}
	if got2.Last == nil || *got2.Last != falseVar {
		t.Fatalf("explicit last:false must decode as pointer to false, got %#v", got2.Last)
	}
}

// Unknown events (dtmf from voicemail trees, anything else) decode without
// error - the handler may count or ignore them, the codec must not error.
func TestFrameDTMF(t *testing.T) {
	f, err := DecodeFrame([]byte(`{"call_id":"c","event":"dtmf","text":"1"}`))
	if err != nil {
		t.Fatalf("dtmf frame must decode without error, got %v", err)
	}
	if f.Event != "dtmf" {
		t.Fatalf("event = %q, want dtmf", f.Event)
	}
	weird, err := DecodeFrame([]byte(`{"call_id":"c","event":"some_future_event"}`))
	if err != nil {
		t.Fatalf("unknown event must decode without error, got %v", err)
	}
	if weird.Event != "some_future_event" {
		t.Fatalf("event = %q, want some_future_event", weird.Event)
	}
}

// T2: codec-derived 20ms chunk sizes per C2.
func TestChunkBytes(t *testing.T) {
	cases := []struct {
		codec string
		rate  int
		want  int
		ok    bool
	}{
		{"L16", 16000, 640, true},
		{"PCMU", 8000, 160, true},
		{"L16", 8000, 0, false},
		{"PCMU", 16000, 0, false},
		{"OPUS", 48000, 0, false},
		{"", 16000, 0, false},
	}
	for _, tc := range cases {
		got, err := ChunkBytes(tc.codec, tc.rate)
		if tc.ok {
			if err != nil {
				t.Fatalf("ChunkBytes(%s,%d) unexpected error: %v", tc.codec, tc.rate, err)
			}
			if got != tc.want {
				t.Fatalf("ChunkBytes(%s,%d) = %d, want %d", tc.codec, tc.rate, got, tc.want)
			}
		} else if err == nil {
			t.Fatalf("ChunkBytes(%s,%d) = %d, want error", tc.codec, tc.rate, got)
		}
	}
}

// EncodeFrame -> DecodeFrame over the wire shape the TextServer uses: each
// frame is one line, multiple frames concatenate cleanly.
func TestFrameLineFraming(t *testing.T) {
	var buf []byte
	for i := 0; i < 3; i++ {
		b, err := EncodeFrame(Frame{CallID: "c", Event: EventSay, Text: "s"})
		if err != nil {
			t.Fatalf("EncodeFrame: %v", err)
		}
		buf = append(buf, b...)
	}
	lines := strings.Split(strings.TrimSuffix(string(buf), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 framed lines, got %d (%q)", len(lines), string(buf))
	}
	for _, ln := range lines {
		if _, err := DecodeFrame([]byte(ln)); err != nil {
			t.Fatalf("DecodeFrame(%q): %v", ln, err)
		}
	}
}
