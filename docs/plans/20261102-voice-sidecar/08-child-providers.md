# Child providers - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat.
> Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voicechild/providers: real STT/TTS adapters behind leaf-07 interfaces (assumption A1: Deepgram STT streaming + OpenAI TTS; interface-pinned so a local pair swaps in later), key fetch over the text socket at setup
- **Dependencies:** leaf 07 (STTProvider/TTSProvider interfaces, keys frame)
- **Estimated Context:** 70K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/voicechild/providers/*.go

## Goal

The ears and mouth. Streaming STT: PCM chunks in, partial+final
transcripts out, low latency (barge-in quality depends on it). TTS: text
in, PCM out at the negotiated rate (16 kHz s16le target; resample from
provider rate inside the adapter). Keys arrive via the C1 keys frame at
call setup - the child never reads env or argv for them.

## Context

Assumption A1 (swap point): Deepgram nova streaming over WebSocket for
STT; OpenAI tts-1 (or gpt-4o-mini-tts) for TTS. Both behind the leaf-07
interfaces so a whisper.cpp/Piper pair is a drop-in later (spec: providers
are config; the socket contract does not change).
jargo already wraps several STT/TTS providers - check
github.com/gojargo/jargo provider packages first; wrap a jargo provider
where one exists instead of hand-rolling the vendor protocol. Record the
choice in the completion report.
Resampling: jargo audio/resample (pure Go) or a minimal linear resampler;
output MUST be s16le mono at the rate the daemon negotiated (setup frame
codec/sample_rate).
Key handling: keys live in process memory only; zero the buffer after
provider client construction where the vendor client allows a static
token; never log; the keys frame type already redacts (leaf 07).

## Verified Anchors

- internal/voicechild/child.go (leaf 07) - STTProvider/TTSProvider interfaces, keys frame path
- github.com/gojargo/jargo - provider wrappers (check before hand-rolling)
- master.md C1 keys frame shape

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voicechild/providers
func NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
func NewTTS(cfg ProviderConfig, key string) (voicechild.TTSProvider, error)
type ProviderConfig struct{ Vendor string; Model string; SampleRate int; Language string }
```

### What This Leaf Consumes

- leaf 07 interfaces; C1 keys frame

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey jargo provider packages; pick wrap-vs-handroll per vendor; record decision | - | - | - | decision in report | - |
| T2 | STT adapter: streaming session, partial/final Transcript events, reconnect-within-call on WS drop (one retry, then error event) | Create: internal/voicechild/providers/stt_deepgram.go (or jargo wrap), stt_test.go | go test -p 2 ./internal/voicechild/providers/ -run TestSTT | - | PASS | Given httptest WS vendor; When PCM streamed; Then finals surface with text |
| T3 | TTS adapter: text -> PCM s16le mono at target rate; streaming response consumed incrementally (first-audio latency matters) | Create: providers/tts_openai.go, tts_test.go | go test -p 2 ./internal/voicechild/providers/ -run TestTTS | - | PASS | Given fake vendor WAV/PCM; When Speak; Then reader yields target-rate s16le |
| T4 | Key fetch wiring: at setup, child requests keys; NewSTT/NewTTS constructed from the keys frame; missing keys -> call fails with named error, child stays up | Modify: internal/voicechild/child.go (keys path only - coordinate: leaf 07 owns the frame shape, this leaf adds construction) | go test -p 2 ./internal/voicechild/ -run TestKeysWiring | - | PASS | Given keys frame with empty stt_key; When setup; Then error names missing key, other calls unaffected |
| T5 | No-key-in-log audit: adapter construction logs vendor+model only | - | go test -p 2 ./internal/voicechild/providers/ -run TestNoKeyLogs | - | PASS | Given constructed adapters; When log captured; Then no key substring |
| T6 | Format/vet/race | - | gofmt -l internal/voicechild; go vet ./internal/voicechild/...; go test -p 2 -race ./internal/voicechild/... | - | clean | - |

## Self-Verification Checklist

- [ ] Adapters satisfy leaf-07 interfaces exactly (compile-time var _ pins)
- [ ] Output rate/encoding matches setup frame negotiation
- [ ] Keys never in env/argv/log; redaction intact
- [ ] Vendor WS tested against a fake, never the live vendor in CI
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list; A1 swap decision recorded

## Review Checklist (For Review Agent)

- [ ] Interface satisfaction pinned at compile time
- [ ] Reconnect policy bounded (no infinite retry loop inside a call)
- [ ] First-audio latency path streams (no full-buffer-then-play)
- [ ] Key lifecycle: memory-only, zeroized where possible

Output: APPROVED or specific gaps.

## Notes

If the user swaps A1 to a local pair, this leaf is the only one that
changes: the interfaces, socket, and daemon are vendor-blind by design.
