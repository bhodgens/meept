# Text-to-Speech (TTS)

**Status**: Implemented
**Date**: 2026-06-11

## Overview

Meept supports client-side Text-to-Speech (TTS) synthesis for reading assistant responses aloud. The TTS system uses Piper TTS (open-source neural TTS) with platform-native fallback.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Meept Client (TUI/Flutter)              │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │  TTS Manager │───▶│  Synthesizer │───▶│ Audio Player │  │
│  │  (queue,     │    │  (piper/     │    │  (oto)       │  │
│  │  interrupt)  │    │   platform)  │    │              │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│                             │                               │
│                             ▼                               │
│                    ┌──────────────┐                         │
│                    │ Piper Binary │                         │
│                    │ (subprocess) │                         │
│                    └──────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

**Key properties:**
- Client-side only (daemon not involved)
- Piper TTS runs as subprocess (like whisper-cli for STT)
- Audio playback via `oto` library (cross-platform)
- Platform-native fallback (`say` on macOS, SAPI on Windows)

## Configuration

### Main Config (`~/.meept/meept.json5`)

```json5
{
  "tts": {
    "enabled": false,                    // Master switch
    "engine": "piper",                   // "piper" | "platform"
    "voice": "danny-medium",             // Voice identifier

    "piper": {
      "bin_path": "piper",               // Piper binary path
      "model_path": "",                  // Empty = ~/.meept/tts/voices/{voice}.onnx
      "config_path": "",                 // Empty = model_path + ".json"
      "speaker": "",                     // For multi-speaker models
    },

    "playback": {
      "volume": 1.0,                     // 0.0 to 1.0
      "rate": 1.0,                       // 0.5 to 2.0
      "audio_device": "",                // Empty = system default
    },

    "behavior": {
      "read_own_messages": false,        // Read user messages too
      "interrupt_on_new_msg": true,      // Stop speaking on new message
      "queue_messages": false,           // Queue vs interrupt
      "max_queue_size": 5,               // Max queued messages
    },
  },
}
```

### Client Config (`~/.meept/client.json5`)

```json5
{
  "tts": {
    "enabled": false,
    "engine": "piper",
    "voice": "danny-medium",
  },
}
```

## Commands

### Voice Management

```bash
# List available voices
meept tts voices list

# Download a voice
meept tts voices download danny-medium

# Remove a voice
meept tts voices remove danny-medium
```

### Configuration

```bash
# Open TTS config in TUI editor
meept config tts
```

## Voices

Piper voices are stored in `~/.meept/tts/voices/`. Each voice consists of:
- `{voice}.onnx` - ONNX neural network model
- `{voice}.onnx.json` - Voice configuration (sample rate, phonemizer, etc.)

**Recommended voices:**
| Voice | Quality | Language | Size |
|-------|---------|----------|------|
| `danny-medium` | medium | en-US | ~80MB |
| `en_US-lessac-high` | high | en-US | ~120MB |
| `en_US-lessac-medium` | medium | en-US | ~70MB |
| `en_GB-alan-medium` | medium | en-GB | ~70MB |

Full catalog: https://huggingface.co/rhasspy/piper-voices

## Dependencies

| Component | Dependency | Install |
|-----------|-----------|---------|
| Piper TTS | `piper` binary | See https://github.com/rhasspy/piper |
| Piper voices | ONNX models | `meept tts voices download` |
| Go audio | `github.com/ebitengine/oto/v3` | `go get` (already in go.mod) |
| Flutter audio | `audioplayers` | `flutter pub add audioplayers` |

### Installing Piper

**macOS:**
```bash
brew install piper-tts
```

**Linux:**
```bash
# Download from https://github.com/rhasspy/piper/releases
wget https://github.com/rhasspy/piper/releases/download/v1.2.0/piper_$(uname -m)_linux.tar.gz
tar xzf piper_*.tar.gz
sudo cp piper /usr/local/bin/
```

## Behavior

### When TTS Triggers

By default, TTS reads assistant responses when:
1. `tts.enabled` is `true`
2. Message is from assistant (not user)
3. No other message is currently being spoken (or interrupt is enabled)

### Interrupt vs Queue

- **Interrupt mode** (`interrupt_on_new_msg: true`): Stop current speech and speak new message immediately
- **Queue mode** (`queue_messages: true`): Add new message to queue, speak after current completes

### Visual Indicators

When TTS is active:
- TUI: Speaker icon in viewport header
- Flutter: Pulsing microphone icon on send button

## Implementation Details

### Go Package: `internal/tts/`

| File | Purpose |
|------|---------|
| `synthesizer.go` | Interface, Config structs, factory |
| `piper.go` | Piper TTS engine (subprocess invocation) |
| `platform.go` | Platform-native fallback (macOS `say`, Windows SAPI) |
| `player.go` | Audio playback via `oto` |
| `manager.go` | Queue management, interrupt logic |
| `synthesizer_test.go` | Unit tests |

### Key Types

```go
type Synthesizer interface {
    Synthesize(ctx context.Context, text string) (*Result, error)
    Play(audio []byte) error
    Stop() error
    IsSpeaking() bool
    Name() string
    CheckAvailable() error
}

type Manager struct {
    // Queue management, interrupt handling
    Speak(text string) error
    Stop() error
    IsSpeaking() bool
}
```

### Piper Subprocess

Piper reads text from stdin, writes WAV to file:

```bash
piper -m voice.onnx -c voice.onnx.json -f output.wav <<< "Hello world"
```

Go implementation:
```go
cmd := exec.CommandContext(ctx, "piper", "-m", model, "-c", config, "-f", tmpFile)
stdin, _ := cmd.StdinPipe()
io.WriteString(stdin, text)
cmd.Run()
audioData, _ := os.ReadFile(tmpFile)
```

## Flutter Integration (TODO)

The Flutter client should implement TTS using:
- `flutter_tts` package for platform-native TTS, OR
- `audioplayers` + Piper subprocess (similar to Go)

See `docs/plans/2026-06-11-tts-piper-integration.md` Task 5 for details.

## Testing

```bash
# Unit tests
go test ./internal/tts/... -v

# Build CLI
go build -o bin/meept ./cmd/meept

# Test voice download
./bin/meept tts voices list
./bin/meept tts voices download danny-medium

# Test config UI
./bin/meept config tts
```

## Troubleshooting

**"piper binary not found"**
- Install Piper: see Installing Piper above
- Or set `tts.piper.bin_path` to full path

**"voice model not found"**
- Download voice: `meept tts voices download danny-medium`
- Or set `tts.piper.model_path` to full path

**No audio playback**
- Check system volume
- Verify `tts.playback.volume` is > 0
- Try `tts.engine: "platform"` as fallback

## Related

- Speech-to-Text: `docs/workflows/speech-to-text.md`
- Client configuration: `docs/configuration/index.md`
- Piper TTS: https://github.com/rhasspy/piper
