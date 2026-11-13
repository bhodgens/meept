package voice

// clientaudio_handoff_test.go - the Flush hand-off ownership contract.
//
// SpeakerSink.Flush hands the pending burst to the Player seam and the
// player may consume it ASYNCHRONOUSLY (oto's AudioPlayer wraps the slice
// in a bytes.Reader and the audio driver reads it on its own goroutine).
// bytes.Buffer.Reset() KEEPS the backing array, so handing off the live
// buffer let the NEXT s.buf.Write overwrite bytes still queued in the
// driver: a burst could replay the following burst's audio (audible
// glitch/repeat, not a crash). Flush must hand off an OWN copy.

import (
	"bytes"
	"testing"
	"time"
)

// handoffPlayer captures the exact slice handed to Play - no defensive
// copy, so a caller that mutates its buffer after the call is observable.
type handoffPlayer struct {
	bursts [][]byte
}

func (f *handoffPlayer) Play(b []byte) error {
	f.bursts = append(f.bursts, b)
	return nil
}
func (f *handoffPlayer) Clear() {}
func (f *handoffPlayer) Close() {}

func TestSpeakerSinkFlushHandsOffOwnedBurst(t *testing.T) {
	fp := &handoffPlayer{}
	sink := NewSpeakerSink(fp, time.Hour) // hour gap: only explicit Flush hands off

	first := bytes.Repeat([]byte{0x01}, 1280)
	if _, err := sink.Write(first); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush 1: %v", err)
	}
	if len(fp.bursts) != 1 {
		t.Fatalf("want 1 burst handed off, got %d", len(fp.bursts))
	}
	handedOff := fp.bursts[0]

	// A second burst reuses the SAME buffer bytes. Reset() keeps the
	// backing array, so without an ownership copy these writes land on
	// top of what the player is still reading.
	second := bytes.Repeat([]byte{0x02}, 640)
	if _, err := sink.Write(second); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	if !bytes.Equal(handedOff, first) {
		t.Fatalf("a later Write mutated the burst already handed to the player: "+
			"len=%d first byte=%#x", len(handedOff), firstByte(handedOff))
	}
	if bytes.Equal(handedOff, second) {
		t.Fatalf("handed-off burst aliases the sink's buffer (byte-identical to the next burst)")
	}
	if sink.Pending() != len(second) {
		t.Fatalf("Pending = %d, want %d (the second burst must stay queued)", sink.Pending(), len(second))
	}
}

func firstByte(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// Flush with no pending bytes stays a no-op: no player call, nil error.
func TestSpeakerSinkFlushEmptyIsNoop(t *testing.T) {
	fp := &handoffPlayer{}
	sink := NewSpeakerSink(fp, time.Hour)
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush empty: %v", err)
	}
	if len(fp.bursts) != 0 {
		t.Fatalf("empty flush must not reach the player, got %d", len(fp.bursts))
	}
}
