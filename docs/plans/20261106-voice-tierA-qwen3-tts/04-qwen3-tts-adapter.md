# Qwen3-TTS streaming adapter - Implementation Leaf (04)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Streaming OpenAI-compatible TTS adapter covering vendor
  openai (cloud, keyed) and qwen3 (local server, keyless), with
  incremental first audio and resampling to the negotiated rate.
- **Dependencies:** 02 (ProviderConfig.BaseURL/Voice fields + vendor
  dispatch stubs) - coordinates via contract C2 only; can author in
  parallel and integrate at review.
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Tier A swap point for the voice: speak with a local Qwen3-TTS server
exposed as an OpenAI-compatible `/v1/audio/speech` streaming endpoint,
while keeping OpenAI cloud TTS working unchanged. The adapter yields the
first PCM chunk as the server streams it - never buffer-then-play.

## Context

- Qwen3-TTS serving (OUT OF REPO SCOPE - the user deploys the server):
  any server honoring `POST {base}/v1/audio/speech` with body
  `{"model":..., "voice":..., "input":..., "response_format":"pcm",
  "stream":true}` returning raw s16le mono 24 kHz PCM chunks
  (vLLM-Omni `vllm serve Qwen/Qwen3-TTS-... --omni --port 8091`, or a
  community FastAPI/MLX server). Documented constraint: streaming pcm is
  24 kHz fixed; `speed` unsupported while streaming.
- internal/voicechild/providers/tts_openai.go - the existing adapter.
  Leaf 02 already added vendor dispatch + BaseURL/Voice; this leaf
  implements the streaming behavior and qwen3 specifics on that core.
- internal/voicechild/providers/resample.go - newResamplingReader
  (stateful, incremental 24k -> negotiated rate). Reuse; do not fork.
- Stream contract T3: first audio must leave Speak()'s control as soon
  as the server sends it (httptest fake writes chunk, sleeps, writes
  more; the test asserts the first read completes BEFORE the body ends).
- Pipeline playback (internal/voicechild/pipeline.go:170-241) reads
  chunk-sized blocks from the returned reader - no change needed there.

## Verified Anchors

- internal/voicechild/providers/tts_openai.go:15-18 - ttsSampleRate 24000
- internal/voicechild/providers/tts_openai.go:27-34 - request struct
- internal/voicechild/providers/tts_openai.go:71-117 - Speak + transport
  seam (setTTSTransport) + resample-on-rate-mismatch
- internal/voicechild/providers/resample.go:26-35 - newResamplingReader
- internal/voicechild/providers/tts_test.go - fake-vendor test pattern
  (httptest + transport swap) to follow

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Same exported surface as leaf 02 left it (no new exported symbols
// beyond what T6 created). Behavior contract:
//   vendor "openai": POST https://api.openai.com/v1/audio/speech
//     (or cfg.BaseURL when set), Bearer key, stream=true, pcm.
//   vendor "qwen3":  POST {cfg.BaseURL}/v1/audio/speech (BaseURL "" is
//     a constructor ERROR: ErrMissingTTSBaseURL - named sentinel, local
//     server has no meaningful default), no Authorization header,
//     stream=true, response_format=pcm, model=cfg.Model ("" = server
//     default), voice=cfg.Voice ("" = server default).
//   Both: io.ReadCloser of s16le mono at cfg.SampleRate (resampled from
//     24 kHz when different), incremental first chunk.
```

### What This Leaf Consumes

- C2 ProviderConfig with BaseURL/Voice (leaf 02).
- voicechild.TTSProvider interface (pipeline.go:23-26) - unchanged.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Streaming request: add stream:true to the request body for both vendors; keep JSON field order stable | Modify: internal/voicechild/providers/tts_openai.go | `go test ./internal/voicechild/providers/ -run TestQwen3TTS` | gate T3 RED (no stream field / no qwen3) | fake server asserts body has "stream":true and response_format pcm | Given fake server; When Speak; Then received body JSON contains stream true |
| T2 | qwen3 specifics: ErrMissingTTSBaseURL sentinel when BaseURL empty; no Authorization header; model/voice passthrough | Modify: tts_openai.go, providers.go (sentinel) | `go test ./internal/voicechild/providers/ -run TestQwen3TTSConstruction` (gate or new, report) | sentinel absent | construction matrix green (qwen3+url+empty key OK; qwen3 no url = sentinel; openai rules unchanged) | see contract block |
| T3 | Incremental first audio: reader yields before body EOF (gate test) | behavior in Speak's returned reader | gate T3 streaming assertions | today's non-stream body still yields incrementally via resampler - gate asserts request-side streaming too | gate T3 passes fully | Given slow fake server; When first Read; Then returns before server handler returns |
| T4 | Resample correctness 24k->16k (negotiated) and passthrough at 24k | covered by gate T3 asserts + tts_test.go extensions if needed | `go test ./internal/voicechild/providers/` | n/a | full package green | Given 24k source; When negotiated 16k; Then output length ratio ~1.5 (tolerance) |
| T5 | No key leakage: qwen3 sends no auth header; openai Bearer only; errors never embed the key | covered in T2 tests + code review | `go test ./internal/voicechild/providers/ -run TestTTSKeyHandling` | n/a | header assertions green | Given both vendors; When request built; Then headers per contract |

## Self-Verification Checklist

- [ ] Gate tests T3 (F3) pass; no gate test modified
- [ ] No live vendor calls in tests; httptest + transport swap only
- [ ] First-chunk incremental behavior proven by test
- [ ] gofmt + `go vet ./internal/voicechild/providers/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** providers package files
2. **Verify command + output:** gate commands, last lines
3. **Deviations:** any request-field additions the fake server needed

## Review Checklist (For Review Agent)

- [ ] Vendor matrix per contract block (openai/qwen3 x key/url/voice)
- [ ] 24 kHz constant unchanged; resample path reused (no fork)
- [ ] No buffering of the full stream before first yield
- [ ] Error paths close response bodies (no leaks)
- [ ] Sentinels named and wrapped with %w per house style

Output: APPROVED or specific gaps with file + line references.

## Notes

- If the user's chosen Qwen3-TTS server needs an extra body field (e.g.
  language), add it as an optional omitempty field here and note it -
  the master's Open Questions section anticipates this.
- Do NOT touch pipeline.go (leaf 05's area) or daemon files.
- Speed field: omit entirely when streaming (vendor constraint).
