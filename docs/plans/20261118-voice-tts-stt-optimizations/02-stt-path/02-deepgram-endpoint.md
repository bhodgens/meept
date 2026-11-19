# Deepgram authoritative endpoint (F4) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Parse speech_final / UtteranceEnd into Transcript.Endpoint and fast-dispatch accumulated finals behind opt-in
- **Dependencies:** 00T-stt-tests.md, foundation C1
- **Estimated Context:** 40K
- **Concurrency Group:** B
- **Audit references:** F4

## Goal

Deepgram distinguishes a finalized segment from a speech endpoint. Today the adapter drops both signals and endpointing is a fixed 900 ms timer. Consume the endpoint as an opt-in fast path.

## Context

`dgMessage` has is_final/from_finalize only (`stt_deepgram.go:41-50`). `emit` discards non-Results messages (`:332-338`), including `UtteranceEnd` (constant at `:37`). Accumulation lives in `pipeline.go:992-1027` and dispatch in `:1077-1101`.

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:34-50` - message types
- `internal/voicechild/providers/stt_deepgram.go:327-345` - emit
- `internal/voicechild/pipeline.go:46` - Transcript
- `internal/voicechild/pipeline.go:1205-1360` - consumeTranscripts
- `internal/voicechild/keys.go` - DeepgramEndpointMs selector

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Transcript.Endpoint bool; when DeepgramEndpointMs>0 and Endpoint==true, dispatch all accumulated parts immediately through existing guards.
```

### What This Leaf Consumes

- C2 Transcript.Endpoint - this branch orchestrator
- C1 DeepgramEndpointMs - foundation 01-keys-contract.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Parse speech_final and UtteranceEnd into Endpoint | Modify: `internal/voicechild/providers/stt_deepgram.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestDeepgramEndpointFlag`` | field absent | PASS | Given speech_final JSON; When emit; Then Transcript.Endpoint=true; Given metadata; Then no transcript emitted |
| T2 | Fast-dispatch accumulated finals on endpoint | Modify: `internal/voicechild/pipeline.go:1205-1360` | ``go test -p 2 ./internal/voicechild/ -run TestEndpointFastDispatch`` | flag unused | PASS | Given DeepgramEndpointMs>0, pending finals, Endpoint=true; When consumed; Then all parts dispatched once via dispatchAggregated; Given ms=0; Then timer path unchanged |
| T3 | Respect mic-hold and echo guards | Modify: `internal/voicechild/pipeline.go` | ``go test -p 2 ./internal/voicechild/ -run TestEndpointGuards`` | guards bypassed | PASS | Given endpoint during agent playback; When consumed; Then echo guard suppresses dispatch; existing provenance unchanged |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] Interface contracts satisfied exactly (field names, defaults, paths)
- [ ] All files at exact specified paths
- [ ] No scope creep; no debug artifacts, no TODOs, no line-number corruption
- [ ] Default values preserve pre-change behavior byte-identically when the new knob is unset

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + output (last 20 lines)
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched
- [ ] Contracts match exactly (signature, type, path, default)
- [ ] No I/O under playMu; no nested locks
- [ ] No scope creep; no bugs; no secrets in logs

## Notes

- Keep all pending parts; never dispatch only the last endpoint response.
- False-cutoff risk: hesitation fixtures required before enabling in production.
