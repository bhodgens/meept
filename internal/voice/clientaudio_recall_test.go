package voice

// clientaudio_recall_test.go - leaf 01 (recallable playback) pins on the
// SpeakerSink + Player seam: Clear drops the buffered burst, silences
// the player (barge-in must stop the tail within ~20 ms, not let a
// handed-off burst play out under the next answer - the talk-over defect
// of transcript local-2d84c384fd93f447), and no pre-Clear bytes ever
// reach Play after the clear.

import (
	"bytes"
	"testing"
	"time"
)

// recallFakePlayer records Play/Clear/Close calls in order.
type recallFakePlayer struct {
	calls   []string
	bursts  [][]byte
	clearN  int
	closedN int
}

func (f *recallFakePlayer) Play(b []byte) error {
	f.calls = append(f.calls, "play")
	f.bursts = append(f.bursts, append([]byte(nil), b...))
	return nil
}
func (f *recallFakePlayer) Clear() {
	f.calls = append(f.calls, "clear")
	f.clearN++
}
func (f *recallFakePlayer) Close() {
	f.calls = append(f.calls, "close")
	f.closedN++
}

func TestSpeakerSinkClearSilencesPlayer(t *testing.T) {
	fp := &recallFakePlayer{}
	sink := NewSpeakerSink(fp, time.Hour) // hour gap: only explicit flush/Clear hand off

	if _, err := sink.Write(bytes.Repeat([]byte{0xAA}, 640)); err != nil {
		t.Fatalf("write: %v", err)
	}
	sink.Clear()
	if fp.clearN != 1 {
		t.Fatalf("Clear must reach the player seam (silence the live burst), got %d calls", fp.clearN)
	}
	if sink.Pending() != 0 {
		t.Fatalf("Clear must drop the buffered burst, %d B pending", sink.Pending())
	}
	// Nothing buffered: neither an explicit Flush nor the sink Close may
	// hand pre-Clear bytes to the player.
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush after clear: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(fp.bursts) != 0 {
		t.Fatalf("no Play may happen for pre-Clear writes: %d bursts", len(fp.bursts))
	}
	if fp.closedN != 1 {
		t.Fatalf("Close must still release the player seam")
	}
}

func TestSpeakerSinkClearMidBurstKeepsLaterAudio(t *testing.T) {
	fp := &recallFakePlayer{}
	sink := NewSpeakerSink(fp, time.Hour)
	// Burst 1 plays out (sentence 1).
	if _, err := sink.Write(bytes.Repeat([]byte{0x01}, 1280)); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush 1: %v", err)
	}
	// Barge-in lands mid-burst-2: the queued tail is dropped, the player
	// is silenced, and post-clear audio starts a CLEAN new burst.
	if _, err := sink.Write(bytes.Repeat([]byte{0x02}, 640)); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	sink.Clear()
	if _, err := sink.Write(bytes.Repeat([]byte{0x03}, 320)); err != nil {
		t.Fatalf("write 3: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush 2: %v", err)
	}
	if len(fp.bursts) != 2 {
		t.Fatalf("want burst1 + post-clear burst only, got %d", len(fp.bursts))
	}
	if !bytes.Equal(fp.bursts[1], bytes.Repeat([]byte{0x03}, 320)) {
		t.Fatalf("post-Clear burst carries pre-Clear bytes: %d B", len(fp.bursts[1]))
	}
	if fp.clearN != 1 {
		t.Fatalf("want exactly one player silence at the barge-in, got %d", fp.clearN)
	}
}

func TestSpeakerSinkNilPlayerClearAndPending(t *testing.T) {
	// nil player = discarding sink: Clear must not panic, Pending tracks
	// the buffer regardless.
	sink := NewSpeakerSink(nil, time.Second)
	if _, err := sink.Write([]byte("xx")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if sink.Pending() != 2 {
		t.Fatalf("Pending = %d, want 2", sink.Pending())
	}
	sink.Clear()
	if sink.Pending() != 0 {
		t.Fatalf("Clear must empty the buffer even with a nil player")
	}
	sink.Close() //nolint:errcheck // nil-player discard path
}
