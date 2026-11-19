# STT path optimization - Implementation Orchestrator

> Dispatch, review in-session, re-dispatch on gaps, track. Do NOT implement
> code. Do NOT commit until a child passes review.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 5 documents
- **Scope:** F2 persistent Whisper, F4 Deepgram endpoint, F5 entity priming

## Goal

Remove the per-utterance Whisper cold-start cost, consume Deepgram's
authoritative endpoint signal, and enable critical-entity priming. Every
feature is opt-in; legacy exec mode, timer endpointing, and empty
vocabulary stay byte-identical.

## Architecture

Vendor adapters (`internal/voicechild/providers`) produce `Transcript`
values consumed by `pipeline.consumeTranscripts`. New keys selectors flow
foundation -> child -> adapter constructors. F4 adds one bool field to
`Transcript` consumed only inside this branch.

## Interface Contracts

### C1: Whisper server selector

```
WhisperServerAddr string // "" = exec per utterance (legacy); set = persistent server
// Owner: 01-whisper-persistent.md
```

### C2: Deepgram endpoint flag

```
type Transcript struct {
    // existing fields
    Endpoint bool // true on speech_final / UtteranceEnd
}
// Owner: 02-deepgram-endpoint.md
```

### C3: Vocabulary selector

```
Vocabulary []string // nil/empty = no priming (legacy)
// Owner: 03-entity-priming.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|--------------|-------------|
| 00T | 00T-stt-tests.md | leaf (test) | none | 25K | A |
| 01 | 01-whisper-persistent.md | leaf | 00T, foundation | 60K | B |
| 02 | 02-deepgram-endpoint.md | leaf | 00T, foundation | 40K | B |
| 03 | 03-entity-priming.md | leaf | 00T, foundation | 35K | B |
| 04 | 04-stt-docs.md | leaf | 01,02,03 | 20K | C |

Same-letter leaves are file-disjoint:
01 owns `stt_whisper.go`; 02 owns `stt_deepgram.go` + `pipeline.go`;
03 owns `stt_whisper.go` + `stt_deepgram.go` (CONFLICT with 01 and 02).
Serialize 03 after 01 and 02 COMPLETE. Dispatch note goes in 03's brief.

## Dispatch Protocol

1. Phase 0: dispatch `00T-stt-tests.md`; validate IN-SESSION; commit RED
   tests after manual gate record.
2. Phase 1: dispatch 01 and 02 in parallel (file-disjoint). Review each;
   commit each.
3. Phase 2: dispatch 03 after 01 and 02 COMPLETE (shares provider files).
4. Phase 3: dispatch 04 docs. Review; commit.
5. Record statuses; report COMPLETE to root.

Review IN-SESSION only. Max 3 re-dispatch cycles.

## Review Checklist

- [ ] All leaf tasks implemented
- [ ] C1/C2/C3 contracts satisfied exactly
- [ ] Gate tests pass GREEN and unmodified
- [ ] No I/O under playMu; no nested locks
- [ ] Legacy modes byte-identical
- [ ] No scope creep or debug artifacts

## Coding Conventions

- Go 1.27; gofmt before reporting.
- search_files/cat for exploration; write once, do not read back.
- Wrap errors with %w; no ignored errors; no `_ = ` in non-test code.
- Tests table-driven; hermetic e2e only; `go test -p 2 ./...`.
- ASCII only; no secret logging.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | "optimizations and refinements based on our skills" | F2/F4/F5 leaves | 00T + leaf tests |
| R4 | "keep everything opt-in or byte-identical by default" | C1/C2/C3 defaults | additive tests |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-stt-tests | COMPLETE | 10 e0475e9d: gate committed; now GREEN |
| 01-whisper-persistent | COMPLETE | 10 5ebd58b5: F2 opt-in persistent client |
| 02-deepgram-endpoint | COMPLETE | 10 f10eaf5c: F4 endpoint; gate fixture corrected by delta protocol |
| 03-entity-priming | COMPLETE | 10 d0041a7f: F5 keyterm + -p; 100-term cap |
| 04-stt-docs | COMPLETE | 10 5561c999: STT knobs documented |

## Integration Test Plan

```bash
go test -p 2 ./internal/voicechild/... ./internal/daemon/...
go test -p 2 -race ./internal/voicechild/...
make e2e-fast-area AREA=voice_clips
```
Expected: green; legacy exec mode unchanged; timer endpointing unchanged
when DeepgramEndpointMs=0; no vocabulary on wire when empty.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F2 | Whisper persistent server | 01-whisper-persistent.md | `TestWhisperServerMode`, `TestWhisperExecFallback` | voice_clips | VALIDATED |
| F4 | Deepgram endpoint flag | 02-deepgram-endpoint.md | `TestDeepgramEndpointFlag`, `TestEndpointFastDispatch` | voice_clips | VALIDATED |
| F5 | Entity priming | 03-entity-priming.md | `TestDeepgramKeytermParams`, `TestWhisperPromptArg` | voice_clips | VALIDATED |

## Open Questions

- Q2 (root): verify installed whisper-cli supports `--server`. If not, F2
  ships exec fallback only and Q2 becomes BLOCKED with rationale.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dated dispatch addenda.

## Notes

- Q3: Deepgram false-cutoff risk needs hesitation/backchannel fixtures.
  Keep default 0 (timer only).
