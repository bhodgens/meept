# Speech-to-Text

## Overview
Client-side speech-to-text transcription for both the TUI and Flutter GUI. Activated by double-enter on an empty input field. Supports three pluggable transcription engines: whisper.cpp subprocess, parakeet.cpp subprocess, and OS-native (macOS Speech framework). All recording and transcription happens client-side; the daemon is not involved.

## Problem
Typing long messages in the TUI or Flutter UI is slow and inconvenient. Speech input provides a faster, more natural way to compose messages, especially for long-form queries or when the user prefers dictation.

## Behavior

### Activation State Machine

| State | Action | Result |
|-------|--------|--------|
| Has text + single Enter | Send normal | Message sent as-is |
| Has text + double Enter | Send as steer | Message sent with steer intent |
| Empty + single Enter | Nothing | No action |
| Empty + double Enter | Activate voice recording | Recording starts |
| Recording + double Enter | Stop recording | Transcribe, place result in input |
| Recording + Escape | Cancel recording | Discard audio, return to idle |
| Transcribing + Escape | Cancel transcription | Discard result, return to idle |

Transcription result is placed in the input field for review by default. When `stt.auto_send` is true, the transcribed text is sent immediately.

### TUI Recording UI

When recording:
- Textarea border and background turn solid orange (`#F97316`)
- Center text: "speak to transcribe..." in black, bold
- Bottom hint: "double-enter to stop  \|  esc to cancel" in small gray text

When transcribing:
- Orange background with blinking/dimmed effect
- Center text: "transcribing..." in black

### Graceful Degradation

When `stt.enabled` is true but the configured engine binary is not found:
- Double-enter on empty input shows a toast: "stt: {engine} not found"
- Does not enter recording state
- Logs warning on startup: `stt engine "{engine}" unavailable: {binary} not in PATH`

## Configuration

### STT Section (in `meept.json5`)

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

**Defaults**: `engine: "whisper"`, `auto_send: false`, `recorder_bin: "ffmpeg"`.

For Flutter UI-specific overrides, the same `stt` section can also be read from `~/.meept/client.json5` (client.json5 takes precedence).

Accessible via `meept config stt`.

## Engines

### whisper

Uses whisper.cpp subprocess. Records audio via `ffmpeg`, then invokes `whisper-cli` for transcription.

- **Recording**: `ffmpeg -f avfoundation -i ':0' -ar 16000 -ac 1 <temp.wav>`
- **Transcription**: `whisper-cli -m <model> -f <file> --no-timestamps`
- **Model**: `~/.meept/models/ggml-base.en.bin` (~148MB)

### parakeet

Uses NVIDIA parakeet.cpp subprocess. Same recording pattern as whisper, different transcription binary.

- **Recording**: `ffmpeg` (same as whisper)
- **Transcription**: `parakeet-transcribe` with model-specific flags
- **Model**: `~/.meept/models/parakeet-tdt-ctc-110m`

### native

Uses OS-provided speech recognition without external dependencies.

- **macOS**: `ffmpeg` for recording, Swift helper calling `SFSpeechRecognizer` for transcription. Falls back to `osascript` if helper is unavailable.
- **Windows**: `ffmpeg` for recording, PowerShell invoking `System.Speech.Recognition.SpeechRecognitionEngine`.
- **Linux**: Not supported; returns error suggesting whisper or parakeet engine.

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

## Observability

### Logging
- Engine availability check on startup
- Recording start/stop events
- Transcription success/failure
- Audio file creation and cleanup

### Metrics
- Recording duration
- Transcription latency
- Success rate per engine

## Edge Cases

### Missing Binary
- Show toast with engine name and expected path
- Do not enter recording state
- Log warning on startup

### Missing Model
- Show toast with model path
- Do not enter recording state

### Mic Permission Denied
- Show toast: "microphone access denied" with OS-specific instructions
- Return to idle state

### Transcription Failure
- Show toast with error message
- Return to idle state
- Clean up temp audio file

### Recording Too Short (<0.5s)
- Discard silently
- Show toast: "too short"

### Empty Transcription
- Show toast: "no speech detected"
- Return to idle state
