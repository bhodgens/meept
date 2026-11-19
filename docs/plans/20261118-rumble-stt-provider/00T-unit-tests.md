# Rumble STT - Unit Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1, F2, F3, F5. Do NOT
> write implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons (undefined symbols / wrong behavior), not
> syntax errors. Explore with search_files/terminal, not read_file on source.
> Write files once and stop. Never retype a masked fixture literal — capture
> it in a shell variable and substitute.

## Meta

- **Parent:** ../master.md
- **Scope:** Unit gate tests for the Rumble STT adapter + factory + GPU cap
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** F1, F2, F3, F5

## Goal

Provide executable RED unit tests defining the Rumble provider contract before
implementation: buffered PCM to one Final transcript, factory construction
from keys frame, whisper-default preservation, and explicit GPU memory cap.

## Context

- `internal/voicechild/pipeline.go:24-26` — `STTProvider.Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)`
- `internal/voicechild/pipeline.go:45-49` — `Transcript{Text, Final}`
- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch
- `internal/voicechild/providers_wiring.go:17-24` — ProviderConfig
- `internal/voicechild/providers/stt_test.go:20-46` — fake HTTP server pattern
- `internal/voicechild/providers/stt_whisper_test.go` — WAV writer test precedent

## Verified Anchors

- `internal/voicechild/pipeline.go:24-26` — STTProvider interface
- `internal/voicechild/pipeline.go:45-49` — Transcript type
- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch
- `internal/voicechild/providers_wiring.go:17-24` — ProviderConfig fields

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test files only; no production symbols.
```

### What This Leaf Consumes

- C1 STTProvider, C2 Transcript, C3 NewSTT, C5 config selector

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Adapter emits one Final transcript | Create: `internal/voicechild/providers/stt_rumble_test.go` | `go test ./internal/voicechild/providers/ -run TestRumbleSTTStream` | undefined newRumbleSTT | PASS | Given fake vLLM server returning JSON; When Stream consumes PCM; Then one Transcript{Final:true} with text |
| T2 | Factory constructs rumble | Modify: `internal/voicechild/providers/stt_deepgram_test.go` (new funcs) | `go test ./internal/voicechild/providers/ -run TestNewSTT_Rumble` | unknown vendor | PASS | Given cfg.Vendor="rumble" + BaseURL; When NewSTT; Then non-nil provider |
| T3 | Whisper default unchanged | Modify: `internal/voicechild/providers/stt_deepgram_test.go` (new funcs) | `go test ./internal/voicechild/providers/ -run TestNewSTT_DefaultUnchanged` | n/a (guardrail) | PASS | Given vendor=""/"whisper"; When NewSTT; Then existing behavior |
| T4 | GPU cap explicit in runtime config | Create: `internal/daemon/rumble_runtime_test.go` | `go test ./internal/daemon/ -run TestRumbleRuntimeConfigCap` | undefined config symbol | PASS | Given Rumble runtime config; When built; Then gpu_memory_utilization <= 0.25, never 0.92 |

T1 detail: fake HTTP server at httptest URL accepts a POST, decodes the
request body contains base64 audio, returns `{"text":"hello there"}`. Assert
Stream returns a channel and yields exactly one Transcript with Text
"hello there" and Final=true within 3 s. Confirm request had no auth header.

T2 detail: table rows: rumble+BaseURL -> provider non-nil; rumble no BaseURL
-> error naming base URL (mirror qwen3 constructor guard).

T4 detail: assert the Rumble sidecar config struct carries an explicit low
GPU fraction. If 02 has not landed, undefined symbol = correct RED.

## Self-Verification Checklist

- [ ] F1/F2/F3/F5 each have ≥1 test
- [ ] RED for missing-implementation reasons only
- [ ] No implementation files touched; no commit; no git add
- [ ] No masked literals copied into fixtures; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + last 20 lines (showing RED)
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Every F row covered; RED reasons correct; behavior asserted not impl detail
- [ ] No implementation code; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Rumble chat completion response shape must be confirmed from HF card; if it
differs, control agent updates the test via delta protocol with source URL.
