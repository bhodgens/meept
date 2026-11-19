# Generic pseudo-stream decorator - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. Do NOT commit. Explore with
> search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Provider-agnostic client-side pseudo-streaming wrapper
- **Dependencies:** 01 (newBaseSTT seam + C2 fields)
- **Estimated Context:** 65K
- **Concurrency Group:** C
- **Audit references:** F2, F4

## Goal

Implement `PseudoStreamSTT`: a decorator around any batch `STTProvider` that
periodically snapshots the accumulated PCM into a fresh provider call, emitting
`Transcript{Final:false}` for intermediate texts and exactly one `Final:true` on
stream close or FlushNow. It depends on the `STTProvider` interface only, so it
works with whisper, rumble, or any future batch vendor. Strictly opt-in via
`cfg.PartialIntervalMs > 0`.

## Context

- `internal/voicechild/providers/stt_deepgram.go` - NewSTT (after leaf 01 split: newBaseSTT wrapper)
- `internal/voicechild/pipeline.go:24-49` - C1 STTProvider / Transcript / sttFlusher
- `internal/voicechild/providers_wiring.go:17-30` - ProviderConfig C2 fields
- C5 in ../master.md is authoritative.

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:154-172` - NewSTT switch (pre-split)
- `internal/voicechild/pipeline.go:24-26,45-49` - STTProvider / Transcript
- `internal/voicechild/providers/stt_rumble.go:68-85` - FlushNow kick pattern to mirror

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type STTBaseFactory func(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
type PseudoStreamSTT struct { ... }
func newPseudoStreamSTT(cfg ProviderConfig, key string, base STTBaseFactory) (voicechild.STTProvider, error)
func (p *PseudoStreamSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error)
func (p *PseudoStreamSTT) FlushNow()
var _ voicechild.STTProvider = (*PseudoStreamSTT)(nil)
```

### What This Leaf Consumes

- C1 STTProvider, C2 ProviderConfig (PartialIntervalMs), C5 - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Implement decorator | Create: `internal/voicechild/providers/pseudostream.go` | `go test ./internal/voicechild/providers/ -run 'TestPseudoStreamEmitsPartialsThenFinal' -count=1 -v` | undefined newPseudoStreamSTT | PASS | Given fake base factory + interval; When PCM fed over time and closed; Then >=1 partial then exactly one final |
| T2 | Wire decorator into NewSTT | Modify: `internal/voicechild/providers/stt_deepgram.go` (NewSTT wrapper only) | `go test ./internal/voicechild/providers/ -run 'TestNewSTTDefaultNoDecorator|TestNewSTT_Rumble' -count=1 -v` | n/a guardrail | PASS | Given PartialIntervalMs=0; When NewSTT; Then exact base type; Given >0; When NewSTT; Then *PseudoStreamSTT |

T1 detail: internal state per Stream: `buf []byte`, `ticker := time.NewTicker(interval)`,
`out` channel. On each tick with len(buf)>0: copy buf, call `base(cfg,key)` to get a
FRESH provider, call `Stream(ctx, snapshotChan)` feeding the copied bytes and
closing the channel, read the LAST final transcript, emit `Transcript{Text:text,
Final:false}` if non-empty. Do this work in a goroutine; never hold a mutex across
base.Stream I/O. On pcm close or FlushNow kick: one last snapshot, emit exactly one
`Transcript{Final:true}` (text from last recognition, empty allowed), then close.
On ctx cancel: close without final. Fail-soft: base errors produce empty text.
Guard the buffer with a mutex only around append/copy, never during calls.

T2 detail: after leaf 01's split, modify only the `NewSTT` thin wrapper:
```
func NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error) {
    base, err := newBaseSTT(cfg, key)
    if err != nil || cfg.PartialIntervalMs <= 0 { return base, err }
    return newPseudoStreamSTT(cfg, key, newBaseSTT)
}
```
No other vendor case changes. Do not touch `newBaseSTT` body.

## Self-Verification Checklist

- All tasks implemented; gate tests GREEN; gate files untouched
- Decorator depends on STTProvider/factory only; no vendor type switch
- Fresh base provider per snapshot; no I/O under mutex; FlushNow safe
- Default (interval 0) byte-identical; gofmt + go vet clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Generic (no vendor coupling); one final exactly; partials before final
- No I/O under mutex; opt-in only; gate tests untouched

Output: APPROVED or specific gaps with file + line references.

## Notes

Native partial vendors (deepgram) already stream; enabling this on them yields
duplicate partials. Document in docs leaf 04; do not special-case in code.
