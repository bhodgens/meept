package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// PiperEngine implements TTS using Piper TTS (rhasspy/piper).
type PiperEngine struct {
	config     Config
	binPath    string
	modelPath  string
	configPath string
	speaker    string
	player     *AudioPlayer
	mu         sync.Mutex
	speaking   bool
}

// NewPiperEngine creates a new Piper TTS engine.
func NewPiperEngine(cfg Config) (*PiperEngine, error) {
	// Validate binary exists
	binPath := cfg.Piper.BinPath
	if binPath == "" {
		binPath = "piper"
	}
	if _, err := exec.LookPath(binPath); err != nil {
		return nil, fmt.Errorf("piper binary not found: %w", err)
	}

	// Validate model exists
	modelPath := cfg.Piper.ModelPath
	if modelPath == "" {
		var err error
		modelPath, err = DefaultVoicePath(cfg.Voice)
		if err != nil {
			return nil, fmt.Errorf("getting default voice path: %w", err)
		}
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("piper model not found at %q: %w", modelPath, err)
	}

	// Config file is alongside the model
	configPath := cfg.Piper.ConfigPath
	if configPath == "" {
		configPath = modelPath + ".json"
	}

	return &PiperEngine{
		config:     cfg,
		binPath:    binPath,
		modelPath:  modelPath,
		configPath: configPath,
		speaker:    cfg.Piper.Speaker,
		player:     NewAudioPlayer(cfg.Playback),
	}, nil
}

// Synthesize generates speech from text using Piper TTS.
// It runs piper as a subprocess, captures the WAV output, and plays it.
func (e *PiperEngine) Synthesize(ctx context.Context, text string) (*Result, error) {
	e.mu.Lock()
	if e.speaking {
		e.mu.Unlock()
		if e.config.Behavior.InterruptOnNewMsg {
			e.Stop()
		} else if e.config.Behavior.QueueMessages {
			// Queue for later - simplified handling
			return nil, fmt.Errorf("queue not implemented in basic engine")
		}
		e.mu.Lock()
	}
	e.speaking = true
	e.mu.Unlock()

	// Create temp file for audio output
	tmpFile, err := os.CreateTemp("", "meept-tts-*.wav")
	if err != nil {
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Build piper command
	args := []string{
		"-m", e.modelPath,
		"-c", e.configPath,
		"-f", tmpPath,
	}
	if e.speaker != "" {
		args = append(args, "-s", e.speaker)
	}

	cmd := exec.CommandContext(ctx, e.binPath, args...)

	// Pipe text to stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, fmt.Errorf("starting piper: %w", err)
	}

	// Write text to stdin
	if _, err := io.WriteString(stdin, text); err != nil {
		stdin.Close()
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, fmt.Errorf("writing text to piper: %w", err)
	}
	stdin.Close()

	// Wait for synthesis to complete
	if err := cmd.Wait(); err != nil {
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, fmt.Errorf("piper synthesis: %w", err)
	}

	// Read audio file
	audioData, err := os.ReadFile(tmpPath)
	if err != nil {
		e.mu.Lock()
		e.speaking = false
		e.mu.Unlock()
		return nil, fmt.Errorf("reading audio file: %w", err)
	}

	// Play audio
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.player.Play(audioData); err != nil {
		logger.Warn("TTS playback failed", "error", err)
	}

	return &Result{
		AudioPath: tmpPath,
		AudioData: audioData,
	}, nil
}

// Play plays audio data through the system audio output.
func (e *PiperEngine) Play(audio []byte) error {
	return e.player.Play(audio)
}

// Stop stops playback immediately.
func (e *PiperEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.speaking = false
	return e.player.Stop()
}

// IsSpeaking returns whether audio is currently playing.
func (e *PiperEngine) IsSpeaking() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.speaking || e.player.IsPlaying()
}

// Name returns the engine name.
func (e *PiperEngine) Name() string {
	return "piper"
}

// CheckAvailable checks if Piper and the voice model are available.
func (e *PiperEngine) CheckAvailable() error {
	// Check binary
	if _, err := exec.LookPath(e.binPath); err != nil {
		return fmt.Errorf("piper binary not found: %w", err)
	}

	// Check model
	if _, err := os.Stat(e.modelPath); err != nil {
		return fmt.Errorf("voice model not found: %w", err)
	}

	// Check config
	if _, err := os.Stat(e.configPath); err != nil {
		return fmt.Errorf("voice config not found: %w", err)
	}

	return nil
}

// checkPiperAvailable checks if piper binary is available with optional path.
func checkPiperAvailable(binPath string) error {
	if binPath == "" {
		binPath = "piper"
	}
	if _, err := exec.LookPath(binPath); err != nil {
		return fmt.Errorf("piper not found in PATH: %w", err)
	}
	return nil
}

// readPiperConfig reads a Piper voice configuration file.
func readPiperConfig(configPath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// Simple JSON parsing for config
	var config map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}

	return config, nil
}
