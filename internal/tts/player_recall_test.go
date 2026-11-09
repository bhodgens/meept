package tts

// player_recall_test.go - leaf 01 (recallable playback) bookkeeping,
// driven through the private burstPlayer/audioDriver seams because oto
// cannot open a real audio context headlessly (CI has no output device).
// Pins: one live burst at a time (Play pauses the retained previous
// player), Stop pauses cur and is idempotent/nil-safe, IsPlaying tracks
// the retained burst, Close halts the burst before suspending the driver.

import (
	"io"
	"testing"
)

// fakeBurst records the oto-player calls AudioPlayer makes.
type fakeBurst struct {
	playN   int
	pauseN  int
	playing bool
}

func (b *fakeBurst) Play()           { b.playN++; b.playing = true }
func (b *fakeBurst) Pause()          { b.pauseN++; b.playing = false }
func (b *fakeBurst) IsPlaying() bool { return b.playing }

// fakeDriver hands out fakeBursts and records Suspend calls.
type fakeDriver struct {
	created   []*fakeBurst
	suspendN  int
	suspendEr error
}

func (d *fakeDriver) NewPlayer(_ io.Reader) burstPlayer {
	b := &fakeBurst{}
	d.created = append(d.created, b)
	return b
}

func (d *fakeDriver) Suspend() error {
	d.suspendN++
	return d.suspendEr
}

// withFakeDriver swaps the package's newContext seam for the test and
// restores it via t.Cleanup.
func withFakeDriver(t *testing.T, d *fakeDriver) {
	t.Helper()
	orig := newContext
	newContext = func(PlaybackConfig) (audioDriver, error) { return d, nil }
	t.Cleanup(func() { newContext = orig })
}

func TestAudioPlayerPlayPausesPreviousBurst(t *testing.T) {
	d := &fakeDriver{}
	withFakeDriver(t, d)
	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})

	if err := p.Play([]byte("burst-one")); err != nil {
		t.Fatalf("play 1: %v", err)
	}
	if len(d.created) != 1 {
		t.Fatalf("want 1 created player, got %d", len(d.created))
	}
	first := d.created[0]
	if first.playN != 1 || !first.playing {
		t.Fatalf("first burst must be playing: %+v", first)
	}

	if err := p.Play([]byte("burst-two")); err != nil {
		t.Fatalf("play 2: %v", err)
	}
	if len(d.created) != 2 {
		t.Fatalf("want 2 created players, got %d", len(d.created))
	}
	second := d.created[1]
	// The talk-over pin: the old burst is PAUSED the moment the new one
	// starts - it never keeps playing under the new answer.
	if first.pauseN != 1 || first.playing {
		t.Fatalf("previous burst must be paused on new Play: %+v", first)
	}
	if second.playN != 1 || !second.playing {
		t.Fatalf("new burst must be playing: %+v", second)
	}
	// Only the latest player is retained: IsPlaying tracks it.
	if !p.IsPlaying() {
		t.Fatalf("IsPlaying must be true while the retained burst plays")
	}
	// The retained burst running out of bytes = no live playback.
	second.playing = false
	if p.IsPlaying() {
		t.Fatalf("IsPlaying must follow the retained player")
	}
}

func TestAudioPlayerStopPausesCurrentAndIsNilSafe(t *testing.T) {
	// Stop before any Play: nil-safe no-op.
	fresh := NewAudioPlayer(PlaybackConfig{})
	if err := fresh.Stop(); err != nil {
		t.Fatalf("Stop before Play: %v", err)
	}
	if fresh.IsPlaying() {
		t.Fatalf("fresh player must not report playing")
	}

	d := &fakeDriver{}
	withFakeDriver(t, d)
	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})
	if err := p.Play([]byte("burst")); err != nil {
		t.Fatalf("play: %v", err)
	}
	burst := d.created[0]

	if err := p.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if burst.pauseN != 1 || burst.playing {
		t.Fatalf("Stop must pause the live burst: %+v", burst)
	}
	if p.IsPlaying() {
		t.Fatalf("IsPlaying false after Stop")
	}

	// Idempotent: a second Stop pauses nothing new and returns nil.
	if err := p.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if burst.pauseN != 1 {
		t.Fatalf("Stop must be idempotent: pauseN=%d", burst.pauseN)
	}

	// Stop clears the retained player: the next Play starts clean and
	// pauses nothing (reuse keeps the same oto context).
	if err := p.Play([]byte("next")); err != nil {
		t.Fatalf("play after stop: %v", err)
	}
	if burst.pauseN != 1 || d.created[1].playN != 1 {
		t.Fatalf("post-Stop Play must not pause the old player again")
	}
}

func TestAudioPlayerClosePausesBurstThenSuspends(t *testing.T) {
	d := &fakeDriver{}
	withFakeDriver(t, d)
	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})
	if err := p.Play([]byte("burst")); err != nil {
		t.Fatalf("play: %v", err)
	}
	burst := d.created[0]

	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if burst.pauseN != 1 {
		t.Fatalf("Close must halt the live burst: %+v", burst)
	}
	if d.suspendN != 1 {
		t.Fatalf("Close must suspend the driver once, got %d", d.suspendN)
	}
	// Context cleared: repeated Close is a no-op.
	if err := p.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if d.suspendN != 1 {
		t.Fatalf("repeated Close must not re-suspend, got %d", d.suspendN)
	}
	// Close before any Play (fresh player): nil-safe.
	if err := NewAudioPlayer(PlaybackConfig{}).Close(); err != nil {
		t.Fatalf("close before play: %v", err)
	}
}

func TestAudioPlayerPlayContextErrorPropagates(t *testing.T) {
	orig := newContext
	t.Cleanup(func() { newContext = orig })
	newContext = func(PlaybackConfig) (audioDriver, error) { return nil, io.ErrClosedPipe }
	p := NewAudioPlayer(PlaybackConfig{})
	if err := p.Play([]byte("x")); err != io.ErrClosedPipe {
		t.Fatalf("context failure must propagate, got %v", err)
	}
	// No burst retained on a failed Play.
	if err := p.Stop(); err != nil {
		t.Fatalf("stop after failed play: %v", err)
	}
}
