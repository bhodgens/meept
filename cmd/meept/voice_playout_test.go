package main

// voice_playout_test.go - leaf 01 (recallable playback) bounds on the
// live-mode CLI drain: the not-yet-played backlog handed to the speaker
// never exceeds liveMaxPendingAudioBytes + one read (steady-state burst
// cap), the tail flushes on socket end, and a play error surfaces via
// done. No real audio device: the sink takes a recording fake player,
// the socket is an io.Reader paced-chunk fake.

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// playoutFakePlayer records every burst handed to the speaker.
type playoutFakePlayer struct {
	bursts [][]byte
	err    error // when set, Play returns it
}

func (f *playoutFakePlayer) Play(burst []byte) error {
	if f.err != nil {
		return f.err
	}
	f.bursts = append(f.bursts, append([]byte(nil), burst...))
	return nil
}
func (f *playoutFakePlayer) Clear() {}
func (f *playoutFakePlayer) Close() {}

// pacedReader emits the backlog in liveSpeakerReadChunk-sized reads then
// io.EOF - the shape of the daemon's 20 ms pacing at the socket.
type pacedReader struct {
	data []byte
}

func (r *pacedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(copy(p, r.data), liveSpeakerReadChunk)
	r.data = r.data[n:]
	return n, nil
}

func TestDrainLiveSpeakerBoundsBacklog(t *testing.T) {
	t.Parallel()
	// ~4 s of paced audio (200 chunks of 640 B) - the old loop handed
	// this ALL to the speaker on the first gap; the bound must slice it
	// into bursts of at most liveMaxPendingAudioBytes + one read.
	total := 200 * liveSpeakerReadChunk
	fp := &playoutFakePlayer{}
	// gap = 1 hour: the ONLY flushes come from the bound and EOF.
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)

	drainLiveSpeaker(&pacedReader{data: bytes.Repeat([]byte{0xAB}, total)}, sink, done, nil)

	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(fp.bursts) == 0 {
		t.Fatalf("no bursts handed to the player")
	}
	maxBurst := liveMaxPendingAudioBytes + liveSpeakerReadChunk
	var sum int
	for i, b := range fp.bursts {
		sum += len(b)
		if len(b) > maxBurst {
			t.Fatalf("burst %d is %d B, over the bound+read (%d B): unbounded backlog regression",
				i, len(b), maxBurst)
		}
	}
	if sum != total {
		t.Fatalf("lost audio across the bound: bursts sum %d, fed %d", sum, total)
	}
	// More than one burst for 4 s of audio = the backlog actually bound
	// (a single-burst drain is the old gap-only behavior).
	if len(fp.bursts) < 2 {
		t.Fatalf("expected the bound to split the stream, got %d burst(s)", len(fp.bursts))
	}
	// Pending backlog never survives the drain.
	if sink.Pending() != 0 {
		t.Fatalf("EOF tail must flush: %d B pending", sink.Pending())
	}
}

func TestDrainLiveSpeakerFlushesTailOnEOF(t *testing.T) {
	t.Parallel()
	fp := &playoutFakePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	// Under the bound: the ONLY handoff is the EOF flush.
	feed := bytes.Repeat([]byte{0x01}, 3*liveSpeakerReadChunk)
	drainLiveSpeaker(&pacedReader{data: feed}, sink, done, nil)
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(fp.bursts) != 1 {
		t.Fatalf("want 1 EOF-flushed burst, got %d", len(fp.bursts))
	}
	if !bytes.Equal(fp.bursts[0], feed) {
		t.Fatalf("tail burst != fed pcm (%d vs %d B)", len(fp.bursts[0]), len(feed))
	}
}

func TestDrainLiveSpeakerPropagatesPlayError(t *testing.T) {
	t.Parallel()
	want := errors.New("speaker gone")
	fp := &playoutFakePlayer{err: want}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	drainLiveSpeaker(&pacedReader{data: bytes.Repeat([]byte{0x02}, 3*liveSpeakerReadChunk)}, sink, done, nil)
	if err := <-done; !errors.Is(err, want) {
		t.Fatalf("play error must surface on done, got %v", err)
	}
}

func TestDrainLiveSpeakerEmptyStreamEndsClean(t *testing.T) {
	t.Parallel()
	fp := &playoutFakePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	drainLiveSpeaker(&pacedReader{data: nil}, sink, done, nil)
	if err := <-done; err != nil {
		t.Fatalf("zero-byte EOF must end clean, got %v", err)
	}
	if len(fp.bursts) != 0 {
		t.Fatalf("nothing was fed; nothing may play")
	}
}
