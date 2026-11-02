# Socket protocol - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Gate tests from 00T that pin frame
> shapes apply (T4/T10 fakes consume this leaf's types). Do NOT commit. Do
> NOT use read_file on existing source files - explore with search_files
> or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voice socket layer: C1 text frames (shared text socket, per-frame call_id), C2 per-call audio socket pairs, frame codec helpers
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voice/socket.go, internal/voice/frame.go, internal/voice/socket_test.go (new pins allowed here; 00T parked splice tests consume these types)

## Goal

The wire the daemon and child speak. One shared text socket (JSON lines,
every frame carries call_id) plus one audio socket pair per call named by
call_control_id, so a slow text reader never blocks audio and one call
never blocks another.

## Context

Unix sockets under $MEEPT_HOME/voice/sock/. Text socket: daemon listens,
child connects at start. Audio: daemon creates `<ccid>.in` (daemon writes,
child reads) and `<ccid>.out` (child writes, daemon reads) per call;
removed on call end. Frames are raw PCM, no envelope. Codec-derived chunk
sizes live here as constants+helper (L16@16k=640B, PCMU@8k=160B per 20ms).
The socket layer does NOT reorder or pace (leaf 06 owns that); it moves
bytes.

Precedent for daemon-side unix socket handling: internal/rpc (socket
server shape). Do not import internal/rpc; mirror the accept-loop pattern.

## Verified Anchors

- internal/config/home.go - MeeptPath() resolution (socket dir derives from it, never a hardcoded ~/.meept)
- master.md C1/C2 - frame schemas verbatim

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voice/frame.go
type Frame struct {
  CallID    string `json:"call_id"`
  Event     string `json:"event"` // setup|prompt|interrupt|end|say|clear
  Text      string `json:"text,omitempty"`
  Last      *bool  `json:"last,omitempty"`
  From, To  string `json:"from,omitempty"` / `json:"to,omitempty"`
  Direction string `json:"direction,omitempty"` // inbound|outbound
  Codec     string `json:"codec,omitempty"`     // L16|PCMU
  SampleRate int   `json:"sample_rate,omitempty"`
  Reason    string `json:"reason,omitempty"`
}
func EncodeFrame(f Frame) ([]byte, error)      // JSON + '\n'
func DecodeFrame(line []byte) (Frame, error)
func ChunkBytes(codec string, sampleRate int) int  // 640 | 160 | error

// internal/voice/socket.go
type TextServer struct{ ... }  // listens, accepts one child, routes frames by CallID
func NewTextServer(path string, logger *slog.Logger) *TextServer
func (s *TextServer) Start(ctx context.Context) error
func (s *TextServer) Send(f Frame) error                 // to child
func (s *TextServer) SetHandler(fn func(Frame))           // frames from child
type AudioPair struct{ ... }   // per-call in/out unix sockets
func NewAudioPair(dir, ccid string) (*AudioPair, error)
func (p *AudioPair) WriteToChild(pcm []byte) error
func (p *AudioPair) ReadFromChild(buf []byte) (int, error)
func (p *AudioPair) Close() error   // removes both socket files
```

### What This Leaf Consumes

- C1/C2 (master.md) verbatim

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Frame codec: encode/decode round-trip; unknown event decodes but is typed; Last is *bool so omitted != false; dtmf frames decode without error (handler may count/ignore; splice never sees them) | Create: internal/voice/frame.go, frame_test.go | go test -p 2 ./internal/voice/ -run TestFrame | - | PASS | Given Frame{Event:"prompt",Last:true}; When round-tripped; Then identical; Given no last key; Then Last==nil; Given {"event":"dtmf"}; Then DecodeFrame succeeds |
| T2 | ChunkBytes: L16/16000->640, PCMU/8000->160, unknown->error | (same) | go test -p 2 ./internal/voice/ -run TestChunkBytes | - | PASS | Given codec pair; When ChunkBytes; Then exact size |
| T3 | TextServer: listen/accept/send/handler; concurrent Send from multiple goroutines safe (-race); closed child connection surfaces error, no panic | Create: internal/voice/socket.go, socket_test.go | go test -p 2 -race ./internal/voice/ -run TestTextServer | - | PASS | Given connected fake child; When daemon Sends say; Then child reads exact JSON line |
| T4 | AudioPair: create/connect/write/read round-trip; Close removes files; double Close safe | (same) | go test -p 2 ./internal/voice/ -run TestAudioPair | - | PASS | Given pair for ccid X; When 640B written; Then child-side reads same 640B |
| T5 | Socket dir derives from config.MeeptPath(); no hardcoded home | Modify: socket.go | go test -p 2 ./internal/voice/ -run TestSocketDir | - | PASS | Given MEEPT_HOME=tmp; When NewAudioPair; Then files under tmp/voice/sock |
| T6 | Format/vet | - | gofmt -l internal/voice; go vet ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] C1 keys byte-exact (call_id, event, text, last, from, to, direction, codec, sample_rate, reason)
- [ ] -race clean on concurrent Send
- [ ] Socket files cleaned on Close and on ctx cancel
- [ ] No reorder/pace logic here (leaf 06 seam respected)
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Frame JSON matches C1 exactly (a wrong key silently breaks the child)
- [ ] AudioPair lifecycle cannot leak socket files on error paths
- [ ] TextServer accepts exactly one child; a second connection is refused or replaces with a logged warning (pick one, document)

Output: APPROVED or specific gaps.

## Notes

Keep the text socket single-child: the process model is one child hosting
N pipelines. If leaf 13's measurement forces one-child-per-call, the text
socket becomes one listener per child with the same Frame type - the
contract does not change.
