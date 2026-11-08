package voiceframe

import (
	"bytes"
	"strings"
	"testing"
)

// TestOpenFrameDecode pins the CL1 open-frame wire shape from
// docs/plans/20261106-voice-local-leg/master.md: decoding the contract's
// byte-exact open line must yield EventOpen plus every per-call provider
// profile field.
func TestOpenFrameDecode(t *testing.T) {
	line := `{"call_id":"cc1","event":"open","from":"alice","to":"local:host","direction":"local","codec":"L16","sample_rate":16000,"local":true,"stt_vendor":"whisper","stt_model":"base","tts_vendor":"piper","tts_model":"en_US-lessac-medium","barge_in_rms":3000}`
	f, err := DecodeFrame([]byte(line))
	if err != nil {
		t.Fatalf("decode open frame: %v", err)
	}
	if f.CallID != "cc1" {
		t.Errorf("CallID = %q, want cc1", f.CallID)
	}
	if f.Event != EventOpen {
		t.Errorf("Event = %q, want %q", f.Event, EventOpen)
	}
	if f.From != "alice" {
		t.Errorf("From = %q, want alice", f.From)
	}
	if f.To != "local:host" {
		t.Errorf("To = %q, want local:host", f.To)
	}
	if f.Direction != "local" {
		t.Errorf("Direction = %q, want local", f.Direction)
	}
	if f.Codec != "L16" {
		t.Errorf("Codec = %q, want L16", f.Codec)
	}
	if f.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", f.SampleRate)
	}
	if !f.Local {
		t.Errorf("Local = %v, want true", f.Local)
	}
	if f.SttVendor != "whisper" {
		t.Errorf("SttVendor = %q, want whisper", f.SttVendor)
	}
	if f.SttModel != "base" {
		t.Errorf("SttModel = %q, want base", f.SttModel)
	}
	if f.TtsVendor != "piper" {
		t.Errorf("TtsVendor = %q, want piper", f.TtsVendor)
	}
	if f.TtsModel != "en_US-lessac-medium" {
		t.Errorf("TtsModel = %q, want en_US-lessac-medium", f.TtsModel)
	}
	if f.TtsVoice != "" {
		t.Errorf("TtsVoice = %q, want empty (omitted in contract line)", f.TtsVoice)
	}
	if f.BargeInRms != 3000 {
		t.Errorf("BargeInRms = %d, want 3000", f.BargeInRms)
	}
}

// TestOpenFrameEncode pins the byte-exact encoded shape: struct order,
// omitempty on every new key, and the trailing newline.
func TestOpenFrameEncode(t *testing.T) {
	f := Frame{
		CallID:     "cc1",
		Event:      EventOpen,
		From:       "alice",
		To:         "local:host",
		Direction:  "local",
		Codec:      "L16",
		SampleRate: 16000,
		Local:      true,
		SttVendor:  "whisper",
		SttModel:   "base",
		TtsVendor:  "piper",
		TtsModel:   "en_US-lessac-medium",
		BargeInRms: 3000,
	}
	got, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode open frame: %v", err)
	}
	want := `{"call_id":"cc1","event":"open","from":"alice","to":"local:host","direction":"local","codec":"L16","sample_rate":16000,"local":true,"stt_vendor":"whisper","stt_model":"base","tts_vendor":"piper","tts_model":"en_US-lessac-medium","barge_in_rms":3000}` + "\n"
	if string(got) != want {
		t.Fatalf("encoded open frame\n got: %s\nwant: %s", got, want)
	}
}

// TestOpenFrameWithVoiceEncode covers the non-empty tts_voice variant from
// the master contract shape (the open frame carries tts_voice when set).
func TestOpenFrameWithVoiceEncode(t *testing.T) {
	f := Frame{
		CallID:     "cc2",
		Event:      EventOpen,
		From:       "alice",
		To:         "local:host",
		Direction:  "local",
		Codec:      "L16",
		SampleRate: 16000,
		Local:      true,
		SttVendor:  "whisper",
		SttModel:   "base",
		TtsVendor:  "piper",
		TtsModel:   "en_US-lessac-medium",
		TtsVoice:   "lessac",
		BargeInRms: 3000,
	}
	got, err := EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode open frame with voice: %v", err)
	}
	want := `{"call_id":"cc2","event":"open","from":"alice","to":"local:host","direction":"local","codec":"L16","sample_rate":16000,"local":true,"stt_vendor":"whisper","stt_model":"base","tts_vendor":"piper","tts_model":"en_US-lessac-medium","tts_voice":"lessac","barge_in_rms":3000}` + "\n"
	if string(got) != want {
		t.Fatalf("encoded open frame with voice\n got: %s\nwant: %s", got, want)
	}
}

// TestZeroValueFrameOmitsNewKeys is the omitempty proof: a zero-value Frame
// must encode byte-identically to the pre-leaf shape - none of the seven
// new keys may appear. Existing frames on the wire are unchanged.
func TestZeroValueFrameOmitsNewKeys(t *testing.T) {
	got, err := EncodeFrame(Frame{})
	if err != nil {
		t.Fatalf("encode zero frame: %v", err)
	}
	for _, key := range []string{"local", "stt_vendor", "stt_model", "tts_vendor", "tts_model", "tts_voice", "barge_in_rms"} {
		if bytes.Contains(got, []byte(`"`+key+`"`)) {
			t.Errorf("zero-value frame emitted new key %q: %s", key, got)
		}
	}
	if !bytes.HasSuffix(got, []byte("}\n")) {
		t.Fatalf("zero-value frame not a single JSON line: %q", got)
	}
	if strings.Contains(strings.TrimSuffix(string(got), "\n"), `"event":"open"`) {
		t.Errorf("zero-value frame must not carry event open: %s", got)
	}
}
