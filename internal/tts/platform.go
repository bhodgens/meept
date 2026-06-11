package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
)

// PlatformEngine uses platform-native TTS.
type PlatformEngine struct {
	config Config
}

// NewPlatformEngine creates a new platform-native TTS engine.
func NewPlatformEngine(cfg Config) (*PlatformEngine, error) {
	return &PlatformEngine{config: cfg}, nil
}

// Synthesize generates speech using platform-native TTS.
func (e *PlatformEngine) Synthesize(ctx context.Context, text string) (*Result, error) {
	switch runtime.GOOS {
	case "darwin":
		return e.synthesizeMacOS(ctx, text)
	case "windows":
		return e.synthesizeWindows(ctx, text)
	default:
		return nil, fmt.Errorf("platform TTS not supported on %s", runtime.GOOS)
	}
}

// synthesizeMacOS uses the macOS `say` command.
func (e *PlatformEngine) synthesizeMacOS(ctx context.Context, text string) (*Result, error) {
	// Use the `say` command which plays through speakers
	// Note: Cannot capture audio to file without additional tools
	cmd := exec.CommandContext(ctx, "say", "-v", "Daniel", text)
	return &Result{}, cmd.Run()
}

// synthesizeWindows uses PowerShell with System.Speech.Synthesis.
func (e *PlatformEngine) synthesizeWindows(ctx context.Context, text string) (*Result, error) {
	// Escape text for PowerShell
	escapedText, _ := json.Marshal(text)
	script := fmt.Sprintf(`
		Add-Type -AssemblyName System.Speech
		$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
		$synth.Speak(%s)
	`, string(escapedText))
	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	return &Result{}, cmd.Run()
}

// Play is a no-op for platform engines as audio is played during synthesis.
func (e *PlatformEngine) Play(audio []byte) error {
	return nil
}

// Stop is a no-op for platform engines.
func (e *PlatformEngine) Stop() error {
	return nil
}

// IsSpeaking returns false for platform engines (no state tracking).
func (e *PlatformEngine) IsSpeaking() bool {
	return false
}

// Name returns the engine name.
func (e *PlatformEngine) Name() string {
	return "platform"
}

// CheckAvailable checks if platform TTS is available.
func (e *PlatformEngine) CheckAvailable() error {
	switch runtime.GOOS {
	case "darwin", "windows":
		return nil
	default:
		return fmt.Errorf("platform TTS not supported on %s", runtime.GOOS)
	}
}
