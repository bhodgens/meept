package voiceframe

// C3 clip-frame tests (plan 20261113-filler-clips-and-context-warm,
// leaf 03): the three contract shapes round-trip byte-exactly, and the
// C3 back-compat rule holds — a frame WITHOUT clip fields encodes to
// today's exact JSON (old child/old daemon fall through unchanged).

import (
	"bytes"
	"testing"
)

// C3 shape 1: {"event":"say","call_id":"...","text":"Just a sec.",
// "last":false,"clip_id":"<64 hex>"}
func TestSayFrameWithClipIDWireShape(t *testing.T) {
	const key = "fd2c3570dfae9ed83b3c70c4bcb0b797f03761565af5d0751f2048d7db70b870"
	last := false
	f := Frame{
		CallID: "cc1",
		Event:  EventSay,
		Text:   "Just a sec.",
		Last:   &last,
		ClipID: key,
	}
	got, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"call_id":"cc1","event":"say","text":"Just a sec.","last":false,"clip_id":"` + key + `"}` + "\n"
	if string(got) != want {
		t.Fatalf("say+clip_id wire bytes:\n got %s\nwant %s", got, want)
	}
	dec, err := DecodeFrame(got)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.ClipID != key || dec.Text != "Just a sec." || dec.Last == nil || *dec.Last != false {
		t.Fatalf("round-trip drift: %+v", dec)
	}
}

// C3 shape 2: clip_render with the clips list (up to 32 entries; the cap
// is a daemon-side contract, the codec carries whatever arrives).
func TestClipRenderFrameWireShape(t *testing.T) {
	const k1 = "1111111111111111111111111111111111111111111111111111111111111111"
	const k2 = "2222222222222222222222222222222222222222222222222222222222222222"
	f := Frame{
		CallID: "cc1",
		Event:  EventClipRender,
		Clips:  []ClipSpec{{Key: k1, Text: "Just a sec."}, {Key: k2, Text: "Hello!"}},
	}
	got, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"call_id":"cc1","event":"clip_render","clips":[{"key":"` + k1 + `","text":"Just a sec."},{"key":"` + k2 + `","text":"Hello!"}]}` + "\n"
	if string(got) != want {
		t.Fatalf("clip_render wire bytes:\n got %s\nwant %s", got, want)
	}
	dec, err := DecodeFrame(got)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Event != EventClipRender || len(dec.Clips) != 2 || dec.Clips[0].Key != k1 || dec.Clips[1].Text != "Hello!" {
		t.Fatalf("clip_render round-trip drift: %+v", dec)
	}
}

// C3 shape 3: clip_ready with ok:true and ok:false — the pointer field
// keeps explicit false on the wire (absent != false, same discipline as
// Last).
func TestClipReadyFrameWireShapes(t *testing.T) {
	const key = "3333333333333333333333333333333333333333333333333333333333333333"
	for _, ok := range []bool{true, false} {
		f := Frame{CallID: "cc1", Event: EventClipReady, Key: key, OK: &ok}
		got, err := EncodeFrame(f)
		if err != nil {
			t.Fatalf("encode ok=%v: %v", ok, err)
		}
		want := `{"call_id":"cc1","event":"clip_ready","key":"` + key + `","ok":` + boolJSON(ok) + "}" + "\n"
		if string(got) != want {
			t.Fatalf("clip_ready ok=%v wire bytes:\n got %s\nwant %s", ok, got, want)
		}
		dec, err := DecodeFrame(got)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if dec.Key != key || dec.OK == nil || *dec.OK != ok {
			t.Fatalf("clip_ready round-trip drift (ok=%v): %+v", ok, dec)
		}
	}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Back-compat (C3): none of the new keys may appear on frames that do
// not set them — a zero-value frame and a plain say frame must encode
// byte-identically to the pre-clip shape, so an old daemon line keeps
// parsing and an old child ignores the additions.
func TestClipFieldsOmittedOnPlainFrames(t *testing.T) {
	f := Frame{CallID: "cc1", Event: EventSay, Text: "hi", Last: func() *bool { v := true; return &v }()}
	got, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"call_id":"cc1","event":"say","text":"hi","last":true}` + "\n"
	if string(got) != want {
		t.Fatalf("plain say frame gained clip keys:\n got %s\nwant %s", got, want)
	}
	zero, err := EncodeFrame(Frame{})
	if err != nil {
		t.Fatalf("encode zero frame: %v", err)
	}
	for _, key := range []string{`"clip_id"`, `"clips"`, `"key"`, `"ok"`} {
		if bytes.Contains(zero, []byte(key)) {
			t.Errorf("zero-value frame emitted new key %s: %s", key, zero)
		}
	}
}

// An old daemon's line (no clip fields) decodes into the new struct with
// zero clip values — the child's clip branch stays inert.
func TestOldDaemonLineDecodesWithoutClipFields(t *testing.T) {
	line := []byte(`{"call_id":"cc1","event":"say","text":"Just a sec.","last":false}` + "\n")
	f, err := DecodeFrame(line)
	if err != nil {
		t.Fatalf("decode old line: %v", err)
	}
	if f.ClipID != "" || f.Clips != nil || f.Key != "" || f.OK != nil {
		t.Fatalf("old line carried clip values: %+v", f)
	}
}
