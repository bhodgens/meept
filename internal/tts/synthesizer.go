// Package tts provides client-side text-to-speech synthesis.
//
// It defines the Synthesizer interface and ships with two implementations:
//   - piper: subprocess invocation of piper (rhasspy/piper)
//   - platform: platform-native TTS (macOS AVSpeechSynthesizer, Windows SAPI)
//
// All synthesis and playback happens client-side; the daemon is not involved.
package tts

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Result represents a TTS synthesis result.
type Result struct {
	AudioPath string // path to generated WAV file
	AudioData []byte // raw PCM data
}

// Config holds TTS configuration.
type Config struct {
	Engine    string
	Voice     string
	VoicePath string
	Piper     PiperConfig
	Playback  PlaybackConfig
	Behavior  BehaviorConfig
}

// PiperConfig holds Piper TTS engine settings.
type PiperConfig struct {
	BinPath    string
	ModelPath  string
	ConfigPath string
	Speaker    string
}

// PlaybackConfig holds audio playback settings.
type PlaybackConfig struct {
	Volume      float64
	Rate        float64
	AudioDevice string
}

// BehaviorConfig holds TTS behavior settings.
type BehaviorConfig struct {
	ReadOwnMessages   bool
	InterruptOnNewMsg bool
	QueueMessages     bool
	MaxQueueSize      int
}

// Synthesizer is the interface for TTS engines.
type Synthesizer interface {
	// Synthesize generates speech from text and returns audio.
	Synthesize(ctx context.Context, text string) (*Result, error)
	// Play plays audio data through system audio.
	Play(audio []byte) error
	// Stop stops playback immediately.
	Stop() error
	// IsSpeaking returns whether audio is currently playing.
	IsSpeaking() bool
	// Name returns the engine name.
	Name() string
	// CheckAvailable returns nil if engine dependencies are available.
	CheckAvailable() error
	// Close releases any engine-owned audio resources.
	// Implementations that hold no resources should return nil.
	Close() error
}

// NewSynthesizer returns the appropriate implementation based on cfg.Engine.
func NewSynthesizer(cfg Config) (Synthesizer, error) {
	switch cfg.Engine {
	case "piper":
		return NewPiperEngine(cfg)
	case "platform":
		return NewPlatformEngine(cfg)
	default:
		return nil, fmt.Errorf("tts: unknown engine %q", cfg.Engine)
	}
}

// DefaultVoicePath returns the default voice model path for a given voice name.
func DefaultVoicePath(voice string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".meept", "tts", "voices", voice+".onnx"), nil
}

var logger = slog.Default().With("component", "tts")
