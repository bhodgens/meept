package voiceframe

// Contract C3 (FROZEN, quoted verbatim from
// docs/plans/20261113-filler-clips-and-context-warm/master.md):
//
//	{"event":"say","call_id":"...","text":"Just a sec.","last":false,"clip_id":"<64 hex>"}
//	{"event":"clip_render","call_id":"<ccid>","clips":[{"key":"<64 hex>","text":"Just a sec."}]}
//	{"event":"clip_ready","call_id":"<ccid>","key":"<64 hex>","ok":true}
//
//	"All new fields are ADDITIVE and optional per the keys-wire convention
//	(internal/daemon/voice_keys.go comments): an old child ignores
//	clip_id/clip_render (falls back to live synth — today's byte-identical
//	behavior); an old daemon never sends them. clip_render.clips is capped
//	at 32 entries per frame; the daemon shards larger pools across frames."
//
// 00T GATE FILE (restored under a distinct name after the leaf-03 impl
// wave claimed clip_frames_test.go — the orchestrator owns the naming
// collision). These assertions complement leaf 03's own wire-shape suite
// with the frozen literal cross-check + tolerant-decode pins.

import (
	"encoding/json"
	"strings"
	"testing"
)

// CF-1: the say frame's ClipID field encodes as an additive `clip_id`
// key (leaf 03's pinned tag: `ClipID string `json:"clip_id,omitempty"“)
// and decodes back.
func TestClipGateFrameSayClipIDRoundTrip(t *testing.T) {
	const hexKey = "668ae471895b27afe336829ba50e1637f1db59c508ee813234663373f6f6ff77"
	f := Frame{
		CallID: "cc-1",
		Event:  EventSay,
		Text:   "Just a sec.",
		Last:   gateBoolPtr(false),
		ClipID: hexKey,
	}
	line, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := strings.TrimSuffix(string(line), "\n")
	if !strings.Contains(got, `"clip_id":"`+hexKey+`"`) {
		t.Fatalf("say frame missing clip_id key: %s", got)
	}
	back, err := DecodeFrame(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.ClipID != hexKey {
		t.Fatalf("ClipID round-trip = %q, want %q", back.ClipID, hexKey)
	}
	if back.Event != EventSay || back.Text != "Just a sec." {
		t.Fatalf("say fields drifted: %+v", back)
	}
}

// CF-2: back-compat — an empty ClipID is OMITTED (omitempty): a frame
// without clip fields serializes to today's exact bytes.
func TestClipGateFrameWithoutClipIDIsTodaysExactJSON(t *testing.T) {
	f := Frame{CallID: "cc-2", Event: EventSay, Text: "One moment.", Last: gateBoolPtr(false)}
	line, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"call_id":"cc-2","event":"say","text":"One moment.","last":false}` + "\n"
	if string(line) != want {
		t.Fatalf("clip-less say frame bytes:\n got %s\nwant %s", line, want)
	}
	back, err := DecodeFrame([]byte(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.ClipID != "" {
		t.Fatalf("absent clip_id must decode empty, got %q", back.ClipID)
	}
}

// CF-3/CF-4: the clip_render and clip_ready lines must decode without
// error through the shared codec (tolerant decode) and carry the pinned
// event constants.
func TestClipGateFrameClipEventsDecodeTolerantly(t *testing.T) {
	render, err := DecodeFrame([]byte(`{"event":"clip_render","call_id":"cc-3","clips":[{"key":"aa","text":"Just a sec."}]}` + "\n"))
	if err != nil {
		t.Fatalf("clip_render must decode: %v", err)
	}
	if render.Event != EventClipRender {
		t.Fatalf("event = %q, want %q (EventClipRender constant per C3)", render.Event, "clip_render")
	}
	if render.CallID != "cc-3" {
		t.Fatalf("call_id = %q", render.CallID)
	}

	ready, err := DecodeFrame([]byte(`{"event":"clip_ready","call_id":"cc-4","key":"bb","ok":true}` + "\n"))
	if err != nil {
		t.Fatalf("clip_ready must decode: %v", err)
	}
	if ready.Event != EventClipReady {
		t.Fatalf("event = %q, want %q (EventClipReady constant per C3)", ready.Event, "clip_ready")
	}
	if ready.CallID != "cc-4" {
		t.Fatalf("call_id = %q", ready.CallID)
	}
}

// CF-5: the C3 literal shapes themselves are the contract — pin the
// field NAMES (not just the constants) so a leaf that re-fields them
// (e.g. "clipId" or "id") fails the gate.
func TestClipGateFrameC3WireLiteralsAreFrozen(t *testing.T) {
	render := `{"event":"clip_render","call_id":"<ccid>","clips":[{"key":"<64 hex>","text":"Just a sec."}]}`
	var probe map[string]any
	if err := json.Unmarshal([]byte(render), &probe); err != nil {
		t.Fatalf("C3 render literal is not JSON: %v", err)
	}
	if probe["event"] != "clip_render" {
		t.Fatalf("render literal event = %v", probe["event"])
	}
	clips, ok := probe["clips"].([]any)
	if !ok || len(clips) != 1 {
		t.Fatalf("render literal clips = %v", probe["clips"])
	}
	entry, ok := clips[0].(map[string]any)
	if !ok {
		t.Fatalf("clip entry not an object: %T", clips[0])
	}
	if _, ok := entry["key"].(string); !ok {
		t.Fatalf("clip entry missing string \"key\": %v", entry)
	}
	if _, ok := entry["text"].(string); !ok {
		t.Fatalf("clip entry missing string \"text\": %v", entry)
	}
	ready := `{"event":"clip_ready","call_id":"<ccid>","key":"<64 hex>","ok":true}`
	var r struct {
		Event  string `json:"event"`
		CallID string `json:"call_id"`
		Key    string `json:"key"`
		OK     bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(ready), &r); err != nil {
		t.Fatalf("C3 ready literal is not JSON: %v", err)
	}
	if r.Event != "clip_ready" || r.Key != "<64 hex>" || !r.OK {
		t.Fatalf("ready literal fields: %+v", r)
	}
}

// CF-6: clip frames never carry last:true semantics (leaf 03: "Last
// *bool semantics untouched ... clip frames never carry it").
func TestClipGateClipFramesNeverCarryLast(t *testing.T) {
	f, err := DecodeFrame([]byte(`{"event":"clip_render","call_id":"cc-6","clips":[]}` + "\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Last != nil {
		t.Fatalf("clip_render decoded Last = %v, want nil (absent)", *f.Last)
	}
}

func gateBoolPtr(v bool) *bool { return &v }
