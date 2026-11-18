package tts

// player_mutex_scope_test.go - the mutex covers map bookkeeping ONLY, never
// the driver I/O (barge-in latency contract).
//
// Play used to hold p.mu across newContext (which opens/waits for the audio
// device) and NewPlayer/Play. Two consequences, both pinned here:
//  1. the repo's "never hold a mutex across I/O" rule was violated, and
//  2. a barge-in Stop() queued behind an in-flight Play - i.e. the barge-in
//     contract's ~20 ms silence could not be met while the device was still
//     coming up.
//
// The fix releases the lock before the driver calls, so a Stop landing while
// a Play is mid-setup returns promptly instead of waiting for the driver.
// Note the phase split this pins: newContext (the device OPEN) runs UNDER
// the lock in phase 1 - re-checked, so concurrent Plays cannot double-init -
// while only NewPlayer/Play run lock-free in phase 2. The slow-driver test
// therefore stalls NewPlayer, the lock-free driver call, which is exactly
// where a wedged setup must NOT make Stop wait.

import (
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// slowDriver models an audio device whose player creation blocks: its
// NewPlayer seam parks until released - exactly the phase-2 lock-free
// driver I/O. A Stop landing during the stall must still return, proving
// the mutex no longer covers the driver call.
type slowDriver struct {
	entered chan struct{} // closed when a player is being created
	release chan struct{} // the test closes this to let NewPlayer finish
	missed  *fakeBurst    // the burst Stop raced past (phase-3 insert)
}

func (d *slowDriver) NewPlayer(_ io.Reader) burstPlayer {
	// Only the FIRST player creation stalls (models the slow device
	// open); later calls — e.g. the follow-up Play — return instantly.
	select {
	case <-d.entered: // already fired: fast path
	default:
		close(d.entered)
		<-d.release
	}
	d.missed = &fakeBurst{}
	return d.missed
}

func (d *slowDriver) Suspend() error { return nil }

func TestAudioPlayerStopNotBlockedBehindDriverSetup(t *testing.T) {
	d := &slowDriver{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	withFakeDriver(t, d)
	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})

	playDone := make(chan error, 1)
	go func() { playDone <- p.Play([]byte("burst")) }()

	// Wait until Play is inside the driver call (lock NOT held).
	select {
	case <-d.entered:
	case <-time.After(2 * time.Second):
		close(d.release)
		t.Fatalf("Play never reached the driver call")
	}

	// The pin: Stop must return while the driver call is still in flight.
	// Under the old lock-across-I/O shape this blocks until d.release.
	stopDone := make(chan error, 1)
	go func() { stopDone <- p.Stop() }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("stop during setup: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		close(d.release)
		<-playDone
		t.Fatalf("Stop blocked behind an in-flight Play's driver I/O (mutex held across the driver call)")
	}

	close(d.release)
	if err := <-playDone; err != nil {
		t.Fatalf("play: %v", err)
	}

	// The burst Stop raced past is inserted in phase 3 AFTER Stop ran, so
	// Stop never saw it live (the set was empty) and could not pause it.
	if d.missed.pauseN != 0 {
		t.Fatalf("Stop raced past this burst; it must not have been paused: %+v", d.missed)
	}
	if !p.IsPlaying() {
		t.Fatalf("the raced-past burst stays live until a later prune")
	}

	// Model the mixer finishing it (reader EOF: oto's own stop): the next
	// Play's phase-1 prune must DROP the finished burst from the live set
	// without pausing it - pruning forgets, it never interrupts. And once
	// pruned, nothing is playing: the Stop-missed burst never survives.
	d.missed.playing = false
	if err := p.Play([]byte("next")); err != nil {
		t.Fatalf("follow-up play: %v", err)
	}
	if d.missed.pauseN != 0 {
		t.Fatalf("pruning a finished burst must not pause it: %+v", d.missed)
	}
	p.mu.Lock()
	live := len(p.live)
	p.mu.Unlock()
	if live != 1 {
		t.Fatalf("prune must drop the raced-past burst, leaving only the fresh one: live=%d", live)
	}
}

// countingDriver hands out finished fakeBursts and counts player
// creations with an atomic, because the lock-free phase-2 calls hit it
// from concurrent Plays. Context opens are counted by the swapped
// newContext wrapper itself in the test below.
type countingDriver struct {
	players atomic.Int32
}

func (d *countingDriver) NewPlayer(_ io.Reader) burstPlayer {
	d.players.Add(1)
	return &fakeBurst{}
}

func (d *countingDriver) Suspend() error { return nil }

// TestAudioPlayerConcurrentPlayOpensOneContext pins the phase-1 re-check:
// with 8 concurrent Plays, exactly ONE context is opened (the first
// entrant publishes it under the lock; the rest find p.ctx non-nil
// there) while all 8 bursts still reach the lock-free driver call.
func TestAudioPlayerConcurrentPlayOpensOneContext(t *testing.T) {
	driver := &countingDriver{}

	// withFakeDriver swaps newContext outright but does not count the
	// factory runs, so wrap the seam directly: the phase-1 re-check under
	// the lock must guarantee the wrapper body runs exactly once.
	orig := newContext
	opened := atomic.Int32{}
	newContext = func(PlaybackConfig) (audioDriver, error) {
		opened.Add(1)
		return driver, nil
	}
	t.Cleanup(func() { newContext = orig })

	p := NewAudioPlayer(PlaybackConfig{SampleRate: 16000})

	done := make(chan error, 8)
	for i := range 8 {
		go func(i int) { done <- p.Play([]byte{byte(i)}) }(i) //nolint:gosec // G115: i ranges 0..7, the byte conversion never truncates
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatalf("concurrent play: %v", err)
		}
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("concurrent Play opened %d contexts, want exactly 1", got)
	}
	if got := driver.players.Load(); got != 8 {
		t.Fatalf("driver created %d players, want 8", got)
	}
}
