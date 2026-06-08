# Speech-to-Text Design

**Date**: 2026-06-08
**Status**: Draft

## Overview

Add client-side speech-to-text transcription to both the TUI and Flutter GUI. Activated by double-enter on an empty input field. Three pluggable transcription engines: whisper.cpp subprocess, parakeet.cpp subprocess, and OS-native (macOS Speech framework). All recording and transcription happens client-side; the daemon is not involved.

## Activation

| State | Action |
|-------|--------|
| Has text + single Enter | Send normal |
| Has text + double Enter | Send as steer |
| Empty + single Enter | Nothing |
| Empty + double Enter | Activate voice recording |
| Recording + double Enter | Stop recording, transcribe, place result in input |
| Recording + Escape | Cancel recording, discard |

Transcription result is placed in the input field for review by default. Configurable via `stt.auto_send`.

## Configuration

New `stt` section in `meept.json5`. For Flutter UI-specific overrides, the same section is also read from `~/.meept/client.json5` (client.json5 takes precedence).

```json5
{
  stt: {
    enabled: true,
    engine: "whisper",           // "whisper" | "parakeet" | "native"
    language: "en",              // language code for transcription
    auto_send: false,            // true = send immediately after transcription

    whisper: {
      bin_path: "whisper-cli",   // path to whisper-cli binary
      model_path: "",            // default: ~/.meept/models/ggml-base.en.bin
      threads: 4,
    },

    parakeet: {
      bin_path: "parakeet-transcribe",
      model_path: "",            // default: ~/.meept/models/parakeet-tdt-ctc-110m
    },

    native: {
      // macOS Speech framework / Windows SAPI - no extra config
    },

    recording: {
      recorder_bin: "ffmpeg",    // or "sox"
      sample_rate: 16000,
      channels: 1,
      format: "wav",
    },
  },
}
```

Defaults: `engine: "whisper"`, `auto_send: false`, `recorder_bin: "ffmpeg"`.

Accessible via `meept config stt`.

## Go Transcriber Interface

New package: `internal/stt/`

```go
// internal/stt/transcriber.go

type Result struct {
    Text       string
    IsFinal    bool
    Confidence float64
}

type Config struct {
    Engine     string
    Language   string
    Whisper    WhisperConfig
    Parakeet   ParakeetConfig
    Native     NativeConfig
    Recording  RecordingConfig
}

type Transcriber interface {
    Start(ctx context.Context, onResult func(Result)) error
    Stop() (string, error)
    IsRecording() bool
    Name() string
}

// NewTranscriber returns the appropriate implementation based on cfg.Engine.
func NewTranscriber(cfg Config) (Transcriber, error)
```

### Implementations

#### `internal/stt/whisper.go` - WhisperEngine

1. `Start()`: spawns `ffmpeg -f avfoundation -i ':0' -ar 16000 -ac 1 /tmp/meept-stt-XXXX.wav` recording to temp file
2. `Stop()`: sends SIGTERM to ffmpeg, waits for file to finalize, spawns `whisper-cli -m <model> -f <file> --no-timestamps`, parses stdout for transcription text
3. Calls `onResult` with final text

#### `internal/stt/parakeet.go` - ParakeetEngine

Same pattern as WhisperEngine but invokes the parakeet CLI binary and parses its output format.

#### `internal/stt/native.go` - NativeEngine

- **macOS**: Uses `ffmpeg` for recording. Transcribes via a small Swift helper binary that calls `SFSpeechRecognizer` with `SFSpeechAudioBufferRecognitionRequest` for streaming results. The helper is built as part of `make build` and installed alongside the `meept` binary. Falls back to `osascript` if the helper is not available.
- **Windows**: Uses `ffmpeg` for recording. Transcribes via PowerShell invoking `System.Speech.Recognition.SpeechRecognitionEngine`.
- **Linux**: Returns error "native STT not supported on Linux; use whisper or parakeet engine".

#### `internal/stt/recorder.go` - Shared recorder logic

Both whisper and parakeet engines share the same `ffmpeg`/`sox` recording logic:
- Detect available recorder binary (`ffmpeg` preferred, `sox` fallback)
- Spawn recorder subprocess writing to temp WAV file
- Graceful stop via SIGTERM
- Temp file cleanup after transcription completes

## TUI Integration

### State Machine

New `recordingState` field on `ChatModel` in `internal/tui/models/chat.go`:

```
idle ──(empty + double-enter)──→ recording
recording ──(double-enter)──→ transcribing ──(result received)──→ idle
recording ──(escape)──→ idle (cancel, discard audio)
transcribing ──(escape)──→ idle (cancel, discard result)
```

### Key Changes

**`internal/tui/models/chat.go`**:
- Add `recordingState` enum: `sttIdle`, `sttRecording`, `sttTranscribing`
- Add `transcriber stt.Transcriber` field, initialized from config in `NewChatModelWithConfig()`
- Modify double-enter detection: when input is empty and double-enter fires, toggle recording instead of steer
- Add Bubble Tea message types: `STTRecordingStartMsg{}`, `STTResultMsg{Text: string}`, `STTErrorMsg{Err: error}`
- `Update()` handler for `STTResultMsg`: place text in textarea (or auto-send if `auto_send` is true)
- `View()` override when recording: render orange overlay instead of normal textarea

**`internal/tui/app.go`**:
- Double-enter interception at app level: when input is empty, pass Enter through to ChatModel instead of intercepting
- Ensure recording state doesn't interfere with slash command handling

**Recording UI** (`chat.go View()`):

When `recordingState == sttRecording`:
- Textarea border and background turn solid orange (`#F97316`)
- Center text "speak to transcribe..." in black, bold
- Normal textarea content hidden
- Bottom hint: "double-enter to stop  |  esc to cancel" in small gray text

When `recordingState == sttTranscribing`:
- Same orange background but with a blinking/dimmed effect
- Center text "transcribing..." in black

### Graceful Degradation

When `stt.enabled` is true but the configured engine binary is not found:
- Double-enter on empty input shows a toast: "stt: whisper-cli not found" (or appropriate engine name)
- Does not enter recording state
- Logs warning on startup: `stt engine "whisper" unavailable: whisper-cli not in PATH`

## Flutter Integration

### New Files

| File | Purpose |
|------|---------|
| `ui/flutter_ui/lib/services/stt_service.dart` | Platform speech service wrapping `speech_to_text` plugin |
| `ui/flutter_ui/lib/providers/stt_provider.dart` | Riverpod provider for recording state |

### Dependencies

Add to `pubspec.yaml`:
```yaml
speech_to_text: ^7.0.0
```

### STT Service

```dart
class SttService {
    final SpeechToText _speech = SpeechToText();

    Future<bool> initialize();  // request permissions, check availability
    void startRecording(Function(String) onResult);
    Future<String> stopRecording();
    bool get isRecording;
    bool get isAvailable;
}
```

Uses macOS Speech framework (`SFSpeechRecognizer`) via the `speech_to_text` Flutter plugin. On Windows, the plugin uses `windows_speech_recognition`. No subprocess, no ffmpeg - native streaming audio capture.

### Send Button Behavior

| State | Button Appearance |
|-------|-------------------|
| Normal | Orange background, `Icons.send` |
| Recording | Blinking orange/dark animation, `Icons.mic` |
| Transcribing | Spinning indicator, `Icons.mic` |

Tapping the send button while recording stops recording and transcribes.

### Input Area Behavior

Same as TUI: empty + double-enter starts recording, recording + double-enter stops and transcribes. Transcription result placed in input field for review (or auto-sent if `auto_send: true`).

### Platform Permissions

- **macOS**: `Info.plist` requires `NSSpeechRecognitionUsageDescription` and `NSMicrophoneUsageDescription`
- **Windows**: No special manifest entries needed; first-use permission dialog from OS

## Config UI Integration

### TUI Config Editor

Add `stt` section to `internal/configui/` with fields:
- enabled (toggle)
- engine (whisper/parakeet/native selector)
- language (text input)
- auto_send (toggle)
- Engine-specific fields shown/hidden based on engine selection

### Flutter Settings

Add STT settings panel in `ui/flutter_ui/lib/features/settings/settings_panel.dart` with the same fields rendered as Flutter form controls.

## Documentation Updates

### Files to Update

| File | Changes |
|------|---------|
| `CLAUDE.md` | Add `internal/stt` to project structure table, add STT config section, update build commands with dependency notes |
| `docs/workflows/speech-to-text.md` | New feature spec document |
| `docs/configuration/index.md` | Add `stt` config section with all options |
| `docs/reference/cli.md` | Document `meept config stt` command |
| `docs/concepts/architecture.md` | Add STT to request flow (optional, client-side path) |
| `config/meept.json5` | Add `stt` section template with comments |
| `internal/config/schema.go` | Add `STTConfig` struct with JSON5 tags |
| `internal/configui/sections_stt.go` | New file for STT config TUI section |

### CLAUDE.md Addition

Add to project structure table:
```
  stt/             # Speech-to-text (transcriber interface, whisper, parakeet, native)
```

Add config section:
```
### Speech-to-Text Configuration

STT is client-side only (TUI and Flutter). Requires external tools depending on engine:
- whisper: `whisper-cli` + `ffmpeg`, model at `~/.meept/models/ggml-base.en.bin`
- parakeet: parakeet CLI + `ffmpeg`, model at `~/.meept/models/`
- native: macOS Speech framework or Windows SAPI (no external deps)

Config: `meept config stt`
```

## Dependencies

| Component | Dependency | Install | Required |
|-----------|-----------|---------|----------|
| TUI + whisper engine | `ffmpeg` or `sox` | `brew install ffmpeg` | Only if using whisper/parakeet engine |
| TUI + whisper engine | `whisper-cli` | Build from whisper.cpp | Only if engine=whisper |
| TUI + whisper engine | Whisper model file (~148MB) | Download to `~/.meept/models/` | Only if engine=whisper |
| TUI + parakeet engine | parakeet CLI | Build from parakeet.cpp | Only if engine=parakeet |
| TUI + native engine | none (macOS built-in) | - | Only on macOS |
| Flutter (all engines) | `speech_to_text` plugin | pubspec.yaml | Always for Flutter |
| Flutter (macOS) | Mic + Speech entitlements | Info.plist entries | Always for Flutter on macOS |

## Error Handling

- **Missing binary**: Show toast with engine name and expected path. Don't enter recording state.
- **Missing model**: Show toast with model path. Don't enter recording state.
- **Mic permission denied**: Show toast "microphone access denied" with OS-specific instructions.
- **Transcription failure**: Show toast with error message. Return to idle state.
- **Recording too short** (<0.5s): Discard silently, show toast "too short".
- **Empty transcription**: Show toast "no speech detected". Return to idle.

## Testing

- `internal/stt/transcriber_test.go`: Unit tests for `NewTranscriber()` returning correct implementation
- `internal/stt/recorder_test.go`: Test recorder binary detection, temp file management
- `internal/stt/whisper_test.go`: Integration test with mock whisper-cli binary
- `internal/stt/parakeet_test.go`: Integration test with mock parakeet binary
- `internal/stt/native_test.go`: Platform-specific native tests
- `internal/tui/models/chat_test.go`: Test recording state machine, double-enter behavior
- Flutter widget tests for send button states and recording overlay
