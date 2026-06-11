# TTS Implementation Brainstorming

**Date**: 2026-06-11
**Goal**: Add Text-to-Speech (TTS) to read dispatcher responses aloud using Piper TTS with the 'danny' medium voice by default.

---

## Overview

Add client-side text-to-speech synthesis to both the TUI and Flutter GUI. The feature will:
- Read LLM/dispatcher responses aloud using Piper TTS
- Use 'danny' medium voice as default
- Be configurable (voice selection, enabled/disabled, rate, volume)
- Work independently of STT (speech-to-text)

---

## Architecture Decision: Where TTS Lives

### Option A: Pure Client-Side (like STT)
**Pros**: Privacy (audio never leaves device), low latency, no daemon involvement
**Cons**: Requires Piper binary + voice models on client, platform-specific wiring

### Option B: Daemon-Side TTS Service
**Pros**: Centralized audio generation, can cache audio files, shared across clients
**Cons**: Requires daemon restart, network transfer of audio, more complex

### Option C: Hybrid (Daemon-managed, Client-cached)
**Pros**: Daemon manages Piper, clients download audio chunks over HTTP
**Cons**: Most complex architecture

**Recommendation**: **Option A - Client-Side** for consistency with existing STT architecture. Piper runs as a subprocess (like whisper-cli), audio plays locally.

---

## Configuration Structure

Add to `meept.json5` and `client.json5`:

```json5
{
  // Text-to-speech configuration (client-side: TUI and Flutter)
  "tts": {
    "enabled": false,
    "engine": "piper",              // "piper" | "platform" (macOS AVSpeechSynthesizer)
    "voice": "danny-medium",        // voice identifier
    "voice_path": "",               // custom voice model path (empty = ~/.meept/tts/voices/)

    "piper": {
      "bin_path": "piper",          // path to piper binary
      "model_path": "",             // default: ~/.meept/tts/voices/danny-medium.onnx
      "config_path": "",            // default: ~/.meept/tts/voices/danny-medium.onnx.json
      "speaker": "",                // multi-speaker model speaker ID (empty = default)
    },

    // Playback settings
    "playback": {
      "volume": 1.0,                // 0.0 to 1.0
      "rate": 1.0,                  // 0.5 to 2.0 (Piper uses speed parameter)
      "audio_device": "",           // empty = system default
    },

    // Behavior settings
    "behavior": {
      "read_own_messages": false,   // false = only read assistant responses
      "interrupt_on_new_message": true, // stop speaking when new message arrives
      "queue_messages": false,      // queue vs interrupt when already speaking
      "max_queue_size": 5,          // max queued messages to speak
    },
  },
}
```

---

## Go TTS Interface Design (`internal/tts/`)

### Core Interface

```go
// internal/tts/synthesizer.go

// Result represents a TTS synthesis result.
type Result struct {
    AudioPath string  // path to generated WAV file (if file-based)
    AudioData  []byte // raw PCM data (if streaming)
    Duration   time.Duration
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

type PiperConfig struct {
    BinPath    string
    ModelPath  string
    ConfigPath string
    Speaker    string
}

type PlaybackConfig struct {
    Volume     float64
    Rate       float64
    AudioDevice string
}

type BehaviorConfig struct {
    ReadOwnMessages     bool
    InterruptOnNewMsg   bool
    QueueMessages       bool
    MaxQueueSize        int
}

// Synthesizer is the interface for TTS engines.
type Synthesizer interface {
    // Synthesize generates speech from text and returns audio.
    // Blocks until synthesis completes.
    Synthesize(ctx context.Context, text string) (*Result, error)

    // SynthesizeStreaming starts async synthesis with callback.
    // Useful for long responses - can start playback before complete.
    SynthesizeStreaming(ctx context.Context, text string, onChunk func([]byte)) error

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
```

### Implementation Files

| File | Purpose |
|------|---------|
| `internal/tts/synthesizer.go` | Interface + Config structs |
| `internal/tts/piper.go` | Piper engine implementation |
| `internal/tts/platform.go` | Platform-native TTS (macOS AVSpeechSynthesizer) |
| `internal/tts/player.go` | Audio playback (shared) |
| `internal/tts/queue.go` | Message queue management |
| `internal/tts/manager.go` | Lifecycle, config loading |
| `internal/tts/synthesizer_test.go` | Unit tests |
| `internal/tts/piper_test.go` | Piper integration tests |

### Piper Engine Implementation (`internal/tts/piper.go`)

```go
type PiperEngine struct {
    config   Config
    binPath  string
    modelPath string
    configPath string
    player   *AudioPlayer
    mu       sync.Mutex
    speaking bool
    stopCh   chan struct{}
}

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
        home, _ := os.UserHomeDir()
        modelPath = filepath.Join(home, ".meept", "tts", "voices", "danny-medium.onnx")
    }
    if _, err := os.Stat(modelPath); err != nil {
        return nil, fmt.Errorf("piper model not found: %w", err)
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
        player:     NewAudioPlayer(cfg.Playback),
    }, nil
}

func (e *PiperEngine) Synthesize(ctx context.Context, text string) (*Result, error) {
    e.mu.Lock()
    if e.speaking {
        e.mu.Unlock()
        if e.config.Behavior.InterruptOnNewMsg {
            e.Stop()
        } else if e.config.Behavior.QueueMessages {
            // Queue for later
            return nil, errQueueFull // or queue it
        }
        e.mu.Lock()
    }
    e.speaking = true
    e.mu.Unlock()

    // Create temp file for audio output
    tmpFile, err := os.CreateTemp("", "meept-tts-*.wav")
    if err != nil {
        return nil, err
    }
    defer os.Remove(tmpFile.Name())

    // Run piper: piper -m model.onnx -c config.json -f output.wav
    cmd := exec.CommandContext(ctx, e.binPath,
        "-m", e.modelPath,
        "-c", e.configPath,
        "-f", tmpFile.Name(),
    )

    // Pipe text to stdin (piper reads from stdin)
    stdin, err := cmd.StdinPipe()
    if err != nil {
        return nil, err
    }

    if err := cmd.Start(); err != nil {
        return nil, err
    }

    // Write text to stdin
    if _, err := io.WriteString(stdin, text); err != nil {
        stdin.Close()
        return nil, err
    }
    stdin.Close()

    // Wait for synthesis to complete
    if err := cmd.Wait(); err != nil {
        return nil, err
    }

    // Read audio file
    audioData, err := os.ReadFile(tmpFile.Name())
    if err != nil {
        return nil, err
    }

    // Calculate duration from WAV header
    duration, _ := parseWavDuration(audioData)

    e.mu.Lock()
    e.player.Play(audioData)
    e.mu.Unlock()

    return &Result{
        AudioPath: tmpFile.Name(),
        AudioData: audioData,
        Duration:  duration,
    }, nil
}

func (e *PiperEngine) Stop() error {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.speaking = false
    return e.player.Stop()
}
```

### Platform-Native Engine (`internal/tts/platform.go`)

```go
// macOS: Uses AVSpeechSynthesizer via osascript or Swift helper
// Windows: Uses System.Speech.Synthesis via PowerShell
// Linux: Not supported (fall back to Piper)

type PlatformEngine struct {
    config Config
    // macOS: NSSpeechSynthesizer via NSScriptCommand
    // Windows: SpeechSynthesizer via PowerShell
}

func (e *PlatformEngine) Synthesize(ctx context.Context, text string) (*Result, error) {
    // macOS: use osascript with AVSpeechSynthesizer
    // Note: Limited control, but no dependencies
    script := fmt.Sprintf(`
        use framework "AVFoundation"
        use scripting
        tell application "System Events"
            set synth to current application's AVSpeechSynthesizer's new()
            set utterance to current application's AVSpeechUtterance's speechUtteranceWithString:%q
            speak utterance using synth
        end tell
    `, text)

    cmd := exec.Command("osascript", "-e", script)
    return &Result{}, cmd.Run()
}
```

---

## TUI Integration (`internal/tui/`)

### Files to Modify

| File | Changes |
|------|---------|
| `internal/tui/app.go` | Add TTS manager initialization, message bus listener |
| `internal/tui/models/chat.go` | Add TTS state, message rendering hook |
| `internal/tui/render/message.go` | Add TTS toggle per-message |
| `internal/config/schema.go` | Add TTSConfig struct |
| `internal/configui/sections_tts.go` | New file: TTS config UI |

### Chat Model Changes

```go
// internal/tui/models/chat.go

type ChatModel struct {
    // ... existing fields ...
    ttsManager   *tts.Manager
    ttsState     ttsState
    ttsQueue     []string
}

type ttsState int

const (
    ttsIdle ttsState = iota
    ttsSpeaking
    ttsPaused
)

// In Update():
case TTSMessageMsg:
    // New message arrived - check if should speak
    if msg.From == "assistant" && chatModel.ttsEnabled {
        if chatModel.behavior.InterruptOnNewMsg {
            chatModel.ttsManager.Stop()
        }
        if !chatModel.ttsManager.IsSpeaking() {
            chatModel.ttsManager.Synthesize(msg.Text)
        } else if chatModel.behavior.QueueMessages {
            chatModel.ttsQueue = append(chatModel.ttsQueue, msg.Text)
        }
    }
```

### Visual Indicators

When TTS is speaking:
- Add small speaker icon in top-right of viewport
- Orange pulse animation around viewport border
- "speaking..." indicator in status bar

---

## Flutter Integration (`ui/flutter_ui/`)

### New Files

| File | Purpose |
|------|---------|
| `ui/flutter_ui/lib/services/tts_service.dart` | Piper wrapper + platform TTS |
| `ui/flutter_ui/lib/providers/tts_provider.dart` | Riverpod provider |
| `ui/flutter_ui/lib/features/settings/tts_settings.dart` | TTS config panel |

### TTS Service

```dart
import 'dart:io';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

/// Text-to-speech service using Piper TTS.
class TtsService {
  Process? _piperProcess;
  bool _isSpeaking = false;
  AudioPlayer? _audioPlayer; // from audioplayers package
  TtsConfig _config;

  Future<bool> initialize() async {
    // Check Piper binary
    final result = await Process.run('which', ['piper']);
    if (result.exitCode != 0) {
      // Piper not available
      return false;
    }

    // Validate voice model
    final homeDir = await getApplicationDocumentsDirectory();
    final voicePath = '${homeDir.path}/.meept/tts/voices/danny-medium.onnx';
    if (!await File(voicePath).exists()) {
      // Voice not downloaded
      return false;
    }

    _audioPlayer = AudioPlayer();
    return true;
  }

  Future<void> speak(String text) async {
    if (_isSpeaking && _config.interruptOnNewMessage) {
      await stop();
    }

    // Run Piper to generate audio
    final tempDir = await getTemporaryDirectory();
    final outputPath = '${tempDir.path}/meept-tts-${DateTime.now().millisecondsSinceEpoch}.wav';

    _piperProcess = await Process.start(
      'piper',
      ['-m', _config.modelPath, '-c', '${_config.modelPath}.json', '-f', outputPath],
    );

    _piperProcess!.stdin.write(text);
    await _piperProcess!.stdin.close();

    await _piperProcess!.exitCode;

    // Play the audio file
    await _audioPlayer!.play(DeviceFileSpeaker(outputPath));
    _isSpeaking = true;

    _audioPlayer!.onPlayerComplete.listen((_) {
      _isSpeaking = false;
      File(outputPath).delete().ignore();
    });
  }

  Future<void> stop() async {
    await _audioPlayer?.stop();
    _piperProcess?.kill();
    _isSpeaking = false;
  }

  bool get isSpeaking => _isSpeaking;
}
```

### pubspec.yaml Dependencies

```yaml
dependencies:
  audioplayers: ^6.0.0  # Audio playback
  path_provider: ^2.1.0 # File paths
  speech_to_text: ^7.0.0 # Already present for STT
```

---

## Piper Installation & Voice Management

### Voice Download Command

Add CLI command: `meept tts voices`

```bash
# List available voices
meept tts voices list

# Download a voice
meept tts voices download danny-medium

# Remove a voice
meept tts voices remove danny-medium

# Update voices
meept tts voices update
```

Voice files from: https://github.com/rhasspy/piper-voices

Default voice: `danny-medium` (EN, medium quality, ~80MB)

---

## Configuration UI

### TUI Config Editor (`internal/configui/sections_tts.go`)

```
┌─ TTS Configuration ───────────────────┐
│                                       │
│  Enabled: [✓] Yes  [ ] No            │
│                                       │
│  Engine:  ● piper   ○ platform       │
│                                       │
│  Voice:   danny-medium          [▼]  │
│                                       │
│  Volume:  [████████░░] 80%           │
│  Rate:    [██████░░░░] 1.0x          │
│                                       │
│  [ ] Read own messages               │
│  [✓] Interrupt on new message        │
│  [ ] Queue messages                  │
│                                       │
│  [Save]  [Cancel]  [Test Voice]      │
└───────────────────────────────────────┘
```

### Flutter Settings Panel

Similar layout in `ui/flutter_ui/lib/features/settings/tts_settings.dart`

---

## Testing Strategy

### Unit Tests
- `internal/tts/synthesizer_test.go`: Config parsing, NewSynthesizer factory
- `internal/tts/piper_test.go`: Piper subprocess invocation, error handling
- `internal/tts/queue_test.go`: Message queue behavior

### Integration Tests
- TUI: Send message, verify TTS triggers (mock Piper)
- Flutter: Widget tests for TTS toggle, settings panel

### Manual Tests
- Voice download and playback
- Interrupt behavior
- Queue behavior with multiple messages
- Platform fallback (no Piper)

---

## Implementation Phases

### Phase 1: Core Infrastructure
1. Add TTS config to schema
2. Implement `internal/tts/` package (interface, Piper engine)
3. Add Piper voice download command
4. Write unit tests

### Phase 2: TUI Integration
1. Wire TTS into chat model
2. Add visual indicators
3. Add config UI section
4. Test end-to-end

### Phase 3: Flutter Integration
1. Implement `tts_service.dart`
2. Add Riverpod provider
3. Add settings panel
4. Add TTS toggle to chat view

### Phase 4: Polish
1. Documentation updates
2. Voice selection UI
3. Performance optimization (streaming synthesis)
4. Audio caching

---

## Open Questions

1. **Piper binary distribution**: Bundle with Meept? Require manual install?
2. **Voice model storage**: `~/.meept/tts/voices/`? Download on first use?
3. **Streaming vs batch**: Start speaking before full response generated?
4. **SSML support**: Piper supports SSML - expose rate/pitch/emphasis controls?
5. **Multiple voices**: Per-agent voice assignment?

---

## Dependencies

| Component | Dependency | Install |
|-----------|-----------|---------|
| Piper TTS | `piper` binary | https://github.com/rhasspy/piper |
| Piper voices | ONNX voice models | `meept tts voices download` |
| Audio playback (Go) | `github.com/hajimehoshi/go-mp3` or `oto` | go get |
| Audio playback (Flutter) | `audioplayers` | pubspec.yaml |
| Platform TTS | macOS/iOS native | Built-in |

---

## Related Files to Update

- `CLAUDE.md`: Add `internal/tts` to project structure
- `docs/workflows/tts.md`: New feature spec
- `docs/configuration/index.md`: TTS config reference
- `config/meept.json5`: Add TTS section template
- `config/client.json5`: Add TTS section template
- `cmd/meept/tts.go`: New CLI command for voice management
