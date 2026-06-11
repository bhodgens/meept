# TTS (Piper) Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Text-to-Speech (TTS) to Meept TUI and Flutter clients using Piper TTS with 'danny' medium voice for reading assistant responses aloud.

**Architecture:** Client-side TTS synthesis using Piper TTS as a subprocess (consistent with existing STT architecture). Audio playback is local; daemon is not involved. Configuration via `tts` section in `meept.json5` and `client.json5`.

**Tech Stack:** Go 1.24 (subprocess invocation, audio playback), Flutter/Dart (audioplayers package), Piper TTS (ONNX-based neural TTS)

---

## Task 1: Configuration Schema and Loading

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `config/meept.json5`
- Modify: `config/client.json5`
- Test: `internal/config/config_test.go` (if exists)

**Step 1: Add TTS config structs to schema.go**

Add after the existing `STTConfig` struct:

```go
// TTSConfig holds text-to-speech settings for client-side speech synthesis.
type TTSConfig struct {
	Enabled  bool   `json:"enabled" toml:"enabled"`
	Engine   string `json:"engine" toml:"engine"` // "piper" | "platform"
	Voice    string `json:"voice" toml:"voice"`   // voice identifier e.g. "danny-medium"
	VoicePath string `json:"voice_path" toml:"voice_path"` // custom voice path

	// Piper-specific settings
	Piper PiperConfig `json:"piper" toml:"piper"`

	// Playback settings
	Playback TTSPlaybackConfig `json:"playback" toml:"playback"`

	// Behavior settings
	Behavior TTSBehaviorConfig `json:"behavior" toml:"behavior"`
}

// PiperConfig holds Piper TTS engine settings.
type PiperConfig struct {
	BinPath    string `json:"bin_path" toml:"bin_path"`
	ModelPath  string `json:"model_path" toml:"model_path"`
	ConfigPath string `json:"config_path" toml:"config_path"`
	Speaker    string `json:"speaker" toml:"speaker"` // for multi-speaker models
}

// TTSPlaybackConfig holds audio playback settings.
type TTSPlaybackConfig struct {
	Volume    float64 `json:"volume" toml:"volume"`       // 0.0 to 1.0
	Rate      float64 `json:"rate" toml:"rate"`           // 0.5 to 2.0
	AudioDevice string `json:"audio_device" toml:"audio_device"`
}

// TTSBehaviorConfig holds TTS behavior settings.
type TTSBehaviorConfig struct {
	ReadOwnMessages   bool `json:"read_own_messages" toml:"read_own_messages"`
	InterruptOnNewMsg bool `json:"interrupt_on_new_msg" toml:"interrupt_on_new_msg"`
	QueueMessages     bool `json:"queue_messages" toml:"queue_messages"`
	MaxQueueSize      int  `json:"max_queue_size" toml:"max_queue_size"`
}
```

Then add the `TTS TTSConfig` field to the main `Config` struct.

**Step 2: Run test to verify config parses correctly**

```bash
go test ./internal/config/... -v -run TestLoadConfig
```

Expected: Tests pass with new fields (or create a simple parse test if none exists).

**Step 3: Add TTS section template to config/meept.json5**

Add after the `stt` section:

```json5
  // Text-to-speech configuration (client-side: TUI and Flutter)
  "tts": {
    "enabled": false,
    "engine": "piper",              // "piper" | "platform"
    "voice": "danny-medium",        // voice identifier

    "piper": {
      "bin_path": "piper",          // path to piper binary
      "model_path": "",             // default: ~/.meept/tts/voices/danny-medium.onnx
      "config_path": "",            // default: model_path + ".json"
      "speaker": "",                // multi-speaker model speaker ID
    },

    "playback": {
      "volume": 1.0,                // 0.0 to 1.0
      "rate": 1.0,                  // 0.5 to 2.0
      "audio_device": "",           // empty = system default
    },

    "behavior": {
      "read_own_messages": false,
      "interrupt_on_new_msg": true,
      "queue_messages": false,
      "max_queue_size": 5,
    },
  },
```

**Step 4: Add TTS section to config/client.json5**

Add new section:

```json5
  // Text-to-speech configuration (TTS)
  "tts": {
    "enabled": false,
    "engine": "piper",
    "voice": "danny-medium",
  },
```

**Step 5: Commit**

```bash
git add internal/config/schema.go config/meept.json5 config/client.json5
git commit -m "feat: add TTS configuration schema"
```

---

## Task 2: Implement Go TTS Package

**Files:**
- Create: `internal/tts/synthesizer.go` (interface + config)
- Create: `internal/tts/piper.go` (Piper engine)
- Create: `internal/tts/platform.go` (platform-native fallback)
- Create: `internal/tts/player.go` (audio playback)
- Create: `internal/tts/manager.go` (lifecycle management)
- Create: `internal/tts/synthesizer_test.go`
- Create: `internal/tts/piper_test.go`

**Step 1: Create synthesizer.go with interface**

```go
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
	"time"
)

// Result represents a TTS synthesis result.
type Result struct {
	AudioPath string        // path to generated WAV file
	AudioData []byte        // raw PCM data
	Duration  time.Duration // audio duration
}

// Config holds TTS configuration.
type Config struct {
	Engine   string
	Voice    string
	VoicePath string
	Piper    PiperConfig
	Playback PlaybackConfig
	Behavior BehaviorConfig
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
	Volume    float64
	Rate      float64
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

// defaultVoicePath returns the default voice model path.
func defaultVoicePath(voice string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".meept", "tts", "voices", voice+".onnx"), nil
}

var logger = slog.Default().With("component", "tts")
```

**Step 2: Run `go build ./internal/tts/...` to verify syntax**

Expected: Errors for undefined functions (NewPiperEngine, NewPlatformEngine) - that's expected.

**Step 3: Create piper.go with PiperEngine implementation**

Full implementation as shown in brainstorming doc (see `docs/brainstorming/2026-06-11-tts-implementation.md`).

**Step 4: Create platform.go with PlatformEngine (stub)**

```go
package tts

import (
	"context"
	"fmt"
	"runtime"
)

// PlatformEngine uses platform-native TTS.
type PlatformEngine struct {
	config Config
}

func NewPlatformEngine(cfg Config) (*PlatformEngine, error) {
	return &PlatformEngine{config: cfg}, nil
}

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

func (e *PlatformEngine) synthesizeMacOS(ctx context.Context, text string) (*Result, error) {
	// Use osascript with say command (simplest, no dependencies)
	// Note: Cannot capture audio, only plays through speakers
	cmd := exec.CommandContext(ctx, "say", "-v", "Daniel", text)
	return &Result{}, cmd.Run()
}

func (e *PlatformEngine) synthesizeWindows(ctx context.Context, text string) (*Result, error) {
	// Use PowerShell with System.Speech.Synthesis
	script := fmt.Sprintf(`
		Add-Type -AssemblyName System.Speech
		$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
		$synth.Speak("%s")
	`, text)
	cmd := exec.CommandContext(ctx, "powershell", "-Command", script)
	return &Result{}, cmd.Run()
}

func (e *PlatformEngine) Play(audio []byte) error {
	return nil // Already played during synthesis
}

func (e *PlatformEngine) Stop() error {
	return nil
}

func (e *PlatformEngine) IsSpeaking() bool {
	return false
}

func (e *PlatformEngine) Name() string {
	return "platform"
}

func (e *PlatformEngine) CheckAvailable() error {
	return nil
}
```

**Step 5: Create player.go for audio playback**

```go
package tts

import (
	"bytes"
	"io"

	"github.com/hajimehoshi/oto/v3"
)

// AudioPlayer handles low-level audio playback.
type AudioPlayer struct {
	config PlaybackConfig
	context *oto.Context
	player  *oto.Player
	mu      sync.Mutex
}

func NewAudioPlayer(cfg PlaybackConfig) *AudioPlayer {
	return &AudioPlayer{config: cfg}
}

func (p *AudioPlayer) Play(audioData []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.context == nil {
		ctx, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate: 22050, // Piper default
			ChannelCount: 1,
			Format: oto.FormatSignedInt16LE,
		})
		if err != nil {
			return err
		}
		p.context = ctx
	}

	var err error
	p.player, err = p.context.NewPlayer(&oto.NewPlayerOptions{
		Source: bytes.NewReader(audioData),
	})
	if err != nil {
		return err
	}

	p.player.Play()
	go func() {
		<-p.player.PlayComplete()
		p.mu.Lock()
		p.player = nil
		p.mu.Unlock()
	}()

	return nil
}

func (p *AudioPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.player != nil {
		p.player.Pause()
	}
	return nil
}

func (p *AudioPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.player != nil && !p.player.IsPaused()
}
```

**Step 6: Create manager.go for lifecycle management**

```go
package tts

import (
	"context"
	"sync"
)

// Manager manages TTS lifecycle and message routing.
type Manager struct {
	mu       sync.RWMutex
	config   Config
	synth    Synthesizer
	queue    []string
	speaking bool
}

func NewManager(cfg Config) (*Manager, error) {
	synth, err := NewSynthesizer(cfg)
	if err != nil {
		return nil, err
	}

	return &Manager{
		config: cfg,
		synth:  synth,
		queue:  make([]string, 0, cfg.Behavior.MaxQueueSize),
	}, nil
}

func (m *Manager) Speak(text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.speaking {
		if m.config.Behavior.InterruptOnNewMsg {
			m.synth.Stop()
			m.speaking = false
		} else if m.config.Behavior.QueueMessages {
			if len(m.queue) >= m.config.Behavior.MaxQueueSize {
				m.queue = m.queue[1:] // Drop oldest
			}
			m.queue = append(m.queue, text)
			return nil
		}
	}

	m.speaking = true
	go func() {
		defer func() {
			m.mu.Lock()
			m.speaking = false
			m.mu.Unlock()
			m.processQueue()
		}()

		ctx := context.Background()
		result, err := m.synth.Synthesize(ctx, text)
		if err != nil {
			logger.Warn("TTS synthesis failed", "error", err)
			return
		}

		if err := m.synth.Play(result.AudioData); err != nil {
			logger.Warn("TTS playback failed", "error", err)
		}
	}()

	return nil
}

func (m *Manager) processQueue() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.speaking = true

		go func() {
			ctx := context.Background()
			result, _ := m.synth.Synthesize(ctx, next)
			m.synth.Play(result.AudioData)
			m.processQueue()
		}()
	}
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speaking = false
	return m.synth.Stop()
}

func (m *Manager) IsSpeaking() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.speaking
}

func (m *Manager) CheckAvailable() error {
	return m.synth.CheckAvailable()
}
```

**Step 7: Write unit tests in synthesizer_test.go**

```go
package tts_test

import (
	"testing"
	"github.com/caimlas/meept/internal/tts"
)

func TestNewSynthesizer_UnknownEngine(t *testing.T) {
	cfg := tts.Config{Engine: "unknown"}
	_, err := tts.NewSynthesizer(cfg)
	if err == nil || err.Error() != `tts: unknown engine "unknown"` {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultVoicePath(t *testing.T) {
	path, err := tts.DefaultVoicePath("danny-medium")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify path structure (don't assume home dir)
	if path == "" {
		t.Error("expected non-empty path")
	}
}
```

**Step 8: Run tests**

```bash
go test ./internal/tts/... -v
```

Expected: Tests pass, coverage reported.

**Step 9: Commit**

```bash
git add internal/tts/*.go
git commit -m "feat: implement TTS package with Piper engine"
```

---

## Task 3: Wire TTS into TUI

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/models/chat.go`
- Modify: `internal/config/schema.go` (already done)
- Create: `internal/configui/sections_tts.go`

**Step 1: Initialize TTS manager in app.go**

Add TTS manager field to `App` struct and initialize in `NewApp()`.

**Step 2: Add TTS state to ChatModel**

Add `ttsManager *tts.Manager` and `ttsEnabled bool` fields.

**Step 3: Wire message bus listener**

When assistant messages arrive, trigger TTS if enabled.

**Step 4: Add visual indicator**

Show speaker icon when TTS is active.

**Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/models/chat.go
git commit -m "feat: wire TTS into TUI chat"
```

---

## Task 4: Add TTS Config UI

**Files:**
- Create: `internal/configui/sections_tts.go`

**Step 1: Create TTS config section component**

Implement TUI form for TTS settings (enabled, engine, voice, volume, behavior toggles).

**Step 2: Register with config editor**

Wire into `meept config` command.

**Step 3: Commit**

```bash
git add internal/configui/sections_tts.go
git commit -m "feat: add TTS config UI section"
```

---

## Task 5: Flutter TTS Service

**Files:**
- Create: `ui/flutter_ui/lib/services/tts_service.dart`
- Create: `ui/flutter_ui/lib/providers/tts_provider.dart`
- Modify: `ui/flutter_ui/pubspec.yaml`
- Create: `ui/flutter_ui/lib/features/settings/tts_settings.dart`

**Step 1: Add audioplayers dependency**

```bash
cd ui/flutter_ui
flutter pub add audioplayers
```

**Step 2: Implement tts_service.dart**

As shown in brainstorming doc.

**Step 3: Create tts_provider.dart**

Riverpod provider for TTS state.

**Step 4: Add TTS settings panel**

Create settings UI.

**Step 5: Wire into chat_input.dart**

Add TTS toggle button.

**Step 6: Commit**

```bash
git add ui/flutter_ui/lib/services/tts_service.dart ui/flutter_ui/lib/providers/tts_provider.dart ui/flutter_ui/lib/features/settings/tts_settings.dart ui/flutter_ui/pubspec.yaml
git commit -m "feat: add Flutter TTS service"
```

---

## Task 6: CLI Voice Management

**Files:**
- Create: `cmd/meept/tts.go`
- Create: `cmd/meept/tts_voices.go`

**Step 1: Implement voice download command**

```bash
meept tts voices download danny-medium
```

**Step 2: Implement voice list command**

```bash
meept tts voices list
```

**Step 3: Commit**

```bash
git add cmd/meept/tts.go cmd/meept/tts_voices.go
git commit -m "feat: add TTS voice management CLI"
```

---

## Task 7: Documentation

**Files:**
- Create: `docs/workflows/tts.md`
- Modify: `CLAUDE.md`
- Modify: `docs/configuration/index.md`

**Step 1: Add TTS to CLAUDE.md project structure**

**Step 2: Write feature spec docs/workflows/tts.md**

**Step 3: Update configuration docs**

**Step 4: Commit**

```bash
git add docs/workflows/tts.md CLAUDE.md docs/configuration/index.md
git commit -m "docs: add TTS documentation"
```

---

## Task 8: Final Verification

**Step 1: Build everything**

```bash
make build
```

**Step 2: Run all tests**

```bash
go test ./... -v
```

**Step 3: Manual test TUI**

```bash
./bin/meept config tts  # Open config
./bin/meept tts voices download danny-medium  # Download voice
```

**Step 4: Manual test Flutter**

```bash
cd ui/flutter_ui
flutter run
```

**Step 5: Commit**

```bash
git commit -am "chore: final verification and integration"
```

---

## Testing Checklist

- [ ] Config parses correctly with TTS section
- [ ] Piper engine synthesized audio (mock test)
- [ ] Platform engine works on macOS
- [ ] TTS triggers on assistant messages
- [ ] Interrupt behavior works
- [ ] Queue behavior works
- [ ] Voice download succeeds
- [ ] Flutter TTS toggle works
- [ ] Settings panel saves changes
