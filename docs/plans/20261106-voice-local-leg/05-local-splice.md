# LocalSplice + AudioPair local mode - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.

## Meta

- **Parent:** master.md
- **Scope:** The local-leg media path: LocalSplice between a client byte
  stream and the child's C2 pair, plus an AudioPair tweak the local mode
  needs.
- **Dependencies:** none (wave A; consumes only socket.go's existing API)
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voice/localsplice.go (new),
  internal/voice/localsplice_test.go (new), internal/voice/socket.go
  (additive-only changes to AudioPair)

## Goal

Give the daemon a splice that has no carrier: one side is the child socket
pair (unchanged protocol), the other side is an arbitrary
io.ReadWriter/Writer pair the caller supplies (leaf 07 hands it unix
conn(s); tests hand it net.Pipe). 20 ms pacing out, chunking in, per-call
interrupt with clear-to-child and a cancel callback, no reorder, no
transcode, L16@16000 only.

## Context

Key files:
- internal/voice/splice.go - RunSplice: pacing loop shape, barge-in
  handling, deps pattern. Borrow the shape, NOT the reorder/transcode/WS
  paths. Its comments cite the uptractor pacing rule; mirror the reason.
- internal/voice/socket.go AudioPair - accepts one conn per socket already;
  exposes Write/Read helpers? (Check current exported surface; the phone
  splice receives `audio` and calls its methods.) If AudioPair lacks a
  method that streams the child .out to an io.Writer, add the loop HERE in
  LocalSplice (keep socket.go changes to what is strictly missing; an
  `OutReader()` or equivalent additive accessor is allowed).
- sun_path limit: pair files live under voice.SocketDir() (already under
  $MEEPT_HOME) - nothing new.

## Interface Contracts (From Parent)

CL7 (verbatim, extended with the exact API this leaf freezes):

```go
// internal/voice/localsplice.go
type LocalSpliceConfig struct {
    Pace         time.Duration // 20ms default; 0 -> 20ms
    ChunkBytes   int           // 0 -> 640 (L16@16k enforced)
    JoinTimeout  time.Duration // 0 -> 10s; client must connect/first-byte
                               // within it or ErrNoClient (Open-side grace)
}
type LocalSpliceDeps struct {
    Audio       *AudioPair
    Client      io.ReadWriter  // mic bytes in, speaker bytes out (single
                               // conn shape; leaf 07 adapts two-socket
                               // clients by piping)
    Text        TextSender     // clear frame to child on barge-in
    CancelDraft func(ccid string)
    Logger      *slog.Logger
}
func RunLocalSplice(ctx context.Context, ccid string, cfg LocalSpliceConfig, deps LocalSpliceDeps) error
var ErrNoClient error
```

Rules: outbound paced 20 ms/chunk of 640 B from the child .out; inbound
chunked to 640 B into the child .in; interrupt (via HandleBargeIn-style
API: a `HandleInterrupt(ccid)` exported method on the splice state, called
by leaf 07's frame path) -> stop writes, send Frame{call_id, event:"clear"}
via Text, CancelDraft once, re-arm when fresh child PCM arrives (same
per-barge-in latch contract as phone). No Telnyx import. L16-only: chunk
size other than 640 in config returns a named error at start.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: bytes written to Client appear on child .in chunk-aligned; bytes pushed onto child .out reach Client paced (assert >= N*20ms wall, not exact) | localsplice_test.go | go test -p 2 ./internal/voice/ -run TestLocalSpliceCarries | no file | PASS |
| T2 | Test: interrupt -> clear frame sent once, writes stop, re-arm on next child PCM; other-call isolation by construction (one RunLocalSplice per ccid) | same | -run TestLocalSpliceBargeIn | - | PASS |
| T3 | Test: JoinTimeout -> ErrNoClient when client never connects/never writes | same | -run TestLocalSpliceJoinTimeout | - | PASS |
| T4 | Implement RunLocalSplice per CL7 | localsplice.go | T1-T3 | - | PASS |
| T5 | Additive AudioPair surface only if missing (accessors); no behavior change to phone paths | socket.go | go test -p 2 ./internal/voice/ -run TestAudioPair | - | PASS |
| T6 | gofmt/vet/race | - | go vet ./internal/voice/...; go test -p 2 -race ./internal/voice/ -run 'TestLocalSplice|TestAudioPair' | - | clean |

## Interface Contract (What This Leaf Exposes)

RunLocalSplice, LocalSpliceConfig, LocalSpliceDeps, ErrNoClient, and the
splice's HandleInterrupt(ccid) entry point (leaf 07 wires the C1 interrupt
frame to it).

## Self-Verification Checklist

- [ ] Phone splice tests untouched and green
- [ ] No goroutine leak on ctx cancel (race + count check)
- [ ] No Telnyx imports in localsplice.go
- [ ] ASCII only

## Review Checklist

- [ ] Pacing rule matches splice.go's documented rationale
- [ ] Barge-in latch is per-barge-in (re-arm contract, bughunt F-1 spirit)
- [ ] Clear frame shape byte-exact ({"call_id":...,"event":"clear"})
- [ ] No artifacts; additive-only socket.go edits
