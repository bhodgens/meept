package tts

import (
	"bytes"
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// burstPlayer is the slice of *oto.Player AudioPlayer drives. PRIVATE
// test seam (leaf 01, recallable playback): oto cannot initialize a real
// audio context headlessly, so the bookkeeping (retain latest, pause on
// Play/Stop) is unit-tested against fakes. Do NOT widen the public API
// with this.
type burstPlayer interface {
	Play()
	Pause()
	IsPlaying() bool
}

// audioDriver is the slice of *oto.Context AudioPlayer uses (same seam
// rationale as burstPlayer).
type audioDriver interface {
	NewPlayer(r io.Reader) burstPlayer
	Suspend() error
}

// otoDriver adapts *oto.Context onto audioDriver (oto's NewPlayer returns
// the concrete *oto.Player, so the seam needs an adapter).
type otoDriver struct{ c *oto.Context }

func (d otoDriver) NewPlayer(r io.Reader) burstPlayer { return d.c.NewPlayer(r) }
func (d otoDriver) Suspend() error                    { return d.c.Suspend() }

// newContext opens the oto audio context and waits for its driver to be
// ready. A package variable so headless tests inject a fake driver;
// production always runs this oto implementation.
var newContext = func(cfg PlaybackConfig) (audioDriver, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   resolveSampleRate(cfg), // MUST match the source PCM rate: oto plays bytes at the context rate, no header parsing, no resampling
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	return otoDriver{c: ctx}, nil
}

// AudioPlayer handles low-level audio output using oto.
//
// Playback model (amended 2026-11-09 after the leaf-01 chop regression):
// successive bursts OVERLAP in oto's mixer - Play never pauses previous
// bursts. Pausing the predecessor per-handoff chopped real-time-paced
// audio at every ~150 ms burst boundary (the tail of burst N died when
// burst N+1 arrived a hair late) - clear-before, choppy-after regression.
// Cancellability is kept at the CANCEL boundary only: Stop() pauses
// EVERY live player, so a barge-in/hangup silences the speakers within
// ~20 ms while normal consecutive playback stays seamless. A finished
// or paused player drops out of the live set lazily;oto's own reader-EOF
// handling stops it and GC reclaims it.
type AudioPlayer struct {
	config PlaybackConfig
	ctx    audioDriver
	live   map[burstPlayer]struct{}
	mu     sync.Mutex
}

// NewAudioPlayer creates a new audio player with the given configuration.
func NewAudioPlayer(cfg PlaybackConfig) *AudioPlayer {
	return &AudioPlayer{config: cfg, live: map[burstPlayer]struct{}{}}
}

// resolveSampleRate maps the playback config onto the oto context rate.
// oto v3 plays raw bytes at the CONTEXT rate (no WAV parsing, no
// resampling), so this MUST match the source PCM rate: the voice local
// leg passes 16000 (its pinned L16@16k wire format); zero/negative keeps
// the historical piper default.
func resolveSampleRate(cfg PlaybackConfig) int {
	if cfg.SampleRate <= 0 {
		return piperSampleRateDefault
	}
	return cfg.SampleRate
}

// Play plays audio data (WAV/PCM) through the system audio output.
// Bursts OVERLAP: previous live bursts are NOT paused (leaf-01's
// per-handoff pause chopped paced real-time audio at every burst
// boundary - the 2026-11-09 chop regression). Cancellation lives in
// Stop(), which pauses every live burst at once.
//
// Mutex scope: the lock covers ONLY the map bookkeeping (lazy ctx init +
// prune + the live-set insert). The driver calls - newContext (which may
// OPEN the audio device and wait for it) and NewPlayer/Play - run with NO
// lock held. Holding the mutex across them violated the repo's
// I/O-under-mutex rule and put a barge-in Stop() behind an in-flight
// Play(), which is exactly the ~20 ms silence target the barge-in contract
// promises. Two concurrent Play calls cannot double-init the context: the
// first publishes it under the lock, the second finds it non-nil there.
func (p *AudioPlayer) Play(audioData []byte) error {
	// Phase 1 - lock: lazy context init (re-checked under the lock so a
	// concurrent Play cannot re-open the device) and the finished-burst
	// prune.
	p.mu.Lock()
	if p.ctx == nil {
		drv, err := newContext(p.config)
		if err != nil {
			p.mu.Unlock()
			return err
		}
		p.ctx = drv
	}
	if p.live == nil {
		p.live = map[burstPlayer]struct{}{}
	}
	// Prune finished bursts lazily on each handoff so the live set stays
	// bounded to the short overlap tail (a burst runs ~150 ms; at most a
	// couple are ever mid-playback).
	for pl := range p.live {
		if !pl.IsPlaying() {
			delete(p.live, pl)
		}
	}
	drv := p.ctx
	p.mu.Unlock()

	// Phase 2 - no lock: the driver I/O. newContext's failure mode and a
	// NewPlayer panic are the only reasons this can fail; a panic here
	// leaves the live set untouched (nothing was inserted yet).
	player := drv.NewPlayer(io.Reader(bytes.NewReader(audioData)))
	player.Play()

	// Phase 3 - lock again, briefly: record the burst. Stop() that lands
	// between phase 2 and here still pauses it (Stop clears the set, and
	// this insert then re-adds a burst Stop already paused - harmless: the
	// player is paused, and the next Play's prune drops it).
	p.mu.Lock()
	p.live[player] = struct{}{}
	p.mu.Unlock()

	return nil
}

// Stop pauses EVERY live burst (leaf 01: recallable playback at the
// CANCEL boundary). Idempotent and safe with nothing live: a no-op
// returning nil. Finished/paused players are dropped from the set.
func (p *AudioPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for pl := range p.live {
		pl.Pause()
		delete(p.live, pl)
	}
	return nil
}

// IsPlaying reports whether ANY burst is currently live and playing.
func (p *AudioPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for pl := range p.live {
		if pl.IsPlaying() {
			return true
		}
	}
	return false
}

// Close releases the oto audio context. oto v3's Context has no Close method,
// but Suspend halts the underlying audio driver. The context reference is
// cleared so subsequent calls are no-ops. Safe to call before any Play()
// (context is nil) or multiple times.
func (p *AudioPlayer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx == nil {
		return nil
	}

	// Halt every live burst before suspending the driver: a playing
	// retained player cannot keep feeding a suspended device.
	for pl := range p.live {
		pl.Pause()
	}
	clear(p.live)

	// Suspend the audio context — oto v3 has no Close(); Suspend halts
	// the audio driver. The GC finalizer handles the rest.
	if err := p.ctx.Suspend(); err != nil {
		return err
	}
	p.ctx = nil
	return nil
}
