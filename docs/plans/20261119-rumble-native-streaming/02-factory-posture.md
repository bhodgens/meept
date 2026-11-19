# Native-vendor factory posture - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. Do NOT commit. Explore with
> search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Prevent double-partialing by bypassing PseudoStreamSTT for native vendors
- **Dependencies:** gates validated
- **Estimated Context:** 35K
- **Concurrency Group:** C
- **Audit references:** F3, F5

## Goal

`NewSTT` currently wraps ANY base provider in `PseudoStreamSTT` when
`PartialIntervalMs > 0`. That is wrong for rumble (now natively streaming) and
deepgram (already streams genuine partials): both would double-partial. Add a
small `nativeStreamingVendor` guard so the client-side proxy is used only for
true batch vendors (whisper). Whisper default stays byte-identical.

## Context

- `internal/voicechild/providers/stt_deepgram.go` - NewSTT wrapper (owner)
- `internal/voicechild/providers/pseudostream.go` - decorator (do not edit)
- C4 in ../master.md is authoritative.

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go` - `func NewSTT`
- `internal/voicechild/providers/pseudostream.go` - `newPseudoStreamSTT`

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func nativeStreamingVendor(vendor string) bool
// true for "rumble" and "deepgram"; false for "whisper", "", unknown.
```

### What This Leaf Consumes

- C1 STTProvider, C4 factory posture - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add guard and change NewSTT | Modify: `internal/voicechild/providers/stt_deepgram.go` (NewSTT wrapper only) | `go test ./internal/voicechild/providers/ -run 'TestNewSTTNativeNoDecorator|TestNewSTTDefaultNoDecorator|TestNewSTT_Rumble' -count=1 -v` | decorator wraps rumble | PASS | Given PartialIntervalMs>0 + vendor rumble/deepgram; When NewSTT; Then base provider; Given whisper; When NewSTT + interval; Then *PseudoStreamSTT |

T1 detail: add `nativeStreamingVendor(v string) bool` in the same file; use it in
NewSTT:
```
base, err := newBaseSTT(cfg, key)
if err != nil { return nil, err }
if cfg.PartialIntervalMs <= 0 || nativeStreamingVendor(cfg.Vendor) {
    return base, nil
}
return newPseudoStreamSTT(cfg, key, newBaseSTT)
```
Do not change newBaseSTT cases. Do not edit pseudostream.go or any test file.

## Self-Verification Checklist

- Task implemented; gate tests GREEN; gate files untouched
- Native vendors bypass decorator; whisper still wraps when opted in
- Default path byte-identical; gofmt clean; no scope creep

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Guard exact vendors; no change to base switch; no gate weakening

Output: APPROVED or specific gaps with file + line references.

## Notes

The pseudo-stream decorator remains available for whisper users who want
client-side partials; this leaf only stops applying it where it is harmful.
