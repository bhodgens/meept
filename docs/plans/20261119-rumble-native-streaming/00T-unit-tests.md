# Rumble native streaming - Unit Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1-F5. Do NOT write
> implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons (wrong wire shape / missing symbols), not
> syntax errors. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Unit gate tests for native Rumble WS protocol and factory posture
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** F1, F2, F3, F4, F5

## Goal

Provide executable RED unit tests defining: binary PCM sent over WS,
partial/final/end mapped to Transcripts, flush frames, silence handling,
and the native-vendor no-decorator guard. These REPLACE the stale
chat.completions tests in `stt_rumble_test.go`.

## Context

Contracts C1-C5 in ../master.md. Anchors:
- `internal/voicechild/providers/stt_rumble.go` - current batch adapter (to be rewritten)
- `internal/voicechild/providers/stt_rumble_test.go` - stale chat.completions tests (rewrite)
- `internal/voicechild/providers/stt_deepgram.go` - NewSTT + wsSession seam
- `internal/voicechild/providers/stt_test.go:372-390` - dialFakeWS httptest pattern

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:85-90` - dialDeepgram via wsutil
- `internal/voicechild/providers/stt_deepgram.go:120-135` - wsSession interface
- `internal/voicechild/providers/stt_test.go:372-390` - dialFakeWS pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test files only; no production symbols.

### What This Leaf Consumes

- C1 STTProvider, C2 WS protocol, C3 mapping, C4 factory, C5 config

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Native WS event mapping | Rewrite: `internal/voicechild/providers/stt_rumble_test.go` | `go test ./internal/voicechild/providers/ -run TestRumbleNativeStreamEvents -count=1` | wrong wire / no partial | PASS | Given fake WS server sending partial/final/end; When PCM fed; Then matching Transcripts emitted in order |
| T2 | Flush frames | Modify same file | `go test ./internal/voicechild/providers/ -run TestRumbleNativeFlush -count=1` | no empty frame sent | PASS | Given pcm close or FlushNow; Then server receives one empty binary frame and end is honored |
| T3 | Native vendor not decorated | Modify: `internal/voicechild/providers/pseudostream_test.go` (new func ONLY) | `go test ./internal/voicechild/providers/ -run TestNewSTTNativeNoDecorator -count=1` | decorator wraps rumble | PASS | Given PartialIntervalMs>0 with vendor rumble; When NewSTT; Then base *RumbleSTT returned, not *PseudoStreamSTT |
| T4 | Silence | Modify: `internal/voicechild/providers/stt_rumble_test.go` (new func) | `go test ./internal/voicechild/providers/ -run TestRumbleNativeSilence -count=1` | hallucinated transcript | PASS | Given fake server sends only end with empty text; When PCM fed; Then no partial/final, channel closes |
| T5 | Whisper default unchanged | Modify: `internal/voicechild/providers/stt_deepgram_test.go` (new func) | `go test ./internal/voicechild/providers/ -run TestNewSTT_DefaultUnchanged -count=1` | n/a guardrail | PASS | Given vendor whisper; When NewSTT; Then unchanged |

T1 detail: fake WS server via `websocket.Accept` at path
`/v1/audio/transcriptions/stream`. On receiving binary frames, send JSON:
`{"type":"partial","text":"Yesterday it was"}`,
`{"type":"final","text":"Yesterday it was"}`,
`{"type":"partial","text":"30 degrees"}`,
`{"type":"final","text":"30 degrees"}`,
`{"type":"end","text":"Yesterday it was 30 degrees"}`.
Set cfg Vendor=rumble, BaseURL=server.URL (http -> ws via dialURL). Assert
received transcripts: partial("Yesterday it was",false), final("Yesterday it was",true),
partial("30 degrees",false), final("30 degrees",true), and no duplicate final
from end (end text fully covered by prior finals). Assert no Authorization header
on the handshake. Timeout <=3 s.

T2 detail: fake server records whether it saw an empty binary frame; assert
it arrives after pcm close and after a FlushNow() kick (two separate subtests).
T3 detail: use reflection on the returned provider type name; expect `*providers.RumbleSTT` not `*providers.PseudoStreamSTT` when PartialIntervalMs=50 and Vendor=rumble. BaseURL required to avoid constructor error.

T4 detail: server sends only `{"type":"end","text":""}`; expect zero transcripts and clean close.

## Self-Verification Checklist

- F1-F5 covered; RED for missing implementation only
- No production files touched; no commit; no git add; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + last 20 lines (showing RED)
3. Deviations

## Review Checklist (For Review Agent)

- Every F row covered; RED reasons correct; native protocol pinned exactly
- No implementation code; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

The stale `TestRumbleSTTStream` chat.completions test MUST be removed or rewritten
to the native WS protocol by this gate leaf, because it encodes the old behavior
the implementation leaf replaces.
