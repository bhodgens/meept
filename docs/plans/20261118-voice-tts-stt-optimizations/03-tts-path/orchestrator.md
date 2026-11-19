# TTS path optimization - Implementation Orchestrator

> Dispatch, review in-session, re-dispatch on gaps, track. Do NOT implement
> code. Do NOT commit until a child passes review.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 6 documents
- **Scope:** F3 clip contention, F6 transport split, F8 resampler reuse, F9 chunk knob, F10 clip yield

## Goal

Remove background contention from first audio, give cloud OpenAI its own
transport, remove per-Read allocation churn, and expose qwen3 chunk size.
Every change is opt-in or byte-identical.

## Architecture

The daemon planner ships clip specs at setup (`voice_wiring.go:1232`).
The child renders them serially on a background worker sharing the TTS
provider (`clips.go:217-346`). Live synthesis and prefetch use the same
provider (`pipeline.go:1849`). TTS vendors share one no-keepalive transport
(`tts_openai.go:59-70,191,206`). Resampling happens per Read in
`resample.go:106-141`.

## Interface Contracts

### C1: Clip prewarm delay selector

```
ClipPrewarmDelayMs int // 0 = legacy immediate; >0 = defer after played edge
// Owner: 02-clip-yield.md
```

### C2: qwen3 chunk size selector

```
Qwen3ChunkSize int // 0 => 4 (legacy); positive overrides request chunk_size
// Owner: 05-chunk-size-knob.md
```

### C3: Invariant

```
No I/O under playMu. Render yield check reads playMu-protected fields only.
// Owner: every leaf
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|--------------|-------------|
| 00T | 00T-tts-tests.md | leaf (test) | none | 25K | A |
| 01 | 01-clip-deferral.md | leaf | 00T, foundation | 45K | B |
| 02 | 02-clip-yield.md | leaf | 00T, foundation | 35K | B |
| 03 | 03-transport-split.md | leaf | 00T, foundation | 30K | B |
| 04 | 04-resampler-reuse.md | leaf | 00T | 25K | C |
| 05 | 05-chunk-size-knob.md | leaf | 00T, foundation | 30K | C |
| 06 | 06-tts-docs.md | leaf | 01-05 | 20K | D |

File ownership: 01 owns daemon `voice_clip_planner.go`; 02 owns child
`clips.go`; 03 owns `tts_openai.go`; 04 owns `resample.go`; 05 owns
`tts_openai.go` (CONFLICT with 03: serialize 05 after 03 COMPLETE).

## Dispatch Protocol

1. Phase 0: dispatch `00T-tts-tests.md`; validate IN-SESSION; commit RED
   tests after manual gate record.
2. Phase 1: dispatch 01, 02, 03 in parallel (file-disjoint). Review; commit.
3. Phase 2: dispatch 04 and 05 after 03 COMPLETE (05 shares file with 03).
4. Phase 3: dispatch 06 docs. Review; commit.
5. Record statuses; report COMPLETE to root.

Review IN-SESSION only. Max 3 re-dispatch cycles.

## Review Checklist

- [ ] All leaf tasks implemented
- [ ] C1/C2 contracts satisfied; defaults exact
- [ ] Gate tests pass GREEN and unmodified
- [ ] No I/O under playMu; no nested locks
- [ ] Legacy paths byte-identical
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
| R1 | "optimizations and refinements based on our skills" | F3/F6/F8/F9/F10 leaves | 00T + leaf tests |
| R4 | "keep everything opt-in or byte-identical by default" | C1/C2 defaults | additive tests |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-tts-tests | COMPLETE | 10 e0475e9d: gate committed, RED confirmed, now GREEN |
| 01-clip-deferral | COMPLETE | 10 52800c19: F3 defer past played edge; delay=0 legacy |
| 02-clip-yield | COMPLETE | 10 52800c19: F10 render yields to live synth; deferred specs retained |
| 03-transport-split | COMPLETE | 10 dd53a70c: F6 cloud keep-alive; qwen3 no-keepalive unchanged |
| 04-resampler-reuse | COMPLETE | 10 dd53a70c: F8 scratch reuse; passthrough aliasing staged |
| 05-chunk-size-knob | COMPLETE | 10 5ebd58b5: F9 override, 0 => 4 |
| 06-tts-docs | COMPLETE | 10 5561c999: TTS knobs documented |

## Integration Test Plan

```bash
go test -p 2 ./internal/daemon/... ./internal/voicechild/...
go test -p 2 -race ./internal/voicechild/...
make e2e-fast-area AREA=voice_clips
```
Expected: green; delay=0 renders immediately; chunk=0 sends 4;
cloud vendor pools, qwen3 does not; resampler output byte-identical.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F3 | Clip deferral after played edge | 01-clip-deferral.md | `TestClipPrewarmDeferred` | voice_clips | VALIDATED |
| F10 | Render yield to live synth | 02-clip-yield.md | `TestRenderYieldsToLive` | voice_clips | VALIDATED |
| F6 | Transport split | 03-transport-split.md | `TestCloudKeepAlive`, `TestQwen3NoKeepAlive` | n/a (internal) | VALIDATED |
| F8 | Resampler buffer reuse | 04-resampler-reuse.md | `TestResamplerReuseByteIdentical` | n/a (internal) | VALIDATED |
| F9 | Chunk size knob | 05-chunk-size-knob.md | `TestQwen3ChunkSizeOverride` | voice_clips | VALIDATED |

## Open Questions

- Q4: benefit of F3 and F9 unmeasured; A/B on target box needed before
  recommending non-zero delay or non-4 chunk.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dated dispatch addenda.

## Notes

- F3 research evidence: `docs/research/realtime-tts-options.md` attributes
  part of a measured 2-6 s gap to clip-prewarm contention.
