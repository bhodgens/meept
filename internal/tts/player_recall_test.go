package tts

// player_recall_test.go - cancellable-playback bookkeeping, driven
// through the private burstPlayer/audioDriver seams because oto cannot
// open a real audio context headlessly (CI has no output device).
//
// Contract (amended 2026-11-09 after the leaf-01 chop regression): Play
// NEVER pauses previous bursts - successive bursts overlap in oto's
// mixer, which is what makes paced real-time audio seamless. Cancellation
// lives in Stop()/Close(): they pause EVERY live burst, so a barge-in
// silences the speakers at once. Finished bursts (IsPlaying false, the
// mux's EOF behavior) are pruned lazily on the next Play. Pins: overlap,
// stop-pauses-all + idempotent/nil-safe, prune-then-stop leaves finished
// bursts untouched, IsPlaying tracks any-live, Close halts all before
// suspending the driver.

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

func TestAudioPlayerPlayOverlapsPreviousBursts(t *testing.T) {
	d := &fakeDriver{}
	withFakeDriver(t, d)
	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})

	if err := p.Play([]byte("burst-one")); err != nil {
		t.Fatalf("play 1: %v", err)
	}
	first := d.created[0]
	if first.playN != 1 || !first.playing {
		t.Fatalf("first burst must be playing: %+v", first)
	}

	if err := p.Play([]byte("burst-two")); err != nil {
		t.Fatalf("play 2: %v", err)
	}
	second := d.created[1]
	// The chop-regression pin: the old burst is NOT paused when the new
	// one starts - oto's mixer overlaps them so paced audio never loses
	// a tail at a burst boundary.
	if first.pauseN != 0 || !first.playing {
		t.Fatalf("previous burst must keep playing (overlap contract): %+v", first)
	}
	if second.playN != 1 || !second.playing {
		t.Fatalf("new burst must be playing: %+v", second)
	}
	if !p.IsPlaying() {
		t.Fatalf("IsPlaying true while any burst plays")
	}
	// All live bursts finished (mux EOF): nothing playing.
	first.playing = false
	second.playing = false
	if p.IsPlaying() {
		t.Fatalf("IsPlaying false when every burst finished")
	}
	// The next Play prunes the finished set (verified via Stop below:
	// pruning must not Pause anything, it only forgets finished players).
	if err := p.Play([]byte("burst-three")); err != nil {
		t.Fatalf("play 3: %v", err)
	}
	third := d.created[2]
	if first.pauseN != 0 || second.pauseN != 0 {
		t.Fatalf("pruning finished bursts must not pause them: first=%d second=%d", first.pauseN, second.pauseN)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if third.pauseN != 1 {
		t.Fatalf("Stop must pause the live burst: %+v", third)
	}
}

func TestAudioPlayerStopPausesAllLiveAndIsNilSafe(t *testing.T) {
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
	if err := p.Play([]byte("a")); err != nil {
		t.Fatalf("play: %v", err)
	}
	if err := p.Play([]byte("b")); err != nil {
		t.Fatalf("play: %v", err)
	}
	first, second := d.created[0], d.created[1]

	// The barge-in contract: ONE Stop silences every live burst - this
	// is where recall lives now (not in Play).
	if err := p.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if first.pauseN != 1 || second.pauseN != 1 || first.playing || second.playing {
		t.Fatalf("Stop must pause ALL live bursts: %+v %+v", first, second)
	}
	if p.IsPlaying() {
		t.Fatalf("IsPlaying false after Stop")
	}

	// Idempotent: a second Stop pauses nothing new and returns nil.
	if err := p.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if first.pauseN != 1 || second.pauseN != 1 {
		t.Fatalf("Stop must be idempotent: %d %d", first.pauseN, second.pauseN)
	}

	// Stop cleared the live set: the next Play starts clean.
	if err := p.Play([]byte("next")); err != nil {
		t.Fatalf("play after stop: %v", err)
	}
	if d.created[2].playN != 1 {
		t.Fatalf("post-Stop Play must start a fresh burst")
	}
}

func TestAudioPlayerClosePausesBurstsThenSuspends(t *testing.T) {
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
