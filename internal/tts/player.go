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

// AudioPlayer handles low-level audio playback using oto.
//
// ONE live burst at a time (leaf 01, recallable playback): the player
// retains the newest oto player, so Stop()/the voice Player seam's Clear
// pause exactly what the speakers are holding. A paused player is
// abandoned from that point on — matching how new players were always
// created, GC reclaiming it is the driver's concern.
type AudioPlayer struct {
	config PlaybackConfig
	ctx    audioDriver
	cur    burstPlayer
	mu     sync.Mutex
}

// NewAudioPlayer creates a new audio player with the given configuration.
func NewAudioPlayer(cfg PlaybackConfig) *AudioPlayer {
	return &AudioPlayer{config: cfg}
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

// Play plays audio data (WAV/PCM) through the system audio output. Any
// previously retained burst is PAUSED first (one live burst at a time):
// the tail of the old utterance never keeps playing under the new one
// (the talk-over defect leaf 01 kills).
func (p *AudioPlayer) Play(audioData []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Initialize context if not already done
	if p.ctx == nil {
		drv, err := newContext(p.config)
		if err != nil {
			return err
		}
		p.ctx = drv
	}

	// Pause the previous burst before starting the new one so only the
	// newest audio reaches the speakers.
	if p.cur != nil {
		p.cur.Pause()
	}

	player := p.ctx.NewPlayer(io.Reader(bytes.NewReader(audioData)))
	player.Play()
	p.cur = player

	return nil
}

// Stop pauses the current burst (leaf 01: recallable playback). Idempotent
// and nil-safe: with no burst retained (never played, or already stopped)
// it is a no-op returning nil. The abandoned player is GC's concern,
// matching how new players were previously created per burst.
func (p *AudioPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cur == nil {
		return nil
	}
	p.cur.Pause()
	p.cur = nil
	return nil
}

// IsPlaying reports whether a burst is currently playing. The retained
// player is authoritative once a burst was handed off (it goes non-playing
// when the bytes run out or Stop pauses it).
func (p *AudioPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cur == nil {
		return false
	}
	return p.cur.IsPlaying()
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

	// Halt the current burst before suspending the driver: a paused
	// retained player cannot keep feeding a suspended device.
	if p.cur != nil {
		p.cur.Pause()
		p.cur = nil
	}

	// Suspend the audio context — oto v3 has no Close(); Suspend halts
	// the audio driver. The GC finalizer handles the rest.
	if err := p.ctx.Suspend(); err != nil {
		return err
	}
	p.ctx = nil
	return nil
}
