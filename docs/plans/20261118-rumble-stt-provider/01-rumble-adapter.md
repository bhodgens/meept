# Rumble STT adapter - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> already exist. Do NOT delete/skip/weaken gate tests (the documented F4 skip
> is authored by gate leaf 00Te, not by you). Do NOT commit. Explore with
> search_files/terminal, not read_file on source. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Child-side Rumble STT adapter (buffered PCM to one Final transcript)
- **Dependencies:** 00T + 00Te at TESTS_VALIDATED
- **Estimated Context:** 80K
- **Concurrency Group:** B
- **Audit references:** F1

## Goal

Implement `internal/voicechild/providers/stt_rumble.go`: a keyless,
BaseURL-required STT provider that buffers PCM for one utterance, posts it to
a warm vLLM server, and emits one `Transcript{Final:true}`. Structurally a
clone of `stt_whisper.go` with an HTTP endpoint instead of a subprocess.

## Context

- `internal/voicechild/pipeline.go:24-26,45-49` — STTProvider / Transcript
- `internal/voicechild/providers/stt_whisper.go` — buffering + WAV writer + segmentation precedent (`writeWAV16` at `:398-436`)
- `internal/voicechild/providers/tts_openai.go:170-180` — BaseURL-required constructor precedent
- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch (owned by 03, do NOT edit)
- `docs/research/rumble2flash-review.md:31` — endpoint shapes

## Verified Anchors

- `internal/voicechild/pipeline.go:24-26` — Stream signature
- `internal/voicechild/providers/stt_whisper.go:398-436` — writeWAV16 helper
- `internal/voicechild/providers/tts_openai.go:170-180` — BaseURL guard pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voicechild/providers/stt_rumble.go
func newRumbleSTT(cfg ProviderConfig) (voicechild.STTProvider, error)
// Sentinel: ErrMissingRumbleBaseURL
// Pin: var _ voicechild.STTProvider = (*RumbleSTT)(nil)
```

### What This Leaf Consumes

- C1 STTProvider, C2 Transcript, C4 keys frame (`cfg.BaseURL`), C5 selector
- C3 factory — leaf 03 adds `case "rumble"` calling `newRumbleSTT`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Define RumbleSTT + BaseURL guard | Create: `internal/voicechild/providers/stt_rumble.go` | `go test ./internal/voicechild/providers/ -run TestNewSTT_Rumble` | undefined newRumbleSTT | PASS | Given empty BaseURL; When constructor; Then error naming base URL; Given BaseURL; Then provider returned |
| T2 | Implement Stream: buffer + POST + Final | Modify: `internal/voicechild/providers/stt_rumble.go` | `go test ./internal/voicechild/providers/ -run TestRumbleSTTStream` | no transcript | PASS | Given PCM chunks; When silence/FlushNow/max-window; Then one POST and one Final=true transcript |
| T3 | Response parsing + redacted logging | Modify: `internal/voicechild/providers/stt_rumble.go` | `go vet ./internal/voicechild/providers/` | n/a | PASS | Given JSON response; When parsed; Then text only, no request body logged; pin compiles |

T2 detail:
- Reuse `writeWAV16` from `stt_whisper.go` (same package). Buffer voiced PCM
  with silence/FlushNow/max-window segmentation identical to WhisperSTT so
  downstream contracts (non-speech filtering, short-final gate, turn
  aggregation, barge-in) stay byte-identical.
- POST base64 WAV to `<BaseURL>/v1/audio/transcriptions` (OpenAI-compatible;
  confirm route from research). Body: `{"model":..., "file":...}` per server
  schema. No auth header (keyless).
- Parse response `{"text": "..."}`; emit one Transcript with Final=true.
- On HTTP error/timeout, emit empty Final transcript and log DEBUG — never
  fail the call (fail-soft like WhisperSTT).
- No I/O under mutex.

T3 detail: never log the request body or base64 audio. Log vendor+model only.

## Self-Verification Checklist

- [ ] Tasks implemented; gate tests GREEN; gate files unmodified
- [ ] BaseURL guard present; no auth header; fail-soft posture
- [ ] gofmt + go vet clean; no new dependency
- [ ] No scope creep (03 owns registration/docs)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations (exact endpoint + response shape source)

## Review Checklist (For Review Agent)

- [ ] Stream signature exact; one Final=true per utterance
- [ ] BaseURL guard; keyless; fail-soft; no secret logging
- [ ] Gate tests unmodified; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Streaming (F4) is a decision record from gate 00Te. Do not implement
incremental input here — Rumble is batch as documented. If you discover a
genuine incremental path, report to orchestrator; it becomes a separate leaf.
