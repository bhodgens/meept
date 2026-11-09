package voice

import (
	"bytes"
	"testing"
	"time"
)

// Pins the 2026-11-09 no-nested-headers contract: SpeakerSink hands the
// player RAW PCM. The pre-amendment Flush prepended a 44-byte RIFF header
// per burst; the CLI drain loops flushed per socket read (one 640-byte
// paced chunk), so every burst was header+chunk (stride 684). In live mode
// oto played those header bytes as garbage samples between every 20 ms of
// speech - the "jittery" noise callers heard - and --out files carried
// hundreds of nested RIFF markers whisper read as "(buzzing)".
func TestSpeakerSinkBurstsAreRawPcm(t *testing.T) {
	fp := &fakePlayer{}
	sink := NewSpeakerSink(fp, 50*time.Millisecond)
	chunk := bytes.Repeat([]byte{0x11}, 640)
	if _, err := sink.Write(chunk); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if fp.playN != 1 {
		t.Fatalf("want 1 play, got %d", fp.playN)
	}
	if got := fp.bursts[0]; !bytes.Equal(got, chunk) {
		t.Fatalf("burst len %d, want raw 640-byte chunk unchanged", len(got))
	}
	if bytes.Contains(fp.bursts[0], []byte("RIFF")) {
		t.Fatalf("burst carries a WAV header (regression of the per-burst header)")
	}
}
