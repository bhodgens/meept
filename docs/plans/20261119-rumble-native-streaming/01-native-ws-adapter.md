# Rumble native streaming adapter - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist and replaced the stale chat.completions tests. Do NOT delete/skip/
> weaken gate tests. Do NOT commit. Explore with search_files/terminal.
> Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Rewrite RumbleSTT as a native WebSocket streaming adapter
- **Dependencies:** 00T + 00Te at TESTS_VALIDATED
- **Estimated Context:** 75K
- **Concurrency Group:** B
- **Audit references:** F1, F2, F4

## Goal

Replace the batch HTTP `RumbleSTT` with a native streaming adapter that opens
ONE WebSocket per call to `/v1/audio/transcriptions/stream`, forwards PCM as
binary frames, and maps the server's `partial`/`final`/`end` JSON events to
`Transcript` values. No base64, no WAV header, no Authorization header, and
no client-side pseudo-streaming.

## Context

- `internal/voicechild/providers/stt_rumble.go` - file to rewrite (keep the path)
- `internal/voicechild/providers/stt_deepgram.go:61-90` - `dialURL` helper, `dialDeepgram`
- `internal/voicechild/providers/stt_deepgram.go:120-135` - `wsSession`, `wsutilSession`
- C2/C3 in ../master.md are authoritative (live server source verified)

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:65-74` - dialURL (http->ws, ws passthrough)
- `internal/voicechild/providers/stt_deepgram.go:85-90` - dialDeepgram via wsutil
- `internal/voicechild/providers/stt_deepgram.go:125-135` - wsSession methods
- `docs/research/rumble2flash-evaluation.md` - evaluation context

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func newRumbleSTT(cfg ProviderConfig) (voicechild.STTProvider, error)
type RumbleSTT struct { ... }   // implements voicechild.STTProvider + FlushNow()
const rumbleStreamPath = "/v1/audio/transcriptions/stream"
```

### What This Leaf Consumes

- C1 STTProvider, C2 WS wire, C3 mapping - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Rewrite adapter to native WS | Rewrite: `internal/voicechild/providers/stt_rumble.go` | `go test ./internal/voicechild/providers/ -run 'TestRumbleNativeStreamEvents|TestRumbleNativeSilence' -count=1 -v` | stale HTTP transport | PASS | Given fake WS server; When PCM fed; Then partial/final mapped, silence yields only end |
| T2 | Flush lifecycle | Modify same file | `go test ./internal/voicechild/providers/ -run TestRumbleNativeFlush -count=1 -v` | no flush frame | PASS | Given pcm close or FlushNow; Then one empty binary frame sent, end honored |
| T3 | Cleanup + vet | Modify same file | `go vet ./internal/voicechild/providers/ && gofmt -l internal/voicechild/providers/stt_rumble.go` | n/a | PASS | Given final code; When vet/gofmt; Then clean |

T1 detail:
- Keep `newRumbleSTT(cfg)` signature and `ErrMissingRumbleBaseURL` guard; default
  SampleRate 16000; default Model "Reson8/Rumble-2-Flash" (unused on the wire but
  kept for config compatibility).
- `Stream(ctx, pcm)`: dial `dialURL(cfg.BaseURL, rumbleStreamPath)` with empty
  headers (keyless) via `wsutil.Dial` / the `wsSession` seam. Use a package var
  `rumbleDialer` (like `sttDialer`) so an httptest WS server can swap it. Return a
  named error on dial failure; create buffered `out` channel cap 16.
- Spawn ONE pump goroutine: sole writer to the WS. It reads `pcm`, writes each
  non-empty chunk as a binary frame (messageType 2) unchanged, and on channel
  close sends one empty binary frame (flush). A `flushCh` buffered(1) receives
  FlushNow kicks; on kick, send one empty binary frame (coalesced).
- Spawn ONE reader goroutine: reads JSON text frames into
  `struct{Type,Text string}`; map `partial`->Final:false, `final`->Final:true,
  `end`->flush remainder then close out. Track finalized text so the `end`
  event's full transcript does not duplicate finals already emitted: compute the
  remainder = strings.TrimSpace(end.Text) with the concatenation of prior final
  texts removed as a prefix; emit one final if remainder non-empty. Skip empty
  partial/final texts. On `end`, cancel the pump and close the WS. On read error
  or ctx cancel, close out silently.
- NO mutex across network I/O. `FlushNow` only does a non-blocking send on
  flushCh. Track any last-seen text only in the reader goroutine. No new dependency.

T2 detail: ensure exactly one flush frame on pcm close; a later FlushNow after
close is a no-op (channel closed). The reader's `end` terminates the stream even
if the pump is still feeding; guard `out` close with sync.Once or a single owner
(the reader). On mid-stream WS error, close `out` without emitting a final.

T3 detail: remove now-unused base64/bytes/net/http imports; gofmt and go vet clean.

## Self-Verification Checklist

- All tasks implemented; all native gates GREEN; gate files untouched
- Single writer goroutine; no I/O under mutex; buffered out channel
- Exact wire: binary PCM in, partial/final/end out, empty flush frame
- No Authorization header; no base64/WAV on WS path; fail-soft mid-stream
- gofmt + go vet clean; no new dependency

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Native protocol exact; partials before finals; end emits only uncovered remainder
- Flush frame exactly once; no double-close of out; no I/O under mutex
- ErrMissingRumbleBaseURL kept; keyless; gate tests untouched

Output: APPROVED or specific gaps with file + line references.

## Notes

The server decodes on a cadence itself; do NOT add client-side timers or
PseudoStreamSTT here. The pipeline receives real partials and uses them for
barge-in/context-warm exactly as it does for deepgram.
