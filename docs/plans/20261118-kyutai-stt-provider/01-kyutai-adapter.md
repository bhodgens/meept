# Kyutai STT adapter - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The
> gate tests ALREADY EXIST and are committed. Do NOT delete, skip, weaken,
> or rewrite any gate test. Do NOT commit — the orchestrator handles git.
> Do NOT use read_file on source files — explore with search_files/terminal.
> Write files once and stop; do not read them back.

## Meta

- **Parent:** ../master.md
- **Scope:** Child-side Kyutai STT provider adapter (WebSocket streaming)
- **Dependencies:** 00T-tests.md at TESTS_VALIDATED
- **Estimated Context:** 80K
- **Concurrency Group:** B
- **Audit references:** F1, F4, F5

## Goal

Implement `internal/voicechild/providers/stt_kyutai.go`: a keyless,
BaseURL-required STT provider that opens a Kyutai WebSocket, streams raw
PCM, and emits partial + final transcripts through `STTProvider.Stream`.
Cancel must close the WS for barge-in.

## Context

- `internal/voicechild/pipeline.go:24-26` — `STTProvider` interface
- `internal/voicechild/pipeline.go:45-49` — `Transcript{Text, Final}`
- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch (owned by leaf 02, do NOT edit)
- `internal/voicechild/providers_wiring.go:17-24` — `ProviderConfig`
- `internal/voicechild/providers/tts_openai.go:170-180` — BaseURL-required constructor precedent (`ErrMissingTTSBaseURL`)
- `internal/voicechild/providers/stt_whisper.go` — `FlushNow` precedent if needed

## Verified Anchors

- `internal/voicechild/pipeline.go:24-26` — STTProvider.Stream signature
- `internal/voicechild/pipeline.go:45-49` — Transcript fields
- `internal/voicechild/providers_wiring.go:17-24` — ProviderConfig fields
- `internal/voicechild/providers/tts_openai.go:170-180` — BaseURL guard pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voicechild/providers/stt_kyutai.go
func newKyutaiSTT(cfg ProviderConfig) (voicechild.STTProvider, error)
// Sentinel: ErrMissingKyutaiBaseURL (or reuse providers.ErrMissingBaseURL if present)
// Compile-time pin: var _ voicechild.STTProvider = (*KyutaiSTT)(nil)
```

### What This Leaf Consumes

- C1 STTProvider — `Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)`
- C2 Transcript — `{ Text string; Final bool }`
- C4 keys frame — `cfg.BaseURL` from `stt_base_url`; `cfg.Model` from `stt_model`
- C3 factory — leaf 02 adds `case "kyutai"` calling `newKyutaiSTT(cfg)`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Define KyutaiSTT type + BaseURL guard | Create: `internal/voicechild/providers/stt_kyutai.go` | `go test ./internal/voicechild/providers/ -run TestNewKyutai_BaseURLRequired` | undefined newKyutaiSTT | PASS | Given empty BaseURL; When constructor; Then error naming base URL; Given BaseURL; Then provider returned |
| T2 | Implement Stream WS client | Modify: `internal/voicechild/providers/stt_kyutai.go` | `go test ./internal/voicechild/providers/ -run TestKyutaiSTTStream` | no transcripts | PASS | Given PCM channel; When Stream; Then partial then final Transcript on channel |
| T3 | Cancel closes WS | Modify: `internal/voicechild/providers/stt_kyutai.go` | `go test ./internal/voicechild/providers/ -run TestKyutaiCancelClosesWS` | WS stays open | PASS | Given active stream; When ctx cancel; Then WS closed within 2 s |
| T4 | Redacted logging + compile-time pin | Modify: `internal/voicechild/providers/stt_kyutai.go` | `go vet ./internal/voicechild/providers/` | n/a | PASS | Given provider; Then logs never include key/URL userinfo; pin compiles |

T2 detail:
- Connect to `ws://<BaseURL>/<path>` per official Kyutai protocol (confirm
  path and JSON frame shape from docs before coding; record source URL in a
  comment). Normalize `http://` → `ws://`, `https://` → `wss://`.
- Spawn one reader goroutine that decodes WS messages into Transcript and
  sends on the returned channel; select on ctx.Done() so cancel unblocks.
- Spawn one writer goroutine (or inline in Stream) that reads PCM chunks and
  writes binary WS frames; close the WS when PCM channel closes.
- Preserve ordering; emit `Final:true` once per utterance on server final.
- No I/O under mutex.

T3 detail: `defer conn.Close()` on ctx.Done(); gate tests observe server close.

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests GREEN
- [ ] No gate test files modified (git diff clean on them)
- [ ] Interface contracts satisfied exactly
- [ ] No dependency added without orchestrator approval
- [ ] gofmt + go vet clean
- [ ] No scope creep (02 owns registration; 03 owns e2e)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations (including exact Kyutai WS path/frame source URL)

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests GREEN and unmodified
- [ ] STTProvider signature exact; Transcript fields exact
- [ ] Cancel closes WS; no goroutine leak (race test if available)
- [ ] No secrets in logs; no debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

If the Kyutai protocol needs a dependency not already in go.mod, STOP and
report to the orchestrator rather than adding it. If `FlushNow` is needed,
implement it after Stream works and add a non-gate test.
