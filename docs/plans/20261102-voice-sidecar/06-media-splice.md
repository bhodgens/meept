# Media splice - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate tests ALREADY EXIST
> (parked .staged from leaf 00T T4) - make them pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voice media splice: Telnyx WS <-> per-call audio sockets; frame reorder window; silence fill; codec transcode (PCMU fallback); outbound pacing 20ms; barge-in clear frame; bounded queue with degraded-then-hangup
- **Dependencies:** leaf 02 (telnyx types), leaf 03 (socket + ChunkBytes)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voice/splice.go, internal/voice/codec.go

## Goal

The daemon's audio path per call. Inbound: read Telnyx media frames
(base64 payload + sequence_number + media.timestamp), reorder, transcode
if PCMU, write fixed-size PCM chunks to the child's audio socket.
Outbound: read child PCM, pace one chunk per 20 ms real time, encode
media frames to Telnyx. Barge-in: on interrupt for this call, stop
writing, send {"event":"clear"} to Telnyx, drop the outbound queue.

## Context

Telnyx media WS frame shapes (verified against Telnyx docs 2026-11):
inbound {"event":"media","media":{"payload":"<base64>","timestamp":...}}
with top-level "sequence_number"; frame order NOT guaranteed; client may
send {"event":"media",...}, {"event":"mark"}, {"event":"clear"}. Outbound
payloads are base64 RTP payloads (headerless raw audio). L16 on this
stream = s16le PCM (NOT big-endian RTP L16) - forward without byte swap.
Borrow from uptractor (terminal cat):
- ~/git/aiedge-uptractor/core/internal/mediabridge/mulaw.go - mulaw8ToPCM16 / pcm24ToMulaw8 (verbatim, module-adapt)
- ~/git/aiedge-uptractor/core/internal/mediabridge/bridge.go - EncodeMediaFrame/DecodeMediaFrame, waitInterrupt poll-between-writes pattern, chunkPace 20ms, barge-in {"event":"clear"} write, WithGreeting first-speaks rule
Do NOT borrow: arrival-order forwarding (no reorder) - this leaf ADDS the
reorder window (spec "Frame order"): buffer up to reorder_window_frames,
emit contiguous run by sequence_number, gap persisting > reorder_gap_ms
filled with silence of the missing duration + metric count, late frames
past the window discarded. Backpressure: bounded per-call queue; full =
call degraded; grace window then hangup via injected HangupFn. NEVER drop
oldest silently.
Pacing: one ChunkBytes-sized frame per 20ms (time.Ticker or timer; fake
clock injectable for tests). A burst overflows the carrier playout buffer
as crackle (uptractor live drill 2).
Barge-in per call: interrupt frame (C1) -> stop outbound writes for THIS
ccid, Telnyx clear frame, flush queue. Other calls untouched. The
employee/supervisor draft cancel is signaled via an injected
CancelDraftFn(ccid) (leaf 14 wires it to the turn context).

websocket lib: github.com/coder/websocket (uptractor uses it; add to
go.mod). Signature verification happened at accept time (leaf 10); the
splice assumes a verified conn.

## Verified Anchors

- ~/git/aiedge-uptractor/core/internal/mediabridge/bridge.go:22-38 - chunk constants + pace rationale (chunk size becomes codec-derived here)
- ~/git/aiedge-uptractor/core/internal/mediabridge/bridge.go:346-383 - waitInterrupt (poll shape; adapt to interrupt frame from text socket)
- ~/git/aiedge-uptractor/core/internal/mediabridge/mulaw.go - transcode pair
- internal/voice/frame.go (leaf 03) - ChunkBytes, Frame

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type SpliceConfig struct{ Codec telnyx.Codec; ReorderWindowFrames int; ReorderGap time.Duration; Pace time.Duration /*20ms*/; QueueLimit int }
type SpliceDeps struct{ TelnyxWS WebSocketConn; Audio *AudioPair; Text TextSender; HangupFn func(ccid, reason string); CancelDraftFn func(ccid string); Logger *slog.Logger; Clock func() time.Time }
func RunSplice(ctx context.Context, ccid string, cfg SpliceConfig, deps SpliceDeps) error
// WebSocketConn narrow interface (Read/Write/Close) so tests fake it
```

### What This Leaf Consumes

- C2 chunk sizes + Codec (leaves 02/03); C1 interrupt/clear semantics (master.md)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate tests; confirm RED | mv internal/voice/splice_test.go.staged -> splice_test.go | go test -p 2 ./internal/voice/ -run TestSplice | undefined: RunSplice | RED confirmed | - |
| T2 | Codec borrow: mulaw pair verbatim; L16 pass-through (no byte swap) pinned by test | Create: internal/voice/codec.go | go test -p 2 ./internal/voice/ -run TestCodec | - | PASS | Given s16le bytes; When L16 path; Then output == input |
| T3 | Reorder: seq 1,3,2 -> child gets 1,2,3; gap>reorder_gap_ms -> silence fill of exact missing duration + counter; late-past-window discarded | Create: internal/voice/splice.go | go test -p 2 ./internal/voice/ -run TestSpliceReorder | - | PASS | Given out-of-order frames; When spliced; Then contiguous order at child |
| T4 | Pacing: fake clock; N outbound chunks take N*20ms; barge-in between writes lands within one chunk interval | (same) | go test -p 2 ./internal/voice/ -run TestSplicePacing | - | PASS | Given 10 chunks; When paced; Then clock advanced >= 180ms before chunk 10 |
| T5 | Barge-in: interrupt -> Telnyx receives {"event":"clear"}, outbound queue empty, CancelDraftFn(ccid) called once, second call's splice unaffected (two concurrent RunSplice) | (same) | go test -p 2 -race ./internal/voice/ -run TestBargeInPerCall | - | PASS | Given two live splices; When interrupt on A; Then B keeps writing |
| T6 | Backpressure: queue full -> degraded log; grace exceeded -> HangupFn(ccid, reason) | (same) | go test -p 2 ./internal/voice/ -run TestSpliceBackpressure | - | PASS | Given stalled child reader; When queue exceeds limit past grace; Then hangup invoked with named reason |
| T7 | Format/vet/race | - | gofmt -l internal/voice; go vet ./internal/voice/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] Gate tests passing untouched
- [ ] No drop-oldest path anywhere (grep before reporting)
- [ ] Chunk size from ChunkBytes(codec,rate), never a fixed constant
- [ ] L16 bytes never swapped; PCMU path only when start frame says PCMU
- [ ] -race clean with two concurrent splices
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list vs uptractor source

## Review Checklist (For Review Agent)

- [ ] Reorder window semantics match spec Frame order section exactly
- [ ] Pacing cannot burst (ticker-driven, not write-speed-driven)
- [ ] clear frame byte-exact {"event":"clear"}
- [ ] HangupFn/CancelDraftFn injected, never direct telnyx client calls from splice

Output: APPROVED or specific gaps.

## Notes

media.timestamp vs sequence_number: prefer sequence_number when present;
fall back to timestamp ordering within the window. Document the choice in
code comments.
