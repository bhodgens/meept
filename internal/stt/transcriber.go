// Package stt provides speech-to-text transcription for the Meept TUI and Flutter clients.
//
// It defines the Transcriber interface and ships three engine implementations:
//   - whisper: subprocess invocation of whisper-cli (whisper.cpp)
//   - parakeet: subprocess invocation of parakeet-transcribe (parakeet.cpp)
//   - native: platform-native transcription (macOS Speech framework / Windows SAPI)
//
// All recording and transcription happens client-side; the daemon is not involved.
package stt

import (
	"context"
	"fmt"
)

// Result represents a transcription result returned by an engine.
type Result struct {
	Text       string
	IsFinal    bool
	Confidence float64
}

// Config holds transcription configuration for the stt package.
type Config struct {
	Engine    string
	Language  string
	Whisper   WhisperConfig
	Parakeet  ParakeetConfig
	Native    NativeConfig
	Recording RecordingConfig
}

// WhisperConfig holds whisper.cpp engine settings.
type WhisperConfig struct {
	BinPath   string
	ModelPath string
	Threads   int
}

// ParakeetConfig holds parakeet engine settings.
type ParakeetConfig struct {
	BinPath   string
	ModelPath string
}

// NativeConfig holds OS-native engine settings.
type NativeConfig struct{}

// RecordingConfig holds audio recording settings shared by all engines.
type RecordingConfig struct {
	RecorderBin string
	SampleRate  int
	Channels    int
	Format      string
}

// Transcriber is the interface for speech-to-text engines.
type Transcriber interface {
	// Start begins recording audio and streaming partial results via onResult.
	Start(ctx context.Context, onResult func(Result)) error
	// Stop stops recording and returns the final transcription text.
	Stop() (string, error)
	// IsRecording returns whether the engine is currently recording.
	IsRecording() bool
	// Name returns the engine name for logging and UI display.
	Name() string
}

// NewTranscriber returns the appropriate implementation based on cfg.Engine.
func NewTranscriber(cfg Config) (Transcriber, error) {
	switch cfg.Engine {
	case "whisper":
		return NewWhisperEngine(cfg)
	case "parakeet":
		return NewParakeetEngine(cfg)
	case "native":
		return NewNativeEngine(cfg)
	default:
		return nil, fmt.Errorf("stt: unknown engine %q", cfg.Engine)
	}
}

// CheckAvailable checks if the configured engine's dependencies are available.
// Returns nil if available, or an error describing what is missing.
func CheckAvailable(cfg Config) error {
	switch cfg.Engine {
	case "whisper":
		return checkWhisperAvailable(cfg)
	case "parakeet":
		return checkParakeetAvailable(cfg)
	case "native":
		return checkNativeAvailable()
	default:
		return fmt.Errorf("stt: unknown engine %q", cfg.Engine)
	}
}
