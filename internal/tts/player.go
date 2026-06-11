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
