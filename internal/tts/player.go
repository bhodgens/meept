package tts

import (
	"bytes"
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// AudioPlayer handles low-level audio playback using oto.
type AudioPlayer struct {
	config PlaybackConfig
	ctx    *oto.Context
	mu     sync.Mutex
}

// NewAudioPlayer creates a new audio player with the given configuration.
func NewAudioPlayer(cfg PlaybackConfig) *AudioPlayer {
	return &AudioPlayer{config: cfg}
}

// Play plays audio data (WAV/PCM) through the system audio output.
func (p *AudioPlayer) Play(audioData []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Initialize context if not already done
	if p.ctx == nil {
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:   22050, // Piper default
			ChannelCount: 1,
			Format:       oto.FormatSignedInt16LE,
		})
		if err != nil {
			return err
		}
		p.ctx = ctx
		<-ready
	}

	player := p.ctx.NewPlayer(io.Reader(bytes.NewReader(audioData)))
	player.Play()

	return nil
}

// Stop is a no-op for the current oto API (playback runs to completion).
func (p *AudioPlayer) Stop() error {
	return nil
}

// IsPlaying always returns false as we don't track playback state.
func (p *AudioPlayer) IsPlaying() bool {
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

	// Suspend the audio context — oto v3 has no Close(); Suspend halts
	// the audio driver. The GC finalizer handles the rest.
	if err := p.ctx.Suspend(); err != nil {
		return err
	}
	p.ctx = nil
	return nil
}
