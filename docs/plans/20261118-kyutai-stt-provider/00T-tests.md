# Kyutai STT - Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for the Kyutai STT
> provider. Do NOT write implementation. Do NOT commit. Tests will fail RED
> for missing-implementation reasons — that is the expected end state. Run
> them once to confirm they fail for the RIGHT reason (missing symbols /
> behavior, not syntax errors). If a fixture literal displays masked, capture
> it in a shell variable and substitute; never retype it (tool redaction
> corruption). Use search_files/terminal to explore, not read_file on source.
> Write files once and stop; do not read them back.

## Meta

- **Parent:** ../master.md
- **Scope:** Unit + e2e gate tests for F1-F5 in the root Test Gate matrix
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** none

## Goal

Provide executable RED tests that define the Kyutai provider contract before
implementation: streaming partial+final transcripts, factory construction
from keys frame, whisper-default preservation, BaseURL guard, and WS cancel
on barge-in. These tests are the gate; implementation leaves make them GREEN.

## Context

- `internal/voicechild/pipeline.go:24-26` — `STTProvider.Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)`
- `internal/voicechild/pipeline.go:45-49` — `Transcript{Text string; Final bool}`
- `internal/voicechild/providers/stt_deepgram.go:154-172` — `NewSTT(cfg, key)` switch; add case in 02
- `internal/voicechild/providers_wiring.go:17-24` — `ProviderConfig{Vendor, Model, BaseURL, SampleRate, ...}`
- `internal/voicechild/providers/stt_test.go:20-46` — fake vendor server pattern
- `e2e/suites/voice-lifecycle/` — keys-provider scenarios
- `e2e/manifest.json:4838-4847` — `voice-keys-providers-01` template

## Verified Anchors

- `internal/voicechild/pipeline.go:24-26` — STTProvider interface
- `internal/voicechild/pipeline.go:45-49` — Transcript type
- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch
- `internal/voicechild/providers_wiring.go:17-24` — ProviderConfig
- `e2e/manifest.json:4838-4847` — keys-provider scenario template

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test files only; no production symbols.
```

### What This Leaf Consumes

- C1 STTProvider — `Stream(ctx, pcm <-chan []byte) (<-chan Transcript, error)`
- C2 Transcript — `{ Text string; Final bool }`
- C3 NewSTT — `NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Adapter streams partial+final | Create: `internal/voicechild/providers/stt_kyutai_test.go` | `go test ./internal/voicechild/providers/ -run TestKyutaiSTTStream` | undefined symbols | PASS | Given a fake Kyutai WS server sending partial then final; When Stream consumes PCM; Then channel yields partial (Final=false) then final (Final=true) |
| T2 | Factory constructs kyutai + BaseURL guard | Modify: `internal/voicechild/providers/stt_deepgram_test.go` (new test funcs) | `go test ./internal/voicechild/providers/ -run TestNewSTT_Kyutai` | unknown vendor error | PASS | Given cfg.Vendor="kyutai" with BaseURL; When NewSTT; Then non-nil provider; Given empty BaseURL; Then error names base URL |
| T3 | Whisper default unchanged | Modify: `internal/voicechild/providers/stt_deepgram_test.go` | `go test ./internal/voicechild/providers/ -run TestNewSTT_DefaultUnchanged` | n/a (guardrail, may pass now) | PASS | Given cfg vendor="" / "whisper"; When NewSTT; Then existing behavior unchanged |
| T4 | Cancel closes WS | Modify: `internal/voicechild/providers/stt_kyutai_test.go` | `go test ./internal/voicechild/providers/ -run TestKyutaiCancelClosesWS` | undefined | PASS | Given an active WS; When ctx cancelled; Then server observes close within deadline |
| T5 | e2e kyutai keys scenario | Create: `e2e/suites/voice-lifecycle/kyutai_stt_test.go`; Modify: `e2e/manifest.json` | `make e2e-fast-area AREA=voice-lifecycle` | skipped/RED | PASS | Given keys line with stt_vendor=kyutai; When call opens; Then transcript arrives with Final=true |

T1 detail:
1. Write a fake WS server (httptest + gorilla/websocket if available, else a
   minimal stdlib WS handshake) that accepts a connection, reads PCM frames,
   and sends `{"type":"partial","text":"hello"}` then
   `{"type":"final","text":"hello there"}`.
2. Assert `Stream` returns a channel; read two transcripts with a 3 s timeout.
3. Assert first `{Text:"hello", Final:false}`, second
   `{Text:"hello there", Final:true}`.

T2 detail: table with rows: kyutai+BaseURL → provider non-nil; kyutai no
BaseURL → error, message contains "base URL".

T4 detail: use the same fake WS; cancel ctx after first partial; assert the
fake server's close frame arrives within 2 s.

T5 is a build-tagged (`//go:build e2e`) suite scenario. If the real Kyutai
server is unavailable, gate the scenario with a `t.Skip("RED: kyutai server
not available")` inside the test body so the tree stays buildable, per AGENTS.md.

## Self-Verification Checklist

- [ ] All F1-F5 rows have at least one test
- [ ] Tests fail RED for missing-implementation reasons only
- [ ] No implementation files created or modified
- [ ] No commit, no git add
- [ ] No masked literals copied into fixtures
- [ ] gofmt clean on test files

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify commands + last 20 lines of output (showing RED)
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Every F row covered; RED reasons correct (not syntax errors)
- [ ] Gate tests assert behavior, not implementation details
- [ ] No implementation code written
- [ ] No line-number corruption; no masked fixture literals

Output: APPROVED or specific gaps with file + line references.

## Notes

Kyutai wire protocol must be confirmed from the official docs before the
fake server is authored; if the actual frame shape differs, the control
agent updates the test (delta protocol) with the source URL.
